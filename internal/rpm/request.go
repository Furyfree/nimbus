// Package rpm describes native RPM request syntax.
package rpm

import "strings"

// SplitRequest recognizes architecture qualifiers supported by the
// Fedora target without splitting dots that belong to a package's name.
func SplitRequest(request string) (name, arch string) {
	for _, suffix := range []string{"x86_64", "i686", "i586", "i486", "i386", "noarch", "aarch64", "ppc64le", "s390x"} {
		if name, ok := strings.CutSuffix(request, "."+suffix); ok {
			return name, suffix
		}
	}
	return request, ""
}
