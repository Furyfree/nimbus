package cli

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/Furyfree/nimbus/internal/userstate"
	"github.com/spf13/cobra"
)

var taskDescriptions = []struct{ id, title, help string }{
	{"account-picture", "Register the managed account picture", "Requires AccountsService, a valid managed JPEG and the desktop authorization session."},
	{"copilot", "Install GitHub Copilot", "Requires the selected native installer helper. The helper owns installation, updates and removal."},
	{"fingerprint", "Enroll a fingerprint", "Requires fprintd and a supported reader. Native enrollment verifies completion."},
	{"hyprland-plugins", "Install selected Hyprland plugins", "Requires the active matching Hyprland session, development headers and Chezmoi plugin selection."},
	{"noctalia-plugins", "Install missing enabled Noctalia plugins", "Requires the running Noctalia desktop session and initialized plugin sources."},
	{"nvidia-mok", "Verify NVIDIA signing-key enrollment", "Requires the selected NVIDIA packages and akmods certificate. Complete native enrollment at reboot, then rerun to verify. Never enter a MOK password into Nimbus."},
	{"onepassword", "Configure 1Password and selected SSH/Git integration", "In 1Password, sign in, unlock, and enable desktop CLI integration. If SSH is selected, enable its agent. Skip manual SSH/Git file edits: Chezmoi applies the selected files after review. SSH remains explicitly opt-in through Chezmoi or init --onepassword-ssh."},
	{"proton-cachyos", "Install Proton-CachyOS Latest for Steam", "Requires native Steam and ProtonPlus. Start Steam once, then close games. Native ProtonPlus owns runners and updates."},
	{"tailscale-operator", "Set the local Tailscale operator", "Requires selected Tailscale and readable daemon preferences; sign-in remains separate."},
	{"wowup", "Inspect WoWUp setup requirements", "Blocked until the selected helper supplies standalone installation."},
}

func newPostinstall(opts *options) *cobra.Command {
	cmd := &cobra.Command{Use: "postinstall", Short: "Guided setup tasks and machine-specific status", Long: "Choose a task for guided setup or status for a compact checklist. Help and previews never launch apps, request authentication or write state.", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	var flags machineFlags
	addMachineFlags(&flags, cmd.PersistentFlags())
	for _, spec := range taskDescriptions {
		child := postinstallExecutor(opts, &flags)
		child.Use, child.Short, child.Long = spec.id, spec.title, spec.help
		execute := child.RunE
		child.Args = noArgs

		child.RunE = func(c *cobra.Command, _ []string) error { return execute(c, []string{spec.id}) }
		cmd.AddCommand(child)
	}
	status := &cobra.Command{Use: "status", Short: "Show applicable setup tasks and session notices", Args: noArgs, RunE: func(c *cobra.Command, _ []string) error {
		snapshot, err := inspectPostinstall(newSource(), flags)
		if err != nil {
			return err
		}
		if opts.json {
			return writeJSON(c.OutOrStdout(), snapshot.view, nil)
		}
		return renderPostinstallStatus(c.OutOrStdout(), snapshot.view)
	}}
	cmd.AddCommand(status)
	return cmd
}
func renderPostinstallStatus(out io.Writer, view postinstallView) error {
	if _, err := fmt.Fprintf(out, "Setup for %s:\n", view.Machine); err != nil {
		return err
	}
	for _, t := range view.Tasks {
		if t.ID == "reboot" || t.ID == "logout" {
			if t.Status != postinstall.Complete {
				if _, err := fmt.Fprintf(out, "Notice: %s: %s\n", t.ID, t.Detail); err != nil {
					return err
				}
			}
			continue
		}
		if t.Status == postinstall.NotApplicable {
			continue
		}
		status := map[postinstall.Status]string{postinstall.Complete: "Verified", postinstall.Pending: "Pending", postinstall.Unknown: "Unable to check", postinstall.Blocked: "Blocked"}[t.Status]
		if _, err := fmt.Fprintf(out, "  %-20s %-16s %s\n", t.ID, status, t.Detail); err != nil {
			return err
		}
	}
	return nil
}
func confirmManual(cmd *cobra.Command, message string) error {
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), message); err != nil {
		return err
	}
	if !postinstallTerminal(cmd.InOrStdin()) {
		return errors.New("manual confirmation requires a terminal; --yes cannot confirm GUI work")
	}
	if !approver(cmd.InOrStdin(), cmd.OutOrStdout(), "") {
		return errors.New("manual prerequisites not confirmed; task remains pending")
	}
	return nil
}
func recordTask(machine, id, source string) error {
	store, err := userstate.Default()
	if err != nil {
		return err
	}
	return store.Update("postinstall", machine, map[string]userstate.Evidence{id: {Revision: 1, Source: source}}, nil)
}
func taskConfirmed(machine, id string) bool {
	store, err := userstate.Default()
	if err != nil {
		return false
	}
	f, err := store.Read("postinstall")
	return err == nil && f.Has(machine, id+".manual", 1, "confirmed")
}
func resetTask(machine, id string) error {
	store, err := userstate.Default()
	if err != nil {
		return err
	}
	return store.Update("postinstall", machine, nil, []string{id + ".manual", id + ".complete"})
}
func acknowledgeTask(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, t postinstall.Task) error {
	if !slices.Contains([]string{"onepassword", "proton-cachyos", "nvidia-mok"}, t.ID) {
		return errors.New("this task is verified automatically and has no manual acknowledgment")
	}
	if t.Status == postinstall.Blocked {
		return errors.New(t.Detail)
	}
	if err := confirmManual(cmd, "Confirm only the manual prerequisites described in this task's help. Required setup and verification still run through the guided task."); err != nil {
		return err
	}
	if t.ID == "onepassword" {
		selection, err := passwordSelection(src)
		if err != nil {
			return err
		}
		if err := confirmPasswordState(before.view.Machine, selection.OnePasswordSSH); err != nil {
			return err
		}
	} else if err := recordTask(before.view.Machine, t.ID+".manual", "confirmed"); err != nil {
		return err
	}
	fresh, err := inspectPostinstall(src, machineFlags{checkout: before.selected.Root, machine: before.view.Machine})
	if err != nil {
		return err
	}
	current, err := selectedTask(fresh.view, t.ID)
	if err != nil {
		return err
	}
	return renderPostinstallStatus(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{current}})
}
