package plan

import (
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
)

func TestDNFDropInIsPlannedFirstAndVerifiedWhole(t *testing.T) {
	c, r := repository(t)
	rendered := DNFDropIn(c.Definitions())
	if !strings.HasPrefix(rendered, "# Written by Nimbus") || !strings.Contains(rendered, "[main]\ndefaultyes=True\nfastestmirror=True\nmax_parallel_downloads=20\n") {
		t.Fatalf("rendered drop-in:\n%s", rendered)
	}
	build := func(have string, managed bool) *Operation {
		src, f := host(t)
		if have != "" {
			f.DNFDropIn.Value = have
		}
		in := Inputs{Resolved: r, Root: c.Definitions(), Definitions: c.Digest(), Facts: f, Source: src}
		if managed {
			in.Applied = applied("dnf:config")
		}
		p, err := Build(in)
		if err != nil {
			t.Fatal(err)
		}
		if p.Operations[0].ID != "dnf:config" && find(p, "dnf:config") != nil {
			t.Fatalf("the drop-in is not the first operation: %s", p.Operations[0].ID)
		}
		return find(p, "dnf:config")
	}
	if op := build("", false); op == nil || op.Action != ActionInstall || op.Steps[len(op.Steps)-1].Argv[0] != "install" || !slices.Contains(op.Steps[len(op.Steps)-1].Argv, inspect.DNFDropInPath) || op.Steps[0].Description != "set defaultyes=True" {
		t.Fatalf("absent = %+v", op)
	}
	if op := build("[main]\nmax_parallel_downloads=3\n", true); op == nil || op.Action != ActionRepair {
		t.Fatalf("differs = %+v", op)
	}
	if op := build(rendered, false); op == nil || op.Action != ActionAdopt {
		t.Fatalf("as declared without receipt = %+v", op)
	}
	if op := build(rendered, true); op == nil || op.Action != ActionKeep {
		t.Fatalf("managed = %+v", op)
	}
	// Nothing declared: a managed file is removed, a foreign one is left.
	root := c.Definitions()
	root.DNF = nil
	src, f := host(t)
	f.DNFDropIn.Value = rendered
	p, _ := Build(Inputs{Resolved: r, Root: root, Definitions: c.Digest(), Facts: f, Source: src, Applied: applied("dnf:config")})
	if op := find(p, "dnf:config"); op == nil || op.Action != ActionRemove || op.Steps[0].Argv[0] != "rm" {
		t.Fatalf("undeclared and managed = %+v", op)
	}
	p, _ = Build(Inputs{Resolved: r, Root: root, Definitions: c.Digest(), Facts: f, Source: src})
	if find(p, "dnf:config") != nil {
		t.Fatal("a drop-in Nimbus never wrote was planned for removal")
	}
}
