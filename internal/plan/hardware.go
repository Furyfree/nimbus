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

// MatchMachine returns the one tracked machine whose declared hardware
// identity appears in the DMI product or board name, or "" when none or
// more than one does. A match is a default for init, never a decision.
func MatchMachine(machines map[string]*definitions.Machine, hw facts.Hardware) string {
	var matches []string
	for id, m := range machines {
		if m.Hardware == "" {
			continue
		}
		needle := strings.ToLower(m.Hardware)
		if strings.Contains(strings.ToLower(hw.Product), needle) || strings.Contains(strings.ToLower(hw.Board), needle) {
			matches = append(matches, id)
		}
	}
	if len(matches) != 1 {
		return ""
	}
	return matches[0]
}
