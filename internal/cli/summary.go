package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/output"
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
	if _, err := fmt.Fprintf(out, "\n%s summary:\n", command); err != nil {
		return err
	}
	if len(steps) == 0 {
		return output.StatusLine(out, "unchanged", "nothing needed to run")
	}
	for _, step := range steps {
		var message strings.Builder
		message.WriteString(step.Name)
		if step.DurationMS > 0 {
			fmt.Fprintf(&message, " (%s)", (time.Duration(step.DurationMS) * time.Millisecond).Round(time.Millisecond))
		}
		if step.Detail != "" {
			fmt.Fprintf(&message, ": %s", step.Detail)
		}
		if err := output.StatusLine(out, step.Status, message.String()); err != nil {
			return err
		}
	}
	return nil
}

type syncResult struct {
	Verbose         bool               `json:"-"`
	OperationLabels map[string]string  `json:"operation_labels,omitempty"`
	MatchingFiles   []string           `json:"matching_files,omitempty"`
	Tasks           []postinstall.Task `json:"pending_tasks,omitempty"`
	Historical      []string           `json:"historical_verification,omitempty"`
	Notices         []string           `json:"notices,omitempty"`
	Notes           []setupNote        `json:"setup_notes,omitempty"`
	Digest          string             `json:"digest"`
	Executed        []string           `json:"executed"`
	Differences     []string           `json:"differences"`
	Upgraded        bool               `json:"upgraded"`
	Reboot          bool               `json:"reboot_required,omitzero"`
	Logout          bool               `json:"logout_required,omitzero"`
	Failed          string             `json:"failed,omitempty"`
	Error           string             `json:"error,omitempty"`
	Steps           []runStep          `json:"steps"`
	Failures        []apply.Failure    `json:"failures,omitempty"`
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
	if err := renderRunSummary(out, name, result.conciseSteps()); err != nil {
		return err
	}
	if len(result.Differences) > 0 {
		if _, err := fmt.Fprintln(out, "differences from the plan:"); err != nil {
			return err
		}
		for _, d := range result.Differences {
			if _, err := fmt.Fprintf(out, "  %s\n", d); err != nil {
				return err
			}
		}
	}
	return renderFinalDetails(out, result)
}
