package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/facts"
)

func newDotfiles(opts *options) *cobra.Command {
	cmd := &cobra.Command{Use: "dotfiles", Short: "Inspect, apply, or update user configuration and tools through Chezmoi", Args: noArgs}
	for _, action := range []string{"diff", "apply", "update"} {
		short := map[string]string{"diff": "Show the local Chezmoi changes", "apply": "Apply the local Chezmoi source, including user-tool scripts", "update": "Pull and apply the Chezmoi source, including user-tool scripts"}[action]
		cmd.AddCommand(&cobra.Command{Use: action, Short: short, Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			src := newSource()
			if _, err := src.LookPath("chezmoi"); err != nil {
				return fmt.Errorf("chezmoi is unavailable: %w", err)
			}
			if action != "diff" {
				s, err := loadSelected(machineFlags{})
				if err != nil {
					return err
				}
				if err := facts.CheckPlatform(src, s.Checkout.Definitions().Compatibility.Fedora); err != nil {
					return err
				}
				path, err := apply.LockPath()
				if err != nil {
					return err
				}
				lock, err := apply.Acquire(path, apply.LockInfo{Command: "dotfiles " + action, PID: os.Getpid(), Started: time.Now().UTC()})
				if err != nil {
					return err
				}
				defer func() { _ = lock.Release() }()
			}
			out := cmd.OutOrStdout()
			if opts.json {
				out = cmd.ErrOrStderr()
			}
			if action != "diff" {
				if _, err := fmt.Fprintln(out, "Chezmoi will apply configuration and run its declared user-tool installation scripts."); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(out, "$ chezmoi %s\n", action); err != nil {
				return err
			}
			err := src.Stream(out, cmd.ErrOrStderr(), "chezmoi", action)
			step := runStep{Name: "chezmoi " + action, Status: "succeeded"}
			if err != nil {
				step.Status, step.Detail = "failed", err.Error()
			}
			if opts.json {
				if reportErr := writeJSON(cmd.OutOrStdout(), []runStep{step}, nil); reportErr != nil {
					return errors.Join(err, reportErr)
				}
			} else if action != "diff" || err != nil {
				if reportErr := renderRunSummary(out, "dotfiles", []runStep{step}); reportErr != nil {
					return errors.Join(err, reportErr)
				}
			}
			if err != nil {
				return reported{}
			}
			return nil
		}})
	}
	return cmd
}
