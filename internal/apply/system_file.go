package apply

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"golang.org/x/sys/unix"
)

// ApplySystemFile performs one reviewed replacement beneath an anchored root.
// The real privileged command passes "/"; tests use a temporary directory.
// Every directory is opened with O_NOFOLLOW, so a rename cannot redirect a
// later lookup through a symlink. Parent directories must not be user-writable.
func ApplySystemFile(root string, change plan.FileChange) (resultErr error) {
	target := change.Target
	allowed := strings.HasPrefix(target, "/etc/") || change.Recovery && definitions.RecoveryTarget(target)
	if path.Clean(target) != target || !allowed {
		return fmt.Errorf("target is outside the allowed system-file boundary")
	}
	if strings.HasPrefix(target, "/etc/") && change.Recovery {
		return fmt.Errorf("recovery integration cannot target /etc")
	}
	allowedOwner := uint32(0)
	if root != "/" {
		allowedOwner = uint32(os.Geteuid())
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(target, "/"), "/")
	for _, part := range parts[:len(parts)-1] {
		created := false
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) && !change.Before.Exists && !change.After.Exists {
			return nil
		}
		if errors.Is(openErr, unix.ENOENT) && !change.Before.Exists && change.After.Exists {
			if err := unix.Mkdirat(fd, part, 0755); err != nil {
				if !errors.Is(err, unix.EEXIST) {
					return err
				}
			} else {
				created = true
			}
			next, openErr = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if openErr != nil {
			return fmt.Errorf("open target directory %s: %w", part, openErr)
		}
		if created {
			if err := unix.Fchmod(next, 0755); err != nil {
				_ = unix.Close(next)
				return err
			}
		}
		var st unix.Stat_t
		if err := unix.Fstat(next, &st); err != nil {
			_ = unix.Close(next)
			return err
		}
		if st.Uid != allowedOwner || st.Mode&0022 != 0 {
			_ = unix.Close(next)
			return fmt.Errorf("target directory %s has unsafe ownership or permissions", part)
		}
		_ = unix.Close(fd)
		fd = next
	}
	base := parts[len(parts)-1]
	before, err := readFileAt(fd, base)
	if err != nil {
		return err
	}
	if !plan.SameFile(before, change.Before) {
		return fmt.Errorf("target changed after approval")
	}
	if !change.After.Exists {
		if !before.Exists {
			return nil
		}
		if err := unix.Unlinkat(fd, base, 0); err != nil {
			return err
		}
		return unix.Fsync(fd)
	}
	uid, err := user.Lookup(change.After.Owner)
	if err != nil {
		return err
	}
	gid, err := user.LookupGroup(change.After.Group)
	if err != nil {
		return err
	}
	owner, err := strconv.Atoi(uid.Uid)
	if err != nil {
		return err
	}
	group, err := strconv.Atoi(gid.Gid)
	if err != nil {
		return err
	}
	mode, err := strconv.ParseUint(change.After.Mode, 8, 32)
	if err != nil || mode > 0777 {
		return fmt.Errorf("invalid file mode")
	}
	var nonce [12]byte
	rand.Read(nonce[:])
	temp := ".nimbus-" + hex.EncodeToString(nonce[:])
	tf, err := unix.Openat(fd, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err := unix.Unlinkat(fd, temp, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove staged system file: %w", err))
		}
	}()
	f := os.NewFile(uintptr(tf), temp)
	if _, err := f.Write(change.After.Content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chown(owner, group); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(os.FileMode(mode)); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Re-read through the same directory descriptor immediately before replace.
	current, err := readFileAt(fd, base)
	if err != nil {
		return err
	}
	if !plan.SameFile(current, before) {
		return fmt.Errorf("target changed during staging")
	}
	if err := unix.Renameat(fd, temp, fd, base); err != nil {
		return err
	}
	return unix.Fsync(fd)
}

func readFileAt(dir int, name string) (facts.SystemFile, error) {
	result := facts.SystemFile{}
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { _ = f.Close() }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return result, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
		return result, fmt.Errorf("target is not a single-link regular file")
	}
	owner, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10))
	if err != nil {
		return result, err
	}
	group, err := user.LookupGroupId(strconv.FormatUint(uint64(st.Gid), 10))
	if err != nil {
		return result, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return result, err
	}
	return facts.SystemFile{Exists: true, Content: data, Owner: owner.Username, Group: group.Name, Mode: fmt.Sprintf("%04o", st.Mode&07777)}, nil
}
