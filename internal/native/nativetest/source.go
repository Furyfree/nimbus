// Package nativetest replays native commands and files without host access.
package nativetest

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// FakeSource replays recorded output. Anything not recorded is an error, so
// a test cannot silently reach the real system.
type FakeSource struct {
	Commands map[string][]byte // key: name and args joined by single spaces
	Failures map[string]string // key as above; value is the error message
	Files    map[string][]byte
	Dirs     map[string][]string
	Paths    map[string]string // command name to resolved path
}

// ErrNotRecorded marks a read the fixture does not cover.
var ErrNotRecorded = errors.New("not recorded in the fixture")

// Key builds the command key used by FakeSource.
func Key(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), " ")
}

// Run replays a recorded command. A recorded failure returns any recorded
// output as well, matching ExecSource.
func (f *FakeSource) Run(name string, args ...string) ([]byte, error) {
	key := Key(name, args...)
	if msg, ok := f.Failures[key]; ok {
		return f.Commands[key], errors.New(msg)
	}
	out, ok := f.Commands[key]
	if !ok {
		return nil, fmt.Errorf("%s: %w", key, ErrNotRecorded)
	}
	return out, nil
}

// Stream replays a recorded command; the fixture has no output to show.
func (f *FakeSource) Stream(_, _ io.Writer, name string, args ...string) error {
	_, err := f.Run(name, args...)
	return err
}

// ReadFile replays a recorded file. A recorded directory reads as EISDIR.
func (f *FakeSource) ReadFile(path string) ([]byte, error) {
	if data, ok := f.Files[path]; ok {
		return data, nil
	}
	if _, ok := f.Dirs[path]; ok {
		return nil, fmt.Errorf("%s: %w", path, syscall.EISDIR)
	}
	return nil, fmt.Errorf("%s: %w", path, os.ErrNotExist)
}

// ReadDir replays a recorded directory listing.
func (f *FakeSource) ReadDir(path string) ([]string, error) {
	names, ok := f.Dirs[path]
	if !ok {
		return nil, fmt.Errorf("%s: %w", path, os.ErrNotExist)
	}
	return names, nil
}

// LookPath replays command presence.
func (f *FakeSource) LookPath(name string) (string, error) {
	p, ok := f.Paths[name]
	if !ok || p == "" {
		return "", fmt.Errorf("%s: %w", name, exec.ErrNotFound)
	}
	return p, nil
}
