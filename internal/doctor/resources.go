package doctor

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	defs "github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/state"
)

// SystemResources checks effective native state without a plan, package
// preview, network access, privilege escalation, or repair.
func SystemResources(src native.Source, r *defs.Resolved, applied *state.Applied, username string) []Check {
	var checks []Check
	add := func(id, provider, observation string, matches bool, err error) {
		c := Check{ID: id, Status: Pass, Observation: observation}
		switch {
		case err != nil:
			c.Status = Unknown
			c.Observation = err.Error()
			c.Remediation = "make the native state readable, then run doctor again"
		case !matches:
			c.Status = Fail
			c.Impact = "selected system integration is not ready"
			c.Remediation = "inspect nimbus sync --plan and review the proposed repair"
		default:
			receipt, ok := applied.Receipts[id]
			if !ok || !receipt.Verified || receipt.Resource != id || receipt.Provider != provider || receipt.Machine != r.Machine {
				c.Status = Fail
				c.Observation += "; Nimbus ownership is not verified"
				c.Remediation = "review nimbus sync --plan before adopting or configuring this resource"
			}
		}
		checks = append(checks, c)
	}
	for _, file := range r.Files {
		have, err := inspect.ObserveFile(src, file.Target)
		matches := have.Exists && bytes.Equal(have.Content, file.Content) && have.Owner == file.Owner && have.Group == file.Group && have.Mode == file.Mode
		add("file:"+file.Target, "system-file", fmt.Sprintf("%s: present %t, owner %s:%s, mode %s, content matches %t", file.Target, have.Exists, have.Owner, have.Group, have.Mode, bytes.Equal(have.Content, file.Content)), matches, err)
		if file.Target == "/etc/docker/daemon.json" {
			unit, unitErr := inspect.ObserveService(src, "docker.service")
			c := Check{ID: "docker-logging", Status: Unknown, Observation: "Docker daemon is not active; effective logging driver cannot be read", Remediation: "after the planned service change and any required logout, run doctor again"}
			if unitErr == nil && unit.Active == "active" {
				out, err := src.Run("docker", "info", "--format", "{{.LoggingDriver}}")
				if err != nil {
					c.Observation = "cannot read the running Docker daemon: " + err.Error()
				} else {
					c.Observation = "running Docker default logging driver: " + strings.TrimSpace(string(out)) + "; existing containers retain their own logging settings"
					c.Status = Pass
					if strings.TrimSpace(string(out)) != "local" {
						c.Status = Fail
						c.Remediation = "review the configured logging driver and planned Docker restart"
					} else {
						c.Remediation = ""
					}
				}
			}
			checks = append(checks, c)
		}
	}
	for _, service := range r.Services {
		have, err := inspect.ObserveService(src, service.Unit)
		matches := have.Load == "loaded" && have.Enabled != "masked" && have.Enabled != "masked-runtime"
		if service.Enabled != nil {
			want := "disabled"
			if *service.Enabled {
				want = "enabled"
			}
			matches = matches && have.Enabled == want
		}
		if service.Running != nil {
			matches = matches && (have.Active == "active") == *service.Running
		}
		add("service:"+service.Unit, "service", fmt.Sprintf("%s: load %s, enablement %s, activity %s", service.Unit, have.Load, have.Enabled, have.Active), matches, err)
		if service.Unit == "greetd.service" && err == nil && have.Active != "active" {
			checks = append(checks, Check{ID: "greeter-login", Status: Fail, Observation: "greetd is not active; package installation and enablement do not prove graphical login", Impact: "the greeter may not appear", Remediation: "after reviewing the plan, reboot and inspect journalctl -b -u greetd; keep console recovery available"})
		}
	}
	for _, group := range r.Groups {
		user := group.User
		if user == "<user>" {
			user = username
		}
		var err error
		present := false
		if user == "" {
			err = fmt.Errorf("invoking username is unknown")
		} else {
			present, err = inspect.ObserveMembership(src, user, group.Name)
		}
		add("group:"+group.Name+":"+user, "group", fmt.Sprintf("%s membership in %s: %t (existing sessions may need logout)", user, group.Name, present), present, err)
	}
	if r.DefaultTarget != "" {
		out, err := src.Run("systemctl", "get-default")
		have := strings.TrimSpace(string(out))
		add("default-target", "default-target", "default boot target: "+have, have == r.DefaultTarget, err)
	}
	if slices.Contains(r.Profiles, "common") {
		checks = append(checks, workstationDefaults(src)...)
	}
	return checks
}

// Report effective native defaults without inventing tuning targets. These
// observations are not claims about workload suitability or hardware support.
func workstationDefaults(src native.Source) []Check {
	var checks []Check
	for _, probe := range []struct {
		id   string
		argv []string
	}{
		{"memory-and-inotify-limits", []string{"sysctl", "vm.max_map_count", "fs.inotify.max_user_watches", "fs.inotify.max_user_instances", "fs.file-max"}},
		{"zram", []string{"zramctl", "--noheadings", "--output", "NAME,DISKSIZE,ALGORITHM"}},
		{"journal-storage", []string{"journalctl", "--disk-usage"}},
		{"oom-policy", []string{"systemctl", "show", "--property=LoadState,ActiveState", "--", "systemd-oomd.service"}},
		{"ssd-trim-schedule", []string{"systemctl", "show", "--property=LoadState,UnitFileState,ActiveState", "--", "fstrim.timer"}},
	} {
		out, err := src.Run(probe.argv[0], probe.argv[1:]...)
		c := Check{ID: probe.id, Status: Pass, Observation: strings.TrimSpace(string(out))}
		if err != nil {
			c.Status = Unknown
			c.Observation = err.Error()
		} else if c.Observation == "" {
			c.Status = Unknown
			c.Observation = "no effective state reported"
		}
		checks = append(checks, c)
	}
	return checks
}
