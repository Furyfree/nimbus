package plan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
)

// The opt-in native test uses only synthetic RPMs in a disposable container.
func TestNativeDNFConstraints(t *testing.T) {
	image := os.Getenv("NIMBUS_DNF_TEST_IMAGE")
	if image == "" {
		t.Skip("set NIMBUS_DNF_TEST_IMAGE to the prepared Fedora test image")
	}
	c, err := definitions.Load(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	r, errs := definitions.Resolve(c, "vm")
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(r.Constraints) != 2 {
		t.Fatal("expected the selected desktop version families")
	}
	dir := t.TempDir()
	for _, f := range r.Files {
		if f.Target == definitions.VersionlockPath {
			if err := os.WriteFile(filepath.Join(dir, "versionlock.toml"), f.Content, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "20-nimbus.conf"), []byte(DNFDropIn(definitions.Root{}, r.Constraints...)), 0644); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "tools", "dnf-constraints", "verify.py"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "docker", "run", "--rm", "--network", "none", "-v", dir+":/policy:ro", "-v", script+":/verify.py:ro", image, "python3", "/verify.py")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native DNF constraints: %v\n%s", err, out)
	}
	t.Log(string(out))
}
