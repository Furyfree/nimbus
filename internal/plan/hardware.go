package plan

import (
	"sort"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
)

// ProposeComponents returns the components whose detection rule matches the
// observed hardware, sorted. It proposes; init lets the owner decide.
func ProposeComponents(components map[string]*definitions.Component, hw facts.Hardware) []string {
	var ids []string
	for id, c := range components {
		d := c.Detect
		if d == nil {
			continue
		}
		if d.Chassis != "" && d.Chassis != hw.Chassis {
			continue
		}
		if d.DisplayVendor != "" && !hasDisplayVendor(hw, d.DisplayVendor) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func hasDisplayVendor(hw facts.Hardware, vendor string) bool {
	for _, dev := range hw.Display {
		if strings.EqualFold(dev.Vendor, vendor) {
			return true
		}
	}
	return false
}
