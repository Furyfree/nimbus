package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/spf13/cobra"
)

// Only the fixed public certificate and enrollment check may use sudo.
// This wrapper is constructed after approval in the explicit task, never status.
type mokVerificationSource struct{ native.Source }

func (s mokVerificationSource) ReadFile(path string) ([]byte, error) {
	if path == postinstall.MOKCertificate {
		return s.Source.Run("sudo", "-n", "--", "/usr/bin/cat", "--", path)
	}
	return s.Source.ReadFile(path)
}
func (s mokVerificationSource) Run(name string, args ...string) ([]byte, error) {
	if name == "mokutil" && len(args) == 3 && args[0] == "--ignore-keyring" && args[1] == "--test-key" && args[2] == postinstall.MOKCertificate {
		return s.Source.Run("sudo", "-n", "--", "/usr/bin/mokutil", args[0], args[1], args[2])
	}
	return s.Source.Run(name, args...)
}

func runMOKVerification(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, task postinstall.Task, yes bool) error {
	if task.VerificationNeedsRoot {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Read-only administrator verification:\n  sudo -- /usr/bin/cat -- %s (public certificate; output stays private)\n  sudo -- /usr/bin/mokutil --ignore-keyring --test-key %s\nNo keys will be generated or enrolled.\n", postinstall.MOKCertificate, postinstall.MOKCertificate); err != nil {
			return err
		}
		if !yes {
			if !postinstallTerminal(cmd.InOrStdin()) {
				return usageError{errors.New("administrator verification requires a terminal or explicit --yes; use --plan to inspect it")}
			}
			if !approver(cmd.InOrStdin(), cmd.OutOrStdout(), "") {
				return errors.New("verification declined; completion was not recorded")
			}
		}
		if err := src.Stream(cmd.OutOrStdout(), cmd.ErrOrStderr(), "sudo", "-v"); err != nil {
			return fmt.Errorf("administrator verification unavailable: %w", err)
		}
		// Recheck selection and ordinary native state after the approval prompt.
		fresh, err := inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine}, "nvidia-mok")
		if err != nil {
			return err
		}
		if fresh.nativeDigest != before.nativeDigest {
			return errors.New("task selection or native state changed during approval; retry verification")
		}
		task, err = selectedTask(fresh.view, task.ID)
		if err != nil {
			return err
		}
		if task.VerificationNeedsRoot {
			task = postinstall.VerifyMOK(mokVerificationSource{src}, task)
		}
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
