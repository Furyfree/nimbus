package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/selector"
	"github.com/Furyfree/nimbus/internal/userstate"
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

// channelNotice returns the one-time notice shown when a channel-aware engine
// first runs against a selector that predates channels. It records the display
// in local evidence, so it is shown once per machine. Read-only commands do not
// call it.
func channelNotice() string {
	path, err := selector.DefaultPath()
	if err != nil {
		return ""
	}
	sel, err := selector.Load(path)
	if err != nil || sel.Schema >= selector.CurrentSchema {
		return ""
	}
	const id = "channel-awareness"
	store, err := userstate.Default()
	if err != nil {
		return ""
	}
	if seen, err := store.Read("channel"); err == nil && seen.Has(sel.Machine, id, 1, "displayed") {
		return ""
	}
	// A record failure only makes the notice repeat; it never hides it.
	_ = store.Update("channel", sel.Machine, map[string]userstate.Evidence{
		id: {Revision: 1, Source: "displayed"},
	}, nil)
	return fmt.Sprintf("Channel support: this engine follows the stable or develop track, and the existing selection counts as stable; run %s/install.sh --channel develop to follow develop, or nimbus channel to inspect the track.", sel.Checkout)
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
		label = fmt.Sprintf("%s (schema %d; recorded as schema %d by the next init or switch)", channel, sel.Schema, selector.CurrentSchema)
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
