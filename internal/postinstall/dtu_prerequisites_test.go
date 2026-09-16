package postinstall

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/state"
)

func TestDTURecordedBaselinePrerequisites(t *testing.T) {
	for _, mode := range []string{"baseline", "absent baseline", "legacy baseline", "wrong architecture", "unrecorded package", "missing package", "unknown packages", "invalid receipt", "foreign receipt", "receipt identity drift", "ambiguous package"} {
		t.Run(mode, func(t *testing.T) {
			in, src, _ := dtuFixture(t)
			in.Applied.Receipts = map[string]state.Receipt{}
			in.Applied.Baseline = &state.Baseline{Schema: state.BaselineSchema}
			for _, pkg := range in.Facts.Packages.Value {
				in.Applied.Baseline.Packages = append(in.Applied.Baseline.Packages, pkg.ID())
			}
			slices.Sort(in.Applied.Baseline.Packages)
			id := "package:dnf:NetworkManager-wifi"
			switch mode {
			case "absent baseline":
				in.Applied.Baseline = nil
			case "legacy baseline":
				in.Applied.Baseline.Schema = 1
			case "wrong architecture":
				in.Applied.Baseline.Packages = []string{"NetworkManager.i686"}
			case "unrecorded package":
				in.Applied.Baseline.Packages = slices.DeleteFunc(in.Applied.Baseline.Packages, func(p string) bool { return p == "python3-dbus.x86_64" })
			case "missing package":
				in.Facts.Packages.Value = slices.DeleteFunc(in.Facts.Packages.Value, func(p inspect.Package) bool { return p.Name == "python3" })
			case "unknown packages":
				in.Facts.Packages.Error = "unavailable"
			case "invalid receipt":
				in.Applied.Receipts[id] = state.Receipt{}
			case "foreign receipt":
				r := receipt(id, "dnf")
				r.Machine = "other"
				in.Applied.Receipts[id] = r
			case "receipt identity drift":
				r := receipt(id, "dnf")
				r.Package = "NetworkManager-wifi.i686"
				in.Applied.Receipts[id] = r
			case "ambiguous package":
				in.Facts.Packages.Value = append(in.Facts.Packages.Value, inspect.Package{Name: "NetworkManager-wifi", Arch: "noarch"})
			}
			before, err := json.Marshal(in.Applied)
			if err != nil {
				t.Fatal(err)
			}
			task := findTask(t, Inspect(src, in), "dtu-network")
			if mode == "baseline" {
				if task.Status != Pending || task.Action == nil {
					t.Fatalf("baseline prerequisites blocked: %+v", task)
				}
			} else if task.Action != nil || (task.Status != Blocked && task.Status != Unknown) {
				t.Fatalf("invalid prerequisite accepted: %+v", task)
			}
			after, err := json.Marshal(in.Applied)
			if err != nil || string(before) != string(after) {
				t.Fatal("inspection mutated applied state")
			}
		})
	}
}
