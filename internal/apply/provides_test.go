package apply

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestProvideReceiptsAndDeselectionUseNativeEvidence(t *testing.T) {
	for _, section := range []string{"Installing", "Installing dependencies", "Installing weak dependencies"} {
		t.Run(section, func(t *testing.T) {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprint(reverse), func(t *testing.T) {
					desired := &definitions.Resolved{Machine: "vm", Packages: []definitions.ResolvedPackage{
						{Canonical: "dnf:virtual-alpha", Prefix: "dnf", Name: "virtual-alpha"},
						{Canonical: "dnf:virtual-zulu", Prefix: "dnf", Name: "virtual-zulu"},
					}}
					names := []string{"native-a", "native-z"}
					if reverse {
						slices.Reverse(names)
					}
					var preview strings.Builder
					fmt.Fprintf(&preview, "Package Arch Version Repository Size\n%s:\n", section)
					for _, name := range names {
						fmt.Fprintf(&preview, " %s x86_64 1-1 fedora 1 KiB\n", name)
					}
					preview.WriteString("Transaction Summary:\n")
					src := &facts.FakeSource{Commands: map[string][]byte{
						facts.Key("dnf5", "--assumeno", "--cacheonly", "install", "virtual-alpha", "virtual-zulu"):                                                              []byte(preview.String()),
						facts.Key("dnf5", "--cacheonly", "check-upgrade"):                                                                                                       nil,
						facts.Key("dnf5", "--cacheonly", "repoquery", "--available", "--whatprovides", "virtual-alpha", "--queryformat", "%{name}|%{arch}|%{evr}|%{repoid}\\n"): []byte("native-z|x86_64|1-1|fedora\n"),
						facts.Key("dnf5", "--cacheonly", "repoquery", "--available", "--whatprovides", "virtual-zulu", "--queryformat", "%{name}|%{arch}|%{evr}|%{repoid}\\n"):  []byte("native-a|x86_64|1-1|fedora\n"),
					}}
					p, err := plan.Build(plan.Inputs{Resolved: desired, Facts: &facts.Facts{}, Source: src})
					if err != nil || !p.Complete {
						t.Fatalf("install plan=%+v error=%v", p, err)
					}
					native := &packageSource{scripted: newScripted(), after: []facts.Package{rpm("native-a", "x86_64", "1"), rpm("native-z", "x86_64", "1")}}
					root := t.TempDir()
					opts := options(t, native.scripted, root)
					opts.Source, opts.FirstApply, opts.Out = native, false, io.Discard
					if r := Run(p, opts); r.Error != "" || len(r.Differences) != 0 || !slices.Equal(r.Executed, []string{"packages:install"}) {
						t.Fatalf("install result: %+v", r)
					}
					applied, err := state.Read(root)
					if err != nil {
						t.Fatal(err)
					}
					for request, want := range map[string]string{"virtual-alpha": "native-z.x86_64", "virtual-zulu": "native-a.x86_64"} {
						if receipt := applied.Receipts["package:dnf:"+request]; !receipt.Verified || receipt.Package != want {
							t.Fatalf("%s receipt=%+v, want verified ownership of %s", request, receipt, want)
						}
					}
					desired.Packages = desired.Packages[:1]
					src.Commands[facts.Key("dnf5", "--assumeno", "--cacheonly", "remove", "--no-autoremove", "native-a.x86_64")] = []byte("Package Arch Version Repository Size\nRemoving:\n native-a x86_64 1-1 @System 1 KiB\nTransaction Summary:\n")
					next, err := plan.Build(plan.Inputs{Resolved: desired, Facts: facts.Inspect(native, ""), Source: src, Applied: applied})
					if err != nil || !next.Complete {
						t.Fatalf("deselection plan=%+v error=%v", next, err)
					}
					removal := slices.IndexFunc(next.Operations, func(op plan.Operation) bool { return op.ID == "packages:remove-owned" })
					if removal < 0 || !slices.Equal(next.Operations[removal].Steps[0].Argv, []string{"dnf5", "-y", "remove", "--no-autoremove", "native-a.x86_64"}) {
						t.Fatalf("wrong native removal: %+v", next)
					}
					native.after = []facts.Package{rpm("native-z", "x86_64", "1")}
					if r := Run(next, opts); r.Error != "" || len(r.Differences) != 0 {
						t.Fatalf("removal result: %+v", r)
					}
					remaining, err := state.Read(root)
					if err != nil {
						t.Fatal(err)
					}
					if len(remaining.Receipts) != 1 || remaining.Receipts["package:dnf:virtual-alpha"].Package != "native-z.x86_64" {
						t.Fatalf("wrong remaining ownership: %+v", remaining.Receipts)
					}
				})
			}
		})
	}
}

func TestMissingProvideEvidencePreventsApply(t *testing.T) {
	desired := &definitions.Resolved{Machine: "vm", Packages: []definitions.ResolvedPackage{{Canonical: "dnf:virtual-tool", Prefix: "dnf", Name: "virtual-tool"}}}
	src := &facts.FakeSource{Commands: map[string][]byte{
		facts.Key("dnf5", "--assumeno", "--cacheonly", "install", "virtual-tool"): []byte("Package Arch Version Repository Size\nInstalling:\n native-tool x86_64 1-1 fedora 1 KiB\nTransaction Summary:\n"),
		facts.Key("dnf5", "--cacheonly", "check-upgrade"):                         nil,
	}}
	p, err := plan.Build(plan.Inputs{Resolved: desired, Facts: &facts.Facts{}, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	native := newScripted()
	root := t.TempDir()
	result := Run(p, options(t, native, root))
	if p.Complete || result.Error == "" || native.ran("sudo ") {
		t.Fatalf("unproven provider reached apply: plan=%+v result=%+v calls=%v", p, result, native.log)
	}
	applied, err := state.Read(root)
	if err != nil || len(applied.Receipts) != 0 {
		t.Fatalf("unproven ownership recorded: %+v error=%v", applied, err)
	}
	if !strings.Contains(result.Error, "incomplete") {
		t.Fatalf("blocked plan error: %s", result.Error)
	}
}
