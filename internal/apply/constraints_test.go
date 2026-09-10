package apply

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/plan"
)

func TestUpgradeVerifiesConstraintAfterNativeSuccess(t *testing.T) {
	for _, version := range []string{"0.56.2", "0.56.3~rc1", "0.57.0", "0.55.3", ""} {
		t.Run(version, func(t *testing.T) {
			src := &packageSource{scripted: newScripted(), packages: []inspect.Package{rpm("hyprland", "x86_64", "0.55.3")}}
			if version != "" {
				src.after = []inspect.Package{rpm("hyprland", "x86_64", version)}
			}
			opts := options(t, src.scripted, t.TempDir())
			opts.Source, opts.Out = src, io.Discard
			opts.Constraints = []definitions.PackageConstraint{{Name: "hyprland", Family: "0.56.*"}}
			r := Upgrade(opts, opts.Root)
			valid := version == "0.56.2"
			if (r.Error == "") != valid || slices.Contains(r.Executed, "upgrade:dnf") != valid {
				t.Fatalf("result %+v", r)
			}
			if !valid && r.Failed != "upgrade:dnf" {
				t.Fatalf("failure %+v", r)
			}
		})
	}
}

func TestInstallOutsideConstraintGetsNoSuccessReceipt(t *testing.T) {
	src := &packageSource{scripted: newScripted(), after: []inspect.Package{rpm("hyprland", "x86_64", "0.57.0")}}
	opts := options(t, src.scripted, t.TempDir())
	opts.Source, opts.Out = src, io.Discard
	opts.Constraints = []definitions.PackageConstraint{{Name: "hyprland", Family: "0.56.*"}}
	op := plan.Operation{ID: "packages:install", Kind: plan.KindPackage, Action: plan.ActionInstall, Items: []string{"dnf:hyprland"}, Resolved: map[string]string{"hyprland": "hyprland.x86_64"}, Steps: []plan.Step{{Argv: []string{"dnf5", "-y", "install", "hyprland"}}}}
	op.Transaction = &plan.Transaction{Packages: []plan.TxPackage{{Name: "hyprland", Arch: "x86_64", EVR: "0.56.2-1", Section: "installing"}}}
	ex := &executor{p: &plan.Plan{}, opts: opts, seen: map[string]inspect.Package{}}
	receipts, _, err := ex.installTransaction(op)
	if err == nil || !strings.Contains(err.Error(), "outside constraint") || len(receipts) != 0 {
		t.Fatalf("receipts %+v, error %v", receipts, err)
	}
}
