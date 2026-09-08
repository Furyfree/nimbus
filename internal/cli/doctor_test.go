package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

// fixtureSource replays a healthy Fedora 44 host whose checkout is the
// repository itself.
func fixtureSource(t *testing.T, root string) *facts.FakeSource {
	t.Helper()
	if stateRoot == state.Root {
		saved := stateRoot
		stateRoot = filepath.Join(t.TempDir(), "state")
		t.Cleanup(func() { stateRoot = saved })
	}
	repoDir := filepath.Join("..", "facts", "testdata", "fedora44")
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join(repoDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	src := &facts.FakeSource{
		Commands: map[string][]byte{
			facts.Key("uname", "-m"):                                                                  []byte("x86_64\n"),
			facts.Key("dnf5", facts.PackageQueryArgs...):                                              read("repoquery-installed.txt"),
			facts.Key("flatpak", "remotes", "--system", "--columns=name,url"):                         []byte(""),
			facts.Key("flatpak", "list", "--system", "--app", "--columns=application,version,origin"): []byte(""),
			facts.Key("systemctl", "is-active", "firewalld"):                                          []byte("active\n"),
			facts.Key("git", facts.GitArgs(root, "rev-parse", "HEAD")...):                             []byte("abc123\n"),
			facts.Key("git", facts.GitArgs(root, "status", "--porcelain")...):                         []byte(""),
		},
		Failures: map[string]string{},
		Files: map[string][]byte{
			facts.OSReleasePath:  read("os-release"),
			facts.SecureBootPath: {6, 0, 0, 0, 1},
			facts.SELinuxPath:    []byte("1\n"),
		},
		Dirs:  map[string][]string{facts.RepoDir: {"fedora.repo"}},
		Paths: map[string]string{},
	}
	src.Files[filepath.Join(facts.RepoDir, "fedora.repo")] = read(filepath.Join("yum.repos.d", "fedora.repo"))
	if root != "" {
		src.Dirs[filepath.Join(root, ".git")] = []string{"config"}
		src.Files[filepath.Join(root, ".git", "config")] = []byte("[remote \"origin\"]\n\turl = https://github.com/Furyfree/nimbus.git\n")
		// The DNF drop-in is already as declared, so plans start at the
		// repositories the tests reason about.
		if c, err := definitions.Load(root); err == nil {
			src.Files[facts.DNFDropInPath] = []byte(plan.DNFDropIn(c.Definitions()))
		}
	}
	for _, name := range facts.RequiredCommands {
		src.Paths[name] = "/usr/bin/" + name
	}
	// This baseline predates system-resource ownership. Record native facts
	// explicitly so package/selection tests can reach their intended boundary.
	src.Dirs["/"] = []string{"etc", "usr", "var"}
	src.Dirs["/etc"] = []string{"systemd", "yum.repos.d"}
	src.Dirs["/etc/systemd"] = []string{"system"}
	src.Dirs["/etc/systemd/system"] = []string{}
	src.Dirs["/usr"] = []string{}
	src.Dirs["/var"] = []string{"lib"}
	src.Dirs["/var/lib"] = []string{}
	for _, path := range []string{"/etc", "/etc/systemd", "/etc/systemd/system", "/usr", "/var", "/var/lib"} {
		src.Commands[facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", path)] = []byte("directory|root|root|755|1\n")
	}
	src.Commands[facts.Key("id", "-un")] = []byte("test\n")
	src.Commands[facts.Key("id", "-nG", "--", "test")] = []byte("test wheel\n")
	src.Commands[facts.Key("systemctl", "get-default")] = []byte("multi-user.target\n")
	for _, unit := range []string{"greetd.service", "bluetooth.service", "avahi-daemon.service", "cups.socket", "cups.path", "docker.service", "containerd.service", "tailscaled.service", "power-profiles-daemon.service"} {
		src.Commands[facts.Key("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", unit)] = []byte("LoadState=loaded\nUnitFileState=disabled\nActiveState=inactive\n")
	}
	return src
}

func withSource(t *testing.T, src facts.Source) {
	t.Helper()
	saved := newSource
	newSource = func() facts.Source { return src }
	t.Cleanup(func() { newSource = saved })
}

func TestDoctorOnRepositoryCheckout(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	withSource(t, fixtureSource(t, root))
	code, out, errOut := run(t, "doctor", "--checkout", root)
	if code != ExitOK {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, "pass    platform: fedora 44 on x86_64") || !strings.Contains(out, "0 failed, 0 unknown, 10 checks") {
		t.Fatalf("output:\n%s", out)
	}
	code, out, _ = run(t, "doctor", "--checkout", root, "--json")
	if code != ExitOK {
		t.Fatalf("json exit %d", code)
	}
	var env struct {
		Data struct {
			Checks []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"checks"`
			Failed int `json:"failed"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.Checks) != 10 || env.Data.Failed != 0 || env.Data.Checks[0].ID != "platform" {
		t.Fatalf("envelope = %+v", env.Data)
	}
}

func TestDoctorFailsWithExitOneAndExplains(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	root, _ = filepath.EvalSymlinks(root)
	src := fixtureSource(t, root)
	src.Files[facts.SELinuxPath] = []byte("0\n")
	withSource(t, src)
	code, out, _ := run(t, "doctor", "--checkout", root)
	if code != ExitFailure {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "fail    selinux: SELinux is permissive") || !strings.Contains(out, "fix: set SELINUX=enforcing") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestDoctorWithBrokenCheckoutStillRunsHostChecks(t *testing.T) {
	broken := t.TempDir()
	src := fixtureSource(t, broken)
	// A directory that is neither definitions nor a Git clone.
	delete(src.Dirs, filepath.Join(broken, ".git"))
	delete(src.Files, filepath.Join(broken, ".git", "config"))
	withSource(t, src)
	code, out, _ := run(t, "doctor", "--checkout", broken)
	if code != ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "fail    definitions:") || !strings.Contains(out, "fail    selector: --checkout override in use;") || !strings.Contains(out, "unknown platform:") || !strings.Contains(out, "pass    selinux") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestDoctorWithoutSelectorFailsDefinitions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	src := fixtureSource(t, "")
	withSource(t, src)
	code, out, _ := run(t, "doctor")
	if code != ExitFailure {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "fail    selector:") || !strings.Contains(out, "fail    definitions: no checkout selected") {
		t.Fatalf("output:\n%s", out)
	}
}
