package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func testInstallLog(t *testing.T) *installLog {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("NIMBUS_INSTALL_LOG_DIR", "")
	l, err := openInstallLog()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.file.Close() })
	return l
}

func TestInstallLogPrivateLifecycleAndRetention(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("NIMBUS_INSTALL_LOG_DIR", "")
	var first, last string
	for i := range 23 {
		l, err := openInstallLog()
		if err != nil {
			t.Fatal(err)
		}
		// Deterministic names exercise chronological retirement without sleeps.
		renamed := filepath.Join(filepath.Dir(l.dir), fmt.Sprintf("run-%03d", i))
		if err := os.Rename(l.dir, renamed); err != nil {
			t.Fatal(err)
		}
		l.dir = renamed
		if i == 0 {
			first = renamed
		}
		last = renamed
		if privateLogPath(l.dir, true) != nil || privateLogPath(filepath.Join(l.dir, "engine.log"), false) != nil {
			t.Fatal("log is not private")
		}
		l.event("test installation detail")
		if err := l.finish(errors.New("installation failed")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("oldest completed run retained")
	}
	entries, err := os.ReadDir(filepath.Dir(last))
	if err != nil || len(entries) != 20 {
		t.Fatalf("retained=%d err=%v", len(entries), err)
	}
	data, _ := os.ReadFile(filepath.Join(last, "engine.log"))
	if !strings.Contains(string(data), "status=failed") || !strings.Contains(string(data), "elapsed=") {
		t.Fatalf("missing closing diagnostic: %s", data)
	}
	active, err := openInstallLog()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = active.file.Close() })
	entries, err = os.ReadDir(filepath.Dir(active.dir))
	if err != nil || len(entries) != 21 {
		t.Fatalf("active run reduced completed retention: entries=%d err=%v", len(entries), err)
	}
	if err := active.finish(nil); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(filepath.Dir(active.dir))
	if err != nil || len(entries) != 20 {
		t.Fatalf("completed retention=%d err=%v", len(entries), err)
	}
}

func TestInstallLogRetirementPreservesActiveAndForeignFiles(t *testing.T) {
	l := testInstallLog(t)
	base := filepath.Dir(l.dir)
	if err := pruneInstallLogs(base, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.dir); err != nil {
		t.Fatal("active run removed")
	}
	if err := l.finish(nil); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(l.dir, "keep-me")
	if err := os.WriteFile(foreign, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := pruneInstallLogs(base, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("foreign file removed")
	}
}

func TestInstallLogRejectsUnsafePaths(t *testing.T) {
	for _, attack := range []string{"directory-symlink", "ancestor-symlink", "file-symlink", "hardlink", "permissions", "marker"} {
		t.Run(attack, func(t *testing.T) {
			l := testInstallLog(t)
			if err := l.finish(nil); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(l.dir, ".finished")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(l.dir, ".active"), []byte("123\n"), 0600); err != nil {
				t.Fatal(err)
			}
			dir := l.dir
			target := filepath.Join(t.TempDir(), "private")
			if err := os.WriteFile(target, []byte("PRESERVE"), 0600); err != nil {
				t.Fatal(err)
			}
			switch attack {
			case "directory-symlink":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				dir = link
			case "ancestor-symlink":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(filepath.Dir(dir), link); err != nil {
					t.Fatal(err)
				}
				dir = filepath.Join(link, filepath.Base(dir))
			case "file-symlink", "hardlink":
				path := filepath.Join(dir, "engine.log")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				link := os.Symlink
				if attack == "hardlink" {
					link = os.Link
				}
				if err := link(target, path); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(dir, 0755); err != nil {
					t.Fatal(err)
				}
			case "marker":
				if err := os.WriteFile(filepath.Join(dir, ".nimbus-install"), []byte("foreign"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("NIMBUS_INSTALL_LOG_DIR", dir)
			if got, err := openInstallLog(); err == nil {
				_ = got.finish(nil) // Cleanup cannot change the failed safety assertion.
				t.Fatal("unsafe log accepted")
			}
			data, _ := os.ReadFile(target)
			if string(data) != "PRESERVE" {
				t.Fatal("unrelated file changed")
			}
		})
	}
}

func TestInstallLogReusesOnlyActiveRuns(t *testing.T) {
	l := testInstallLog(t)
	t.Setenv("NIMBUS_INSTALL_LOG_DIR", l.dir)
	child, err := openInstallLog()
	if err != nil {
		t.Fatal(err)
	}
	if child.owned || child.dir != l.dir {
		t.Fatal("child replaced the outer log")
	}
	if err := child.finish(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(l.dir, ".active")); err != nil {
		t.Fatal("child closed the outer run")
	}
	if err := l.finish(nil); err != nil {
		t.Fatal(err)
	}
	if child, err := openInstallLog(); err == nil {
		_ = child.finish(nil) // Cleanup cannot change the failed lifecycle assertion.
		t.Fatal("finished run reused")
	}
}

func TestInstallLogCompletionRefusesSymlink(t *testing.T) {
	l := testInstallLog(t)
	target := filepath.Join(t.TempDir(), "preserve")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(l.dir, ".finished")); err != nil {
		t.Fatal(err)
	}
	if err := l.finish(nil); err == nil {
		t.Fatal("unsafe completion accepted")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "unchanged" {
		t.Fatal("completion damaged unrelated file")
	}
	if _, err := os.Stat(filepath.Join(l.dir, ".active")); err != nil {
		t.Fatal("invalid completion made run eligible for deletion")
	}
}

type transcriptSource struct{ *nativetest.FakeSource }

func (s transcriptSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	if _, err := io.WriteString(out, "NATIVE-STDOUT\n"); err != nil {
		return err
	}
	_, err := io.WriteString(errOut, "NATIVE-STDERR\n")
	return err
}

func TestInstallLogExcludesSecretsAndPreservesNativeDiagnostics(t *testing.T) {
	l := testInstallLog(t)
	var terminal strings.Builder
	src := installSource{transcriptSource{&nativetest.FakeSource{Commands: map[string][]byte{
		"chezmoi data": []byte("FAKE-TOKEN"), "dnf5 makecache": []byte("repository metadata refreshed\n"),
	}, Failures: map[string]string{"chezmoi data": "FAKE-SECRET-ERROR"}}}, l, &terminal}
	if _, err := src.Run("dnf5", "makecache"); err != nil {
		t.Fatal(err)
	}
	_, err := src.Run("chezmoi", "data")
	if err == nil {
		t.Fatal("failed secret command succeeded")
	}
	if _, err := fmt.Fprintln(installWriter{&terminal, l}, err); err != nil {
		t.Fatal(err)
	}
	if err := src.Stream(installWriter{&terminal, l}, installWriter{&terminal, l}, "chezmoi", "apply"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(l.dir, "engine.log"))
	for _, secret := range []string{"FAKE-TOKEN", "FAKE-SECRET-ERROR", "NATIVE-STDOUT", "NATIVE-STDERR"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("secret-capable output leaked: %s", secret)
		}
	}
	if !strings.Contains(terminal.String(), "FAKE-SECRET-ERROR") || !strings.Contains(terminal.String(), "NATIVE-STDOUT") {
		t.Fatal("native diagnostics disappeared")
	}
	if err := src.Stream(installWriter{&terminal, l}, installWriter{&terminal, l}, "sudo", "dnf5", "-y", "install", "demo"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(l.dir, "engine.log"))
	for _, want := range []string{"repository metadata refreshed", "NATIVE-STDOUT", "NATIVE-STDERR", "command end", "elapsed="} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing safe detail: %s", want)
		}
	}
}

func TestInstallLogWriteFailureIsReported(t *testing.T) {
	l := testInstallLog(t)
	if err := l.file.Close(); err != nil {
		t.Fatal(err)
	}
	s := installSource{&nativetest.FakeSource{Commands: map[string][]byte{"dnf5 makecache": []byte("output")}}, l, io.Discard}
	if _, err := s.Run("dnf5", "makecache"); err == nil {
		t.Fatal("log failure hidden")
	}
	if err := l.finish(nil); err == nil {
		t.Fatal("closing log failure hidden")
	}
}

func TestInstallLogPrivateDiagnosticsPreserveOutputFailure(t *testing.T) {
	for _, mode := range []string{"shown", "unavailable", "failed"} {
		t.Run(mode, func(t *testing.T) {
			l := testInstallLog(t)
			const nativeSecret = "FAKE-NATIVE-SECRET"
			writeErr := errors.New("FAKE-WRITER-SECRET")
			var terminal strings.Builder
			var diagnostic io.Writer = &terminal
			switch mode {
			case "unavailable":
				diagnostic = nil
			case "failed":
				diagnostic = resultErrorWriter{match: nativeSecret, err: writeErr}
			}
			src := installSource{&nativetest.FakeSource{Failures: map[string]string{"chezmoi data": nativeSecret}}, l, diagnostic}
			_, err := src.Run("chezmoi", "data")
			if err == nil {
				t.Fatal("private native command failure disappeared")
			}
			if mode == "failed" && !errors.Is(err, writeErr) {
				t.Errorf("terminal write cause was lost: %v", err)
			}
			if mode == "shown" {
				if !strings.Contains(terminal.String(), nativeSecret) || !strings.Contains(err.Error(), "see terminal diagnostics") {
					t.Errorf("displayed diagnostic was not reported accurately: %q, %v", terminal.String(), err)
				}
			} else if !strings.Contains(err.Error(), "output not logged") || strings.Contains(err.Error(), "see terminal diagnostics") {
				t.Errorf("unavailable diagnostic was reported as shown: %v", err)
			}
			if _, writeErr := fmt.Fprintln(installWriter{&terminal, l}, err); writeErr != nil {
				t.Fatal(writeErr)
			}
			if finishErr := l.finish(err); finishErr != nil {
				t.Fatal(finishErr)
			}
			data, readErr := os.ReadFile(filepath.Join(l.dir, "engine.log"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, secret := range []string{nativeSecret, writeErr.Error()} {
				if strings.Contains(err.Error(), secret) || strings.Contains(string(data), secret) {
					t.Errorf("secret-capable diagnostics entered logged error text: %q", secret)
				}
			}
		})
	}
}

func TestReadOnlyCommandsDoNotCreateInstallationLogs(t *testing.T) {
	root, _ := installerFixture(t)
	base := os.Getenv("XDG_STATE_HOME")
	for _, args := range [][]string{{"validate"}, {"status"}, {"doctor"}, {"sync", "--plan"}} {
		args = append(args, "--checkout", root)
		if args[0] != "validate" {
			args = append(args, "--machine", "vm")
		}
		if code, out, errOut := run(t, args...); code == ExitUsage {
			t.Fatalf("inspection arguments were rejected: %v\n%s%s", args, out, errOut)
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatalf("inspection wrote installation state: %v %v", entries, err)
	}
}

func TestInstallLogDoesNotPrintExpectedInspectionFailures(t *testing.T) {
	l := testInstallLog(t)
	var terminal strings.Builder
	src := installSource{&nativetest.FakeSource{Commands: map[string][]byte{"systemctl is-active firewalld": []byte("inactive")}, Failures: map[string]string{"systemctl is-active firewalld": "exit status 3", "sudo -n -v": "password required"}}, l, &terminal}
	if _, err := src.Run("systemctl", "is-active", "firewalld"); err == nil {
		t.Fatal("probe failure lost")
	}
	if _, err := src.Run("sudo", "-n", "-v"); err == nil {
		t.Fatal("sudo cache miss lost")
	}
	if terminal.Len() != 0 {
		t.Fatalf("inspection made terminal noise: %s", terminal.String())
	}
}

func TestInstallLogKeepsRecorderAndKeyExtractionFailuresVisible(t *testing.T) {
	for _, command := range [][]string{{"sudo", "/usr/bin/nimbus", "internal", "record", "--stage", "/tmp/fixture"}, {"rpm2archive", "/tmp/fixture.rpm"}} {
		t.Run(command[0], func(t *testing.T) {
			l := testInstallLog(t)
			var terminal strings.Builder
			src := installSource{&nativetest.FakeSource{Failures: map[string]string{nativetest.Key(command[0], command[1:]...): "native validation failed: FIXTURE-DETAIL"}}, l, &terminal}
			_, err := src.Run(command[0], command[1:]...)
			if err == nil {
				t.Fatal("native failure disappeared")
			}
			if _, err := fmt.Fprintln(installWriter{&terminal, l}, err); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(terminal.String(), "FIXTURE-DETAIL") {
				t.Fatal("native cause hidden from terminal")
			}
			data, readErr := os.ReadFile(filepath.Join(l.dir, "engine.log"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if strings.Contains(string(data), "FIXTURE-DETAIL") {
				t.Fatal("metadata-only command error entered transcript")
			}
		})
	}
}

func TestSystemResourceLoggingExcludesSecretCapableProperties(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"systemctl", []string{"show", "--property=LoadState,UnitFileState,ActiveState", "--", "greetd.service"}, true},
		{"sudo", []string{"systemctl", "enable", "--", "greetd.service"}, true},
		{"systemctl", []string{"show", "--property=Environment", "docker.service"}, false},
		{"systemctl", []string{"status", "docker.service"}, false},
		{"systemd-tmpfiles", []string{"--create", "/etc/tmpfiles.d/nimbus-noctalia-greeter.conf"}, true},
		{"systemd-tmpfiles", []string{"--create"}, false},
	} {
		if got := publicInstallCommand(tc.name, tc.args); got != tc.want {
			t.Fatalf("%s %v: %t", tc.name, tc.args, got)
		}
	}
}
