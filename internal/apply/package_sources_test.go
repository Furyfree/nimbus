package apply

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/plan"
)

type packageSourceResult struct {
	*nativetest.FakeSource
	after string
	calls int
}

func (s *packageSourceResult) Stream(_, _ io.Writer, _ string, _ ...string) error {
	s.calls++
	s.Commands[nativetest.Key("dnf5", inspect.PackageQueryArgs...)] = []byte(s.after)
	return nil
}

func TestSourceVerificationBeforeCompletion(t *testing.T) {
	for _, scenario := range []string{"verified repair", "unexpected source", "missing package", "bad preview"} {
		t.Run(scenario, func(t *testing.T) {
			src := &packageSourceResult{FakeSource: &nativetest.FakeSource{Commands: map[string][]byte{}}, after: "steam|0|1|1|x86_64|rpmfusion-nonfree|User\n"}
			if scenario == "unexpected source" {
				src.after = strings.ReplaceAll(src.after, "rpmfusion-nonfree", "nimbus-terra")
			}
			if scenario == "missing package" {
				src.after = ""
			}
			old := inspect.Package{Name: "steam", Arch: "x86_64", Version: "2", Release: "1", FromRepo: "nimbus-terra"}
			ex := &executor{p: &plan.Plan{Machine: "test"}, opts: Options{Source: src, Out: io.Discard, Now: time.Now}, seen: inspect.PackageMap([]inspect.Package{old})}
			op := plan.Operation{Action: plan.ActionRepair, Items: []string{"rpmfusion-nonfree:steam"}, Resolved: map[string]string{"steam": "steam.x86_64"}, PackageSources: plan.PackageSources{"steam.x86_64": {"rpmfusion-nonfree"}}, Steps: []plan.Step{{Argv: []string{"dnf5", "-y", "distro-sync", "--from-repo=rpmfusion-nonfree", "steam.x86_64"}}}, Transaction: &plan.Transaction{Packages: []plan.TxPackage{{Name: "steam", Arch: "x86_64", EVR: "1-1", Repository: "rpmfusion-nonfree", Section: "downgrading"}}}}
			if scenario == "bad preview" {
				op.Transaction.Packages[0].Repository = "nimbus-terra"
			}
			receipts, _, err := ex.installTransaction(op)
			if scenario != "verified repair" {
				if err == nil || len(receipts) != 0 {
					t.Fatalf("invalid source recorded completion: %v %+v", err, receipts)
				}
				if scenario == "bad preview" && src.calls != 0 {
					t.Fatal("unapproved source reached DNF")
				}
				return
			}
			if err != nil || len(receipts) != 1 {
				t.Fatalf("verified source: %v %+v", err, receipts)
			}
			r := receipts[0]
			if r.Operation != plan.ActionRepair || !strings.Contains(r.Previous, "nimbus-terra") || !strings.Contains(r.Verification, "rpmfusion-nonfree") {
				t.Fatalf("inaccurate repair evidence: %+v", r)
			}
		})
	}
}
