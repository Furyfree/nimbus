package plan

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/state"
)

type FileChange struct {
	ActivationChanged bool             `json:"activation_changed,omitzero"`
	ChangedAt         time.Time        `json:"changed_at,omitempty"`
	Target            string           `json:"target"`
	Before            facts.SystemFile `json:"before"`
	After             facts.SystemFile `json:"after"`
	Previous          string           `json:"previous"`
	Recovery          bool             `json:"recovery,omitzero"`
	Triggers          []string         `json:"triggers,omitempty"`
}

type ResourceChange struct {
	Name     string `json:"name"`
	User     string `json:"user,omitempty"`
	Before   string `json:"before"`
	After    string `json:"after"`
	Previous string `json:"previous"`
	Enabled  *bool  `json:"enabled,omitempty"`
	Running  *bool  `json:"running,omitempty"`
}

func encodeResource(v any) string { data, _ := json.Marshal(v); return string(data) }
func ownedResource(r state.Receipt, id, provider, machine string) bool {
	return r.Verified && r.Resource == id && r.Provider == provider && r.Machine == machine
}
func SameFile(a, b facts.SystemFile) bool {
	return a.Exists == b.Exists && bytes.Equal(a.Content, b.Content) && a.Owner == b.Owner && a.Group == b.Group && a.Mode == b.Mode
}

func (b *builder) systemResources(earlier []Operation) []Operation {
	var ops []Operation
	selected := map[string]bool{}
	pendingPackages := ""
	for _, op := range earlier {
		if op.Kind == KindPackage && op.Action == ActionInstall {
			pendingPackages = op.ID
		}
	}
	for _, file := range b.in.Resolved.Files {
		id := "file:" + file.Target
		selected[id] = true
		op := Operation{ID: id, Kind: KindFile, Action: ActionInstall, Risk: RiskLow, Summary: "install " + file.Target, Paths: []string{"component:" + file.Component}}
		before, err := facts.ObserveFile(b.in.Source, file.Target)
		after := facts.SystemFile{Exists: true, Content: file.Content, Owner: file.Owner, Group: file.Group, Mode: file.Mode}
		receipt, managed := b.in.Applied.Receipts[id]
		op.File = &FileChange{Target: file.Target, Before: before, After: after, Previous: encodeResource(before), Recovery: file.Recovery, Triggers: file.Triggers}
		if !before.Exists {
			op.Notes = append(op.Notes, "Create missing root-owned parent directories with mode 0755; retain directories on removal.")
		}
		switch {
		case err != nil:
			op.Blocked = err.Error()
		case managed && !ownedResource(receipt, id, KindFile, b.in.Resolved.Machine):
			op.Blocked = "file ownership receipt is invalid or foreign"
		case !managed && before.Exists:
			op.Blocked = "existing file has no Nimbus ownership receipt; an explicit migration is required"
		case managed:
			op.File.Previous = receipt.Previous
			op.File.ChangedAt = receipt.ChangeTime()
			var recorded facts.SystemFile
			if err := json.Unmarshal([]byte(receipt.Intended), &recorded); err != nil {
				op.Blocked = "file receipt lacks valid intended state"
				break
			}
			op.File.ActivationChanged = !SameFile(before, recorded)
			for _, trigger := range file.Triggers {
				if !slices.Contains(receipt.Triggers, trigger) {
					op.File.ActivationChanged = true
				}
			}
			if SameFile(before, after) && receipt.Definitions.Digest == b.in.Definitions && slices.Equal(receipt.Triggers, file.Triggers) && !op.File.ActivationChanged {
				op.Action = ActionKeep
				op.Summary = file.Target + " is managed and as declared"
			} else if SameFile(before, after) {
				op.Action = ActionAdopt
				op.Summary = "accept matching managed file " + file.Target
			} else {
				op.Action = ActionRepair
				op.Summary = "repair " + file.Target
			}
		}
		if op.Action != ActionKeep && op.Action != ActionAdopt {
			op.Steps = []Step{{Description: "atomically install the reviewed file and restore its SELinux label", Argv: []string{"nimbus", "internal", "system-file", "--plan", "<plan-digest>", "--payload", "<approved-file-change>"}, Privileged: true}, {Description: "restore SELinux file context", Argv: []string{"restorecon", "--", file.Target}, Privileged: true}}
		}
		if pendingPackages != "" {
			op.After = pendingPackages
		}
		ops = append(ops, op)
	}
	for _, service := range b.in.Resolved.Services {
		id := "service:" + service.Unit
		selected[id] = true
		op := b.serviceOperation(id, service.ServiceDecl, service.Component, pendingPackages)
		ops = append(ops, op)
	}
	for _, group := range b.in.Resolved.Groups {
		user := group.User
		if user == "<user>" {
			user = b.in.Facts.User.Value.Name
		}
		id := "group:" + group.Name + ":" + user
		selected[id] = true
		op := Operation{ID: id, Kind: KindGroup, Action: ActionInstall, Risk: RiskMedium, Summary: "add " + user + " to " + group.Name, Paths: []string{"component:" + group.Component}, Notes: []string{"New group membership requires logout and login."}}
		present, err := facts.ObserveMembership(b.in.Source, user, group.Name)
		before := fmt.Sprint(present)
		op.Resource = &ResourceChange{Name: group.Name, User: user, Before: before, After: "true", Previous: before}
		receipt, managed := b.in.Applied.Receipts[id]
		switch {
		case user == "":
			op.Blocked = "invoking username is unknown"
		case err != nil:
			op.Blocked = err.Error()
		case managed && !ownedResource(receipt, id, KindGroup, b.in.Resolved.Machine):
			op.Blocked = "membership ownership receipt is invalid or foreign"
		case managed:
			op.Resource.Previous = receipt.Previous
			if present {
				op.Action = ActionKeep
			}
		case present:
			op.Action = ActionAdopt
		}
		if !present {
			op.Steps = []Step{{Description: "add supplementary membership", Argv: []string{"usermod", "--append", "--groups", group.Name, "--", user}, Privileged: true}}
		}
		if pendingPackages != "" {
			op.After = pendingPackages
		}
		ops = append(ops, op)
	}
	if target := b.in.Resolved.DefaultTarget; target != "" {
		id := "default-target"
		selected[id] = true
		op := Operation{ID: id, Kind: KindTarget, Action: ActionRepair, Risk: RiskMedium, Summary: "set default boot target to " + target, Notes: []string{"Takes effect on the next boot; the current graphical session is not stopped."}}
		out, err := b.in.Source.Run("systemctl", "get-default")
		before := strings.TrimSpace(string(out))
		op.Resource = &ResourceChange{Name: id, Before: before, After: target, Previous: before}
		receipt, managed := b.in.Applied.Receipts[id]
		switch {
		case err != nil:
			op.Blocked = err.Error()
		case before != "graphical.target" && before != "multi-user.target":
			op.Blocked = "unknown prior boot target; manual migration required"
		case managed && !ownedResource(receipt, id, KindTarget, b.in.Resolved.Machine):
			op.Blocked = "boot-target receipt is invalid or foreign"
		case managed:
			op.Resource.Previous = receipt.Previous
			if before == target {
				op.Action = ActionKeep
			}
		case before == target:
			op.Action = ActionAdopt
		}
		if before != target {
			op.Steps = []Step{{Description: "set next boot target", Argv: []string{"systemctl", "set-default", target}, Privileged: true}}
		}
		if pendingPackages != "" {
			op.After = pendingPackages
		}
		ops = append(ops, op)
	}
	// Retirement uses only verified ownership and restores the original state.
	for _, id := range slices.Sorted(maps.Keys(b.in.Applied.Receipts)) {
		receipt := b.in.Applied.Receipts[id]
		if selected[id] {
			continue
		}
		if receipt.Provider != KindFile && receipt.Provider != KindService && receipt.Provider != KindGroup && receipt.Provider != KindTarget {
			continue
		}
		op := b.retireResource(id, receipt)
		if pendingPackages != "" {
			op.After = pendingPackages
		}
		ops = append(ops, op)
	}
	// Triggers run after every changed file, once per apply. A failed trigger
	// has no successful receipt, so the next sync will retry it.
	triggers := map[string]bool{}
	for _, op := range ops {
		if op.File != nil && op.Action == ActionRemove {
			for _, id := range op.File.Triggers {
				triggers[id] = true
			}
		}
	}
	for _, file := range b.in.Resolved.Files {
		for _, id := range file.Triggers {
			triggers[id] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(triggers)) {
		triggerID := "trigger:" + id
		receipt, ok := b.in.Applied.Receipts[triggerID]
		changed := !ok || !ownedResource(receipt, triggerID, KindTrigger, b.in.Resolved.Machine)
		for _, file := range b.in.Resolved.Files {
			for _, requested := range file.Triggers {
				if requested == id {
					if recorded, ok := b.in.Applied.Receipts["file:"+file.Target]; ok && (recorded.ChangeTime().After(receipt.Timestamp) || !slices.Contains(recorded.Triggers, id)) {
						changed = true
					}
				}
			}
		}
		for _, op := range ops {
			if op.Kind == KindFile && op.Action != ActionKeep && (op.Action != ActionAdopt || (op.File != nil && op.File.ActivationChanged)) {
				if op.File != nil {
					for _, requested := range op.File.Triggers {
						if requested == id {
							changed = true
						}
					}
				}
				for _, file := range b.in.Resolved.Files {
					if op.ID == "file:"+file.Target {
						for _, requested := range file.Triggers {
							if requested == id {
								changed = true
							}
						}
					}
				}
			}
		}
		if changed {
			op := Operation{ID: triggerID, Kind: KindTrigger, Action: ActionRepair, Risk: RiskMedium, Summary: "run trigger " + id, After: pendingPackages, Steps: []Step{{Description: "run fixed trigger", Argv: definitions.TriggerArgs(id), Privileged: true}}}
			if id == "noctalia-state-directory" {
				before, err := facts.ObserveDirectory(b.in.Source, "/var/lib/noctalia-greeter")
				desired := facts.Directory{Exists: true, Owner: "greetd", Group: "greetd", Mode: "0750"}
				op.Resource = &ResourceChange{Name: "/var/lib/noctalia-greeter", Before: encodeResource(before), After: encodeResource(desired), Previous: encodeResource(before)}
				if err != nil {
					op.Blocked = err.Error()
				} else if before.Exists && before != desired {
					op.Blocked = "existing greeter state directory has foreign ownership or mode; explicit migration required"
				}
			}
			ops = append(ops, op)
		}
	}
	// Files and memberships precede service activation; reload definitions first.
	slices.SortStableFunc(ops, func(a, b Operation) int {
		rank := func(op Operation) int {
			if op.Action == ActionRemove {
				switch op.Kind {
				case KindTarget:
					return -4
				case KindService:
					return -3
				case KindGroup:
					return -2
				case KindFile:
					return -1
				}
			}
			switch op.Kind {
			case KindFile:
				return 0
			case KindGroup:
				return 1
			case KindTrigger:
				return 2
			case KindService:
				return 3
			default:
				return 4
			}
		}
		return cmp.Compare(rank(a), rank(b))
	})
	return ops
}

func (b *builder) serviceOperation(id string, want definitions.ServiceDecl, component, pending string) Operation {
	op := Operation{ID: id, Kind: KindService, Action: ActionRepair, Risk: RiskMedium, Summary: "configure " + want.Unit, Paths: []string{"component:" + component}}
	have, err := facts.ObserveService(b.in.Source, want.Unit)
	op.Resource = &ResourceChange{Name: want.Unit, Before: encodeResource(have), After: encodeResource(want), Previous: encodeResource(have), Enabled: want.Enabled, Running: want.Running}
	receipt, managed := b.in.Applied.Receipts[id]
	switch {
	case err != nil:
		op.Blocked = err.Error()
	case have.Load == "not-found" && pending != "":
		op.After = pending
	case have.Load != "loaded":
		op.Blocked = "unit is not loaded: " + have.Load
	case have.Enabled == "masked" || have.Enabled == "masked-runtime":
		op.Blocked = "masked unit requires explicit manual migration"
	case managed && !ownedResource(receipt, id, KindService, b.in.Resolved.Machine):
		op.Blocked = "service receipt is invalid or foreign"
	}
	if managed {
		op.Resource.Previous = receipt.Previous
	}
	if want.Enabled != nil {
		enabled := have.Enabled == "enabled"
		if have.Enabled != "enabled" && have.Enabled != "disabled" && have.Load != "not-found" {
			op.Blocked = "unsupported unit enablement state " + have.Enabled
		}
		if enabled != *want.Enabled {
			verb := "disable"
			if *want.Enabled {
				verb = "enable"
			}
			op.Steps = append(op.Steps, Step{Description: verb + " unit", Argv: []string{"systemctl", verb, "--", want.Unit}, Privileged: true})
		}
	}
	if want.Running != nil && (have.Active == "active") != *want.Running {
		verb := "stop"
		if *want.Running {
			verb = "start"
		}
		op.Steps = append(op.Steps, Step{Description: verb + " unit", Argv: []string{"systemctl", verb, "--", want.Unit}, Privileged: true})
	}
	if len(op.Steps) == 0 {
		if managed {
			op.Action = ActionKeep
		} else {
			op.Action = ActionAdopt
		}
	}
	if want.Unit == "greetd.service" && want.Enabled != nil && *want.Enabled {
		names, e := b.in.Source.ReadDir("/etc/systemd/system")
		if e != nil {
			op.Blocked = "cannot inspect display-manager ownership: " + e.Error()
		} else {
			for _, name := range names {
				if name == "display-manager.service" {
					out, e := b.in.Source.Run("readlink", "--", "/etc/systemd/system/display-manager.service")
					if e != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "/greetd.service") {
						op.Blocked = "another display manager owns display-manager.service; explicit migration is required"
					}
				}
			}
		}
	}
	if pending != "" {
		op.After = pending
	}
	return op
}

func (b *builder) retireResource(id string, receipt state.Receipt) Operation {
	op := Operation{ID: id, Kind: receipt.Provider, Action: ActionRemove, Risk: RiskMedium, Summary: "restore prior state of " + id}
	if !ownedResource(receipt, id, receipt.Provider, b.in.Resolved.Machine) {
		op.Blocked = "resource ownership is invalid or foreign"
		return op
	}
	switch receipt.Provider {
	case KindFile:
		target := strings.TrimPrefix(id, "file:")
		before, err := facts.ObserveFile(b.in.Source, target)
		var intended, previous facts.SystemFile
		if err != nil {
			op.Blocked = err.Error()
		} else if json.Unmarshal([]byte(receipt.Intended), &intended) != nil || json.Unmarshal([]byte(receipt.Previous), &previous) != nil {
			op.Blocked = "file receipt lacks valid recovery state"
		} else if !SameFile(before, intended) && (before.Exists || previous.Exists) {
			op.Blocked = "managed file changed since the last receipt; accept or restore it before removal"
		}
		op.File = &FileChange{Target: target, Before: before, After: previous, Previous: receipt.Previous, Recovery: definitions.RecoveryTarget(target), Triggers: receipt.Triggers}
		if op.File.Triggers != nil {
			kept := []string{}
			for _, trigger := range op.File.Triggers {
				if trigger == "noctalia-state-directory" {
					op.Notes = append(op.Notes, "Retain /var/lib/noctalia-greeter and its runtime data; removal deletes only the owned tmpfiles rule.")
				} else {
					kept = append(kept, trigger)
				}
			}
			op.File.Triggers = kept
		}
		op.Steps = []Step{{Description: "atomically restore the reviewed prior file state", Argv: []string{"nimbus", "internal", "system-file", "--plan", "<plan-digest>", "--payload", "<approved-file-change>"}, Privileged: true}}
	case KindService:
		unit := strings.TrimPrefix(id, "service:")
		current, inspectErr := facts.ObserveService(b.in.Source, unit)
		if inspectErr != nil {
			op.Blocked = inspectErr.Error()
			break
		}
		if current.Load == "not-found" {
			if current.Active != "inactive" {
				op.Blocked = "unit definition is absent but the unit is not inactive; inspect it before retiring ownership"
				break
			}
			op.Action = ActionRetire
			op.Summary = "retire ownership of absent unit " + unit
			op.Resource = &ResourceChange{Name: unit, Before: encodeResource(current), After: encodeResource(current), Previous: receipt.Previous}
			break
		}
		var previous facts.Service
		if json.Unmarshal([]byte(receipt.Previous), &previous) != nil || previous.Unit != unit || previous.Load != "loaded" {
			op.Blocked = "service receipt lacks supported recovery state"
			break
		}
		desired := definitions.ServiceDecl{Unit: unit}
		var intended definitions.ServiceDecl
		if json.Unmarshal([]byte(receipt.Intended), &intended) != nil {
			op.Blocked = "service receipt lacks desired state"
			break
		}
		if intended.Enabled != nil {
			if previous.Enabled != "enabled" && previous.Enabled != "disabled" {
				op.Blocked = "unsupported prior enablement state"
				break
			}
			desired.Enabled = new(previous.Enabled == "enabled")
		}
		if intended.Running != nil {
			desired.Running = new(previous.Active == "active")
		}
		op = b.serviceOperation(id, desired, "retired", "")
		op.Action = ActionRemove
		op.Summary = "restore prior service state of " + unit
		if unit == "greetd.service" {
			var current facts.Service
			_ = json.Unmarshal([]byte(op.Resource.Before), &current)
			if current.Active == "active" {
				op.Blocked = "greetd is active; move to a console and stop it explicitly before removing desktop integration"
			}
		}
	case KindGroup:
		fields := strings.Split(id, ":")
		if len(fields) != 3 || (receipt.Previous != "true" && receipt.Previous != "false") {
			op.Blocked = "invalid membership recovery state"
			break
		}
		present, err := facts.ObserveMembership(b.in.Source, fields[2], fields[1])
		if err != nil {
			op.Blocked = err.Error()
		}
		op.Resource = &ResourceChange{Name: fields[1], User: fields[2], Before: fmt.Sprint(present), After: receipt.Previous, Previous: receipt.Previous}
		if present && receipt.Previous == "false" {
			primary, e := b.in.Source.Run("id", "-gn", "--", fields[2])
			if e != nil {
				op.Blocked = "cannot establish primary membership: " + e.Error()
			} else if strings.TrimSpace(string(primary)) == fields[1] {
				op.Blocked = "membership is now the primary group; removal is forbidden"
			}
			op.Steps = []Step{{Description: "remove Nimbus-added membership", Argv: []string{"gpasswd", "--delete", fields[2], fields[1]}, Privileged: true}}
		}
	case KindTarget:
		if receipt.Previous != "graphical.target" && receipt.Previous != "multi-user.target" {
			op.Blocked = "invalid boot target recovery state"
			break
		}
		out, err := b.in.Source.Run("systemctl", "get-default")
		if err != nil {
			op.Blocked = err.Error()
		}
		before := strings.TrimSpace(string(out))
		if before != receipt.Intended {
			op.Blocked = "boot target changed since last receipt"
		}
		op.Resource = &ResourceChange{Name: id, Before: before, After: receipt.Previous, Previous: receipt.Previous}
		op.Steps = []Step{{Description: "restore prior default target", Argv: []string{"systemctl", "set-default", receipt.Previous}, Privileged: true}}
	}
	return op
}

func deferPackageRemovalForResources(ops []Operation) {
	pending := ""
	for _, op := range ops {
		if op.After != "" && op.Action == ActionRemove && (op.Kind == KindFile || op.Kind == KindService || op.Kind == KindGroup || op.Kind == KindTarget) {
			pending = op.ID
			break
		}
	}
	if pending == "" {
		return
	}
	for i := range ops {
		if ops[i].Kind == KindPackage && (ops[i].Action == ActionRemove || ops[i].Action == ActionPrune) {
			ops[i].After = pending
		}
	}
}
