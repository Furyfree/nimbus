package plan

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/state"
)

func resourceBuilder() (*builder, *facts.FakeSource) {
	src := &facts.FakeSource{Commands: map[string][]byte{}, Files: map[string][]byte{}, Dirs: map[string][]string{"/": {"etc"}, "/etc": {}, "/etc/systemd/system": {}}, Failures: map[string]string{}}
	src.Commands[facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc")] = []byte("directory|root|root|755|1")
	b := &builder{in: Inputs{Resolved: &definitions.Resolved{Machine: "vm"}, Facts: &facts.Facts{User: facts.Section[facts.User]{Value: facts.User{Name: "owner"}}}, Source: src, Definitions: "new", Applied: &state.Applied{Receipts: map[string]state.Receipt{}}}}
	return b, src
}
func answerUnit(src *facts.FakeSource, unit, enabled, active string) {
	src.Commands[facts.Key("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", unit)] = []byte("LoadState=loaded\nUnitFileState=" + enabled + "\nActiveState=" + active + "\n")
}
func fileReceipt(target string, value facts.SystemFile) state.Receipt {
	return state.Receipt{Resource: "file:" + target, Provider: KindFile, Machine: "vm", Verified: true, Previous: encodeResource(facts.SystemFile{}), Intended: encodeResource(value)}
}
func answerFile(src *facts.FakeSource, target string, value facts.SystemFile) {
	src.Dirs["/etc"] = []string{"nimbus.conf"}
	src.Files[target] = value.Content
	src.Commands[facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", target)] = []byte("regular file|" + value.Owner + "|" + value.Group + "|644|1")
}

func TestSystemFilesRequireOwnershipAndBindFullPayload(t *testing.T) {
	b, src := resourceBuilder()
	target := "/etc/nimbus.conf"
	b.in.Resolved.Files = []definitions.ResolvedFile{{Target: target, Content: []byte("desired\n"), Owner: "root", Group: "root", Mode: "0644"}}
	ops := b.systemResources(nil)
	if len(ops) != 1 || ops[0].Blocked != "" || string(ops[0].File.After.Content) != "desired\n" {
		t.Fatalf("fresh file: %+v", ops)
	}
	have := facts.SystemFile{Exists: true, Content: []byte("old\n"), Owner: "root", Group: "root", Mode: "0644"}
	answerFile(src, target, have)
	if op := b.systemResources(nil)[0]; op.Blocked == "" {
		t.Fatal("foreign file was adopted")
	}
	b.in.Applied.Receipts["file:"+target] = fileReceipt(target, have)
	if op := b.systemResources(nil)[0]; op.Blocked != "" || op.Action != ActionRepair {
		t.Fatalf("managed repair: %+v", op)
	}
	b.in.Resolved.Files = nil
	have.Content = []byte("drift\n")
	answerFile(src, target, have)
	if op := b.systemResources(nil)[0]; op.Blocked == "" {
		t.Fatal("drifted file removal was allowed")
	}
}

func TestFileChangeTimeJSONPreservesPlanDigest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changed time.Time
	}{
		{"zero", time.Time{}},
		{"nonzero", time.Unix(5, 0).UTC()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, src := resourceBuilder()
			target := "/etc/nimbus.conf"
			have := facts.SystemFile{Exists: true, Content: []byte("same"), Owner: "root", Group: "root", Mode: "0644"}
			answerFile(src, target, have)
			b.in.Resolved.Files = []definitions.ResolvedFile{{Target: target, Content: have.Content, Owner: have.Owner, Group: have.Group, Mode: have.Mode}}
			r := fileReceipt(target, have)
			r.Operation, r.ChangedAt = ActionAdopt, tc.changed
			b.in.Applied.Receipts[r.Resource] = r
			p := &Plan{Machine: b.in.Resolved.Machine, Definitions: b.in.Definitions, Operations: b.systemResources(nil)}
			var err error
			p.Digest, err = digest(p)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if present := strings.Contains(string(data), `"changed_at"`); present == tc.changed.IsZero() {
				t.Fatalf("plan changed_at presence=%t for %s: %s", present, tc.changed, data)
			}
			var decoded Plan
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			roundTrip, err := digest(&decoded)
			if err != nil {
				t.Fatal(err)
			}
			if roundTrip != p.Digest || !decoded.Operations[0].File.ChangedAt.Equal(tc.changed) {
				t.Fatalf("plan lost its change time or digest after JSON round trip: %+v", decoded)
			}
			decoded.Operations[0].File.ChangedAt = tc.changed.Add(time.Second)
			changed, err := digest(&decoded)
			if err != nil {
				t.Fatal(err)
			}
			if changed == p.Digest {
				t.Fatal("plan digest does not bind file change time")
			}
		})
	}
}

func TestBuildRejectsUnencodableReceiptChangeTime(t *testing.T) {
	for _, field := range []string{"changed_at", "timestamp"} {
		t.Run(field, func(t *testing.T) {
			b, src := resourceBuilder()
			target := "/etc/nimbus.conf"
			have := facts.SystemFile{Exists: true, Content: []byte("observed"), Owner: "root", Group: "root", Mode: "0644"}
			answerFile(src, target, have)
			r := fileReceipt(target, have)
			r.Operation = ActionRepair
			// Older or externally written receipts can decode a time whose
			// offset JSON encoding rejects. Both change-time sources matter.
			if err := json.Unmarshal([]byte(`{"`+field+`":"2026-09-09T12:00:00+24:00"}`), &r); err != nil {
				t.Fatal(err)
			}
			b.in.Applied.Receipts[r.Resource] = r
			for _, content := range []string{"reviewed change", "different change"} {
				b.in.Resolved.Files = []definitions.ResolvedFile{{Target: target, Content: []byte(content), Owner: have.Owner, Group: have.Group, Mode: have.Mode}}
				p, err := Build(b.in)
				if p != nil || err == nil || !strings.Contains(err.Error(), "encode plan digest") {
					t.Fatalf("unencodable receipt produced plan %+v, error %v", p, err)
				}
				if _, ok := errors.AsType[*json.MarshalerError](err); !ok {
					t.Fatalf("encoding failure was not preserved: %v", err)
				}
			}
		})
	}
}

func TestServiceEnablementDoesNotStartGreeterAndRejectsForeignManager(t *testing.T) {
	b, src := resourceBuilder()
	answerUnit(src, "greetd.service", "disabled", "inactive")
	want := definitions.ServiceDecl{Unit: "greetd.service", Enabled: new(true)}
	op := b.serviceOperation("service:greetd.service", want, "desktop", "")
	if op.Blocked != "" || len(op.Steps) != 1 || op.Steps[0].Argv[1] != "enable" {
		t.Fatalf("enable only: %+v", op)
	}
	src.Dirs["/etc/systemd/system"] = []string{"display-manager.service"}
	src.Commands[facts.Key("readlink", "--", "/etc/systemd/system/display-manager.service")] = []byte("/usr/lib/systemd/system/gdm.service\n")
	if op := b.serviceOperation("service:greetd.service", want, "desktop", ""); op.Blocked == "" {
		t.Fatal("foreign display manager takeover allowed")
	}
}

func TestMembershipRemovalPreservesPreexistingAndPrimaryGroups(t *testing.T) {
	b, src := resourceBuilder()
	src.Commands[facts.Key("id", "-nG", "--", "owner")] = []byte("owner docker")
	receipt := state.Receipt{Resource: "group:docker:owner", Provider: KindGroup, Machine: "vm", Verified: true, Previous: "true", Intended: "true"}
	if op := b.retireResource(receipt.Resource, receipt); op.Blocked != "" || len(op.Steps) != 0 {
		t.Fatalf("preexisting membership: %+v", op)
	}
	receipt.Previous = "false"
	src.Commands[facts.Key("id", "-gn", "--", "owner")] = []byte("docker")
	if op := b.retireResource(receipt.Resource, receipt); op.Blocked == "" {
		t.Fatal("primary group removal allowed")
	}
}

func TestTriggerRetriesAfterFileReceiptAndDeduplicates(t *testing.T) {
	b, src := resourceBuilder()
	target := "/etc/nimbus.conf"
	have := facts.SystemFile{Exists: true, Content: []byte("same"), Owner: "root", Group: "root", Mode: "0644"}
	answerFile(src, target, have)
	b.in.Resolved.Files = []definitions.ResolvedFile{{Target: target, Content: have.Content, Owner: have.Owner, Group: have.Group, Mode: have.Mode, Triggers: []string{"systemd-daemon-reload", "systemd-daemon-reload"}}}
	receipt := fileReceipt(target, have)
	receipt.Definitions.Digest = "new"
	receipt.Timestamp = time.Unix(20, 0)
	b.in.Applied.Receipts[receipt.Resource] = receipt
	b.in.Applied.Receipts["trigger:systemd-daemon-reload"] = state.Receipt{Resource: "trigger:systemd-daemon-reload", Provider: KindTrigger, Machine: "vm", Verified: true, Definitions: state.Definitions{Digest: "new"}, Timestamp: time.Unix(10, 0)}
	ops := b.systemResources(nil)
	count := 0
	for _, op := range ops {
		if op.Kind == KindTrigger {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one retry, got %+v", ops)
	}
}

func TestRetirementWaitsForReplanBeforePackageRemoval(t *testing.T) {
	b, src := resourceBuilder()
	answerUnit(src, "demo.service", "enabled", "inactive")
	previous := facts.Service{Unit: "demo.service", Load: "loaded", Enabled: "disabled", Active: "inactive"}
	b.in.Applied.Receipts["service:demo.service"] = state.Receipt{Resource: "service:demo.service", Provider: KindService, Machine: "vm", Verified: true, Previous: encodeResource(previous), Intended: encodeResource(definitions.ServiceDecl{Unit: "demo.service", Enabled: new(true)})}
	ops := b.systemResources([]Operation{{ID: "packages:install", Kind: KindPackage, Action: ActionInstall}})
	if len(ops) != 1 || ops[0].After != "packages:install" {
		t.Fatalf("retirement must wait for package state: %+v", ops)
	}
	ops = append(ops, Operation{ID: "packages:remove", Kind: KindPackage, Action: ActionRemove})
	deferPackageRemovalForResources(ops)
	if ops[1].After != "service:demo.service" {
		t.Fatalf("package removal bypassed pending unit restoration: %+v", ops)
	}
}

func TestDefinitionOnlyFileAdoptionDoesNotRestartServices(t *testing.T) {
	b, src := resourceBuilder()
	target := "/etc/nimbus.conf"
	have := facts.SystemFile{Exists: true, Content: []byte("same"), Owner: "root", Group: "root", Mode: "0644"}
	answerFile(src, target, have)
	b.in.Resolved.Files = []definitions.ResolvedFile{{Target: target, Content: have.Content, Owner: have.Owner, Group: have.Group, Mode: have.Mode, Triggers: []string{"docker-restart"}}}
	file := fileReceipt(target, have)
	file.Operation = ActionAdopt
	file.Definitions.Digest = "older-definitions"
	file.Timestamp = time.Unix(30, 0)
	file.ChangedAt = time.Unix(5, 0)
	file.Triggers = []string{"docker-restart"}
	b.in.Applied.Receipts[file.Resource] = file
	b.in.Applied.Receipts["trigger:docker-restart"] = state.Receipt{Resource: "trigger:docker-restart", Provider: KindTrigger, Machine: "vm", Verified: true, Definitions: state.Definitions{Digest: "older-definitions"}, Timestamp: time.Unix(10, 0)}
	ops := b.systemResources(nil)
	if len(ops) != 1 || ops[0].Action != ActionAdopt || !ops[0].File.ChangedAt.Equal(file.ChangedAt) {
		t.Fatalf("definition-only change restarted a service or lost mutation time: %+v", ops)
	}
	file.ChangedAt = time.Unix(20, 0)
	b.in.Applied.Receipts[file.Resource] = file
	ops = b.systemResources(nil)
	if len(ops) != 2 || ops[1].ID != "trigger:docker-restart" {
		t.Fatalf("failed trigger was not retried after real file mutation: %+v", ops)
	}
}

func TestMissingDeselectedUnitRetiresReceiptWithoutRecreatingIt(t *testing.T) {
	b, src := resourceBuilder()
	unit := "demo.service"
	src.Commands[facts.Key("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", unit)] = []byte("LoadState=not-found\nUnitFileState=\nActiveState=inactive\n")
	receipt := state.Receipt{Resource: "service:" + unit, Provider: KindService, Machine: "vm", Verified: true}
	op := b.retireResource(receipt.Resource, receipt)
	if op.Action != ActionRetire || op.Blocked != "" || len(op.Steps) != 0 {
		t.Fatalf("absent unit recreated or blocked: %+v", op)
	}
	src.Commands[facts.Key("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", unit)] = []byte("LoadState=not-found\nUnitFileState=\nActiveState=active\n")
	if op := b.retireResource(receipt.Resource, receipt); op.Blocked == "" {
		t.Fatal("running unit lost its ownership receipt")
	}
}

func TestAdoptedManualChangeAndNewTriggerAssociationRetryActivation(t *testing.T) {
	for _, reason := range []string{"manual-file-change", "new-trigger-association"} {
		t.Run(reason, func(t *testing.T) {
			b, src := resourceBuilder()
			target := "/etc/nimbus.conf"
			have := facts.SystemFile{Exists: true, Content: []byte("current"), Owner: "root", Group: "root", Mode: "0644"}
			answerFile(src, target, have)
			b.in.Resolved.Files = []definitions.ResolvedFile{{Target: target, Content: have.Content, Owner: have.Owner, Group: have.Group, Mode: have.Mode, Triggers: []string{"docker-restart"}}}
			file := fileReceipt(target, have)
			file.Definitions.Digest = "old"
			file.Operation = ActionAdopt
			file.ChangedAt = time.Unix(5, 0)
			file.Triggers = []string{"docker-restart"}
			if reason == "manual-file-change" {
				previous := have
				previous.Content = []byte("previous")
				file.Intended = encodeResource(previous)
			} else {
				file.Triggers = nil
			}
			b.in.Applied.Receipts[file.Resource] = file
			trigger := state.Receipt{Resource: "trigger:docker-restart", Provider: KindTrigger, Machine: "vm", Verified: true, Timestamp: time.Unix(10, 0)}
			b.in.Applied.Receipts[trigger.Resource] = trigger
			ops := b.systemResources(nil)
			if len(ops) != 2 || ops[0].Action != ActionAdopt || !ops[0].File.ActivationChanged || ops[1].ID != trigger.Resource {
				t.Fatalf("activation missing: %+v", ops)
			}
			// Adoption succeeds but its trigger fails. The persisted activation time
			// must continue to schedule the trigger even after the association is saved.
			file.Intended = encodeResource(have)
			file.Definitions.Digest = b.in.Definitions
			file.Triggers = []string{"docker-restart"}
			file.ChangedAt = time.Unix(20, 0)
			file.Timestamp = time.Unix(20, 0)
			b.in.Applied.Receipts[file.Resource] = file
			ops = b.systemResources(nil)
			if len(ops) != 2 || ops[0].Action != ActionKeep || ops[1].ID != trigger.Resource {
				t.Fatalf("failed activation retry missing: %+v", ops)
			}
			trigger.Timestamp = time.Unix(30, 0)
			b.in.Applied.Receipts[trigger.Resource] = trigger
			ops = b.systemResources(nil)
			if len(ops) != 1 || ops[0].Action != ActionKeep {
				t.Fatalf("successful activation repeats: %+v", ops)
			}
		})
	}
}
