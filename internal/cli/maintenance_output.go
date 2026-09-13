package cli

import (
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/plan"
)

func (r *syncResult) rememberOperations(p *plan.Plan) {
	if r.OperationLabels == nil {
		r.OperationLabels = map[string]string{}
	}
	for _, op := range p.Operations {
		if op.Action == plan.ActionKeep || slices.Contains(r.Executed, op.ID) {
			continue
		}
		r.OperationLabels[op.ID] = op.Summary
		if op.Kind == plan.KindFile && op.Action == plan.ActionAdopt && !slices.Contains(r.MatchingFiles, op.ID) {
			r.MatchingFiles = append(r.MatchingFiles, op.ID)
		}
		if op.ID == "packages:install" && op.Transaction != nil {
			r.OperationLabels[op.ID] = fmt.Sprintf("package installation (%d packages including dependencies)", len(op.Transaction.Rows("installing"))+len(op.Transaction.Rows("installing dependencies"))+len(op.Transaction.Rows("installing weak dependencies")))
		}
	}
}

func (r *syncResult) conciseSteps() []runStep {
	var steps []runStep
	matching := 0
	var snapshots []string
	cleanups := 0
	for _, step := range r.Steps {
		if step.Status == "succeeded" {
			if slices.Contains(r.MatchingFiles, step.Name) {
				matching++
				continue
			}
			if step.Name == "snapper pre" || step.Name == "snapper post" {
				snapshots = append(snapshots, strings.TrimPrefix(step.Name, "snapper ")+" "+step.Detail)
				continue
			}
			if step.Name == "snapper cleanup" {
				cleanups++
				continue
			}
		}
		if label := r.OperationLabels[step.Name]; label != "" && step.Status == "succeeded" {
			step.Name = label
		}
		switch step.Name {
		case "upgrade:dnf":
			step.Name = "DNF update"
		case "upgrade:flatpak":
			step.Name = "system Flatpak update"
		}
		steps = append(steps, step)
	}
	if matching > 0 {
		steps = append(steps, runStep{Name: "file ownership records", Status: "succeeded", Detail: fmt.Sprintf("%d matching files; contents unchanged", matching)})
	}
	if len(snapshots) > 0 {
		steps = append(steps, runStep{Name: "Snapper snapshots", Status: "succeeded", Detail: strings.Join(snapshots, ", ")})
	}
	if cleanups > 0 {
		steps = append(steps, runStep{Name: "snapshot retention", Status: "succeeded"})
	}
	return steps
}

func (r *syncResult) collectPlanNotices(p *plan.Plan) {
	for _, op := range p.Operations {
		if plan.IsConstraintOperation(op) {
			continue
		}
		for _, note := range op.Notes {
			if !slices.Contains(r.Notices, note) {
				r.Notices = append(r.Notices, note)
			}
		}
	}
}

func showReplanned(out io.Writer, previous, current *plan.Plan, prune bool, result *syncResult) error {
	result.collectPlanNotices(current)
	old := map[string]plan.Operation{}
	for _, op := range previous.Operations {
		old[op.ID] = op
	}
	delta := *current
	delta.Operations = nil
	delta.Snapshots, delta.RepositoryReconciliation = nil, ""
	for _, op := range current.Operations {
		if op.Action == plan.ActionKeep || slices.Contains(result.Executed, op.ID) {
			continue
		}
		before, exists := old[op.ID]
		a, b := before, op
		// Becoming runnable is already covered by the approved dependencies.
		a.After, b.After, a.Notes, b.Notes = "", "", nil, nil
		if exists && reflect.DeepEqual(a, b) {
			continue
		}
		delta.Operations = append(delta.Operations, op)
		change := "updated plan: " + op.Summary
		if !slices.Contains(result.Differences, change) {
			result.Differences = append(result.Differences, change)
		}
	}
	if len(delta.Operations) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "Changes to the remaining plan:"); err != nil {
		return fmt.Errorf("show updated plan: %w", err)
	}
	_, err := out.Write(renderExecutionPlan(&delta, prune, false))
	if err != nil {
		return fmt.Errorf("show updated plan: %w", err)
	}
	return nil
}
