package cli

import (
	"fmt"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/spf13/cobra"
)

// rootVerification runs the approved, read-only administrator checks shared
// by tasks whose completion cannot be verified without root.
func rootVerification(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, task postinstall.Task, preview string, verify func(native.Source, postinstall.Task) postinstall.Task, yes bool) (postinstall.Task, error) {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), preview); err != nil {
		return task, err
	}
	if !yes {
		if !postinstallTerminal(cmd.InOrStdin()) {
			return task, usageError{fmt.Errorf("administrator verification requires a terminal or explicit --yes; use --plan to inspect it")}
		}
		if !approver(cmd.InOrStdin(), cmd.OutOrStdout(), "") {
			return task, fmt.Errorf("verification declined; completion was not recorded")
		}
	}
	if err := src.Stream(cmd.OutOrStdout(), cmd.ErrOrStderr(), "sudo", "-v"); err != nil {
		return task, fmt.Errorf("administrator verification unavailable: %w", err)
	}
	fresh, err := inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine}, task.ID)
	if err != nil {
		return task, err
	}
	if fresh.nativeDigest != before.nativeDigest {
		return task, fmt.Errorf("task selection or native state changed during approval; retry verification")
	}
	freshTask, err := selectedTask(fresh.view, task.ID)
	if err != nil {
		return task, err
	}
	if !freshTask.VerificationNeedsRoot {
		return freshTask, nil
	}
	return verify(src, freshTask), nil
}
