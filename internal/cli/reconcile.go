package cli

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
)

// reconcileRepositories closes the package-scriptlet part of source
// preparation before another transaction can use a duplicate provider. The
// caller holds the sync lock and has shown this policy with the approved plan.
// Only duplicate-disable overrides are covered; new canonical source or key
// drift needs a new sync and its own reviewed plan.
func reconcileRepositories(s *selected, flags machineFlags, src facts.Source, approvedCheckout facts.Checkout, options func(*plan.Plan) apply.Options, out io.Writer, result *syncResult) error {
	p, _, err := replanUnchanged(s, flags, src, false, approvedCheckout)
	if err != nil {
		return fmt.Errorf("inspect repositories after package transaction: %w", err)
	}
	repairs, err := duplicateRepositoryRepairs(p)
	if err != nil {
		return err
	}
	if len(repairs.Operations) == 0 {
		return nil
	}
	fmt.Fprintln(out, "repository reconciliation after package transaction:")
	out.Write(renderPlan(repairs, false, false))
	r := apply.Run(repairs, options(p))
	result.Executed = append(result.Executed, r.Executed...)
	result.Differences = append(result.Differences, r.Differences...)
	result.Failures = append(result.Failures, r.Failures...)
	for _, op := range repairs.Operations {
		if slices.Contains(r.Executed, op.ID) {
			result.Differences = append(result.Differences, "after package transaction: "+op.Summary)
		}
	}
	if r.Error != "" {
		result.Failed, result.Error = r.Failed, r.Error
		return fmt.Errorf("repository reconciliation: %s", r.Error)
	}
	verified, _, err := replanUnchanged(s, flags, src, false, approvedCheckout)
	if err != nil {
		return fmt.Errorf("verify repositories after reconciliation: %w", err)
	}
	remaining, err := duplicateRepositoryRepairs(verified)
	if err != nil {
		return err
	}
	if len(remaining.Operations) != 0 {
		return fmt.Errorf("repository reconciliation did not converge; run sync again")
	}
	if _, err := src.Run("dnf5", "makecache"); err != nil {
		return fmt.Errorf("refresh metadata after repository reconciliation: %w", err)
	}
	return nil
}

func duplicateRepositoryRepairs(p *plan.Plan) (*plan.Plan, error) {
	repairs := *p
	repairs.Operations = nil
	repairs.Complete = true
	for _, op := range p.Operations {
		if op.Kind != plan.KindRepository || op.Action == plan.ActionKeep {
			continue
		}
		allowed := op.Action == plan.ActionRepair && op.Blocked == "" && op.After == "" && len(op.Steps) > 0
		for _, step := range op.Steps {
			argv := step.Argv
			if step.Description != plan.DisableDuplicateDescription || !step.Privileged || len(argv) < 4 || strings.Join(argv[:3], " ") != "dnf5 config-manager setopt" {
				allowed = false
				continue
			}
			if slices.ContainsFunc(argv[3:], func(option string) bool { return !strings.HasSuffix(option, ".enabled=0") }) {
				allowed = false
			}
		}
		if !allowed {
			reason := cmp.Or(op.Blocked, op.Summary)
			return nil, fmt.Errorf("repository changed beyond duplicate reconciliation: %s; run sync again", reason)
		}
		repairs.Operations = append(repairs.Operations, op)
	}
	return &repairs, nil
}
