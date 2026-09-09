// Package apply executes a plan: under the operation lock it runs the exact
// native commands the plan holds with their output visible, verifies each
// result, records receipts through the privileged record action, and
// reports what a transaction did beyond its preview. A failed operation
// stops the run and never gets a receipt.
package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// LockInfo is the diagnostic content of the lock file. The kernel lock is
// authoritative; this text is never used to decide whether the lock is held.
type LockInfo struct {
	Command   string    `json:"command"`
	Operation string    `json:"operation"`
	PID       int       `json:"pid"`
	Started   time.Time `json:"started"`
}

// Lock is the held operation lock.
type Lock struct {
	file *os.File
}

// LockPath returns $XDG_RUNTIME_DIR/nimbus/operation.lock. A missing or
// invalid runtime directory blocks mutation.
func LockPath() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set; mutation needs the user's runtime directory")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("runtime directory: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("runtime directory %s must be a directory, not a symlink or file", dir)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return "", fmt.Errorf("runtime directory %s is not owned by this user", dir)
	}
	return filepath.Join(dir, "nimbus", "operation.lock"), nil
}

// Acquire takes the kernel advisory lock without waiting. Another running
// operation is reported with its diagnostic content when readable.
func Acquire(path string, info LockInfo) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dirInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("lock directory must be a directory, not a symlink")
	}
	if stat, ok := dirInfo.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return nil, errors.New("lock directory is not owned by this user")
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	fileInfo, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	stat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !fileInfo.Mode().IsRegular() || !ok || int(stat.Uid) != os.Getuid() || stat.Nlink != 1 {
		_ = f.Close()
		return nil, errors.New("lock must be a regular file owned by this user with one link")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		var other LockInfo
		data, _ := os.ReadFile(path)
		_ = json.Unmarshal(data, &other)
		_ = f.Close()
		if other.PID != 0 {
			return nil, fmt.Errorf("another Nimbus operation is running: %s (%s, pid %d, started %s)", other.Command, other.Operation, other.PID, other.Started.Format(time.RFC3339))
		}
		return nil, errors.New("another Nimbus operation is running")
	}
	// Stale content is replaced only now, after the lock is held.
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, err
	}
	data, _ := json.Marshal(info)
	if _, err := f.WriteAt(append(data, '\n'), 0); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Lock{file: f}, nil
}

// Release drops the lock. The file stays; its content is diagnostic only.
// It closes the descriptor even if unlocking fails.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(unlockErr, closeErr)
}
