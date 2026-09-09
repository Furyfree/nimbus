package plan

import (
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/state"
)

func retirementFixture(kind string) (*builder, *facts.FakeSource, state.Receipt) {
	f := &facts.Facts{}
	id, native := "repository:old", "nimbus-old"
	if kind == KindFlatpakRemote {
		id, native = "flatpak-remote:old", "old"
		f.Flatpak.Value.Remotes = []facts.FlatpakRemote{{Name: native, URL: "https://example.invalid/repo", GPGVerify: true, KeyFingerprints: []string{"key"}}}
	} else {
		f.Repositories.Value = []facts.Repository{{ID: native, File: "/etc/yum.repos.d/nimbus-old.repo", BaseURL: "https://example.invalid/repo", Enabled: true, GPGCheck: "1"}}
	}
	src := &facts.FakeSource{Commands: map[string][]byte{"flatpak list --system --all --app --runtime --columns=ref,origin,options": nil}}
	r := state.Receipt{Resource: id, Provider: kind, Machine: "vm", Verified: true,
		Source: &state.SourceOwnership{Applied: SourceSnapshot(kind, []string{native}, f)}}
	b := &builder{in: Inputs{Facts: f, Source: src, Resolved: &definitions.Resolved{Machine: "vm"},
		Applied: &state.Applied{Receipts: map[string]state.Receipt{id: r}}}}
	return b, src, r
}

func TestSourceRetirementUsesOwnedIdentityAndPreservesPreexistingSources(t *testing.T) {
	for _, kind := range []string{KindRepository, KindFlatpakRemote} {
		t.Run(kind, func(t *testing.T) {
			b, _, receipt := retirementFixture(kind)
			ops := b.sourceRetirements()
			if len(ops) != 1 || ops[0].Blocked != "" || len(ops[0].Steps) != 1 || ops[0].Action != ActionRemove {
				t.Fatalf("owned source was not retired: %+v", ops)
			}
			if strings.Contains(strings.Join(ops[0].Steps[0].Argv, " "), "--force") {
				t.Fatal("remote deletion can never force past installed refs")
			}
			receipt.Source.Original = slices.Clone(receipt.Source.Applied)
			b.in.Applied.Receipts[receipt.Resource] = receipt
			ops = b.sourceRetirements()
			if ops[0].Action != ActionRetire || len(ops[0].Steps) != 0 || ops[0].Blocked != "" {
				t.Fatalf("preexisting source would be changed: %+v", ops[0])
			}
		})
	}
}

func TestSourceRetirementRejectsLegacyForeignChangedAndUnknownState(t *testing.T) {
	for _, which := range []string{"legacy", "unverified", "foreign", "changed URL", "duplicate", "unknown packages", "unknown provenance", "unsafe native ID"} {
		t.Run(which, func(t *testing.T) {
			b, _, receipt := retirementFixture(KindRepository)
			switch which {
			case "legacy":
				receipt.Source = nil
			case "unverified":
				receipt.Verified = false
			case "foreign":
				receipt.Machine = "other"
			case "changed URL":
				b.in.Facts.Repositories.Value[0].BaseURL = "https://foreign.invalid"
			case "duplicate":
				b.in.Facts.Repositories.Value = append(b.in.Facts.Repositories.Value, b.in.Facts.Repositories.Value[0])
			case "unknown packages":
				b.in.Facts.Packages.Error = "unreadable"
			case "unknown provenance":
				b.in.Facts.Packages.Value = []facts.Package{{Name: "adopted", FromRepo: "@commandline"}}
			case "unsafe native ID":
				receipt.Source.Applied[0].ID = "*"
			}
			b.in.Applied.Receipts[receipt.Resource] = receipt
			ops := b.sourceRetirements()
			if ops[0].Blocked == "" || len(ops[0].Steps) != 0 {
				t.Fatalf("uncertain source retirement allowed: %+v", ops[0])
			}
		})
	}
}

func TestRetirementRetainsSourcesUsedByAdoptedPackagesAndRuntimes(t *testing.T) {
	b, _, _ := retirementFixture(KindRepository)
	b.in.Facts.Packages.Value = []facts.Package{{Name: "adopted", FromRepo: "nimbus-old"}}
	op := b.sourceRetirements()[0]
	if op.Action != ActionKeep || len(op.Steps) != 0 || len(op.Notes) == 0 {
		t.Fatalf("installed package lost its source: %+v", op)
	}
	for _, ref := range []string{"app/org.example.App/x86_64/stable", "runtime/org.example.Platform/x86_64/stable", "runtime/org.example.Platform.Locale/x86_64/stable"} {
		b, src, _ := retirementFixture(KindFlatpakRemote)
		kind, partial, _ := strings.Cut(ref, "/")
		options := ""
		if kind == "runtime" {
			options = "runtime"
		}
		src.Commands["flatpak list --system --all --app --runtime --columns=ref,origin,options"] = []byte(partial + "\told\t" + options + "\n")
		op := b.sourceRetirements()[0]
		if op.Action != ActionKeep || len(op.Steps) != 0 || !strings.Contains(strings.Join(op.Notes, ""), ref) {
			t.Fatalf("installed ref lost its source: %+v", op)
		}
	}
}

func TestFlatpakRetirementRefusesIncompleteInventoryOrChangedTrust(t *testing.T) {
	for _, which := range []string{"unrecorded", "malformed", "missing origin", "changed key"} {
		b, src, _ := retirementFixture(KindFlatpakRemote)
		switch which {
		case "unrecorded":
			delete(src.Commands, "flatpak list --system --all --app --runtime --columns=ref,origin,options")
		case "malformed":
			src.Commands["flatpak list --system --all --app --runtime --columns=ref,origin,options"] = []byte("broken\n")
		case "missing origin":
			src.Commands["flatpak list --system --all --app --runtime --columns=ref,origin,options"] = []byte("runtime/org.example.Platform/x86_64/stable\t\n")
		case "changed key":
			b.in.Facts.Flatpak.Value.Remotes[0].KeyFingerprints = []string{"foreign"}
		}
		if op := b.sourceRetirements()[0]; op.Blocked == "" || len(op.Steps) != 0 {
			t.Fatalf("%s allowed: %+v", which, op)
		}
	}
}

func TestSourceOwnershipSurvivesRepairAndSelection(t *testing.T) {
	b, _, receipt := retirementFixture(KindRepository)
	original := state.NativeSource{ID: "nimbus-old", Enabled: false}
	receipt.Source.Original = []state.NativeSource{original}
	b.in.Applied.Receipts[receipt.Resource] = receipt
	got := b.sourceOwnership(Operation{ID: receipt.Resource, Kind: KindRepository}, []string{"nimbus-old"})
	if len(got.Original) != 1 || got.Original[0] != original {
		t.Fatal("repair changed original enablement")
	}
	b.in.Resolved.Repositories = []string{"old"}
	if ops := b.sourceRetirements(); len(ops) != 0 {
		t.Fatal("selected source was retired")
	}
}

func TestSourceRetirementWaitsForEveryReviewedConsumerRemoval(t *testing.T) {
	b, _, _ := retirementFixture(KindRepository)
	b.in.Facts.Packages.Value = []facts.Package{{Name: "owned", Arch: "x86_64", FromRepo: "nimbus-old"}}
	removal := Operation{ID: "packages:remove-owned", Kind: KindPackage, Action: ActionRemove,
		Transaction: &Transaction{Packages: []TxPackage{{Name: "owned", Arch: "x86_64", Section: "removing"}}}}
	op := b.sourceRetirements([]Operation{removal})[0]
	if op.After != removal.ID || op.Blocked != "" || op.Action != ActionRemove {
		t.Fatalf("source did not wait for removal: %+v", op)
	}
	b.in.Facts.Packages.Value = append(b.in.Facts.Packages.Value, facts.Package{Name: "adopted", Arch: "x86_64", FromRepo: "nimbus-old"})
	op = b.sourceRetirements([]Operation{removal})[0]
	if op.Action != ActionKeep || op.After != "" {
		t.Fatalf("retained package would lose its source: %+v", op)
	}
	b, src, _ := retirementFixture(KindFlatpakRemote)
	src.Commands["flatpak list --system --all --app --runtime --columns=ref,origin,options"] = []byte("org.example.App/x86_64/stable\told\tcurrent\n")
	removal = Operation{ID: "flatpak:org.example.App", Kind: KindFlatpak, Action: ActionRemove}
	op = b.sourceRetirements([]Operation{removal})[0]
	if op.After != removal.ID {
		t.Fatalf("remote did not wait for app removal: %+v", op)
	}
	src.Commands["flatpak list --system --all --app --runtime --columns=ref,origin,options"] = []byte("org.example.App/x86_64/stable\told\tcurrent\norg.example.Platform/x86_64/stable\told\truntime\n")
	op = b.sourceRetirements([]Operation{removal})[0]
	if op.Action != ActionKeep || op.After != "" {
		t.Fatalf("retained runtime would lose its source: %+v", op)
	}
}

func TestSourceRetirementRetainsReconciledVendorConsumersAndSelectedAliases(t *testing.T) {
	b, _, _ := retirementFixture(KindRepository)
	b.in.Facts.Repositories.Value = append(b.in.Facts.Repositories.Value, facts.Repository{
		ID: "vendor-old", File: "vendor.repo", BaseURL: "https://example.invalid/repo", Enabled: false,
	})
	b.in.Facts.Packages.Value = []facts.Package{{Name: "adopted", FromRepo: "vendor-old"}}
	if op := b.sourceRetirements()[0]; op.Action != ActionKeep || len(op.Steps) != 0 {
		t.Fatalf("reconciled source lost its adopted consumer: %+v", op)
	}
	b, _, receipt := retirementFixture(KindRepository)
	b.in.Root.Repositories = map[string]definitions.Repository{"alias": {Kind: "copr", Project: "owner/project"}}
	b.in.Resolved.Repositories = []string{"alias"}
	receipt.Source.Applied[0].ID = "copr:copr.fedorainfracloud.org:owner:project"
	b.in.Applied.Receipts[receipt.Resource] = receipt
	if op := b.sourceRetirements()[0]; op.Action != ActionKeep || len(op.Steps) != 0 {
		t.Fatalf("selected native source was retired under another declaration: %+v", op)
	}
}

func TestReselectedDisabledRepositoryPlansVerifiedEnablement(t *testing.T) {
	c, resolved := repository(t)
	src, f := host(t)
	const id = "brave"
	r := c.Definitions().Repositories[id]
	f.Repositories.Value = append(f.Repositories.Value, facts.Repository{
		ID: "nimbus-" + id, File: "nimbus-" + id + ".repo", BaseURL: r.BaseURL,
		Enabled: false, GPGCheck: "1", GPGKey: "file://" + KeyPath(id),
	})
	b := &builder{in: Inputs{Root: c.Definitions(), Resolved: resolved, Facts: f, Source: src,
		Applied: &state.Applied{Receipts: map[string]state.Receipt{}}}, repos: map[string][]facts.Repository{},
		ready: map[string]bool{}, blockedRepo: map[string]string{}, pendingRepo: map[string]bool{}, duplicates: map[string][]string{}}
	for _, repo := range f.Repositories.Value {
		b.repos[repo.ID] = append(b.repos[repo.ID], repo)
	}
	for _, op := range b.repositories() {
		if op.ID != "repository:"+id {
			continue
		}
		var steps strings.Builder
		for _, step := range op.Steps {
			steps.WriteString(strings.Join(step.Argv, " ") + "\n")
		}
		if op.Action != ActionRepair || op.Blocked != "" || !strings.Contains(steps.String(), "nimbus-brave.enabled=1") || !strings.Contains(steps.String(), "--overwrite") {
			t.Fatalf("disabled source cannot converge when reselected: %+v", op)
		}
		return
	}
	t.Fatal("missing re-enable operation")
}
