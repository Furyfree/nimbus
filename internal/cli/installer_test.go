package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/facts"
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
	*facts.FakeSource
	t         *testing.T
	calls     []string
	reads     []string
	failApply bool
}

func (s *installerSource) Run(name string, args ...string) ([]byte, error) {
	s.reads = append(s.reads, facts.Key(name, args...))
	return s.FakeSource.Run(name, args...)
}

func (s *installerSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	key := facts.Key(name, args...)
	s.calls = append(s.calls, key)
	path, err := apply.LockPath()
	if err != nil {
		s.t.Fatal(err)
	}
	lock, err := apply.Acquire(path, apply.LockInfo{})
	if err == nil {
		lock.Release()
		s.t.Fatal("installer released its lock between stages")
	}
	home := os.Getenv("HOME")
	if key == "chezmoi apply" && s.failApply {
		return errors.New("required secret unavailable")
	}
	if name == filepath.Join(home, ".cargo/bin/cargo") {
		if !contains(s.calls, "chezmoi apply") {
			s.t.Fatal("Cargo ran before user configuration was applied")
		}
		s.Commands[facts.Key(name, facts.CargoListArgs...)] = []byte("demo v1.0.0:\n    demo\n")
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
		"profiles/common.toml": "schema = 1\nid = \"common\"\npackages = [\"cargo:demo\"]\ncomponents = []\n",
	}
	for path, data := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	src := fixtureSource(t, root)
	home := os.Getenv("HOME")
	src.Dirs[filepath.Join(home, ".cargo/bin")] = []string{"cargo"}
	src.Commands[facts.Key(filepath.Join(home, ".cargo/bin/cargo"), facts.CargoListArgs...)] = nil
	src.Commands[facts.Key(filepath.Join(home, ".cargo/bin/cargo"), "install", "demo")] = nil
	src.Paths["chezmoi"] = "/usr/bin/chezmoi"
	src.Commands["dnf5 makecache"] = nil
	src.Commands["dnf5 --cacheonly check-upgrade"] = []byte("Repositories loaded.\n")
	src.Commands["sudo dnf5 -y upgrade"] = nil
	src.Commands["chezmoi init --promptString Machine=vm --promptBool ManagedByNimbus=true --promptMultichoice Profiles=common -- https://github.com/Furyfree/dotfiles.git"] = nil
	src.Commands["chezmoi apply"] = nil
	src.Commands["chezmoi source-path"] = []byte(home)
	src.Commands[facts.Key("git", facts.GitArgs(home, "config", "--get", "remote.origin.url")...)] = []byte("https://github.com/Furyfree/dotfiles.git")
	src.Commands[facts.Key("chezmoi", facts.ChezmoiDataArgs...)] = []byte(`{"Machine":"vm","ManagedByNimbus":true,"Profiles":["common"]}`)
	wrapper := &installerSource{FakeSource: src, t: t}
	withSource(t, wrapper)
	return root, wrapper
}

func TestInitAppliesDotfilesBeforeCargoAndRetriesAFailure(t *testing.T) {
	root, src := installerFixture(t)
	src.failApply = true
	code, out, errOut := run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitFailure || !strings.Contains(out, "skipped    remaining Nimbus user tools: dotfiles or tool installation failed") || !strings.Contains(errOut, "required secret unavailable") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	for _, call := range src.calls {
		if strings.Contains(call, "cargo install demo") {
			t.Fatal("Cargo ran after failed apply")
		}
	}
	src.failApply = false
	src.calls = nil
	code, out, errOut = run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitOK || !strings.Contains(out, "succeeded  remaining Nimbus user tools") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	if !contains(src.calls, facts.Key(filepath.Join(os.Getenv("HOME"), ".cargo/bin/cargo"), "install", "demo")) {
		t.Fatal("Cargo was not installed")
	}
}

func TestInitCannotWriteWhileAnotherOperationHoldsTheLock(t *testing.T) {
	root, _ := installerFixture(t)
	path, _ := apply.LockPath()
	lock, err := apply.Acquire(path, apply.LockInfo{Command: "other", PID: os.Getpid()})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	code, _, _ := run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitFailure {
		t.Fatal("concurrent init succeeded")
	}
	path, _ = selector.DefaultPath()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("selector written: %v", err)
	}
}

func TestUnsupportedPlatformDoesNotRefreshMetadataOrWriteSelector(t *testing.T) {
	for _, command := range []string{"init", "sync"} {
		t.Run(command, func(t *testing.T) {
			root, src := installerFixture(t)
			src.Files[facts.OSReleasePath] = []byte("ID=fedora\nVERSION_ID=43\n")
			code, out, errOut := run(t, command, "--checkout", root, "--machine", "vm", "-y")
			if code != ExitFailure || !strings.Contains(out+errOut, "unsupported platform") {
				t.Fatalf("%d %s%s", code, out, errOut)
			}
			if len(src.calls) > 0 || contains(src.reads, "dnf5 makecache") {
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
	delete(src.Dirs, filepath.Join(home, ".cargo/bin"))
	src.Dirs[filepath.Join(home, ".local/bin")] = []string{"mise"}
	// System build prerequisites are already present in this lifecycle fixture.
	checkout, err := loadCheckout(root)
	if err != nil {
		t.Fatal(err)
	}
	query := facts.Key("dnf5", facts.PackageQueryArgs...)
	for _, pkg := range checkout.Components["mise"].Packages {
		src.Commands[query] = append(src.Commands[query], []byte(pkg+"|0|1|1.fc44|x86_64|fedora|User\n")...)
	}

	// Chezmoi propagates an after-script failure even though files were applied.
	src.Failures["chezmoi apply"] = "mise install failed: crate build failed"
	code, out, errOut := run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitFailure || !strings.Contains(out, "failed     dotfiles and tools") || !strings.Contains(out+errOut, "crate build failed") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	delete(src.Failures, "chezmoi apply")
	src.calls = nil
	code, out, errOut = run(t, "init", "--checkout", root, "--machine", "vm", "-y")
	if code != ExitOK || !strings.Contains(out, "succeeded  dotfiles and tools") || strings.Contains(out, "remaining Nimbus user tools") {
		t.Fatalf("%d %s%s", code, out, errOut)
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
				src.Commands[facts.Key("git", facts.GitArgs(home, "config", "--get", "remote.origin.url")...)] = []byte("https://github.com/example/other.git")
			} else {
				want = "stored selection differs"
				src.Commands[facts.Key("chezmoi", facts.ChezmoiDataArgs...)] = []byte(`{"Machine":"other","ManagedByNimbus":true,"Profiles":["common"]}`)
			}
			code, out, errOut := run(t, "init", "--checkout", root, "--machine", "vm", "-y")
			if code != ExitFailure || !strings.Contains(out+errOut, want) {
				t.Fatalf("%d %s%s", code, out, errOut)
			}
			if contains(src.calls, "chezmoi apply") {
				t.Fatal("unrelated source was applied")
			}
		})
	}
}
