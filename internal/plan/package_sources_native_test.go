package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

func TestNativeDNFPackageSources(t *testing.T) {
	image := os.Getenv("NIMBUS_DNF_SOURCE_TEST_IMAGE")
	if image == "" {
		t.Skip("set NIMBUS_DNF_SOURCE_TEST_IMAGE to the prepared Fedora image")
	}
	b, _ := sourceBuilder()
	b.in.Resolved.Packages = []definitions.ResolvedPackage{sourcePackage("a", "nimbus-source-probe"), sourcePackage("b", "nimbus-source-editor")}
	install, err := b.installSourceArgs(b.in.Resolved.Packages)
	if err != nil {
		t.Fatal(err)
	}
	b.in.Facts.Packages.Value = []inspect.Package{{Name: "nimbus-source-probe", Arch: "noarch", Version: "1", Release: "1", FromRepo: "nimbus-a"}, {Name: "nimbus-source-editor", Arch: "noarch", FromRepo: "nimbus-b"}, {Name: "nimbus-source-dependency", Arch: "noarch"}, {Name: "filesystem", Arch: "x86_64"}}
	upgrade := b.sourceUpgrade().Command
	// Obtain repair argv from the planner using a synthetic preview.
	_, src := sourceBuilder()
	b.in.Source = src
	src.Commands["dnf5 --assumeno --cacheonly distro-sync --from-repo=nimbus-a nimbus-source-probe.noarch"] = previewText([]TxPackage{{Name: "nimbus-source-probe", Arch: "noarch", EVR: "1-1", Repository: "nimbus-a", Section: "downgrading"}})
	op := b.repairPackageSource(b.in.Resolved.Packages[0], b.in.Facts.Packages.Value[0])
	if op.Blocked != "" {
		t.Fatal(op.Blocked)
	}
	repair := op.Steps[0].Argv[2:]
	src.Commands["dnf5 --assumeno --cacheonly distro-sync --from-repo=nimbus-a nimbus-source-probe.noarch"] = []byte("Repositories loaded.\nNothing to do.\n")
	src.Commands["dnf5 --assumeno --cacheonly reinstall --from-repo=nimbus-a nimbus-source-probe.noarch"] = previewText([]TxPackage{{Name: "nimbus-source-probe", Arch: "noarch", EVR: "1-1", Repository: "nimbus-a", Section: "reinstalling"}})
	op = b.repairPackageSource(b.in.Resolved.Packages[0], b.in.Facts.Packages.Value[0])
	if op.Blocked != "" {
		t.Fatal(op.Blocked)
	}
	script, err := os.ReadFile(filepath.Join("..", "..", "tools", "dnf-sources", "verify.py"))
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(map[string]any{"script": string(script), "install": install, "upgrade": upgrade, "repair": repair, "reinstall": op.Steps[0].Argv[2:]})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "docker", "run", "--rm", "--network", "none", "-i", "--entrypoint", "python3", image, "-c", "import json,sys; payload=json.load(sys.stdin); exec(compile(payload['script'], 'verify.py', 'exec'))")
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native source checks: %v\n%s", err, out)
	}
	t.Log(string(out))
}
