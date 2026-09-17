package cli

import (
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
	for _, mode := range []string{"help", "preview", "json", "status", "cancel", "success", "already installed", "failure", "no effect", "drift", "profile drift", "profile failure", "connection failure", "profile no effect", "invalid item", "item success", "certificate only", "keep existing", "empty answer", "replace existing", "replacement drift", "baseline prerequisites"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\ncomponents=['dtu-network']\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "components"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "components/dtu-network.toml"), []byte("schema=1\nid='dtu-network'\npackages=['NetworkManager','NetworkManager-wifi','policycoreutils','libselinux-utils','python3','python3-dbus']\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"NetworkManager", "NetworkManager-wifi", "policycoreutils", "libselinux-utils", "python3", "python3-dbus"} {
				key := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
				src.Commands[key] = append(src.Commands[key], []byte(name+"|0|1|1|x86_64|fedora|User\n")...)
				r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:dnf:" + name, Provider: "dnf", Verified: true, Operation: "install", PlanDigest: "fixture"}
				if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "baseline prerequisites" {
				names := []string{"NetworkManager-wifi.x86_64", "python3-dbus.x86_64", "python3.x86_64"}
				slices.Sort(names)
				baseline := state.Baseline{Schema: state.BaselineSchema, Packages: names}
				data, err := json.Marshal(baseline)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(stateRoot, state.BaselineFile), data, 0600); err != nil {
					t.Fatal(err)
				}
				remove := []string{"package:dnf:NetworkManager-wifi", "package:dnf:python3", "package:dnf:python3-dbus"}
				if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Remove: remove}); err != nil {
					t.Fatal(err)
				}
			}
			inspectArgv := postinstall.DTUProfileCommand("inspect")
			inspectKey := nativetest.Key(inspectArgv[0], inspectArgv[1:]...)
			pendingProfile := `{"observed":"` + strings.Repeat("a", 64) + `","configured":false,"connected":false}`
			readyProfile := `{"observed":"` + strings.Repeat("b", 64) + `","configured":true,"connected":false,"existing":[{"uuid":"test-profile-id","ssid":"eduroam"}]}`
			src.Commands[inspectKey] = []byte(pendingProfile)
			existingMode := slices.Contains([]string{"keep existing", "empty answer", "replace existing", "replacement drift"}, mode)
			if existingMode {
				src.Commands[inspectKey] = []byte(strings.Replace(readyProfile, `"configured":true`, `"configured":false`, 1))
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
			certificate, err := os.ReadFile("../postinstall/dtu/ca.pem")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "already installed" || mode == "certificate only" {
				if mode == "already installed" {
					src.Commands[inspectKey] = []byte(readyProfile)
				}
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
				t.Fatal("unexpected network request")
				return nil, io.ErrUnexpectedEOF
			})
			src.onStream = func(command string) {
				if strings.HasPrefix(command, "python3 -I -B -c ") {
					if mode == "profile failure" {
						src.streamErr = io.ErrUnexpectedEOF
						return
					}
					if mode == "profile no effect" {
						return
					}
					if mode == "replace existing" && !strings.HasSuffix(command, "--replace-existing") {
						t.Fatal("replacement was not bound to approval")
					}
					if mode == "item success" && !strings.HasSuffix(command, "--onepassword-item "+strings.Repeat("a", 26)) {
						t.Fatal("missing item UUID")
					}
					src.Commands[inspectKey] = []byte(readyProfile)
					if mode == "connection failure" {
						src.streamErr = io.ErrUnexpectedEOF
					}
					return
				}
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
			case "keep existing", "empty answer", "replace existing", "replacement drift":
				oldTerminal := postinstallTerminal
				t.Cleanup(func() { postinstallTerminal = oldTerminal })
				postinstallTerminal = func(io.Reader) bool { return true }
			case "help":
				args = []string{"dtu-network", "--help"}
			case "preview", "json":
				args = []string{"dtu-network", "--plan"}
			case "status":
				args = []string{"status"}
			case "failure":
				src.streamErr = io.ErrUnexpectedEOF
			case "invalid item":
				args = append(args, "--onepassword-item", "not-an-item-uuid")
			case "item success":
				args = append(args, "--onepassword-item", strings.Repeat("a", 26))
			case "cancel", "drift", "profile drift":
				args = []string{"dtu-network"}
				oldTerminal, oldApprover := postinstallTerminal, approver
				t.Cleanup(func() { postinstallTerminal, approver = oldTerminal, oldApprover })
				postinstallTerminal = func(io.Reader) bool { return true }
				approver = func(io.Reader, io.Writer, string) bool {
					if mode == "drift" {
						src.Commands["/usr/sbin/getenforce"] = []byte("Permissive")
					}
					if mode == "profile drift" {
						src.Commands[inspectKey] = []byte(readyProfile)
					}
					return mode != "cancel"
				}
			}
			cmd, out := postinstallCommand(root, mode == "json", args...)
			if existingMode {
				answer := "n\n"
				if mode == "empty answer" {
					answer = "\n"
				}
				if mode == "replace existing" || mode == "replacement drift" {
					answer = "yes\n"
				}
				if mode == "replacement drift" {
					cmd.SetIn(&dtuChangingReader{Reader: strings.NewReader(answer), change: func() { src.Commands[inspectKey] = []byte(readyProfile) }})
				} else {
					cmd.SetIn(strings.NewReader(answer))
				}
			}
			err = cmd.Execute()
			success := slices.Contains([]string{"success", "already installed", "item success", "certificate only", "replace existing", "baseline prerequisites"}, mode)
			readOnly := slices.Contains([]string{"help", "preview", "json", "status"}, mode)
			kept := mode == "keep existing" || mode == "empty answer"
			if (err == nil) != (readOnly || success || kept) {
				t.Fatalf("result: %v\n%s", err, out)
			}
			if (readOnly || mode == "cancel" || mode == "drift" || mode == "profile drift" || mode == "invalid item" || mode == "already installed" || kept || mode == "replacement drift") && (requests != 0 || len(src.streams) != 0) {
				t.Fatal("unapproved work")
			}
			if mode == "certificate only" && len(src.streams) != 1 {
				t.Fatal("already prepared CA was rewritten")
			}
			if mode == "profile failure" || mode == "profile no effect" {
				if len(src.Files[postinstall.DTUCertificatePath]) == 0 {
					t.Fatal("partial CA installation lost")
				}
			}
			if mode == "connection failure" {
				if !strings.Contains(out.String(), "After action: dtu-network: setup failed") || strings.Contains(out.String(), "After action: dtu-network: complete") {
					t.Fatalf("misleading failure report: %s", out)
				}
			}
			if mode == "baseline prerequisites" {
				applied, err := state.Read(stateRoot)
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"NetworkManager-wifi", "python3", "python3-dbus"} {
					if _, adopted := applied.Receipts["package:dnf:"+name]; adopted {
						t.Fatal("postinstall adopted a prerequisite")
					}
				}
			}
			store, err := userstate.Default()
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := store.Read("postinstall")
			if err != nil {
				t.Fatal(err)
			}
			if evidence.Has("vm", "dtu-network.complete", 1, "verified") != (success && mode != "already installed") {
				t.Fatal("incorrect completion record")
			}
			if mode == "success" {
				cmd, out = postinstallCommand(root, false, "dtu-network", "--yes")
				if err := cmd.Execute(); err != nil || requests != 0 || len(src.streams) != 3 {
					t.Fatalf("repeat did not converge: %v %s", err, out)
				}
				delete(src.Files, postinstall.DTUCertificatePath)
				src.Dirs["/etc/NetworkManager/certs"] = nil
				cmd, out = postinstallCommand(root, false, "status")
				if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "existing eduroam/DTUsecure") {
					t.Fatalf("completion hid missing file: %v %s", err, out)
				}
			}
		})
	}
}

type dtuChangingReader struct {
	*strings.Reader
	change func()
}

func (r *dtuChangingReader) Read(p []byte) (int, error) {
	r.change()
	return r.Reader.Read(p)
}
