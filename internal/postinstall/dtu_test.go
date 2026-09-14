package postinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func dtuFixture(t *testing.T) (Inputs, *nativetest.FakeSource, []byte) {
	t.Helper()
	in, src := fixture("NetworkManager", "policycoreutils", "libselinux-utils")
	in.Resolved.Components = []definitions.ResolvedComponent{{ID: "dtu-network"}}
	src.Dirs = map[string][]string{"/": {"etc"}, "/etc": {"NetworkManager"}, "/etc/NetworkManager": {}}
	for _, dir := range []string{"/etc", "/etc/NetworkManager", dtuCertificateDir} {
		src.Commands["stat --format=%F|%U|%G|%a|%h -- "+dir] = []byte("directory|root|root|755|2")
		src.Commands["stat --format=%U|%G|%a -- "+dir] = []byte("root|root|755")
	}
	for _, name := range []string{"stat", "/usr/sbin/getenforce", "/usr/sbin/matchpathcon", "/usr/sbin/restorecon"} {
		src.Paths[name] = name
	}
	src.Commands["/usr/sbin/getenforce"] = []byte("Enforcing\n")
	for _, path := range []string{dtuCertificateDir, DTUCertificatePath} {
		src.Commands["/usr/sbin/matchpathcon -n -- "+path] = []byte("system_u:object_r:NetworkManager_etc_rw_t:s0\n")
		src.Commands["stat --format=%C -- "+path] = []byte("system_u:object_r:NetworkManager_etc_rw_t:s0\n")
	}
	data, err := os.ReadFile("testdata/dtu-eduroam.pem")
	if err != nil {
		t.Fatal(err)
	}
	previousNow, previousFetch := dtuNow, dtuFetch
	t.Cleanup(func() { dtuNow, dtuFetch = previousNow, previousFetch })
	dtuNow = func() time.Time { return time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) }
	dtuFetch = func(context.Context) ([]byte, error) {
		t.Fatal("unexpected download during inspection")
		return nil, nil
	}
	t.Setenv("TMPDIR", t.TempDir())
	return in, src, data
}

func dtuInstalled(src *nativetest.FakeSource, data []byte) {
	src.Dirs["/etc/NetworkManager"] = []string{"certs"}
	src.Dirs[dtuCertificateDir] = []string{"dtu-eduroam.pem"}
	src.Files[DTUCertificatePath] = data
	src.Commands["stat --format=%F|%U|%G|%a|%h -- "+DTUCertificatePath] = []byte("regular file|root|root|644|1")
}

func TestDTUInspectionIsOfflineAndNativeStateIsAuthoritative(t *testing.T) {
	for _, mode := range []string{"missing", "matching", "wrong mode", "wrong label", "wrong directory label", "disabled SELinux", "unknown file", "expired", "unknown SELinux", "unreadable", "unsafe parent", "symlink", "missing package", "unselected", "root"} {
		t.Run(mode, func(t *testing.T) {
			in, src, data := dtuFixture(t)
			want := Pending
			if mode != "missing" {
				dtuInstalled(src, data)
			}
			switch mode {
			case "matching":
				want = Complete
			case "wrong mode":
				src.Commands["stat --format=%F|%U|%G|%a|%h -- "+DTUCertificatePath] = []byte("regular file|root|root|600|1")
			case "wrong label":
				src.Commands["stat --format=%C -- "+DTUCertificatePath] = []byte("unconfined_u:object_r:user_tmp_t:s0")
			case "wrong directory label":
				src.Commands["stat --format=%C -- "+dtuCertificateDir] = []byte("unconfined_u:object_r:user_tmp_t:s0")
			case "disabled SELinux":
				src.Commands["/usr/sbin/getenforce"] = []byte("Disabled")
				want = Complete
			case "unknown file":
				src.Files[DTUCertificatePath] = []byte("PRIVATE DO NOT RENDER")
				want = Blocked
			case "expired":
				dtuNow = func() time.Time { return time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC) }
				want = Blocked
			case "unknown SELinux":
				src.Commands["/usr/sbin/getenforce"] = []byte("unexpected")
				want = Unknown
			case "unreadable":
				src.Failures["stat --format=%F|%U|%G|%a|%h -- "+DTUCertificatePath] = "PRIVATE DO NOT RENDER"
				want = Unknown
			case "unsafe parent":
				src.Commands["stat --format=%U|%G|%a -- "+dtuCertificateDir] = []byte("root|root|777")
				want = Unknown
			case "symlink":
				src.Commands["stat --format=%F|%U|%G|%a|%h -- "+DTUCertificatePath] = []byte("symbolic link|root|root|777|1")
				want = Unknown
			case "missing package":
				in.Facts.Packages.Value = nil
				want = Blocked
			case "unselected":
				in.Resolved.Components = nil
			case "root":
				in.Facts.User.Value.Name = "root"
				want = Blocked
			}
			tasks := Inspect(src, in)
			if mode == "unselected" {
				if len(tasks) != 0 {
					t.Fatal(tasks)
				}
				return
			}
			task := findTask(t, tasks, "dtu-network")
			if task.Status != want || (task.Action != nil) != (want == Pending) {
				t.Fatalf("unexpected task: %+v", task)
			}
			encoded, _ := json.Marshal(task)
			if bytes.Contains(encoded, []byte("PRIVATE")) || bytes.Contains(encoded, []byte("BEGIN CERTIFICATE")) {
				t.Fatal("file contents leaked")
			}
		})
	}
}

type dtuSource struct {
	*nativetest.FakeSource
	t       *testing.T
	data    []byte
	mode    string
	streams []string
	staged  string
}

func (s *dtuSource) Stream(_, _ io.Writer, name string, args ...string) error {
	s.streams = append(s.streams, nativetest.Key(name, args...))
	if name != "sudo" || args[0] != "--" {
		s.t.Fatal("unexpected native mutation")
	}
	if strings.Contains(strings.Join(args, " "), "internal system-file") {
		s.staged = args[len(args)-1]
		data, err := os.ReadFile(s.staged)
		if err != nil {
			s.t.Fatal(err)
		}
		info, err := os.Stat(s.staged)
		if err != nil || info.Mode().Perm() != 0600 {
			s.t.Fatal("unsafe staging")
		}
		var payload apply.FilePayload
		if err := json.Unmarshal(data, &payload); err != nil {
			s.t.Fatal(err)
		}
		if payload.Change.Target != DTUCertificatePath || !bytes.Equal(payload.Change.After.Content, s.data) || payload.Change.After.Mode != "0644" || payload.Change.After.Owner != "root" || payload.Change.After.Group != "root" {
			s.t.Fatal("wrong file payload")
		}
		if s.mode == "write failure" {
			return errors.New("fixture write failure")
		}
		if s.mode != "no effect" {
			dtuInstalled(s.FakeSource, s.data)
		}
		if s.mode == "changed after write" {
			s.Files[DTUCertificatePath] = []byte("foreign")
		}
		return nil
	}
	if nativetest.Key(name, args...) != "sudo -- /usr/sbin/restorecon -- "+dtuCertificateDir+" "+DTUCertificatePath {
		s.t.Fatal("unexpected label target")
	}
	if s.mode == "label failure" {
		return errors.New("fixture label failure")
	}
	if s.mode == "label no effect" {
		s.Commands["stat --format=%C -- "+DTUCertificatePath] = []byte("wrong")
	}
	return nil
}

func TestDTUInstallationDownloadDriftAndVerification(t *testing.T) {
	for _, mode := range []string{"success", "disabled SELinux", "download failure", "wrong checksum", "canceled", "drift", "write failure", "label failure", "label no effect", "no effect", "changed after write"} {
		t.Run(mode, func(t *testing.T) {
			in, base, data := dtuFixture(t)
			if mode == "disabled SELinux" {
				base.Commands["/usr/sbin/getenforce"] = []byte("Disabled")
			}
			task := findTask(t, Inspect(base, in), "dtu-network")
			src := &dtuSource{FakeSource: base, t: t, data: data, mode: mode}
			fetched := 0
			dtuFetch = func(context.Context) ([]byte, error) {
				fetched++
				switch mode {
				case "download failure":
					return nil, errors.New("offline")
				case "wrong checksum":
					return []byte("not the certificate"), nil
				case "drift":
					dtuInstalled(base, []byte("foreign"))
				}
				return data, nil
			}
			ctx := t.Context()
			if mode == "canceled" {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			err := RunDTUNetwork(ctx, src, io.Discard, io.Discard, task)
			success := mode == "success" || mode == "disabled SELinux"
			if (err == nil) != success {
				t.Fatalf("result: %v", err)
			}
			if mode == "canceled" && fetched != 0 {
				t.Fatal("download after cancellation")
			}
			if (mode == "download failure" || mode == "wrong checksum" || mode == "canceled" || mode == "drift") && len(src.streams) != 0 {
				t.Fatal("mutation before validation")
			}
			if mode == "disabled SELinux" && len(src.streams) != 1 {
				t.Fatal("labeling on disabled SELinux")
			}
			if src.staged != "" {
				if _, err := os.Stat(filepath.Dir(src.staged)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("staged payload remains")
				}
			}
			if success && findTask(t, Inspect(base, in), "dtu-network").Status != Complete {
				t.Fatal("install did not converge")
			}
		})
	}
}

type dtuTransport func(*http.Request) (*http.Response, error)

func (f dtuTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDTUDownloadHTTPBoundaries(t *testing.T) {
	for _, mode := range []string{"success", "redirect", "status", "oversized", "failure"} {
		t.Run(mode, func(t *testing.T) {
			old := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = old })
			requests := 0
			http.DefaultTransport = dtuTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.String() != DTUCertificateURL || req.Method != http.MethodGet {
					t.Fatal("unexpected download endpoint")
				}
				response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("bundle")), Request: req}
				switch mode {
				case "redirect":
					response.StatusCode = 302
					response.Header.Set("Location", "http://untrusted.invalid/cert")
				case "status":
					response.StatusCode = 503
				case "oversized":
					response.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", dtuMaxDownload+1)))
				case "failure":
					return nil, errors.New("offline")
				}
				return response, nil
			})
			_, err := fetchDTUCertificate(t.Context())
			if (err == nil) != (mode == "success") || requests != 1 {
				t.Fatalf("result: %v, requests %d", err, requests)
			}
		})
	}
}

func TestDTUCertificatePinValidityAndForgedActions(t *testing.T) {
	in, src, data := dtuFixture(t)
	if err := validateDTUCertificate(data, dtuNow()); err != nil {
		t.Fatal(err)
	}
	for _, now := range []time.Time{time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 12, 2, 11, 25, 30, 0, time.UTC)} {
		if validateDTUCertificate(data, now) == nil {
			t.Fatal("accepted outside bundle validity")
		}
	}
	for _, payload := range [][]byte{nil, []byte("<html>error</html>"), append(bytes.Clone(data), 'x')} {
		if validateDTUCertificate(payload, dtuNow()) == nil {
			t.Fatal("accepted unpinned data")
		}
	}
	for _, mode := range []string{"id", "kind", "digest", "argv", "commands", "status"} {
		task := findTask(t, Inspect(src, in), "dtu-network")
		switch mode {
		case "id":
			task.ID = "other"
		case "kind":
			task.Action.Kind = OpenApplication
		case "digest":
			task.Action.DTU.Observed = "invalid"
		case "argv":
			task.Action.Argv = []string{"touch", "/tmp/other"}
		case "commands":
			task.Action.Commands = [][]string{{"true"}}
		case "status":
			task.Status = Complete
		}
		if _, err := DTUCommands(task); err == nil {
			t.Fatal("accepted forged action", mode)
		}
	}
}
