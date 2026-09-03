package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
)

// fixtureSource replays a healthy Fedora 44 host whose checkout is the
// repository itself.
func fixtureSource(t *testing.T, root string) *facts.FakeSource {
	t.Helper()
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
			facts.Key("git", "-C", root, "rev-parse", "HEAD"):                                         []byte("abc123\n"),
			facts.Key("git", "-C", root, "status", "--porcelain"):                                     []byte(""),
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
	for _, name := range facts.RequiredCommands {
		src.Paths[name] = "/usr/bin/" + name
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
	if !strings.Contains(out, "pass    platform: fedora 44 on x86_64") || !strings.Contains(out, "0 failed, 0 unknown, 9 checks") {
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
	if len(env.Data.Checks) != 9 || env.Data.Failed != 0 || env.Data.Checks[0].ID != "platform" {
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
	withSource(t, src)
	code, out, _ := run(t, "doctor", "--checkout", broken)
	if code != ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "fail    definitions:") || !strings.Contains(out, "unknown platform:") || !strings.Contains(out, "pass    selinux") {
		t.Fatalf("output:\n%s", out)
	}
}
