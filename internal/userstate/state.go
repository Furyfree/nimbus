// Package userstate stores private, local setup evidence, never desired state.
package userstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Evidence struct {
	Fingerprint string    `json:"fingerprint,omitempty"`
	Revision    int       `json:"revision"`
	Source      string    `json:"source"`
	At          time.Time `json:"at"`
}
type File struct {
	Schema   int                            `json:"schema"`
	Machines map[string]map[string]Evidence `json:"machines"`
}
type Store struct{ Dir string }

func Default() (Store, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Store{}, err
		}
		base = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(base) {
		return Store{}, errors.New("XDG_STATE_HOME must be absolute")
	}
	return Store{filepath.Join(base, "nimbus")}, nil
}
func validKind(kind string) bool {
	return kind == "setup-notes" || kind == "postinstall" || kind == "channel"
}
func validID(id string) bool { return id != "" && !strings.ContainsAny(id, "\x00\r\n") }
func check(path string, dir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) || info.Mode()&os.ModeSymlink != 0 || info.IsDir() != dir || info.Mode().Perm()&0077 != 0 || (!dir && (!info.Mode().IsRegular() || st.Nlink != 1)) {
		return fmt.Errorf("unsafe local state path: %s", path)
	}
	return nil
}
func ancestors(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("local state path must be clean and absolute")
	}
	current := string(filepath.Separator)
	for part := range strings.SplitSeq(strings.TrimPrefix(path, current), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe local state ancestor: %s", current)
		}
	}
	return nil
}

func (s Store) Read(kind string) (*File, error) {
	if !validKind(kind) {
		return nil, errors.New("invalid local state kind")
	}
	if err := ancestors(s.Dir); err != nil {
		return nil, err
	}
	empty := &File{Schema: 1, Machines: map[string]map[string]Evidence{}}
	if err := check(s.Dir, true); errors.Is(err, os.ErrNotExist) {
		return empty, nil
	} else if err != nil {
		return nil, err
	}
	path := filepath.Join(s.Dir, kind+".json")
	if err := check(path, false); errors.Is(err, os.ErrNotExist) {
		return empty, nil
	} else if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
		return nil, errors.New("unsafe opened local state file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var state File
	if err := dec.Decode(&state); err != nil {
		return nil, fmt.Errorf("read %s (preserved; repair this file): %w", path, err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing local state data")
	}
	if state.Schema != 1 || state.Machines == nil {
		return nil, errors.New("unsupported or invalid local state schema")
	}
	for machine, entries := range state.Machines {
		if !validID(machine) || entries == nil {
			return nil, errors.New("invalid machine state")
		}
		for id, e := range entries {
			if !validID(id) || e.Revision < 1 || e.At.IsZero() || (e.Source != "displayed" && e.Source != "confirmed" && e.Source != "verified") {
				return nil, errors.New("invalid local setup evidence")
			}
		}
	}
	return &state, nil
}
func (f *File) Has(machine, id string, revision int, source string) bool {
	e, ok := f.Machines[machine][id]
	return ok && e.Revision == revision && e.Source == source
}

// Update locks, rereads and atomically replaces just one state file. Readers
// create nothing. Reset removes evidence only, never application configuration.
func (s Store) Update(kind, machine string, changes map[string]Evidence, reset []string) error {
	if !validKind(kind) || !validID(machine) {
		return errors.New("invalid local state key")
	}
	if !filepath.IsAbs(s.Dir) {
		return errors.New("local state directory must be absolute")
	}
	if err := ancestors(s.Dir); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	if err := check(s.Dir, true); err != nil {
		return err
	}
	lockPath := filepath.Join(s.Dir, kind+".lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := check(lockPath, false); err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	f, err := s.Read(kind)
	if err != nil {
		return err
	}
	if f.Machines[machine] == nil {
		f.Machines[machine] = map[string]Evidence{}
	}
	for _, id := range reset {
		delete(f.Machines[machine], id)
	}
	for id, e := range changes {
		if !validID(id) || e.Revision < 1 || (e.Source != "displayed" && e.Source != "confirmed" && e.Source != "verified") {
			return errors.New("invalid evidence")
		}
		if e.At.IsZero() {
			e.At = time.Now().UTC()
		}
		f.Machines[machine][id] = e
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(s.Dir, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err = temp.Write(data); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), filepath.Join(s.Dir, kind+".json")); err != nil {
		return err
	}
	dir, err := os.Open(s.Dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
