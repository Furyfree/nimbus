package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/userstate"
)

func TestPasswordTaskSignInRecovery(t *testing.T) {
	for _, mode := range []string{"recovered", "guided", "already-signed-in", "declined", "canceled", "still-signed-out", "other-error", "no-terminal", "yes"} {
		t.Run(mode, func(t *testing.T) {
			root, src := postinstallFixture(t)
			if err := confirmPasswordState("vm", false); err != nil {
				t.Fatal(err)
			}
			saved := postinstallTerminal
			postinstallTerminal = func(io.Reader) bool { return mode != "no-terminal" }
			t.Cleanup(func() { postinstallTerminal = saved })
			if mode == "guided" {
				savedPrompt := promptLineFn
				promptLineFn = func(io.Reader, io.Writer, string, string) (string, error) { return "no", nil }
				t.Cleanup(func() { promptLineFn = savedPrompt })
			}
			whoami := "op whoami --format=json"
			if mode != "already-signed-in" {
				src.Failures[whoami] = "[ERROR] 2026/09/13 19:50:01 account is not signed in"
			}
			if mode == "other-error" {
				src.Failures[whoami] = "private diagnostic: connection refused"
			}
			if mode == "canceled" {
				src.streamErr = errors.New("private cancellation diagnostic")
			}
			src.onStream = func(command string) {
				if command != "op signin" {
					t.Fatalf("unexpected action: %s", command)
				}
				if mode != "still-signed-out" {
					delete(src.Failures, whoami)
				}
			}
			args := []string{"1password"}
			if mode != "guided" {
				args = append(args, "--mark-done")
			}
			if mode == "yes" {
				args = append(args, "--yes")
			}
			cmd, out := postinstallCommand(root, false, args...)
			answer := "yes\n"
			if mode == "declined" {
				answer = "no\n"
			}
			cmd.SetIn(strings.NewReader(answer))
			err := cmd.Execute()
			complete := slices.Contains([]string{"recovered", "guided", "already-signed-in", "yes"}, mode)
			if (err == nil) != complete {
				t.Fatal(err, out)
			}
			wantSignIn := slices.Contains([]string{"recovered", "guided", "canceled", "still-signed-out", "yes"}, mode)
			if (len(src.streams) == 1) != wantSignIn || len(src.streams) > 1 {
				t.Fatal("wrong sign-in count", src.streams)
			}
			if strings.Contains(out.String(), "Open 1Password") || strings.Contains(out.String(), "private diagnostic") || (err != nil && strings.Contains(err.Error(), "private")) {
				t.Fatal("repeated onboarding or exposed private error", out, err)
			}
			store, _ := userstate.Default()
			evidence, readErr := store.Read("postinstall")
			if readErr != nil || evidence.Has("vm", "onepassword.complete", 1, "verified") != complete {
				t.Fatal(readErr, evidence)
			}
			if !passwordConfirmed("vm", false) {
				t.Fatal("manual confirmation lost")
			}
		})
	}
}

// Execute only a private stub through the production subprocess runner, so
// tests exercise native stdin, stderr and exit status without invoking op.
type passwordProcessSource struct {
	native.Source
	helper string
}

func (s passwordProcessSource) Run(name string, args ...string) ([]byte, error) {
	if name != "op" {
		return nil, errors.New("unexpected command")
	}
	return (native.ExecSource{}).Run(s.helper, args...)
}
func (s passwordProcessSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	if name != "op" {
		return errors.New("unexpected command")
	}
	return (native.ExecSource{}).Stream(out, errOut, s.helper, args...)
}

func TestPasswordSignInNativeInputAndPrivateOutput(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "fake-op")
	t.Setenv("NIMBUS_TEST_SESSION", filepath.Join(dir, "session"))
	body := `#!/bin/sh
case "$1" in
whoami)
  if [ ! -f "$NIMBUS_TEST_SESSION" ]; then
    echo '[ERROR] account is not signed in' >&2
    exit 1
  fi
  echo 'private-account-output'
  ;;
signin)
  echo 'Native account authorization prompt' >&2
  read -r answer
  [ "$answer" = allow ] || exit 1
  echo 'private-session-token'
  : > "$NIMBUS_TEST_SESSION"
  ;;
*) exit 99 ;;
esac
`
	if err := os.WriteFile(helper, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	if _, err := io.WriteString(writer, "allow\n"); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	stdin := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() { os.Stdin = stdin })
	src := passwordProcessSource{Source: &nativetest.FakeSource{Paths: map[string]string{"op": helper}}, helper: helper}
	cmd, out := postinstallCommand(dir, false)
	cmd.SetContext(t.Context())
	if err := passwordTaskPrerequisites(cmd, src, false, true); err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out.String(), "Native account authorization prompt") || strings.Contains(out.String(), "private-") {
		t.Fatal("native prompt lost or private stdout exposed", out)
	}
}
