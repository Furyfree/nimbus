package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Furyfree/nimbus/internal/agentproxy"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/spf13/cobra"
)

var runAgentProxy = agentproxy.Run

const proxySetupHelp = `Connect GitHub Copilot to local Codex, Claude, Grok and Antigravity agents.
Prerequisites: open Copilot, apply Mise tools (Node 24+, Herdr and the agent CLIs),
and sign into each desired CLI separately. This task does not start sign-in.
After approval:
  - Apply only Chezmoi's agent-proxy configuration, excluding scripts.
  - Build pinned upstream 66cc75af597f with Nimbus's reviewed compatibility patch
    and verified source checksum; use its native user-service installer.
  - Register a loopback provider using Copilot's native API and credential store.
  - Discover models, add/update owned entries and remove owned stale entries.
Failed discoveries preserve that provider's existing models. Manual entries stay.
Private model ownership stays under XDG_STATE_HOME/nimbus. Setup opts into model
refresh after nimbus sync --upgrade; refresh never installs or signs in.
Existing trial ownership can be imported only when native identities match.
The proxy and Herdr user services may restart; finish active requests first.
Recovery: retry this task. The native installer owns release backups/rollback;
Nimbus retains partial model ownership for retry. No sudo is used.
`

const proxySetupPreview = `Set up local agents in Copilot:
  - Apply the managed proxy configuration.
  - Install/update the pinned proxy through its native user-service installer.
  - Register the local provider and refresh its managed model lists.
Keep Copilot open. Proxy services may restart; finish active requests first.
See --help for prerequisites, ownership and recovery details.
`

func agentProxyOperation(src native.Source, status agentproxy.Result, reset bool) (string, string, error) {
	if reset {
		return "Disable automatic model refresh. Services, credentials and models stay in place.\n", "disable-refresh", nil
	}
	if status.Status == "blocked" {
		return "", "", errors.New(status.Detail)
	}
	if status.Status == "configured" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", "", err
			}
			base = filepath.Join(home, ".config")
		}
		changes, err := src.Run("chezmoi", "--skip-secrets", "status", "--exclude=scripts", filepath.Join(base, "agent-proxy/config.yaml"))
		if err != nil {
			return "", "", errors.New("Cannot check managed proxy configuration; no changes made")
		}
		if len(bytes.TrimSpace(changes)) == 0 {
			return "Agent proxy is already configured.\nRefresh model lists in Copilot (add/update/remove managed entries).\n", "refresh", nil
		}

		return "Proxy configuration has changed.\n" + proxySetupPreview, "setup", nil
	}
	return proxySetupPreview, "setup", nil
}

func newAgentProxyPostinstall(opts *options, flags *machineFlags) *cobra.Command {
	var preview, yes, reset bool
	cmd := &cobra.Command{Use: "agent-proxy", Short: "Connect Copilot to local agents and refresh models", Long: proxySetupHelp, Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := loadSelected(*flags)
			if err != nil {
				return err
			}
			applicable := false
			for _, p := range s.Resolved.Packages {
				if p.Name == "github-copilot-installer" {
					applicable = true
					break
				}
			}
			if !applicable {
				return errors.New("agent-proxy requires the selected GitHub Copilot component")
			}
			src := newSource()
			status := agentproxy.Inspect(src, s.Resolved.Machine)
			plan, mode, err := agentProxyOperation(src, status, reset)
			if err != nil {
				return err
			}

			if opts.json && preview {
				return writeJSON(cmd.OutOrStdout(), struct {
					Status agentproxy.Result `json:"status"`
					Plan   string            `json:"plan"`
				}{status, plan}, nil)
			}
			out := cmd.OutOrStdout()
			if opts.json {
				out = cmd.ErrOrStderr()
			}
			if _, err := fmt.Fprint(out, plan); err != nil {
				return err
			}
			if preview {
				return nil
			}
			if opts.json && !yes {
				return usageError{errors.New("mutation with --json requires --yes")}
			}
			if !yes && (!postinstallTerminal(cmd.InOrStdin()) || !approver(cmd.InOrStdin(), out, "")) {
				return errors.New("agent-proxy setup not approved")
			}

			var result agentproxy.Result
			activity := "configure agent proxy"
			if mode == "refresh" {
				activity = "refresh model lists"
			}
			err = native.Activity(out, activity, func() error {
				var runErr error
				result, runErr = runAgentProxy(cmd.Context(), mode, s.Resolved.Machine, out)
				return runErr
			})
			if opts.json {
				return errors.Join(err, writeJSON(cmd.OutOrStdout(), result, nil))
			}
			if renderErr := renderAgentProxy(out, result); renderErr != nil {
				return errors.Join(err, renderErr)
			}
			if err != nil && result.Status == "failed" {
				return reported{}
			}
			return err
		}}
	cmd.Flags().BoolVarP(&preview, "plan", "p", false, "show setup without downloads, authentication or writes")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "approve the displayed user-service setup and model ownership")
	cmd.Flags().BoolVar(&reset, "reset", false, "disable automatic refresh while retaining services and model ownership")
	return cmd
}

func renderAgentProxy(out io.Writer, result agentproxy.Result) error {
	if result.Status == "" {
		return nil
	}
	for _, provider := range result.Providers {
		label := map[string]string{"codex": "Codex", "claude": "Claude", "grok": "Grok", "agy": "Antigravity"}[provider.Provider]
		if label == "" {
			label = provider.Provider
		}
		var err error
		switch provider.Status {
		case "verified":
			_, err = fmt.Fprintf(out, "✓ %s: %d models\n", label, provider.Count)
		case "skipped":
			_, err = fmt.Fprintf(out, "- %s: skipped - %s\n", label, provider.Reason)
		case "failed":
			_, err = fmt.Fprintf(out, "Warning: %s: %s\n", label, provider.Reason)
		}
		if err != nil {
			return err
		}
	}
	if len(result.Providers) > 0 {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	prefix := ""
	if result.Status == "failed" {
		prefix = "Error: "
	}
	if _, err := fmt.Fprintln(out, prefix+result.Detail); err != nil {
		return err
	}

	for _, line := range result.Changes {
		if _, err := fmt.Fprintln(out, "  "+line); err != nil {
			return err
		}
	}
	for _, line := range result.Notices {
		if _, err := fmt.Fprintln(out, "Notice: "+line); err != nil {
			return err
		}
	}
	return nil
}

// Only an explicit setup registration opts this machine into update refresh.
// The Topgrade system callback and ordinary sync never enter this path.
func refreshAgentProxy(cmd *cobra.Command, src native.Source, machine string, out io.Writer, result *syncResult) error {
	status := agentproxy.Inspect(src, machine)
	if status.Status == "not-configured" {
		return nil
	}
	if status.Status != "configured" {
		result.Steps = append(result.Steps, runStep{Name: "agent models", Status: "skipped", Detail: status.Detail})
		return nil
	}
	var refreshed agentproxy.Result
	err := native.Activity(out, "refresh local agent models", func() error {
		var runErr error
		refreshed, runErr = runAgentProxy(cmd.Context(), "refresh", machine, out)
		return runErr
	})
	if err != nil && refreshed.Status == "" {
		refreshed.Status, refreshed.Detail = "failed", err.Error()
	}
	result.Steps = append(result.Steps, runStep{Name: "agent models", Status: refreshed.Status, Detail: refreshed.Detail})
	for _, change := range refreshed.Changes {
		result.Steps = append(result.Steps, runStep{Name: "agent model", Status: "succeeded", Detail: change})
	}
	for _, provider := range refreshed.Providers {
		if provider.Status == "failed" || provider.Status == "skipped" {
			result.Steps = append(result.Steps, runStep{Name: provider.Provider, Status: provider.Status, Detail: provider.Reason})
		}
	}
	for _, notice := range refreshed.Notices {
		result.Steps = append(result.Steps, runStep{Name: "agent models", Status: "skipped", Detail: notice})
	}
	return err
}
