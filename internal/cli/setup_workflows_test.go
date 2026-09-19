package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/Furyfree/nimbus/internal/userstate"
)

func TestNotesRevisionDeliveryAndConsumption(t *testing.T) {
	root, base := installerFixture(t)
	catalog := filepath.Join(root, "setup-notes.json")
	base.Files[catalog] = []byte(`{"schema":1,"notes":[{"id":"dotfiles.shell","revision":1,"text":"Open a new terminal","profiles":[]},{"id":"dotfiles.desktop","revision":1,"text":"Desktop only","profiles":["hyprland-noctalia"]}]}`)
	for range 2 {
		code, out, errOut := run(t, "sync", "--checkout", root, "--machine", "vm", "--yes")
		if code != 0 || !strings.Contains(out, "1 setup note. Run: nimbus setup-notes") ||
			strings.Contains(out, "Open a new terminal") || strings.Contains(out, "Desktop only") {
			t.Fatalf("%d %s%s", code, out, errOut)
		}
	}
	code, out, errOut := run(t, "setup-notes", "--checkout", root, "--machine", "vm")
	if code != 0 || !strings.Contains(out, "Review remaining setup with nimbus postinstall status.") || !strings.Contains(out, "Open a new terminal") || strings.Contains(out, "Desktop only") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	code, out, errOut = run(t, "sync", "--checkout", root, "--machine", "vm", "--yes")
	if code != 0 || strings.Contains(out, "setup note. Run: nimbus setup-notes") {
		t.Fatalf("displayed note stayed pending: %d %s%s", code, out, errOut)
	}
	base.Files[catalog] = []byte(`{"schema":1,"notes":[{"id":"dotfiles.shell","revision":2,"text":"Revised instructions","profiles":[]}]}`)
	code, out, errOut = run(t, "sync", "--checkout", root, "--machine", "vm", "--yes")
	if code != 0 || !strings.Contains(out, "1 setup note") || strings.Contains(out, "Revised instructions") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	code, out, errOut = run(t, "setup-notes", "--checkout", root, "--machine", "vm")
	if code != 0 || !strings.Contains(out, "Revised instructions") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	store, err := userstate.Default()
	if err != nil {
		t.Fatal(err)
	}
	f, err := store.Read("postinstall")
	if err != nil || len(f.Machines) != 0 {
		t.Fatal("notes completed tasks", f, err)
	}
}
func TestHelpNeverInspectsOrCreatesState(t *testing.T) {
	root, src := postinstallFixture(t)
	for _, args := range [][]string{nil, {"--help"}, {"onepassword", "--help"}} {
		cmd, out := postinstallCommand(root, false, args...)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "1Password") {
			t.Fatal(out)
		}
	}
	if len(src.reads) != 0 || len(src.streams) != 0 {
		t.Fatal(src.reads, src.streams)
	}
	store, _ := userstate.Default()
	if _, err := os.Stat(store.Dir); !os.IsNotExist(err) {
		t.Fatal("help created local state")
	}
}
func TestOnePasswordSelectedFilesAndFailedVerification(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "ineffective apply"}[fail], func(t *testing.T) {
			root, src := postinstallFixture(t)
			src.Commands[nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["common"],"onePasswordSsh":true}`)
			src.Paths["/opt/1Password/op-ssh-sign"] = "/opt/1Password/op-ssh-sign"
			src.Commands["env SSH_AUTH_SOCK="+filepath.Join(os.Getenv("HOME"), ".1password", "agent.sock")+" ssh-add -l"] = []byte("public identity, never printed")
			targets := passwordTargets(true)
			src.Commands[nativetest.Key("chezmoi", append([]string{"--color=false", "status", "--parent-dirs", "--exclude=scripts", "--"}, targets...)...)] = []byte(" A .config/1Password/ssh\n A .config/1Password/ssh/agent.toml\n M .config/git/config\n")
			cat := nativetest.Key("chezmoi", append([]string{"cat", "--"}, targets...)...)
			verify := nativetest.Key("chezmoi", append([]string{"verify", "--exclude=scripts", "--"}, targets...)...)
			src.Commands[cat] = []byte("synthetic SSH and public selectors")
			src.Commands[verify] = nil
			src.Commands[nativetest.Key("chezmoi", "--skip-secrets", "verify", "--exclude=scripts", "--", targets[3], targets[4])] = nil
			if fail {
				src.Failures[verify] = "mismatch"
			}
			src.onStream = func(call string) {
				if strings.HasPrefix(call, "chezmoi apply --parent-dirs --exclude=scripts -- ") {
					for _, path := range targets {
						src.Files[path] = []byte("synthetic selected file")
					}
				}
			}
			savedTerminal, savedApprover := postinstallTerminal, approver
			t.Cleanup(func() { postinstallTerminal, approver = savedTerminal, savedApprover })
			postinstallTerminal = func(io.Reader) bool { return true }
			approver = func(io.Reader, io.Writer, string) bool { return true }
			cmd, out := postinstallCommand(root, false, "onepassword")
			err := cmd.Execute()
			if (err != nil) != fail {
				t.Fatalf("%v %s", err, out)
			}
			if strings.Contains(out.String(), "public identity") {
				t.Fatal("identity output leaked")
			}
			want := nativetest.Key("chezmoi", append([]string{"apply", "--parent-dirs", "--exclude=scripts", "--"}, targets...)...)
			if !slices.Contains(src.streams, want) || slices.Contains(src.streams, "chezmoi apply") {
				t.Fatal(src.streams)
			}
			snapshot, err := inspectPostinstall(src, machineFlags{checkout: root, machine: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			task, _ := selectedTask(snapshot.view, "onepassword")
			if (task.Status == "complete") == fail {
				t.Fatal(task)
			}
			src.Files[targets[0]] = []byte("changed native SSH file")
			snapshot, err = inspectPostinstall(src, machineFlags{checkout: root, machine: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			task, _ = selectedTask(snapshot.view, "onepassword")
			if task.Status == "complete" {
				t.Fatal("stored completion hid changed SSH configuration")
			}
			if err := resetTask("vm", "onepassword"); err != nil {
				t.Fatal(err)
			}
			if taskConfirmed("vm", "onepassword") {
				t.Fatal("reset left manual confirmation")
			}
		})
	}
}
func TestFinalReportRetainsFailuresAndNotices(t *testing.T) {
	r := syncResult{Steps: []runStep{{Name: "configuration", Status: "succeeded"}, {Name: "updates", Status: "failed", Detail: "download failed"}}, Notices: []string{"Noctalia overrides disable lockscreen widgets"}, Notes: []setupNote{{ID: "note", Revision: 1, Text: "Guidance"}}, Tasks: []postinstall.Task{{ID: "nvidia-mok", Status: postinstall.Pending, Title: "Sign NVIDIA modules and enroll their key", Detail: "certificate pending enrollment", Reboot: true, BeforeReboot: true}, {ID: "fde-enroll", Status: postinstall.Blocked, Title: "Enroll the FDE key", Detail: "reboot first", Reboot: true, BeforeReboot: true}, {ID: "hyprland-plugins", Status: postinstall.Unknown, Session: true, Detail: "Run this task from a terminal in the active Hyprland desktop session."}, {ID: "fingerprint", Status: postinstall.Unknown, Detail: "Fingerprint device and enrollment state could not be established."}}, Reboot: true}
	var out strings.Builder
	if err := r.render(&out, false); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"download failed", "Noctalia overrides", "Reboot required", "Run before rebooting: nimbus postinstall nvidia-mok (Sign NVIDIA modules and enroll their key)", "3 tasks, 1 setup note. Run: nimbus setup-notes", "Verification problems:", "Fingerprint device and enrollment state could not be established."} {
		if !strings.Contains(out.String(), text) {
			t.Fatal(out.String())
		}
	}
	if strings.Contains(out.String(), "Guidance") || strings.Contains(out.String(), "fde-enroll") || strings.Contains(out.String(), "active Hyprland desktop session") || strings.Count(out.String(), "Run before rebooting") != 1 {
		t.Fatal("report repeated task detail or mislabeled a task", out.String())
	}
	var finish strings.Builder
	if err := renderInstallFinish(&finish, &r, "/tmp/logs"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Logs: /tmp/logs", "Before rebooting:", "Sign NVIDIA modules and enroll their key: $ nimbus postinstall nvidia-mok", "Reboot to finish, then open a terminal and run:", "Remaining setup and guidance (3 tasks, 1 setup note): $ nimbus setup-notes"} {
		if !strings.Contains(finish.String(), text) {
			t.Fatal(finish.String())
		}
	}
	if strings.Contains(finish.String(), "fde-enroll") || strings.Contains(finish.String(), "Fingerprint") {
		t.Fatal("finish block repeated remaining or blocked tasks", finish.String())
	}
	logout := r
	logout.Reboot, logout.Logout = false, true
	var logoutOut strings.Builder
	if err := renderInstallFinish(&logoutOut, &logout, "/tmp/logs"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logoutOut.String(), "Log out and back in to finish, then open a terminal and run:") {
		t.Fatal(logoutOut.String())
	}
	quiet := r
	quiet.Reboot, quiet.Logout = false, false
	var quietOut strings.Builder
	if err := renderInstallFinish(&quietOut, &quiet, "/tmp/logs"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(quietOut.String(), "Open a terminal and run:") || strings.Contains(quietOut.String(), "finish, then") {
		t.Fatal(quietOut.String())
	}
	if err := r.render(&previewErrorWriter{err: syscall.ENOSPC}, false); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal(err)
	}
}

func TestNotesAreOwnedByNimbusWithoutChezmoiInspection(t *testing.T) {
	root, base := installerFixture(t)
	base.Files[filepath.Join(root, "setup-notes.json")] = []byte(`{"schema":1,"notes":[{"id":"nimbus.test","revision":1,"text":"Nimbus guidance"},{"id":"dotfiles.test","revision":1,"text":"Selected dotfiles guidance","requires_dotfiles":true}]}`)
	src := &maintenanceSource{Source: base}
	withSource(t, src)
	delete(base.Commands, "chezmoi source-path")
	code, out, errOut := run(t, "setup-notes", "--checkout", root, "--machine", "vm")
	if code != 0 || !strings.Contains(out, "Selected dotfiles guidance") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	if len(src.events) != 0 {
		t.Fatalf("guidance invoked external tools: %v", src.events)
	}
	selected, err := loadSelected(machineFlags{checkout: root, machine: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	selected.Checkout.Machines["vm"].Dotfiles = nil
	notes, err := setupNotes(src, selected)
	if err != nil || len(notes) != 1 || notes[0].ID != "nimbus.test" {
		t.Fatal(notes, err)
	}
	delete(base.Files, filepath.Join(root, "setup-notes.json"))
	if _, err := setupNotes(src, selected); err == nil {
		t.Fatal("missing Nimbus catalog was silently ignored")
	}
}

func TestShippedNimbusNoteCatalog(t *testing.T) {
	root, src := installerFixture(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "setup-notes.json"))
	if err != nil {
		t.Fatal(err)
	}
	src.Files[filepath.Join(root, "setup-notes.json")] = data
	selected, err := loadSelected(machineFlags{checkout: root, machine: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	selected.Resolved.Profiles = append(selected.Resolved.Profiles, "hyprland-noctalia")
	notes, err := setupNotes(src, selected)
	if err != nil || len(notes) != 7 {
		t.Fatal(notes, err)
	}
}
