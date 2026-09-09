package definitions

import (
	"cmp"
	"maps"
	"regexp"
	"slices"
)

type ResolvedService struct {
	ServiceDecl
	Component string `json:"component"`
}
type ResolvedGroup struct {
	GroupDecl
	Component string `json:"component"`
}

var unitNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]*\.(service|timer|socket|path)$`)

// TriggerArgs is a closed set of reviewed commands; definitions cannot supply argv.
func TriggerArgs(id string) []string {
	switch id {
	case "systemd-daemon-reload":
		return []string{"systemctl", "daemon-reload"}
	case "noctalia-state-directory":
		return []string{"systemd-tmpfiles", "--create", "/etc/tmpfiles.d/nimbus-noctalia-greeter.conf"}
	case "udev-reload":
		return []string{"udevadm", "control", "--reload"}
	case "sysctl-reload":
		return []string{"sysctl", "--system"}
	case "docker-restart":
		return []string{"systemctl", "try-restart", "docker.service"}
	}
	return nil
}

func validateResources(comp *Component, where string, errs *ErrorList) {
	seen := map[string]bool{}
	for _, service := range comp.Services {
		if !unitNameRE.MatchString(service.Unit) {
			errs.Add(where, "invalid service unit %q", service.Unit)
		}
		if service.Enabled == nil && service.Running == nil {
			errs.Add(where, "service %q needs enabled or running", service.Unit)
		}
		if seen[service.Unit] {
			errs.Add(where, "duplicate service %q", service.Unit)
		}
		seen[service.Unit] = true
	}
	clear(seen)
	for _, group := range comp.Groups {
		if !accountRe.MatchString(group.Name) || (group.User != "<user>" && !accountRe.MatchString(group.User)) {
			errs.Add(where, "invalid group membership %q/%q", group.Name, group.User)
		}
		key := group.Name + ":" + group.User
		if seen[key] {
			errs.Add(where, "duplicate group membership %s", key)
		}
		seen[key] = true
	}
	if comp.DefaultTarget != "" && comp.DefaultTarget != "graphical.target" && comp.DefaultTarget != "multi-user.target" {
		errs.Add(where, "default_target must be graphical.target or multi-user.target")
	}
	for _, file := range comp.Files {
		for _, id := range file.Triggers {
			if TriggerArgs(id) == nil {
				errs.Add(where, "unknown file trigger %q", id)
			}
		}
	}
}

func resolveResources(c *Checkout, r *Resolved, errs *ErrorList) {
	units, groups := map[string]string{}, map[string]string{}
	for _, rc := range r.Components {
		comp := c.Components[rc.ID]
		if comp.Recovery != nil && comp.Recovery.Enabled {
			for _, source := range slices.Sorted(maps.Keys(RecoveryFiles)) {
				entry, ok := c.Entry(source)
				if !ok {
					errs.Add("components/"+rc.ID+".toml", "recovery source %s is missing", source)
					continue
				}
				target := RecoveryFiles[source]
				for _, f := range r.Files {
					if f.Target == target {
						errs.Add("components/"+rc.ID+".toml", "duplicate recovery target %s", target)
					}
				}
				r.Files = append(r.Files, ResolvedFile{Target: target, Source: source, Owner: "root", Group: "root", Mode: "0644", Component: rc.ID, Content: entry.Content, Recovery: true})
			}
		}
		for _, s := range comp.Services {
			if prev, ok := units[s.Unit]; ok {
				errs.Add("components/"+rc.ID+".toml", "service %s also belongs to %s", s.Unit, prev)
				continue
			}
			units[s.Unit] = rc.ID
			r.Services = append(r.Services, ResolvedService{ServiceDecl: s, Component: rc.ID})
		}
		for _, g := range comp.Groups {
			key := g.Name + ":" + g.User
			if prev, ok := groups[key]; ok {
				errs.Add("components/"+rc.ID+".toml", "membership %s also belongs to %s", key, prev)
				continue
			}
			groups[key] = rc.ID
			r.Groups = append(r.Groups, ResolvedGroup{GroupDecl: g, Component: rc.ID})
		}
		if comp.DefaultTarget != "" {
			if r.DefaultTarget != "" {
				errs.Add("components/"+rc.ID+".toml", "default_target has more than one owner")
			}
			r.DefaultTarget = comp.DefaultTarget
		}
	}
	slices.SortFunc(r.Services, func(a, b ResolvedService) int { return cmp.Compare(a.Unit, b.Unit) })
	slices.SortFunc(r.Groups, func(a, b ResolvedGroup) int {
		return cmp.Compare(a.Name+":"+a.User, b.Name+":"+b.User)
	})
}

// RecoveryFiles fixes both ends of the separately typed /usr integration.
var RecoveryFiles = map[string]string{
	"system/recovery/hyprland.lua":            "/usr/local/lib/nimbus/recovery/hyprland.lua",
	"system/recovery/nimbus-recovery.desktop": "/usr/share/wayland-sessions/nimbus-recovery.desktop",
}

func RecoveryTarget(target string) bool {
	for t := range maps.Values(RecoveryFiles) {
		if target == t {
			return true
		}
	}
	return false
}
