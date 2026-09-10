package plan

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/inspect"
)

func TestHardwareProposesComponents(t *testing.T) {
	c := repositoryOnly(t)
	laptop := inspect.Hardware{Product: "HP EliteBook X G1a 14 inch Notebook Next Gen AI PC", Board: "8CB1", Chassis: "laptop", Display: []inspect.PCIDevice{{Vendor: "1002", Device: "150e"}}}
	if got := strings.Join(ProposeComponents(c.Components, laptop), " "); got != "amd-graphics laptop-power" {
		t.Fatalf("laptop proposal = %q", got)
	}
	desktop := inspect.Hardware{Product: "MS-7D32", Board: "MAG Z690 TOMAHAWK WIFI (MS-7D32)", Chassis: "desktop", Display: []inspect.PCIDevice{{Vendor: "8086", Device: "4680"}, {Vendor: "10de", Device: "2206"}}}
	if got := strings.Join(ProposeComponents(c.Components, desktop), " "); got != "desktop-display intel-graphics nvidia" {
		t.Fatalf("desktop proposal = %q", got)
	}
	// A virtual machine: no chassis kind, a virtio display, no known board.
	vm := inspect.Hardware{Product: "Standard PC (Q35 + ICH9, 2009)", Board: "", Chassis: "", Display: []inspect.PCIDevice{{Vendor: "1af4", Device: "1050"}}}
	if got := ProposeComponents(c.Components, vm); len(got) != 0 {
		t.Fatalf("vm proposal = %v", got)
	}
}
