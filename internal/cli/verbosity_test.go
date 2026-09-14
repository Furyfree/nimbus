package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/snapper"
)

func TestConfigurationPlansSkipUnrelatedUpdateSolver(t *testing.T) {
	root, base := installerFixture(t)
	src := &maintenanceSource{Source: base}
	s, err := loadSelected(machineFlags{checkout: root, machine: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	normal, _, err := planWithState(s, src, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range src.events {
		if strings.Contains(call, "check-upgrade") || strings.Contains(call, "--assumeno upgrade") {
			t.Fatal("configuration queried updates", call)
		}
	}
	inspected, _, err := planWithState(s, src, false)
	if err != nil || normal.Digest != inspected.Digest {
		t.Fatal("update observations changed approval", err)
	}
	if !slices.ContainsFunc(src.events, func(call string) bool {
		return strings.Contains(call, "check-upgrade") || strings.Contains(call, "--assumeno upgrade")
	}) {
		t.Fatal("status no longer queries updates")
	}
}

func TestCompactPreviewPreservesChangesAndVerboseDetails(t *testing.T) {
	p := &plan.Plan{Machine: "test", Operations: []plan.Operation{
		{ID: "greeter-sync:test", Kind: plan.KindGreeterSync, Action: plan.ActionRepair, Summary: "verify greeter; enable if missing", Steps: []plan.Step{{Argv: []string{"greeter", "status", "test"}, Privileged: true}}, Notes: []string{"ownership policy"}},
		{ID: "remove:a", Kind: plan.KindPackage, Action: plan.ActionRemove, Summary: "remove old package"},
		{ID: "blocked:b", Kind: plan.KindPackage, Action: plan.ActionInstall, Summary: "install b", Blocked: "source unavailable"},
	}}
	compact := string(renderPlanView(p, false, false, true))
	for _, text := range []string{"enable if missing", "source unavailable"} {
		if !strings.Contains(compact, text) {
			t.Fatal(compact)
		}
	}
	full := string(renderPlanView(p, false, false, false))
	if strings.Contains(compact, "ownership policy") || !strings.Contains(full, "ownership policy") || !strings.Contains(full, "greeter status test") {
		t.Fatal(compact, full)
	}
}

func TestRunRecordsArePrivateBoundedAndContainNoCommandOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for i := range 23 {
		r, err := beginRunRecord("sync", "vm")
		if err != nil {
			t.Fatal(err)
		}
		r.Commit = "abc123"
		r.Phases = []runPhase{{Name: "Chezmoi apply", DurationMS: 12}}
		result := syncResult{Error: "SECRET native output", Notices: []string{"SECRET"}, Differences: []string{"SECRET"}, Steps: []runStep{{Name: "private path", Detail: "SECRET", Status: "skipped"}}}
		if err := r.finish(&result, "Chezmoi apply", true); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(r.path)
		if err != nil || strings.Contains(string(data), "SECRET") || strings.Contains(string(data), "private path") {
			t.Fatal("unsafe record", err)
		}
		var got runRecord
		if err := json.Unmarshal(data, &got); err != nil || got.Schema != 1 || got.Outcome != "failed" || got.Skipped != 1 || got.Commit != "abc123" {
			t.Fatal(err, got)
		}
		info, err := os.Stat(r.path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal(err)
		}
		if i == 22 {
			entries, err := os.ReadDir(filepath.Dir(r.path))
			if err != nil || len(entries) != 20 {
				t.Fatal("retention", len(entries), err)
			}
		}
	}
}

func TestRunRecordRejectsSymlinkAndRelativeState(t *testing.T) {
	for _, kind := range []string{"symlink", "relative"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XDG_STATE_HOME", root)
			if kind == "relative" {
				t.Setenv("XDG_STATE_HOME", "relative")
			} else if err := os.Symlink(t.TempDir(), filepath.Join(root, "nimbus")); err != nil {
				t.Fatal(err)
			}
			if _, err := beginRunRecord("sync", "vm"); err == nil {
				t.Fatal("accepted unsafe state")
			}
		})
	}
}

func TestRunRecordRetentionPreservesInvalidForeignAndActiveFiles(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	active, err := beginRunRecord("sync", "vm")
	if err != nil {
		t.Fatal(err)
	}
	valid, _ := json.Marshal(active)
	var unknown map[string]any
	if err := json.Unmarshal(valid, &unknown); err != nil {
		t.Fatal(err)
	}
	unknown["unrecognized"] = "leave this file alone"
	unknownBytes, _ := json.Marshal(unknown)
	invalid := map[string][]byte{
		"run-20000101T000000.000000000Z-1.json": []byte(`{"schema":1,"finished":"2026-01-01T00:00:00Z"}`),
		"run-20000101T000000.000000000Z-2.json": unknownBytes,
		"run-foreign.json":                      valid,
	}
	for name, data := range invalid {
		if err := os.WriteFile(filepath.Join(filepath.Dir(active.path), name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for range 22 {
		r, err := beginRunRecord("sync", "vm")
		if err != nil {
			t.Fatal(err)
		}
		if err := r.finish(&syncResult{}, "", false); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range invalid {
		got, err := os.ReadFile(filepath.Join(filepath.Dir(active.path), name))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("modified foreign file %s: %v", name, err)
		}
	}
	if _, err := os.Stat(active.path); err != nil {
		t.Fatal("active record removed", err)
	}
}

func TestRunRecordFailureKeepsKnownWorkflowStagePrivate(t *testing.T) {
	for _, name := range []string{"selection", "system installation", "dotfiles and tools", "snapper post", "snapper cleanup"} {
		t.Run(name, func(t *testing.T) {
			want := name
			if strings.HasPrefix(name, "snapper") {
				want = "snapper"
			}
			result := syncResult{Steps: []runStep{{Name: name, Status: "failed", Detail: "SECRET"}}}
			if got := recordFailurePhase(&result, "installation"); got != want {
				t.Fatal(got)
			}
		})
	}
	result := syncResult{Failures: []apply.Failure{{ID: "snapper cleanup", Error: "SECRET"}}}
	if got := recordFailurePhase(&result, "apply"); got != "snapper" {
		t.Fatal(got)
	}
	if got := recordFailurePhase(&syncResult{Steps: []runStep{{Name: "SECRET", Status: "failed"}}}, "apply"); got != "apply" {
		t.Fatal(got)
	}
}

func TestLateRecordWriteFailureDoesNotHideBehindReportedError(t *testing.T) {
	for _, original := range []error{reported{}, nativeExit{code: 7}} {
		t.Run(fmt.Sprintf("%T", original), func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			r, err := beginRunRecord("upgrade", "vm")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(r.path); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = finishRunRecord(&out, r, &syncResult{}, "Topgrade", original)
			if !errors.Is(err, original) || !strings.Contains(out.String(), "finish run record") {
				t.Fatal(err, out.String())
			}
		})
	}
}

func TestGreeterResolutionRejectsUnrelatedPlanDrift(t *testing.T) {
	original := &plan.Plan{Operations: []plan.Operation{{ID: "greeter", Kind: plan.KindGreeterSync, Action: plan.ActionRepair}, {ID: "package:a", Kind: plan.KindPackage, Action: plan.ActionKeep}}, Snapshots: &snapper.Plan{}}
	resolved := *original
	resolved.Operations = slices.Clone(original.Operations)
	resolved.Operations[0].Action = plan.ActionKeep
	if !sameNonGreeterOperations(original, &resolved) {
		t.Fatal("greeter-only resolution rejected")
	}
	resolved.Operations[1].Action = plan.ActionRemove
	if sameNonGreeterOperations(original, &resolved) {
		t.Fatal("unapproved package removal accepted")
	}
	resolved.Operations = slices.Clone(original.Operations)
	resolved.Snapshots = &snapper.Plan{Setup: true}
	if sameNonGreeterOperations(original, &resolved) {
		t.Fatal("unapproved snapshot setup accepted")
	}
}

func TestRunRecordFailureProducesOneJSONResult(t *testing.T) {
	for _, failApply := range []bool{false, true} {
		t.Run(fmt.Sprint(failApply), func(t *testing.T) {
			root, base := installerFixture(t)
			base.failApply = failApply
			withSource(t, handoffOutputSource{Source: base, afterStream: func(name string, args []string) {
				if name != "chezmoi" || !slices.Equal(args, []string{"apply"}) {
					return
				}
				paths, err := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "nimbus", "runs", "run-*.json"))
				if err != nil {
					t.Fatal(err)
				}
				for _, path := range paths {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
			}})
			code, out, errOut := run(t, "sync", "--checkout", root, "--machine", "vm", "--yes", "--json")
			var envelope struct {
				Data syncResult `json:"data"`
			}
			if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != ExitFailure {
				t.Fatalf("%d %s %s: %v", code, out, errOut, err)
			}
			if !strings.Contains(envelope.Data.Error, "finish run record") || strings.Contains(envelope.Data.Error, "reported") {
				t.Fatal(envelope.Data.Error)
			}
			if failApply && !strings.Contains(envelope.Data.Error, "Chezmoi") {
				t.Fatal("lost primary failure", envelope.Data.Error)
			}
		})
	}
}

func TestRunRecordKeepsPrimaryFailureBeforeSnapshotCleanup(t *testing.T) {
	result := syncResult{Failed: "packages:install", Failures: []apply.Failure{{ID: "packages:install"}, {ID: "snapper cleanup"}}, Steps: []runStep{{Name: "snapper cleanup", Status: "failed"}}}
	if got := recordFailurePhase(&result, "apply"); got != "apply" {
		t.Fatal(got)
	}
}
