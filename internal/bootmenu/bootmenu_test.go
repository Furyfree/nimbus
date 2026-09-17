package bootmenu

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func mirrorFixture(t *testing.T, entries []string, saved string) (*nativetest.FakeSource, string, string, string) {
	t.Helper()
	root := t.TempDir()
	entriesDir := filepath.Join(root, "entries")
	mirrorDir := filepath.Join(root, "entries-nimbus")
	grubenv := filepath.Join(root, "grubenv")
	if err := os.MkdirAll(entriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range entries {
		if err := os.WriteFile(filepath.Join(entriesDir, name+".conf"), []byte("title "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}, Files: map[string][]byte{}, Dirs: map[string][]string{}, Paths: map[string]string{}}
	src.Dirs[entriesDir] = nil
	for _, name := range entries {
		src.Dirs[entriesDir] = append(src.Dirs[entriesDir], name+".conf")
	}
	if saved != "" {
		src.Files[grubenv] = []byte("# GRUB Environment Block\nsaved_entry=" + saved + "\n")
	}
	return src, entriesDir, grubenv, mirrorDir
}

func TestMirror(t *testing.T) {
	t.Run("mirrors the saved entry and drops stale copies", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-7.2.5-200.fc44.x86_64", "m-6.19.10-300.fc44.x86_64"}, "m-7.2.5-200.fc44.x86_64")
		if err := os.MkdirAll(mirrorDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mirrorDir, "m-6.19.10-300.fc44.x86_64.conf"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mirrorDir, "notes.txt"), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		id, err := Mirror(src, entriesDir, grubenv, mirrorDir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "m-7.2.5-200.fc44.x86_64" {
			t.Fatalf("got %q", id)
		}
		got, err := os.ReadDir(mirrorDir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, entry := range got {
			names = append(names, entry.Name())
		}
		if !slices.Contains(names, id+".conf") || slices.Contains(names, "m-6.19.10-300.fc44.x86_64.conf") || !slices.Contains(names, "notes.txt") {
			t.Fatalf("mirror contents: %v", names)
		}
	})
	t.Run("a stale saved entry falls back to the newest", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-7.2.5-200.fc44.x86_64", "m-6.19.10-300.fc44.x86_64", "m-0-rescue-abc"}, "m-9.9.9-removed.fc44.x86_64")
		id, err := Mirror(src, entriesDir, grubenv, mirrorDir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "m-7.2.5-200.fc44.x86_64" {
			t.Fatalf("got %q", id)
		}
	})
	t.Run("without a grubenv the newest wins", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, []string{"m-6.19.10-300.fc44.x86_64", "m-7.2.5-200.fc44.x86_64"}, "")
		id, err := Mirror(src, entriesDir, grubenv, mirrorDir)
		if err != nil || id != "m-7.2.5-200.fc44.x86_64" {
			t.Fatalf("got %q, %v", id, err)
		}
	})
	t.Run("no entries blocks", func(t *testing.T) {
		src, entriesDir, grubenv, mirrorDir := mirrorFixture(t, nil, "")
		if _, err := Mirror(src, entriesDir, grubenv, mirrorDir); err == nil {
			t.Fatal("expected an error without BLS entries")
		}
	})
}

func TestCompareVersions(t *testing.T) {
	ordered := []string{"m-0-rescue-abc", "m-6.19.10-300.fc44.x86_64", "m-7.2.5-200.fc44.x86_64", "m-7.10.1-1.fc44.x86_64"}
	for i := range len(ordered) - 1 {
		if compareVersions(ordered[i], ordered[i+1]) >= 0 {
			t.Fatalf("%q should sort before %q", ordered[i], ordered[i+1])
		}
		if compareVersions(ordered[i+1], ordered[i]) <= 0 {
			t.Fatalf("%q should sort after %q", ordered[i+1], ordered[i])
		}
	}
	if compareVersions(ordered[1], ordered[1]) != 0 {
		t.Fatal("equal versions must compare equal")
	}
}

// The engine payload is a contract: the marker gate, the internal call, the
// blscfg filters and the submenu label are what the plan and SPEC promise.
func TestPreviousKernelsPayloadContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "system", "root", "etc", "grub.d", "09_nimbus_previous_kernels"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		Marker,
		`rm -f "$mirror"/*.conf`,
		"/usr/bin/nimbus internal boot-menu",
		"blscfg $entry",
		"blscfg non-default",
		"submenu 'Previous kernels'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("payload lacks %q", want)
		}
	}
}
