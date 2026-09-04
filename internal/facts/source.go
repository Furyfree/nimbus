// Package facts inspects the installed Fedora system through native
// read-only interfaces and produces structured facts for doctor, status,
// planning, and tests. It never writes, never invokes sudo, and never uses
// the network.
package facts

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
)

// Source is every read the inspector performs. The real source runs
// commands and reads files; tests replay recorded Fedora output.
type Source interface {
	// Run executes a command without a shell and returns its stdout. A
	// non-zero exit is an error carrying stderr.
	Run(name string, args ...string) ([]byte, error)
	// Stream executes a command with its output written to stdout and
	// stderr as it happens, for native tools whose progress the user
	// should see. A non-zero exit is an error.
	Stream(stdout, stderr io.Writer, name string, args ...string) error
	ReadFile(path string) ([]byte, error)
	// ReadDir returns the sorted entry names of a directory.
	ReadDir(path string) ([]string, error)
	// LookPath resolves a command name on PATH.
	LookPath(name string) (string, error)
}

// ExecSource reads the live system.
type ExecSource struct{}

// Run executes name with args under a C locale so output is stable. On a
// non-zero exit the captured stdout is still returned with the error, since
// tools such as systemctl report state that way.
func (ExecSource) Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// Stream runs name with args attached to the given writers and to this
// process's stdin, which sudo and interactive prompts need.
func (ExecSource) Stream(stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// ReadFile reads a file.
func (ExecSource) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// ReadDir lists a directory.
func (ExecSource) ReadDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// LookPath resolves a command.
func (ExecSource) LookPath(name string) (string, error) { return exec.LookPath(name) }

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

// ErrIsDirectory is returned by FakeSource.ReadFile for a recorded
// directory, matching the EISDIR the real filesystem reports.
var ErrIsDirectory = errors.New("is a directory")

// IsDirectoryError reports whether a ReadFile error means the path is a
// directory, from either source.
func IsDirectoryError(err error) bool {
	return errors.Is(err, ErrIsDirectory) || errors.Is(err, syscall.EISDIR)
}

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
		return nil, fmt.Errorf("%s: %w", path, ErrIsDirectory)
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
