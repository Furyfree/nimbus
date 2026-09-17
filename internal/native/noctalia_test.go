package native

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestNoctaliaStopHelper(t *testing.T) {
	if os.Getenv("NIMBUS_TEST_NOCTALIA_CHILD") != "1" {
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	_, _ = os.Stdout.WriteString("ready\n")
	<-signals
	os.Exit(0)
}

func TestStopNoctaliaIdentityAndGracefulExit(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	binary := filepath.Join(t.TempDir(), "noctalia")
	dest, err := os.OpenFile(binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(dest, source); err != nil {
		t.Fatal(err)
	}
	if err = dest.Close(); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(binary, "-test.run=^TestNoctaliaStopHelper$")
	child.Env = append(os.Environ(), "NIMBUS_TEST_NOCTALIA_CHILD=1")
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatal("helper not ready")
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(child.Process.Pid), "stat"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, _ := strings.Cut(string(data), ") ")
	started := strings.Fields(tail)[19]
	if err = StopNoctalia(t.Context(), child.Process.Pid, "wrong", binary); err == nil {
		t.Fatal("accepted changed identity")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = StopNoctalia(ctx, child.Process.Pid, started, binary); err == nil {
		t.Fatal("ignored cancellation")
	}
	if err = StopNoctalia(t.Context(), child.Process.Pid, started, binary); err != nil {
		t.Fatal(err)
	}
	if err = child.Wait(); err != nil {
		t.Fatalf("did not stop gracefully: %v", err)
	}
}
