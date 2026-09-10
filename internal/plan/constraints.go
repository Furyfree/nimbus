package plan

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

// IsConstraintOperation identifies the fixed generated native version-lock file.
func IsConstraintOperation(op Operation) bool {
	return op.Kind == KindFile && op.File != nil && op.File.Target == definitions.VersionlockPath
}

func (b *builder) constraintOperations() []Operation {
	const id = "file:" + definitions.VersionlockPath
	fileIndex := slices.IndexFunc(b.in.Resolved.Files, func(f definitions.ResolvedFile) bool { return f.Target == definitions.VersionlockPath })
	receipt, managed := b.in.Applied.Receipts[id]
	if fileIndex < 0 {
		if !managed {
			return nil
		}
		op := b.retireResource(id, receipt)
		b.repoChanges = append(b.repoChanges, id)
		return []Operation{op}
	}
	file := b.in.Resolved.Files[fileIndex]
	before, err := inspect.ObserveFile(b.in.Source, file.Target)
	after := inspect.SystemFile{Exists: true, Content: file.Content, Owner: file.Owner, Group: file.Group, Mode: file.Mode}
	op := Operation{ID: id, Kind: KindFile, Action: ActionInstall, Risk: RiskMedium, Summary: "configure native package version constraints", Paths: []string{"machine:package_constraints"}, File: &FileChange{Target: file.Target, Before: before, After: after, Previous: encodeResource(before)}}
	for _, c := range b.in.Resolved.Constraints {
		op.Notes = append(op.Notes, c.Package+": allow stable "+c.Family+" at RPM epoch 0; include packaging revisions")
		for _, p := range b.in.Facts.Packages.Value {
			if p.Name == c.Name && !c.Matches(p.EVR()) {
				op.Notes = append(op.Notes, fmt.Sprintf("Installed %s %s is outside %s; system upgrade must reach the selected family, and no implicit downgrade is allowed.", p.Name, p.EVR(), c.Family))
			}
		}
	}
	switch {
	case err != nil:
		op.Blocked = err.Error()
	case managed && !ownedResource(receipt, id, KindFile, b.in.Resolved.Machine):
		op.Blocked = "version-lock ownership receipt is invalid or foreign"
	case !managed && before.Exists:
		op.Blocked = "existing version-lock file is not owned by Nimbus; preserve it and explicitly migrate its rules before enabling constraints"
	case managed:
		var recorded, previous inspect.SystemFile
		op.File.Previous = receipt.Previous
		if json.Unmarshal([]byte(receipt.Intended), &recorded) != nil || json.Unmarshal([]byte(receipt.Previous), &previous) != nil {
			op.Blocked = "version-lock receipt lacks intended or recovery state"
		} else if before.Exists && !SameFile(before, recorded) {
			op.Blocked = "version-lock file changed outside Nimbus; preserve and review foreign rules before continuing"
		} else if SameFile(before, after) {
			op.Action = ActionKeep
		} else {
			op.Action = ActionRepair
		}
	}
	if op.Action != ActionKeep {
		op.Steps = []Step{{Description: "atomically install the reviewed version-lock file", Argv: []string{"nimbus", "internal", "system-file", "--plan", "<plan-digest>", "--payload", "<approved-file-change>"}, Privileged: true}, {Description: "restore SELinux file context", Argv: []string{"restorecon", "--", file.Target}, Privileged: true}}
		b.repoChanges = append(b.repoChanges, id)
	}
	return []Operation{op}
}

func (b *builder) constraintUpdates() Updates {
	if len(b.repoChanges) > 0 {
		return Updates{Unavailable: "native update preview waits for package sources and version constraints"}
	}
	out, err := b.in.Source.Run("dnf5", "--cacheonly", "--assumeno", "upgrade")
	tx, parseErr := ParsePreview(out)
	if parseErr != nil {
		return Updates{Unavailable: parseErr.Error()}
	}
	if err != nil && !tx.NothingToDo && len(tx.Packages) == 0 {
		return Updates{Unavailable: err.Error()}
	}
	u := Updates{Available: []Upgrade{}}
	for _, p := range tx.Packages {
		if strings.HasPrefix(p.Section, "upgrading") {
			u.Available = append(u.Available, Upgrade{Name: p.Name, Arch: p.Arch, EVR: p.EVR, Repository: p.Repository})
		}
	}
	return u
}
