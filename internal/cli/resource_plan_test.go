package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestPlanShowsSystemFileDiffAndNativeUnitChanges(t *testing.T) {
	p := &plan.Plan{Machine: "vm", Complete: true, Operations: []plan.Operation{
		{ID: "file:/etc/example", Kind: plan.KindFile, Action: plan.ActionRepair, Summary: "repair /etc/example", File: &plan.FileChange{Target: "/etc/example", Before: facts.SystemFile{Exists: true, Content: []byte("before\n")}, After: facts.SystemFile{Exists: true, Content: []byte("after"), Owner: "root", Group: "root", Mode: "0644"}}},
		{ID: "service:greetd.service", Kind: plan.KindService, Action: plan.ActionRepair, Summary: "configure greetd.service", Steps: []plan.Step{{Description: "enable unit", Argv: []string{"systemctl", "enable", "--", "greetd.service"}, Privileged: true}}},
	}}
	out := string(renderPlan(p, false, false))
	for _, want := range []string{"system resources:", "owner root, group root, mode 0644", "-before\n+after\n", "No newline at end of file", "systemctl enable -- greetd.service (privileged)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

func TestWhyExplainsSelectedSystemResourcesWithoutInspection(t *testing.T) {
	for _, resource := range []string{"file:/etc/greetd/nimbus.toml", "service:greetd.service", "trigger:systemd-daemon-reload", "default-target"} {
		code, out, errOut := run(t, "why", resource, "--checkout", repoRoot(t), "--machine", "vm")
		if code != ExitOK || !strings.Contains(out, "component:hyprland-session") || !strings.Contains(out, "profile:hyprland-noctalia") {
			t.Fatalf("%s: %d %s %s", resource, code, out, errOut)
		}
	}
}

func TestSyncResourceRetirementReportsSessionRequirements(t *testing.T) {
	for _, tc := range []struct {
		name, id, provider, before, previous, command, note string
		logout, reboot                                      bool
	}{
		{name: "remove added membership", id: "group:docker:test", provider: plan.KindGroup, before: "true", previous: "false", command: "sudo gpasswd --delete test docker", note: "logout and login", logout: true},
		{name: "retire preexisting membership", id: "group:docker:test", provider: plan.KindGroup, before: "true", previous: "true"},
		{name: "retire absent membership", id: "group:docker:test", provider: plan.KindGroup, before: "false", previous: "false"},
		{name: "restore boot target", id: "default-target", provider: plan.KindTarget, before: "graphical.target", previous: "multi-user.target", command: "sudo systemctl set-default multi-user.target", note: "next boot", reboot: true},
		{name: "retire unchanged boot target", id: "default-target", provider: plan.KindTarget, before: "multi-user.target", previous: "multi-user.target"},
	} {
		for _, asJSON := range []bool{false, true} {
			t.Run(tc.name+"/"+map[bool]string{false: "text", true: "json"}[asJSON], func(t *testing.T) {
				root, src := installerFixture(t)
				if err := os.WriteFile(filepath.Join(root, "profiles/common.toml"), []byte("schema=1\nid='common'\npackages=[]\n"), 0644); err != nil {
					t.Fatal(err)
				}
				receipt := state.Receipt{Schema: state.ReceiptSchema, Resource: tc.id, Provider: tc.provider, Machine: "vm", Verified: true,
					Operation: plan.ActionInstall, PlanDigest: "earlier", Previous: tc.previous, Intended: tc.before}
				if tc.provider == plan.KindGroup {
					receipt.Intended = "true"
				}
				if err := state.Record(stateRoot, receipt.PlanDigest, &state.Stage{Schema: state.Schema, PlanDigest: receipt.PlanDigest, Receipts: []state.Receipt{receipt}}); err != nil {
					t.Fatal(err)
				}
				if tc.provider == plan.KindGroup {
					groups := "test wheel"
					if tc.before == "true" {
						groups += " docker"
					}
					src.Commands["id -nG -- test"] = []byte(groups + "\n")
					src.Commands["id -gn -- test"] = []byte("test\n")
				} else {
					src.Commands["systemctl get-default"] = []byte(tc.before + "\n")
				}
				if tc.command != "" {
					src.Commands[tc.command] = nil
				}
				withSource(t, handoffOutputSource{Source: src, afterStream: func(name string, args []string) {
					if facts.Key(name, args...) != tc.command {
						t.Fatalf("unexpected mutation: %s %v", name, args)
					}
					if tc.provider == plan.KindGroup {
						src.Commands["id -nG -- test"] = []byte("test wheel\n")
					} else {
						src.Commands["systemctl get-default"] = []byte(tc.previous + "\n")
					}
				}})
				args := []string{"sync", "--checkout", root, "--machine", "vm", "--no-upgrade"}
				code, preview, errOut := run(t, append(slices.Clone(args), "--plan")...)
				if code != ExitOK || len(src.calls) != 0 {
					t.Fatalf("retirement preview failed or mutated: %d %s%s; calls=%v", code, preview, errOut, src.calls)
				}
				if tc.note != "" && !strings.Contains(strings.ToLower(preview), tc.note) {
					t.Errorf("retirement preview omitted %q: %s", tc.note, preview)
				}
				if tc.note == "" && (strings.Contains(strings.ToLower(preview), "logout") || strings.Contains(strings.ToLower(preview), "next boot")) {
					t.Errorf("unchanged retirement requires no session change: %s", preview)
				}
				args = append(args, "--yes")
				if asJSON {
					args = append(args, "--json")
				}
				code, out, errOut := run(t, args...)
				if code != ExitOK {
					t.Fatalf("retirement failed: %d %s%s", code, out, errOut)
				}
				if asJSON {
					var env struct{ Data syncResult }
					if err := json.Unmarshal([]byte(out), &env); err != nil {
						t.Fatal(err)
					}
					if env.Data.Logout != tc.logout || env.Data.Reboot != tc.reboot || !slices.Contains(env.Data.Executed, tc.id) {
						t.Errorf("retirement session requirements missing or incorrect: %+v", env.Data)
					}
					if strings.Contains(out, `"logout_required"`) != tc.logout || strings.Contains(out, `"reboot_required"`) != tc.reboot {
						t.Errorf("JSON session requirements should appear only when needed: %s", out)
					}
				} else {
					_, summary, ok := strings.Cut(out, "\nsync summary:\n")
					if !ok || !strings.Contains(summary, tc.id) || strings.Contains(summary, "Log out and log in again") != tc.logout || strings.Contains(summary, "Reboot required") != tc.reboot {
						t.Errorf("retirement closing report missing or incorrect: %s", out)
					}
				}
				var wantCalls []string
				if tc.command != "" {
					wantCalls = []string{tc.command}
				}
				if !slices.Equal(src.calls, wantCalls) {
					t.Errorf("retirement commands = %v, want %v", src.calls, wantCalls)
				}
				applied, err := state.Read(stateRoot)
				if err != nil {
					t.Fatal(err)
				}
				if _, retained := applied.Receipts[tc.id]; retained {
					t.Fatal("successful retirement retained its ownership receipt")
				}
			})
		}
	}
}
