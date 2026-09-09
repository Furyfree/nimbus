package cli

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/version"
)

// Installation logs are opt-in at the command boundary: inspection never
// creates one. Shell bootstrap and direct init share the same private layout.
type installLog struct {
	mu      sync.Mutex
	file    *os.File
	dir     string
	owned   bool
	started time.Time
	err     error
}

func openInstallLog() (*installLog, error) {
	dir := os.Getenv("NIMBUS_INSTALL_LOG_DIR")
	owned := dir == ""
	if owned {
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			base = filepath.Join(home, ".local", "state")
		}
		base = filepath.Join(base, "nimbus", "install")
		if err := makeLogParents(base); err != nil {
			return nil, err
		}
		if err := privateLogPath(base, true); err != nil {
			return nil, err
		}
		if err := pruneInstallLogs(base, 20); err != nil {
			return nil, err
		}
		var err error
		dir, err = os.MkdirTemp(base, "run-"+time.Now().UTC().Format("20060102T150405Z")+"-")
		if err != nil {
			return nil, err
		}
		for name, content := range map[string]string{".nimbus-install": "1\n", ".active": fmt.Sprintln(os.Getpid())} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
				return nil, err
			}
		}
	} else {
		if err := validateLogParents(dir); err != nil {
			return nil, err
		}
		if err := privateLogPath(dir, true); err != nil {
			return nil, err
		}
		marker := filepath.Join(dir, ".nimbus-install")
		if err := privateLogPath(marker, false); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(marker)
		if err != nil || string(data) != "1\n" {
			return nil, errors.New("invalid installation log marker")
		}
		if err := privateLogPath(filepath.Join(dir, ".active"), false); err != nil {
			return nil, fmt.Errorf("installation log is not active: %w", err)
		}
		if _, err := os.Lstat(filepath.Join(dir, ".finished")); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("installation log is already finished")
		}
	}
	path := filepath.Join(dir, "engine.log")
	if _, err := os.Lstat(path); err == nil {
		if err := privateLogPath(path, false); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	i, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 || !i.Mode().IsRegular() || i.Mode().Perm() != 0600 {
		f.Close()
		return nil, errors.New("unsafe opened installation log file")
	}
	l := &installLog{file: f, dir: dir, owned: owned, started: time.Now()}
	l.event("init started engine=%s", version.Engine)
	return l, nil
}

func privateLogPath(path string, directory bool) error {
	i, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	if !ok || st.Uid != uint32(os.Getuid()) || i.Mode().Perm() != mode || i.IsDir() != directory || (!directory && (!i.Mode().IsRegular() || st.Nlink != 1)) {
		return fmt.Errorf("unsafe installation log path: %s", path)
	}
	return nil
}

func logParents(path string, create bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("installation log path must be absolute and clean")
	}
	current := string(filepath.Separator)
	for part := range strings.SplitSeq(strings.TrimPrefix(path, current), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		i, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && create {
			if err := os.Mkdir(current, 0700); err != nil {
				return err
			}
			i, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe installation log ancestor: %s", current)
		}
	}
	return nil
}

func makeLogParents(path string) error     { return logParents(path, true) }
func validateLogParents(path string) error { return logParents(path, false) }

func (l *installLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return 0, l.err
	}
	n, err := l.file.Write(p)
	if err != nil {
		l.err = err
	}
	return n, err
}

func (l *installLog) event(format string, args ...any) {
	fmt.Fprintf(l, "[%s] %s\n", time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf(format, args...))
}

func (l *installLog) finish(runErr error) error {
	status := "succeeded"
	if runErr != nil {
		status = "failed"
	}
	l.event("init finished status=%s elapsed=%s", status, time.Since(l.started).Round(time.Millisecond))
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.file.Sync(); err != nil && l.err == nil {
		l.err = err
	}
	if err := l.file.Close(); err != nil && l.err == nil {
		l.err = err
	}
	if l.owned {
		if l.err != nil {
			status = "failed"
		}
		marker, err := os.OpenFile(filepath.Join(l.dir, ".finished"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_, err = fmt.Fprintln(marker, status)
			err = errors.Join(err, marker.Close())
		}
		if err != nil && l.err == nil {
			l.err = err
		}
		if err == nil {
			err = os.Remove(filepath.Join(l.dir, ".active"))
			if err == nil {
				err = pruneInstallLogs(filepath.Dir(l.dir), 20)
			}
			if err != nil && l.err == nil {
				l.err = err
			}
		}
	}
	return l.err
}

// Unknown files and active runs make a directory ineligible for retirement.
// Never recursively delete a directory: an unexpected artifact must survive.
func pruneInstallLogs(base string, keep int) error {
	entries, err := os.ReadDir(base)
	if err != nil {
		return err
	}
	safe := func(dir string, children []os.DirEntry) bool {
		return !slices.ContainsFunc(children, func(child os.DirEntry) bool {
			return !slices.Contains([]string{".nimbus-install", ".finished", "bootstrap.log", "engine.log", "mise.log"}, child.Name()) || privateLogPath(filepath.Join(dir, child.Name()), false) != nil
		})
	}
	var candidates []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "run-") {
			continue
		}
		dir := filepath.Join(base, entry.Name())
		if privateLogPath(dir, true) != nil {
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, ".active")); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if privateLogPath(filepath.Join(dir, ".finished"), false) != nil || privateLogPath(filepath.Join(dir, ".nimbus-install"), false) != nil {
			continue
		}
		marker, err := os.ReadFile(filepath.Join(dir, ".nimbus-install"))
		if err != nil || string(marker) != "1\n" {
			continue
		}
		children, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if safe(dir, children) {
			candidates = append(candidates, dir)
		}
	}
	slices.Sort(candidates)
	for _, dir := range candidates[:max(0, len(candidates)-keep)] {
		children, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if !safe(dir, children) {
			continue
		}
		for _, child := range children {
			if err := os.Remove(filepath.Join(dir, child.Name())); err != nil {
				return err
			}
		}
		if err := os.Remove(dir); err != nil {
			return err
		}
	}
	return nil
}

type installWriter struct {
	terminal io.Writer
	log      *installLog
}

func (w installWriter) Write(p []byte) (int, error) {
	n, err := w.terminal.Write(p)
	if _, logErr := w.log.Write(p[:n]); err == nil {
		err = logErr
	}
	return n, err
}
func unlogged(w io.Writer) io.Writer {
	if w, ok := w.(installWriter); ok {
		return w.terminal
	}
	return w
}

type installSource struct {
	facts.Source
	log      *installLog
	terminal io.Writer
}

// Only package-manager/identity commands have transcript-safe output.
// Chezmoi, arbitrary maker scripts, shell commands and credential tools are
// metadata-only; their output may include rendered secrets.
func publicInstallCommand(name string, args []string) bool {
	if name == "sudo" {
		if len(args) == 0 {
			return false
		}
		return publicInstallCommand(args[0], args[1:])
	}
	if name == "systemctl" && len(args) > 0 {
		if slices.Contains([]string{"enable", "disable", "start", "stop", "try-restart", "daemon-reload", "get-default", "set-default"}, args[0]) {
			return true
		}
		// Other show properties and status output can contain service secrets.
		return len(args) >= 2 && args[0] == "show" && args[1] == "--property=LoadState,UnitFileState,ActiveState"
	}
	if name == "systemd-tmpfiles" {
		return slices.Equal(args, []string{"--create", "/etc/tmpfiles.d/nimbus-noctalia-greeter.conf"})
	}
	return slices.Contains([]string{"dnf5", "rpm", "rpmkeys", "gpg", "flatpak"}, name)
}

func (s installSource) Run(name string, args ...string) ([]byte, error) {
	start := time.Now()
	label := filepath.Base(name)
	if publicInstallCommand(name, args) {
		label = fmt.Sprintf("%s %q", name, args)
	}
	s.log.event("command start %s (inspection)", label)
	var output []byte
	var err error
	if exec, ok := s.Source.(facts.ExecSource); ok && publicInstallCommand(name, args) {
		output, err = exec.RunLogged(s.log, name, args...)
	} else {
		output, err = s.Source.Run(name, args...)
		if publicInstallCommand(name, args) {
			s.log.Write(output)
		}
	}
	s.log.commandEnd(label, start, err)
	probe := name == "sudo" && slices.Equal(args, []string{"-n", "-v"}) ||
		name == "systemctl" && len(args) > 0 && args[0] == "is-active"
	return output, s.commandError(name, args, err, !probe)
}

func (s installSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	start := time.Now()
	label := filepath.Base(name)
	if publicInstallCommand(name, args) {
		label = fmt.Sprintf("%s %q", name, args)
	}
	s.log.event("command start %s", label)
	out, errOut = unlogged(out), unlogged(errOut)
	if publicInstallCommand(name, args) {
		out, errOut = installWriter{out, s.log}, installWriter{errOut, s.log}
	}
	err := s.Source.Stream(out, errOut, name, args...)
	s.log.commandEnd(label, start, err)
	return s.commandError(name, args, err, true)
}

func (l *installLog) commandEnd(label string, start time.Time, err error) {
	status := "0"
	if err != nil {
		status = "unavailable"
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			status = fmt.Sprint(exit.ExitCode())
		}
	}
	l.event("command end %s exit=%s elapsed=%s", label, status, time.Since(start).Round(time.Millisecond))
}

func (l *installLog) checkoutIdentity(src facts.Source, name, root, origin string) {
	output, err := src.Run("git", facts.GitArgs(root, "rev-parse", "HEAD")...)
	commit := strings.TrimSpace(string(output))
	_, invalid := hex.DecodeString(commit)
	if err != nil || invalid != nil || (len(commit) != 40 && len(commit) != 64) {
		l.event("%s source origin=%s commit=unavailable", name, origin)
		return
	}
	l.event("%s source origin=%s commit=%s", name, origin, commit)
}

// Secret-capable commands keep their native diagnostics on the terminal. Their
// error text must not later leak through the logged installation summary.
type privateInstallError struct {
	name  string
	cause error
	shown bool
}

func (e privateInstallError) Error() string {
	if !e.shown {
		return e.name + " failed (output not logged)"
	}
	return e.name + " failed; see terminal diagnostics (not logged)"
}
func (e privateInstallError) Unwrap() error { return e.cause }
func (s installSource) commandError(name string, args []string, err error, showDiagnostic bool) error {
	if err != nil && !publicInstallCommand(name, args) {
		if s.terminal != nil && showDiagnostic {
			fmt.Fprintln(s.terminal, err)
		}
		err = privateInstallError{filepath.Base(name), err, showDiagnostic}
	}
	s.log.mu.Lock()
	logErr := s.log.err
	s.log.mu.Unlock()
	if logErr != nil {
		return errors.Join(err, fmt.Errorf("installation log: %w", logErr))
	}
	return err
}
