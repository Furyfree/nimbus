package facts

import (
	"fmt"
	"slices"
	"strings"
)

// CheckPlatform refuses mutation when the platform is unknown or unsupported.
func CheckPlatform(src Source, releases []string) error {
	p, err := platform(src)
	if err != nil {
		return fmt.Errorf("platform is unknown: %w", err)
	}
	if p.ID != "fedora" || p.Arch != "x86_64" || !slices.Contains(releases, p.VersionID) {
		return fmt.Errorf("unsupported platform %s %s on %s; this checkout supports Fedora %s on x86_64", p.ID, p.VersionID, p.Arch, strings.Join(releases, ", "))
	}
	return nil
}
