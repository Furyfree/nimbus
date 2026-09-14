package plan

import (
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
	"github.com/Furyfree/nimbus/internal/state"
)

func sourceBuilder() (*builder, *nativetest.FakeSource) {
	src := &nativetest.FakeSource{Commands: map[string][]byte{}, Failures: map[string]string{}}
	b := &builder{in: Inputs{Applied: &state.Applied{}, Root: definitions.Root{Repositories: map[string]definitions.Repository{"a": {Kind: "dnf"}, "b": {Kind: "dnf"}}}, Resolved: &definitions.Resolved{}, Facts: &inspect.Facts{}, Source: src}, repos: map[string][]inspect.Repository{}}
	for _, id := range []string{"fedora", "updates", "nimbus-a", "nimbus-b"} {
		b.repos[id] = []inspect.Repository{{ID: id, Enabled: true}}
	}
	b.repos["updates-testing"] = []inspect.Repository{{ID: "updates-testing", Enabled: false}}
	return b, src
}

func sourcePackage(prefix, name string) definitions.ResolvedPackage {
	return definitions.ResolvedPackage{Prefix: prefix, Name: name, Canonical: prefix + ":" + name}
}

func TestPackageSourceGroupsKeepDependenciesAvailable(t *testing.T) {
	b, _ := sourceBuilder()
	args, err := b.installSourceArgs([]definitions.ResolvedPackage{sourcePackage("b", "second"), sourcePackage("dnf", "base"), sourcePackage("a", "first")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"do", "--action=install", "--from-repo=fedora,updates", "base", "--from-repo=nimbus-a", "first", "--from-repo=nimbus-b", "second"}
	if !slices.Equal(args, want) {
		t.Fatalf("source groups: %v", args)
	}
	delete(b.repos, "nimbus-a")
	if _, err := b.installSourceArgs([]definitions.ResolvedPackage{sourcePackage("a", "first")}); err == nil {
		t.Fatal("missing enabled source must block rather than fall back")
	}
}

func TestPackageSourceChecksCoverDependenciesAndReplacements(t *testing.T) {
	rules := PackageSources{"steam.x86_64": {"rpmfusion-nonfree", "rpmfusion-nonfree-updates"}}
	for _, section := range []string{"installing", "installing dependencies", "installing weak dependencies", "upgrading", "downgrading", "reinstalling"} {
		t.Run(section, func(t *testing.T) {
			tx := &Transaction{Packages: []TxPackage{{Name: "steam", Arch: "x86_64", Repository: "nimbus-terra", Section: section}}}
			if err := rules.CheckTransaction(tx); err == nil {
				t.Fatal("selected package escaped its declared source")
			}
			tx.Packages[0].Repository = "rpmfusion-nonfree-updates"
			tx.Packages = append(tx.Packages, TxPackage{Name: "dependency", Arch: "x86_64", Repository: "fedora", Section: "installing dependencies"})
			if err := rules.CheckTransaction(tx); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := rules.CheckTransaction(&Transaction{Packages: []TxPackage{{Name: "steam", Arch: "x86_64", Section: SectionReplaced}}}); err == nil {
		t.Fatal("selected package replacement escaped source verification")
	}
	// A resolved provide cannot overwrite a conflicting direct selection.
	rules.add("steam.x86_64", []string{"nimbus-terra"})
	if err := rules.Check("steam", "x86_64", "nimbus-terra"); err == nil {
		t.Fatal("conflicting source selections must block")
	}
}

func TestSourceCorrectionUsesDowngradeOrSameVersionReinstall(t *testing.T) {
	for _, scenario := range []string{"downgrade", "same version", "unavailable", "wrong source"} {
		t.Run(scenario, func(t *testing.T) {
			b, src := sourceBuilder()
			pkg := sourcePackage("a", "steam")
			inst := inspect.Package{Name: "steam", Arch: "x86_64", Version: "2", Release: "1", FromRepo: "nimbus-b"}
			b.in.Resolved.Packages = []definitions.ResolvedPackage{pkg}
			b.in.Facts.Packages.Value = []inspect.Package{inst}
			distro := nativetest.Key("dnf5", "--assumeno", "--cacheonly", "distro-sync", "--from-repo=nimbus-a", "steam.x86_64")
			rows := []TxPackage{{Name: "steam", Arch: "x86_64", EVR: "1-1", Repository: "nimbus-a", Section: "downgrading"}, {Name: "steam", Arch: "x86_64", EVR: "2-1", Repository: "nimbus-b", Section: SectionReplaced}}
			src.Commands[distro] = previewText(rows)
			if scenario == "same version" {
				src.Commands[distro] = []byte("Repositories loaded.\nNothing to do.\n")
				rows[0].Section, rows[0].EVR = "reinstalling", "2-1"
				src.Commands[nativetest.Key("dnf5", "--assumeno", "--cacheonly", "reinstall", "--from-repo=nimbus-a", "steam.x86_64")] = previewText(rows)
			}
			if scenario == "unavailable" {
				delete(src.Commands, distro)
			}
			if scenario == "wrong source" {
				rows[0].Repository = "nimbus-b"
				src.Commands[distro] = previewText(rows)
			}
			op := b.repairPackageSource(pkg, inst)
			blocked := scenario == "unavailable" || scenario == "wrong source"
			if (op.Blocked != "") != blocked || op.Action != ActionRepair {
				t.Fatalf("repair blocked=%q action=%s", op.Blocked, op.Action)
			}
			if scenario == "same version" && !slices.Contains(op.Steps[0].Argv, "reinstall") {
				t.Fatal("same version requires reinstall")
			}
		})
	}
}

func TestUpgradeConstrainsSelectedNativeIdentities(t *testing.T) {
	b, src := sourceBuilder()
	b.in.Resolved.Packages = []definitions.ResolvedPackage{sourcePackage("a", "steam"), sourcePackage("b", "editor")}
	b.in.Facts.Packages.Value = []inspect.Package{{Name: "steam", Arch: "x86_64"}, {Name: "editor", Arch: "x86_64"}, {Name: "dependency", Arch: "i686"}, {Name: "dependency", Arch: "x86_64"}}
	u := b.sourceUpgrade()
	want := []string{"do", "--action=upgrade", "dependency.i686", "dependency.x86_64", "--from-repo=nimbus-a", "steam.x86_64", "--from-repo=nimbus-b", "editor.x86_64"}
	if !slices.Equal(u.Command, want) {
		t.Fatalf("upgrade: %v", u.Command)
	}
	key := nativetest.Key("dnf5", append([]string{"--cacheonly", "--assumeno"}, want...)...)
	src.Commands[key] = previewText([]TxPackage{{Name: "steam", Arch: "x86_64", EVR: "2-1", Repository: "nimbus-b", Section: "upgrading"}})
	if u := b.sourceUpgrade(); !strings.Contains(u.Unavailable, "repository nimbus-b") {
		t.Fatalf("wrong-source upgrade accepted: %q", u.Unavailable)
	}
	src.Commands[key] = []byte("Repositories loaded.\nNothing to do.\n")
	if u := b.sourceUpgrade(); u.Unavailable != "" || !u.Transaction.NothingToDo {
		t.Fatalf("current update: %+v", u)
	}
}

func TestDisabledSourceCannotPassProvenanceVerification(t *testing.T) {
	b, _ := sourceBuilder()
	b.in.Resolved.Packages = []definitions.ResolvedPackage{sourcePackage("dnf", "demo")}
	if err := b.packageSources().Check("demo", "x86_64", "updates-testing"); err == nil {
		t.Fatal("disabled testing source must not be accepted")
	}
}
