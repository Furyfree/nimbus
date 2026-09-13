package native

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoggedOutputArrivesBeforeExit(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var log bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- (ExecSource{}).StreamLogged(writer, writer, &log, "sh", "-c", "printf started; sleep 1; printf finished")
		writer.Close()
	}()
	first := make([]byte, 7)
	if _, err := io.ReadFull(reader, first); err != nil {
		t.Fatal(err)
	}
	if string(first) != "started" {
		t.Fatal(string(first))
	}
	select {
	case err := <-done:
		t.Fatalf("output arrived after exit: %v", err)
	default:
	}
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not exit")
	}
	if string(rest) != "finished" || !strings.Contains(log.String(), "startedfinished") {
		t.Fatal(string(rest), log.String())
	}
}

func TestLoggedTTYChild(t *testing.T) {
	path := os.Getenv("NIMBUS_TEST_TTY_LOG")
	if path == "" {
		t.Skip("subprocess only")
	}
	log, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	body := `test -t 0 && test -t 1 || exit 91; stty -echo; printf 'READY\n'; read answer; test "$answer" = 'synthetic-private-answer' || exit 92; stty size; printf 'WAITING\n'; sleep 20`
	err = (ExecSource{}).StreamLogged(os.Stdout, os.Stderr, log, "sh", "-c", body)
	if err == nil {
		t.Fatal("interruption did not terminate child")
	}
}
func TestLoggedTTYProgressPrivacyResizeAndCancellation(t *testing.T) {
	for _, tool := range []string{"python3", "stty"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	python := `
import os,pty,select,signal,sys,time,fcntl,termios,struct
master,slave=pty.openpty()
pid=os.fork()
if pid==0:
    os.setsid(); fcntl.ioctl(slave,termios.TIOCSCTTY,0)
    for fd in range(3): os.dup2(slave,fd)
    os.close(master); os.close(slave)
    os.environ['NIMBUS_TEST_TTY_LOG']=sys.argv[2]
    os.execv(sys.argv[1],[sys.argv[1],'-test.run=^TestLoggedTTYChild$'])
os.close(slave)
output=b''
def until(marker):
    global output
    deadline=time.monotonic()+8
    while marker not in output:
        if time.monotonic()>deadline: raise RuntimeError(repr(output))
        if select.select([master],[],[],0.1)[0]: output+=os.read(master,65536)
try:
    until(b'READY')
    fcntl.ioctl(master,termios.TIOCSWINSZ,struct.pack('HHHH',37,111,0,0))
    os.kill(pid,signal.SIGWINCH)
    time.sleep(0.1)
    os.write(master,b'synthetic-private-answer\n')
    until(b'WAITING')
    assert b'37 111' in output,repr(output)
    os.write(master,b'\x03')
    deadline=time.monotonic()+8
    while True:
        ended,status=os.waitpid(pid,os.WNOHANG)
        if ended:
            assert os.waitstatus_to_exitcode(status)==0,(status,output)
            break
        if time.monotonic()>deadline: raise RuntimeError('interrupt did not stop child')
        if select.select([master],[],[],0.1)[0]:
            try: output+=os.read(master,65536)
            except OSError: pass
    log=open(sys.argv[2],'rb').read()
    assert b'READY' in log and b'WAITING' in log,log
    assert b'synthetic-private-answer' not in log,log
finally:
    try: os.kill(pid,signal.SIGKILL)
    except ProcessLookupError: pass
    os.close(master)
`
	cmd := exec.CommandContext(t.Context(), "python3", "-c", python, os.Args[0], filepath.Join(t.TempDir(), "terminal.log"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("TTY relay: %v\n%s", err, output)
	}
}
