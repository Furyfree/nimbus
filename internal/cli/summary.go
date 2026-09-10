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

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/plan"
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

type syncResult struct {
	Digest      string          `json:"digest"`
	Executed    []string        `json:"executed"`
	Differences []string        `json:"differences"`
	Upgraded    bool            `json:"upgraded"`
	Reboot      bool            `json:"reboot_required,omitzero"`
	Logout      bool            `json:"logout_required,omitzero"`
	Failed      string          `json:"failed,omitempty"`
	Error       string          `json:"error,omitempty"`
	Steps       []runStep       `json:"steps"`
	Failures    []apply.Failure `json:"failures,omitempty"`
}

func (result *syncResult) finish(phase string, retErr error, currentPlan *plan.Plan) {
	if retErr != nil && result.Error == "" {
		result.Failed, result.Error = phase, retErr.Error()
	}
	for _, id := range result.Executed {
		result.Steps = append(result.Steps, runStep{Name: id, Status: "succeeded"})
	}
	failedIDs := map[string]bool{}
	for _, failure := range result.Failures {
		failedIDs[failure.ID] = true
		result.Steps = append(result.Steps, runStep{Name: failure.ID, Status: "failed", Detail: failure.Error})
	}
	if result.Error != "" && !failedIDs[result.Failed] {
		failedIDs[result.Failed] = true
		result.Steps = append(result.Steps, runStep{Name: result.Failed, Status: "failed", Detail: result.Error})
	}
	if currentPlan != nil {
		for _, op := range currentPlan.Operations {
			if op.Action == plan.ActionKeep || slices.Contains(result.Executed, op.ID) || failedIDs[op.ID] {
				continue
			}
			detail := "an earlier stage did not complete"
			if op.Blocked != "" {
				detail = "blocked: " + op.Blocked
			} else if op.After != "" {
				detail = "waiting for " + describeAfter(currentPlan, op.After)
			}
			result.Steps = append(result.Steps, runStep{Name: op.ID, Status: "skipped", Detail: detail})
		}
	}
}

func (result *syncResult) render(out io.Writer, systemUpgrade bool) error {
	var summary bytes.Buffer
	name := "sync"
	if systemUpgrade {
		name = "upgrade"
	}
	_ = renderRunSummary(&summary, name, result.Steps)
	if result.Reboot {
		fmt.Fprintln(&summary, "Reboot required to use the configured boot target or greeter.")
	}
	if result.Logout {
		fmt.Fprintln(&summary, "Log out and log in again to use changed group memberships.")
	}
	if len(result.Differences) > 0 {
		fmt.Fprintln(&summary, "differences from the plan:")
		for _, d := range result.Differences {
			fmt.Fprintf(&summary, "  %s\n", d)
		}
	}
	_, err := summary.WriteTo(out)
	return err
}
