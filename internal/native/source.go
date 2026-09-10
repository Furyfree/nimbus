// Package native executes native commands and accesses the filesystem.
package native

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Source provides native command execution and filesystem access. Callers
// decide which operations are allowed; inspection uses only read-only calls.
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

// ExecSource runs native commands and accesses the live filesystem.
type ExecSource struct{}

// Run executes name with args under a C locale so output is stable. On a
// non-zero exit the captured stdout is still returned with the error, since
// tools such as systemctl report state that way.
func (ExecSource) Run(name string, args ...string) ([]byte, error) {
	return (ExecSource{}).RunLogged(io.Discard, name, args...)
}

// RunLogged also copies both output streams to a caller-owned diagnostic
// sink. Only the mutating installer opts in, for non-secret native commands.
func (ExecSource) RunLogged(log io.Writer, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = io.MultiWriter(&stdout, log)
	cmd.Stderr = io.MultiWriter(&stderr, log)
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return stdout.Bytes(), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return stdout.Bytes(), fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), msg, err)
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
	return names, nil
}

// LookPath resolves a command.
func (ExecSource) LookPath(name string) (string, error) { return exec.LookPath(name) }
