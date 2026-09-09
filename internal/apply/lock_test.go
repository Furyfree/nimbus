package apply

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	if st, err := os.Stat(filepath.Dir(path)); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("directory metadata: %v, %v", st, err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("lock metadata: %v, %v", st, err)
	}
	if _, err := Acquire(path, info); err == nil || !strings.Contains(err.Error(), "op-1") {
		t.Fatalf("second acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	// Stale content never counts as a held lock.
	if err := os.WriteFile(path, []byte(`{"command":"apply","operation":"stale","pid":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	l2, err := Acquire(path, info)
	if err != nil {
		t.Fatalf("stale content blocked the lock: %v", err)
	}
	if err := l2.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestLockReleasePreservesUnlockAndCloseFailures(t *testing.T) {
	lock, err := Acquire(filepath.Join(t.TempDir(), "operation.lock"), LockInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.file.Close(); err != nil {
		t.Fatal(err)
	}
	err = lock.Release()
	if !errors.Is(err, syscall.EBADF) || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("release lost an unlock or close failure: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("repeated release: %v", err)
	}
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
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", link)
	if _, err := LockPath(); err == nil {
		t.Fatal("symlinked runtime dir accepted")
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
		if err := lock.Release(); err != nil {
			t.Fatal(err)
		}
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
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
				if err := lock.Release(); err != nil {
					t.Fatal(err)
				}
			}
			if err == nil {
				t.Fatal("linked lock accepted")
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "keep me" {
				t.Fatalf("target overwritten: %q", data)
			}
		})
	}
}
