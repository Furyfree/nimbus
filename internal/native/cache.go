package native

import "slices"

// PrivilegedCache keeps a freshly refreshed upgrade plan on the same root DNF
// cache used by its transaction. It is used only after explicit authentication.
type PrivilegedCache struct{ Source }

func (s PrivilegedCache) Run(name string, args ...string) ([]byte, error) {
	if name == "dnf5" && slices.Contains(args, "--cacheonly") {
		return s.Source.Run("sudo", append([]string{name}, args...)...)
	}
	return s.Source.Run(name, args...)
}
