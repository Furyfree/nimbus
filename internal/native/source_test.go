package native

import (
	"errors"
	"strings"
	"syscall"
	"testing"
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

func TestExecSourceReportsDirectories(t *testing.T) {
	_, err := (ExecSource{}).ReadFile(t.TempDir())
	if !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("directory read = %v", err)
	}
}
