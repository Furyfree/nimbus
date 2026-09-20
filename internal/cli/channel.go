package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/selector"
	"github.com/Furyfree/nimbus/internal/version"
)

const engineRepoFile = "/etc/yum.repos.d/nimbus-engine.repo"

// newChannel reports the selector channel and whether the checkout branch and
// engine repository still match it. It is read-only.
func newChannel() *cobra.Command {
	return &cobra.Command{
		Use:   "channel",
		Short: "Show the engine channel and any checkout or repository drift",
		Long: `Show the channel recorded in the selector and whether the checkout branch
and the nimbus-engine repository still match it. Read-only; switching is done
by the checkout bootstrap.`,
		Args: cobra.NoArgs,
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
				_, err = fmt.Fprintln(out, "The checkout bootstrap aligns the branch, repository and engine for a channel.")
			}
			return err
		},
	}
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
