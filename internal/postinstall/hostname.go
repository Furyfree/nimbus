package postinstall

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
)

var hostnameName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// HostnameForMachine is the naming policy: the selected machine ID with the
// nimbus- prefix, for example nimbus-laptop. The IDs are already lowercase,
// dash-separated and short; HostnameAction validates the derived name again.
func HostnameForMachine(machine string) string {
	return "nimbus-" + machine
}

// HostnameAction constructs the single native change. The intended name is
// derived from validated machine definitions, never from a shell expansion.
func HostnameAction(hostname string) *Action {
	if !hostnameName.MatchString(hostname) || hostname == "localhost" {
		return nil
	}
	return &Action{Kind: SetHostname, Hostname: hostname,
		Argv: []string{"sudo", "--", "/usr/bin/hostnamectl", "set-hostname", hostname}}
}

func hostnameTask(src native.Source, in Inputs) Task {
	intended := HostnameForMachine(in.Resolved.Machine)
	t := Task{
		ID: "hostname", Owner: "machine:" + in.Resolved.Machine, Title: "Set the persistent hostname", Status: Unknown,
		Prerequisites: []string{"The machine ID derives a valid static hostname."},
		Instructions: []string{
			"Close browsers first. Chromium compares the hostname recorded in a profile lock against the current one and can refuse to open a profile when they differ.",
			"NetworkManager keeps a valid static hostname instead of updating it from DHCP, so the name stays stable across Wi-Fi changes and reboots.",
		},
		Verification: "The task re-reads hostnamectl after the change. A matching static hostname is required; the transient name is not.",
		Recovery:     "Set another name with hostnamectl set-hostname, or restore the previous one. Nimbus removes no browser data and deletes no profile locks.",
	}
	action := HostnameAction(intended)
	if action == nil {
		t.Status, t.Detail = Blocked, "The machine ID "+in.Resolved.Machine+" does not derive a valid hostname ("+intended+")."
		return t
	}
	if _, err := src.LookPath("hostnamectl"); err != nil {
		t.Status, t.Detail = Blocked, "hostnamectl is unavailable; repair the systemd package state before changing the hostname."
		return t
	}
	out, err := src.Run("/usr/bin/hostnamectl", "--static")
	if err != nil {
		t.Detail = "The static hostname could not be read; check systemd-hostnamed, then retry"
		return t
	}
	current := strings.TrimSpace(string(out))
	if current == intended {
		t.Status = Complete
		t.Detail = "The static hostname is already " + intended + "."
		return t
	}
	t.Status = Pending
	if current == "" {
		t.Detail = "No static hostname is set; the machine " + in.Resolved.Machine + " targets " + intended + "."
	} else {
		t.Detail = "Static hostname is " + current + "; the machine " + in.Resolved.Machine + " targets " + intended + "."
	}
	t.Action = action
	return t
}

// HostnameCommands validates the forwarded native change for the CLI runner.
func HostnameCommands(task Task) ([][]string, error) {
	if task.ID != "hostname" || task.Status != Pending || task.Action == nil || task.Action.Kind != SetHostname ||
		task.Action.Hostname == "" || task.Action.Hostname != strings.TrimSpace(task.Action.Hostname) {
		return nil, errors.New("task has no supported hostname action")
	}
	expected := HostnameAction(task.Action.Hostname)
	if expected == nil || !slices.Equal(task.Action.Argv, expected.Argv) {
		return nil, errors.New("unsupported hostname command")
	}
	return [][]string{task.Action.Argv}, nil
}
