package cli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/userstate"
)

func TestPasswordOptInAndTargetedApply(t *testing.T) {
	for _, mode := range []string{"new", "diff", "native-first-install", "cli-complete", "init-failed", "choice-not-saved", "profiles-changed", "approval-drift", "gui-declined", "apply-declined", "status-drift", "apply-failed"} {
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
			src.Commands[nativetest.Key("chezmoi", append([]string{"--color=false", "status", "--parent-dirs", "--exclude=scripts", "--"}, targets...)...)] = []byte(" A .config/1Password/ssh\n A .config/1Password/ssh/agent.toml\n M .config/git/config\n")
			src.Commands[nativetest.Key("chezmoi", append([]string{"cat", "--"}, targets...)...)] = []byte("fixture rendered config")
			src.Commands[nativetest.Key("chezmoi", append([]string{"verify", "--exclude=scripts", "--"}, targets...)...)] = nil
			src.Commands[nativetest.Key("chezmoi", "--skip-secrets", "verify", "--exclude=scripts", "--", targets[3], targets[4])] = nil
			init := "chezmoi init --prompt --promptString Machine=vm --promptBool ManagedByNimbus=true --promptMultichoice Profiles=development/common --promptBool Enable 1Password SSH integration=true"
			apply := nativetest.Key("chezmoi", append([]string{"apply", "--parent-dirs", "--exclude=scripts", "--"}, targets...)...)
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
					if mode == "native-first-install" {
						testPasswordNativeApply(t, call, targets)
					}
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
				case mode != "diff" || !strings.HasPrefix(call, "chezmoi --no-pager diff --parent-dirs --exclude=scripts -- "):
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
			approvals := 0
			approver = func(io.Reader, io.Writer, string) bool {
				approvals++
				if approvals == 2 && mode == "status-drift" {
					src.Commands[nativetest.Key("chezmoi", append([]string{"--color=false", "status", "--parent-dirs", "--exclude=scripts", "--"}, targets...)...)] = []byte(" M .config/1Password/ssh\n")
				}
				return mode != "gui-declined" && !(mode == "apply-declined" && approvals == 2)
			}
			args := []string{"1password"}
			if mode == "diff" {
				args = append(args, "--diff")
			}
			cmd, out := postinstallCommand(root, false, args...)
			err := cmd.Execute()
			success := slices.Contains([]string{"new", "diff", "native-first-install", "cli-complete"}, mode)
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
				if !strings.Contains(out.String(), "Create .config/1Password/ssh") || !strings.Contains(out.String(), "Update .config/git/config") || strings.Contains(out.String(), "fixture rendered config") {
					t.Fatal("missing concise preview or leaked content", out)
				}
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

// Exercise the actual apply invocation against native Chezmoi with synthetic
// files only. Authentication and other task operations remain fixtures.
func testPasswordNativeApply(t *testing.T, call string, targets []string) {
	t.Helper()
	binary, err := exec.LookPath("chezmoi")
	if err != nil {
		t.Skip("native Chezmoi not installed")
	}
	root := t.TempDir()
	source, dest := filepath.Join(root, "source"), filepath.Join(root, "home")
	if err := os.MkdirAll(dest, 0700); err != nil {
		t.Fatal(err)
	}
	names := []string{"private_dot_ssh/private_config", "private_dot_ssh/private_github.pub", "private_dot_ssh/private_homelab.pub", "dot_config/private_1Password/ssh/agent.toml", "dot_config/git/config", "unrelated"}
	for _, name := range names {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture content\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(root, "chezmoi.toml")
	if err := os.WriteFile(config, nil, 0600); err != nil {
		t.Fatal(err)
	}
	args := strings.Fields(call)[1:]
	home, _ := os.UserHomeDir()
	for i, arg := range args {
		if strings.HasPrefix(arg, home+"/") {
			args[i] = filepath.Join(dest, strings.TrimPrefix(arg, home+"/"))
		}
	}
	args = append([]string{"--config", config, "--source", source, "--destination", dest, "--no-tty", "--persistent-state", filepath.Join(root, "state.db")}, args...)
	run := func() ([]byte, error) {
		cmd := exec.CommandContext(t.Context(), binary, args...)
		cmd.Env = append(os.Environ(), "HOME="+dest, "XDG_CONFIG_HOME="+root+"/config", "XDG_DATA_HOME="+root+"/data", "XDG_CACHE_HOME="+root+"/cache", "XDG_STATE_HOME="+root+"/state")
		return cmd.CombinedOutput()
	}
	if out, err := run(); err != nil {
		t.Fatalf("first install failed: %v %s", err, out)
	}
	for _, target := range targets {
		path := filepath.Join(dest, strings.TrimPrefix(target, home+"/"))
		if data, err := os.ReadFile(path); err != nil || string(data) != "fixture content\n" {
			t.Fatalf("missing target: %s: %v", path, err)
		}
	}
	info, err := os.Stat(filepath.Join(dest, ".ssh"))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("managed directory permissions not applied", info, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "unrelated")); !os.IsNotExist(err) {
		t.Fatal("unrelated source was applied", err)
	}
	conflict := filepath.Join(dest, ".ssh/config")
	if err := os.WriteFile(conflict, []byte("local edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := run(); err == nil {
		t.Fatalf("unanswered conflict did not stop apply: %s", out)
	}
	if data, err := os.ReadFile(conflict); err != nil || string(data) != "local edit\n" {
		t.Fatal("local edit was overwritten", err)
	}
}

func TestPasswordDiffFlagsNeverAuthenticateInPreview(t *testing.T) {
	for _, flag := range []string{"--plan", "--mark-done", "--reset", "--help"} {
		t.Run(flag, func(t *testing.T) {
			root, src := postinstallFixture(t)
			cmd, out := postinstallCommand(root, false, "onepassword", "--diff", flag)
			err := cmd.Execute()
			if (err == nil) != (flag == "--help") {
				t.Fatal(err, out)
			}
			if len(src.reads) != 0 || len(src.streams) != 0 {
				t.Fatal("inspected or authenticated before flag validation", src.reads, src.streams)
			}
		})
	}
}
