package cli

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/postinstall"
)

type runStep struct {
	DurationMS int64  `json:"duration_ms,omitzero"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
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
	Tasks       []postinstall.Task `json:"pending_tasks,omitempty"`
	Notices     []string           `json:"notices,omitempty"`
	Notes       []setupNote        `json:"setup_notes,omitempty"`
	Digest      string             `json:"digest"`
	Executed    []string           `json:"executed"`
	Differences []string           `json:"differences"`
	Upgraded    bool               `json:"upgraded"`
	Reboot      bool               `json:"reboot_required,omitzero"`
	Logout      bool               `json:"logout_required,omitzero"`
	Failed      string             `json:"failed,omitempty"`
	Error       string             `json:"error,omitempty"`
	Steps       []runStep          `json:"steps"`
	Failures    []apply.Failure    `json:"failures,omitempty"`
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
	name := "sync"
	if systemUpgrade {
		name = "upgrade"
	}
	return result.renderNamed(out, name)
}

func (result *syncResult) renderNamed(out io.Writer, name string) error {
	var summary bytes.Buffer
	_ = renderRunSummary(&summary, name, result.Steps)
	if len(result.Differences) > 0 {
		fmt.Fprintln(&summary, "differences from the plan:")
		for _, d := range result.Differences {
			fmt.Fprintf(&summary, "  %s\n", d)
		}
	}
	if err := renderFinalDetails(&summary, result); err != nil {
		return err
	}
	_, err := summary.WriteTo(out)
	return err
}
