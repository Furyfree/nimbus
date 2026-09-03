package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
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

// answerLaptopInstall records a preview answer for the laptop's install
// transaction by asking the planner once which argv it will run.
func answerLaptopInstall(t *testing.T, src *facts.FakeSource, root string) {
	t.Helper()
	withSource(t, src)
	_, out, _ := run(t, "plan", "--checkout", root, "--machine", "laptop", "--json")
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
	args := append([]string{"--assumeno", "--cacheonly", "install", "--allowerasing"}, names...)
	var b strings.Builder
	b.WriteString("Repositories loaded.\nPackage Arch Version Repository Size\nInstalling:\n")
	for _, n := range names {
		fmt.Fprintf(&b, " %s x86_64 0:1-1.fc44 fedora 1.0 KiB\n", n)
	}
	b.WriteString("\nTransaction Summary:\n Installing: 1 package\n\nOperation aborted by the user.\n")
	src.Commands[facts.Key("dnf5", args...)] = []byte(b.String())
	src.Failures[facts.Key("dnf5", args...)] = "exit status 1"
}
