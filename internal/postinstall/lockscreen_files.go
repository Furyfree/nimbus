package postinstall

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Furyfree/nimbus/internal/userstate"
)

// Refuse symlinks, shared writable directories and files belonging to others.
// Native state is not a general-purpose Nimbus file target.
func lockscreenParent(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Lockscreen path must be clean and absolute")
	}
	parent := filepath.Dir(path)
	for current := parent; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (st.Uid != uint32(os.Getuid()) && st.Uid != 0) || (info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0) {
			return fmt.Errorf("Unsafe lockscreen directory: %s", current)
		}
		if current == parent && st.Uid != uint32(os.Getuid()) {
			return errors.New("Noctalia directory is not owned by the invoking user")
		}
		if current == string(filepath.Separator) {
			break
		}
	}
	return nil
}

func safeLockscreenRead(path string) ([]byte, error) {
	if err := lockscreenParent(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 || info.Mode().Perm()&0022 != 0 {
		return nil, errors.New("Unsafe Noctalia configuration file ownership, type or permissions")
	}
	data, err := io.ReadAll(io.LimitReader(f, 4*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4*1024*1024 {
		return nil, errors.New("Noctalia settings exceed the repair size limit")
	}
	return data, nil
}

func backupLockscreenSettings(data []byte) (string, error) {
	store, err := userstate.Default()
	if err != nil {
		return "", err
	}
	// Validate ancestors before creating the private backup directory.
	current := store.Dir
	for {
		_, err := os.Lstat(current)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("Invalid backup path")
		}
		current = parent
	}
	if err := lockscreenParent(filepath.Join(current, ".backup-check")); err != nil {
		return "", err
	}
	if err := os.MkdirAll(store.Dir, 0700); err != nil {
		return "", err
	}
	if err := lockscreenParent(filepath.Join(store.Dir, ".backup-check")); err != nil {
		return "", err
	}
	info, err := os.Stat(store.Dir)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0077 != 0 {
		return "", errors.New("Nimbus backup directory must be private (0700)")
	}
	f, err := os.CreateTemp(store.Dir, "noctalia-lockscreen-*.toml.backup")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	dir, err := os.Open(store.Dir)
	if err != nil {
		return name, err
	}
	defer dir.Close()
	return name, dir.Sync()
}

func replaceLockscreenSettings(path string, before, after []byte) error {
	current, err := safeLockscreenRead(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return errors.New("Settings changed before replacement; no replacement performed")
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".nimbus-lockscreen-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(after); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Noctalia is stopped. Detect an external edit immediately before replacing.
	current, err = safeLockscreenRead(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return errors.New("Settings changed before replacement; no replacement performed")
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
