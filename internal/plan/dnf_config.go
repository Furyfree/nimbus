package plan

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

// DNFDropIn renders the [dnf] table of nimbus.toml as the libdnf5 drop-in
// Nimbus owns: sorted keys, booleans as DNF spells them. It is empty when
// nothing is declared.
func DNFDropIn(root definitions.Root, constraints ...definitions.PackageConstraint) string {
	if len(constraints) > 0 {
		root.DNF = maps.Clone(root.DNF)
		if root.DNF == nil {
			root.DNF = map[string]any{}
		}
		var excludes []string
		if existing, ok := root.DNF["excludepkgs"].(string); ok && existing != "" {
			excludes = append(excludes, existing)
		}
		for _, c := range constraints {
			// Include the epoch to avoid excluding similarly named subpackages.
			excludes = append(excludes, c.Name+"-0:*[!0-9.]*-*")
		}
		root.DNF["excludepkgs"] = strings.Join(excludes, ",")
	}
	if len(root.DNF) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Written by Nimbus from the [dnf] table of nimbus.toml; edit that instead.\n[main]\n")
	for _, key := range slices.Sorted(maps.Keys(root.DNF)) {
		fmt.Fprintf(&b, "%s=%s\n", key, dnfValue(root.DNF[key]))
	}
	return b.String()
}

func dnfValue(v any) string {
	switch v := v.(type) {
	case bool:
		if v {
			return "True"
		}
		return "False"
	default:
		return fmt.Sprint(v)
	}
}

// DNFDropInPlaceholder stands for the rendered drop-in in the plan; apply
// writes the file to its stage directory and fills the path in.
const DNFDropInPlaceholder = "<rendered drop-in>"

// dnfConfig plans the libdnf5 drop-in first, so every transaction that
// follows downloads with the declared settings.
func (b *builder) dnfConfig() []Operation {
	const id = "dnf:config"
	want := DNFDropIn(b.in.Root, b.in.Resolved.Constraints...)
	_, managed := b.in.Applied.Receipts[id]
	op := Operation{ID: id, Kind: KindDNFConfig, Risk: RiskLow, Paths: []string{"nimbus.toml:dnf"}}
	if !b.in.Facts.DNFDropIn.Known() {
		op.Action, op.Summary = ActionInstall, "configure DNF through "+inspect.DNFDropInPath
		op.Blocked = "the DNF drop-in cannot be read: " + b.in.Facts.DNFDropIn.Error
		return []Operation{op}
	}
	have := b.in.Facts.DNFDropIn.Value
	switch {
	case want == "" && have == "":
		return nil
	case want == "" && !managed:
		// A file Nimbus never wrote is not Nimbus's to remove.
		return nil
	case want == "":
		op.Action, op.Summary = ActionRemove, "remove "+inspect.DNFDropInPath+", no longer declared"
		op.Steps = []Step{{Description: "remove the drop-in", Argv: []string{"rm", "-f", inspect.DNFDropInPath}, Privileged: true}}
		return []Operation{op}
	case have == want && managed:
		op.Action, op.Summary = ActionKeep, inspect.DNFDropInPath+" is managed and as declared"
		return []Operation{op}
	case have == want:
		op.Action, op.Summary = ActionAdopt, "adopt "+inspect.DNFDropInPath+", already as declared"
		return []Operation{op}
	case have == "":
		op.Action, op.Summary = ActionInstall, "configure DNF through "+inspect.DNFDropInPath
	default:
		op.Action, op.Summary = ActionRepair, "rewrite "+inspect.DNFDropInPath+", which differs from the declared options"
	}
	for _, key := range slices.Sorted(maps.Keys(b.in.Root.DNF)) {
		op.Steps = append(op.Steps, Step{Description: "set " + key + "=" + dnfValue(b.in.Root.DNF[key])})
	}
	for _, c := range b.in.Resolved.Constraints {
		op.Steps = append(op.Steps, Step{Description: "exclude nonnumeric versions of " + c.Name + " through excludepkgs"})
	}
	op.Steps = append(op.Steps, Step{Description: "write the drop-in", Argv: []string{"install", "-m", "0644", DNFDropInPlaceholder, inspect.DNFDropInPath}, Privileged: true})
	return []Operation{op}
}
