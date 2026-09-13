package cli

import (
	"errors"
	"strings"
	"syscall"
	"testing"
)

func TestRunSummaryPreservesFormattingAndReportsOutputFailure(t *testing.T) {
	steps := []runStep{{Name: "system installation", Status: "succeeded", DurationMS: 1500}, {Name: "dotfiles", Status: "failed", Detail: "hook failed"}}
	var out strings.Builder
	if err := renderRunSummary(&out, "init", steps); err != nil {
		t.Fatal(err)
	}
	want := "\ninit summary:\n  succeeded  system installation (1.5s)\n  failed     dotfiles: hook failed\n"
	if out.String() != want {
		t.Fatalf("summary = %q, want %q", out.String(), want)
	}
	if err := renderRunSummary(&previewErrorWriter{err: syscall.ENOSPC}, "init", steps); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("summary error = %v", err)
	}
	if err := displayNotes(&previewErrorWriter{err: syscall.ENOSPC}, []setupNote{{ID: "example", Revision: 1, Text: "Guidance"}}); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal(err)
	}
}
