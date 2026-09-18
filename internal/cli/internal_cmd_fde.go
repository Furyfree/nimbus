package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/postinstall"
)

// newInternalFDEUKI is the narrow privileged image builder the kernel-install
// hook and approved setup call. It validates the marker gate itself; there is
// no arbitrary file or command input.
func newInternalFDEUKI() *cobra.Command {
	var onlyIfCurrent bool
	cmd := &cobra.Command{Use: "fde-uki", Hidden: true, Args: func(_ *cobra.Command, args []string) error {
		if err := cobra.RangeArgs(1, 3)(nil, args); err != nil {
			return err
		}
		if onlyIfCurrent && args[0] != "add" {
			return usageError{errors.New("--only-if-current applies only to fde-uki add")}
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) error {
		if os.Geteuid() != 0 {
			return errors.New("internal fde-uki must run as root through kernel-install or approved setup")
		}
		switch args[0] {
		case "genkey":
			return postinstall.EnsureFDEKeys(native.ExecSource{}, cmd.OutOrStdout())
		case "state":
			data, err := postinstall.ReadFDEEnrollment()
			if err != nil || len(data) == 0 {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		case "record":
			if len(args) != 3 {
				return usageError{errors.New("internal fde-uki record needs a keyslot and token")}
			}
			return postinstall.WriteFDEEnrollment(args[1], args[2])
		case "add":
			if len(args) != 2 {
				return usageError{errors.New("internal fde-uki add needs a kernel release")}
			}
			if onlyIfCurrent {
				return postinstall.BuildFDEUKICurrent(native.ExecSource{}, args[1], cmd.OutOrStdout())
			}
			return postinstall.BuildFDEUKI(native.ExecSource{}, args[1], cmd.OutOrStdout())
		case "remove":
			if len(args) != 2 {
				return usageError{errors.New("internal fde-uki remove needs a kernel release")}
			}
			return postinstall.RemoveFDEUKI(native.ExecSource{}, args[1], cmd.OutOrStdout())
		default:
			return usageError{fmt.Errorf("unsupported fde-uki command %q", args[0])}
		}
	}}
	cmd.Flags().BoolVar(&onlyIfCurrent, "only-if-current", false, "skip the rebuild when the image already targets another kernel")
	return cmd
}
