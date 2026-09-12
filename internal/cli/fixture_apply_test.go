package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
)

// withForeignTerra adds Terra's own repository file to the fixture host, which
// Nimbus does not own, so the repository operation blocks and the plan stays
// incomplete.
func withForeignTerra(t *testing.T, src *nativetest.FakeSource) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "inspect", "testdata", "fedora44", "yum.repos.d", "terra.repo"))
	if err != nil {
		t.Fatal(err)
	}
	src.Dirs[inspect.RepoDir] = append(src.Dirs[inspect.RepoDir], "terra.repo")
	src.Files[filepath.Join(inspect.RepoDir, "terra.repo")] = data
}

// readyRepositories writes declared repositories to the fixture host as an
// apply round would leave them, optionally restricted to the supplied IDs.
// The next plan can preview transactions using those prepared sources.
func readyRepositories(t *testing.T, src *nativetest.FakeSource, root string, only ...string) {
	t.Helper()
	c, err := definitions.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for id, r := range c.Definitions().Repositories {
		if len(only) > 0 && !slices.Contains(only, id) {
			continue
		}
		if r.Kind == "flatpak" {
			src.Commands[nativetest.Key("flatpak", "remotes", "--system", "--columns=name,url")] = []byte("flathub\thttps://dl.flathub.org/repo/\n")

			src.Files[filepath.Join(inspect.FlatpakRepoPath, "config")] = []byte("[remote \"" + id + "\"]\ngpg-verify=true\n")
			src.Commands[nativetest.Key("gpg", inspect.KeyInspectArgs(filepath.Join(inspect.FlatpakRepoPath, id+".trustedkeys.gpg"))...)] = []byte("pub:::::::::\nfpr:::::::::" + definitions.NormalizeFingerprint(r.Key) + ":\n")
			continue
		}
		src.Commands[nativetest.Key("gpg", inspect.KeyInspectArgs(plan.KeyPath(id))...)] = []byte("pub:::::::::\nfpr:::::::::" + definitions.NormalizeFingerprint(r.Key) + ":\n")
		b := []byte{}
		if r.Kind == "dnf" && r.ReleasePackage == "" {
			b = fmt.Appendf(b, "[nimbus-%s]\n", id)
			for _, o := range plan.OwnedRepoOptions(id, r) {
				b = fmt.Appendf(b, "%s=%s\n", o.Key, o.Value)
			}
		} else {
			for _, host := range plan.DNFRepoIDs(id, r) {
				b = fmt.Appendf(b, "[%s]\nenabled=1\ngpgcheck=1\npriority=%d\ngpgkey=file://%s\n", host, *r.Priority, plan.KeyPath(id))
			}
		}
		name := "nimbus-" + id + ".repo"
		src.Dirs[inspect.RepoDir] = append(src.Dirs[inspect.RepoDir], name)
		src.Files[filepath.Join(inspect.RepoDir, name)] = b
	}
}

// answerLaptopInstall records a preview answer for the laptop's install
// transaction by asking the planner once which argv it will run.
func answerLaptopInstall(t *testing.T, src *nativetest.FakeSource, root string) {
	t.Helper()
	withSource(t, src)
	_, out, _ := run(t, "sync", "--plan", "--checkout", root, "--machine", "laptop", "--json")
	var env struct {
		Data struct {
			Operations []struct {
				ID    string `json:"id"`
				Steps []struct {
					Argv []string `json:"argv"`
				} `json:"steps"`
			} `json:"operations"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("plan json: %v\n%s", err, out)
	}
	var argv []string
	for _, op := range env.Data.Operations {
		if op.ID == "packages:install" && len(op.Steps) > 0 {
			argv = op.Steps[0].Argv
		}
	}
	if argv == nil {
		t.Fatalf("no install step in plan:\n%s", out)
	}
	var names []string
	skip := false
	for _, a := range argv[3:] { // dnf5 -y install ...
		if skip {
			skip = false
			continue
		}
		if a == "--store" {
			skip = true
			continue
		}
		if !strings.HasPrefix(a, "-") {
			names = append(names, a)
		}
	}
	// Each row comes from the repository its prefix names, as the planner
	// requires.
	c, err := definitions.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, errs := definitions.Resolve(c, "laptop")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	repoOf := map[string]string{}
	for _, p := range resolved.Packages {
		if p.Prefix != definitions.PrefixDNF && p.Prefix != definitions.PrefixFlatpak {
			repoOf[p.Name] = plan.DNFRepoIDs(p.Prefix, c.Definitions().Repositories[p.Prefix])[0]
		}
	}
	args := append([]string{"--assumeno", "--cacheonly", "install", "--allowerasing"}, names...)
	b := []byte("Repositories loaded.\nPackage Arch Version Repository Size\nInstalling:\n")
	for _, n := range names {
		repo := cmp.Or(repoOf[n], "fedora")
		b = fmt.Appendf(b, " %s x86_64 0:1-1.fc44 %s 1.0 KiB\n", n, repo)
	}
	b = append(b, "\nTransaction Summary:\n Installing: 1 package\n\nOperation aborted by the user.\n"...)
	src.Commands[nativetest.Key("dnf5", args...)] = b
	src.Failures[nativetest.Key("dnf5", args...)] = "exit status 1"
}
