package plan

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/state"
)

func identityBuilder(packages []facts.Package, desired []definitions.ResolvedPackage, receipts map[string]state.Receipt) (*builder, *facts.FakeSource) {
	src := &facts.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}}
	return &builder{in: Inputs{Resolved: &definitions.Resolved{Packages: desired}, Facts: &facts.Facts{Packages: facts.Section[[]facts.Package]{Value: packages}}, Applied: &state.Applied{Receipts: receipts}, Source: src}, ready: map[string]bool{}}, src
}

func TestQualifiedPackageConvergesAndOwnsOnlyItsArchitecture(t *testing.T) {
	pkgs := []facts.Package{{Name: "driver-libs", Arch: "x86_64", Version: "1", Release: "1"}, {Name: "driver-libs", Arch: "i686", Version: "1", Release: "1"}}
	selected := []definitions.ResolvedPackage{{Name: "driver-libs.i686", Prefix: "dnf", Canonical: "dnf:driver-libs.i686"}}
	b, src := identityBuilder(pkgs, selected, nil)
	ops := b.packages()
	if len(ops) != 1 || ops[0].Action != ActionAdopt || ops[0].Resolved["driver-libs.i686"] != "driver-libs.i686" {
		t.Fatalf("adoption: %+v", ops)
	}
	id := ops[0].ID
	b.in.Applied.Receipts = map[string]state.Receipt{id: {Resource: id, Provider: "dnf", Package: "driver-libs.i686"}}
	if ops = b.packages(); len(ops) != 1 || ops[0].Action != ActionKeep {
		t.Fatalf("repeat: %+v", ops)
	}
	b.in.Resolved.Packages = nil
	src.Commands[facts.Key("dnf5", "--assumeno", "--cacheonly", "remove", "driver-libs.i686")] = previewText([]TxPackage{{Name: "driver-libs", Arch: "i686", EVR: "1-1", Section: "removing", Repository: "@System"}})
	ops = b.ownedRemovals()
	if len(ops) != 1 || ops[0].Blocked != "" || strings.Join(ops[0].Steps[0].Argv, " ") != "dnf5 -y remove driver-libs.i686" {
		t.Fatalf("removal: %+v", ops)
	}
}

func TestLegacyMultilibOwnershipBlocksRemovalAndMigration(t *testing.T) {
	id := "package:dnf:driver-libs"
	b, _ := identityBuilder([]facts.Package{{Name: "driver-libs", Arch: "x86_64"}, {Name: "driver-libs", Arch: "i686"}}, nil, map[string]state.Receipt{id: {Resource: id, Provider: "dnf"}})
	ops := b.ownedRemovals()
	if len(ops) != 1 || !strings.Contains(ops[0].Blocked, "architecture") {
		t.Fatalf("ambiguous legacy removal: %+v", ops)
	}
	b.in.Resolved.Packages = []definitions.ResolvedPackage{{Name: "driver-libs", Prefix: "dnf", Canonical: "dnf:driver-libs"}}
	ops = b.packages()
	if len(ops) != 1 || !strings.Contains(ops[0].Blocked, "architecture") {
		t.Fatalf("ambiguous adoption: %+v", ops)
	}
	b.in.Resolved.Packages = []definitions.ResolvedPackage{{Name: "driver-libs.x86_64", Prefix: "dnf", Canonical: "dnf:driver-libs.x86_64"}, {Name: "driver-libs.i686", Prefix: "dnf", Canonical: "dnf:driver-libs.i686"}}
	if ops = b.ownedRemovals(); len(ops) != 1 || ops[0].Action != ActionRetire || ops[0].Blocked != "" {
		t.Fatalf("explicit architecture migration: %+v", ops)
	}
}

func TestProvideReceiptConvergesAndRemovesNativePackage(t *testing.T) {
	id := "package:dnf:virtual-tool"
	b, src := identityBuilder([]facts.Package{{Name: "actual-tool", Arch: "x86_64"}}, []definitions.ResolvedPackage{{Name: "virtual-tool", Prefix: "dnf", Canonical: "dnf:virtual-tool"}}, map[string]state.Receipt{id: {Resource: id, Provider: "dnf", Package: "actual-tool.x86_64"}})
	if ops := b.packages(); len(ops) != 1 || ops[0].Action != ActionKeep {
		t.Fatalf("provide repeated install: %+v", ops)
	}
	b.in.Resolved.Packages = nil
	src.Commands[facts.Key("dnf5", "--assumeno", "--cacheonly", "remove", "actual-tool.x86_64")] = previewText([]TxPackage{{Name: "actual-tool", Arch: "x86_64", EVR: "1-1", Section: "removing", Repository: "@System"}})
	ops := b.ownedRemovals()
	if len(ops) != 1 || ops[0].Blocked != "" || ops[0].Steps[0].Argv[3] != "actual-tool.x86_64" {
		t.Fatalf("provide removal: %+v", ops)
	}
}
