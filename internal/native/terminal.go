package native

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Furyfree/nimbus/internal/output"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// StreamLogged attaches a logging pseudo-terminal when interactive output must
// also be logged. The child keeps the invoking session, process group and
// controlling terminal, so terminal signals and sudo's credential cache keep
// working; only stdout and stderr are redirected through the pty. Terminal
// input is never recorded because stdin stays on the real terminal.
func (ExecSource) StreamLogged(stdout, stderr io.Writer, log io.Writer, name string, args ...string) error {
	stdout, stderr = output.Native(stdout), output.Native(stderr)
	outFile, outOK := stdout.(*os.File)
	errFile, errOK := stderr.(*os.File)
	if !term.IsTerminal(int(os.Stdin.Fd())) || !outOK || !errOK || !term.IsTerminal(int(outFile.Fd())) || !term.IsTerminal(int(errFile.Fd())) {
		return (ExecSource{}).Stream(io.MultiWriter(stdout, log), io.MultiWriter(stderr, log), name, args...)
	}
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return err
	}
	defer master.Close()
	// File.Fd disables deadlines, so every ioctl goes through SyscallConn.
	// That keeps the descriptor pollable: a lingering slave holder can then be
	// drained with SetReadDeadline instead of hanging the copy.
	control, err := master.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	if err := control.Control(func(fd uintptr) {
		ioctlErr = unix.IoctlSetPointerInt(int(fd), unix.TIOCSPTLCK, 0)
	}); err != nil {
		return err
	}
	if ioctlErr != nil {
		return ioctlErr
	}
	var number int
	if err := control.Control(func(fd uintptr) {
		number, ioctlErr = unix.IoctlGetInt(int(fd), unix.TIOCGPTN)
	}); err != nil {
		return err
	}
	if ioctlErr != nil {
		return ioctlErr
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return err
	}
	defer slave.Close()
	resize := func() {
		if size, err := unix.IoctlGetWinsize(int(os.Stdin.Fd()), unix.TIOCGWINSZ); err == nil {
			_ = control.Control(func(fd uintptr) {
				_ = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, size)
			})
		}
	}
	resize()
	// Subscribe before the child starts: a window resize arriving right after
	// start must not be lost to a scheduling gap between Start and Notify.
	resizes := make(chan os.Signal, 1)
	signal.Notify(resizes, syscall.SIGWINCH)
	defer signal.Stop(resizes)
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, slave, slave
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = slave.Close()
	done := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() {
		for {
			select {
			case <-done:
				return
			case <-resizes:
				resize()
			}
		}
	})
	outputDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.MultiWriter(stdout, log), master)
		if err != nil && !errors.Is(err, syscall.EIO) && !errors.Is(err, os.ErrClosed) && !errors.Is(err, os.ErrDeadlineExceeded) {
			// The terminal or log writer failed. Keep draining so the child
			// never blocks on a full pty buffer; the log keeps the first error.
			_, _ = io.Copy(io.Discard, master)
		}
		outputDone <- err
	}()
	runErr := cmd.Wait()
	close(done)
	workers.Wait()
	var outputErr error
	select {
	case outputErr = <-outputDone:
	case <-time.After(250 * time.Millisecond):
		// A lingering grandchild can hold the slave open after the direct
		// child exits. Force the pending read to return; close is the
		// fallback when this descriptor has no deadlines.
		if err := master.SetReadDeadline(time.Now()); err != nil {
			_ = master.Close()
		}
		outputErr = <-outputDone
	}
	if errors.Is(outputErr, syscall.EIO) || errors.Is(outputErr, os.ErrClosed) || errors.Is(outputErr, os.ErrDeadlineExceeded) {
		outputErr = nil
	}
	if err := errors.Join(runErr, outputErr); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Activity reports elapsed time while a quiet operation is still running. It
// never captures the command transcript, and returns only after the ticker ends.
func Activity(out io.Writer, label string, run func() error) error {
	done := make(chan error, 1)
	go func() { done <- run() }()
	start := time.Now()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			if _, err := fmt.Fprintf(out, "  %s still running (%s)...\n", label, time.Since(start).Round(time.Second)); err != nil {
				return errorsJoinWait(err, done)
			}
		}
	}
}
func errorsJoinWait(err error, done <-chan error) error {
	runErr := <-done
	if runErr != nil {
		return fmt.Errorf("progress: %w; operation: %w", err, runErr)
	}
	return err
}
