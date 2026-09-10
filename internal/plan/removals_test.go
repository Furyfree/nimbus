package plan

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestPlanRemovesDeclaredPackages(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	// ffmpeg-free is installed on this host, so media-codecs must swap it.
	f.Packages.Value = append(f.Packages.Value, inspect.Package{Name: "ffmpeg-free", Epoch: "0", Version: "8.0.1", Release: "6.fc44", Arch: "x86_64", FromRepo: "fedora", Reason: "user"})
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
	key := nativetest.Key("dnf5", "--assumeno", "--cacheonly", "remove", "--no-autoremove", "ffmpeg-free")
	src.Commands[key] = previewText([]TxPackage{{Name: "ffmpeg-free", Arch: "x86_64", EVR: "0:8.0.1-6.fc44", Repository: "@System", Section: "removing"}})
	src.Failures[key] = "exit status 1"
	p := answerInstall(t, src, in, nil)
	op := find(p, "packages:remove")
	if op == nil || op.Blocked != "" || op.Risk != RiskMedium || strings.Join(op.Steps[0].Argv, " ") != "dnf5 -y remove --no-autoremove ffmpeg-free" {
		t.Fatalf("remove = %+v", op)
	}
	if find(p, "package:dnf:ffmpeg-free") != nil {
		t.Fatal("a removed package must not be adopted")
	}
	src.Commands[key] = previewText([]TxPackage{
		{Name: "ffmpeg-free", Arch: "x86_64", EVR: "0:8.0.1-6.fc44", Repository: "@System", Section: "removing"},
		{Name: "libavcodec-free", Arch: "x86_64", EVR: "0:8.0.1-6.fc44", Repository: "@System", Section: "removing unused dependencies"},
	})
	p, _ = Build(in)
	if op := find(p, "packages:remove"); op == nil || op.Blocked == "" || !strings.Contains(op.Blocked, "libavcodec-free") {
		t.Fatalf("extra removal accepted: %+v", op)
	}
	// When the install transaction already erases it, no second removal.
	erased := answerInstall(t, src, in, func(rows []TxPackage) []TxPackage {
		return append(rows, TxPackage{Name: "ffmpeg-free", Arch: "x86_64", EVR: "0:8.0.1-6.fc44", Repository: "@System", Section: "removing"})
	})
	if find(erased, "packages:remove") != nil || find(erased, "packages:install").Blocked != "" {
		t.Fatalf("erased removal planned twice: %+v", find(erased, "packages:remove"))
	}
}

func TestAppliedStateShapesThePlan(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	a := applied("package:dnf:dnf5-plugins", "package:dnf:no-longer-wanted")
	// Everything on the fixture host existed before Nimbus took over.
	var baseline []string
	for _, p := range f.Packages.Value {
		baseline = append(baseline, p.Name)
	}
	slices.Sort(baseline)
	a.Baseline = &state.Baseline{Schema: state.BaselineSchema, Packages: baseline}
	f.Packages.Value = append(f.Packages.Value,
		inspect.Package{Name: "no-longer-wanted", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "fedora", Reason: "user"},
		inspect.Package{Name: "hand-installed", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "fedora", Reason: "user"},
	)
	key := nativetest.Key("dnf5", "--assumeno", "--cacheonly", "remove", "--no-autoremove", "no-longer-wanted.x86_64")
	src.Commands[key] = previewText([]TxPackage{{Name: "no-longer-wanted", Arch: "x86_64", EVR: "0:1-1", Repository: "@System", Section: "removing"}})
	src.Failures[key] = "exit status 1"
	in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Applied: a, Source: src}
	p := answerInstall(t, src, in, nil)

	if op := find(p, "package:dnf:dnf5-plugins"); op == nil || op.Action != ActionKeep {
		t.Fatalf("managed package = %+v", op)
	}
	owned := find(p, "packages:remove-owned")
	if owned == nil || owned.Blocked != "" || strings.Join(owned.Steps[0].Argv, " ") != "dnf5 -y remove --no-autoremove no-longer-wanted.x86_64" || !slices.Contains(owned.Paths, "package:dnf:no-longer-wanted") {
		t.Fatalf("owned removal = %+v", owned)
	}
	names := map[string]bool{}
	for _, pr := range p.Prune {
		names[pr.Name] = true
	}
	if names["gzip.x86_64"] || names["no-longer-wanted.x86_64"] || !names["hand-installed.x86_64"] {
		t.Fatalf("prune buckets wrong: %+v", p.Prune)
	}
	if find(p, "packages:prune") != nil {
		t.Fatal("prune transaction planned without Prune")
	}

	pruneKey := nativetest.Key("dnf5", "--assumeno", "--cacheonly", "remove", "--no-autoremove", "hand-installed.x86_64")
	src.Commands[pruneKey] = previewText([]TxPackage{{Name: "hand-installed", Arch: "x86_64", EVR: "0:1-1", Repository: "@System", Section: "removing"}})
	src.Failures[pruneKey] = "exit status 1"
	in.Prune = true
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if op := find(p, "packages:prune"); op == nil || op.Action != ActionPrune || op.Blocked != "" || !slices.Contains(op.Paths, "unmanaged:hand-installed.x86_64") {
		t.Fatalf("prune transaction = %+v", op)
	}
	if find(p, "packages:remove-owned") == nil {
		t.Fatal("owned removal lost with Prune")
	}
}

func TestReceiptOfAnAbsentPackageIsRetired(t *testing.T) {
	c, r := repository(t)
	src, f := host(t)
	withoutTerraFile(f)
	a := applied("package:dnf:vanished")
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Applied: a, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	op := find(p, "package:dnf:vanished")
	if op == nil || op.Action != ActionRetire || len(op.Steps) != 0 || op.Blocked != "" {
		t.Fatalf("retire = %+v", op)
	}
	if find(p, "packages:remove-owned") != nil {
		t.Fatal("an absent package must not be removed")
	}
}

func TestAPrefixChangeRetiresTheOldReceiptAndAdoptsTheNew(t *testing.T) {
	c, r := repository(t)
	src, f := readyHost(t, c)
	// ghostty was recorded under dnf: and is now selected as terra:ghostty;
	// the package stays, the old receipt goes, the new identity is adopted.
	f.Packages.Value = append(f.Packages.Value, inspect.Package{Name: "ghostty", Version: "1", Release: "1", Arch: "x86_64", FromRepo: "nimbus-terra", Reason: "user"})
	a := applied("package:dnf:ghostty")
	a.Baseline = &state.Baseline{Schema: state.BaselineSchema, Packages: []string{"bash"}}
	p, err := Build(Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src, Applied: a})
	if err != nil {
		t.Fatal(err)
	}
	if op := find(p, "package:dnf:ghostty"); op == nil || op.Action != ActionRetire {
		t.Fatalf("old receipt = %+v", op)
	}
	if op := find(p, "package:terra:ghostty"); op == nil || op.Action != ActionAdopt || len(op.Notes) != 0 {
		t.Fatalf("new identity = %+v", op)
	}
	if op := find(p, "packages:remove-owned"); op != nil {
		t.Fatalf("the package must not be removed: %+v", op)
	}
}

func TestRemovedProvidersPreserveExistingState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		receipt state.Receipt
	}{
		{"Copilot", state.Receipt{Resource: "package:copilot:github", Provider: "dnf", Package: "github.x86_64", Machine: "vm", Verified: true}},
		{"foreign Copilot", state.Receipt{Resource: "package:copilot:github", Provider: "dnf", Package: "github.x86_64", Machine: "other"}},
		{"WoWUp", state.Receipt{Resource: "package:appimage:wowup-cf", Provider: "appimage", Package: "wowup-cf", Machine: "vm", Verified: true}},
		{"Cargo", state.Receipt{Resource: "package:cargo:demo", Provider: "cargo", Package: "demo", Machine: "vm", Verified: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, f := host(t)
			f.Packages.Value = []inspect.Package{{Name: "github", Arch: "x86_64", Reason: "user"}}
			if tc.receipt.Provider != "dnf" {
				f.Packages.Value = nil
			}
			a := &state.Applied{Receipts: map[string]state.Receipt{tc.receipt.Resource: tc.receipt}, Baseline: &state.Baseline{}}
			b := builder{in: Inputs{Resolved: &definitions.Resolved{Machine: "vm"}, Facts: f, Applied: a, Source: src}}
			if ops := b.ownedRemovals(); len(ops) != 0 {
				t.Fatalf("removed provider still planned application operations: %+v", ops)
			}

			if candidates := b.prune(); len(candidates) != 0 {
				t.Fatalf("legacy application would be pruned: %+v", candidates)
			}
			if !reflect.DeepEqual(a.Receipts[tc.receipt.Resource], tc.receipt) {
				t.Fatal("legacy receipt changed")
			}
		})
	}
}
