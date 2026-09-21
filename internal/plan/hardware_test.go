package plan

import (
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

func TestHardwareProposesComponents(t *testing.T) {
	components := map[string]*definitions.Component{
		"amd-graphics":    {Detect: &definitions.Detect{DisplayVendor: "1002"}},
		"intel-graphics":  {Detect: &definitions.Detect{DisplayVendor: "8086"}},
		"nvidia":          {Detect: &definitions.Detect{DisplayVendor: "10de"}},
		"laptop-power":    {Detect: &definitions.Detect{Chassis: "laptop"}},
		"desktop-display": {Detect: &definitions.Detect{Chassis: "desktop"}},
	}
	laptop := inspect.Hardware{Product: "HP EliteBook X G1a 14 inch Notebook Next Gen AI PC", Board: "8CB1", Chassis: "laptop", Display: []inspect.PCIDevice{{Vendor: "1002", Device: "150e"}}}
	if got := strings.Join(ProposeComponents(components, laptop), " "); got != "amd-graphics laptop-power" {
		t.Fatalf("laptop proposal = %q", got)
	}
	desktop := inspect.Hardware{Product: "MS-7D32", Board: "MAG Z690 TOMAHAWK WIFI (MS-7D32)", Chassis: "desktop", Display: []inspect.PCIDevice{{Vendor: "8086", Device: "4680"}, {Vendor: "10de", Device: "2206"}}}
	if got := strings.Join(ProposeComponents(components, desktop), " "); got != "desktop-display intel-graphics nvidia" {
		t.Fatalf("desktop proposal = %q", got)
	}
	// A virtual machine: no chassis kind, a virtio display, no known board.
	vm := inspect.Hardware{Product: "Standard PC (Q35 + ICH9, 2009)", Board: "", Chassis: "", Display: []inspect.PCIDevice{{Vendor: "1af4", Device: "1050"}}}
	if got := ProposeComponents(components, vm); len(got) != 0 {
		t.Fatalf("vm proposal = %v", got)
	}
}
