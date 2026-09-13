package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/pelletier/go-toml/v2"
)

// inspectFinal only reads native observations. It never authenticates to an app,
// repairs preferences, or converts an inspection failure into completion.
func inspectFinal(src native.Source, s *selected, result *syncResult, notes bool, allNotes bool) {
	snapshot, err := inspectPostinstall(src, machineFlags{checkout: s.Root, machine: s.Resolved.Machine})
	if err != nil {
		result.Notices = append(result.Notices, "Setup status unavailable: "+err.Error())
	} else {
		for _, t := range snapshot.view.Tasks {
			if t.Status == postinstall.Complete || t.Status == postinstall.NotApplicable {
				continue
			}
			if t.ID == "reboot" || t.ID == "logout" {
				result.Notices = append(result.Notices, t.ID+": "+t.Detail)
				continue
			}
			result.Tasks = append(result.Tasks, t)
		}
	}
	if slices.Contains(s.Resolved.Profiles, "hyprland-noctalia") {
		home, _ := os.UserHomeDir()
		data, err := src.ReadFile(filepath.Join(home, ".config", "noctalia", "settings.toml"))
		if err == nil {
			var settings struct {
				LockscreenWidgets struct {
					Enabled *bool `toml:"enabled"`
				} `toml:"lockscreen_widgets"`
			}
			if err := toml.Unmarshal(data, &settings); err != nil {
				result.Notices = append(result.Notices, "Noctalia GUI overrides could not be parsed; effective lockscreen appearance is unverified.")
			} else if settings.LockscreenWidgets.Enabled != nil && !*settings.LockscreenWidgets.Enabled {
				result.Notices = append(result.Notices, "Noctalia GUI overrides disable managed lockscreen widgets. Reset only that override in the lockscreen editor; other preferences are preserved.")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			result.Notices = append(result.Notices, "Noctalia GUI overrides are unreadable; effective appearance is unverified.")
		}
	}
	if notes {
		result.Notes, err = pendingNotes(src, s, allNotes)
		if err != nil {
			result.Notices = append(result.Notices, "Setup guidance unavailable: "+err.Error())
		}
	}
}
func renderFinalDetails(out io.Writer, result *syncResult) error {
	if result.Reboot {
		if _, err := fmt.Fprintln(out, "Reboot required to activate changed boot or greeter configuration."); err != nil {
			return err
		}
	}
	if result.Logout {
		if _, err := fmt.Fprintln(out, "Log out and back in to activate changed session settings or group membership."); err != nil {
			return err
		}
	}
	if len(result.Tasks) > 0 {
		if _, err := fmt.Fprintln(out, "\nRemaining setup:"); err != nil {
			return err
		}
		for _, t := range result.Tasks {
			if _, err := fmt.Fprintf(out, "  %s: %s\n    nimbus postinstall %s\n", t.ID, t.Detail, t.ID); err != nil {
				return err
			}
		}
	}
	for _, notice := range result.Notices {
		if _, err := fmt.Fprintln(out, "Notice: "+notice); err != nil {
			return err
		}
	}
	return displayNotes(out, result.Notes)
}
func skippedMaintenance(result *syncResult, phase string, upgrade bool) {
	phases := []string{"repository preflight", "repository update", "system sync", "Chezmoi apply", "software updates", "final inspection"}
	start := slices.Index(phases, phase)
	for _, name := range phases[max(0, start+1):] {
		if name == "software updates" && !upgrade {
			continue
		}
		if !slices.ContainsFunc(result.Steps, func(s runStep) bool { return strings.EqualFold(s.Name, name) }) {
			result.Steps = append(result.Steps, runStep{Name: name, Status: "skipped", Detail: "an earlier phase did not complete"})
		}
	}
}
