package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/Furyfree/nimbus/internal/state"
	"github.com/Furyfree/nimbus/internal/userstate"
)

func TestPostinstallAliasAndTypoBeforeInspection(t *testing.T) {
	root, src := postinstallFixture(t)
	for _, name := range []string{"1password", "onepassword"} {
		cmd, out := postinstallCommand(root, false, name, "--help")
		if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "--mark-done") {
			t.Fatal(err, out)
		}
	}
	for _, args := range [][]string{{"1passwored", "--mark-done"}, {"1passwored"}} {
		cmd, _ := postinstallCommand(root, false, args...)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown setup task") || !strings.Contains(err.Error(), "onepassword") {
			t.Fatal(err)
		}
	}
	cmd, out := postinstallCommand(root, false, "copilot", "--help")
	if err := cmd.Execute(); err != nil || strings.Contains(out.String(), "--mark-done") {
		t.Fatal(err, out)
	}
	if len(src.reads) != 0 || len(src.streams) != 0 {
		t.Fatal("help or typo inspected native state", src.reads, src.streams)
	}
}

func TestExistingPasswordCompletionNeverApplies(t *testing.T) {
	for _, mode := range []string{"mark", "guided", "drift", "missing", "locked"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			src.Commands[nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"onePasswordSsh":true}`)
			src.Paths["/opt/1Password/op-ssh-sign"] = "/opt/1Password/op-ssh-sign"
			src.Commands["env SSH_AUTH_SOCK="+filepath.Join(os.Getenv("HOME"), ".1password", "agent.sock")+" ssh-add -l"] = []byte("private-test-key")
			targets := passwordTargets(true)
			verify := nativetest.Key("chezmoi", append([]string{"verify", "--exclude=scripts", "--"}, targets...)...)
			src.Commands[verify] = nil
			src.Commands[nativetest.Key("chezmoi", "--skip-secrets", "verify", "--exclude=scripts", "--", targets[3], targets[4])] = nil
			for _, path := range targets {
				src.Files[path] = []byte("fixture config")
			}
			if mode == "drift" {
				src.Failures[verify] = "mismatch"
			}
			if mode == "missing" {
				delete(src.Files, targets[0])
			}
			if mode == "locked" {
				src.Failures["op whoami --format=json"] = "private account diagnostic"
			}
			saved := postinstallTerminal
			postinstallTerminal = func(io.Reader) bool { return true }
			t.Cleanup(func() { postinstallTerminal = saved })
			args := []string{"1password"}
			if mode != "guided" {
				args = append(args, "--mark-done")
			}
			cmd, out := postinstallCommand(root, false, args...)
			cmd.SetIn(strings.NewReader("yes\n"))
			err := cmd.Execute()
			success := mode == "mark" || mode == "guided"
			if (err == nil) != success {
				t.Fatal(err, out)
			}
			if len(src.streams) != 0 {
				t.Fatal("verification applied configuration or launched an app", src.streams)
			}
			if strings.Contains(out.String(), "private-test-key") || strings.Contains(fmt.Sprint(err), "private account") {
				t.Fatal("private output leaked")
			}
			store, _ := userstate.Default()
			evidence, e := store.Read("postinstall")
			if e != nil || evidence.Has("vm", "onepassword.complete", 1, "verified") != success {
				t.Fatal(e, evidence)
			}
			if !passwordConfirmed("vm", true) {
				t.Fatal("GUI confirmation was lost")
			}
			src.reads = nil
			cmd, _ = postinstallCommand(root, false, "status")
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if slices.Contains(src.reads, "op whoami --format=json") {
				t.Fatal("status authenticated")
			}
		})
	}
}

type certificateDeniedSource struct{ native.Source }

func (s certificateDeniedSource) ReadFile(path string) ([]byte, error) {
	if path == postinstall.MOKCertificate {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
	}
	return s.Source.ReadFile(path)
}

func TestMOKExplicitVerificationAndUnprivilegedStatus(t *testing.T) {
	saved := postinstallTerminal
	postinstallTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { postinstallTerminal = saved })
	for _, mode := range []string{"verified", "declined", "not-enrolled", "sudo-failure"} {
		t.Run(mode, func(t *testing.T) {
			root, base := installerFixture(t)
			for path, data := range map[string]string{
				"components/nvidia.toml": "schema=1\nid='nvidia'\npackages=['akmods','akmod-nvidia','mokutil']\n",
				"profiles/common.toml":   "schema=1\nid='common'\ncomponents=['nvidia']\n",
			} {
				if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			src := &postinstallSource{FakeSource: base.FakeSource}
			selected, err := loadSelected(machineFlags{checkout: root, machine: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			for _, pkg := range selected.Resolved.Packages {
				src.Commands[nativetest.Key("dnf5", inspect.PackageQueryArgs...)] = append(src.Commands[nativetest.Key("dnf5", inspect.PackageQueryArgs...)], []byte(pkg.Name+"|0|1|1|x86_64|fedora|User\n")...)
				r := state.Receipt{Schema: state.ReceiptSchema, Machine: "vm", Resource: "package:" + pkg.Canonical, Provider: "dnf", Package: pkg.Name + ".x86_64", Verified: true, Operation: "install", PlanDigest: "fixture"}
				if err := state.Record(stateRoot, "fixture", &state.Stage{Schema: state.Schema, PlanDigest: "fixture", Receipts: []state.Receipt{r}}); err != nil {
					t.Fatal(err)
				}
			}
			src.Files[inspect.SecureBootPath] = []byte{0, 0, 0, 0, 1}
			for _, tool := range []string{"sudo", "akmods", "dracut", "modinfo", "nvidia-smi"} {
				src.Paths[tool] = "/usr/bin/" + tool
			}
			src.Paths["mokutil"] = "/usr/bin/mokutil"
			src.Commands["sudo -n -- /usr/bin/cat -- "+postinstall.MOKCertificate] = []byte("public-certificate")
			check := "sudo -n -- /usr/bin/mokutil --ignore-keyring --test-key " + postinstall.MOKCertificate
			src.Commands[check] = []byte(postinstall.MOKCertificate + " is already enrolled")
			src.ExitCodes = map[string]int{check: 1}
			if mode == "not-enrolled" {
				src.Commands[check] = []byte(postinstall.MOKCertificate + " is not enrolled")
				src.ExitCodes[check] = 0
			}
			if mode == "sudo-failure" {
				src.streamErr = errors.New("sudo failed")
			}
			withSource(t, certificateDeniedSource{src})
			for _, args := range [][]string{{"status"}, {"nvidia-mok", "--plan"}, {"nvidia-mok", "--help"}} {
				cmd, _ := postinstallCommand(root, false, args...)
				if err := cmd.Execute(); err != nil {
					t.Fatal(err)
				}
			}
			if slices.ContainsFunc(src.reads, func(c string) bool { return strings.HasPrefix(c, "sudo ") }) || len(src.streams) > 0 {
				t.Fatal("read-only inspection escalated")
			}
			cmd, out := postinstallCommand(root, false, "nvidia-mok", "--mark-done")
			answer := "yes\n"
			if mode == "declined" {
				answer = "no\n"
			}
			cmd.SetIn(strings.NewReader(answer))
			err = cmd.Execute()
			if (err == nil) != (mode == "verified") {
				t.Fatal(err, out)
			}
			store, _ := userstate.Default()
			evidence, e := store.Read("postinstall")
			if e != nil || evidence.Has("vm", "nvidia-mok.complete", 1, "verified") != (mode == "verified") {
				t.Fatal(e, evidence)
			}
			src.reads = nil
			src.streams = nil
			cmd, out = postinstallCommand(root, false, "status")
			want := "Permission denied"
			if mode == "verified" {
				want = "Previously verified"
			}
			if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), want) {
				t.Fatal(err, out)
			}
			if mode == "verified" {
				if !strings.Contains(out.String(), "Recheck: nimbus postinstall nvidia-mok (requests sudo)") {
					t.Fatal("missing administrator recheck command", out)
				}
				if strings.Contains(out.String(), "Unable to check") {
					t.Fatal("historical verification displayed as failure", out)
				}
				var result syncResult
				inspectFinal(certificateDeniedSource{src}, selected, &result, false, false)
				if slices.ContainsFunc(result.Tasks, func(task postinstall.Task) bool { return task.ID == "nvidia-mok" }) || !slices.ContainsFunc(result.Historical, func(notice string) bool { return strings.Contains(notice, "nvidia-mok: Previously verified") }) {
					t.Fatal("historical verification belongs in notices", result)
				}
				// Existing evidence cannot hide a fresh, readable negative result.
				src.Files[postinstall.MOKCertificate] = []byte("public certificate")
				src.Commands["mokutil --ignore-keyring --test-key "+postinstall.MOKCertificate] = []byte(postinstall.MOKCertificate + " is not enrolled")
				snapshot, err := inspectPostinstall(src, machineFlags{checkout: root, machine: "vm"})
				if err != nil {
					t.Fatal(err)
				}
				task, err := selectedTask(snapshot.view, "nvidia-mok")
				if err != nil || task.PreviouslyVerified || task.Status != postinstall.Pending {
					t.Fatal("saved evidence hid current state", task, err)
				}
			}
			if slices.ContainsFunc(src.reads, func(c string) bool { return strings.HasPrefix(c, "sudo ") }) || len(src.streams) > 0 {
				t.Fatal("status used saved permission to escalate")
			}
		})
	}
}
