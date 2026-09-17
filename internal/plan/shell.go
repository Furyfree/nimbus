package plan

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/state"
)

func (b *builder) loginShell(pendingPackages string) []Operation {
	user := b.in.Facts.User.Value.Name
	id := "login-shell:" + user
	var ops []Operation
	if shell := b.in.Resolved.Shell; shell != "" {
		op := Operation{ID: id, Kind: KindShell, Action: ActionRepair, Risk: RiskMedium,
			Paths: []string{"machine:shell"}, After: pendingPackages}
		before, err := inspect.ObserveLoginShell(b.in.Source, user)
		after := inspect.LoginShell{UID: before.UID, Shell: "/bin/" + shell}
		// Fedora's merged /usr supports both spellings; retain a matching one.
		if before.Shell == "/usr/bin/"+shell {
			after.Shell = before.Shell
		}
		op.Resource = &ResourceChange{Name: id, User: user, Before: encodeResource(before), After: encodeResource(after), Previous: encodeResource(before)}
		op.Summary = fmt.Sprintf("set %s login shell: %s -> %s", user, before.Shell, after.Shell)
		receipt, managed := b.in.Applied.Receipts[id]
		switch {
		case err != nil:
			op.Blocked = err.Error()
		case managed && !shellReceipt(receipt, id, b.in.Resolved.Machine, before.UID):
			op.Blocked = "login-shell receipt is invalid, foreign or belongs to a different account identity"
		case managed:
			op.Resource.Previous = receipt.Previous
		}
		if before == after {
			op.Action = ActionAdopt
			op.Summary = user + " already uses " + after.Shell + " as the login shell"
			if managed && receipt.Intended == op.Resource.After {
				op.Action = ActionKeep
			}
		} else {
			op.Steps = []Step{{Description: "set the local account login shell", Argv: []string{"usermod", "--shell", after.Shell, "--", user}, Privileged: true}}
			op.Notes = []string{"Takes effect after logout and login; running shells and both sets of dotfiles stay unchanged."}
		}
		if op.Blocked == "" && pendingPackages == "" {
			if err := inspect.CheckLoginShell(b.in.Source, after.Shell); err != nil {
				op.Blocked = err.Error()
			}
		}
		ops = append(ops, op)
	}
	for _, key := range slices.Sorted(maps.Keys(b.in.Applied.Receipts)) {
		r := b.in.Applied.Receipts[key]
		if r.Provider != KindShell || (key == id && b.in.Resolved.Shell != "") {
			continue
		}
		op := Operation{ID: key, Kind: KindShell, Action: ActionRetire, Risk: RiskLow,
			Summary: "stop managing " + key + "; keep the current login shell", Paths: []string{"receipt"}}
		if key != id || !strings.HasPrefix(key, "login-shell:") {
			op.Blocked = "login-shell receipt belongs to another user"
		} else if have, err := inspect.ObserveLoginShell(b.in.Source, user); err != nil {
			op.Blocked = err.Error()
		} else if !shellReceipt(r, key, b.in.Resolved.Machine, have.UID) {
			op.Blocked = "login-shell receipt is invalid, foreign or belongs to a different account identity"
		} else {
			op.Resource = &ResourceChange{Name: key, User: user, Before: encodeResource(have), After: encodeResource(have), Previous: r.Previous}
		}
		ops = append(ops, op)
	}
	return ops
}

func shellReceipt(r state.Receipt, id, machine string, uid uint64) bool {
	var intended inspect.LoginShell
	return ownedResource(r, id, KindShell, machine) && json.Unmarshal([]byte(r.Intended), &intended) == nil && intended.UID == uid && uid != 0
}
