package cli

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
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
