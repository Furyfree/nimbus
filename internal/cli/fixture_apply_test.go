package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
)

// withoutTerra drops Terra's own repository file so the fixture host has no
// foreign file and the plan can be complete.
func withoutTerra(src *facts.FakeSource) {
	var kept []string
	for _, name := range src.Dirs[facts.RepoDir] {
		if name != "terra.repo" {
			kept = append(kept, name)
		}
	}
	src.Dirs[facts.RepoDir] = kept
}

// withForeignTerra adds Terra's own repository file to the fixture host, which
// Nimbus does not own, so the repository operation blocks and the plan stays
// incomplete.
func withForeignTerra(t *testing.T, src *facts.FakeSource) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "facts", "testdata", "fedora44", "yum.repos.d", "terra.repo"))
	if err != nil {
		t.Fatal(err)
	}
	src.Dirs[facts.RepoDir] = append(src.Dirs[facts.RepoDir], "terra.repo")
	src.Files[filepath.Join(facts.RepoDir, "terra.repo")] = data
}

// readyRepositories writes every declared DNF and COPR repository to the
// fixture host as an apply round would leave it, so the next plan previews
// its transaction instead of waiting for repository changes.
func readyRepositories(t *testing.T, src *facts.FakeSource, root string) {
	t.Helper()
	c, err := definitions.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for id, r := range c.Definitions().Repositories {
		if r.Kind == "flatpak" {
			continue
		}
		var b strings.Builder
		if r.Kind == "dnf" && r.ReleasePackage == "" {
			fmt.Fprintf(&b, "[nimbus-%s]\n", id)
			for _, o := range plan.OwnedRepoOptions(id, r) {
				fmt.Fprintf(&b, "%s=%s\n", o.Key, o.Value)
			}
		} else {
			for _, host := range plan.DNFRepoIDs(id, r) {
				fmt.Fprintf(&b, "[%s]\nenabled=1\ngpgcheck=1\npriority=%d\n", host, *r.Priority)
			}
		}
		name := "nimbus-" + id + ".repo"
		src.Dirs[facts.RepoDir] = append(src.Dirs[facts.RepoDir], name)
		src.Files[filepath.Join(facts.RepoDir, name)] = []byte(b.String())
	}
	src.Commands[facts.Key("flatpak", "remotes", "--system", "--columns=name,url")] = []byte("flathub\thttps://dl.flathub.org/repo/\n")
}

// answerLaptopInstall records a preview answer for the laptop's install
// transaction by asking the planner once which argv it will run.
func answerLaptopInstall(t *testing.T, src *facts.FakeSource, root string) {
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
		if p.Prefix != definitions.PrefixDNF && p.Prefix != definitions.PrefixFlatpak && p.Prefix != definitions.PrefixCargo {
			repoOf[p.Name] = plan.DNFRepoIDs(p.Prefix, c.Definitions().Repositories[p.Prefix])[0]
		}
	}
	args := append([]string{"--assumeno", "--cacheonly", "install", "--allowerasing"}, names...)
	var b strings.Builder
	b.WriteString("Repositories loaded.\nPackage Arch Version Repository Size\nInstalling:\n")
	for _, n := range names {
		repo := repoOf[n]
		if repo == "" {
			repo = "fedora"
		}
		fmt.Fprintf(&b, " %s x86_64 0:1-1.fc44 %s 1.0 KiB\n", n, repo)
	}
	b.WriteString("\nTransaction Summary:\n Installing: 1 package\n\nOperation aborted by the user.\n")
	src.Commands[facts.Key("dnf5", args...)] = []byte(b.String())
	src.Failures[facts.Key("dnf5", args...)] = "exit status 1"
}
