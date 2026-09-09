package cli

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func shellHelper(t *testing.T, home, body string, extra ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("bash", "-c", `. "$LIBRARY"`+"\n"+body)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "CHECKOUT=" + repoRoot(t), "LIBRARY=" + filepath.Join(repoRoot(t), "install.sh")}, extra...)
	return cmd.CombinedOutput()
}

func TestShellLogsFailureWithoutInput(t *testing.T) {
	home := t.TempDir()
	out, err := shellHelper(t, home, `installer_session
installer_run bash -c 'echo visible-output; echo diagnostic >&2; read -r ignored; exit 17' <<<'private-input'
`)
	if err == nil {
		t.Fatal("command failure was lost")
	}
	runs, _ := filepath.Glob(filepath.Join(home, ".local/state/nimbus/install/run-*"))
	if len(runs) != 1 {
		t.Fatalf("runs=%v output=%s", runs, out)
	}
	data, err := os.ReadFile(filepath.Join(runs[0], "bootstrap.log"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"visible-output", "diagnostic", "status=17", "elapsed_seconds="} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "private-input") {
		t.Fatal("logged command input")
	}
	if _, err := os.Stat(filepath.Join(runs[0], ".active")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("run remains active: %v", err)
	}
	finished, err := os.ReadFile(filepath.Join(runs[0], ".finished"))
	if err != nil || !strings.Contains(string(finished), "status=17") {
		t.Fatalf("completion=%s err=%v", finished, err)
	}
}

func TestShellLogRetentionPreservesUnknownAndActiveRuns(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "logs")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	for i := range 30 {
		dir := filepath.Join(root, fmt.Sprintf("run-%03d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{".nimbus-install": "1\n", ".finished": "status=0\n", "bootstrap.log": "public output"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Protected directories are both newer and older than eligible runs.
	for _, index := range []int{0, 27} {
		dir := filepath.Join(root, fmt.Sprintf("run-%03d", index))
		if err := os.WriteFile(filepath.Join(dir, "unrelated"), []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range []int{1, 28} {
		dir := filepath.Join(root, fmt.Sprintf("run-%03d", index))
		if err := os.WriteFile(filepath.Join(dir, ".active"), []byte("123\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, ".finished")); err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range []int{2, 29} {
		dir := filepath.Join(root, fmt.Sprintf("run-%03d", index))
		if err := os.Link(filepath.Join(dir, "bootstrap.log"), filepath.Join(home, fmt.Sprintf("unrelated-log-%d", index))); err != nil {
			t.Fatal(err)
		}
	}
	out, err := shellHelper(t, home, `installer_prune "$LOG_ROOT"`, "LOG_ROOT="+root)
	if err != nil {
		t.Fatalf("prune: %v %s", err, out)
	}
	// Keep all six protected directories plus the newest 20 completed runs.
	for i := range 30 {
		name := fmt.Sprintf("run-%03d", i)
		_, err := os.Stat(filepath.Join(root, name))
		if i >= 3 && i <= 6 {
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("did not prune %s: %v", name, err)
			}
		} else if err != nil {
			t.Fatalf("removed protected or recent completed %s: %v", name, err)
		}
	}
}

func TestShellRejectsSymlinkLogsWithoutWriting(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "preserve")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	out, err := shellHelper(t, home, `installer_session`, "NIMBUS_INSTALL_LOG_DIR="+link)
	if err == nil || !strings.Contains(string(out), "symlink") {
		t.Fatalf("accepted unsafe logs: %v %s", err, out)
	}
	files, _ := os.ReadDir(target)
	if len(files) != 0 {
		t.Fatalf("wrote before checking log path: %v", files)
	}
}

func TestShellRejectsArgumentsBeforeStartingRun(t *testing.T) {
	for _, args := range []string{"--unknown", "--machine", "--machine vm --new other", "--onepassword-ssh --new vm --no-dotfiles", "--dotfiles example"} {
		t.Run(args, func(t *testing.T) {
			home := t.TempDir()
			out, err := shellHelper(t, home, "installer_arguments "+args+"\ninstaller_session")
			if err == nil {
				t.Fatalf("accepted %s: %s", args, out)
			}
			if _, err := os.Stat(filepath.Join(home, ".local")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("wrote before validating arguments: %v", err)
			}
		})
	}
}

func TestShellHandoffStopsChildOnInterruption(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			home := t.TempDir()
			ready := filepath.Join(home, "ready")
			cmd := exec.Command("bash", "-c", `. "$LIBRARY"
installer_session
installer_handoff bash -c 'printf "%s" "$BASHPID" > "$READY"; exec sleep 20'
`)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "CHECKOUT=" + repoRoot(t), "LIBRARY=" + filepath.Join(repoRoot(t), "install.sh"), "READY=" + ready}
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			deadline := time.Now().Add(3 * time.Second)
			child := 0
			for time.Now().Before(deadline) {
				raw, err := os.ReadFile(ready)
				if err == nil {
					child, _ = strconv.Atoi(string(raw))
					if child > 0 {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if child == 0 {
				t.Fatal("child did not start")
			}
			t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("interruption was lost: %s", out.String())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("installer did not finish after interrupt")
			}
			if err := syscall.Kill(child, 0); err == nil {
				t.Fatal("child survived installer interruption")
			}
			runs, _ := filepath.Glob(filepath.Join(home, ".local/state/nimbus/install/run-*"))
			if len(runs) != 1 {
				t.Fatalf("logs missing: %s", out.String())
			}
			if _, err := os.Stat(filepath.Join(runs[0], ".active")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("interrupted run still active: %v", err)
			}
		})
	}
}

func TestShellRejectsUnsafeInheritedRun(t *testing.T) {
	for _, unsafe := range []string{"hardlink-log", "hardlink-marker", "hardlink-active", "missing-active", "finished", "public-log", "symlink-active"} {
		t.Run(unsafe, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, "run-fixture")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			contents := map[string]string{".nimbus-install": "1\n", ".active": "123\n", "bootstrap.log": "preserve\n"}
			for name, content := range contents {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			target := filepath.Join(dir, "bootstrap.log")
			switch unsafe {
			case "hardlink-log", "hardlink-marker", "hardlink-active":
				name := "bootstrap.log"
				if unsafe == "hardlink-marker" {
					name = ".nimbus-install"
				}
				if unsafe == "hardlink-active" {
					name = ".active"
				}
				if err := os.Link(filepath.Join(dir, name), filepath.Join(home, "unrelated")); err != nil {
					t.Fatal(err)
				}
			case "missing-active":
				if err := os.Remove(filepath.Join(dir, ".active")); err != nil {
					t.Fatal(err)
				}
			case "finished":
				if err := os.WriteFile(filepath.Join(dir, ".finished"), []byte("status=0\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "public-log":
				if err := os.Chmod(target, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink-active":
				active := filepath.Join(dir, ".active")
				if err := os.Remove(active); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, active); err != nil {
					t.Fatal(err)
				}
			}
			out, err := shellHelper(t, home, `installer_session`, "NIMBUS_INSTALL_LOG_DIR="+dir)
			if err == nil {
				t.Fatalf("accepted %s: %s", unsafe, out)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != contents["bootstrap.log"] {
				t.Fatalf("modified unrelated data: %q %v", data, err)
			}
		})
	}
}

func TestShellPreservesNativeFailureWhenTeeFails(t *testing.T) {
	for _, native := range []int{0, 17} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			home := t.TempDir()
			body := fmt.Sprintf(`installer_session
tee() { cat; return 23; }
installer_run bash -c 'echo native-output; exit %d'
`, native)
			out, err := shellHelper(t, home, body)
			exit, ok := errors.AsType[*exec.ExitError](err)
			want := cmp.Or(native, 1)
			if !ok || exit.ExitCode() != want || !strings.Contains(string(out), "installation log write failed (tee status=23)") {
				t.Fatalf("lost command/log error: %v %s", err, out)
			}
		})
	}
}

func TestShellCompletionDoesNotFollowReplacedMarker(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "unrelated")
	if err := os.WriteFile(target, []byte("preserve\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := shellHelper(t, home, `installer_session
ln -s "$TARGET" "$NIMBUS_INSTALL_LOG_DIR/.finished"
`, "TARGET="+target)
	if err == nil || !strings.Contains(string(out), "cannot create installation completion marker") {
		t.Fatalf("completion replacement accepted: %v %s", err, out)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "preserve\n" {
		t.Fatalf("overwrote unrelated completion target: %q %v", data, err)
	}
	runs, _ := filepath.Glob(filepath.Join(home, ".local/state/nimbus/install/run-*"))
	if len(runs) != 1 {
		t.Fatalf("missing run: %v", runs)
	}
	if _, err := os.Stat(filepath.Join(runs[0], ".active")); err != nil {
		t.Fatalf("invalid completion marked inactive: %v", err)
	}
}

func TestShellNormalizesLogBaseSlashes(t *testing.T) {
	for _, variable := range []string{"HOME", "XDG_STATE_HOME"} {
		t.Run(variable, func(t *testing.T) {
			home := t.TempDir()
			repeated := strings.ReplaceAll(home, "/", "//") + "///"
			extra := []string{}
			if variable == "HOME" {
				home = repeated
			} else {
				extra = append(extra, "XDG_STATE_HOME="+repeated)
			}
			out, err := shellHelper(t, home, `installer_session
printf 'exported-log=%s\n' "$NIMBUS_INSTALL_LOG_DIR"
`, extra...)
			if err != nil {
				t.Fatalf("slash normalization: %v %s", err, out)
			}
			found := false
			for line := range strings.SplitSeq(string(out), "\n") {
				if path, ok := strings.CutPrefix(line, "exported-log="); ok {
					found = true
					if path != filepath.Clean(path) {
						t.Fatalf("exported noncanonical log path %q", path)
					}
				}
			}
			if !found {
				t.Fatalf("missing exported log path: %s", out)
			}
		})
	}
}

func TestShellStillRejectsTraversalInLogBase(t *testing.T) {
	home := t.TempDir()
	out, err := shellHelper(t, home, `installer_session`, "XDG_STATE_HOME="+home+"//../outside//")
	if err == nil || !strings.Contains(string(out), "absolute and normalized") {
		t.Fatalf("accepted log traversal: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, "nimbus")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrote before rejecting traversal: %v", err)
	}
}
