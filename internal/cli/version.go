package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/version"
)

func newVersion(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Report the engine build and supported schema versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.Current()
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), info, nil)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "nimbus %s\n", info.Engine)
			fmt.Fprintf(w, "definition schema: %d\n", info.DefinitionSchema)
			fmt.Fprintf(w, "output schema: %d\n", info.OutputSchema)
			return nil
		},
	}
}
