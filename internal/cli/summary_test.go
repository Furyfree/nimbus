package cli

import (
	"fmt"
	"strings"
	"testing"
)

func TestSetupNotesPreserveLiveOutputAndCollectOnlyInstructions(t *testing.T) {
	var live, footer strings.Builder
	w := &setupNoteWriter{out: &live}
	chunks := []string{
		"Compiling...\nSetup no", "te: Sign in to 1Password.\r\n",
		"Setup note: Sign in to 1Password.\n",
		"error: install failed\nSetup note: Open a new terminal.",
	}
	for _, chunk := range chunks {
		if n, err := w.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("write = %d, %v", n, err)
		}
	}
	w.render(&footer)
	if live.String() != strings.Join(chunks, "") {
		t.Fatal("native output was changed")
	}
	want := "\nSetup notes:\n  - Sign in to 1Password.\n  - Open a new terminal.\n"
	if footer.String() != want {
		t.Fatalf("footer = %q", footer.String())
	}
}

func TestSetupNotesBoundCaptureWithoutTruncatingLiveOutput(t *testing.T) {
	var live, footer strings.Builder
	w := &setupNoteWriter{out: &live}
	var input strings.Builder
	input.WriteString("Setup note: " + strings.Repeat("x", 5000) + "\nSetup note: \x1b[31mcontrol\n")
	for i := range 40 {
		fmt.Fprintf(&input, "Setup note: instruction %d\n", i)
	}
	if _, err := w.Write([]byte(input.String())); err != nil {
		t.Fatal(err)
	}
	w.render(&footer)
	if live.String() != input.String() || len(w.notes) != 32 || strings.Contains(footer.String(), "control") {
		t.Fatalf("capture bounds or live output failed: %q", footer.String())
	}
}
