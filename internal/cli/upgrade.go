package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/Furyfree/nimbus/internal/output"
	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/plan"
)

const upgradeActive = "NIMBUS_UPGRADE_ACTIVE"

// nativeExit preserves the delegated updater's status at the command boundary.
type nativeExit struct{ code int }

func (e nativeExit) Error() string {
	return fmt.Sprintf("update command exited with status %d", e.code)
}

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
					if os.Getenv("NIMBUS_UPGRADE_VERBOSE") == "true" {
						opts.verbose = true
					}
					if os.Getenv(maintenanceReport) != "" && os.Getenv("NIMBUS_UPGRADE_YES") == "true" {
						sf.yes = true
					}
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
				if path := os.Getenv(maintenanceReport); path != "" && os.Getenv(upgradeActive) != "" && !sf.plan {
					var result syncResult
					sf.result = &result
					err := runSync(cmd, opts, flags, sf)
					return errors.Join(err, writeUpgradeReport(path, &result))
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

func runTopgrade(cmd *cobra.Command, args []string, preview bool, flags machineFlags, yes ...bool) (retErr error) {
	if os.Getenv(upgradeActive) != "" {
		return errors.New("recursive upgrade refused: configure Topgrade's Nimbus system step as nimbus upgrade --system")
	}
	if preview {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Run Topgrade with its user configuration:\n  $ %s\nNative steps determine their updates when executed.\n", strings.Join(append([]string{"topgrade"}, args...), " "))
		return err
	}
	if os.Getenv(maintenanceReport) == "" {
		identity := flags
		if selected, err := maintenanceSelection(flags); err == nil {
			identity = selected
		}
		record, err := beginRunRecord("upgrade", identity.machine)
		if err != nil {
			return fmt.Errorf("start run record: %w", err)
		}
		defer func() { retErr = finishRunRecord(cmd.ErrOrStderr(), record, &syncResult{}, "Topgrade", retErr) }()
	}
	child := exec.CommandContext(cmd.Context(), "topgrade", args...)
	verbose, _ := cmd.Flags().GetBool("verbose")
	child.Env = append(os.Environ(), fmt.Sprintf("NIMBUS_UPGRADE_VERBOSE=%t", verbose), upgradeActive+"=1", "NIMBUS_UPGRADE_CHECKOUT="+flags.checkout, "NIMBUS_UPGRADE_MACHINE="+flags.machine, fmt.Sprintf("NIMBUS_UPGRADE_YES=%t", len(yes) > 0 && yes[0]))
	child.Stdin, child.Stdout, child.Stderr = cmd.InOrStdin(), output.Native(cmd.OutOrStdout()), output.Native(cmd.ErrOrStderr())
	return runChild(child)
}

func runChild(child *exec.Cmd) error {
	if err := child.Run(); err != nil {
		if exited, ok := errors.AsType[*exec.ExitError](err); ok {
			code := exited.ExitCode()
			if status, ok := exited.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				code = 128 + int(status.Signal())
			}
			return nativeExit{code: code}
		}
		return fmt.Errorf("start %s (install it and check PATH): %w", child.Path, err)
	}
	return nil
}

// Upgrades require ready sources and constraints. Other system drift belongs
// to sync and neither blocks nor becomes an implicit upgrade operation.
func systemUpgradePlan(p *plan.Plan) *plan.Plan {
	copy := *p
	copy.Operations = nil
	copy.Complete = true
	if p.Updates.Unavailable != "" {
		copy.Complete = false
		copy.Operations = append(copy.Operations, plan.Operation{ID: "upgrade:dnf", Kind: plan.KindPackage, Action: plan.ActionRepair, Summary: "check source-constrained DNF updates", Blocked: p.Updates.Unavailable})
	}
	if p.Snapshots != nil && (p.Snapshots.Setup || len(p.Snapshots.Changes()) > 0) {
		snapshots := *p.Snapshots
		snapshots.Blocked = "run nimbus sync first to configure root snapshots and retention"
		copy.Snapshots = &snapshots
		copy.Complete = false
	}
	for _, op := range p.Operations {
		if op.Kind == plan.KindPackage && op.Action == plan.ActionRepair {
			op.Blocked = "run nimbus sync first: " + op.Summary
			copy.Operations = append(copy.Operations, op)
			copy.Complete = false
			continue
		}
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
