package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/postinstall"
)

func TestCompactOwnershipAndStableReplan(t *testing.T) {
	p := &plan.Plan{Machine: "vm", Operations: []plan.Operation{
		{ID: "file:/etc/a", Kind: plan.KindFile, Action: plan.ActionAdopt, Summary: "accept /etc/a", After: "packages:install"},
		{ID: "file:/etc/b", Kind: plan.KindFile, Action: plan.ActionAdopt, Summary: "accept /etc/b", After: "packages:install"},
		{ID: "file:/etc/dnf/versionlock.toml", Kind: plan.KindFile, Action: plan.ActionKeep, Notes: []string{"stable version policy"}},
	}}
	out := string(renderExecutionPlan(p, false, false))
	if !strings.Contains(out, "2 matching files") || strings.Contains(out, "accept /etc/") {
		t.Fatal(out)
	}
	full := string(renderPlan(p, false, false))
	if !strings.Contains(full, "accept /etc/a") {
		t.Fatal(full)
	}
	next := *p
	next.Operations = slices.Clone(p.Operations)
	for i := range next.Operations {
		next.Operations[i].After = ""
	}
	var delta strings.Builder
	result := &syncResult{}
	if err := showReplanned(&delta, p, &next, false, result); err != nil || delta.Len() != 0 || len(result.Differences) > 0 {
		t.Fatal(err, delta.String(), result)
	}
	result.rememberOperations(p)
	result.Steps = []runStep{{Name: "file:/etc/a", Status: "succeeded"}, {Name: "file:/etc/b", Status: "succeeded"}, {Name: "upgrade:dnf", Status: "failed", Detail: "download failed"}}
	result.Tasks = []postinstall.Task{{ID: "onepassword", Status: postinstall.Pending, Detail: "confirmation needed"}, {ID: "nvidia-mok", Status: postinstall.Unknown, Detail: "Permission denied"}}
	var report strings.Builder
	if err := result.render(&report, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 matching files; contents unchanged", "download failed", "Remaining setup:", "Verification problems:"} {
		if !strings.Contains(report.String(), want) {
			t.Fatal(report.String())
		}
	}
}
