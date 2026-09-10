package inspect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

// fedora44 builds a source that replays the recorded Fedora 44 output.
func fedora44(t *testing.T) *nativetest.FakeSource {
	t.Helper()
	dir := filepath.Join("testdata", "fedora44")
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	entries, err := os.ReadDir(filepath.Join(dir, "yum.repos.d"))
	if err != nil {
		t.Fatal(err)
	}
	src := &nativetest.FakeSource{
		Commands: map[string][]byte{
			nativetest.Key("uname", "-m"):                                                                  []byte("x86_64\n"),
			nativetest.Key("dnf5", PackageQueryArgs...):                                                    read("repoquery-installed.txt"),
			nativetest.Key("flatpak", "remotes", "--system", "--columns=name,url"):                         []byte("flathub\thttps://dl.flathub.org/repo/\n"),
			nativetest.Key("flatpak", "list", "--system", "--app", "--columns=application,version,origin"): []byte("com.spotify.Client\t1.2.60\tflathub\nmd.obsidian.Obsidian\t1.8.10\tflathub\n"),
			nativetest.Key("systemctl", "is-active", "firewalld"):                                          []byte("active\n"),
		},
		Failures: map[string]string{},
		Files: map[string][]byte{
			OSReleasePath:  read("os-release"),
			SecureBootPath: {6, 0, 0, 0, 1},
			SELinuxPath:    []byte("1\n"),
		},
		Dirs:  map[string][]string{RepoDir: {}},
		Paths: map[string]string{},
	}
	for _, e := range entries {
		src.Dirs[RepoDir] = append(src.Dirs[RepoDir], e.Name())
		src.Files[filepath.Join(RepoDir, e.Name())] = read(filepath.Join("yum.repos.d", e.Name()))
	}
	for _, name := range RequiredCommands {
		src.Paths[name] = "/usr/bin/" + name
	}
	return src
}

// gitCheckout records a clone of origin in the source: a .git directory
// with its config, and the Git commands the inspector runs against it.
func gitCheckout(t *testing.T, src *nativetest.FakeSource, dirty bool) string {
	t.Helper()
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	src.Dirs[gitDir] = []string{"config"}
	src.Files[filepath.Join(gitDir, "config")] = []byte("[remote \"origin\"]\n\turl = git@github.com:furyfree-org/nimbus.git\n")
	src.Commands[nativetest.Key("git", GitArgs(root, "rev-parse", "HEAD")...)] = []byte("0123456789abcdef0123456789abcdef01234567\n")
	status := ""
	if dirty {
		status = " M docs/SPEC.md\n"
	}
	src.Commands[nativetest.Key("git", GitArgs(root, "status", "--porcelain")...)] = []byte(status)
	return root
}
