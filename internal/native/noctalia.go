package native

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// StopNoctalia uses a pidfd to avoid signalling a reused PID. It never escalates
// or force-kills a shell. Callers must first verify unlocked IPC and approval.
func StopNoctalia(ctx context.Context, pid int, started, binary string) error {
	if pid <= 1 || started == "" || filepath.Base(binary) != "noctalia" {
		return errors.New("Invalid Noctalia process identity")
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return errors.New("Cannot safely open the Noctalia process; nothing stopped")
	}
	defer unix.Close(fd)
	root := fmt.Sprintf("/proc/%d/", pid)
	exe, err := os.Readlink(root + "exe")
	if err != nil || exe != binary {
		return errors.New("Noctalia executable changed; nothing stopped")
	}
	status, err := os.ReadFile(root + "status")
	if err != nil {
		return errors.New("Cannot verify Noctalia process ownership")
	}
	owned := false
	for line := range strings.SplitSeq(string(status), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			owned = len(fields) == 5
			for _, uid := range fields[1:] {
				owned = owned && uid == strconv.Itoa(os.Getuid())
			}
		}
	}
	if !owned {
		return errors.New("Noctalia process is not owned by this user")
	}
	stat, err := os.ReadFile(root + "stat")
	if err != nil {
		return errors.New("Cannot verify Noctalia process identity")
	}
	_, tail, ok := strings.Cut(string(stat), ") ")
	fields := strings.Fields(tail)
	if !ok || len(fields) < 20 || fields[19] != started {
		return errors.New("Noctalia process was replaced; nothing stopped")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		return errors.New("Noctalia could not be stopped gracefully")
	}
	// Once signalled, wait even if the caller is cancelled so it can reliably
	// restart the shell before returning. Never escalate to SIGKILL.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 100)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return errors.New("Noctalia exit could not be confirmed; inspect the desktop session")
		}
		if n > 0 {
			return nil
		}
	}
	return errors.New("Noctalia did not exit within ten seconds; settings untouched. If it exits later, run noctalia --daemon")
}
