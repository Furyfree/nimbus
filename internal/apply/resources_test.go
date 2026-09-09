package apply

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func encodeTest(v any) string { data, _ := json.Marshal(v); return string(data) }
func resourceExecutor(src facts.Source) *executor {
	return &executor{p: &plan.Plan{Machine: "vm", Digest: "approved"}, opts: Options{Source: src, Now: func() time.Time { return time.Unix(20, 0) }, Out: io.Discard}}
}

func TestServiceNoopMutationNeverGetsSuccessReceipt(t *testing.T) {
	before := facts.Service{Unit: "demo.service", Load: "loaded", Enabled: "disabled", Active: "inactive"}
	src := &facts.FakeSource{Commands: map[string][]byte{
		facts.Key("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", "demo.service"): []byte("LoadState=loaded\nUnitFileState=disabled\nActiveState=inactive\n"),
		facts.Key("sudo", "systemctl", "enable", "--", "demo.service"):                                         nil,
	}}
	op := plan.Operation{ID: "service:demo.service", Kind: plan.KindService, Action: plan.ActionRepair, Resource: &plan.ResourceChange{Name: before.Unit, Before: encodeTest(before), After: encodeTest(definitions.ServiceDecl{Unit: before.Unit, Enabled: new(true)}), Previous: encodeTest(before), Enabled: new(true)}, Steps: []plan.Step{{Argv: []string{"systemctl", "enable", "--", before.Unit}}}}
	receipts, _, err := resourceExecutor(src).systemResource(op)
	if err == nil || len(receipts) != 0 {
		t.Fatalf("no-effect enable: receipts=%v err=%v", receipts, err)
	}
}

func TestServiceAdoptionRecordsOriginalState(t *testing.T) {
	before := facts.Service{Unit: "demo.service", Load: "loaded", Enabled: "enabled", Active: "inactive"}
	src := &facts.FakeSource{Commands: map[string][]byte{facts.Key("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", before.Unit): []byte("LoadState=loaded\nUnitFileState=enabled\nActiveState=inactive\n")}}
	op := plan.Operation{ID: "service:demo.service", Kind: plan.KindService, Action: plan.ActionAdopt, Resource: &plan.ResourceChange{Name: before.Unit, Before: encodeTest(before), After: encodeTest(definitions.ServiceDecl{Unit: before.Unit, Enabled: new(true)}), Previous: encodeTest(before), Enabled: new(true)}}
	receipts, _, err := resourceExecutor(src).systemResource(op)
	if err != nil || len(receipts) != 1 || receipts[0].Previous != encodeTest(before) || !receipts[0].Verified {
		t.Fatalf("adoption: %v %v", receipts, err)
	}
}

func TestGreeterDirectoryTriggerVerifiesTypeAndOwnership(t *testing.T) {
	for _, observation := range []string{"symbolic link|greetd|greetd|750|1", "directory|root|root|750|2", "directory|greetd|greetd|755|2", "directory|greetd|greetd|750|2"} {
		t.Run(observation, func(t *testing.T) {
			src := &facts.FakeSource{Commands: map[string][]byte{
				facts.Key("sudo", "systemd-tmpfiles", "--create", "/etc/tmpfiles.d/nimbus-noctalia-greeter.conf"): nil,
				facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/var/lib/noctalia-greeter"):                   []byte(observation),
			}}
			src.Dirs = map[string][]string{"/": {"var"}, "/var": {"lib"}, "/var/lib": {"noctalia-greeter"}}
			for _, dir := range []string{"/var", "/var/lib"} {
				src.Commands[facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", dir)] = []byte("directory|root|root|755|2")
			}
			desired := encodeTest(facts.Directory{Exists: true, Owner: "greetd", Group: "greetd", Mode: "0750"})
			receipts, _, err := resourceExecutor(src).systemResource(plan.Operation{ID: "trigger:noctalia-state-directory", Kind: plan.KindTrigger, Action: plan.ActionRepair, Resource: &plan.ResourceChange{Name: "/var/lib/noctalia-greeter", Before: desired, After: desired}})
			valid := observation == "directory|greetd|greetd|750|2"
			if (err == nil) != valid || (len(receipts) == 1) != valid {
				t.Fatalf("receipts=%v err=%v", receipts, err)
			}
		})
	}
}

type fileCommandSource struct {
	*facts.FakeSource
	after          facts.SystemFile
	mutated        bool
	restoreFailure bool
	inspectFailure bool
}

func (s *fileCommandSource) Run(name string, args ...string) ([]byte, error) {
	if s.mutated && s.inspectFailure && name == "stat" {
		return nil, fmt.Errorf("inspection unavailable")
	}
	return s.FakeSource.Run(name, args...)
}
func (s *fileCommandSource) Stream(_, _ io.Writer, name string, args ...string) error {
	if name != "sudo" {
		return fmt.Errorf("unexpected command %s", name)
	}
	if args[0] == "restorecon" {
		if s.restoreFailure {
			return fmt.Errorf("restorecon failed")
		}
		return nil
	}
	if args[0] == "systemctl" {
		return nil
	}
	data, err := os.ReadFile(args[len(args)-1])
	if err != nil {
		return err
	}
	var payload FilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	s.mutated = true
	s.after = payload.Change.After
	if s.after.Exists {
		s.Dirs["/etc"] = []string{"nimbus.conf"}
		s.Files["/etc/nimbus.conf"] = s.after.Content
		s.Commands[facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc/nimbus.conf")] = []byte("regular file|root|root|644|1")
	} else {
		s.Dirs["/etc"] = nil
		delete(s.Files, "/etc/nimbus.conf")
	}
	return nil
}
func fileCommands() *fileCommandSource {
	return &fileCommandSource{FakeSource: &facts.FakeSource{Dirs: map[string][]string{"/": {"etc"}, "/etc": {}}, Files: map[string][]byte{}, Commands: map[string][]byte{
		facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc"): []byte("directory|root|root|755|1"),
		facts.Key("dnf5", facts.PackageQueryArgs...):               nil,
	}}}
}
func TestPostWriteFailureReportsAppliedUnrecordedFile(t *testing.T) {
	for _, kind := range []string{"restorecon", "reinspection"} {
		t.Run(kind, func(t *testing.T) {
			src := fileCommands()
			src.restoreFailure = kind == "restorecon"
			src.inspectFailure = kind == "reinspection"
			ex := resourceExecutor(src)
			ex.opts.Stage = t.TempDir()
			op := plan.Operation{ID: "file:/etc/nimbus.conf", Kind: plan.KindFile, Action: plan.ActionInstall, File: &plan.FileChange{Target: "/etc/nimbus.conf", After: facts.SystemFile{Exists: true, Content: []byte("new"), Owner: "root", Group: "root", Mode: "0644"}}}
			receipts, _, err := ex.systemFile(op)
			if err == nil || !strings.Contains(err.Error(), "applied but not recorded") || !strings.Contains(err.Error(), "restore the reviewed previous state") || len(receipts) != 0 || !src.mutated {
				t.Fatalf("postwrite result: %v %v", receipts, err)
			}
		})
	}
}
func TestDeferredRetirementRecordFailureIdentifiesEveryFile(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			src := &greeterRetirementSource{FakeSource: fileCommands().FakeSource}
			before := facts.SystemFile{Exists: true, Content: []byte("old"), Owner: "root", Group: "root", Mode: "0644"}
			p := &plan.Plan{Machine: "vm", Digest: "approved", Complete: true}
			var ids []string
			for i := range count {
				name := fmt.Sprintf("nimbus-%d.conf", i)
				target := "/etc/" + name
				id := "file:" + target
				ids = append(ids, id)
				src.Dirs["/etc"] = append(src.Dirs["/etc"], name)
				src.Files[target] = before.Content
				src.Commands[facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", target)] = []byte("regular file|root|root|644|1")
				p.Operations = append(p.Operations, plan.Operation{ID: id, Kind: plan.KindFile, Action: plan.ActionRemove, File: &plan.FileChange{Target: target, Before: before, Triggers: []string{"systemd-daemon-reload"}}})
			}
			trigger := "trigger:systemd-daemon-reload"
			p.Operations = append(p.Operations, plan.Operation{ID: trigger, Kind: plan.KindTrigger, Action: plan.ActionRepair})
			result := Run(p, Options{Source: src, Stage: t.TempDir(), Record: func(_ string, stage *state.Stage) error {
				if len(stage.Remove) > 0 {
					return fmt.Errorf("record failed")
				}
				return nil
			}})
			if result.Failed != ids[0] || len(result.Failures) != count || !strings.Contains(result.Error, "applied but not recorded") {
				t.Fatalf("retirement failure: %+v", result)
			}
			for i, id := range ids {
				if slices.Contains(result.Executed, id) || result.Failures[i].ID != id || result.Failures[i].Error != result.Error {
					t.Fatalf("unrecorded file %s reported incorrectly: %+v", id, result)
				}
			}
			if !slices.Equal(result.Executed, []string{trigger}) {
				t.Fatalf("recorded activation lost from report: %+v", result)
			}
		})
	}
}

func TestMatchingFileAdoptionPreservesMutationTime(t *testing.T) {
	src := fileCommands()
	have := facts.SystemFile{Exists: true, Content: []byte("same"), Owner: "root", Group: "root", Mode: "0644"}
	src.Dirs["/etc"] = []string{"nimbus.conf"}
	src.Files["/etc/nimbus.conf"] = have.Content
	src.Commands[facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc/nimbus.conf")] = []byte("regular file|root|root|644|1")
	changed := time.Unix(5, 0)
	op := plan.Operation{ID: "file:/etc/nimbus.conf", Kind: plan.KindFile, Action: plan.ActionAdopt, File: &plan.FileChange{Target: "/etc/nimbus.conf", Before: have, After: have, ChangedAt: changed}}
	receipts, _, err := resourceExecutor(src).systemFile(op)
	if err != nil || len(receipts) != 1 || !receipts[0].ChangedAt.Equal(changed) || !receipts[0].Timestamp.After(changed) || src.mutated {
		t.Fatalf("adoption changed mutation history: %+v %v", receipts, err)
	}
}

func TestMissingUnitRetirementOnlyRemovesVerifiedReceipt(t *testing.T) {
	have := facts.Service{Unit: "demo.service", Load: "not-found", Active: "inactive"}
	src := &facts.FakeSource{Commands: map[string][]byte{facts.Key("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", have.Unit): []byte("LoadState=not-found\nUnitFileState=\nActiveState=inactive\n")}}
	op := plan.Operation{ID: "service:demo.service", Kind: plan.KindService, Action: plan.ActionRetire, Resource: &plan.ResourceChange{Name: have.Unit, Before: encodeTest(have), After: encodeTest(have)}}
	receipts, remove, err := resourceExecutor(src).systemResource(op)
	if err != nil || len(receipts) != 0 || len(remove) != 1 || remove[0] != op.ID {
		t.Fatalf("retirement: receipts=%v remove=%v err=%v", receipts, remove, err)
	}
}

func TestAdoptionWithActivationChangeAdvancesChangeTimeWithoutWritingFile(t *testing.T) {
	src := fileCommands()
	have := facts.SystemFile{Exists: true, Content: []byte("same"), Owner: "root", Group: "root", Mode: "0644"}
	src.Dirs["/etc"] = []string{"nimbus.conf"}
	src.Files["/etc/nimbus.conf"] = have.Content
	src.Commands[facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc/nimbus.conf")] = []byte("regular file|root|root|644|1")
	op := plan.Operation{ID: "file:/etc/nimbus.conf", Kind: plan.KindFile, Action: plan.ActionAdopt, File: &plan.FileChange{Target: "/etc/nimbus.conf", Before: have, After: have, ChangedAt: time.Unix(5, 0), ActivationChanged: true}}
	receipts, _, err := resourceExecutor(src).systemFile(op)
	if err != nil || len(receipts) != 1 || !receipts[0].ChangedAt.Equal(receipts[0].Timestamp) || src.mutated {
		t.Fatalf("activation adoption: %+v %v", receipts, err)
	}
}

type greeterRetirementSource struct {
	*facts.FakeSource
	commands []string
}

func (s *greeterRetirementSource) Stream(_, _ io.Writer, name string, args ...string) error {
	s.commands = append(s.commands, facts.Key(name, args...))
	if name != "sudo" {
		return fmt.Errorf("unexpected command %s", name)
	}
	if args[0] == "systemctl" && len(args) == 2 && args[1] == "daemon-reload" {
		return nil
	}
	if len(args) < 3 || args[1] != "internal" || args[2] != "system-file" {
		return fmt.Errorf("unexpected privileged command %v", args)
	}
	data, err := os.ReadFile(args[len(args)-1])
	if err != nil {
		return err
	}
	var payload FilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.Change.After.Exists {
		return fmt.Errorf("retirement unexpectedly writes a file")
	}
	target := payload.Change.Target
	delete(s.Files, target)
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	s.Dirs[dir] = slices.DeleteFunc(s.Dirs[dir], func(name string) bool { return name == base })
	return nil
}

func TestGreeterFileRetirementReloadsUnitsWithoutRecreatingStateDirectory(t *testing.T) {
	tmpfiles := "/etc/tmpfiles.d/nimbus-noctalia-greeter.conf"
	dropin := "/etc/systemd/system/greetd.service.d/nimbus.conf"
	retained := "/var/lib/noctalia-greeter/retained-state"
	src := &greeterRetirementSource{FakeSource: &facts.FakeSource{Files: map[string][]byte{retained: []byte("keep")}, Dirs: map[string][]string{}, Commands: map[string][]byte{facts.Key("dnf5", facts.PackageQueryArgs...): nil}}}
	applied := &state.Applied{Receipts: map[string]state.Receipt{}}
	for _, target := range []string{tmpfiles, dropin} {
		desired := facts.SystemFile{Exists: true, Content: []byte("owned configuration"), Owner: "root", Group: "root", Mode: "0644"}
		src.Files[target] = desired.Content
		src.Commands[facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", target)] = []byte("regular file|root|root|644|1")
		current := target
		for current != "/" {
			parent := filepath.Dir(current)
			base := filepath.Base(current)
			if !slices.Contains(src.Dirs[parent], base) {
				src.Dirs[parent] = append(src.Dirs[parent], base)
			}
			current = parent
			if current != "/" {
				src.Commands[facts.Key("stat", "--format=%F|%U|%G|%a|%h", "--", current)] = []byte("directory|root|root|755|2")
			}
		}
		trigger := "systemd-daemon-reload"
		if target == tmpfiles {
			trigger = "noctalia-state-directory"
		}
		id := "file:" + target
		applied.Receipts[id] = state.Receipt{Resource: id, Provider: plan.KindFile, Machine: "vm", Verified: true, Previous: encodeTest(facts.SystemFile{}), Intended: encodeTest(desired), Triggers: []string{trigger}}
	}
	p, err := plan.Build(plan.Inputs{Resolved: &definitions.Resolved{Machine: "vm"}, Facts: &facts.Facts{}, Applied: applied, Source: src})
	if err != nil || !p.Complete {
		t.Fatalf("retirement plan: %+v %v", p, err)
	}
	reloads := 0
	retentionShown := false
	for _, op := range p.Operations {
		if op.ID == "trigger:noctalia-state-directory" {
			t.Fatal("retirement attempts to recreate greeter state")
		}
		if op.ID == "trigger:systemd-daemon-reload" {
			reloads++
		}
		if op.ID == "file:"+tmpfiles {
			if len(op.File.Triggers) != 0 {
				t.Fatalf("creation trigger remains: %+v", op)
			}
			retentionShown = retentionShown || slices.ContainsFunc(op.Notes, func(note string) bool {
				return strings.Contains(note, "Retain /var/lib/noctalia-greeter")
			})
		}
	}
	if reloads != 1 || !retentionShown {
		t.Fatalf("reload/retention missing: %+v", p.Operations)
	}
	retired := []string{}
	result := Run(p, Options{Source: src, Stage: t.TempDir(), Record: func(_ string, stage *state.Stage) error { retired = append(retired, stage.Remove...); return nil }})
	if result.Error != "" || len(retired) != 2 {
		t.Fatalf("retirement failed: %+v retired=%v", result, retired)
	}
	if string(src.Files[retained]) != "keep" {
		t.Fatal("greeter runtime state was deleted")
	}
	for _, target := range []string{tmpfiles, dropin} {
		if _, exists := src.Files[target]; exists {
			t.Fatalf("owned file remains: %s", target)
		}
	}
	if i := slices.IndexFunc(src.commands, func(command string) bool { return strings.Contains(command, "systemd-tmpfiles") }); i >= 0 {
		t.Fatalf("creation command ran on removal: %s", src.commands[i])
	}
}
