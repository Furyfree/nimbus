package plan

import (
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

// ProposeComponents returns the components whose detection rule matches the
// observed hardware, sorted. It proposes; init lets the owner decide.
func ProposeComponents(components map[string]*definitions.Component, hw inspect.Hardware) []string {
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
	slices.Sort(ids)
	return ids
}

func hasDisplayVendor(hw inspect.Hardware, vendor string) bool {
	return slices.ContainsFunc(hw.Display, func(dev inspect.PCIDevice) bool {
		return strings.EqualFold(dev.Vendor, vendor)
	})
}
