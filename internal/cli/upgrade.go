package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/plan"
)

const upgradeActive = "NIMBUS_UPGRADE_ACTIVE"

// nativeExit preserves the delegated updater's status at the command boundary.
type nativeExit struct{ code int }

func (e nativeExit) Error() string { return fmt.Sprintf("Topgrade exited with status %d", e.code) }

func newUpgrade(opts *options) *cobra.Command {
	var flags machineFlags
	var sf syncFlags
	cmd := &cobra.Command{
		Use:   "upgrade [-- TOPGRADE_ARGS...]",
		Short: "Run the configured Topgrade update workflow",
		Long:  "Run Topgrade with its normal configuration and terminal prompts. Pass extra arguments after --. Topgrade's system step uses nimbus upgrade --system; it must not call sync or upgrade recursively.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if sf.systemUpgrade {
				if os.Getenv(upgradeActive) != "" {
					if flags.checkout == "" {
						flags.checkout = os.Getenv("NIMBUS_UPGRADE_CHECKOUT")
					}
					if flags.machine == "" {
						flags.machine = os.Getenv("NIMBUS_UPGRADE_MACHINE")
					}
				}
				if len(args) != 0 {
					return usageError{errors.New("--system does not accept Topgrade arguments")}
				}
				return runSync(cmd, opts, flags, sf)
			}
			if sf.yes {
				return usageError{errors.New("--yes requires --system; pass Topgrade arguments after --")}
			}
			if opts.json {
				return usageError{errors.New("upgrade delegates interactive output to Topgrade and does not support --json")}
			}
			return runTopgrade(cmd, args, sf.plan, flags)
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	cmd.Flags().BoolVar(&sf.systemUpgrade, "system", false, "update system packages and Flatpaks only (Topgrade callback)")
	cmd.Flags().BoolVarP(&sf.plan, "plan", "p", false, "preview without running update commands")
	cmd.Flags().BoolVarP(&sf.yes, "yes", "y", false, "approve the system update preview")
	return cmd
}

func runTopgrade(cmd *cobra.Command, args []string, preview bool, flags machineFlags) error {
	if os.Getenv(upgradeActive) != "" {
		return errors.New("recursive upgrade refused: configure Topgrade's Nimbus system step as nimbus upgrade --system")
	}
	if preview {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Run Topgrade with its user configuration: %q\nTopgrade selects its native update steps and prompts. Their transactions are determined when they run.\nIts Nimbus system callback takes root snapshots and cleans up when Snapper is selected.\n", append([]string{"topgrade"}, args...))
		return err
	}
	child := exec.CommandContext(cmd.Context(), "topgrade", args...)
	child.Env = append(os.Environ(), upgradeActive+"=1", "NIMBUS_UPGRADE_CHECKOUT="+flags.checkout, "NIMBUS_UPGRADE_MACHINE="+flags.machine)
	child.Stdin, child.Stdout, child.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := child.Run(); err != nil {
		if exited, ok := errors.AsType[*exec.ExitError](err); ok {
			code := exited.ExitCode()
			if status, ok := exited.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				code = 128 + int(status.Signal())
			}
			return nativeExit{code: code}
		}
		return fmt.Errorf("start Topgrade (install it and check PATH): %w", err)
	}
	return nil
}

// Upgrades require ready sources and constraints. Other system drift belongs
// to sync and neither blocks nor becomes an implicit upgrade operation.
func systemUpgradePlan(p *plan.Plan) *plan.Plan {
	copy := *p
	copy.Operations = nil
	copy.Complete = true
	if p.Snapshots != nil && (p.Snapshots.Setup || len(p.Snapshots.Changes()) > 0) {
		snapshots := *p.Snapshots
		snapshots.Blocked = "run nimbus sync first to configure root snapshots and retention"
		copy.Snapshots = &snapshots
		copy.Complete = false
	}
	for _, op := range p.Operations {
		if op.Kind != plan.KindRepository && op.Kind != plan.KindFlatpakRemote && op.Kind != plan.KindDNFConfig && !plan.IsConstraintOperation(op) {
			continue
		}
		if op.Action != plan.ActionKeep || op.Blocked != "" || op.After != "" {
			op.Blocked = "run nimbus sync first: " + op.Summary
			copy.Complete = false
		}
		copy.Operations = append(copy.Operations, op)
	}
	return &copy
}
