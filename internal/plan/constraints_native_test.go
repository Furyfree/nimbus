package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
)

// The opt-in native test uses only synthetic RPMs in a disposable container.
func TestNativeDNFConstraints(t *testing.T) {
	c := &definitions.Checkout{
		Machines: map[string]*definitions.Machine{
			"test": {Schema: 1, ID: "test", Profiles: []string{"common"},
				PackageConstraints: map[string]string{"dnf:hyprland": "0.56.*", "dnf:noctalia": "5.*"}},
		},
		Profiles: map[string]*definitions.Profile{
			"common": {Schema: 1, ID: "common", Packages: []string{"hyprland", "noctalia"}},
		},
	}
	r, errs := definitions.Resolve(c, "test")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var policy []byte
	for _, f := range r.Files {
		if f.Target == definitions.VersionlockPath {
			policy = f.Content
		}
	}
	if len(policy) == 0 {
		t.Fatal("no generated constraint policy")
	}
	image := os.Getenv("NIMBUS_DNF_TEST_IMAGE")
	if image == "" {
		t.Skip("set NIMBUS_DNF_TEST_IMAGE to the prepared Fedora image")
	}
	script, err := os.ReadFile(filepath.Join("..", "..", "tools", "dnf-constraints", "verify.py"))
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(map[string]string{"script": string(script), "policy": string(policy), "config": string(DNFDropIn(definitions.Root{}, r.Constraints...))})
	if err != nil {
		t.Fatal(err)
	}
	// Transfer fixtures through stdin; never relabel or expose host files to the
	// container, and do not depend on an image's default entrypoint.
	loader := `import json,sys,pathlib
payload=json.load(sys.stdin)
pathlib.Path('/policy').mkdir()
pathlib.Path('/policy/versionlock.toml').write_text(payload['policy'])
pathlib.Path('/policy/20-nimbus.conf').write_text(payload['config'])
exec(compile(payload['script'],'verify.py','exec'))`
	cmd := exec.CommandContext(t.Context(), "docker", "run", "--rm", "--network", "none", "-i", "--entrypoint", "python3", image, "-c", loader)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native DNF constraints: %v\n%s", err, out)
	}
	t.Log(string(out))
}
