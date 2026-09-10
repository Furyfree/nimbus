package plan

import (
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func constraintBuilder() (*builder, *nativetest.FakeSource, inspect.SystemFile) {
	b, src := resourceBuilder()
	src.Dirs["/etc"] = []string{"dnf"}
	src.Dirs["/etc/dnf"] = nil
	src.Commands[nativetest.Key("stat", "--format=%F|%U|%G|%a|%h", "--", "/etc/dnf")] = []byte("directory|root|root|755|1")
	want := inspect.SystemFile{Exists: true, Content: []byte("version = \"1.0\"\n"), Owner: "root", Group: "root", Mode: "0644"}
	b.in.Resolved.Files = []definitions.ResolvedFile{{Target: definitions.VersionlockPath, Content: want.Content, Owner: want.Owner, Group: want.Group, Mode: want.Mode}}
	b.in.Resolved.Constraints = []definitions.PackageConstraint{{Package: "dnf:hyprland", Name: "hyprland", Family: "0.56.*"}}
	return b, src, want
}

func answerConstraintFile(src *nativetest.FakeSource, value inspect.SystemFile) {
	answerFile(src, definitions.VersionlockPath, value)
	src.Dirs["/etc"] = []string{"dnf"}
	src.Dirs["/etc/dnf"] = []string{"versionlock.toml"}
}

func TestConstraintOwnershipAndRemoval(t *testing.T) {
	for _, mode := range []string{"new", "foreign", "foreign matching", "owned", "owned edited", "owned missing", "remove", "remove edited", "foreign receipt"} {
		t.Run(mode, func(t *testing.T) {
			b, src, want := constraintBuilder()
			if mode != "new" && mode != "owned missing" {
				answerConstraintFile(src, want)
			}
			if strings.HasPrefix(mode, "owned") || strings.HasPrefix(mode, "remove") || mode == "foreign receipt" {
				r := fileReceipt(definitions.VersionlockPath, want)
				if mode == "foreign receipt" {
					r.Machine = "other"
				}
				b.in.Applied.Receipts[r.Resource] = r
			}
			if mode == "foreign" || strings.HasSuffix(mode, "edited") {
				want.Content = []byte("foreign rules")
				answerConstraintFile(src, want)
			}
			if strings.HasPrefix(mode, "remove") {
				b.in.Resolved.Files = nil
				b.in.Resolved.Constraints = nil
			}
			ops := b.constraintOperations()
			if len(ops) != 1 {
				t.Fatalf("operations %+v", ops)
			}
			op := ops[0]
			blocked := strings.HasPrefix(mode, "foreign") || strings.HasSuffix(mode, "edited")
			if (op.Blocked != "") != blocked {
				t.Fatalf("operation %+v", op)
			}
			if !blocked {
				wantAction := map[string]string{"new": ActionInstall, "owned": ActionKeep, "owned missing": ActionRepair, "remove": ActionRemove}[mode]
				if op.Action != wantAction {
					t.Fatalf("action %s, want %s", op.Action, wantAction)
				}
				if mode == "remove" && op.File.After.Exists {
					t.Fatal("removal did not restore absent prior state")
				}
			}
			if op.Action != ActionKeep && !slices.Contains(b.repoChanges, op.ID) {
				t.Fatal("native transaction would run before policy preparation")
			}
			if extra := b.systemResources(ops); len(extra) != 0 {
				t.Fatalf("duplicate resource operations %+v", extra)
			}
		})
	}
}

func TestConstraintUpdatePreviewUsesNativeTransaction(t *testing.T) {
	b, src, _ := constraintBuilder()
	b.repoChanges = []string{"file:" + definitions.VersionlockPath}
	if u := b.updates(); u.Unavailable == "" {
		t.Fatal("preview used unapplied locks")
	}
	b.repoChanges = nil
	src.Commands[nativetest.Key("dnf5", "--cacheonly", "--assumeno", "upgrade")] = []byte("Package Arch Version Repository Size\nUpgrading:\n hyprland x86_64 0:0.56.2-1.fc44 copr 1 MiB\nTransaction Summary:\n Upgrading: 1 package\nOperation aborted by the user.\n")
	u := b.updates()
	if u.Unavailable != "" || len(u.Available) != 1 || u.Available[0].EVR != "0:0.56.2-1.fc44" {
		t.Fatalf("updates %+v", u)
	}
	src.Commands[nativetest.Key("dnf5", "--cacheonly", "--assumeno", "upgrade")] = []byte("Failed to resolve the transaction:\nProblem: nothing provides needed ABI\n")
	if u := b.updates(); !strings.Contains(u.Unavailable, "needed ABI") {
		t.Fatalf("solver conflict %+v", u)
	}
}

func TestConstraintStableFilterPreservesDeclaredOptions(t *testing.T) {
	root := definitions.Root{DNF: map[string]any{"excludepkgs": "foreign*", "install_weak_deps": false}}
	c := definitions.PackageConstraint{Name: "hyprland", Family: "0.56.*"}
	got := DNFDropIn(root, c)
	if !strings.Contains(got, "excludepkgs=foreign*,hyprland-0:*[!0-9.]*-*") || root.DNF["excludepkgs"] != "foreign*" {
		t.Fatalf("drop-in %s, source %+v", got, root.DNF)
	}
	if strings.Contains(DNFDropIn(root), "hyprland") {
		t.Fatal("removal retained generated exclusion")
	}
}

func TestDNFConfigurationChangesDeferPreviewWithoutConstraints(t *testing.T) {
	c, resolved := repository(t)
	src, f := readyHost(t, c)
	f.DNFDropIn.Value = "[main]\nexcludepkgs=hyprland*\n"
	if len(resolved.Constraints) != 0 {
		t.Fatal("this regression requires unconstrained definitions")
	}
	p, err := Build(Inputs{Resolved: resolved, Root: c.Definitions(), Facts: f, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	op := find(p, "packages:install")
	if op == nil || op.After != "dnf:config" || op.Transaction != nil || op.Blocked != "" {
		t.Fatalf("transaction must wait for the approved native settings: %+v", op)
	}
}
