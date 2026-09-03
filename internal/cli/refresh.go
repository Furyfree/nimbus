package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newRefresh is the one explicit network step outside upgrade: it refreshes
// DNF's metadata cache so plan and status read current package lists. It is
// its own command so plan stays free of network access.
func newRefresh(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Refresh DNF's metadata cache so plan reads current package lists",
		Long: `Refresh runs dnf5 makecache as the normal user. It is the only network
step among the read-only commands and changes nothing but DNF's metadata
cache; plan, status, and the views read that cache and never refresh it
themselves.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := newSource().Run("dnf5", "makecache"); err != nil {
				return fmt.Errorf("refresh metadata: %w", err)
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"refreshed": "dnf5 makecache"}, nil)
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "metadata cache refreshed with dnf5 makecache")
			return err
		},
	}
}
