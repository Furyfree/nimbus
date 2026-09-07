"""Exercise installer process supervision without installation or privileges."""
import os
from pathlib import Path
import pty
import select
import signal
import subprocess
import sys
import tempfile
import time
import unittest

REPO = Path(__file__).resolve().parents[2]
HELPER = REPO / "tools/install/handoff.py"
WORKER = """
import os, signal, sys, time
from pathlib import Path
root = Path(sys.argv[1])
(root / 'child').write_text(str(os.getpid()))
if sys.argv[2] == 'tty':
    with open('/dev/tty') as tty, open('/dev/tty', 'w') as output:
        print('TTY_READY', file=output, flush=True)
        value = tty.readline()
else:
    value = sys.stdin.readline()
(root / 'input').write_text(value)
child = os.fork()
if child == 0:
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    def stop(sig, frame):
        time.sleep(.35)
        (root / 'grandchild-stopped').write_text('yes')
        sys.exit(0)
    signal.signal(signal.SIGTERM, stop)
    (root / 'grandchild').write_text(str(os.getpid()))
elif sys.argv[2] == 'tty':
    with open('/dev/tty') as tty:
        while True:
            tty.readline()
while True:
    time.sleep(.02)
"""


class HandoffTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="nimbus-handoff-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def command(self, *args):
        return [sys.executable, "-I", "-B", str(HELPER), *args]

    def assert_gone(self, pid):
        with self.assertRaises(ProcessLookupError):
            os.kill(pid, 0)

    def run_fixture(self, nested, mode, sig, terminal_interrupt=False):
        root = self.root / f"{nested}-{mode}-{sig}-{terminal_interrupt}"
        root.mkdir()
        worker = root / "worker.py"
        worker.write_text(WORKER)
        inner = root / "nested.sh"
        inner.write_text('. "$LIBRARY"\ninstaller_session\n'
                         'installer_handoff "$PYTHON" -I -B "$WORKER" "$FIXTURE" "$MODE"\n')
        env = {
            "PATH": os.environ["PATH"], "HOME": str(root),
            "XDG_STATE_HOME": str(root / "state"), "CHECKOUT": str(REPO),
            "LIBRARY": str(REPO / "install.sh"), "PYTHON": sys.executable,
            "WORKER": str(worker), "FIXTURE": str(root), "MODE": mode,
            "INNER": str(inner),
        }
        command = '. "$LIBRARY"\ninstaller_session\ninstaller_handoff '
        command += 'bash "$INNER"' if nested else '"$PYTHON" -I -B "$WORKER" "$FIXTURE" "$MODE"'
        reaped = False
        fd = None
        process = None
        output = b""
        if mode == "tty":
            pid, fd = pty.fork()
            if not pid:
                os.execve("/bin/bash", ["bash", "-c", command], env)
        else:
            process = subprocess.Popen(["bash", "-c", command], env=env,
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                       stderr=subprocess.STDOUT, start_new_session=True)
            pid = process.pid
        try:
            if mode == "tty":
                end = time.monotonic() + 5
                while b"TTY_READY" not in output and time.monotonic() < end:
                    if select.select([fd], [], [], .05)[0]:
                        try:
                            output += os.read(fd, 65536)
                        except OSError:
                            break
                self.assertIn(b"TTY_READY", output)
                os.write(fd, b"profile-from-tty\n")
            else:
                process.stdin.write(b"profile-from-pipe\n")
                process.stdin.close()
                process.stdin = None
            end = time.monotonic() + 5
            while not (root / "grandchild").exists() and time.monotonic() < end:
                time.sleep(.01)
            self.assertTrue((root / "grandchild").exists(), output)
            if terminal_interrupt:
                os.write(fd, b"\x03")
            else:
                os.kill(pid, sig)
            if mode == "tty":
                end = time.monotonic() + 5
                while time.monotonic() < end:
                    got, status = os.waitpid(pid, os.WNOHANG)
                    if got:
                        reaped = True
                        break
                    if select.select([fd], [], [], .02)[0]:
                        try:
                            output += os.read(fd, 65536)
                        except OSError:
                            pass
                self.assertTrue(reaped, ("installer did not stop", output))
                code = os.waitstatus_to_exitcode(status)
            else:
                output = process.communicate(timeout=5)[0]
                reaped = True
                code = process.returncode
            self.assertEqual(code, 128 + sig, output)
            self.assertTrue((root / "grandchild-stopped").exists(), output)
            for name in ("child", "grandchild"):
                self.assert_gone(int((root / name).read_text()))
            finished = list((root / "state/nimbus/install").glob("run-*/.finished"))
            self.assertEqual(len(finished), 1, output)
            self.assertGreaterEqual(finished[0].stat().st_mtime_ns,
                                    (root / "grandchild-stopped").stat().st_mtime_ns)
            self.assertFalse(list((root / "state/nimbus/install").glob("run-*/.active")))
            self.assertEqual((root / "input").read_text(), "profile-from-" + mode + "\n")
        finally:
            if not reaped:
                try:
                    os.kill(pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
                end = time.monotonic() + 2
                while time.monotonic() < end:
                    try:
                        if os.waitpid(pid, os.WNOHANG)[0]:
                            reaped = True
                            break
                    except ChildProcessError:
                        reaped = True
                        break
                    time.sleep(.02)
                if not reaped:
                    os.kill(pid, signal.SIGKILL)
                    os.waitpid(pid, 0)
                # Only fixture PIDs are eligible for forced failure cleanup.
                for name in ("grandchild", "child"):
                    if (root / name).exists():
                        try:
                            os.kill(int((root / name).read_text()), signal.SIGKILL)
                        except ProcessLookupError:
                            pass
            if fd is not None:
                os.close(fd)
            if process is not None:
                if process.stdout is not None:
                    process.stdout.close()
                if process.stdin is not None:
                    process.stdin.close()
                if process.returncode is None:
                    process.wait()

    def test_parent_cancellation_matrix(self):
        for nested in (False, True):
            for mode in ("pipe", "tty"):
                for sig in (signal.SIGINT, signal.SIGTERM):
                    with self.subTest(nested=nested, mode=mode, signal=sig):
                        self.run_fixture(nested, mode, sig)

    def test_terminal_ctrl_c(self):
        for nested in (False, True):
            with self.subTest(nested=nested):
                self.run_fixture(nested, "tty", signal.SIGINT, True)

    def test_pipe_and_exit_code(self):
        result = subprocess.run(self.command(sys.executable, "-c",
            'import sys; assert sys.stdin.readline()=="selected-profile\\n"; sys.exit(37)'),
            input="selected-profile\n", text=True, capture_output=True, start_new_session=True)
        self.assertEqual(result.returncode, 37, result.stderr)

    def test_launch_failure(self):
        result = subprocess.run(self.command("/missing-nimbus-test-command"),
                                capture_output=True, text=True, start_new_session=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("No such file", result.stderr)

    def test_success_preserves_detached_daemon(self):
        pidfile = self.root / "daemon-pid"
        worker = self.root / "daemon.py"
        worker.write_text("""
import os, sys, time
from pathlib import Path
pidfile = Path(sys.argv[1])
if os.fork() == 0:
    os.setsid()
    with open('/dev/null', 'r+b') as null:
        for fd in (0, 1, 2):
            os.dup2(null.fileno(), fd)
    pidfile.write_text(str(os.getpid()))
    while True:
        time.sleep(.02)
while not pidfile.exists():
    time.sleep(.01)
""")
        # A separate subreaper owns the detached fixture after the successful
        # handoff returns, so both pass and failure cleanup can reap it.
        wrapper = """
import ctypes, os, signal, subprocess, sys, time
from pathlib import Path
assert ctypes.CDLL(None).prctl(36, 1, 0, 0, 0) == 0
pidfile = Path(sys.argv[3])
process = subprocess.Popen([sys.executable, '-I', '-B', sys.argv[1],
                            sys.executable, '-I', '-B', sys.argv[2], str(pidfile)])
try:
    assert process.wait(timeout=3) == 0
    daemon = int(pidfile.read_text())
    os.kill(daemon, 0)
    print('DAEMON_PRESERVED', flush=True)
finally:
    if pidfile.exists():
        try:
            os.kill(int(pidfile.read_text()), signal.SIGTERM)
        except ProcessLookupError:
            pass
    try:
        process.wait(timeout=3)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()
    while True:
        try:
            os.waitpid(-1, 0)
        except ChildProcessError:
            break
"""
        result = subprocess.run([sys.executable, "-I", "-B", "-c", wrapper,
                                 str(HELPER), str(worker), str(pidfile)],
                                capture_output=True, text=True,
                                start_new_session=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("DAEMON_PRESERVED", result.stdout)
        self.assert_gone(int(pidfile.read_text()))

    def test_interactive_ctrl_z_and_fg(self):
        for nested in (False, True):
            with self.subTest(nested=nested):
                self.run_interactive_suspend(nested)

    def test_interactive_background_start(self):
        self.run_interactive_suspend(False, initial_background=True)

    def run_interactive_suspend(self, nested, initial_background=False):
        root = self.root / f"interactive-{nested}-{initial_background}"
        root.mkdir()
        worker = root / "worker.py"
        worker.write_text(WORKER)
        inner = root / "inner.sh"
        inner.write_text('. "$LIBRARY"\ninstaller_session\n'
                         'installer_handoff "$PYTHON" -I -B "$WORKER" "$FIXTURE" tty\n')
        outer = root / "outer.sh"
        command = ('bash "$INNER"' if nested else
                   '"$PYTHON" -I -B "$WORKER" "$FIXTURE" tty')
        outer.write_text('printf "%s" "$BASHPID" > "$FIXTURE/outer-pid"\n'
                         '. "$LIBRARY"\ninstaller_session\ninstaller_handoff ' + command + "\n")
        env = {
            "PATH": os.environ["PATH"], "HOME": str(root),
            "XDG_STATE_HOME": str(root / "state"), "CHECKOUT": str(REPO),
            "LIBRARY": str(REPO / "install.sh"), "PYTHON": sys.executable,
            "WORKER": str(worker), "FIXTURE": str(root), "INNER": str(inner),
            "OUTER": str(outer), "PS1": "NIMBUS_TEST_PROMPT> ", "TERM": "dumb",
        }
        pid, fd = pty.fork()
        if not pid:
            os.execve("/bin/bash", ["bash", "--noprofile", "--norc", "-i"], env)
        reaped = False

        def read_until(token):
            output = b""
            end = time.monotonic() + 5
            while token not in output and time.monotonic() < end:
                if select.select([fd], [], [], .05)[0]:
                    try:
                        output += os.read(fd, 65536)
                    except OSError:
                        break
            self.assertIn(token, output)
            return output

        def wait_for_background_stop():
            output = b""
            end = time.monotonic() + 5
            while b"Stopped" not in output and time.monotonic() < end:
                self.assertEqual(os.tcgetpgrp(fd), pid)
                os.write(fd, b"jobs\n")
                output += read_until(b"NIMBUS_TEST_PROMPT> ")
                time.sleep(.02)
            self.assertIn(b"Stopped", output)
            self.assertEqual(os.tcgetpgrp(fd), pid)
            self.assertFalse(list((root / "state/nimbus/install").glob("run-*/.finished")))

        try:
            read_until(b"NIMBUS_TEST_PROMPT> ")
            if initial_background:
                os.write(fd, b'bash "$OUTER" &\n')
                read_until(b"NIMBUS_TEST_PROMPT> ")
                wait_for_background_stop()
                worker_pid = int((root / "child").read_text())
                os.write(fd, b"fg\n")
                end = time.monotonic() + 5
                while os.tcgetpgrp(fd) != worker_pid and time.monotonic() < end:
                    time.sleep(.01)
                self.assertEqual(os.tcgetpgrp(fd), worker_pid)
            else:
                os.write(fd, b'bash "$OUTER"\n')
                read_until(b"TTY_READY")
            os.write(fd, b"profile-from-tty\n")
            end = time.monotonic() + 5
            while not (root / "grandchild").exists() and time.monotonic() < end:
                time.sleep(.01)
            self.assertTrue((root / "grandchild").exists())
            worker_pid = int((root / "child").read_text())
            self.assertEqual(os.tcgetpgrp(fd), worker_pid)
            os.write(fd, b"\x1a")
            output = read_until(b"NIMBUS_TEST_PROMPT> ")
            self.assertIn(b"Stopped", output)
            self.assertEqual(os.tcgetpgrp(fd), pid)
            self.assertFalse(list((root / "state/nimbus/install").glob("run-*/.finished")))
            os.write(fd, b"bg\n")
            read_until(b"NIMBUS_TEST_PROMPT> ")
            wait_for_background_stop()
            os.write(fd, b"fg\n")
            end = time.monotonic() + 5
            while os.tcgetpgrp(fd) != worker_pid and time.monotonic() < end:
                time.sleep(.01)
            self.assertEqual(os.tcgetpgrp(fd), worker_pid)
            os.write(fd, b"\x03")
            read_until(b"NIMBUS_TEST_PROMPT> ")
            self.assertEqual(os.tcgetpgrp(fd), pid)
            os.write(fd, b"printf 'LAST_STATUS=%s\\n' \"$?\"\n")
            output = read_until(b"NIMBUS_TEST_PROMPT> ")
            self.assertIn(b"LAST_STATUS=130\r\n", output)
            self.assertTrue((root / "grandchild-stopped").exists())
            for name in ("child", "grandchild"):
                self.assert_gone(int((root / name).read_text()))
            finished = list((root / "state/nimbus/install").glob("run-*/.finished"))
            self.assertEqual(len(finished), 1)
            self.assertGreaterEqual(finished[0].stat().st_mtime_ns,
                                    (root / "grandchild-stopped").stat().st_mtime_ns)
            os.write(fd, b"exit\n")
            end = time.monotonic() + 5
            while time.monotonic() < end:
                if os.waitpid(pid, os.WNOHANG)[0]:
                    reaped = True
                    break
                time.sleep(.01)
            self.assertTrue(reaped)
        finally:
            if not reaped:
                # Resume stopped fixture groups before their cancellation
                # handlers run; never target the test runner's process group.
                for name in ("child", "outer-pid"):
                    if (root / name).exists():
                        group = int((root / name).read_text())
                        try:
                            os.killpg(group, signal.SIGCONT)
                            os.killpg(group, signal.SIGTERM)
                        except ProcessLookupError:
                            pass
                time.sleep(.5)
                try:
                    os.kill(pid, signal.SIGHUP)
                except ProcessLookupError:
                    pass
                end = time.monotonic() + 2
                while time.monotonic() < end:
                    if os.waitpid(pid, os.WNOHANG)[0]:
                        reaped = True
                        break
                    time.sleep(.01)
                if not reaped:
                    os.kill(pid, signal.SIGKILL)
                    os.waitpid(pid, 0)
            os.close(fd)

    def test_foreground_restored(self):
        pid, fd = pty.fork()
        if not pid:
            env = {**os.environ, "PYTHON": sys.executable, "HELPER": str(HELPER)}
            command = '''set -e
"$PYTHON" -I -B "$HELPER" "$PYTHON" -c 'import os; assert os.tcgetpgrp(0)==os.getpgrp()'
"$PYTHON" -c 'import os; assert os.tcgetpgrp(0)==os.getpgrp(); print("RESTORED",flush=True)'
'''
            os.execve("/bin/bash", ["bash", "-c", command], env)
        output = b""
        try:
            end = time.monotonic() + 5
            while time.monotonic() < end:
                if not select.select([fd], [], [], .05)[0]:
                    continue
                try:
                    chunk = os.read(fd, 65536)
                except OSError:
                    break
                if not chunk:
                    break
                output += chunk
            got, status = os.waitpid(pid, os.WNOHANG)
            if not got:
                os.kill(pid, signal.SIGKILL)
                os.waitpid(pid, 0)
                self.fail(("foreground check did not finish", output))
            self.assertEqual(os.waitstatus_to_exitcode(status), 0, output)
            self.assertIn(b"RESTORED", output)
        finally:
            os.close(fd)

    def test_permission_denied_waits_for_descendants(self):
        pidfile = self.root / "pid"
        worker = ('import os,time,pathlib; p=os.fork(); time.sleep(.25 if p==0 else 0); '
                  f'pathlib.Path({str(pidfile)!r}).write_text(str(os.getpid())) if p==0 else None; '
                  'os._exit(0 if p==0 else 1)')
        wrapper = ('import importlib.util,sys; '
                   f'spec=importlib.util.spec_from_file_location("handoff",{str(HELPER)!r}); '
                   'm=importlib.util.module_from_spec(spec); spec.loader.exec_module(m); '
                   'm.os.killpg=lambda *args: (_ for _ in ()).throw(PermissionError()); '
                   f'sys.exit(m.main([sys.executable,"-c",{worker!r}]))')
        start = time.monotonic()
        result = subprocess.run([sys.executable, "-I", "-B", "-c", wrapper],
                                capture_output=True, text=True, start_new_session=True)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("waiting for privileged descendants", result.stderr)
        self.assertGreaterEqual(time.monotonic() - start, .25)
        self.assert_gone(int(pidfile.read_text()))


if __name__ == "__main__":
    unittest.main(verbosity=2)
