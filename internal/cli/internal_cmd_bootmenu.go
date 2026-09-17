package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/bootmenu"
	"github.com/Furyfree/nimbus/internal/native"
)

// newInternalBootMenu is the narrow privileged mirror builder the grub.d
// payload runs during grub2-mkconfig. It validates the marker itself; paths
// are fixed and there is no argument input.
func newInternalBootMenu() *cobra.Command {
	return &cobra.Command{Use: "boot-menu", Hidden: true, Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if os.Geteuid() != 0 {
			return errors.New("internal boot-menu must run as root through grub2-mkconfig")
		}
		if _, err := os.Stat(bootmenu.Marker); err != nil {
			return errors.New("the boot-theme marker is absent; the grouped kernel menu is not enabled")
		}
		id, err := bootmenu.Mirror(native.ExecSource{}, bootmenu.EntriesDir, bootmenu.Grubenv, bootmenu.MirrorDir)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), id)
		return err
	}}
}
