package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLockIsExclusiveAndDiagnostic(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	path, err := LockPath()
	if err != nil {
		t.Fatal(err)
	}
	info := LockInfo{Command: "apply", Operation: "op-1", PID: os.Getpid(), Started: time.Now().UTC()}
	l, err := Acquire(path, info)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Dir(path)); st.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode %o", st.Mode().Perm())
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode %o", st.Mode().Perm())
	}
	if _, err := Acquire(path, info); err == nil || !strings.Contains(err.Error(), "op-1") {
		t.Fatalf("second acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	// Stale content never counts as a held lock.
	os.WriteFile(path, []byte(`{"command":"apply","operation":"stale","pid":1}`), 0o600)
	l2, err := Acquire(path, info)
	if err != nil {
		t.Fatalf("stale content blocked the lock: %v", err)
	}
	l2.Release()
}

func TestLockPathNeedsAValidRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := LockPath(); err == nil {
		t.Fatal("missing runtime dir accepted")
	}
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "absent"))
	if _, err := LockPath(); err == nil {
		t.Fatal("absent runtime dir accepted")
	}
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err == nil {
		t.Setenv("XDG_RUNTIME_DIR", link)
		if _, err := LockPath(); err == nil {
			t.Fatal("symlinked runtime dir accepted")
		}
	}
}

func TestLockDoesNotTruncateSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "important")
	if err := os.WriteFile(target, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "operation.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	lock, err := Acquire(link, LockInfo{})
	if lock != nil {
		lock.Release()
	}
	data, _ := os.ReadFile(target)
	if err == nil || string(data) != "keep me" {
		t.Fatalf("symlink accepted or target changed: error=%v contents=%q", err, data)
	}
}

func TestLockRejectsLinkedDirectoriesAndHardlinks(t *testing.T) {
	for _, kind := range []string{"directory symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "important")
			if err := os.WriteFile(target, []byte("keep me"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "lock")
			if kind == "hardlink" {
				if err := os.Link(target, path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink(root, path); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(path, "operation.lock")
			}
			lock, err := Acquire(path, LockInfo{})
			if lock != nil {
				lock.Release()
			}
			if err == nil {
				t.Fatal("linked lock accepted")
			}
			data, _ := os.ReadFile(target)
			if string(data) != "keep me" {
				t.Fatalf("target overwritten: %q", data)
			}
		})
	}
}
