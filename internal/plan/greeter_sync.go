package plan

import (
	"encoding/json"
	"maps"
	"slices"

	"github.com/Furyfree/nimbus/internal/inspect"
)

func (b *builder) greeterSync(pendingPackages string) []Operation {
	user := b.in.Facts.User.Value.Name
	id := KindGreeterSync + ":" + user
	var ops []Operation
	if owner := b.in.Resolved.GreeterPasswordlessSync; owner != "" {
		op := Operation{ID: id, Kind: KindGreeterSync, Action: ActionRepair, Risk: RiskMedium,
			Summary: "verify greeter appearance-sync authorization for " + user + "; enable if missing",
			Paths:   []string{"component:" + owner}, After: pendingPackages}
		account, err := inspect.ObserveLoginShell(b.in.Source, user)
		before := inspect.GreeterSync{UID: account.UID}
		if err == nil && pendingPackages == "" {
			err = inspect.CheckGreeterSync(b.in.Source)
			if err == nil {
				before, err = inspect.ObserveGreeterSync(b.in.Source, user, false)
			}
		}
		after := inspect.GreeterSync{UID: account.UID, Known: true, Enabled: true}
		op.Resource = &ResourceChange{Name: id, User: user, Before: encodeResource(before), After: encodeResource(after), Previous: encodeResource(before)}
		if err != nil {
			op.Blocked = err.Error()
		}
		if receipt, exists := b.in.Applied.Receipts[id]; exists {
			var intended inspect.GreeterSync
			if !ownedResource(receipt, id, KindGreeterSync, b.in.Resolved.Machine) || json.Unmarshal([]byte(receipt.Intended), &intended) != nil || intended != after {
				op.Blocked = "greeter authorization receipt is invalid, foreign or belongs to a different account identity"
			} else {
				op.Resource.Previous = receipt.Previous
				if before == after {
					op.Action = ActionKeep
				}
			}
		}
		if before == after && op.Action != ActionKeep {
			op.Action = ActionAdopt
		}
		// Even a prior receipt cannot replace inspection of root-owned policy.
		// Recheck at apply so an administrator's subsequent removal is detected.
		op.Steps = []Step{
			{Description: "check native authorization with administrator access", Argv: []string{inspect.GreeterBinary, "passwordless-sync", "status", user}, Privileged: true},
			{Description: "only if missing: authorize constrained appearance sync, then verify native status", Argv: []string{inspect.GreeterBinary, "passwordless-sync", "enable", user}, Privileged: true},
		}
		if before == after {
			op.Summary = "greeter appearance-sync authorization for " + user + " is enabled"
			op.Steps = nil
		}
		op.Notes = []string{"Noctalia owns its Polkit rule; other users and administrator rules are preserved. Only secure appearance sync is authorized.", "Deselection retires Nimbus tracking and retains native authorization; revoke explicitly with the greeter's passwordless-sync disable command."}
		ops = append(ops, op)
	}
	for _, key := range slices.Sorted(maps.Keys(b.in.Applied.Receipts)) {
		r := b.in.Applied.Receipts[key]
		if r.Provider != KindGreeterSync || (key == id && b.in.Resolved.GreeterPasswordlessSync != "") {
			continue
		}
		op := Operation{ID: key, Kind: KindGreeterSync, Action: ActionRetire, Risk: RiskLow,
			Summary: "retire greeter authorization tracking; retain native authorization", Paths: []string{"receipt"}}
		account, err := inspect.ObserveLoginShell(b.in.Source, user)
		var intended inspect.GreeterSync
		if err != nil || key != id || !ownedResource(r, key, KindGreeterSync, b.in.Resolved.Machine) || json.Unmarshal([]byte(r.Intended), &intended) != nil || intended.UID != account.UID || intended.UID == 0 || !intended.Known || !intended.Enabled {
			op.Blocked = "greeter authorization receipt is invalid or belongs to another account"
		}
		op.Resource = &ResourceChange{Name: key, User: user, Before: encodeResource(intended), After: encodeResource(intended), Previous: r.Previous}
		ops = append(ops, op)
	}
	return ops
}
