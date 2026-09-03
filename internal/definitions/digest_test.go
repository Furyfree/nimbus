package definitions

import (
	"os"
	"path/filepath"
	"testing"
)

func digestOf(t *testing.T, root string) string {
	t.Helper()
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return c.Digest()
}

func TestDigestIsStableAndBounded(t *testing.T) {
	root := writeTree(t, baseTree())
	before := digestOf(t, root)
	if before != digestOf(t, root) {
		t.Fatal("digest changed between identical loads")
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("outside the boundary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if before != digestOf(t, root) {
		t.Fatal("files outside the definition boundary changed the digest")
	}
	conf := filepath.Join(root, "system", "root", "etc", "example.conf")
	if err := os.Chmod(conf, 0o640); err != nil {
		t.Fatal(err)
	}
	if before != digestOf(t, root) {
		t.Fatal("a non-executable permission change altered the digest")
	}
	if err := os.Chmod(conf, 0o744); err != nil {
		t.Fatal(err)
	}
	if before == digestOf(t, root) {
		t.Fatal("setting the owner execute bit did not alter the digest")
	}
	if err := os.Chmod(conf, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, []byte("example = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if before == digestOf(t, root) {
		t.Fatal("a content change did not alter the digest")
	}
}

func TestDigestFraming(t *testing.T) {
	a := Digest([]Entry{{Path: "a", Mode: 0o100644, Content: []byte("x")}, {Path: "b", Mode: 0o100644, Content: []byte("y")}})
	b := Digest([]Entry{{Path: "b", Mode: 0o100644, Content: []byte("y")}, {Path: "a", Mode: 0o100644, Content: []byte("x")}})
	if a != b {
		t.Fatal("entry order changed the digest")
	}
	c := Digest([]Entry{{Path: "ab", Mode: 0o100644, Content: []byte("")}, {Path: "", Mode: 0o100644, Content: []byte("xy")}})
	d := Digest([]Entry{{Path: "a", Mode: 0o100644, Content: []byte("b")}, {Path: "", Mode: 0o100644, Content: []byte("xy")}})
	if c == d {
		t.Fatal("framing does not separate path and content")
	}
	if a[:7] != "sha256:" || len(a) != 7+64 {
		t.Fatalf("unexpected digest format %q", a)
	}
}
