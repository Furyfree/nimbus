package output

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestPalettePreservesTextAndAlignment(t *testing.T) {
	for _, tc := range []struct{ text, colored string }{
		{"  succeeded  Chezmoi apply\n", good + bold + "succeeded"},
		{"  onepassword          Verified            Local configuration checked.\n", good + bold + "Verified"},
		{"  nvidia-mok           Previously verified Current check requires sudo.\n", accent + bold + "Previously verified"},
		{"  nvidia-mok           Unable to check     Unknown enrollment.\n", warn + bold + "Unable to check"},
		{"pending    dnf:demo\n", warn + bold + "pending"},
		{"blocked    dnf:demo\n", bad + bold + "blocked"},
		{"error: repository is unavailable\n", bad + bold + "error"},
		{"unchanged  nothing to do\n", dim + "unchanged"},
		{"Usage:\n", accent + bold + "Usage:"},
		{"Remaining setup:\n", warn + bold + "Remaining setup:"},
		{"Verification problems:\n", bad + bold + "Verification problems:"},
		{"  sync        Make the system match the definitions\n", accent + bold + "sync"},
		{"  noctalia-lockscreen Restore the managed lockscreen layout\n", accent + bold + "noctalia-lockscreen"},
		{"  -h, --help   help for nimbus\n", accent + bold + "-h, --help"},
		{"      --machine string   selected machine\n", accent + bold + "--machine"},
		{"  nimbus postinstall nvidia-mok\n", accent + bold},
		{"    $ sudo dnf5 -y upgrade\n", accent + bold + "$ sudo"},
		{"+enabled = true\n", good + "+enabled"},
		{"-enabled = false\n", bad + "-enabled"},
	} {
		t.Run(strings.TrimSpace(tc.text), func(t *testing.T) {
			var out strings.Builder
			w := ColorWriter(&out, func() bool { return true })
			n, err := io.WriteString(w, tc.text)
			if err != nil || n != len(tc.text) || ansiPattern.ReplaceAllString(out.String(), "") != tc.text || !strings.Contains(out.String(), tc.colored) {
				t.Fatalf("%d %v %q", n, err, out.String())
			}
		})
	}
}

func TestPromptsAreImmediateAndNativeControlsStayIntact(t *testing.T) {
	var out strings.Builder
	w := ColorWriter(&out, func() bool { return true })
	for _, chunk := range []string{"Proceed? [Y/n] ", "\n", "  succeeded", "  one step", "\n", "\x1b[35mNative text\x1b[0m\rprogress\n"} {
		before := out.Len()
		n, err := io.WriteString(w, chunk)
		if err != nil || n != len(chunk) || out.Len() <= before {
			t.Fatal("write was buffered or failed", n, err)
		}
	}
	if !strings.HasSuffix(out.String(), "\x1b[35mNative text\x1b[0m\rprogress\n") {
		t.Fatal("native controls changed", out.String())
	}
	if Native(w) != &out {
		t.Fatal("lost original writer")
	}
}

type brokenWriter struct{ err error }

func (w brokenWriter) Write([]byte) (int, error) { return 0, w.err }

func TestColorWriteFailuresPropagate(t *testing.T) {
	failure := errors.New("closed terminal")
	for _, err := range []error{failure, nil} {
		w := ColorWriter(brokenWriter{err}, func() bool { return true })
		_, got := io.WriteString(w, "error: example\n")
		want := err
		if want == nil {
			want = io.ErrShortWrite
		}
		if !errors.Is(got, want) {
			t.Fatal(got)
		}
	}
}

func TestConcurrentProgress(t *testing.T) {
	var out strings.Builder
	w := ColorWriter(&out, func() bool { return true })
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() { _, _ = io.WriteString(w, "succeeded operation\n") })
	}
	workers.Wait()
	if strings.Count(ansiPattern.ReplaceAllString(out.String(), ""), "succeeded operation\n") != 8 {
		t.Fatal("interleaved writes", out.String())
	}
}

func TestNonTerminalIsPlain(t *testing.T) {
	var out strings.Builder
	if ColorEnabled(&out) {
		t.Fatal("buffer is not a terminal")
	}
	w := ColorWriter(&out, func() bool { return ColorEnabled(&out) })
	_, err := io.WriteString(w, "failed operation\n")
	if err != nil || out.String() != "failed operation\n" {
		t.Fatal(err, out.String())
	}
}

func TestTerminalColorChild(t *testing.T) {
	if os.Getenv("NIMBUS_TEST_COLOR_CHILD") == "" {
		t.Skip("subprocess only")
	}
	w := ColorWriter(os.Stdout, func() bool { return ColorEnabled(os.Stdout) })
	if _, err := io.WriteString(w, "succeeded color-probe\n  - A long setup note whose continuation must stay indented when the terminal is narrow.\n"); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalDetectionAndOptOut(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable")
	}
	python := `
import os,pty,subprocess,sys,fcntl,termios,struct,re
for term,no_color,want,width in [('xterm-256color','',True,40),('xterm-256color','',True,120),('xterm-256color','1',False,40),('dumb','',False,40)]:
    master,slave=pty.openpty()
    fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack("HHHH",24,width,0,0))
    env=os.environ.copy()
    env.update(TERM=term,NO_COLOR=no_color,NIMBUS_TEST_COLOR_CHILD='1')
    child=subprocess.Popen([sys.argv[1],'-test.run=^TestTerminalColorChild$'],stdout=slave,stderr=slave,env=env)
    os.close(slave)
    data=b''
    try:
        while True:
            chunk=os.read(master,65536)
            if not chunk: break
            data+=chunk
    except OSError: pass
    finally: os.close(master)
    assert child.wait(timeout=10)==0,data
    assert (b'\x1b[' in data)==want,(term,no_color,data)
    assert b'color-probe' in data,data
    plain=re.sub(rb'\x1b\[[0-9;]*m',b'',data)
    assert all(len(line)<=width for line in plain.splitlines()),plain
    if width==40: assert b'\r\n    ' in plain,plain
`
	cmd := exec.CommandContext(t.Context(), "python3", "-c", python, os.Args[0])
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestBrightOutcomesAndRemainingViews(t *testing.T) {
	for _, text := range []string{"211 managed and unchanged\n", "37 checks passed.\n", "✓ Plugin files present.\n", "* gaming  <- machine\n", "package dnf:noctalia\n", "  selected by component:hyprland-session\n", "nvidia-mok [Previously verified]: Check enrollment\n", "3. Run software updates:\n", "Checks: 4 passed, 1 failed, 0 unknown.\n"} {
		styled := highlight(text, true)
		if styled == text || !strings.Contains(styled, bold) || ansiPattern.ReplaceAllString(styled, "") != text {
			t.Fatalf("missing emphasis or changed text: %q", styled)
		}
	}
}

func TestTerminalWrappingAndImmediatePrompts(t *testing.T) {
	for _, text := range []string{
		"pass    file:/etc/systemd/user/long-autostart.service: present true, owner root:root, mode 0644, content matches true\n",
		"        A detailed observation that already has indentation and needs to continue on the same column.\n",
	} {
		wrapped := wrapLine(text, 60)
		for _, line := range strings.Split(strings.TrimSuffix(wrapped, "\n"), "\n")[1:] {
			if !strings.HasPrefix(line, "        ") || strings.HasPrefix(line, "         ") {
				t.Fatalf("misaligned continuation: %q", line)
			}
		}
	}

	for _, width := range []int{40, 80, 140} {
		for _, color := range []bool{false, true} {
			var out strings.Builder
			w := ColorWriter(&out, func() bool { return color }).(*colorWriter)
			w.width = func() int { return width }
			text := "  - Review the selected local configuration and then check the application settings before proceeding.\n"
			if _, err := io.WriteString(w, text); err != nil {
				t.Fatal(err)
			}
			plain := ansiPattern.ReplaceAllString(out.String(), "")
			for i, line := range strings.Split(strings.TrimSuffix(plain, "\n"), "\n") {
				if len(line) > width || (i > 0 && !strings.HasPrefix(line, "    ")) {
					t.Fatalf("width %d: %q", width, line)
				}
			}
			if strings.Join(strings.Fields(plain), " ") != strings.Join(strings.Fields(text), " ") {
				t.Fatal("wrapping lost words", plain)
			}
			out.Reset()
			if _, err := io.WriteString(w, "Proceed? [Y/n] "); err != nil || !strings.Contains(out.String(), "Proceed?") {
				t.Fatal("prompt buffered", err)
			}
		}
	}
}
