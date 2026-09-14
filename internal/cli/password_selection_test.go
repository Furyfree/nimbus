package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/userstate"
)

func TestPasswordOptInAndTargetedApply(t *testing.T) {
	for _, mode := range []string{"new", "cli-complete", "init-failed", "choice-not-saved", "profiles-changed", "approval-drift", "gui-declined", "apply-failed"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			data := nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)
			src.Commands[data] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["development","common"]}`)
			if mode == "cli-complete" {
				if err := confirmPasswordState("vm", false); err != nil {
					t.Fatal(err)
				}
				fingerprint, _ := passwordFingerprint(src, false)
				store, _ := userstate.Default()
				if err := store.Update("postinstall", "vm", map[string]userstate.Evidence{"onepassword.complete": {Revision: 1, Source: "verified", Fingerprint: fingerprint}}, nil); err != nil {
					t.Fatal(err)
				}
			}
			src.Paths["/opt/1Password/op-ssh-sign"] = "/opt/1Password/op-ssh-sign"
			src.Commands["env SSH_AUTH_SOCK="+filepath.Join(os.Getenv("HOME"), ".1password", "agent.sock")+" ssh-add -l"] = nil
			targets := passwordTargets(true)
			src.Commands[nativetest.Key("chezmoi", append([]string{"cat", "--"}, targets...)...)] = []byte("fixture rendered config")
			src.Commands[nativetest.Key("chezmoi", append([]string{"verify", "--exclude=scripts", "--"}, targets...)...)] = nil
			src.Commands[nativetest.Key("chezmoi", "--skip-secrets", "verify", "--exclude=scripts", "--", targets[3], targets[4])] = nil
			init := "chezmoi init --prompt --promptString Machine=vm --promptBool ManagedByNimbus=true --promptMultichoice Profiles=development/common --promptBool Enable 1Password SSH integration=true"
			apply := nativetest.Key("chezmoi", append([]string{"apply", "--exclude=scripts", "--"}, targets...)...)
			src.onStream = func(call string) {
				switch {
				case strings.HasPrefix(call, "chezmoi init"):
					if call != init || slices.Contains(src.reads, "op whoami --format=json") {
						t.Fatal("changed selection or authenticated before opt-in", call, src.reads)
					}
					if mode == "init-failed" {
						src.streamErr = errors.New("fixture init failure")
					}
					if mode != "choice-not-saved" {
						src.Commands[data] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["development","common"],"onePasswordSsh":true}`)
					}
					if mode == "profiles-changed" {
						src.Commands[data] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["common"],"onePasswordSsh":true}`)
					}
				case call == apply:
					if !passwordConfirmed("vm", true) {
						t.Fatal("SSH GUI work was not confirmed")
					}
					if mode == "apply-failed" {
						src.streamErr = errors.New("fixture apply failure")
						return
					}
					for _, path := range targets {
						src.Files[path] = []byte("fixture applied config")
					}
				case !strings.HasPrefix(call, "chezmoi diff --exclude=scripts -- "):
					t.Fatalf("unexpected action: %s", call)
				}
			}
			savedTerminal, savedPrompt, savedApprover := postinstallTerminal, promptLineFn, approver
			t.Cleanup(func() { postinstallTerminal, promptLineFn, approver = savedTerminal, savedPrompt, savedApprover })
			postinstallTerminal = func(io.Reader) bool { return true }
			promptLineFn = func(io.Reader, io.Writer, string, string) (string, error) {
				if mode == "approval-drift" {
					src.Commands[data] = []byte(`{"Machine":"another","ManagedByNimbus":true}`)
				}
				return "yes", nil
			}
			approver = func(io.Reader, io.Writer, string) bool { return mode != "gui-declined" }
			cmd, out := postinstallCommand(root, false, "1password")
			err := cmd.Execute()
			success := mode == "new" || mode == "cli-complete"
			if (err == nil) != success {
				t.Fatal(err, out)
			}
			if slices.Contains(src.streams, apply) != (success || mode == "apply-failed") {
				t.Fatal("incorrect apply behavior", src.streams)
			}
			snapshot, err := inspectPostinstall(src, machineFlags{checkout: root, machine: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			task, _ := selectedTask(snapshot.view, "onepassword")
			if (task.Status == "complete") != success {
				t.Fatal("completion hid incomplete setup", task)
			}
			if success {
				promptLineFn = func(io.Reader, io.Writer, string, string) (string, error) {
					t.Fatal("asked for opt-in again")
					return "", nil
				}
				src.streams = nil
				cmd, _ = postinstallCommand(root, false, "1password")
				if err := cmd.Execute(); err != nil || len(src.streams) != 0 {
					t.Fatal("completed setup was reapplied", err, src.streams)
				}
			}
		})
	}
}

func TestPasswordOptInNeverImplicit(t *testing.T) {
	for _, mode := range []string{"no", "default", "invalid", "eof", "yes-flag", "nonterminal", "plan", "mark-done", "reset", "help", "status"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			if err := confirmPasswordState("vm", false); err != nil {
				t.Fatal(err)
			}
			savedTerminal := postinstallTerminal
			t.Cleanup(func() { postinstallTerminal = savedTerminal })
			postinstallTerminal = func(io.Reader) bool { return mode != "nonterminal" }
			args := []string{"onepassword"}
			if slices.Contains([]string{"plan", "mark-done", "reset", "help"}, mode) {
				args = append(args, "--"+mode)
			}
			if mode == "yes-flag" || mode == "nonterminal" {
				args = append(args, "--yes")
			}
			if mode == "status" {
				args = []string{"status"}
			}
			cmd, out := postinstallCommand(root, false, args...)
			input := "no\n"
			if mode == "default" || mode == "yes-flag" {
				input = "\n"
			} else if mode == "invalid" {
				input = "maybe\n"
			} else if mode == "eof" {
				input = ""
			}
			cmd.SetIn(strings.NewReader(input))
			err := cmd.Execute()
			if (err != nil) != (mode == "invalid" || mode == "eof") || len(src.streams) != 0 {
				t.Fatal(err, out, src.streams)
			}
			selection, err := passwordSelection(src)
			if err != nil || selection.OnePasswordSSH {
				t.Fatal("implicit opt-in", selection, err)
			}
			if slices.Contains([]string{"plan", "mark-done", "reset", "help", "status"}, mode) && strings.Contains(out.String(), "Enable 1Password SSH/Git integration?") {
				t.Fatal("verification/inspection offered mutation", out)
			}
		})
	}
}
