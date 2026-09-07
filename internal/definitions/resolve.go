package definitions

import (
	"sort"
	"strings"
)

// Resolved is the deterministic desired graph of one machine.
type Resolved struct {
	Machine      string              `json:"machine"`
	Profiles     []string            `json:"profiles"`
	Components   []ResolvedComponent `json:"components"`
	Packages     []ResolvedPackage   `json:"packages"`
	Removes      []string            `json:"removes"`
	Files        []ResolvedFile      `json:"files"`
	Repositories []string            `json:"repositories"`
	// Installers are the user-scope tools of the selected components.
	Installers []ResolvedInstaller `json:"installers"`
}

// ResolvedInstaller is one component's installer with its selection paths.
type ResolvedInstaller struct {
	Component string    `json:"component"`
	Installer Installer `json:"installer"`
	Paths     []string  `json:"paths"`
}

// ResolvedComponent is a selected component with every path that selected it.
type ResolvedComponent struct {
	ID    string   `json:"id"`
	Paths []string `json:"paths"`
}

// ResolvedPackage is one canonical package with its selection provenance.
type ResolvedPackage struct {
	Canonical string   `json:"canonical"`
	Prefix    string   `json:"prefix"`
	Name      string   `json:"name"`
	Paths     []string `json:"paths"`
}

// ResolvedFile is one generic system file with its derived target.
type ResolvedFile struct {
	Target    string `json:"target"`
	Source    string `json:"source"`
	Owner     string `json:"owner"`
	Group     string `json:"group"`
	Mode      string `json:"mode"`
	Component string `json:"component"`
}

// Resolve computes the desired graph of one machine from configuration only.
// The checkout must already have passed Validate's structural checks.
func Resolve(c *Checkout, machineID string) (*Resolved, ErrorList) {
	var errs ErrorList
	m, ok := c.Machines[machineID]
	if !ok {
		errs.Add("machines/"+machineID+".toml", "unknown machine")
		return nil, errs
	}
	where := "machines/" + m.ID + ".toml"

	// Components: profile selections, explicit selections, then requires.
	compPaths := map[string][]string{}
	add := func(id, path string) {
		compPaths[id] = append(compPaths[id], path)
	}
	for _, pid := range m.Profiles {
		p := c.Profiles[pid]
		if p == nil {
			continue
		}
		for _, cid := range p.Components {
			add(cid, "profile:"+pid)
		}
	}
	for _, cid := range m.Components {
		add(cid, "machine")
	}
	for changed := true; changed; {
		changed = false
		for _, cid := range sortedKeys(compPaths) {
			comp := c.Components[cid]
			if comp == nil {
				continue
			}
			for _, req := range comp.Requires {
				path := "component:" + cid
				if !contains(compPaths[req], path) {
					add(req, path)
					changed = true
				}
			}
		}
	}
	for _, cid := range sortedKeys(compPaths) {
		comp := c.Components[cid]
		if comp == nil {
			continue
		}
		for _, other := range comp.Conflicts {
			if _, selected := compPaths[other]; selected {
				errs.Add(where, "component %q conflicts with selected component %q", cid, other)
			}
		}
	}

	// Packages with provenance.
	pkgs := map[string]*ResolvedPackage{}
	byName := map[string]map[string]bool{}
	addPkg := func(raw, path string) {
		ref, err := ParseRef(raw)
		if err != nil {
			return
		}
		key := ref.Canonical()
		rp := pkgs[key]
		if rp == nil {
			rp = &ResolvedPackage{Canonical: key, Prefix: ref.Prefix, Name: ref.Name}
			pkgs[key] = rp
		}
		if !contains(rp.Paths, path) {
			rp.Paths = append(rp.Paths, path)
		}
		if byName[ref.Name] == nil {
			byName[ref.Name] = map[string]bool{}
		}
		byName[ref.Name][ref.Prefix] = true
	}
	for _, pid := range m.Profiles {
		if p := c.Profiles[pid]; p != nil {
			for _, raw := range p.Packages {
				addPkg(raw, "profile:"+pid)
			}
		}
	}
	for _, cid := range sortedKeys(compPaths) {
		if comp := c.Components[cid]; comp != nil {
			for _, raw := range comp.Packages {
				addPkg(raw, "component:"+cid)
			}
		}
	}
	for _, raw := range m.Packages {
		addPkg(raw, "machine")
	}
	for _, name := range sortedKeys(byName) {
		if len(byName[name]) > 1 {
			errs.Add(where, "package %q is selected from more than one repository: %s", name, strings.Join(sortedKeys(byName[name]), ", "))
		}
	}

	// Exclusions: only a package a selected profile or component installs,
	// never a package of a component that another selected component
	// requires, whatever other paths also select it.
	required := map[string]bool{}
	for cid := range compPaths {
		if comp := c.Components[cid]; comp != nil {
			for _, req := range comp.Requires {
				required[req] = true
			}
		}
	}
	for _, raw := range m.PackageExclusions {
		ref, err := ParseRef(raw)
		if err != nil {
			continue
		}
		rp := pkgs[ref.Canonical()]
		if rp == nil {
			errs.Add(where, "package_exclusions: %q matches no selected package", raw)
			continue
		}
		blocked := ""
		for _, p := range rp.Paths {
			if p == "machine" {
				blocked = "is listed in this manifest's packages; remove it there instead"
			} else if cid, ok := strings.CutPrefix(p, "component:"); ok && required[cid] {
				blocked = "belongs to component " + cid + ", which another selected component requires"
			}
		}
		if blocked != "" {
			errs.Add(where, "package_exclusions: %q %s", raw, blocked)
			continue
		}
		delete(pkgs, ref.Canonical())
	}

	// Removes and files. A removal names a native package, so it conflicts
	// with the same name selected from any RPM repository.
	selectedRPM := map[string]string{}
	for _, rp := range pkgs {
		if rp.Prefix != PrefixFlatpak && rp.Prefix != PrefixCargo {
			selectedRPM[rp.Name] = rp.Canonical
		}
	}
	removes := map[string]bool{}
	files := map[string]ResolvedFile{}
	for _, cid := range sortedKeys(compPaths) {
		comp := c.Components[cid]
		if comp == nil {
			continue
		}
		for _, name := range comp.Removes {
			if canonical, selected := selectedRPM[name]; selected {
				errs.Add(where, "component %q removes %q, which is also selected as %s", cid, name, canonical)
			}
			removes[name] = true
		}
		for _, f := range comp.Files {
			target := "/" + f.Source
			if prev, dup := files[target]; dup {
				errs.Add(where, "components %q and %q both declare %s", prev.Component, cid, target)
				continue
			}
			files[target] = ResolvedFile{Target: target, Source: "system/root/" + f.Source, Owner: f.Owner, Group: f.Group, Mode: f.Mode, Component: cid}
		}
	}

	r := &Resolved{Machine: m.ID, Profiles: append([]string(nil), m.Profiles...)}
	for _, cid := range sortedKeys(compPaths) {
		paths := append([]string(nil), compPaths[cid]...)
		sort.Strings(paths)
		r.Components = append(r.Components, ResolvedComponent{ID: cid, Paths: paths})
	}
	repos := map[string]bool{}
	for _, key := range sortedKeys(pkgs) {
		rp := pkgs[key]
		sort.Strings(rp.Paths)
		r.Packages = append(r.Packages, *rp)
		switch rp.Prefix {
		case PrefixDNF, PrefixCargo:
		case PrefixFlatpak:
			if id := c.FlatpakRepository(); id != "" {
				repos[id] = true
			}
		default:
			repos[rp.Prefix] = true
		}
	}
	r.Removes = sortedKeys(removes)
	for _, target := range sortedKeys(files) {
		r.Files = append(r.Files, files[target])
	}
	r.Repositories = sortedKeys(repos)
	for _, rc := range r.Components {
		if comp := c.Components[rc.ID]; comp != nil && comp.Installer != nil {
			r.Installers = append(r.Installers, ResolvedInstaller{Component: rc.ID, Installer: *comp.Installer, Paths: rc.Paths})
		}
	}
	if r.Installers == nil {
		r.Installers = []ResolvedInstaller{}
	}
	if r.Components == nil {
		r.Components = []ResolvedComponent{}
	}
	if r.Packages == nil {
		r.Packages = []ResolvedPackage{}
	}
	if r.Removes == nil {
		r.Removes = []string{}
	}
	if r.Files == nil {
		r.Files = []ResolvedFile{}
	}
	if r.Repositories == nil {
		r.Repositories = []string{}
	}
	return r, errs
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
