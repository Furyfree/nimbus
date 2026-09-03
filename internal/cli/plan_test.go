package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPlanRendersSectionsAndReportsIncomplete(t *testing.T) {
	root := repoRoot(t)
	src := fixtureSource(t, root)
	src.Commands[facts.Key("dnf5", "--cacheonly", "check-upgrade")] = []byte("Repositories loaded.\n")
	withSource(t, src)
	code, out, errOut := run(t, "plan", "--checkout", root, "--machine", "laptop")
	if code != ExitFailure {
		t.Fatalf("an incomplete plan must exit 1, got %d\n%s%s", code, out, errOut)
	}
	for _, want := range []string{"plan for laptop", "apply:", "enable repository docker", "nimbus-docker.repo", "blocked: dnf5 preview failed", "prune candidates (", "updates (apply never installs these", "incomplete:", "sha256:"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "sudo dnf5 -y install --allowerasing") {
		t.Fatal("no installable transaction was recorded, yet argv rendered as runnable")
	}
	code, out, _ = run(t, "plan", "--checkout", root, "--machine", "laptop", "--json")
	if code != ExitFailure {
		t.Fatalf("json exit %d", code)
	}
	var env struct {
		Data struct {
			Machine    string `json:"machine"`
			Complete   bool   `json:"complete"`
			Digest     string `json:"digest"`
			Operations []struct {
				ID      string `json:"id"`
				Blocked string `json:"blocked"`
			} `json:"operations"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Machine != "laptop" || env.Data.Complete || !strings.HasPrefix(env.Data.Digest, "sha256:") || len(env.Data.Operations) == 0 {
		t.Fatalf("envelope = %+v", env.Data)
	}
}

func TestStatusSummarizesThePlan(t *testing.T) {
	root := repoRoot(t)
	withSource(t, fixtureSource(t, root))
	code, out, _ := run(t, "status", "--checkout", root, "--machine", "desktop")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "machine desktop: 6 profiles, 14 components, 286 desired packages") || !strings.Contains(out, "plan incomplete") {
		t.Fatalf("status output:\n%s", out)
	}
	code, out, _ = run(t, "status", "--checkout", root, "--machine", "desktop", "--json")
	if code != ExitOK || !strings.Contains(out, `"repositories_to_enable"`) {
		t.Fatalf("json status: %d\n%s", code, out)
	}
}

func TestMachineOverridesWithoutSelector(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := repoRoot(t)
	withSource(t, fixtureSource(t, root))
	if code, _, errOut := run(t, "status", "--checkout", root); code != ExitFailure || !strings.Contains(errOut, "--machine") {
		t.Fatalf("missing machine without a selector: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "status", "--checkout", root, "--machine", "nope"); code != ExitFailure || !strings.Contains(errOut, "unknown machine") {
		t.Fatalf("unknown machine: %d %q", code, errOut)
	}
}
