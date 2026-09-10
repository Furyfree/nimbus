package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/selector"
)

func TestApprovalRequiresAnAnswer(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{{"", false}, {"y", true}, {"  ", false}, {"\n", true}, {"yes\n", true}, {"n\n", false}} {
		if got := approver(strings.NewReader(tc.input), io.Discard, ""); got != tc.want {
			t.Errorf("input %q approved=%t", tc.input, got)
		}
	}
}

func TestSyncReloadsDefinitionsAfterApproval(t *testing.T) {
	applyEnv(t)
	root := editableCheckout(t)
	src := fixtureSource(t, root)
	withSource(t, src)
	saved := approver
	approver = func(io.Reader, io.Writer, string) bool {
		path := manifestPath(root, "laptop")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, []byte("\n# changed during review\n")...), 0644); err != nil {
			t.Fatal(err)
		}
		return true
	}
	t.Cleanup(func() { approver = saved })
	code, out, errOut := run(t, "sync", "-n", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(out, "changed while the question was open") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	if _, err := os.Stat(stateRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state written: %v", err)
	}
}

func TestSelectionRejectsChangedDefinitionsBeforeWriting(t *testing.T) {
	applyEnv(t)
	root := editableCheckout(t)
	src := fixtureSource(t, root)
	withSource(t, src)
	before, err := os.ReadFile(manifestPath(root, "laptop"))
	if err != nil {
		t.Fatal(err)
	}
	saved := approver
	approver = func(io.Reader, io.Writer, string) bool {
		path := filepath.Join(root, "components", "docker.toml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, []byte("\n# changed during review\n")...), 0644); err != nil {
			t.Fatal(err)
		}
		return true
	}
	t.Cleanup(func() { approver = saved })
	code, _, errOut := run(t, "components", "add", "docker", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure || !strings.Contains(errOut, "changed while the plan was being reviewed") {
		t.Fatalf("%d %s", code, errOut)
	}
	after, _ := os.ReadFile(manifestPath(root, "laptop"))
	if string(after) != string(before) {
		t.Fatal("unapproved manifest written")
	}
}

type installerSource struct {
	*nativetest.FakeSource
	t         *testing.T
	calls     []string
	reads     []string
	failApply bool
}

func (s *installerSource) Run(name string, args ...string) ([]byte, error) {
	s.reads = append(s.reads, nativetest.Key(name, args...))
	return s.FakeSource.Run(name, args...)
}

func (s *installerSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	key := nativetest.Key(name, args...)
	s.calls = append(s.calls, key)
	path, err := apply.LockPath()
	if err != nil {
		s.t.Fatal(err)
	}
	lock, err := apply.Acquire(path, apply.LockInfo{})
	if err == nil {
		_ = lock.Release() // Cleanup cannot change the failed lock assertion.
		s.t.Fatal("installer released its lock between stages")
	}
	if key == "chezmoi apply" && s.failApply {
		if _, err := fmt.Fprintln(out, "FAKE-RENDERED-SECRET"); err != nil {
			return err
		}
		return errors.New("required secret unavailable")
	}
	data, err := s.FakeSource.Run(name, args...)
	if _, writeErr := out.Write(data); writeErr != nil {
		return writeErr
	}
	return err
}

func installerFixture(t *testing.T) (string, *installerSource) {
	t.Helper()
	applyEnv(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	for _, dir := range []string{"machines", "profiles", "components", "system", ".git"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"nimbus.toml":          "schema = 1\n[compatibility]\nfedora = [\"44\"]\nmin_engine = \"0.0.0\"\n",
		".git/config":          "[remote \"origin\"]\nurl = https://github.com/Furyfree/nimbus.git\n",
		"machines/vm.toml":     "schema = 1\nid = \"vm\"\nprofiles = [\"common\"]\n[dotfiles]\nrepo = \"https://github.com/Furyfree/dotfiles.git\"\n",
		"profiles/common.toml": "schema = 1\nid = \"common\"\npackages = []\ncomponents = []\n",
	}
	for path, data := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	src := fixtureSource(t, root)
	home := os.Getenv("HOME")
	src.Paths["chezmoi"] = "/usr/bin/chezmoi"
	src.Commands["dnf5 makecache"] = nil
	src.Commands["dnf5 --cacheonly check-upgrade"] = []byte("Repositories loaded.\n")
	src.Commands["sudo dnf5 -y upgrade"] = nil
	src.Commands["chezmoi init --promptString Machine=vm --promptBool ManagedByNimbus=true --promptMultichoice Profiles=common --promptBool Enable 1Password SSH integration=false -- https://github.com/Furyfree/dotfiles.git"] = nil
	src.Commands["chezmoi apply"] = nil
	src.Commands["chezmoi source-path"] = []byte(home)
	src.Commands[nativetest.Key("git", inspect.GitArgs(home, "config", "--get", "remote.origin.url")...)] = []byte("https://github.com/Furyfree/dotfiles.git")
	src.Commands[nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["common"]}`)
	wrapper := &installerSource{FakeSource: src, t: t}
	withSource(t, wrapper)
	return root, wrapper
}

func TestInitKeepsHandoffErrorsPrivateAndRetriesFailure(t *testing.T) {
	root, src := installerFixture(t)
	src.failApply = true
	code, out, errOut := run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitFailure || !strings.Contains(out, "failed     dotfiles and tools") || !strings.Contains(errOut, "required secret unavailable") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	logs, err := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "nimbus", "install", "run-*", "engine.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("missing installation log: %v %v", logs, err)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"required secret unavailable", "FAKE-RENDERED-SECRET"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("Chezmoi secret-capable output entered log: %s", secret)
		}
	}
	for _, detail := range []string{"stage end", "elapsed_ms=", "status=failed"} {
		if !strings.Contains(string(data), detail) {
			t.Fatalf("missing stage diagnostic %s", detail)
		}
	}
	src.failApply = false
	src.calls = nil
	code, out, errOut = run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitOK || !strings.Contains(out, "succeeded  dotfiles and tools") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	if !slices.Contains(src.calls, "chezmoi apply") {
		t.Fatal("Chezmoi was not retried")
	}
}

func TestInitCannotWriteWhileAnotherOperationHoldsTheLock(t *testing.T) {
	root, _ := installerFixture(t)
	path, err := apply.LockPath()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := apply.Acquire(path, apply.LockInfo{Command: "other", PID: os.Getpid()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Error(err)
		}
	})
	code, _, _ := run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitFailure {
		t.Fatal("concurrent init succeeded")
	}
	path, err = selector.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("selector written: %v", err)
	}
}

func TestUnsupportedPlatformDoesNotRefreshMetadataOrWriteSelector(t *testing.T) {
	for _, command := range []string{"init", "sync"} {
		t.Run(command, func(t *testing.T) {
			root, src := installerFixture(t)
			src.Files[inspect.OSReleasePath] = []byte("ID=fedora\nVERSION_ID=43\n")
			code, out, errOut := run(t, command, "--checkout", root, "--machine", "vm", "-y")
			if code != ExitFailure || !strings.Contains(out+errOut, "unsupported platform") {
				t.Fatalf("%d %s%s", code, out, errOut)
			}
			if len(src.calls) > 0 || slices.Contains(src.reads, "dnf5 makecache") {
				t.Fatalf("mutation calls: %v", src.calls)
			}
			path, _ := selector.DefaultPath()
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("selector written: %v", err)
			}
		})
	}
}

func TestInitDelegatesMiseToolsToChezmoiAndRetriesFailure(t *testing.T) {
	root, src := installerFixture(t)
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema = 1\nid = \"common\"\npackages = []\ncomponents = [\"mise\"]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "components/mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "components/mise.toml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	home := os.Getenv("HOME")
	src.Dirs[filepath.Join(home, ".local/bin")] = []string{"mise"}
	// System build prerequisites are already present in this lifecycle fixture.
	checkout, err := loadCheckout(root)
	if err != nil {
		t.Fatal(err)
	}
	query := nativetest.Key("dnf5", inspect.PackageQueryArgs...)
	for _, pkg := range checkout.Components["mise"].Packages {
		src.Commands[query] = append(src.Commands[query], []byte(pkg+"|0|1|1.fc44|x86_64|fedora|User\n")...)
	}

	// Chezmoi propagates an after-script failure even though files were applied.
	src.Commands["chezmoi apply"] = []byte("Setup note: Sign in to 1Password.\nCompiling a crate...\n")
	src.Failures["chezmoi apply"] = "mise install failed: crate build failed"
	code, out, errOut := run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitFailure || !strings.Contains(out, "failed     dotfiles and tools") || !strings.Contains(out+errOut, "crate build failed") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	footer := out[strings.Index(out, "init summary:"):]
	if !strings.Contains(footer, "Setup notes:") || !strings.Contains(footer, "Sign in to 1Password.") || strings.Contains(footer, "Compiling a crate") {
		t.Fatalf("setup instructions lost or compiler output repeated: %s", footer)
	}
	delete(src.Failures, "chezmoi apply")
	src.calls = nil
	code, out, errOut = run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitOK || !strings.Contains(out, "succeeded  dotfiles and tools") || strings.Contains(out, "remaining Nimbus user tools") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	if strings.Count(out, "Sign in to 1Password.") != 2 || strings.LastIndex(out, "Setup notes:") < strings.Index(out, "init summary:") {
		t.Fatalf("successful init did not repeat its setup notes last: %s", out)
	}
	count := 0
	for _, call := range src.calls {
		if call == "chezmoi apply" {
			count++
		}
		if strings.Contains(call, "/mise") || strings.Contains(call, "/cargo") {
			t.Fatalf("Nimbus duplicated Chezmoi's tool installation: %v", src.calls)
		}
	}
	if count != 1 {
		t.Fatalf("Chezmoi ran %d times: %v", count, src.calls)
	}
	// Ordinary sync never applies dotfiles or installs their declared tools.
	src.calls = nil
	code, out, errOut = run(t, "sync", "--checkout", root, "--machine", "vm", "-y", "-n")
	if code != ExitOK || len(src.calls) != 0 {
		t.Fatalf("sync crossed the dotfiles lifecycle: %d %v %s%s", code, src.calls, out, errOut)
	}
}

func TestInvalidNewManifestLeavesNoFileOrSelector(t *testing.T) {
	root, _ := installerFixture(t)
	for _, id := range []string{"a", "b"} {
		data := "schema = 1\nid = \"" + id + "\"\n"
		if id == "a" {
			data += "conflicts = [\"b\"]\n"
		}
		if err := os.WriteFile(filepath.Join(root, "components", id+".toml"), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	saved := pickerFn
	pickerFn = func(title string, _ []pickItem) ([]string, error) {
		if strings.HasPrefix(title, "profiles") {
			return []string{"common"}, nil
		}
		return []string{"a", "b"}, nil
	}
	t.Cleanup(func() { pickerFn = saved })
	code, out, errOut := run(t, "init", "--checkout", root, "--new", "newbox", "--no-dotfiles", "-y")
	if code != ExitFailure || !strings.Contains(out+errOut, "conflict") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	for _, path := range []string{manifestPath(root, "newbox"), filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "nimbus/config.toml")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid initialization wrote %s", path)
		}
	}
}

func TestInitRefusesUnrelatedChezmoiStateBeforeApplying(t *testing.T) {
	for _, which := range []string{"origin", "selection"} {
		t.Run(which, func(t *testing.T) {
			root, src := installerFixture(t)
			home := os.Getenv("HOME")
			src.Dirs[filepath.Join(home, ".local/share/chezmoi")] = []string{".git"}
			want := "source does not match"
			if which == "origin" {
				src.Commands[nativetest.Key("git", inspect.GitArgs(home, "config", "--get", "remote.origin.url")...)] = []byte("https://github.com/example/other.git")
			} else {
				want = "stored selection differs"
				src.Commands[nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)] = []byte(`{"Machine":"other","ManagedByNimbus":true,"Profiles":["common"]}`)
			}
			code, out, errOut := run(t, "init", "--checkout", root, "--machine", "vm", "-y")
			if code != ExitFailure || !strings.Contains(out+errOut, want) {
				t.Fatalf("%d %s%s", code, out, errOut)
			}
			if slices.Contains(src.calls, "chezmoi apply") {
				t.Fatal("unrelated source was applied")
			}
		})
	}
}

func TestInitRejectsIgnoredDotfilesFlags(t *testing.T) {
	root, src := installerFixture(t)
	code, out, errOut := run(t, "init", "--checkout", root, "--machine", "vm", "--no-dotfiles", "-y")
	if code != ExitUsage || len(src.calls) > 0 {
		t.Fatalf("ignored flag: code=%d calls=%v output=%s%s", code, src.calls, out, errOut)
	}
}

func TestInstallerRequiresAnOpenableControllingTerminal(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// The terminal guard must stop the complete entry before host inspection.
	cmd := exec.Command(bash, "-c", string(data))
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + filepath.Dir(bash)}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "a controlling terminal is required") {
		t.Fatalf("installer passed the terminal guard: %v %s", err, output)
	}
}

func TestInitTrustChangeRefusesWithoutPrompt(t *testing.T) {
	for _, field := range []string{"origin", "checkout"} {
		for _, yes := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/yes=%t", field, yes), func(t *testing.T) {
				root, src := installerFixture(t)
				path, _ := selector.DefaultPath()
				old := &selector.Selector{Schema: selector.CurrentSchema, Checkout: root, Machine: "vm", Origin: "github.com/Furyfree/nimbus"}
				if field == "origin" {
					old.Origin = "github.com/previous/nimbus"
				} else {
					old.Checkout = t.TempDir()
				}
				if err := selector.Write(path, old); err != nil {
					t.Fatal(err)
				}
				before, _ := os.ReadFile(path)
				saved := approver
				t.Cleanup(func() { approver = saved })
				approver = func(io.Reader, io.Writer, string) bool {
					t.Fatal("init asked to change selector trust")
					return true
				}
				args := []string{"init", "--checkout", root, "--machine", "vm"}
				if yes {
					args = append(args, "--yes")
				}
				code, out, errOut := run(t, args...)
				after, _ := os.ReadFile(path)
				if code != ExitFailure || string(before) != string(after) || len(src.calls) != 0 || !strings.Contains(out+errOut, "selector trust change refused") || !strings.Contains(out+errOut, "explicitly update the selector") {
					t.Fatalf("trust refusal: code=%d calls=%v output=%s%s", code, src.calls, out, errOut)
				}
			})
		}
	}
}

func TestSyncStillRequiresConfirmation(t *testing.T) {
	root, src := installerFixture(t)
	if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=['dnf5-plugins']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := approver
	t.Cleanup(func() { approver = saved })
	var asked bool
	approver = func(io.Reader, io.Writer, string) bool { asked = true; return false }
	code, out, errOut := run(t, "sync", "--checkout", root, "--machine", "vm")
	if code != ExitFailure || !asked || len(src.calls) != 0 || !strings.Contains(out+errOut, "not applied") {
		t.Fatalf("sync applied without confirmation: %d asked=%t calls=%v output=%s%s", code, asked, src.calls, out, errOut)
	}
}

func TestSyncRefusesMissingCheckoutIdentity(t *testing.T) {
	root, src := installerFixture(t)
	src.Failures[nativetest.Key("git", inspect.GitArgs(root, "rev-parse", "HEAD")...)] = "unreadable Git identity"
	code, out, _ := run(t, "sync", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitFailure || len(src.calls) != 0 || !strings.Contains(out, "inspectable Git clone") {
		t.Fatalf("unidentified sync: %d %v %s", code, src.calls, out)
	}
	if _, err := os.Stat(stateRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state written: %v", err)
	}
}

func TestInitRefreshPreservesEnabledSSH(t *testing.T) {
	root, src := installerFixture(t)
	src.Commands[nativetest.Key("chezmoi", inspect.ChezmoiDataArgs...)] = []byte(`{"Machine":"other","ManagedByNimbus":true,"Profiles":["common"],"onePasswordSsh":true}`)
	code, out, errOut := run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitFailure || !strings.Contains(out+errOut, "'Enable 1Password SSH integration=true'") || slices.Contains(src.calls, "chezmoi apply") {
		t.Fatalf("refresh lost SSH: %d %s%s", code, out, errOut)
	}
}

func TestSelectionRejectsChangedCheckoutIdentityBeforeWriting(t *testing.T) {
	for _, field := range []string{"commit", "origin"} {
		t.Run(field, func(t *testing.T) {
			applyEnv(t)
			root := editableCheckout(t)
			src := fixtureSource(t, root)
			withSource(t, src)
			before, err := os.ReadFile(manifestPath(root, "laptop"))
			if err != nil {
				t.Fatal(err)
			}
			saved := approver
			t.Cleanup(func() { approver = saved })
			approver = func(io.Reader, io.Writer, string) bool {
				if field == "commit" {
					src.Commands[nativetest.Key("git", inspect.GitArgs(root, "rev-parse", "HEAD")...)] = []byte("changed\n")
				} else {
					src.Files[filepath.Join(root, ".git/config")] = []byte("[remote \"origin\"]\nurl=https://example.invalid/other\n")
				}
				return true
			}
			code, out, errOut := run(t, "components", "add", "docker", "--checkout", root, "--machine", "laptop")
			if code != ExitFailure || !strings.Contains(errOut, "checkout identity changed") {
				t.Fatalf("%d %s%s", code, out, errOut)
			}
			after, err := os.ReadFile(manifestPath(root, "laptop"))
			if err != nil || string(after) != string(before) {
				t.Fatalf("manifest changed: %v", err)
			}
		})
	}
}

func TestInstallerPrerequisitesDoNotAsk(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(string(data), "prerequisites=()\n")
	if !ok {
		t.Fatal("missing prerequisite installation flow")
	}
	body, _, ok = strings.Cut(body, "\n# Normalize a Git locator")
	if !ok {
		t.Fatal("missing prerequisite installation boundary")
	}
	for _, tc := range []struct {
		name, gitStatus, want string
		python                bool
	}{
		{"missing Git", "1", "sudo dnf5 -y install git-core", true},
		{"missing Python", "0", "sudo dnf5 -y install python3", false},
		{"missing both", "1", "sudo dnf5 -y install git-core python3", false},
		{"already installed", "0", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			python := filepath.Join(t.TempDir(), "python3")
			if tc.python {
				if err := os.WriteFile(python, []byte("fixture"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			script := "set -eu\nprerequisites=()\nsay() { :; }\n" +
				"command() { return " + tc.gitStatus + "; }\n" +
				"installer_run() { printf '%s\\n' \"$*\"; }\n" +
				strings.ReplaceAll(body, "/usr/bin/python3", python)
			cmd := exec.Command("bash", "-c", script)
			output, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != tc.want {
				t.Fatalf("prerequisite unexpectedly asked or changed: %v %s", err, output)
			}
		})
	}
}

func TestInstallerAcceptsOnlyCheckoutRoots(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(string(data), "# Normalize a Git locator")
	if !ok {
		t.Fatal("missing checkout validation")
	}
	body = "# Normalize a Git locator" + body
	body, _, ok = strings.Cut(body, "\nBOOTSTRAP=")
	if !ok {
		t.Fatal("missing checkout boundary")
	}
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	worktree := filepath.Join(home, "linked")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", repo)
	git("-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	git("-C", repo, "remote", "add", "origin", "https://github.com/Furyfree/nimbus.git")
	git("-C", repo, "worktree", "add", "--detach", worktree)
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path     string
		accepted bool
	}{{repo, true}, {worktree, true}, {nested, false}} {
		cmd := exec.Command("bash", "-c", "set -eu\nsay() { :; }\nfail() { echo \"$*\"; exit 1; }\n"+body)
		cmd.Env = append(os.Environ(), "HOME="+home, "CHECKOUT="+tc.path, "ORIGIN_ID=github.com/furyfree/nimbus")
		out, err := cmd.CombinedOutput()
		if (err == nil) != tc.accepted {
			t.Fatalf("checkout %s: %v %s", tc.path, err, out)
		}
	}
}
