package apply

import (
	"fmt"
	"slices"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func (ex *executor) sourceRetirement(op plan.Operation) ([]state.Receipt, []string, error) {
	f := facts.Inspect(ex.opts.Source, "")
	if err := plan.CheckSourceRetirement(op, f, ex.opts.Source); err != nil {
		return nil, nil, fmt.Errorf("source changed after approval: %w", err)
	}
	for _, step := range op.Steps {
		if err := ex.sudo(step.Argv...); err != nil {
			return nil, nil, err
		}
	}
	f = facts.Inspect(ex.opts.Source, "")
	if err := plan.CheckSourceRetirement(op, f, ex.opts.Source); err != nil {
		return nil, nil, fmt.Errorf("source retirement verification: %w", err)
	}
	for _, current := range plan.SourceSnapshot(op.Kind, plan.SourceIDs(op.Source), f) {
		originallyEnabled := false
		for _, previous := range op.Source.Original {
			originallyEnabled = originallyEnabled || previous.ID == current.ID && previous.Enabled
		}
		if !originallyEnabled && current.Enabled {
			return nil, nil, fmt.Errorf("source retirement verification: %s remains enabled", current.ID)
		}
	}
	return nil, []string{op.ID}, nil
}

func recordSourceOwnership(receipt *state.Receipt, op plan.Operation, ids []string, f *facts.Facts) {
	if op.Source == nil {
		return // Legacy repairs do not manufacture removal authority.
	}
	receipt.Source = &state.SourceOwnership{
		Original: slices.Clone(op.Source.Original),
		Applied:  plan.SourceSnapshot(op.Kind, ids, f),
	}
}
