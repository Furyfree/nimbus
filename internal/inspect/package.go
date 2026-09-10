package inspect

import (
	"github.com/Furyfree/nimbus/internal/rpm"
)

// PackageID is the native RPM name and architecture, independent of version.
func PackageID(name, arch string) string {
	if arch == "" {
		return name
	}
	return name + "." + arch
}

func (p Package) ID() string { return PackageID(p.Name, p.Arch) }

func (p Package) Matches(request string) bool {
	name, arch := rpm.SplitRequest(request)
	return p.Name == name && (arch == "" || p.Arch == arch)
}

// FindPackage prefers the target's native architecture for a bare name.
// Qualified requests only match that exact architecture.
func FindPackage(packages []Package, request string) (Package, bool) {
	var found Package
	ok := false
	for _, p := range packages {
		if !p.Matches(request) {
			continue
		}
		priority := func(arch string) int {
			switch arch {
			case "x86_64":
				return 2
			case "noarch":
				return 1
			default:
				return 0
			}
		}
		if !ok || priority(p.Arch) > priority(found.Arch) || (priority(p.Arch) == priority(found.Arch) && p.ID()+" "+p.EVR() > found.ID()+" "+found.EVR()) {
			found, ok = p, true
		}
	}
	return found, ok
}

// PackageMap preserves parallel architectures and installonly versions.
func PackageMap(packages []Package) map[string]Package {
	out := make(map[string]Package, len(packages))
	for _, p := range packages {
		out[p.ID()+" "+p.EVR()] = p
	}
	return out
}
