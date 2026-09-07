package plan

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRepositoryReconciliationIsApprovedInThePlan(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.RepositoryReconciliation, "DNF overrides") {
		t.Fatalf("plan omits post-transaction source policy: %+v", p)
	}
	data, err := json.Marshal(p)
	if err != nil || !strings.Contains(string(data), `"repository_reconciliation"`) {
		t.Fatalf("JSON policy missing: %s %v", data, err)
	}
	original := p.Digest
	p.RepositoryReconciliation = ""
	if digest(p) == original {
		t.Fatal("removing post-transaction policy did not change approval digest")
	}
	r.Repositories = nil
	p, err = Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	if p.RepositoryReconciliation != "" {
		t.Fatal("plan without selected repositories gained source reconciliation")
	}
}
