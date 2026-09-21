package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/output"
	"github.com/Furyfree/nimbus/internal/postinstall"
	"github.com/Furyfree/nimbus/internal/userstate"
	"github.com/spf13/cobra"
)

var taskDescriptions = []struct{ id, title, help string }{
	{"account-picture", "Register the managed account picture", "Requires AccountsService, a valid managed JPEG and the desktop authorization session."},
	{"copilot", "Install GitHub Copilot", "Requires the selected native installer helper. The helper owns installation, updates and removal."},
	{"dtu-network", "Set up DTU eduroam on Linux", "Requires the selected dtu-network component and installed packages with verified Nimbus receipts or recorded baseline identities. After approval, install the reviewed CAT CA bundle and configure a user-restricted eduroam profile through NetworkManager. Choose manual credentials or a 1Password item UUID with username and password fields; a bare username receives @dtu.dk. Existing eduroam/DTUsecure profiles require a separate default-No deletion/recreation confirmation, including with --yes; declining preserves profiles and certificates without reading credentials. Automatic connection is enabled after profile/password verification, even off campus. A separate prompt offers immediate connection; if eduroam is not found, setup stays configured for later. Status and --plan stay offline, never read credentials and never request sudo. Completion verifies configuration, not Wi-Fi or Internet access. --reset clears evidence only."},
	{"fingerprint", "Enroll a fingerprint", "Requires fprintd and a supported reader. Native enrollment verifies completion."},
	{"hostname", "Set the persistent hostname", "Requires the machine manifest hostname. Close browsers first: Chromium treats a lock naming another hostname as another computer. After approval, set the static hostname through hostnamectl and verify it. NetworkManager then keeps the name stable across Wi-Fi changes and reboots. Status and --plan stay read-only; no browser data or profile locks are touched."},
	{"hyprland-plugins", "Install selected Hyprland plugins", "Requires the active matching Hyprland session, development headers and Chezmoi plugin selection."},
	{"noctalia-lockscreen", "Restore the managed lockscreen layout", "Requires an applied Chezmoi layout and the unlocked Noctalia desktop session. After approval, gracefully stop only Noctalia, save a private settings backup, remove only lockscreen_widgets overrides, restart the shell and verify configuration. Close the lockscreen editor first. The bar briefly disappears; applications and Hyprland stay running. No sudo, automatic locking or repair during sync. --reset clears evidence only; it does not restore the backup."},
	{"noctalia-plugins", "Install missing enabled Noctalia plugins", "Requires the running Noctalia desktop session and initialized plugin sources."},
	{"nvidia-mok", "Sign NVIDIA modules and enroll their key", "Requires the selected NVIDIA packages and akmods certificate. After approval, inspect and preserve existing signing keys, rebuild mismatched modules, refresh the boot image and submit MOK enrollment. Complete native enrollment at reboot, then rerun to verify. Never enter a MOK password into Nimbus. This helper is not installation-tested yet: the manual native flow is verified, but an unexpected result should be treated as unvalidated and reported."},
	{"onepassword", "Configure 1Password and selected SSH/Git integration", "The guided task offers SSH/Git integration when it is not selected (default: no). An explicit yes saves the choice through Chezmoi while preserving machine and profiles; --yes cannot opt in. In 1Password, sign in, unlock, and enable desktop CLI integration. If SSH is selected, enable its agent. Skip manual SSH/Git file edits: Chezmoi creates managed parent directories and applies only the selected files after a concise change summary and approval. Use --diff during guided setup to show the full private diff without a pager. Existing file-conflict prompts remain enabled. --mark-done verifies the existing selection without changing it."},
	{"proton-cachyos", "Install Proton-CachyOS Latest for Steam", "Requires native Steam and ProtonPlus. Start Steam once, then close games. Native ProtonPlus owns runners and updates."},
	{"tailscale-operator", "Set up Tailscale sign-in and local operator", "Requires selected Tailscale and readable daemon preferences and status. If sign-in is needed, preview and approve sudo tailscale up --operator=USER, then complete the native browser sign-in. This connects the machine; --yes cannot complete authentication. Initial setup verifies both operator and a running connection. Already signed-in machines only need the operator setting; stopped connections stay stopped. Do not use tailscale login for initial operator setup: switching profiles can clear the setting. Device approval remains in the Tailscale admin console. Status and --plan never sign in or request sudo."},
	{"voxtype", "Set up local dictation", "Requires the selected Voxtype package and the native CLI. After approval, download the Whisper model selected by the managed Chezmoi config and enable the user unit. The model download needs network access and can exceed a gigabyte. Enabling voxtype.service starts dictation at each graphical login; no root access is involved. Nimbus never removes models or reads recordings. Status and --plan stay read-only."},
	{"wowup", "Install WoWUp CurseForge", "Requires the selected helper. After approval, the helper verifies and installs the official AppImage; installing or reinstalling the helper RPM queues the same systemd job. Nimbus does not manage addons or application data."},
}

func newPostinstall(opts *options) *cobra.Command {
	cmd := &cobra.Command{Use: "postinstall", Short: "Guided setup tasks and machine-specific status", Long: "Choose a task for guided setup or status for a compact checklist. Help and previews never launch apps, request authentication or write state.", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	cmd.SuggestionsMinimumDistance = 2
	cmd.Args = func(c *cobra.Command, args []string) error {
		if len(args) != 0 {
			return unknownPostinstallTask(c, args[0])
		}
		return nil
	}
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if c == cmd && len(c.Flags().Args()) > 0 {
			return unknownPostinstallTask(c, c.Flags().Args()[0])
		}
		return usageError{err}
	})
	var flags machineFlags
	addMachineFlags(&flags, cmd.PersistentFlags())
	for _, spec := range taskDescriptions {
		child := postinstallExecutor(opts, &flags, spec.id)
		child.Use, child.Short, child.Long = spec.id, spec.title, spec.help
		if child.Flags().Lookup("mark-done") != nil {
			child.Long += "\n\nUse --mark-done for existing setup: verify and record completion without installation or configuration changes."
		}
		if spec.id == "onepassword" {
			child.Long += "\nVerification may request 1Password authorization. If the CLI account is not signed in, the task offers op signin and retries verification. Account and key output remains private.\nStatus checks confirmed GUI prerequisites and local configuration. It does not inspect current vault unlock, remote SSH access or signing-key registration."
		}
		if spec.id == "nvidia-mok" {
			child.Long += "\nIf the certificate is permission-protected, this task previews and offers a read-only administrator check. Status and --plan never request sudo.\nPreviously verified means enrollment was verified earlier but cannot currently be rechecked without sudo. Enrollment alone does not prove the NVIDIA driver loads."
		}
		if spec.id == "onepassword" {
			child.Aliases = []string{"1password"}
		}
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
		return renderPostinstallStatus(c.OutOrStdout(), snapshot.view, opts.verbose)
	}}
	cmd.AddCommand(status, newAgentProxyPostinstall(opts, &flags), newZeronPostinstall(opts, &flags))
	return cmd
}
func renderPostinstallStatus(out io.Writer, view postinstallView, verbose ...bool) error {
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
		status := postinstallStatusLabel(t)
		detail := postinstallStatusDetail(t)
		if len(verbose) > 0 && verbose[0] {
			detail = t.Detail
		}
		if err := output.StatusRow(out, t.ID, status, detail); err != nil {
			return err
		}
		if t.PreviouslyVerified && t.Status == postinstall.Unknown && t.VerificationNeedsRoot {
			if _, err := fmt.Fprintf(out, "    Recheck: nimbus postinstall %s (requests sudo)\n", t.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func postinstallStatusLabel(task postinstall.Task) string {
	if task.PreviouslyVerified && task.Status == postinstall.Unknown && task.VerificationNeedsRoot {
		return "Previously verified"
	}
	return map[postinstall.Status]string{postinstall.Complete: "Verified", postinstall.Pending: "Pending", postinstall.Unknown: "Unable to check", postinstall.Blocked: "Blocked", postinstall.NotApplicable: "Not applicable"}[task.Status]
}

// Compact successful checks for the checklist only. Native details remain in
// JSON and task previews; never replace a pending or failed check's explanation.
func postinstallStatusDetail(task postinstall.Task) string {
	if task.PreviouslyVerified && task.Status == postinstall.Unknown && task.VerificationNeedsRoot {
		return "Recheck requires sudo."
	}
	if task.Status != postinstall.Complete {
		return task.Detail
	}
	switch task.ID {
	case "account-picture":
		return "Matches managed picture."
	case "copilot":
		return "Application installed."
	case "hyprland-plugins":
		return "ScrollOverview built and loaded."
	case "hostname":
		return "Static hostname matches the manifest."
	case "noctalia-plugins":
		return "Enabled plugin files present."
	case "nvidia-mok":
		return "Certificate enrolled or trusted."
	case "onepassword":
		return "GUI and local integration checked."
	case "proton-cachyos":
		return "Latest runner installed."
	case "tailscale-operator":
		return task.Detail
	case "voxtype":
		return "Model downloaded and user unit enabled."
	default:
		return task.Detail
	}
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
	keys := []string{id + ".manual", id + ".complete"}
	return store.Update("postinstall", machine, nil, keys)
}
func unknownPostinstallTask(cmd *cobra.Command, name string) error {
	message := fmt.Sprintf("unknown setup task %q", name)
	// Cobra computes edit distance against canonical names, not aliases.
	suggestedName := name
	if strings.HasPrefix(name, "1") {
		suggestedName = "one" + strings.TrimPrefix(name, "1")
	}
	if suggestions := cmd.SuggestionsFor(suggestedName); len(suggestions) > 0 {
		message += "; did you mean " + strings.Join(suggestions, " or ") + "?"
	}
	return usageError{errors.New(message)}
}

// markExistingTask never invokes a task's installation or repair action.
func markExistingTask(cmd *cobra.Command, src native.Source, before *postinstallSnapshot, t postinstall.Task, yes bool) error {
	switch t.ID {
	case "onepassword":
		return runOnePassword(cmd, src, before, t, yes, true, false)
	case "nvidia-mok":
		return runMOKVerification(cmd, src, before, t, yes)
	}
	if t.Status != postinstall.Complete {
		return fmt.Errorf("existing setup was not verified: %s; run nimbus postinstall %s for guided setup", t.Detail, t.ID)
	}
	if err := recordTask(before.view.Machine, t.ID+".complete", "verified"); err != nil {
		return err
	}
	return renderPostinstallStatus(cmd.OutOrStdout(), postinstallView{Machine: before.view.Machine, Tasks: []postinstall.Task{t}})
}
