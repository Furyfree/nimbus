package apply

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func rpm(name, arch, version string) facts.Package {
	return facts.Package{Name: name, Arch: arch, Version: version, Release: "1", FromRepo: "fedora"}
}

func TestTransactionComparisonKeepsVersionsAndArchitectures(t *testing.T) {
	tests := []struct {
		name          string
		before, after []facts.Package
		rows          []plan.TxPackage
		want          string
	}{
		{"wrong upgrade version", []facts.Package{rpm("lib", "x86_64", "1")}, []facts.Package{rpm("lib", "x86_64", "3")}, []plan.TxPackage{{Name: "lib", Arch: "x86_64", EVR: "2-1", Section: "upgrading"}}, "preview expected 2-1"},
		{"collateral architecture removal", []facts.Package{rpm("lib", "x86_64", "1"), rpm("lib", "i686", "1")}, []facts.Package{rpm("lib", "x86_64", "1")}, nil, "DNF also removed lib.i686"},
		{"retained old kernel", []facts.Package{rpm("kernel", "x86_64", "1"), rpm("kernel", "x86_64", "2")}, []facts.Package{rpm("kernel", "x86_64", "2")}, nil, "from 1-1, 2-1 to 2-1"},
		{"missing requested installation", nil, nil, []plan.TxPackage{{Name: "lib", Arch: "i686", EVR: "1-1", Section: "installing"}}, "lib.i686 is absent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := transactionDifferences(&plan.Transaction{Packages: tt.rows}, facts.PackageMap(tt.before), facts.PackageMap(tt.after))
			if !strings.Contains(strings.Join(diffs, "\n"), tt.want) {
				t.Fatalf("differences %v lack %q", diffs, tt.want)
			}
		})
	}
	tx := &plan.Transaction{Packages: []plan.TxPackage{{Name: "kernel", Arch: "x86_64", EVR: "3-1", Section: "installing"}, {Name: "kernel", Arch: "x86_64", EVR: "1-1", Section: "removing"}}}
	diffs := transactionDifferences(tx, facts.PackageMap([]facts.Package{rpm("kernel", "x86_64", "1"), rpm("kernel", "x86_64", "2")}), facts.PackageMap([]facts.Package{rpm("kernel", "x86_64", "2"), rpm("kernel", "x86_64", "3")}))
	if len(diffs) != 0 {
		t.Fatalf("expected installonly transaction: %v", diffs)
	}
}

type packageSource struct {
	*scripted
	packages, after      []facts.Package
	nativeErr, verifyErr error
	ranNative            bool
}

func (s *packageSource) Run(name string, args ...string) ([]byte, error) {
	if name == "dnf5" && len(args) > 2 && args[2] == "repoquery" {
		if s.ranNative && s.verifyErr != nil {
			return nil, s.verifyErr
		}
		out := []byte{}
		for _, p := range s.packages {
			out = fmt.Appendf(out, "%s|%s|%s|%s|%s|%s|user\n", p.Name, p.Epoch, p.Version, p.Release, p.Arch, p.FromRepo)
		}
		return out, nil
	}
	return s.scripted.Run(name, args...)
}
func (s *packageSource) Stream(out, errout io.Writer, name string, args ...string) error {
	if name == "sudo" && args[0] == "dnf5" {
		s.packages = s.after
		s.ranNative = true
		return s.nativeErr
	}
	return s.scripted.Stream(out, errout, name, args...)
}

func TestFailedNativeTransactionStillReportsPartialChanges(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			src := &packageSource{scripted: newScripted(), packages: []facts.Package{rpm("old", "i686", "1"), rpm("keep", "x86_64", "1")}, after: nil}
			if fail {
				src.nativeErr = errors.New("native transaction interrupted")
			}
			opts := options(t, src.scripted, t.TempDir())
			opts.Source = src
			opts.FirstApply = false
			opts.Out = io.Discard
			op := plan.Operation{ID: "packages:remove", Kind: plan.KindPackage, Action: plan.ActionRemove, Transaction: &plan.Transaction{Packages: []plan.TxPackage{{Name: "old", Arch: "i686", EVR: "1-1", Section: "removing"}}}, Steps: []plan.Step{{Argv: []string{"dnf5", "-y", "remove", "--no-autoremove", "old.i686"}}}}
			r := Run(&plan.Plan{Complete: true, Operations: []plan.Operation{op}}, opts)
			if (r.Error != "") != fail || !strings.Contains(strings.Join(r.Differences, "\n"), "DNF also removed keep.x86_64") {
				t.Fatalf("result: %+v", r)
			}
		})
	}
}

func TestInstallRecordsNativeIdentityAndVerificationFailure(t *testing.T) {
	src := &packageSource{scripted: newScripted(), after: []facts.Package{rpm("lib", "i686", "1")}}
	opts := options(t, src.scripted, t.TempDir())
	opts.Source = src
	opts.FirstApply = false
	opts.Out = io.Discard
	op := plan.Operation{ID: "packages:install", Kind: plan.KindPackage, Action: plan.ActionInstall, Items: []string{"dnf:lib.i686"}, Resolved: map[string]string{"lib.i686": "lib.i686"}, Transaction: &plan.Transaction{Packages: []plan.TxPackage{{Name: "lib", Arch: "i686", EVR: "1-1", Section: "installing"}}}, Steps: []plan.Step{{Argv: []string{"dnf5", "-y", "install", "lib.i686"}}}}
	ex := &executor{p: &plan.Plan{}, opts: opts, seen: map[string]facts.Package{}}
	opts.Now = func() time.Time { return time.Time{} }
	ex.opts.Now = opts.Now
	receipts, _, err := ex.installTransaction(op)
	if err != nil || len(receipts) != 1 || receipts[0].Package != "lib.i686" {
		t.Fatalf("receipt: %+v, %v", receipts, err)
	}
	src.ranNative = false
	src.verifyErr = errors.New("package database unavailable")
	r := Run(&plan.Plan{Complete: true, Operations: []plan.Operation{op}}, opts)
	if r.Error == "" || !strings.Contains(strings.Join(r.Differences, "\n"), "verification incomplete") {
		t.Fatalf("result: %+v", r)
	}
}

func TestIndependentCargoInstallsContinueAfterFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := newScripted()
	src.Dirs[home+"/.cargo/bin"] = []string{"cargo"}
	src.fail[home+"/.cargo/bin/cargo install broken"] = "build failed"
	opts := options(t, src, t.TempDir())
	opts.FirstApply = false
	var ops []plan.Operation
	for _, crate := range []string{"broken", "working"} {
		ops = append(ops, plan.Operation{ID: "package:cargo:" + crate, Kind: plan.KindUser, Action: plan.ActionInstall, Steps: []plan.Step{{Argv: []string{plan.HomeDir + "/.cargo/bin/cargo", "install", crate}}}})
	}
	r := Run(&plan.Plan{Complete: true, Operations: ops}, opts)
	if len(r.Failures) != 1 || r.Failures[0].ID != "package:cargo:broken" || len(r.Executed) != 1 || r.Executed[0] != "package:cargo:working" {
		t.Fatalf("result: %+v", r)
	}
}

func TestUpgradeReportsExtraDependencyAndPartialFailure(t *testing.T) {
	src := &packageSource{scripted: newScripted(), packages: []facts.Package{rpm("tool", "x86_64", "1")}, after: []facts.Package{rpm("tool", "x86_64", "2"), rpm("extra", "i686", "1")}, nativeErr: errors.New("interrupted after changes")}
	opts := options(t, src.scripted, t.TempDir())
	opts.Source = src
	opts.UpgradePreview = &plan.Transaction{Packages: []plan.TxPackage{{Name: "tool", Arch: "x86_64", EVR: "2-1", Section: "upgrading", Repository: "fedora"}}}
	r := Upgrade(opts, opts.Root)
	if len(r.Failures) != 1 || r.Failures[0].ID != "upgrade:dnf" || !strings.Contains(strings.Join(r.Differences, "\n"), "DNF also installed extra.i686") {
		t.Fatalf("result: %+v", r)
	}
}

func TestAbsentRetirementRechecksInventoryAfterEarlierOperations(t *testing.T) {
	for _, kind := range []string{plan.KindPackage, plan.KindFlatpak} {
		for _, transition := range []string{"absent", "present", "unknown"} {
			t.Run(kind+"/"+transition, func(t *testing.T) {
				src := newScripted()
				id, provider, native := "package:dnf:virtual-tool", "dnf", "actual-tool.x86_64"
				if kind == plan.KindFlatpak {
					id, provider, native = "flatpak:org.example.Gone", "flatpak", ""
				}
				r := state.Receipt{Schema: state.ReceiptSchema, Resource: id, Provider: provider, Package: native,
					Machine: "vm", Operation: plan.ActionInstall, PlanDigest: "old", Verified: true}
				root := t.TempDir()
				if err := state.Record(root, "old", &state.Stage{Schema: state.Schema, PlanDigest: "old", Receipts: []state.Receipt{r}}); err != nil {
					t.Fatal(err)
				}
				applied, err := state.Read(root)
				if err != nil {
					t.Fatal(err)
				}
				// An earlier adoption completes after Run's initial inventory. The
				// retirement must inspect again before deleting its own receipt.
				desired := &definitions.Resolved{Machine: "vm", Packages: []definitions.ResolvedPackage{{Name: "bash", Prefix: "dnf", Canonical: "dnf:bash"}}}
				p, err := plan.Build(plan.Inputs{Resolved: desired, Facts: facts.Inspect(src, ""), Applied: applied, Source: src})
				if err != nil || !p.Complete || len(p.Operations) != 2 || p.Operations[1].Action != plan.ActionRetire {
					t.Fatalf("retirement plan: %+v %v", p, err)
				}
				opts := options(t, src, root)
				opts.FirstApply = false
				opts.Record = func(digest string, stage *state.Stage) error {
					if err := state.Record(root, digest, stage); err != nil {
						return err
					}
					if len(stage.Receipts) != 0 {
						if kind == plan.KindPackage {
							switch transition {
							case "present":
								src.installed = append(src.installed, "actual-tool")
							case "unknown":
								src.fail["dnf5"] = "package inventory unavailable"
							}
						} else {
							switch transition {
							case "present":
								src.apps = []string{"org.example.Gone"}
							case "unknown":
								src.fail["flatpak list"] = "app inventory unavailable"
							}
						}
					}
					return nil
				}
				result := Run(p, opts)
				after, err := state.Read(root)
				if err != nil {
					t.Fatal(err)
				}
				_, retained := after.Receipts[id]
				valid := transition == "absent"
				if (result.Error == "") != valid || retained == valid || slices.Contains(result.Executed, id) != valid {
					t.Fatalf("retirement after %s: %+v; receipt retained=%t", transition, result, retained)
				}
				if transition == "unknown" && !strings.Contains(result.Error, "inventory unavailable") {
					t.Fatalf("inspection failure lost: %+v", result)
				}
				if !after.Receipts["package:dnf:bash"].Verified || !slices.Contains(result.Executed, "package:dnf:bash") {
					t.Fatalf("earlier adoption lost: %+v receipts=%+v", result, after.Receipts)
				}
				if src.ran("sudo ") {
					t.Fatal("receipt-only retirement invoked a native mutation")
				}
			})
		}
	}
}

func TestRetirementPreservesQualifiedAndLegacyOwnershipTransfers(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "qualified selection", true: "legacy multilib"}[legacy], func(t *testing.T) {
			src := &packageSource{scripted: newScripted(), packages: []facts.Package{rpm("tool", "x86_64", "1")}}
			const oldID = "package:dnf:tool"
			r := state.Receipt{Schema: state.ReceiptSchema, Resource: oldID, Provider: "dnf", Package: "tool.x86_64",
				Machine: "vm", Operation: plan.ActionInstall, PlanDigest: "old", Verified: true}
			desired := []definitions.ResolvedPackage{{Name: "tool.x86_64", Prefix: "dnf", Canonical: "dnf:tool.x86_64"}}
			if legacy {
				r.Package = ""
				src.packages = append(src.packages, rpm("tool", "i686", "1"))
				desired = append(desired, definitions.ResolvedPackage{Name: "tool.i686", Prefix: "dnf", Canonical: "dnf:tool.i686"})
			}
			root := t.TempDir()
			if err := state.Record(root, "old", &state.Stage{Schema: state.Schema, PlanDigest: "old", Receipts: []state.Receipt{r}}); err != nil {
				t.Fatal(err)
			}
			applied, err := state.Read(root)
			if err != nil {
				t.Fatal(err)
			}
			p, err := plan.Build(plan.Inputs{Resolved: &definitions.Resolved{Machine: "vm", Packages: desired}, Facts: facts.Inspect(src, ""), Applied: applied, Source: src})
			if err != nil || !p.Complete {
				t.Fatalf("transfer plan: %+v %v", p, err)
			}
			if !slices.ContainsFunc(p.Operations, func(op plan.Operation) bool {
				return op.ID == oldID && op.Action == plan.ActionRetire && op.AbsentPackage == ""
			}) {
				t.Fatal("missing ownership transfer")
			}
			opts := options(t, src.scripted, root)
			opts.Source, opts.FirstApply = src, false
			result := Run(p, opts)
			after, err := state.Read(root)
			if err != nil || result.Error != "" || len(after.Receipts) != len(desired) {
				t.Fatalf("transfer: %+v receipts=%+v error=%v", result, after, err)
			}
			if _, retained := after.Receipts[oldID]; retained {
				t.Fatal("old ownership retained")
			}
			for _, pkg := range desired {
				if got := after.Receipts["package:"+pkg.Canonical]; got.Package != pkg.Name || !got.Verified {
					t.Fatalf("new ownership lost: %+v", got)
				}
			}
			if src.ran("sudo ") || src.ranNative {
				t.Fatal("ownership transfer mutated native packages")
			}
		})
	}
}
