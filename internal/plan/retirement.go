package plan

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/state"
)

// SourceSnapshot records only stable native source identity and enablement.
func SourceSnapshot(kind string, ids []string, f *inspect.Facts) []state.NativeSource {
	var result []state.NativeSource
	if kind == KindFlatpakRemote {
		for _, r := range f.Flatpak.Value.Remotes {
			if slices.Contains(ids, r.Name) {
				result = append(result, state.NativeSource{ID: r.Name, URL: r.URL, Enabled: true,
					Verify: r.GPGVerify, Keys: sourceKeys(r.KeyFingerprints)})
			}
		}
	} else {
		for _, r := range f.Repositories.Value {
			if slices.Contains(ids, r.ID) {
				result = append(result, state.NativeSource{ID: r.ID, File: r.File, URL: r.BaseURL,
					Metalink: r.Metalink, Mirrorlist: r.Mirrorlist, GPGKey: r.GPGKey, Enabled: r.Enabled,
					Verify: r.GPGCheck == "1", Keys: sourceKeys(r.KeyFingerprints)})
			}
		}
	}
	slices.SortFunc(result, func(a, b state.NativeSource) int { return strings.Compare(a.ID, b.ID) })
	return result
}

func sourceKeys(keys []string) string {
	return strings.Join(slices.Sorted(slices.Values(keys)), ",")
}

func (b *builder) sourceOwnership(op Operation, ids []string) *state.SourceOwnership {
	if receipt, ok := b.in.Applied.Receipts[op.ID]; ok {
		if receipt.Source == nil || !receipt.Verified || receipt.Machine != b.in.Resolved.Machine {
			return nil // Repair does not invent ownership for legacy/foreign state.
		}
		return new(*receipt.Source)
	}
	return &state.SourceOwnership{Original: SourceSnapshot(op.Kind, ids, b.in.Facts)}
}

func (b *builder) sourceRetirements(prior ...[]Operation) []Operation {
	var result []Operation
	for _, id := range slices.Sorted(maps.Keys(b.in.Applied.Receipts)) {
		r := b.in.Applied.Receipts[id]
		if r.Provider != KindRepository && r.Provider != KindFlatpakRemote {
			continue
		}
		name := strings.TrimPrefix(id, r.Provider+":")
		if slices.Contains(b.in.Resolved.Repositories, name) {
			continue
		}
		op := Operation{ID: id, Kind: r.Provider, Action: ActionRemove, Risk: RiskMedium,
			Summary: "retire unselected source " + name, Paths: []string{"receipt"}, Source: r.Source}
		if !r.Verified || r.Machine != b.in.Resolved.Machine || r.Source == nil || len(r.Source.Applied) == 0 {
			op.Blocked = "source retirement is unsupported without a verified receipt for this machine and its original native identity; inspect with native tools"
			result = append(result, op)
			continue
		}
		native := SourceIDs(r.Source)
		shared := slices.ContainsFunc(b.in.Resolved.Repositories, func(selected string) bool {
			ids := DNFRepoIDs(selected, b.in.Root.Repositories[selected])
			if b.in.Root.Repositories[selected].Kind == "flatpak" {
				ids = []string{selected}
			}
			return slices.ContainsFunc(native, func(id string) bool { return slices.Contains(ids, id) })
		})
		if shared {
			op.Action = ActionKeep
			op.Summary = "retain source " + name + "; its native identity is still selected"
			result = append(result, op)
			continue
		}
		if err := CheckSourceRetirement(op, b.in.Facts, b.in.Source); err != nil {
			if inUse, ok := errors.AsType[sourceInUse](err); ok {
				var earlier []Operation
				if len(prior) > 0 {
					earlier = prior[0]
				}
				if after := sourceRemovalDependency(inUse, earlier); after != "" {
					op.After = after
					op.Notes = []string{"reinspect source identity and all installed consumers after their reviewed removal"}
				} else {
					op.Action = ActionKeep
					op.Summary = "retain source " + name + " while installed software needs it"
					op.Notes = []string{err.Error()}
				}
			} else {
				op.Blocked = err.Error()
			}
			result = append(result, op)
			continue
		}
		var steps []Step
		current := SourceSnapshot(op.Kind, SourceIDs(op.Source), b.in.Facts)
		for _, native := range current {
			prior := sourceOriginal(op.Source, native.ID)
			if prior != nil && prior.Enabled {
				continue // Leave preexisting enablement under native ownership.
			}
			if op.Kind == KindFlatpakRemote {
				steps = append(steps, Step{Description: "remove the unused Nimbus-created system remote (never force)",
					Argv: []string{"flatpak", "remote-delete", "--system", native.ID}, Privileged: true})
			} else if native.Enabled {
				steps = append(steps, Step{Description: "disable through an override; retain repository files, keys and release packages",
					Argv: []string{"dnf5", "config-manager", "setopt", native.ID + ".enabled=0"}, Privileged: true})
			}
		}
		op.Steps = steps
		if len(steps) == 0 {
			op.Action = ActionRetire
			op.Summary = "retire tracking for " + name + "; preserve preexisting sources and already-disabled configuration"
		}
		result = append(result, op)
	}
	return result
}

func sourceOriginal(ownership *state.SourceOwnership, id string) *state.NativeSource {
	i := slices.IndexFunc(ownership.Original, func(source state.NativeSource) bool { return source.ID == id })
	if i >= 0 {
		return &ownership.Original[i]
	}
	return nil
}

// SourceIDs returns the exact native IDs recorded after source creation.
func SourceIDs(ownership *state.SourceOwnership) []string {
	var ids []string
	for _, r := range ownership.Applied {
		ids = append(ids, r.ID)
	}
	return ids
}

var nativeSourceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]*$`)

type sourceInUse struct {
	reason    string
	consumers []string
}

func (e sourceInUse) Error() string { return e.reason }

func sourceRemovalDependency(inUse sourceInUse, ops []Operation) string {
	pending := map[string]bool{}
	for _, consumer := range inUse.consumers {
		pending[consumer] = true
	}
	last := ""
	for _, op := range ops {
		if op.Action != ActionRemove || op.Blocked != "" {
			continue
		}
		if op.Kind == KindPackage && op.Transaction != nil {
			for _, row := range op.Transaction.Packages {
				id := (inspect.Package{Name: row.Name, Arch: row.Arch}).ID()
				if row.Section == "removing" && pending[id] {
					delete(pending, id)
					last = op.ID
				}
			}
		}
		if op.Kind == KindFlatpak {
			before := len(pending)
			maps.DeleteFunc(pending, func(consumer string, _ bool) bool {
				parts := strings.Split(consumer, "/")
				return len(parts) == 4 && parts[0] == "app" && op.ID == "flatpak:"+parts[1]
			})
			if len(pending) < before {
				last = op.ID
			}
		}
	}
	if len(pending) == 0 {
		return last
	}
	return ""
}

// CheckSourceRetirement rechecks identity and all installed consumers without
// refreshing metadata or running a privileged/native mutation.
func CheckSourceRetirement(op Operation, f *inspect.Facts, src native.Source) error {
	if op.Source == nil || len(op.Source.Applied) == 0 {
		return fmt.Errorf("source ownership identity is missing")
	}
	if op.Kind == KindFlatpakRemote && !f.Flatpak.Known() {
		return fmt.Errorf("flatpak state is unknown: %s", f.Flatpak.Error)
	}
	if op.Kind == KindRepository && (!f.Repositories.Known() || !f.Packages.Known()) {
		return fmt.Errorf("repository or installed package state is unknown")
	}
	ids := SourceIDs(op.Source)
	if slices.ContainsFunc(ids, func(id string) bool { return !nativeSourceID.MatchString(id) }) {
		return fmt.Errorf("receipt contains an unsafe native source ID")
	}
	current := SourceSnapshot(op.Kind, ids, f)
	seen := map[string]bool{}
	var retiring []string
	for _, have := range current {
		if seen[have.ID] {
			return fmt.Errorf("native source %s is ambiguous", have.ID)
		}
		seen[have.ID] = true
		matched := false
		for _, want := range op.Source.Applied {
			if want.ID == have.ID {
				want.Enabled = have.Enabled
				// Disabled DNF sections do not inspect their keyring. No native
				// mutation is authorized for them; only tracking can be retired.
				if !have.Enabled {
					want.Keys = have.Keys
				}
				matched = want == have
			}
		}
		if !matched {
			return fmt.Errorf("source %s changed since its receipt; refusing retirement", have.ID)
		}
		prior := sourceOriginal(op.Source, have.ID)
		if prior == nil || !prior.Enabled {
			retiring = append(retiring, have.ID)
		}
	}
	if len(retiring) == 0 {
		return nil
	}
	if op.Kind == KindRepository {
		// Vendor packages may retain the ID of a duplicate provider that
		// Nimbus disabled during reconciliation. Its same-location packages
		// still depend on the selected source for updates.
		consumedIDs := slices.Clone(retiring)
		for _, candidate := range f.Repositories.Value {
			for _, owned := range current {
				if slices.Contains(retiring, owned.ID) && candidate.BaseURL != "" && candidate.BaseURL == owned.URL {
					consumedIDs = append(consumedIDs, candidate.ID)
				}
			}
		}
		var consumers []string
		for _, pkg := range f.Packages.Value {
			if pkg.FromRepo == "" || strings.HasPrefix(pkg.FromRepo, "@") {
				return fmt.Errorf("cannot retire source while installed package %s has unknown repository provenance", pkg.ID())
			}
			if slices.Contains(consumedIDs, pkg.FromRepo) {
				consumers = append(consumers, pkg.ID())
			}
		}
		if len(consumers) > 0 {
			return sourceInUse{"source is still needed by installed packages " + strings.Join(consumers, ", "), consumers}
		}
		return nil
	}
	if src == nil {
		return fmt.Errorf("all Flatpak refs must be inspected before source retirement")
	}
	out, err := src.Run("flatpak", "list", "--system", "--all", "--app", "--runtime", "--columns=ref,origin,options")
	if err != nil {
		return fmt.Errorf("inspect all Flatpak refs before retirement: %w", err)
	}
	var consumers []string
	for line := range strings.SplitSeq(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		columns := strings.Split(line, "\t")
		parts := strings.Split(columns[0], "/")
		if len(columns) != 3 || len(parts) != 3 || slices.Contains(parts, "") || !nativeSourceID.MatchString(columns[1]) {
			return fmt.Errorf("all-ref Flatpak inventory is incomplete or malformed")
		}
		if slices.Contains(retiring, columns[1]) {
			kind := "app/"
			if slices.Contains(strings.Split(columns[2], ","), "runtime") {
				kind = "runtime/"
			}
			consumers = append(consumers, kind+columns[0])
		}
	}
	if len(consumers) > 0 {
		return sourceInUse{"source is still needed by installed refs " + strings.Join(consumers, ", "), consumers}
	}
	return nil
}
