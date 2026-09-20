package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/Furyfree/nimbus/internal/userstate"
	"github.com/spf13/cobra"
)

func passwordSelection(src native.Source) (inspect.Chezmoi, error) {
	data, err := src.Run("chezmoi", inspect.ChezmoiDataArgs...)
	if err != nil {
		return inspect.Chezmoi{}, errors.New("Chezmoi selection is unavailable; complete nimbus init")
	}
	return inspect.ParseChezmoiData(data)
}

func samePasswordSelection(a, b inspect.Chezmoi) bool {
	return a.Initialized == b.Initialized && a.Machine == b.Machine &&
		a.ManagedByNimbus == b.ManagedByNimbus && a.OnePasswordSSH == b.OnePasswordSSH &&
		slices.Equal(a.Profiles, b.Profiles)
}

// Changing the stored choice is separate from confirming GUI prerequisites or
// approving a file apply. --yes never supplies this opt-in.
func offerPasswordIntegration(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, selection inspect.Chezmoi) (bool, error) {
	if selection.OnePasswordSSH {
		return false, nil
	}
	out := cmd.OutOrStdout()
	if !postinstallTerminal(cmd.InOrStdin()) {
		_, err := fmt.Fprintln(out, "SSH/Git integration is not selected. Run nimbus postinstall onepassword interactively to enable it; --yes does not opt in.")
		return false, err
	}
	if _, err := fmt.Fprintln(out, "Optional SSH/Git integration: use 1Password as the default SSH agent and Git signing helper.\nChezmoi will manage SSH config, GitHub/Homelab public-key selectors, agent selection and Git config. Existing keys are reused.\nEnabling saves the choice through chezmoi init --prompt, preserving your machine and profiles.\nNo files are applied or scripts run at this step. After GUI setup, review the selected files before applying.\nThe choice stays enabled if later setup fails; rerun this task to finish."); err != nil {
		return false, err
	}
	answer, err := promptLineFn(cmd.InOrStdin(), out, "Enable 1Password SSH/Git integration? (yes/no)", "no")
	if err != nil {
		return false, fmt.Errorf("SSH/Git choice was not recorded: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "no", "n":
		_, err := fmt.Fprintln(out, "Keeping CLI-only setup; SSH/Git configuration will not be applied.")
		return false, err
	case "yes", "y":
	default:
		return false, errors.New("answer yes or no; SSH/Git choice was not changed")
	}
	lockPath, err := apply.LockPath()
	if err != nil {
		return false, err
	}
	lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: "postinstall onepassword", PID: os.Getpid(), Started: time.Now().UTC()})
	if err != nil {
		return false, err
	}
	defer lock.Release()
	fresh, err := passwordSelection(src)
	if err != nil || !samePasswordSelection(fresh, selection) {
		return false, errors.New("Chezmoi selection changed during approval; retry the task")
	}
	checked, err := inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine}, "onepassword")
	if err != nil || checked.digest != before.digest {
		return false, errors.New("postinstall state changed after approval; retry the task")
	}
	if err := cmd.Context().Err(); err != nil {
		return false, err
	}
	args := []string{"init", "--prompt", "--promptString", "Machine=" + selection.Machine,
		"--promptBool", "ManagedByNimbus=true",
		"--promptMultichoice", "Profiles=" + strings.Join(selection.Profiles, "/"),
		"--promptBool", "Enable 1Password SSH integration=true"}
	if err := src.Stream(out, cmd.ErrOrStderr(), "chezmoi", args...); err != nil {
		return false, errors.New("Chezmoi could not save the SSH/Git choice; inspect its configuration before retrying; no integration files were applied")
	}
	fresh, err = passwordSelection(src)
	selection.OnePasswordSSH = true
	if err != nil || !samePasswordSelection(fresh, selection) {
		return false, errors.New("Chezmoi did not retain the requested SSH/Git choice and machine profiles; inspect its configuration before retrying; no integration files were applied")
	}
	_, err = fmt.Fprintln(out, "SSH/Git integration enabled in Chezmoi. Continue with GUI prerequisites, then review the files.")
	return true, err
}

// SSH adds manual GUI prerequisites, so enabling it requires fresh confirmation.
func passwordManualRevision(ssh bool) int {
	if ssh {
		return 2
	}
	return 1
}
func passwordConfirmed(machine string, ssh bool) bool {
	store, err := userstate.Default()
	if err != nil {
		return false
	}
	evidence, err := store.Read("postinstall")
	return err == nil && evidence.Has(machine, "onepassword.manual", passwordManualRevision(ssh), "confirmed")
}
func confirmPasswordState(machine string, ssh bool) error {
	store, err := userstate.Default()
	if err != nil {
		return err
	}
	return store.Update("postinstall", machine, map[string]userstate.Evidence{"onepassword.manual": {Revision: passwordManualRevision(ssh), Source: "confirmed"}}, nil)
}
func passwordTargets(ssh bool) []string {
	if !ssh {
		return nil
	}
	home, _ := os.UserHomeDir()
	paths := []string{".ssh/config", ".ssh/github.pub", ".ssh/homelab.pub", ".config/1Password/ssh/agent.toml", ".config/git/config"}
	for i, p := range paths {
		paths[i] = filepath.Join(home, p)
	}
	return paths
}

var errPasswordNotSignedIn = errors.New("1Password CLI account is not signed in; run op signin, then retry verification")

func passwordPrerequisites(src native.Source, out io.Writer, ssh bool) error {
	if _, err := src.LookPath("op"); err != nil {
		return errors.New("1Password CLI is missing; install the selected prerequisites with nimbus sync")
	}
	if _, err := fmt.Fprintln(out, "Checking desktop CLI access (account output is not displayed or logged)..."); err != nil {
		return err
	}
	// This check is permitted only inside the explicit guided flow, never status,
	// plan, automatic reporting or an unattended --yes confirmation.
	if _, err := src.Run("op", "whoami", "--format=json"); err != nil {
		if strings.Contains(err.Error(), "account is not signed in") {
			return errPasswordNotSignedIn
		}
		return errors.New("1Password CLI access could not be verified; run op whoami directly for the native diagnostic, then retry")
	}
	if ssh {
		if _, err := src.LookPath("/opt/1Password/op-ssh-sign"); err != nil {
			return errors.New("1Password SSH signing helper is unavailable")
		}
		home, _ := os.UserHomeDir()
		socket := filepath.Join(home, ".1password", "agent.sock")
		// ssh-add only lists public identities. Suppress identities and native errors.
		if _, err := src.Run("env", "SSH_AUTH_SOCK="+socket, "ssh-add", "-l"); err != nil {
			return errors.New("1Password SSH agent has no accessible identities; enable the agent and keep the app unlocked")
		}
	}
	return nil
}

// Only an explicitly selected task offers sign-in. Shared init checks and
// routine inspection must not start this recovery flow implicitly.
func passwordTaskPrerequisites(cmd *cobra.Command, src native.Source, ssh, yes bool) error {
	err := passwordPrerequisites(src, cmd.OutOrStdout(), ssh)
	if !errors.Is(err, errPasswordNotSignedIn) {
		return err
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "The CLI account is not signed in. Run op signin to authorize it through 1Password, then retry verification.\nNative action: op signin (sign-in output is discarded; native prompts are not logged)"); err != nil {
		return err
	}
	if !yes {
		if !postinstallTerminal(cmd.InOrStdin()) {
			return errors.New("CLI sign-in requires a terminal or explicit --yes; run op signin and retry this task")
		}
		if !approver(cmd.InOrStdin(), cmd.OutOrStdout(), "") {
			return errors.New("CLI sign-in declined; GUI confirmation is saved and verification remains incomplete")
		}
	}
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	// Stream preserves native input and account-selection prompts. Never expose
	// stdout: manual CLI authentication can return session material there.
	if err := src.Stream(io.Discard, cmd.ErrOrStderr(), "op", "signin"); err != nil {
		return errors.New("1Password CLI sign-in failed or was canceled; completion was not recorded; retry this task after signing in")
	}
	// Sign-in exit status alone does not establish account or agent readiness.
	return passwordPrerequisites(src, cmd.OutOrStdout(), ssh)
}
func passwordFingerprint(src native.Source, ssh bool) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "ssh=%t\n", ssh)
	for _, path := range passwordTargets(ssh) {
		data, err := src.ReadFile(path)
		if err != nil || len(data) == 0 {
			return "", errors.New("required integration files are missing or unreadable")
		}
		fmt.Fprintf(h, "%s:%d\n", path, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Chezmoi includes the same managed parents in status and apply. Contents stay
// private; only paths and native create/update actions appear in the summary.
func passwordFileStatus(src native.Source, targets []string) ([]byte, error) {
	status, err := src.Run("chezmoi", append([]string{"--color=false", "status", "--parent-dirs", "--exclude=scripts", "--"}, targets...)...)
	if err != nil {
		return nil, errors.New("Chezmoi could not inspect the selected SSH/Git changes; no configuration was applied")
	}
	return status, nil
}

func renderPasswordFileStatus(out io.Writer, status []byte) error {
	if _, err := fmt.Fprintln(out, "Configure selected SSH/Git files and their managed parent directories:"); err != nil {
		return err
	}
	for line := range strings.SplitSeq(string(status), "\n") {
		if line == "" {
			continue
		}
		if len(line) < 4 || line[2] != ' ' {
			return errors.New("unexpected Chezmoi file status; no configuration was applied")
		}
		var action string
		switch line[1] {
		case 'A':
			action = "Create"
		case 'M':
			action = "Update"
		case ' ':
			continue
		default:
			return errors.New("unexpected Chezmoi file action; no configuration was applied")
		}
		if _, err := fmt.Fprintf(out, "  %s %s\n", action, line[3:]); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out, "Scripts excluded. Existing file-conflict prompts remain enabled.")
	return err
}

func runOnePassword(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, t postinstall.Task, yes, verifyOnly, showDiff bool) error {
	if t.Status == postinstall.Blocked {
		return errors.New(t.Detail)
	}
	selection, err := passwordSelection(src)
	if err != nil {
		return err
	}
	if !selection.ManagedByNimbus || selection.Machine != before.view.Machine {
		return errors.New("Chezmoi belongs to another selection; run nimbus init")
	}
	if !verifyOnly {
		changed, err := offerPasswordIntegration(cmd, src, before, selection)
		if err != nil {
			return err
		}
		if changed {
			selection.OnePasswordSSH = true
			before, err = inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine}, "onepassword")
			if err != nil {
				return err
			}
			t, err = selectedTask(before.view, t.ID)
			if err != nil {
				return err
			}
			if t.Status == postinstall.Blocked {
				return errors.New(t.Detail)
			}
		}
	}
	if t.Status == postinstall.Complete && !verifyOnly {
		return renderPostinstall(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{t}})
	}
	message := "Open 1Password, sign in and unlock it. Enable Integrate with 1Password CLI in Settings > Developer.\nSkip manual SSH/Git file edits; Nimbus will apply the selected integration through Chezmoi."
	if selection.OnePasswordSSH {
		message += "\nEnable the SSH agent in Settings > Developer and keep 1Password running. Reuse your existing keys."
	} else {
		message += "\nSSH integration is not selected; this task will not configure it. Run nimbus postinstall onepassword interactively to opt in when wanted."
	}
	if verifyOnly {
		message = strings.ReplaceAll(message, "Nimbus will apply the selected integration through Chezmoi.", "this command only verifies existing integration.")
		message += "\nVerify existing setup only; no configuration will be applied."
	}
	message += "\nVerification may request 1Password authorization. Account and key output stays private."
	if !passwordConfirmed(before.view.Machine, selection.OnePasswordSSH) {
		if err := confirmManual(cmd, message+"\nReady to check prerequisites and continue?"); err != nil {
			return err
		}
		if err := confirmPasswordState(before.view.Machine, selection.OnePasswordSSH); err != nil {
			return err
		}
	} else {
		message = "GUI prerequisites were confirmed on this machine. Verification may request 1Password authorization; account and key output stays private."
		if verifyOnly {
			message += "\nVerify existing setup only; no configuration will be applied."
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), message); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "GUI prerequisites confirmed. Verifying selected integration..."); err != nil {
		return err
	}
	freshBefore, err := inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine}, "onepassword")
	if err != nil {
		return err
	}
	currentTask, err := selectedTask(freshBefore.view, t.ID)
	if err != nil || currentTask.Status == postinstall.Blocked || freshBefore.nativeDigest != before.nativeDigest {
		return errors.New("postinstall ownership or definitions changed after approval; retry the task")
	}
	if err := passwordTaskPrerequisites(cmd, src, selection.OnePasswordSSH, yes); err != nil {
		return err
	}
	targets := passwordTargets(selection.OnePasswordSSH)
	verifiedFingerprint := ""
	if !verifyOnly && len(targets) > 0 {
		existing, readErr := passwordFingerprint(src, selection.OnePasswordSSH)
		if readErr == nil {
			if _, verifyErr := src.Run("chezmoi", append([]string{"verify", "--exclude=scripts", "--"}, targets...)...); verifyErr == nil {
				verifyOnly, verifiedFingerprint = true, existing
			}
		}
	}
	if verifyOnly && verifiedFingerprint == "" {
		verifiedFingerprint, err = passwordFingerprint(src, selection.OnePasswordSSH)
		if err != nil {
			return fmt.Errorf("existing integration is incomplete; no configuration was applied. Run nimbus postinstall onepassword: %w", err)
		}
		if len(targets) > 0 {
			if _, err := src.Run("chezmoi", append([]string{"verify", "--exclude=scripts", "--"}, targets...)...); err != nil {
				return errors.New("GUI confirmation saved, but existing integration could not be verified; no configuration was applied. Run nimbus postinstall onepassword to review and repair it")
			}
		}
	}
	if len(targets) > 0 && !verifyOnly {
		status, err := passwordFileStatus(src, targets)
		if err != nil {
			return err
		}
		if err := renderPasswordFileStatus(cmd.OutOrStdout(), status); err != nil {
			return err
		}
		if showDiff {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Private SSH/Git diff (not logged):"); err != nil {
				return err
			}
			if err := src.Stream(cmd.OutOrStdout(), cmd.ErrOrStderr(), "chezmoi", append([]string{"--no-pager", "diff", "--parent-dirs", "--exclude=scripts", "--"}, targets...)...); err != nil {
				return errors.New("Chezmoi could not preview the selected integration; see terminal diagnostics")
			}
		}
		expected, err := src.Run("chezmoi", append([]string{"cat", "--"}, targets...)...)
		if err != nil {
			return errors.New("cannot render integration for approval")
		}
		expectedHash := sha256.Sum256(expected)
		expected = nil
		approved, err := inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine}, "onepassword")
		if err != nil {
			return err
		}
		if !yes && !approver(cmd.InOrStdin(), cmd.OutOrStdout(), "") {
			return errors.New("configuration apply declined; task remains pending")
		}
		lockPath, err := apply.LockPath()
		if err != nil {
			return err
		}
		lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: "postinstall onepassword", PID: os.Getpid(), Started: time.Now().UTC()})
		if err != nil {
			return err
		}
		defer lock.Release()
		fresh, err := passwordSelection(src)
		if err != nil || !samePasswordSelection(fresh, selection) {
			return errors.New("Chezmoi selection changed during approval; retry the task")
		}
		checked, err := inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine}, "onepassword")
		if err != nil || checked.digest != approved.digest {
			return errors.New("postinstall state changed after approval; retry the task")
		}
		checkContent, err := src.Run("chezmoi", append([]string{"cat", "--"}, targets...)...)
		if err != nil || sha256.Sum256(checkContent) != expectedHash {
			return errors.New("selected integration changed after preview; review again")
		}
		checkContent = nil
		checkedStatus, err := passwordFileStatus(src, targets)
		if err != nil || !bytes.Equal(checkedStatus, status) {
			return errors.New("selected files or parent directories changed after preview; review again")
		}
		if err := src.Stream(cmd.OutOrStdout(), cmd.ErrOrStderr(), "chezmoi", append([]string{"apply", "--parent-dirs", "--exclude=scripts", "--"}, targets...)...); err != nil {
			return errors.New("Chezmoi integration apply failed; files may be partly updated; retry after fixing the reported error")
		}
		if _, err := src.Run("chezmoi", append([]string{"verify", "--exclude=scripts", "--"}, targets...)...); err != nil {
			return errors.New("selected Chezmoi integration does not verify; task remains pending")
		}
		if err := passwordTaskPrerequisites(cmd, src, true, yes); err != nil {
			return err
		}
	}
	freshSelection, err := passwordSelection(src)
	if err != nil || !samePasswordSelection(freshSelection, selection) {
		return errors.New("Chezmoi selection changed during verification; retry the task")
	}
	fingerprint, err := passwordFingerprint(src, selection.OnePasswordSSH)
	if err != nil {
		return err
	}
	if verifyOnly && fingerprint != verifiedFingerprint {
		return errors.New("integration changed during verification; retry without recording completion")
	}
	store, err := userstate.Default()
	if err != nil {
		return err
	}
	if err := store.Update("postinstall", before.view.Machine, map[string]userstate.Evidence{t.ID + ".complete": {Revision: 1, Source: "verified", Fingerprint: fingerprint}}, nil); err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), "1Password setup complete: GUI steps confirmed; selected local integration verified. Remote SSH access and signing-key registration were not tested.")
	return err
}

func enrichPostinstall(src native.Source, s *selected, view *postinstallView) error {
	store, err := userstate.Default()
	if err != nil {
		return err
	}
	evidence, err := store.Read("postinstall")
	if err != nil {
		return fmt.Errorf("local setup state: %w", err)
	}
	for i := range view.Tasks {
		t := &view.Tasks[i]
		if t.ID == "nvidia-mok" && t.VerificationNeedsRoot && evidence.Has(view.Machine, t.ID+".complete", 1, "verified") {
			t.PreviouslyVerified = true
			t.Detail = "Enrollment verified earlier; current check requires sudo. Recheck: nimbus postinstall nvidia-mok (requests sudo)."
		}
		if t.ID != "onepassword" || t.Action == nil {
			continue
		}
		t.Action = nil
		t.Instructions = []string{"Sign in, unlock, and enable desktop CLI integration. If SSH is selected, enable its agent.", "Skip manual SSH/Git file edits; Chezmoi applies selected files after review. Run this task to check prerequisites and complete configuration."}
		t.Status, t.Detail = postinstall.Pending, "GUI prerequisites have not been confirmed on this machine."
		selection, err := passwordSelection(src)
		if err != nil {
			t.Status, t.Detail = postinstall.Unknown, err.Error()
			continue
		}
		if !selection.ManagedByNimbus || selection.Machine != s.Resolved.Machine {
			t.Status, t.Detail = postinstall.Blocked, "Chezmoi selection does not match this machine; run nimbus init."
			continue
		}
		if selection.OnePasswordSSH {
			t.Instructions = append(t.Instructions, "After confirmation, preview, apply and verify only these Chezmoi targets (scripts excluded): "+strings.Join(passwordTargets(true), ", "))
			t.Verification = "Check CLI access, available SSH identities and selected Chezmoi targets before recording completion. Status later checks local files without authenticating; remote access remains a separate check."
		} else {
			t.Instructions = append(t.Instructions, "SSH is not selected. The guided task offers an explicit opt-in (default: no), saves it through Chezmoi, then confirms GUI prerequisites and previews selected SSH/Git files. Declining keeps CLI-only setup; --mark-done never changes the selection.")
			t.Verification = "Record confirmed GUI prerequisites only after CLI access succeeds. Status does not inspect the current vault unlock."
		}
		if _, err := src.LookPath("op"); err != nil {
			t.Status, t.Detail = postinstall.Blocked, "1Password CLI is missing; run nimbus sync."
			continue
		}
		if !evidence.Has(view.Machine, t.ID+".manual", passwordManualRevision(selection.OnePasswordSSH), "confirmed") {
			continue
		}
		t.Detail = "GUI prerequisites confirmed; selected integration still needs verification. Run this task or use --mark-done for existing setup."
		if !evidence.Has(view.Machine, t.ID+".complete", 1, "verified") {
			continue
		}
		fingerprint, err := passwordFingerprint(src, selection.OnePasswordSSH)
		if err != nil || fingerprint != evidence.Machines[view.Machine][t.ID+".complete"].Fingerprint {
			t.Detail = "Selected integration or its files changed since verification; rerun this task."
			continue
		}
		if selection.OnePasswordSSH {
			if _, err := src.LookPath("/opt/1Password/op-ssh-sign"); err != nil {
				t.Status, t.Detail = postinstall.Blocked, "The selected SSH signing helper is missing; run nimbus sync."
				continue
			}
			// Check current files without asking the vault for fresh secret rendering.
			targets := passwordTargets(true)
			missing := false
			for _, path := range targets {
				data, err := src.ReadFile(path)
				if err != nil || len(strings.TrimSpace(string(data))) == 0 {
					missing = true
					break
				}
			}
			if missing {
				t.Detail = "A required SSH/Git file is missing or unreadable; rerun this task."
				continue
			}
			args := []string{"--skip-secrets", "verify", "--exclude=scripts", "--", targets[3], targets[4]}
			if _, err := src.Run("chezmoi", args...); err != nil {
				t.Status, t.Detail = postinstall.Unknown, "Selected Git/agent configuration could not be verified; rerun this task."
				continue
			}
		}
		t.Status, t.Detail = postinstall.Complete, "GUI prerequisites confirmed; local configuration checked. Current vault unlock and remote access are not inspected by status."
	}
	return nil
}
