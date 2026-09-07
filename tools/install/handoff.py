#!/usr/bin/env python3
"""Linux command supervisor preserving the caller's terminal and stdin."""
import ctypes
import errno
import os
import signal
import subprocess
import sys
import time


def foreground(fd, pgid):
    previous = signal.signal(signal.SIGTTOU, signal.SIG_IGN)
    try:
        os.tcsetpgrp(fd, pgid)
    finally:
        signal.signal(signal.SIGTTOU, previous)


def main(argv):
    if not argv:
        raise SystemExit("handoff: missing command")
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(36, 1, 0, 0, 0) != 0:  # PR_SET_CHILD_SUBREAPER
        raise OSError(ctypes.get_errno(), "cannot supervise command descendants")
    cancelled = 0

    def cancel(sig, frame):
        nonlocal cancelled
        if not cancelled:
            cancelled = sig

    signal.signal(signal.SIGINT, cancel)
    signal.signal(signal.SIGTERM, cancel)
    for sig in (signal.SIGTSTP, signal.SIGTTIN, signal.SIGTTOU):
        signal.signal(sig, signal.SIG_DFL)
    tty = None
    caller_group = os.getpgrp()
    try:
        tty = os.open("/dev/tty", os.O_RDWR | os.O_CLOEXEC)
    except OSError as error:
        if error.errno not in (errno.ENXIO, errno.ENODEV, errno.ENOENT):
            raise
    process = None
    root_status = None
    signalled = False
    setup_failed = False
    exit_status = 1
    waiting_pids = None
    try:
        if cancelled:
            return 128 + cancelled
        process = subprocess.Popen(argv, process_group=0)
        if tty is not None:
            try:
                if os.tcgetpgrp(tty) == caller_group:
                    foreground(tty, process.pid)
                os.killpg(process.pid, signal.SIGCONT)
            except ProcessLookupError:
                pass
            except OSError as error:
                print(f"handoff: cannot hand over terminal: {error}", file=sys.stderr)
                setup_failed = True
        while True:
            failed = root_status is not None and root_status != 0
            if (cancelled or setup_failed or failed) and not signalled:
                print("handoff: stopping command group; waiting for descendants", file=sys.stderr)
                # Async shell descendants can inherit SIGINT ignored. TERM is
                # the cancellation signal; preserve the caller's exit status.
                try:
                    os.killpg(process.pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
                except PermissionError:
                    print("handoff: waiting for privileged descendants to stop", file=sys.stderr)
                signalled = True
            try:
                pid, status = os.waitpid(-1, os.WNOHANG | os.WUNTRACED)
            except ChildProcessError:
                break
            if pid and os.WIFSTOPPED(status):
                try:
                    if pid == process.pid:
                        stopped = os.WSTOPSIG(status)
                        if tty is not None and stopped in (
                                signal.SIGTSTP, signal.SIGTTIN, signal.SIGTTOU):
                            if os.tcgetpgrp(tty) == process.pid:
                                foreground(tty, caller_group)
                            os.killpg(caller_group, stopped)
                            if os.tcgetpgrp(tty) == caller_group:
                                foreground(tty, process.pid)
                        os.killpg(process.pid, signal.SIGCONT)
                    else:
                        os.kill(pid, signal.SIGCONT)
                except ProcessLookupError:
                    pass
                except OSError as error:
                    print(f"handoff: cannot resume command: {error}", file=sys.stderr)
                    setup_failed = True
                continue
            if pid == process.pid:
                root_status = os.waitstatus_to_exitcode(status)
                process.returncode = root_status
                if root_status == 0 and not cancelled and not setup_failed:
                    # A successful native command may leave a deliberate daemon.
                    break
            if pid == 0:
                if signalled:
                    try:
                        with open(f"/proc/self/task/{os.getpid()}/children") as children:
                            pending = children.read().strip()
                        if pending and pending != waiting_pids:
                            print(f"handoff: waiting for process IDs: {pending}", file=sys.stderr)
                            waiting_pids = pending
                    except OSError:
                        pass  # The generic waiting message remains available.
                time.sleep(0.02)
        if cancelled:
            exit_status = 128 + cancelled
        elif not setup_failed:
            exit_status = 128 - root_status if root_status < 0 else root_status
    finally:
        if tty is not None:
            try:
                if process is not None and os.tcgetpgrp(tty) == process.pid:
                    foreground(tty, caller_group)
            except OSError as error:
                print(f"handoff: cannot restore terminal: {error}", file=sys.stderr)
                if exit_status == 0:
                    exit_status = 1
            finally:
                os.close(tty)
    return exit_status


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv[1:]))
    except OSError as error:
        print(f"handoff: {error}", file=sys.stderr)
        sys.exit(1)
