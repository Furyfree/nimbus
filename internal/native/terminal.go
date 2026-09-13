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

// StreamLogged attaches a native pseudo-terminal when interactive output must
// also be logged. Only output is copied; terminal input is never recorded.
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
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		return err
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		return err
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return err
	}
	defer slave.Close()
	resize := func() {
		if size, err := unix.IoctlGetWinsize(int(os.Stdin.Fd()), unix.TIOCGWINSZ); err == nil {
			_ = unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, size)
		}
	}
	resize()
	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(os.Stdin.Fd()), state)
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = slave.Close()
	resizes := make(chan os.Signal, 1)
	signal.Notify(resizes, syscall.SIGWINCH)
	defer signal.Stop(resizes)
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
	workers.Go(func() {
		buffer := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
			}
			fds := []unix.PollFd{{Fd: int32(os.Stdin.Fd()), Events: unix.POLLIN}}
			n, err := unix.Poll(fds, 50)
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if err != nil {
				return
			}
			if n == 0 {
				continue
			}
			if fds[0].Revents&unix.POLLIN == 0 {
				return
			}
			n, err = unix.Read(int(os.Stdin.Fd()), buffer)
			if err != nil || n == 0 {
				return
			}
			if _, err = master.Write(buffer[:n]); err != nil {
				return
			}
		}
	})
	outputDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.MultiWriter(stdout, log), master)
		if err != nil && !errors.Is(err, syscall.EIO) && !errors.Is(err, os.ErrClosed) {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
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
		_ = master.Close()
		outputErr = <-outputDone
	}
	if errors.Is(outputErr, syscall.EIO) || errors.Is(outputErr, os.ErrClosed) {
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
