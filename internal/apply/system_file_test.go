package apply

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
)

func localFile(t *testing.T, content string) facts.SystemFile {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		t.Fatal(err)
	}
	return facts.SystemFile{Exists: true, Content: []byte(content), Owner: u.Username, Group: g.Name, Mode: "0644"}
}
func TestAtomicSystemFileInstallRepairRemove(t *testing.T) {
	root := t.TempDir()
	after := localFile(t, "one\n")
	c := plan.FileChange{Target: "/etc/nimbus/config", After: after}
	if err := ApplySystemFile(root, c); err != nil {
		t.Fatal(err)
	}
	c.Before = after
	c.After = localFile(t, "two\n")
	if err := ApplySystemFile(root, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "etc/nimbus/config"))
	if err != nil || string(data) != "two\n" {
		t.Fatalf("file %q %v", data, err)
	}
	if err := ApplySystemFile(root, c); err == nil {
		t.Fatal("stale approved input accepted")
	}
	c.Before = c.After
	c.After = facts.SystemFile{}
	if err := ApplySystemFile(root, c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/nimbus/config")); !os.IsNotExist(err) {
		t.Fatalf("removal: %v", err)
	}
}
func TestSystemFileRejectsSymlinksHardlinksAndForeignParents(t *testing.T) {
	for _, kind := range []string{"parent-symlink", "target-symlink", "hardlink", "writable-parent"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "etc/managed"), 0755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "etc/managed/file")
			c := plan.FileChange{Target: "/etc/managed/file", After: localFile(t, "new")}
			switch kind {
			case "parent-symlink":
				os.Remove(filepath.Join(root, "etc/managed"))
				os.Symlink(t.TempDir(), filepath.Join(root, "etc/managed"))
			case "target-symlink":
				os.Symlink(filepath.Join(t.TempDir(), "victim"), target)
			case "hardlink":
				os.WriteFile(target, []byte("old"), 0644)
				os.Link(target, target+"-alias")
			case "writable-parent":
				os.Chmod(filepath.Dir(target), 0777)
			}
			if err := ApplySystemFile(root, c); err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
}
func TestSystemFileRestrictsRecoveryTargets(t *testing.T) {
	for _, target := range []string{"/home/user/config", "/usr/bin/nimbus", "/etc/../usr/bin/nimbus"} {
		if err := ApplySystemFile(t.TempDir(), plan.FileChange{Target: target, Recovery: true, After: localFile(t, "bad")}); err == nil {
			t.Fatalf("accepted %s", target)
		}
	}
}
