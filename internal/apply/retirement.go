package apply

import (
	"fmt"
	"slices"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func (ex *executor) sourceRetirement(op plan.Operation) ([]state.Receipt, []string, error) {
	observe := func() *inspect.Facts {
		if op.Kind == plan.KindFlatpakRemote {
			return &inspect.Facts{Flatpak: inspect.SystemFlatpak(ex.opts.Source)}
		}
		return &inspect.Facts{
			Packages:     inspect.Packages(ex.opts.Source),
			Repositories: inspect.Repositories(ex.opts.Source),
		}
	}
	f := observe()
	if err := plan.CheckSourceRetirement(op, f, ex.opts.Source); err != nil {
		return nil, nil, fmt.Errorf("source changed after approval: %w", err)
	}
	for _, step := range op.Steps {
		if err := ex.sudo(step.Argv...); err != nil {
			return nil, nil, err
		}
	}
	f = observe()
	if err := plan.CheckSourceRetirement(op, f, ex.opts.Source); err != nil {
		return nil, nil, fmt.Errorf("source retirement verification: %w", err)
	}
	for _, current := range plan.SourceSnapshot(op.Kind, plan.SourceIDs(op.Source), f) {
		originallyEnabled := slices.ContainsFunc(op.Source.Original, func(previous state.NativeSource) bool {
			return previous.ID == current.ID && previous.Enabled
		})
		if !originallyEnabled && current.Enabled {
			return nil, nil, fmt.Errorf("source retirement verification: %s remains enabled", current.ID)
		}
	}
	return nil, []string{op.ID}, nil
}

func recordSourceOwnership(receipt *state.Receipt, op plan.Operation, ids []string, f *inspect.Facts) {
	if op.Source == nil {
		return // Legacy repairs do not manufacture removal authority.
	}
	receipt.Source = &state.SourceOwnership{
		Original: slices.Clone(op.Source.Original),
		Applied:  plan.SourceSnapshot(op.Kind, ids, f),
	}
}
