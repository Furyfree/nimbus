package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

type runStep struct {
	DurationMS int64  `json:"duration_ms,omitzero"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
}

// setupNoteWriter keeps explicit instructions while forwarding all native
// output unchanged. It never retains an installation transcript.
type setupNoteWriter struct {
	out     io.Writer
	mu      sync.Mutex
	line    []byte
	tooLong bool
	notes   []string
}

func (w *setupNoteWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.out.Write(p)
	for _, b := range p[:n] {
		if b == '\n' {
			w.finishLine()
		} else if !w.tooLong {
			if len(w.line) == 4096 {
				w.line = nil
				w.tooLong = true
			} else {
				w.line = append(w.line, b)
			}
		}
	}
	return n, err
}

func (w *setupNoteWriter) finishLine() {
	if !w.tooLong {
		line := strings.TrimSuffix(string(w.line), "\r")
		if note, ok := strings.CutPrefix(line, "Setup note: "); ok {
			note = strings.TrimSpace(note)
			if note != "" && strings.IndexFunc(note, unicode.IsControl) < 0 && len(w.notes) < 32 && !slices.Contains(w.notes, note) {
				w.notes = append(w.notes, note)
			}
		}
	}
	w.line, w.tooLong = nil, false
}

func (w *setupNoteWriter) render(out io.Writer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.finishLine()
	if len(w.notes) == 0 {
		return
	}
	fmt.Fprintln(out, "\nSetup notes:")
	for _, note := range w.notes {
		fmt.Fprintf(out, "  - %s\n", note)
	}
}

func renderRunSummary(out io.Writer, command string, steps []runStep) {
	fmt.Fprintf(out, "\n%s summary:\n", command)
	if len(steps) == 0 {
		fmt.Fprintln(out, "  unchanged: nothing needed to run")
	}
	for _, step := range steps {
		fmt.Fprintf(out, "  %-10s %s", step.Status, step.Name)
		if step.DurationMS > 0 {
			fmt.Fprintf(out, " (%s)", (time.Duration(step.DurationMS) * time.Millisecond).Round(time.Millisecond))
		}
		if step.Detail != "" {
			fmt.Fprintf(out, ": %s", step.Detail)
		}
		fmt.Fprintln(out)
	}
}
