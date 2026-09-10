package apply

import (
	"errors"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/plan"
)

func localFile(t *testing.T, content string) inspect.SystemFile {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		t.Fatal(err)
	}
	return inspect.SystemFile{Exists: true, Content: []byte(content), Owner: u.Username, Group: g.Name, Mode: "0644"}
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
	c.After = inspect.SystemFile{}
	if err := ApplySystemFile(root, c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/nimbus/config")); !errors.Is(err, fs.ErrNotExist) {
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
				if err := os.Remove(filepath.Join(root, "etc/managed")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "etc/managed")); err != nil {
					t.Fatal(err)
				}
			case "target-symlink":
				if err := os.Symlink(filepath.Join(t.TempDir(), "victim"), target); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(target, target+"-alias"); err != nil {
					t.Fatal(err)
				}
			case "writable-parent":
				if err := os.Chmod(filepath.Dir(target), 0777); err != nil {
					t.Fatal(err)
				}
			}
			if err := ApplySystemFile(root, c); err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
}
func TestSystemFileRestrictsLegacyRecoveryTargets(t *testing.T) {
	for _, target := range []string{"/home/user/config", "/usr/bin/nimbus", "/etc/../usr/bin/nimbus"} {
		if err := ApplySystemFile(t.TempDir(), plan.FileChange{Target: target, Recovery: true, After: localFile(t, "bad")}); err == nil {
			t.Fatalf("accepted %s", target)
		}
	}
}

func TestSystemFileRemovesLegacyRecoveryFiles(t *testing.T) {
	for _, target := range []string{
		"/usr/local/lib/nimbus/recovery/hyprland.lua",
		"/usr/share/wayland-sessions/nimbus-recovery.desktop",
	} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, target)
			if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
				t.Fatal(err)
			}
			before := localFile(t, "legacy session")
			if err := os.WriteFile(file, before.Content, 0644); err != nil {
				t.Fatal(err)
			}
			change := plan.FileChange{Target: target, Before: before, Recovery: true}
			if err := ApplySystemFile(root, change); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("legacy file remains: %v", err)
			}
			change.Before = inspect.SystemFile{}
			if err := ApplySystemFile(root, change); err != nil {
				t.Fatalf("retry: %v", err)
			}
		})
	}
}
