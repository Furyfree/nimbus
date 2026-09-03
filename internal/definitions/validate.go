package definitions

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	releaseRe     = regexp.MustCompile(`^[0-9]+$`)
	versionRe     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	fingerprintRe = regexp.MustCompile(`^[0-9A-F]{40}$`)
	sha256Re      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	coprRe        = regexp.MustCompile(`^[A-Za-z0-9_.@-]+/[A-Za-z0-9_.-]+$`)
	accountRe     = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)
	modeRe        = regexp.MustCompile(`^0[0-7]{3}$`)
)

// NormalizeFingerprint removes spaces and uppercases a key fingerprint.
func NormalizeFingerprint(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, " ", ""))
}

// Validate checks every definition in the checkout and resolves every
// machine. It returns every problem found.
func Validate(c *Checkout) ErrorList {
	var errs ErrorList
	validateRoot(c, &errs)
	for _, id := range sortedKeys(c.Profiles) {
		validateProfile(c, c.Profiles[id], &errs)
	}
	for _, id := range sortedKeys(c.Components) {
		validateComponent(c, c.Components[id], &errs)
	}
	validateRequireCycles(c, &errs)
	for _, id := range sortedKeys(c.Machines) {
		validateMachine(c, c.Machines[id], &errs)
	}
	if len(errs) > 0 {
		return errs
	}
	for _, id := range sortedKeys(c.Machines) {
		if _, rerrs := Resolve(c, id); len(rerrs) > 0 {
			errs = append(errs, rerrs...)
		}
	}
	return errs
}

func validateRoot(c *Checkout, errs *ErrorList) {
	r := c.Root_
	if r.Schema != CurrentSchema {
		errs.Add(RootFile, "schema %d is not supported; this engine reads schema %d", r.Schema, CurrentSchema)
	}
	if len(r.Compatibility.Fedora) == 0 {
		errs.Add(RootFile, "compatibility.fedora must list at least one release")
	}
	for _, rel := range r.Compatibility.Fedora {
		if !releaseRe.MatchString(rel) {
			errs.Add(RootFile, "compatibility.fedora entry %q is not a release number", rel)
		}
	}
	if !versionRe.MatchString(r.Compatibility.MinEngine) {
		errs.Add(RootFile, "compatibility.min_engine %q is not MAJOR.MINOR.PATCH", r.Compatibility.MinEngine)
	}
	flatpaks := 0
	for _, id := range sortedKeys(r.Repositories) {
		repo := r.Repositories[id]
		where := "repositories." + id
		if id == PrefixDNF || id == PrefixFlatpak {
			errs.Add(RootFile, "%s: %q is a reserved prefix", where, id)
		} else if !prefixRe.MatchString(id) {
			errs.Add(RootFile, "%s: invalid repository ID", where)
		}
		if !fingerprintRe.MatchString(NormalizeFingerprint(repo.Key)) {
			errs.Add(RootFile, "%s: key must be a 40-hex-digit fingerprint", where)
		}
		if repo.Priority < 0 {
			errs.Add(RootFile, "%s: priority must not be negative", where)
		}
		keySources := 0
		if repo.KeyURL != "" {
			keySources++
		}
		if repo.KeyFile != "" {
			keySources++
			if strings.HasPrefix(repo.KeyFile, "/") || strings.Contains(repo.KeyFile, "..") || path.Clean(repo.KeyFile) != repo.KeyFile {
				errs.Add(RootFile, "%s: key_file must be a clean path relative to system/", where)
			} else if _, ok := c.Entry("system/" + repo.KeyFile); !ok {
				errs.Add(RootFile, "%s: key_file system/%s does not exist", where, repo.KeyFile)
			}
		}
		if keySources > 1 {
			errs.Add(RootFile, "%s: use key_url or key_file, not both", where)
		}
		switch repo.Kind {
		case "dnf":
			if (repo.BaseURL == "") == (repo.ReleasePackage == "") {
				errs.Add(RootFile, "%s: a dnf repository needs exactly one of baseurl or release_package", where)
			}
			if repo.ReleasePackage != "" && !sha256Re.MatchString(repo.SHA256) {
				errs.Add(RootFile, "%s: release_package needs a lowercase hex sha256", where)
			}
			if repo.ReleasePackage == "" && repo.SHA256 != "" {
				errs.Add(RootFile, "%s: sha256 belongs only to release_package", where)
			}
			if repo.BaseURL != "" && keySources == 0 {
				errs.Add(RootFile, "%s: a baseurl repository needs key_url or key_file", where)
			}
			if repo.Project != "" || repo.URL != "" {
				errs.Add(RootFile, "%s: project and url belong to other repository kinds", where)
			}
		case "copr":
			if !coprRe.MatchString(repo.Project) {
				errs.Add(RootFile, "%s: a copr repository needs project = \"owner/project\"", where)
			}
			if repo.BaseURL != "" || repo.ReleasePackage != "" || repo.SHA256 != "" || repo.URL != "" || repo.KeyFile != "" {
				errs.Add(RootFile, "%s: a copr repository takes only project, key, key_url, and priority", where)
			}
		case "flatpak":
			flatpaks++
			if repo.URL == "" {
				errs.Add(RootFile, "%s: a flatpak repository needs url", where)
			}
			if repo.BaseURL != "" || repo.ReleasePackage != "" || repo.SHA256 != "" || repo.Project != "" || keySources != 0 || repo.Priority != 0 {
				errs.Add(RootFile, "%s: a flatpak repository takes only url and key", where)
			}
		default:
			errs.Add(RootFile, "%s: kind must be dnf, copr, or flatpak", where)
		}
	}
	if flatpaks > 1 {
		errs.Add(RootFile, "only one flatpak repository may be declared")
	}
}

// FlatpakRepository returns the ID of the single flatpak repository, or "".
func (c *Checkout) FlatpakRepository() string {
	for id, repo := range c.Root_.Repositories {
		if repo.Kind == "flatpak" {
			return id
		}
	}
	return ""
}

func (c *Checkout) validateRefs(where string, raws []string, errs *ErrorList) []Ref {
	refs := make([]Ref, 0, len(raws))
	seen := map[string]bool{}
	for _, raw := range raws {
		ref, err := ParseRef(raw)
		if err != nil {
			errs.Add(where, "%v", err)
			continue
		}
		switch ref.Prefix {
		case PrefixDNF:
		case PrefixFlatpak:
			if c.FlatpakRepository() == "" {
				errs.Add(where, "%q needs a flatpak repository in %s", raw, RootFile)
			}
		default:
			repo, ok := c.Root_.Repositories[ref.Prefix]
			if !ok {
				errs.Add(where, "%q names repository %q, which %s does not declare", raw, ref.Prefix, RootFile)
			} else if repo.Kind == "flatpak" {
				errs.Add(where, "%q must use the flatpak: prefix", raw)
			}
		}
		if seen[ref.Canonical()] {
			errs.Add(where, "duplicate package reference %q", raw)
		}
		seen[ref.Canonical()] = true
		refs = append(refs, ref)
	}
	return refs
}

func validateIDList(where, kind string, ids []string, exists func(string) bool, self string, errs *ErrorList) {
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			errs.Add(where, "duplicate %s %q", kind, id)
		}
		seen[id] = true
		if id == self {
			errs.Add(where, "%s %q refers to itself", kind, id)
		}
		if !exists(id) {
			errs.Add(where, "unknown %s %q", kind, id)
		}
	}
}

func validateProfile(c *Checkout, p *Profile, errs *ErrorList) {
	where := "profiles/" + p.ID + ".toml"
	if p.Schema != CurrentSchema {
		errs.Add(where, "schema %d is not supported", p.Schema)
	}
	c.validateRefs(where, p.Packages, errs)
	validateIDList(where, "component", p.Components, c.hasComponent, "", errs)
}

func validateComponent(c *Checkout, comp *Component, errs *ErrorList) {
	where := "components/" + comp.ID + ".toml"
	if comp.Schema != CurrentSchema {
		errs.Add(where, "schema %d is not supported", comp.Schema)
	}
	validateIDList(where, "required component", comp.Requires, c.hasComponent, comp.ID, errs)
	validateIDList(where, "conflicting component", comp.Conflicts, c.hasComponent, comp.ID, errs)
	c.validateRefs(where, comp.Packages, errs)
	seen := map[string]bool{}
	for _, name := range comp.Removes {
		if err := ParseRPMName(name); err != nil {
			errs.Add(where, "removes: %v", err)
		}
		if seen[name] {
			errs.Add(where, "removes: duplicate %q", name)
		}
		seen[name] = true
	}
	sources := map[string]bool{}
	for i, f := range comp.Files {
		fw := where + " files[" + itoa(i) + "]"
		switch {
		case f.Source == "":
			errs.Add(fw, "source is required")
		case !strings.HasPrefix(f.Source, "etc/"):
			errs.Add(fw, "source %q must start with etc/", f.Source)
		case path.Clean(f.Source) != f.Source || strings.Contains(f.Source, ".."):
			errs.Add(fw, "source %q must be a clean relative path", f.Source)
		default:
			if _, ok := c.Entry("system/root/" + f.Source); !ok {
				errs.Add(fw, "source system/root/%s does not exist", f.Source)
			}
		}
		if sources[f.Source] {
			errs.Add(fw, "duplicate source %q", f.Source)
		}
		sources[f.Source] = true
		if !accountRe.MatchString(f.Owner) {
			errs.Add(fw, "owner %q is not a valid account name", f.Owner)
		}
		if !accountRe.MatchString(f.Group) {
			errs.Add(fw, "group %q is not a valid group name", f.Group)
		}
		if !modeRe.MatchString(f.Mode) {
			errs.Add(fw, "mode %q must be four octal digits such as 0644", f.Mode)
		}
	}
}

func validateMachine(c *Checkout, m *Machine, errs *ErrorList) {
	where := "machines/" + m.ID + ".toml"
	if m.Schema != CurrentSchema {
		errs.Add(where, "schema %d is not supported", m.Schema)
	}
	if len(m.Profiles) == 0 {
		errs.Add(where, "profiles must not be empty")
	}
	validateIDList(where, "profile", m.Profiles, c.hasProfile, "", errs)
	hasCommon := false
	for _, id := range m.Profiles {
		if id == "common" {
			hasCommon = true
		}
	}
	if !hasCommon {
		errs.Add(where, "profiles must include \"common\"; the resolver never adds it")
	}
	validateIDList(where, "component", m.Components, c.hasComponent, "", errs)
	c.validateRefs(where, m.Packages, errs)
	c.validateRefs(where+" package_exclusions", m.PackageExclusions, errs)
	if m.Dotfiles != nil && m.Dotfiles.Repo == "" {
		errs.Add(where, "dotfiles.repo must not be empty when the table is present")
	}
}

func validateRequireCycles(c *Checkout, errs *ErrorList) {
	const (
		unvisited = iota
		visiting
		done
	)
	state := map[string]int{}
	var visit func(id string, stack []string)
	visit = func(id string, stack []string) {
		switch state[id] {
		case visiting:
			errs.Add("components/"+id+".toml", "requires cycle: %s -> %s", strings.Join(stack, " -> "), id)
			return
		case done:
			return
		}
		state[id] = visiting
		comp := c.Components[id]
		if comp != nil {
			for _, req := range comp.Requires {
				if _, ok := c.Components[req]; ok {
					visit(req, append(stack, id))
				}
			}
		}
		state[id] = done
	}
	for _, id := range sortedKeys(c.Components) {
		visit(id, nil)
	}
}

func (c *Checkout) hasComponent(id string) bool { _, ok := c.Components[id]; return ok }
func (c *Checkout) hasProfile(id string) bool   { _, ok := c.Profiles[id]; return ok }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
