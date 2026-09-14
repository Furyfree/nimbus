package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/Furyfree/nimbus/internal/state"
	"github.com/Furyfree/nimbus/internal/userstate"
)

type dtuHTTP func(*http.Request) (*http.Response, error)

func (f dtuHTTP) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDTUCLIApprovalAndCompletion(t *testing.T) {
	for _, mode := range []string{"help", "preview", "json", "status", "cancel", "success", "already installed", "failure", "no effect", "drift"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\ncomponents=['dtu-network']\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "components"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "components/dtu-network.toml"), []byte("schema=1\nid='dtu-network'\npackages=['NetworkManager','policycoreutils','libselinux-utils']\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"NetworkManager", "policycoreutils", "libselinux-utils"} {
				key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
				src.Commands[key] = append(src.Commands[key], []byte(name+"|0|1|1|x86_64|fedora|User\n")...)
				r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:dnf:" + name, Provider: "dnf", Verified: true, Operation: "install", PlanDigest: "fixture"}
				if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
					t.Fatal(err)
				}
			}
			src.Dirs = map[string][]string{"/": {"etc"}, "/etc": {"NetworkManager"}, "/etc/NetworkManager": {}}
			for _, dir := range []string{"/etc", "/etc/NetworkManager", "/etc/NetworkManager/certs"} {
				src.Commands["stat --format=%F|%U|%G|%a|%h -- "+dir] = []byte("directory|root|root|755|2")
				src.Commands["stat --format=%U|%G|%a -- "+dir] = []byte("root|root|755")
			}
			for _, name := range []string{"stat", "/usr/sbin/getenforce", "/usr/sbin/matchpathcon", "/usr/sbin/restorecon"} {
				src.Paths[name] = name
			}
			src.Commands["/usr/sbin/getenforce"] = []byte("Enforcing")
			for _, path := range []string{"/etc/NetworkManager/certs", postinstall.DTUCertificatePath} {
				src.Commands["/usr/sbin/matchpathcon -n -- "+path] = []byte("system_u:object_r:NetworkManager_etc_t:s0\n")
				src.Commands["stat --format=%C -- "+path] = []byte("unconfined_u:object_r:NetworkManager_etc_t:s0\n")
				src.Commands["/usr/sbin/matchpathcon -V -- "+path] = []byte(path + " verified.\n")
			}
			certificate, err := os.ReadFile("../postinstall/testdata/dtu-eduroam.pem")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "already installed" {
				src.Dirs["/etc/NetworkManager"] = []string{"certs"}
				src.Dirs["/etc/NetworkManager/certs"] = []string{"dtu-eduroam.pem"}
				src.Files[postinstall.DTUCertificatePath] = certificate
				src.Commands["stat --format=%F|%U|%G|%a|%h -- "+postinstall.DTUCertificatePath] = []byte("regular file|root|root|644|1")
			}
			requests := 0
			oldHTTP := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = oldHTTP })
			http.DefaultTransport = dtuHTTP(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.String() != postinstall.DTUCertificateURL {
					t.Fatal("unexpected network request")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(certificate)), Header: make(http.Header), Request: req}, nil
			})
			src.onStream = func(command string) {
				if command == "sudo -- /usr/sbin/restorecon -- /etc/NetworkManager/certs "+postinstall.DTUCertificatePath {
					return
				}
				if !strings.Contains(command, "internal system-file --plan ") {
					t.Fatal("unexpected mutation", command)
				}
				if mode == "no effect" {
					return
				}
				fields := strings.Fields(command)
				data, err := os.ReadFile(fields[len(fields)-1])
				if err != nil {
					t.Fatal(err)
				}
				var payload apply.FilePayload
				if err := json.Unmarshal(data, &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Change.Target != postinstall.DTUCertificatePath {
					t.Fatal("wrong destination")
				}
				src.Dirs["/etc/NetworkManager"] = []string{"certs"}
				src.Dirs["/etc/NetworkManager/certs"] = []string{"dtu-eduroam.pem"}
				src.Files[postinstall.DTUCertificatePath] = payload.Change.After.Content
				src.Commands["stat --format=%F|%U|%G|%a|%h -- "+postinstall.DTUCertificatePath] = []byte("regular file|root|root|644|1")
			}
			args := []string{"dtu-network", "--yes"}
			switch mode {
			case "help":
				args = []string{"dtu-network", "--help"}
			case "preview", "json":
				args = []string{"dtu-network", "--plan"}
			case "status":
				args = []string{"status"}
			case "failure":
				src.streamErr = io.ErrUnexpectedEOF
			case "cancel", "drift":
				args = []string{"dtu-network"}
				oldTerminal, oldApprover := postinstallTerminal, approver
				t.Cleanup(func() { postinstallTerminal, approver = oldTerminal, oldApprover })
				postinstallTerminal = func(io.Reader) bool { return true }
				approver = func(io.Reader, io.Writer, string) bool {
					if mode == "drift" {
						src.Commands["/usr/sbin/getenforce"] = []byte("Permissive")
					}
					return mode != "cancel"
				}
			}
			cmd, out := postinstallCommand(root, mode == "json", args...)
			err = cmd.Execute()
			readOnly := slices.Contains([]string{"help", "preview", "json", "status"}, mode)
			if (err == nil) != (readOnly || mode == "success" || mode == "already installed") {
				t.Fatalf("result: %v\n%s", err, out)
			}
			if (readOnly || mode == "cancel" || mode == "drift" || mode == "already installed") && (requests != 0 || len(src.streams) != 0) {
				t.Fatal("unapproved work")
			}
			store, err := userstate.Default()
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := store.Read("postinstall")
			if err != nil {
				t.Fatal(err)
			}
			if evidence.Has("vm", "dtu-network.complete", 1, "verified") != (mode == "success" || mode == "already installed") {
				t.Fatal("incorrect completion record")
			}
			if mode == "success" {
				cmd, out = postinstallCommand(root, false, "dtu-network", "--yes")
				if err := cmd.Execute(); err != nil || requests != 1 || len(src.streams) != 2 {
					t.Fatalf("repeat did not converge: %v %s", err, out)
				}
				delete(src.Files, postinstall.DTUCertificatePath)
				src.Dirs["/etc/NetworkManager/certs"] = nil
				cmd, out = postinstallCommand(root, false, "status")
				if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "Pending") {
					t.Fatalf("completion hid missing file: %v %s", err, out)
				}
			}
		})
	}
}
