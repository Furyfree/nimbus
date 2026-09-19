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
func inspectFinal(src native.Source, s *selected, result *syncResult, notes bool) {
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
				// Pending duplicates the reboot/logout line or the final
				// block; blocked and unverifiable states still need the text.
				if t.Status != postinstall.Pending {
					result.Notices = append(result.Notices, t.ID+": "+t.Detail)
				}
				continue
			}
			result.Tasks = append(result.Tasks, t)
		}
	}
	if notes {
		result.Notes, err = pendingNotes(src, s)
		if err != nil {
			result.Notices = append(result.Notices, "Setup guidance unavailable: "+err.Error())
		}
	}
}
func renderFinalDetails(out io.Writer, result *syncResult, install bool) error {
	if !install {
		if result.Reboot {
			if _, err := fmt.Fprintln(out, "Reboot required to activate changed boot or greeter configuration."); err != nil {
				return err
			}
		}
		for _, task := range result.Tasks {
			if result.Reboot && task.BeforeReboot && task.Status == postinstall.Pending {
				if _, err := fmt.Fprintf(out, "Run before rebooting: nimbus postinstall %s (%s)\n", task.ID, task.Title); err != nil {
					return err
				}
			}
		}
		if result.Logout {
			if _, err := fmt.Fprintln(out, "Log out and back in to activate changed session settings or group membership."); err != nil {
				return err
			}
		}
	}
	problems, remaining := false, 0
	for _, task := range result.Tasks {
		if !reportProblem(task) {
			remaining++
			continue
		}
		if !problems {
			if _, err := fmt.Fprintln(out, "\nVerification problems:"); err != nil {
				return err
			}
			problems = true
		}
		if _, err := fmt.Fprintf(out, "  %s: %s\n    nimbus postinstall %s\n", task.ID, task.Detail, task.ID); err != nil {
			return err
		}
	}
	if !install && (remaining > 0 || len(result.Notes) > 0) {
		if _, err := fmt.Fprintf(out, "\nRemaining setup: %s. Run: nimbus setup-notes\n", taskNoteCounts(remaining, len(result.Notes))); err != nil {
			return err
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
	return nil
}

// nimbusBanner marks the finished installation without carrying meaning in
// color or width, so it stays readable in transcripts and narrow terminals.
const nimbusBanner = `    _   _ ___ __  __ ___  _   _ ___
   | \ | |_ _|  \/  | _ )| | | / __|
   |  \| || || |\/| | _ \| |_| \__ \
   |_|\_|___|_|  |_|___/ \___/|___/`

// renderInstallFinish closes init: log location, the tasks that must run
// before the requested reboot, the reboot or logout instruction and the
// after-reboot pointer to the guidance catalog. Sync keeps the plain lines.
func renderInstallFinish(out io.Writer, result *syncResult, logDir string) error {
	if _, err := fmt.Fprintf(out, "\n%s\n\nLogs: %s\n", nimbusBanner, logDir); err != nil {
		return err
	}
	if result.Reboot {
		var before []postinstall.Task
		for _, task := range result.Tasks {
			if task.BeforeReboot && task.Status == postinstall.Pending {
				before = append(before, task)
			}
		}
		if len(before) > 0 {
			if _, err := fmt.Fprintln(out, "\nBefore rebooting:"); err != nil {
				return err
			}
			for _, task := range before {
				if _, err := fmt.Fprintf(out, "  nimbus postinstall %-16s %s\n", task.ID, task.Title); err != nil {
					return err
				}
			}
		}
	}
	remaining, notes := 0, len(result.Notes)
	for _, task := range result.Tasks {
		if !reportProblem(task) {
			remaining++
		}
	}
	if remaining > 0 || notes > 0 {
		lead := "Then open a terminal and run:"
		switch {
		case result.Reboot:
			if _, err := fmt.Fprintln(out, "\nReboot to finish."); err != nil {
				return err
			}
		case result.Logout:
			if _, err := fmt.Fprintln(out, "\nLog out and back in to finish."); err != nil {
				return err
			}
		default:
			lead = "Open a terminal and run:"
		}
		if _, err := fmt.Fprintf(out, "%s\n  nimbus setup-notes\n    remaining setup and guidance: %s\n", lead, taskNoteCounts(remaining, notes)); err != nil {
			return err
		}
		return nil
	}
	if result.Reboot {
		if _, err := fmt.Fprintln(out, "\nReboot to finish."); err != nil {
			return err
		}
	} else if result.Logout {
		if _, err := fmt.Fprintln(out, "\nLog out and back in to finish."); err != nil {
			return err
		}
	}
	return nil
}

// reportProblem marks a task that failed or could not be verified. A session
// check before first login is expected to be unknown and stays in remaining
// setup instead.
func reportProblem(task postinstall.Task) bool {
	return !task.Session && (task.Status == postinstall.Unknown || task.VerificationNeedsRoot)
}

func taskNoteCounts(tasks, notes int) string {
	var parts []string
	switch {
	case tasks == 1:
		parts = append(parts, "1 task")
	case tasks > 1:
		parts = append(parts, fmt.Sprintf("%d tasks", tasks))
	}
	switch {
	case notes == 1:
		parts = append(parts, "1 setup note")
	case notes > 1:
		parts = append(parts, fmt.Sprintf("%d setup notes", notes))
	}
	return strings.Join(parts, ", ")
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
