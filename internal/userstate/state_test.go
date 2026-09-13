package userstate

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStateIsolationAndReset(t *testing.T) {
	s := Store{filepath.Join(t.TempDir(), "nimbus")}
	f, err := s.Read("postinstall")
	if err != nil || f.Has("desktop", "gui", 1, "confirmed") {
		t.Fatal(f, err)
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatal("read created state")
	}
	var wg sync.WaitGroup
	for _, machine := range []string{"desktop", "laptop"} {
		wg.Go(func() {
			if err := s.Update("postinstall", machine, map[string]Evidence{"gui": {Revision: 1, Source: "confirmed"}}, nil); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := s.Update("postinstall", "desktop", nil, []string{"gui"}); err != nil {
		t.Fatal(err)
	}
	f, err = s.Read("postinstall")
	if err != nil || f.Has("desktop", "gui", 1, "confirmed") || !f.Has("laptop", "gui", 1, "confirmed") || f.Has("laptop", "gui", 2, "confirmed") {
		t.Fatal(f, err)
	}
	notes, err := s.Read("setup-notes")
	if err != nil || len(notes.Machines) != 0 {
		t.Fatal(notes, err)
	}
}
func TestCorruptionAndSymlinksPreserved(t *testing.T) {
	s := Store{t.TempDir()}
	path := filepath.Join(s.Dir, "postinstall.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Update("postinstall", "vm", nil, nil); err == nil {
		t.Fatal("corruption ignored")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "broken" {
		t.Fatal("corrupt file replaced")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read("postinstall"); err == nil {
		t.Fatal("symlink accepted")
	}
}
