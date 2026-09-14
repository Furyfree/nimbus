package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/postinstall"
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
			if t.PreviouslyVerified && t.Status == postinstall.Unknown && t.VerificationNeedsRoot {
				result.Historical = append(result.Historical, t.ID+": Previously verified. "+t.Detail)
				continue
			}
			if t.ID == "reboot" || t.ID == "logout" {
				result.Notices = append(result.Notices, t.ID+": "+t.Detail)
				continue
			}
			result.Tasks = append(result.Tasks, t)
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
	for _, group := range []struct {
		title   string
		problem bool
	}{{"Remaining setup", false}, {"Verification problems", true}} {
		shown := false
		for _, task := range result.Tasks {
			problem := task.Status == postinstall.Unknown || task.VerificationNeedsRoot
			if problem != group.problem {
				continue
			}
			if !shown {
				if _, err := fmt.Fprintf(out, "\n%s:\n", group.title); err != nil {
					return err
				}
				shown = true
			}
			if _, err := fmt.Fprintf(out, "  %s: %s\n    nimbus postinstall %s\n", task.ID, task.Detail, task.ID); err != nil {
				return err
			}
		}
	}

	if result.Verbose {
		for _, notice := range result.Historical {
			if _, err := fmt.Fprintln(out, "Notice: "+notice); err != nil {
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
	phases := []string{"repository preflight", "repository update", "system sync", "Chezmoi apply", "software updates", "agent model refresh", "final inspection"}
	start := slices.Index(phases, phase)
	for _, name := range phases[max(0, start+1):] {
		if (name == "software updates" || name == "agent model refresh") && !upgrade {
			continue
		}
		if !slices.ContainsFunc(result.Steps, func(s runStep) bool { return strings.EqualFold(s.Name, name) }) {
			result.Steps = append(result.Steps, runStep{Name: name, Status: "skipped", Detail: "an earlier phase did not complete"})
		}
	}
}
