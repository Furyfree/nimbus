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
	cmd := &cobra.Command{Use: "fde-uki", Hidden: true, Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if os.Geteuid() != 0 {
			return errors.New("internal fde-uki must run as root through kernel-install or approved setup")
		}
		switch args[0] {
		case "add":
			return postinstall.BuildFDEUKI(native.ExecSource{}, args[1], cmd.OutOrStdout())
		case "remove":
			_, err := fmt.Fprintln(cmd.ErrOrStderr(), "fde-uki remove: scoped removal is not implemented; the current image is retained")
			return err
		default:
			return usageError{fmt.Errorf("unsupported fde-uki command %q", args[0])}
		}
	}}
	return cmd
}
