package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"syscall"

	"github.com/spf13/cobra"
)

const maintenanceReport = "NIMBUS_MAINTENANCE_REPORT"

func writeUpgradeReport(path string, result *syncResult) error {
	if err := privateLogPath(path, false); err != nil {
		return fmt.Errorf("unsafe maintenance report: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(result)
}
func runMaintenanceUpgrade(cmd *cobra.Command, flags machineFlags, result *syncResult, yes ...bool) error {
	report, err := os.CreateTemp("", "nimbus-upgrade-*.json")
	if err != nil {
		return err
	}
	path := report.Name()
	defer os.Remove(path)
	if err := report.Close(); err != nil {
		return err
	}
	old, exists := os.LookupEnv(maintenanceReport)
	if err := os.Setenv(maintenanceReport, path); err != nil {
		return err
	}
	defer func() {
		if exists {
			_ = os.Setenv(maintenanceReport, old)
		} else {
			_ = os.Unsetenv(maintenanceReport)
		}
	}()
	var args []string
	approved := len(yes) > 0 && yes[0]
	if approved {
		args = []string{"--yes"}
	}
	runErr := runTopgrade(cmd, args, false, flags, approved)
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.Join(runErr, err)
	}
	if len(data) > 0 {
		var system syncResult
		if err := json.Unmarshal(data, &system); err != nil {
			return errors.Join(runErr, fmt.Errorf("invalid system update report: %w", err))
		}
		result.Steps = append(result.Steps, system.Steps...)
		for _, notice := range system.Notices {
			if !slices.Contains(result.Notices, notice) {
				result.Notices = append(result.Notices, notice)
			}
		}
		result.Differences = append(result.Differences, system.Differences...)
		result.Failures = append(result.Failures, system.Failures...)
		result.Executed = append(result.Executed, system.Executed...)
		result.Reboot = result.Reboot || system.Reboot
		result.Logout = result.Logout || system.Logout
		if system.Error != "" {
			runErr = errors.Join(runErr, fmt.Errorf("system update: %s", system.Error))
		}
	} else {
		result.Notices = append(result.Notices, "Topgrade supplied no Nimbus system-update report; inspect its configured pre-command.")
	}
	return runErr
}
