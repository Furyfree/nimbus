package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		return errors.New("1Password CLI access failed; unlock the desktop app and enable CLI integration, then retry")
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

func runOnePassword(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, t postinstall.Task, yes bool) error {
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
	if t.Status == postinstall.Complete {
		return renderPostinstall(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{t}})
	}
	message := "Open 1Password, sign in and unlock it. Enable Integrate with 1Password CLI in Settings > Developer.\nSkip manual SSH/Git file edits; Nimbus will apply the selected integration through Chezmoi."
	if selection.OnePasswordSSH {
		message += "\nEnable the SSH agent in Settings > Developer and keep 1Password running. Reuse your existing keys."
	} else {
		message += "\nSSH integration is not selected; this task will not configure it. Opt in through Chezmoi's Enable 1Password SSH integration setting when wanted."
	}
	if !passwordConfirmed(before.view.Machine, selection.OnePasswordSSH) {
		if err := confirmManual(cmd, message+"\nReady to check prerequisites and continue?"); err != nil {
			return err
		}
		if err := confirmPasswordState(before.view.Machine, selection.OnePasswordSSH); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), message+"\nManual prerequisites were confirmed on this machine; checking them again."); err != nil {
			return err
		}
	}
	freshBefore, err := inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine})
	if err != nil {
		return err
	}
	currentTask, err := selectedTask(freshBefore.view, t.ID)
	if err != nil || currentTask.Status == postinstall.Blocked || freshBefore.nativeDigest != before.nativeDigest {
		return errors.New("postinstall ownership or definitions changed after approval; retry the task")
	}
	if err := passwordPrerequisites(src, cmd.OutOrStdout(), selection.OnePasswordSSH); err != nil {
		return err
	}
	targets := passwordTargets(selection.OnePasswordSSH)
	if len(targets) > 0 {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Reviewing only selected SSH/Git files. This preview can contain private host configuration; it is never logged."); err != nil {
			return err
		}
		if err := src.Stream(cmd.OutOrStdout(), cmd.ErrOrStderr(), "chezmoi", append([]string{"diff", "--exclude=scripts", "--"}, targets...)...); err != nil {
			return errors.New("Chezmoi could not preview the selected integration; see terminal diagnostics")
		}
		expected, err := src.Run("chezmoi", append([]string{"cat", "--"}, targets...)...)
		if err != nil {
			return errors.New("cannot render integration for approval")
		}
		expectedHash := sha256.Sum256(expected)
		expected = nil
		approved, err := inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine})
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
		if err != nil || fresh.Machine != selection.Machine || fresh.OnePasswordSSH != selection.OnePasswordSSH || !fresh.ManagedByNimbus {
			return errors.New("Chezmoi selection changed during approval; retry the task")
		}
		checked, err := inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine})
		if err != nil || checked.digest != approved.digest {
			return errors.New("postinstall state changed after approval; retry the task")
		}
		checkContent, err := src.Run("chezmoi", append([]string{"cat", "--"}, targets...)...)
		if err != nil || sha256.Sum256(checkContent) != expectedHash {
			return errors.New("selected integration changed after preview; review again")
		}
		checkContent = nil
		if err := src.Stream(cmd.OutOrStdout(), cmd.ErrOrStderr(), "chezmoi", append([]string{"apply", "--exclude=scripts", "--"}, targets...)...); err != nil {
			return errors.New("Chezmoi integration apply failed; files may be partly updated; retry after fixing the reported error")
		}
		if _, err := src.Run("chezmoi", append([]string{"verify", "--exclude=scripts", "--"}, targets...)...); err != nil {
			return errors.New("selected Chezmoi integration does not verify; task remains pending")
		}
		if err := passwordPrerequisites(src, cmd.OutOrStdout(), true); err != nil {
			return err
		}
	}
	fingerprint, err := passwordFingerprint(src, selection.OnePasswordSSH)
	if err != nil {
		return err
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
			t.Instructions = append(t.Instructions, "SSH is not selected. Confirm the GUI prerequisites and check CLI access; no SSH/Git files will be applied.")
			t.Verification = "Record confirmed GUI prerequisites only after CLI access succeeds. Status does not inspect the current vault unlock."
		}
		if _, err := src.LookPath("op"); err != nil {
			t.Status, t.Detail = postinstall.Blocked, "1Password CLI is missing; run nimbus sync."
			continue
		}
		if !evidence.Has(view.Machine, t.ID+".manual", passwordManualRevision(selection.OnePasswordSSH), "confirmed") {
			continue
		}
		t.Detail = "GUI prerequisites confirmed; run the guided task to apply and verify selected integration."
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
