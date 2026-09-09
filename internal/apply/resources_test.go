package apply

import (
	"encoding/json"
	"errors"
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

func TestServiceIntentAdoptionUpdatesRetirementWithoutNativeMutation(t *testing.T) {
	for _, dropRunning := range []bool{false, true} {
		t.Run(map[bool]string{false: "add running", true: "drop running"}[dropRunning], func(t *testing.T) {
			const id = "service:demo.service"
			original := facts.Service{Unit: "demo.service", Load: "loaded", Enabled: "disabled", Active: "inactive"}
			have := facts.Service{Unit: original.Unit, Load: "loaded", Enabled: "enabled", Active: "active"}
			old := definitions.ServiceDecl{Unit: have.Unit, Enabled: new(true)}
			want := definitions.ServiceDecl{Unit: have.Unit, Enabled: new(true), Running: new(true)}
			if dropRunning {
				old, want = want, old
			}
			root := t.TempDir()
			receipt := state.Receipt{Schema: state.ReceiptSchema, Resource: id, Provider: plan.KindService, Machine: "vm", Verified: true,
				Operation: plan.ActionRepair, PlanDigest: "earlier", Previous: encodeTest(original), Intended: encodeTest(old)}
			if err := state.Record(root, receipt.PlanDigest, &state.Stage{Schema: state.Schema, PlanDigest: receipt.PlanDigest, Receipts: []state.Receipt{receipt}}); err != nil {
				t.Fatal(err)
			}
			src := &serviceCommandSource{before: have, after: have, FakeSource: &facts.FakeSource{Commands: map[string][]byte{
				facts.Key("dnf5", facts.PackageQueryArgs...):      nil,
				facts.Key("dnf5", "--cacheonly", "check-upgrade"): nil,
			}}}
			resolved := &definitions.Resolved{Machine: "vm", Services: []definitions.ResolvedService{{ServiceDecl: want}}}
			build := func() *plan.Plan {
				t.Helper()
				applied, err := state.Read(root)
				if err != nil {
					t.Fatal(err)
				}
				p, err := plan.Build(plan.Inputs{Resolved: resolved, Facts: &facts.Facts{}, Source: src, Applied: applied})
				if err != nil || !p.Complete || len(p.Operations) != 1 {
					t.Fatalf("service plan: %+v %v", p, err)
				}
				return p
			}
			p := build()
			if p.Operations[0].Action != plan.ActionAdopt || len(p.Operations[0].Steps) != 0 {
				t.Fatalf("changed intent needs receipt adoption: %+v", p.Operations[0])
			}
			opts := Options{Source: src, Record: func(digest string, stage *state.Stage) error { return state.Record(root, digest, stage) }}
			if result := Run(p, opts); result.Error != "" || src.mutated {
				t.Fatalf("adoption ran a native mutation or failed: %+v, mutated=%t", result, src.mutated)
			}
			applied, err := state.Read(root)
			if err != nil {
				t.Fatal(err)
			}
			if got := applied.Receipts[id]; got.Intended != encodeTest(want) || got.Previous != receipt.Previous || got.Operation != plan.ActionAdopt {
				t.Fatalf("adoption lost current intent or original recovery state: %+v", got)
			}
			if p := build(); p.Operations[0].Action != plan.ActionKeep {
				t.Fatalf("unchanged intent did not converge: %+v", p.Operations[0])
			}
			resolved.Services = nil
			p = build()
			commands := []string{"systemctl disable -- demo.service"}
			src.after.Enabled = "disabled"
			if !dropRunning {
				commands = append(commands, "systemctl stop -- demo.service")
				src.after.Active = "inactive"
			}
			var planned []string
			for _, step := range p.Operations[0].Steps {
				planned = append(planned, facts.Key(step.Argv[0], step.Argv[1:]...))
			}
			if !slices.Equal(planned, commands) {
				t.Fatalf("retirement uses stale intent: %v, want %v", planned, commands)
			}
			for _, command := range commands {
				src.Commands["sudo "+command] = nil
			}
			if result := Run(p, opts); result.Error != "" || !src.mutated {
				t.Fatalf("retirement failed: %+v", result)
			}
			applied, err = state.Read(root)
			if err != nil || len(applied.Receipts) != 0 {
				t.Fatalf("retired service receipt remains: %+v %v", applied, err)
			}
		})
	}
}

type targetCommandSource struct {
	*facts.FakeSource
	mutations []string
}

func (s *targetCommandSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	s.mutations = append(s.mutations, facts.Key(name, args...))
	if err := s.FakeSource.Stream(out, errOut, name, args...); err != nil {
		return err
	}
	s.Commands[facts.Key("systemctl", "get-default")] = []byte(args[len(args)-1] + "\n")
	return nil
}

func TestTargetIntentAdoptionPreservesRecoveryAndConverges(t *testing.T) {
	for _, original := range []string{"graphical.target", "multi-user.target"} {
		t.Run(original, func(t *testing.T) {
			const id = "default-target"
			const target = "multi-user.target"
			root := t.TempDir()
			receipt := state.Receipt{Schema: state.ReceiptSchema, Resource: id, Provider: plan.KindTarget, Machine: "vm", Verified: true,
				Operation: plan.ActionRepair, PlanDigest: "earlier", Previous: original, Intended: "graphical.target"}
			if err := state.Record(root, receipt.PlanDigest, &state.Stage{Schema: state.Schema, PlanDigest: receipt.PlanDigest, Receipts: []state.Receipt{receipt}}); err != nil {
				t.Fatal(err)
			}
			src := &targetCommandSource{FakeSource: &facts.FakeSource{Commands: map[string][]byte{
				facts.Key("systemctl", "get-default"):             []byte(target + "\n"),
				facts.Key("dnf5", facts.PackageQueryArgs...):      nil,
				facts.Key("dnf5", "--cacheonly", "check-upgrade"): nil,
			}}}
			resolved := &definitions.Resolved{Machine: "vm", DefaultTarget: target}
			build := func() *plan.Plan {
				t.Helper()
				applied, err := state.Read(root)
				if err != nil {
					t.Fatal(err)
				}
				p, err := plan.Build(plan.Inputs{Resolved: resolved, Facts: &facts.Facts{}, Source: src, Applied: applied})
				if err != nil || !p.Complete || len(p.Operations) != 1 {
					t.Fatalf("target plan: %+v %v", p, err)
				}
				return p
			}
			p := build()
			if p.Operations[0].Action != plan.ActionAdopt || len(p.Operations[0].Steps) != 0 {
				t.Fatalf("matching target needs receipt adoption: %+v", p.Operations[0])
			}
			opts := Options{Source: src, Record: func(digest string, stage *state.Stage) error { return state.Record(root, digest, stage) }}
			if result := Run(p, opts); result.Error != "" || len(src.mutations) != 0 || result.Reboot {
				t.Fatalf("receipt adoption changed the live target or failed: %+v commands=%v", result, src.mutations)
			}
			applied, err := state.Read(root)
			if err != nil {
				t.Fatal(err)
			}
			if got := applied.Receipts[id]; got.Intended != target || got.Previous != original || got.Operation != plan.ActionAdopt {
				t.Fatalf("adoption lost current intent or original recovery target: %+v", got)
			}
			if p := build(); p.Operations[0].Action != plan.ActionKeep {
				t.Fatalf("unchanged target did not converge: %+v", p.Operations[0])
			}
			resolved.DefaultTarget = ""
			p = build()
			var commands []string
			if original != target {
				commands = []string{"sudo systemctl set-default " + original}
				src.Commands[commands[0]] = nil
			}
			var planned []string
			for _, step := range p.Operations[0].Steps {
				planned = append(planned, facts.Key("sudo", step.Argv...))
			}
			if p.Operations[0].Resource.After != original || !slices.Equal(planned, commands) {
				t.Fatalf("retirement lost original target or requires unnecessary commands: %+v", p.Operations[0])
			}
			if result := Run(p, opts); result.Error != "" || !slices.Equal(src.mutations, commands) {
				t.Fatalf("target retirement failed: %+v commands=%v", result, src.mutations)
			}
			applied, err = state.Read(root)
			if err != nil || len(applied.Receipts) != 0 {
				t.Fatalf("retired target receipt remains: %+v %v", applied, err)
			}
		})
	}
}

type membershipSource struct {
	*facts.FakeSource
	observations []string
}

func (s *membershipSource) Run(name string, args ...string) ([]byte, error) {
	if facts.Key(name, args...) == "id -nG -- owner" {
		if len(s.observations) == 0 {
			return nil, errors.New("unexpected membership inspection")
		}
		out := s.observations[0]
		s.observations = s.observations[1:]
		return []byte(out), nil
	}
	return s.FakeSource.Run(name, args...)
}

func TestPreexistingMembershipRetirementPreservesObservation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		observations []string
		valid        bool
	}{
		{"still present", []string{"owner docker", "owner docker", "owner docker"}, true},
		{"removed externally", []string{"owner", "owner", "owner"}, true},
		{"changed before execution", []string{"owner", "owner docker"}, false},
		{"changed during verification", []string{"owner", "owner", "owner docker"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "group:docker:owner"
			root := t.TempDir()
			receipt := state.Receipt{Schema: state.ReceiptSchema, Resource: id, Provider: plan.KindGroup, Machine: "vm", Verified: true,
				Operation: plan.ActionAdopt, PlanDigest: "earlier", Previous: "true", Intended: "true"}
			if err := state.Record(root, receipt.PlanDigest, &state.Stage{Schema: state.Schema, PlanDigest: receipt.PlanDigest, Receipts: []state.Receipt{receipt}}); err != nil {
				t.Fatal(err)
			}
			src := &membershipSource{observations: tc.observations, FakeSource: &facts.FakeSource{Commands: map[string][]byte{
				facts.Key("dnf5", facts.PackageQueryArgs...):      nil,
				facts.Key("dnf5", "--cacheonly", "check-upgrade"): nil,
			}}}
			applied, err := state.Read(root)
			if err != nil {
				t.Fatal(err)
			}
			p, err := plan.Build(plan.Inputs{Resolved: &definitions.Resolved{Machine: "vm"}, Facts: &facts.Facts{}, Source: src, Applied: applied})
			if err != nil || !p.Complete || len(p.Operations) != 1 || len(p.Operations[0].Steps) != 0 {
				t.Fatalf("membership retirement plan: %+v %v", p, err)
			}
			result := Run(p, Options{Source: src, Record: func(digest string, stage *state.Stage) error { return state.Record(root, digest, stage) }})
			if (result.Error == "") != tc.valid || len(src.observations) != 0 {
				t.Fatalf("retirement verification: %+v, remaining observations=%v", result, src.observations)
			}
			applied, err = state.Read(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, retained := applied.Receipts[id]; retained == tc.valid {
				t.Fatalf("membership receipt retained=%t after valid retirement=%t", retained, tc.valid)
			}
		})
	}
}

func TestResourceRetirementReportsSessionRequirements(t *testing.T) {
	for _, kind := range []string{plan.KindGroup, plan.KindTarget} {
		for _, outcome := range []string{"changed", "unchanged", "native failure", "verification failure", "record failure", "pending"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				base := &facts.FakeSource{Commands: map[string][]byte{facts.Key("dnf5", facts.PackageQueryArgs...): nil}, Failures: map[string]string{}}
				var source facts.Source
				op := plan.Operation{Kind: kind, Action: plan.ActionRemove}
				changed := outcome != "unchanged"
				if kind == plan.KindGroup {
					op.ID = "group:docker:owner"
					before := "false"
					observations := []string{"owner", "owner"}
					if changed {
						before = "true"
						observations = []string{"owner docker", "owner"}
						op.Steps = []plan.Step{{Argv: []string{"gpasswd", "--delete", "owner", "docker"}, Privileged: true}}
					}
					op.Resource = &plan.ResourceChange{Name: "docker", User: "owner", Before: before, After: "false", Previous: "false"}
					source = &membershipSource{FakeSource: base, observations: observations}
				} else {
					op.ID = "default-target"
					before := "graphical.target"
					if changed {
						before = "multi-user.target"
						op.Steps = []plan.Step{{Argv: []string{"systemctl", "set-default", "graphical.target"}, Privileged: true}}
					}
					base.Commands[facts.Key("systemctl", "get-default")] = []byte(before + "\n")
					op.Resource = &plan.ResourceChange{Name: op.ID, Before: before, After: "graphical.target", Previous: "graphical.target"}
					source = &targetCommandSource{FakeSource: base}
				}
				if outcome == "verification failure" {
					if kind == plan.KindGroup {
						source = &membershipSource{FakeSource: base, observations: []string{"owner docker", "owner docker"}}
					} else {
						source = base
					}
				}
				for _, step := range op.Steps {
					command := facts.Key("sudo", step.Argv...)
					base.Commands[command] = nil
					if outcome == "native failure" {
						base.Failures[command] = "native mutation failed"
					}
				}
				if outcome == "pending" {
					op.After = "packages:install"
				}
				recordCalled := false
				result := Run(&plan.Plan{Complete: true, Digest: "approved", Operations: []plan.Operation{op}}, Options{Source: source, Record: func(_ string, stage *state.Stage) error {
					recordCalled = true
					if len(stage.Receipts) != 0 || !slices.Equal(stage.Remove, []string{op.ID}) {
						t.Fatalf("retirement must only remove its receipt: %+v", stage)
					}
					if outcome == "record failure" {
						return errors.New("record failed")
					}
					return nil
				}})
				verified := outcome != "native failure" && outcome != "verification failure" && outcome != "pending"
				failed := outcome == "native failure" || outcome == "verification failure" || outcome == "record failure"
				if recordCalled != verified || (result.Error != "") != failed {
					t.Fatalf("retirement outcome: %+v, record called=%t", result, recordCalled)
				}
				if result.Logout != (verified && changed && kind == plan.KindGroup) || result.Reboot != (verified && changed && kind == plan.KindTarget) {
					t.Fatalf("retirement requirements: %+v", result)
				}
				if (len(result.Executed) == 1) != (verified && !failed) {
					t.Fatalf("retirement execution report: %+v", result)
				}
			})
		}
	}
}

type serviceCommandSource struct {
	*facts.FakeSource
	before, after facts.Service
	mutated       bool
}

func (s *serviceCommandSource) Run(name string, args ...string) ([]byte, error) {
	if facts.Key(name, args...) == facts.Key("systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", s.before.Unit) {
		have := s.before
		if s.mutated {
			have = s.after
		}
		return fmt.Appendf(nil, "LoadState=%s\nUnitFileState=%s\nActiveState=%s\n", have.Load, have.Enabled, have.Active), nil
	}
	return s.FakeSource.Run(name, args...)
}

func (s *serviceCommandSource) Stream(out, errOut io.Writer, name string, args ...string) error {
	if err := s.FakeSource.Stream(out, errOut, name, args...); err != nil {
		return err
	}
	s.mutated = true
	return nil
}

func TestServiceVerificationRequiresSupportedNativeEnablement(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled *bool
		after   string
		valid   bool
	}{
		{"disabled", new(false), "disabled", true},
		{"still enabled", new(false), "enabled", false},
		{"runtime enabled", new(false), "enabled-runtime", false},
		{"static", new(false), "static", false},
		{"masked", new(false), "masked", false},
		{"runtime masked", new(false), "masked-runtime", false},
		{"enabled", new(true), "enabled", true},
		{"only runtime enabled", new(true), "enabled-runtime", false},
		{"unmanaged static", nil, "static", true},
		{"unmanaged masked", nil, "masked", false},
		{"unmanaged runtime masked", nil, "masked-runtime", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := facts.Service{Unit: "demo.service", Load: "loaded", Enabled: "enabled", Active: "inactive"}
			want := definitions.ServiceDecl{Unit: before.Unit, Enabled: tc.enabled}
			verb := "disable"
			if tc.enabled == nil {
				before.Enabled = "static"
				want.Running = new(true)
				verb = "start"
			} else if *tc.enabled {
				before.Enabled = "disabled"
				verb = "enable"
			}
			after := before
			after.Enabled = tc.after
			if want.Running != nil {
				after.Active = "active"
			}
			argv := []string{"systemctl", verb, "--", before.Unit}
			src := &serviceCommandSource{before: before, after: after, FakeSource: &facts.FakeSource{Commands: map[string][]byte{facts.Key("sudo", argv...): nil}}}
			op := plan.Operation{ID: "service:" + before.Unit, Kind: plan.KindService, Action: plan.ActionRepair,
				Resource: &plan.ResourceChange{Name: before.Unit, Before: encodeTest(before), After: encodeTest(want), Previous: encodeTest(before), Enabled: want.Enabled, Running: want.Running},
				Steps:    []plan.Step{{Argv: argv, Privileged: true}}}
			receipts, _, err := resourceExecutor(src).systemResource(op)
			if (err == nil) != tc.valid || (len(receipts) == 1) != tc.valid || !src.mutated {
				t.Fatalf("verification: receipts=%+v err=%v mutated=%t", receipts, err, src.mutated)
			}
		})
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

func TestDaemonReloadVerificationPreservesInspectionFailure(t *testing.T) {
	src := &facts.FakeSource{Commands: map[string][]byte{facts.Key("sudo", "systemctl", "daemon-reload"): nil}}
	ex := resourceExecutor(src)
	ex.p.Operations = []plan.Operation{{Kind: plan.KindService, Resource: &plan.ResourceChange{Name: "demo.service"}}}
	receipts, removed, err := ex.systemResource(plan.Operation{ID: "trigger:systemd-daemon-reload", Kind: plan.KindTrigger, Action: plan.ActionRepair})
	if !errors.Is(err, facts.ErrNotRecorded) || len(receipts) != 0 || len(removed) != 0 {
		t.Fatalf("reload verification cause lost: receipts=%v removed=%v err=%v", receipts, removed, err)
	}
}

type fileCommandSource struct {
	*facts.FakeSource
	after          facts.SystemFile
	mutated        bool
	restoreFailure bool
	inspectFailure bool
	cleanupFailure bool
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
	if s.cleanupFailure {
		staged := args[len(args)-1]
		if err := os.Remove(staged); err != nil {
			return err
		}
		if err := os.Mkdir(staged, 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(staged, "leftover"), nil, 0600); err != nil {
			return err
		}
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
	for _, kind := range []string{"restorecon", "reinspection", "payload cleanup"} {
		t.Run(kind, func(t *testing.T) {
			src := fileCommands()
			src.restoreFailure = kind == "restorecon"
			src.inspectFailure = kind == "reinspection"
			src.cleanupFailure = kind == "payload cleanup"
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
