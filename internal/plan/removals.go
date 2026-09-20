package plan

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

// removeTransaction previews the removal of declared removes that are still
// installed and that the install transaction does not already erase.
func (b *builder) removeTransaction(installTx *Transaction) (Operation, bool) {
	erased := map[string]bool{}
	if installTx != nil {
		for _, row := range installTx.Packages {
			if strings.HasPrefix(row.Section, "removing") || row.Section == SectionReplaced {
				erased[inspect.PackageID(row.Name, row.Arch)] = true
			}
		}
	}
	var names []string
	for _, name := range b.in.Resolved.Removes {
		if slices.ContainsFunc(b.in.Facts.Packages.Value, func(inst inspect.Package) bool { return inst.Matches(name) && !erased[inst.ID()] }) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return Operation{}, false
	}
	op := b.previewRemoval("packages:remove", names, fmt.Sprintf("remove %s, replaced by declared packages", strings.Join(names, ", ")))
	for _, n := range names {
		op.Paths = append(op.Paths, "removes:"+n)
	}
	return op, true
}

// ownedRemovals uses the verified native identity, or an unambiguous legacy
// receipt, to remove packages the definitions no longer select.
func (b *builder) ownedRemovals() []Operation {
	desired := map[string]bool{}
	desiredNative := map[string]bool{}
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == definitions.PrefixFlatpak {
			desired["flatpak:"+p.Name] = true
			continue
		}
		id := "package:" + p.Canonical
		desired[id] = true
		if inst, ok := InstalledPackage(p.Name, b.in.Applied.Receipts[id], b.in.Facts.Packages.Value); ok {
			desiredNative[inst.ID()] = true
		}
	}
	var names, receiptIDs []string
	var ops []Operation
	for _, id := range slices.Sorted(maps.Keys(b.in.Applied.Receipts)) {
		r := b.in.Applied.Receipts[id]
		if desired[id] {
			continue
		}
		// The removed Copilot provider recorded an RPM receipt. Preserve it
		// so handing its lifecycle to the helper cannot remove or prune the app.
		if id == "package:copilot:github" {
			continue
		}
		switch r.Provider {
		case "dnf":
			r.Resource = id
			name, err := ReceiptPackage(r, b.in.Facts.Packages.Value)
			if err != nil {
				// Explicit selections of every matching architecture transfer
				// ownership without guessing which one the old receipt meant.
				matched, covered := false, true
				for _, inst := range b.in.Facts.Packages.Value {
					if inst.Matches(PackageName(id)) {
						matched = true
						covered = covered && desiredNative[inst.ID()]
					}
				}
				if matched && covered {
					ops = append(ops, Operation{ID: id, Kind: KindPackage, Action: ActionRetire, Risk: RiskLow, Summary: "retire legacy receipt after explicit architecture selections", Paths: []string{"receipt"}})
					continue
				}
				ops = append(ops, Operation{ID: id, Kind: KindPackage, Action: ActionRemove, Risk: RiskMedium, Summary: "resolve ownership of " + PackageName(id), Blocked: err.Error()})
				continue
			}
			_, present := inspect.FindPackage(b.in.Facts.Packages.Value, name)
			if desiredNative[name] || !present {
				op := Operation{ID: id, Kind: KindPackage, Action: ActionRetire, Risk: RiskLow, Summary: "retire the receipt of " + name + ", which is absent or selected by another reference", Paths: []string{"receipt"}}
				if !present {
					op.AbsentPackage = name
				}
				ops = append(ops, op)
				continue
			}
			names = append(names, name)
			receiptIDs = append(receiptIDs, id)
		case "flatpak":
			name := strings.TrimPrefix(id, "flatpak:")
			if !b.in.Facts.Flatpak.Known() {
				ops = append(ops, Operation{ID: id, Kind: KindFlatpak, Action: ActionRemove, Risk: RiskMedium, Summary: "inspect Flatpak " + name + " before removal", Blocked: b.in.Facts.Flatpak.Error})
				continue
			}
			present := slices.ContainsFunc(b.in.Facts.Flatpak.Value.Apps, func(app inspect.FlatpakApp) bool { return app.ID == name })
			if !present {
				ops = append(ops, Operation{ID: id, Kind: KindFlatpak, Action: ActionRetire, Risk: RiskLow, Summary: "retire the receipt of Flatpak " + name + ", which is absent", Paths: []string{"receipt"}, AbsentPackage: name})
				continue
			}
			ops = append(ops, Operation{ID: id, Kind: KindFlatpak, Action: ActionRemove, Risk: RiskMedium, Summary: "remove Flatpak " + name + ", no longer selected", Paths: []string{"receipt"}, Steps: []Step{{Description: "remove the application", Argv: []string{"flatpak", "uninstall", "--system", "--noninteractive", name}, Privileged: true}}})
		}
	}
	if len(names) > 0 {
		slices.Sort(names)
		names = slices.Compact(names)
		op := b.previewRemoval("packages:remove-owned", names, "remove "+strings.Join(names, ", ")+", installed by Nimbus and no longer selected")
		op.Paths = receiptIDs
		ops = append([]Operation{op}, ops...)
	}
	return ops
}

// previewRemoval previews the removal of exactly these packages and blocks
// when DNF would remove anything else.
func (b *builder) previewRemoval(id string, names []string, summary string) Operation {
	slices.Sort(names)
	op := Operation{ID: id, Kind: KindPackage, Action: ActionRemove, Risk: RiskMedium, Summary: summary,
		Steps: []Step{{Description: "run the reviewed removal", Argv: append([]string{"dnf5", "-y", "remove", "--no-autoremove"}, names...), Privileged: true}}}
	if b.waitsForRepositories(&op) {
		return op
	}
	out, err := b.in.Source.Run("dnf5", append([]string{"--assumeno", "--cacheonly", "remove", "--no-autoremove"}, names...)...)
	tx, perr := ParsePreview(out)
	if perr != nil {
		op.Blocked = previewFailure(out, err, perr)
		return op
	}
	op.Transaction = tx
	var extra []string
	for _, row := range tx.Packages {
		pkg := inspect.Package{Name: row.Name, Arch: row.Arch}
		if !slices.ContainsFunc(names, pkg.Matches) {
			extra = append(extra, row.Name)
		}
	}
	if len(extra) > 0 {
		op.Blocked = "removal would also remove " + strings.Join(extra, ", ") + ", which nothing declares"
	}
	return op
}

// pruneTransaction promotes the prune candidates into one removal.
func (b *builder) pruneTransaction(cands []Prune) Operation {
	names := make([]string, 0, len(cands))
	for _, c := range cands {
		names = append(names, c.Name)
	}
	op := b.previewRemoval("packages:prune", names, fmt.Sprintf("prune %d unmanaged packages", len(names)))
	op.Action = ActionPrune
	for _, n := range names {
		op.Paths = append(op.Paths, "unmanaged:"+n)
	}
	return op
}

// prune lists user-installed packages with no desired selection or receipt
// that are outside the baseline. It waits for the first sync to record it.
func (b *builder) prune() []Prune {
	if b.in.Applied == nil || b.in.Applied.Baseline == nil {
		// Without the baseline every pre-existing package would look
		// unmanaged; the first sync records it and prune waits for that.
		return []Prune{}
	}
	desired := map[string]bool{}
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix != definitions.PrefixFlatpak {
			if inst, ok := InstalledPackage(p.Name, b.in.Applied.Receipts["package:"+p.Canonical], b.in.Facts.Packages.Value); ok {
				desired[inst.ID()] = true
			}
		}
	}
	for _, r := range b.in.Resolved.Removes {
		desired[r] = true
	}
	managed := map[string]bool{}
	for id, r := range b.in.Applied.Receipts {
		if r.Provider == "dnf" {
			managed[cmp.Or(r.Package, PackageName(id))] = true
		}
	}
	var out []Prune
	for _, p := range b.in.Facts.Packages.Value {
		if p.Reason != "user" || desired[p.ID()] || desired[p.Name] || b.in.Applied.InBaseline(p.ID()) || b.in.Applied.InBaseline(p.Name) || managed[p.ID()] || managed[p.Name] || IsReleasePackage(b.in.Root, p.Name) {
			continue
		}
		out = append(out, Prune{Name: p.ID(), EVR: p.EVR(), Repository: p.FromRepo})
	}
	slices.SortFunc(out, func(a, b Prune) int { return cmp.Compare(a.Name, b.Name) })
	if out == nil {
		out = []Prune{}
	}
	return out
}
