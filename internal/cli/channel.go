package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/output"
	"github.com/Furyfree/nimbus/internal/selector"
	"github.com/Furyfree/nimbus/internal/version"
)

const engineRepoFile = "/etc/yum.repos.d/nimbus-engine.repo"

func newChannel() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Show the engine channel and any checkout or repository drift",
		Long: `Show the channel recorded in the selector and whether the checkout branch
and the nimbus-engine repository still match it. With no subcommand this is
read-only. Use channel switch stable|develop to change channels.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := selector.DefaultPath()
			if err != nil {
				return err
			}
			sel, err := selector.Load(path)
			if err != nil {
				return err
			}
			branch, branchErr := selector.CheckoutBranch(sel.Checkout)
			repo, repoErr := engineRepoChannel()
			lines, drift := channelReport(sel, branch, branchErr, repo, repoErr, version.Engine)
			out := cmd.OutOrStdout()
			for _, line := range lines {
				if _, err := fmt.Fprintln(out, line); err != nil {
					return err
				}
			}
			for _, line := range drift {
				if _, err := fmt.Fprintln(out, "drift:    "+line); err != nil {
					return err
				}
			}
			if len(drift) > 0 {
				_, err = fmt.Fprintln(out, "Run nimbus channel switch stable|develop to align the checkout and engine.")
			}
			return err
		},
	}
	cmd.AddCommand(newChannelSwitch())
	return cmd
}

func newChannelSwitch() *cobra.Command {
	return &cobra.Command{
		Use:       "switch stable|develop",
		Short:     "Switch the checkout and engine to another channel",
		Long:      "Use the saved checkout to switch its branch and signed engine package after approval. Local changes and incompatible state stop the installer. Run nimbus sync afterwards to reconcile the workstation.",
		ValidArgs: []string{selector.ChannelStable, selector.ChannelDevelop},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 || args[0] != selector.ChannelStable && args[0] != selector.ChannelDevelop {
				return usageError{errors.New("use nimbus channel switch stable|develop")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				return usageError{errors.New("channel switching is interactive and does not support --json")}
			}
			path, err := selector.DefaultPath()
			if err != nil {
				return err
			}
			sel, err := selector.Load(path)
			if err != nil {
				return err
			}
			root, err := canonical(sel.Checkout)
			if err != nil {
				return err
			}
			if err := selector.Verify(sel, root); err != nil {
				return err
			}
			if !strings.EqualFold(sel.Origin, "github.com/Furyfree/nimbus") {
				return errors.New("channel switching requires the Nimbus origin")
			}
			branch, err := selector.CheckoutBranch(root)
			if err != nil {
				return err
			}
			src := newSource()
			data, err := src.ReadFile(engineRepoFile)
			if err != nil {
				return fmt.Errorf("inspect engine repository: %w", err)
			}
			repo, err := repoChannel(string(data))
			if err != nil || repo == "unrecognized" {
				return errors.New("engine repository is not a recognized channel; repair it before switching")
			}
			target := args[0]
			_, drift := channelReport(sel, branch, nil, repo, nil, "")
			if sel.Channel == target && len(drift) == 0 {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "Already on %s; nothing changed.\n", target)
				return err
			}
			script := filepath.Join(root, "install.sh")
			if err := channelSwitchReady(src, sel, root, script); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Switch Nimbus from %s to %s using %s.\nThis updates the checkout branch and signed engine package. Sudo may ask for authentication.\n", sel.Channel, target, root); err != nil {
				return err
			}
			if !approver(cmd.InOrStdin(), cmd.OutOrStdout(), "") {
				return errors.New("channel switch cancelled; nothing changed")
			}
			lockPath, err := apply.LockPath()
			if err != nil {
				return err
			}
			lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: "channel switch " + target, PID: os.Getpid(), Started: time.Now().UTC()})
			if err != nil {
				return err
			}
			defer func() { _ = lock.Release() }()
			fresh, err := selector.Load(path)
			if err != nil || *fresh != *sel {
				return errors.New("selection changed during approval; retry channel switching")
			}
			if err := channelSwitchReady(src, sel, root, script); err != nil {
				return err
			}
			child := exec.CommandContext(cmd.Context(), "bash", script, "--channel", target)
			child.Dir = root
			child.Env = append(os.Environ(), "NIMBUS_CHECKOUT="+root, "NIMBUS_CHANNEL_SWITCH=1")
			child.Stdin, child.Stdout, child.Stderr = cmd.InOrStdin(), output.Native(cmd.OutOrStdout()), output.Native(cmd.ErrOrStderr())
			return runChild(child)
		},
	}
}

// Recheck the executable checkout under the lock after approval.
func channelSwitchReady(src native.Source, sel *selector.Selector, root, script string) error {
	if err := selector.Verify(sel, root); err != nil {
		return err
	}
	info, err := os.Lstat(script)
	if err != nil {
		return fmt.Errorf("read channel installer: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("channel installer must be a regular file, not a symlink")
	}
	status, err := src.Run("git", inspect.GitArgs(root, "status", "--porcelain")...)
	if err != nil {
		return err
	}
	if len(status) != 0 {
		return errors.New("checkout has local changes; commit or stash them before switching channels")
	}
	return nil
}

// migrateSelector records a selector that predates channels as schema 2 on
// the stable track, keeping checkout, machine and origin. It is a mutation of
// Nimbus's own file: sync announces it in the preview, prints it when it
// happens and lists it in the closing report. Plan mode never writes. A
// selector that is absent, already current or supplied through flags needs
// nothing; an unreadable one is an error before any system change.
func migrateSelector(plan bool) (detail string, err error) {
	path, err := selector.DefaultPath()
	if err != nil {
		return "", err
	}
	sel, err := selector.Load(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", nil
	case err != nil:
		return "", err
	case sel.Schema >= selector.CurrentSchema:
		return "", nil
	}
	detail = fmt.Sprintf("schema %d, channel %s", selector.CurrentSchema, selector.ChannelStable)
	if plan {
		return detail, nil
	}
	if err := selector.SetChannel(path, selector.ChannelStable); err != nil {
		return "", fmt.Errorf("record the selector as schema %d: %w", selector.CurrentSchema, err)
	}
	return detail, nil
}

// engineRepoChannel names the channel the installed engine repository points
// at, or an error when it is missing or unrecognized.
func engineRepoChannel() (string, error) {
	data, err := os.ReadFile(engineRepoFile)
	if err != nil {
		return "", err
	}
	return repoChannel(string(data))
}

// repoChannel maps a nimbus-engine repository file to its channel.
func repoChannel(content string) (string, error) {
	baseurl := ""
	for line := range strings.SplitSeq(content, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "baseurl="); ok {
			baseurl = strings.TrimSpace(rest)
			break
		}
	}
	switch {
	case baseurl == "":
		return "", fmt.Errorf("%s declares no baseurl", engineRepoFile)
	case strings.Contains(baseurl, "/results/furyfree/nimbus-develop/"):
		return selector.ChannelDevelop, nil
	case strings.Contains(baseurl, "/results/furyfree/nimbus/"):
		return selector.ChannelStable, nil
	default:
		return "unrecognized", nil
	}
}

// channelReport renders the read-only channel status. Drift lists the
// branch, repository or inspection problems that prevent a clean match.
func channelReport(sel *selector.Selector, branch string, branchErr error, repo string, repoErr error, engine string) (lines, drift []string) {
	channel := sel.Channel
	if channel == "" {
		channel = selector.ChannelStable
	}
	label := channel
	if sel.Schema < selector.CurrentSchema {
		label = fmt.Sprintf("%s (schema %d; recorded as schema %d by the next sync)", channel, sel.Schema, selector.CurrentSchema)
	}
	lines = append(lines, "channel:  "+label)
	if branchErr != nil {
		lines = append(lines, "checkout: "+sel.Checkout+" (branch unavailable)")
	} else {
		lines = append(lines, "checkout: "+sel.Checkout+" (branch "+branch+")")
	}
	lines = append(lines, "machine:  "+sel.Machine)
	lines = append(lines, "origin:   "+sel.Origin)
	lines = append(lines, "engine:   "+engine)
	if repoErr != nil {
		lines = append(lines, "repo:     unavailable ("+repoErr.Error()+")")
	} else {
		lines = append(lines, "repo:     "+repo)
	}

	wantBranch := "main"
	if channel == selector.ChannelDevelop {
		wantBranch = "develop"
	}
	switch {
	case branchErr != nil:
		drift = append(drift, "the checkout branch could not be inspected: "+branchErr.Error())
	case branch != wantBranch:
		drift = append(drift, fmt.Sprintf("checkout branch %s does not match channel %s (branch %s)", branch, channel, wantBranch))
	}
	switch {
	case repoErr != nil:
		drift = append(drift, "the engine repository could not be inspected: "+repoErr.Error())
	case repo != channel:
		drift = append(drift, fmt.Sprintf("engine repository points at %s while the channel is %s", repo, channel))
	}
	return lines, drift
}
