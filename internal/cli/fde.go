package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/spf13/cobra"
)

// fdeVerificationSource allows only the fixed image inspection to use sudo.
// It is constructed after approval in the explicit task, never for status.
type fdeVerificationSource struct{ native.Source }

func (s fdeVerificationSource) Run(name string, args ...string) ([]byte, error) {
	if name == postinstall.FDEUKITool && len(args) == 2 && args[0] == "inspect" && args[1] == postinstall.FDEUKIPath {
		return s.Source.Run("sudo", "-n", "--", postinstall.FDEUKITool, args[0], args[1])
	}
	return s.Source.Run(name, args...)
}

// rootVerification runs the approved, read-only administrator checks shared
// by tasks whose completion cannot be verified without root.
func rootVerification(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, task postinstall.Task, preview string, verify func(native.Source, postinstall.Task) postinstall.Task, yes bool) (postinstall.Task, error) {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), preview); err != nil {
		return task, err
	}
	if !yes {
		if !postinstallTerminal(cmd.InOrStdin()) {
			return task, usageError{errors.New("administrator verification requires a terminal or explicit --yes; use --plan to inspect it")}
		}
		if !approver(cmd.InOrStdin(), cmd.OutOrStdout(), "") {
			return task, errors.New("verification declined; completion was not recorded")
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
		return task, errors.New("task selection or native state changed during approval; retry verification")
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

func runFDEVerification(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, task postinstall.Task, yes bool) error {
	if task.VerificationNeedsRoot {
		preview := fmt.Sprintf("Read-only administrator verification:\n  sudo -- ukify inspect %s (image sections and embedded command line)\nNo changes will be made.\n", postinstall.FDEUKIPath)
		verified, err := rootVerification(cmd, src, before, task, preview, func(s native.Source, t postinstall.Task) postinstall.Task {
			return postinstall.VerifyFDE(fdeVerificationSource{s}, t)
		}, yes)
		if err != nil {
			return err
		}
		task = verified
	}
	if err := renderPostinstallStatus(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{task}}); err != nil {
		return err
	}
	if task.Status == postinstall.NotApplicable {
		return nil
	}
	if task.Status != postinstall.Complete {
		return fmt.Errorf("completion not recorded: %s", task.Detail)
	}
	if err := recordTask(before.view.Machine, task.ID+".complete", "verified"); err != nil {
		return err
	}
	_, err := io.WriteString(cmd.OutOrStdout(), "Completion recorded. Routine status remains unprivileged and may still need administrator verification.\n")
	return err
}

func runFDESetup(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, task postinstall.Task) error {
	if err := postinstall.RunFDESetup(cmd.Context(), src, cmd.OutOrStdout(), cmd.ErrOrStderr(), task); err != nil {
		return fmt.Errorf("postinstall fde action failed: %w", err)
	}
	verified := postinstall.VerifyFDE(fdeVerificationSource{src}, task)
	if err := renderPostinstallStatus(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{verified}}); err != nil {
		return err
	}
	if verified.Status != postinstall.Complete {
		return fmt.Errorf("setup finished but verification did not complete: %s", verified.Detail)
	}
	if err := recordTask(before.view.Machine, task.ID+".complete", "verified"); err != nil {
		return err
	}
	_, err := io.WriteString(cmd.OutOrStdout(), "Reboot to boot the Nimbus image. Completion recorded; routine status may need administrator verification for the image content.\n")
	return err
}
