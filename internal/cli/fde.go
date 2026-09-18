package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/inspect"
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

// fdeVerify runs the approved read-only checks; a damaged or stale image
// comes back Pending with the repair action so the caller can offer it.
func fdeVerify(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, task postinstall.Task, yes bool) (postinstall.Task, error) {
	preview := fmt.Sprintf("Read-only administrator verification:\n  sudo -- ukify inspect %s (sections, kernel release and embedded command line)\n  sudo -- mokutil --ignore-keyring --test-key %s (enrollment state)\n  sudo -- cryptsetup luksDump --dump-json-metadata <luks-device> (TPM token state)\n  sudo -- nimbus internal fde-uki state (ownership record)\nNo changes will be made.\n", postinstall.FDEUKIPath, postinstall.FDEMOKCertificate())
	return rootVerification(cmd, src, before, task, preview, func(s native.Source, t postinstall.Task) postinstall.Task {
		return postinstall.VerifyFDE(fdeVerificationSource{s}, t)
	}, yes)
}

// runFDERemoveAction removes only Nimbus's FDE ownership. Removal is
// destructive, so it requires interactive confirmation and --yes cannot
// accept it.
func runFDERenew(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, task postinstall.Task) error {
	if err := postinstall.RunFDERenew(cmd.Context(), src, cmd.OutOrStdout(), cmd.ErrOrStderr(), task); err != nil {
		return fmt.Errorf("postinstall fde renewal failed: %w", err)
	}
	verified := postinstall.VerifyFDE(fdeVerificationSource{src}, task)
	if err := renderPostinstallStatus(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{verified}}); err != nil {
		return err
	}
	if verified.Status != postinstall.Complete {
		return fmt.Errorf("renewal finished but verification did not complete: %s", verified.Detail)
	}
	if err := recordTask(before.view.Machine, postinstall.FDEEvidence, "verified"); err != nil {
		return err
	}
	_, err := io.WriteString(cmd.OutOrStdout(), "Renewal recorded. Reboot to confirm automatic unlock; the disk passphrase remains the fallback.\n")
	return err
}

func runFDERemoveAction(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, flags machineFlags, yes, preview bool) error {
	task := postinstall.Task{ID: "fde", Owner: "component:fde", Title: "Remove TPM automatic disk unlock",
		Status: postinstall.Pending, Action: &postinstall.Action{Kind: postinstall.RemoveFDE}}
	commands, err := postinstall.FDERemoveCommands(task)
	if err != nil {
		return err
	}
	task.Verification = "The removal lists only what Nimbus owns: the recorded TPM keyslot, the Nimbus firmware entries, the image, the marker and the key material."
	task.Recovery = "The disk passphrase and Fedora's GRUB entries remain a valid unlock and boot path. The MOK certificate stays enrolled; remove it with mokutil if desired."
	if err := renderPostinstall(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{task}}); err != nil {
		return err
	}
	_ = commands
	if preview {
		_, err := io.WriteString(cmd.OutOrStdout(), "No changes will be made.\n")
		return err
	}
	if yes {
		return usageError{errors.New("FDE removal requires interactive confirmation; --yes does not accept it")}
	}
	if !postinstallTerminal(cmd.InOrStdin()) {
		return usageError{errors.New("FDE removal requires a terminal; use --plan to inspect it")}
	}
	if _, err := io.WriteString(cmd.OutOrStdout(), "Remove the Nimbus TPM enrollment, firmware entry, image, marker and key material? The disk passphrase and Fedora's GRUB entries remain. Default: No.\n"); err != nil {
		return err
	}
	if !confirmDefaultNo(cmd.InOrStdin(), cmd.OutOrStdout(), "Remove Nimbus's FDE ownership? [y/N] ") {
		return errors.New("FDE removal not approved; nothing was run")
	}
	path, err := apply.LockPath()
	if err != nil {
		return err
	}
	lock, err := apply.Acquire(path, apply.LockInfo{Command: "postinstall fde --remove", Operation: before.digest, PID: os.Getpid(), Started: time.Now().UTC()})
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	fresh, err := inspectPostinstall(src, flags, "fde")
	if err != nil {
		return err
	}
	if fresh.digest != before.digest {
		return errors.New("native state changed after approval; inspect and retry the removal")
	}
	if err := inspect.CheckPlatform(src, fresh.selected.Checkout.Definitions().Compatibility.Fedora); err != nil {
		return err
	}
	if err := postinstall.RunFDERemove(cmd.Context(), src, cmd.OutOrStdout(), cmd.ErrOrStderr(), task); err != nil {
		return fmt.Errorf("postinstall fde removal failed: %w", err)
	}
	if err := resetTask(before.view.Machine, "fde"); err != nil {
		return err
	}
	_, err = io.WriteString(cmd.OutOrStdout(), "Removal completed. The Nimbus MOK certificate stays enrolled; remove it with mokutil if desired. The fde component can now be deselected and its packages removed.\n")
	return err
}

func runFDEEnroll(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, task postinstall.Task) error {
	if err := postinstall.RunFDEEnroll(cmd.Context(), src, cmd.OutOrStdout(), cmd.ErrOrStderr(), task); err != nil {
		return fmt.Errorf("postinstall fde enrollment failed: %w", err)
	}
	verified := postinstall.VerifyFDE(fdeVerificationSource{src}, task)
	if err := renderPostinstallStatus(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{verified}}); err != nil {
		return err
	}
	if verified.Status != postinstall.Complete {
		return fmt.Errorf("enrollment finished but verification did not complete: %s", verified.Detail)
	}
	if err := recordTask(before.view.Machine, postinstall.FDEEvidence, "verified"); err != nil {
		return err
	}
	_, err := io.WriteString(cmd.OutOrStdout(), "Enrollment recorded. Reboot to confirm automatic unlock; the disk passphrase remains the fallback.\n")
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
		if verified.Action != nil && verified.Action.Kind == postinstall.EnrollFDE {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Setup finished. Reboot into the Nimbus image, then run this task again to enroll automatic TPM unlock.")
			return err
		}
		if verified.FDESecure() && strings.Contains(verified.Detail, "MOK enrollment is pending") {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Setup finished. Complete Enroll MOK at the next reboot, then run this task again to enroll automatic TPM unlock.")
			return err
		}
		return fmt.Errorf("setup finished but verification did not complete: %s", verified.Detail)
	}
	if err := recordTask(before.view.Machine, postinstall.FDEEvidence, "verified"); err != nil {
		return err
	}
	_, err := io.WriteString(cmd.OutOrStdout(), "Reboot to boot the Nimbus image. Completion recorded; routine status may need administrator verification for the image content.\n")
	return err
}
