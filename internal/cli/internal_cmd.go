package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/state"
)

// stateRoot is the applied-state directory. Tests point it at a temporary
// directory; nothing in the suites reads or writes /var/lib/nimbus.
var stateRoot = state.Root

// newInternal is the hidden group for the narrow privileged actions that
// sync runs through sudo. Each accepts only staged data bound to the plan
// digest and does one atomic thing; there is no general
// privileged executor.
func newInternal() *cobra.Command {
	group := &cobra.Command{Use: "internal", Hidden: true, Short: "Narrow privileged actions used by apply", Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
	var planDigest, stagePath string
	record := &cobra.Command{
		Use:    "record",
		Hidden: true,
		Short:  "Atomically record staged receipts bound to a plan digest",
		Args:   noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if os.Geteuid() != 0 {
				return fmt.Errorf("internal record must run as root through sudo")
			}
			if planDigest == "" || stagePath == "" {
				return usageError{fmt.Errorf("--plan and --stage are required")}
			}
			data, err := os.ReadFile(stagePath)
			if err != nil {
				return err
			}
			var st state.Stage
			if err := json.Unmarshal(data, &st); err != nil {
				return fmt.Errorf("stage %s: %w", stagePath, err)
			}
			return state.Record(stateRoot, planDigest, &st)
		},
	}
	record.Flags().StringVar(&planDigest, "plan", "", "plan digest the stage must be bound to")
	record.Flags().StringVar(&stagePath, "stage", "", "staged receipts file written by the normal user")
	group.AddCommand(record)
	group.AddCommand(newInternalSystemFile())
	return group
}

func newInternalSystemFile() *cobra.Command {
	var digest, payloadPath string
	cmd := &cobra.Command{Use: "system-file", Hidden: true, Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if os.Geteuid() != 0 {
			return fmt.Errorf("internal system-file must run as root through sudo")
		}
		if digest == "" || payloadPath == "" {
			return usageError{fmt.Errorf("--plan and --payload are required")}
		}
		data, err := os.ReadFile(payloadPath)
		if err != nil {
			return err
		}
		var payload apply.FilePayload
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&payload); err != nil {
			return fmt.Errorf("file payload: %w", err)
		}
		if !errors.Is(dec.Decode(new(any)), io.EOF) {
			return fmt.Errorf("file payload contains trailing data")
		}
		if payload.PlanDigest != digest {
			return fmt.Errorf("file payload does not match the approved plan")
		}
		return apply.ApplySystemFile("/", payload.Change)
	}}
	cmd.Flags().StringVar(&digest, "plan", "", "approved plan digest")
	cmd.Flags().StringVar(&payloadPath, "payload", "", "staged file change bound to the plan")
	return cmd
}
