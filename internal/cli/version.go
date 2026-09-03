package cli

import (
	"bytes"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/version"
)

func newVersion(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Report the engine build and supported schema versions",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderVersion(cmd.OutOrStdout(), opts.json)
		},
	}
}

// renderVersion serves both nimbus version and nimbus --version.
func renderVersion(w io.Writer, asJSON bool) error {
	info := version.Current()
	if asJSON {
		return writeJSON(w, info, nil)
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "nimbus %s\n", info.Engine)
	fmt.Fprintf(&buf, "definition schema: %d\n", info.DefinitionSchema)
	fmt.Fprintf(&buf, "output schema: %d\n", info.OutputSchema)
	_, err := w.Write(buf.Bytes())
	return err
}
