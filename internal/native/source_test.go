package native

import (
	"bytes"
	"errors"
	"strings"
	"syscall"
	"testing"

	"github.com/Furyfree/nimbus/internal/output"
)

func TestExecSourceReturnsOutputOnFailure(t *testing.T) {
	out, err := (ExecSource{}).Run("sh", "-c", "echo inactive; exit 3")
	if err == nil {
		t.Fatal("non-zero exit must be an error")
	}
	if strings.TrimSpace(string(out)) != "inactive" {
		t.Fatalf("stdout lost on failure: %q", out)
	}
	if _, err := (ExecSource{}).Run("sh", "-c", "echo bad >&2; exit 1"); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("stderr missing from error: %v", err)
	}
}

func TestNativeProgressBypassesNimbusColors(t *testing.T) {
	var out, errOut bytes.Buffer
	stdout := output.ColorWriter(&out, func() bool { return true })
	stderr := output.ColorWriter(&errOut, func() bool { return true })
	err := (ExecSource{}).Stream(stdout, stderr, "sh", "-c", "printf 'failed native output\\n'; printf 'warning native error\\n' >&2")
	if err != nil || out.String() != "failed native output\n" || errOut.String() != "warning native error\n" {
		t.Fatal(err, out.String(), errOut.String())
	}
}

func TestExecSourceReportsDirectories(t *testing.T) {
	_, err := (ExecSource{}).ReadFile(t.TempDir())
	if !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("directory read = %v", err)
	}
}
