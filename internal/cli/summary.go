package cli

import (
	"bytes"
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
			if note != "" && !strings.ContainsFunc(note, unicode.IsControl) && len(w.notes) < 32 && !slices.Contains(w.notes, note) {
				w.notes = append(w.notes, note)
			}
		}
	}
	w.line, w.tooLong = nil, false
}

func (w *setupNoteWriter) render(out io.Writer) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.finishLine()
	if len(w.notes) == 0 {
		return nil
	}
	var summary bytes.Buffer
	fmt.Fprintln(&summary, "\nSetup notes:")
	for _, note := range w.notes {
		fmt.Fprintf(&summary, "  - %s\n", note)
	}
	_, err := summary.WriteTo(out)
	return err
}

func renderRunSummary(out io.Writer, command string, steps []runStep) error {
	var summary bytes.Buffer
	fmt.Fprintf(&summary, "\n%s summary:\n", command)
	if len(steps) == 0 {
		fmt.Fprintln(&summary, "  unchanged: nothing needed to run")
	}
	for _, step := range steps {
		fmt.Fprintf(&summary, "  %-10s %s", step.Status, step.Name)
		if step.DurationMS > 0 {
			fmt.Fprintf(&summary, " (%s)", (time.Duration(step.DurationMS) * time.Millisecond).Round(time.Millisecond))
		}
		if step.Detail != "" {
			fmt.Fprintf(&summary, ": %s", step.Detail)
		}
		fmt.Fprintln(&summary)
	}
	_, err := summary.WriteTo(out)
	return err
}
