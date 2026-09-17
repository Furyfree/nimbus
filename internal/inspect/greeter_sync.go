package inspect

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
)

const GreeterBinary = "/usr/bin/noctalia-greeter"
const GreeterHelper = "/usr/bin/noctalia-greeter-apply-appearance"
const GreeterPolicy = "/usr/share/polkit-1/actions/org.noctalia.greeter.apply-appearance.policy"

// ErrGreeterAdminStatus prevents a failed approved query being treated as an
// ordinary unprivileged permission limitation.
var ErrGreeterAdminStatus = errors.New("administrator greeter status check failed")

// GreeterSync describes only the native helper's managed authorization rule.
// Known=false means inspection needs privilege, never that access is disabled.
type GreeterSync struct {
	UID     uint64 `json:"uid"`
	Known   bool   `json:"known"`
	Enabled bool   `json:"enabled"`
}

// CheckGreeterSync rejects legacy helpers and policy before authorizing sync.
// The native enable command additionally validates root ownership and rule integrity.
func CheckGreeterSync(src native.Source) error {
	version, err := src.Run("rpm", "-q", "--queryformat", "%{VERSION}", "noctalia")
	parts := strings.Split(strings.TrimSpace(string(version)), ".")
	if err != nil || len(parts) != 3 {
		return fmt.Errorf("cannot establish compatible Noctalia version (requires 5.1.0 or newer)")
	}
	numbers := [3]uint64{}
	for i, part := range parts {
		numbers[i], err = strconv.ParseUint(part, 10, 32)
		if err != nil {
			return fmt.Errorf("unsupported Noctalia version; requires stable 5.1.0 or newer")
		}
	}
	if numbers[0] < 5 || (numbers[0] == 5 && numbers[1] < 1) {
		return fmt.Errorf("Noctalia 5.1.0 or newer is required for constrained greeter sync; upgrade the Fedora package first")
	}
	out, err := src.Run(GreeterHelper, "--supports", "secure-sync-v1")
	if err != nil || strings.TrimSpace(string(out)) != "secure-sync-v1" {
		return fmt.Errorf("greeter requires the secure-sync-v1 helper; install compatible Noctalia and greeter packages")
	}
	data, err := src.ReadFile(GreeterPolicy)
	if err != nil {
		return fmt.Errorf("read constrained greeter policy: %w", err)
	}
	var policy struct {
		XMLName xml.Name `xml:"policyconfig"`
		Actions []struct {
			ID          string `xml:"id,attr"`
			Annotations []struct {
				Key   string `xml:"key,attr"`
				Value string `xml:",chardata"`
			} `xml:"annotate"`
		} `xml:"action"`
	}
	if len(data) > 128*1024 || xml.Unmarshal(data, &policy) != nil {
		return fmt.Errorf("invalid constrained greeter policy")
	}
	matches := 0
	for _, action := range policy.Actions {
		if action.ID != "org.noctalia.greeter.sync-appearance" {
			continue
		}
		matches++
		values := map[string]string{}
		for _, a := range action.Annotations {
			if _, exists := values[a.Key]; exists {
				return fmt.Errorf("duplicate constrained greeter policy annotation")
			}
			values[a.Key] = strings.TrimSpace(a.Value)
		}
		if values["org.freedesktop.policykit.exec.path"] != GreeterHelper || values["org.freedesktop.policykit.exec.argv1"] != "--sync" {
			return fmt.Errorf("greeter policy does not constrain authorization to secure appearance sync")
		}
	}
	if matches != 1 {
		return fmt.Errorf("greeter policy needs exactly one constrained sync action")
	}
	return nil
}

// ObserveGreeterSync never requests authentication unless the approved executor
// explicitly selects privileged inspection. Preview callers must pass false.
func ObserveGreeterSync(src native.Source, user string, privileged bool) (GreeterSync, error) {
	account, err := ObserveLoginShell(src, user)
	if err != nil {
		return GreeterSync{}, err
	}
	result := GreeterSync{UID: account.UID}
	name, args := GreeterBinary, []string{"passwordless-sync", "status", user}
	if privileged {
		name, args = "sudo", append([]string{"--", GreeterBinary}, args...)
	}
	out, err := src.Run(name, args...)
	if err != nil {
		if !privileged && !errors.Is(err, ErrGreeterAdminStatus) && strings.Contains(err.Error(), "failed to open the Polkit rules directory: Permission denied") {
			return result, nil
		}
		return result, fmt.Errorf("inspect greeter authorization: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 || lines[1] != "Other administrator-authored Polkit rules are not included in this status." {
		return result, fmt.Errorf("unrecognized greeter authorization status")
	}
	switch lines[0] {
	case user + ": enabled by the noctalia-greeter managed rule":
		result.Enabled = true
	case user + ": not enabled by the noctalia-greeter managed rule":
	default:
		return result, fmt.Errorf("unrecognized greeter authorization status for %s", user)
	}
	result.Known = true
	return result, nil
}
