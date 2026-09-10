package definitions

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Furyfree/nimbus/internal/rpm"
)

// Ref is a parsed package reference using a reserved provider prefix or a
// repository ID from nimbus.toml. Bare Fedora names use "dnf".
type Ref struct {
	Prefix string
	Name   string
}

// Canonical is the provider-qualified identity, such as "dnf:ripgrep".
func (r Ref) Canonical() string { return r.Prefix + ":" + r.Name }

func conflictingSources(a, b Ref) bool {
	if a.Prefix == b.Prefix {
		return false
	}
	if a.Name == b.Name {
		return true
	}
	if a.Prefix == PrefixFlatpak || b.Prefix == PrefixFlatpak {
		return false
	}
	aName, aArch := rpm.SplitRequest(a.Name)
	bName, bArch := rpm.SplitRequest(b.Name)
	return aName == bName && (aArch == "" || bArch == "" || aArch == bArch)
}

// Reserved prefixes.
const (
	PrefixDNF     = "dnf"
	PrefixFlatpak = "flatpak"
)

// ValidateID checks a machine, profile, component, or repository ID: lowercase
// letters, digits, and dashes, starting with a letter or digit.
func ValidateID(id string) error {
	if !prefixRe.MatchString(id) {
		return fmt.Errorf("%q must be lowercase letters, digits, and dashes", id)
	}
	return nil
}

var (
	prefixRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	rpmNameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	flatpakIDRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*(\.[A-Za-z_][A-Za-z0-9_-]*){2,}$`)
)

// ParseRef parses one package reference. It checks the grammar only; whether
// a prefix names a declared repository is checked against nimbus.toml.
func ParseRef(raw string) (Ref, error) {
	if raw == "" {
		return Ref{}, fmt.Errorf("empty package reference")
	}
	if strings.TrimSpace(raw) != raw {
		return Ref{}, fmt.Errorf("package reference %q has surrounding whitespace", raw)
	}
	prefix, name, qualified := strings.Cut(raw, ":")
	if !qualified {
		if !rpmNameRe.MatchString(raw) {
			return Ref{}, fmt.Errorf("invalid Fedora package name %q", raw)
		}
		return Ref{Prefix: PrefixDNF, Name: raw}, nil
	}
	switch {
	case prefix == "":
		return Ref{}, fmt.Errorf("package reference %q has an empty prefix", raw)
	case name == "":
		return Ref{}, fmt.Errorf("package reference %q has an empty name", raw)
	case !prefixRe.MatchString(prefix):
		return Ref{}, fmt.Errorf("package reference %q has an invalid prefix", raw)
	}
	if prefix == PrefixFlatpak {
		if !flatpakIDRe.MatchString(name) {
			return Ref{}, fmt.Errorf("invalid Flatpak application ID %q", name)
		}
	} else if !rpmNameRe.MatchString(name) {
		return Ref{}, fmt.Errorf("invalid package name %q", name)
	}
	return Ref{Prefix: prefix, Name: name}, nil
}

// ParseRPMName accepts a bare RPM name, as used by component removes.
func ParseRPMName(raw string) error {
	if !rpmNameRe.MatchString(raw) {
		return fmt.Errorf("invalid package name %q", raw)
	}
	return nil
}
