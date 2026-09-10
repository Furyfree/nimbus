package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/launch"
)

func newLaunch(opts *options) *cobra.Command {
	group := &cobra.Command{Use: "launch", Short: "Open a browser or webapp", Args: noArgs}
	for _, webapp := range []bool{false, true} {
		var private bool
		use := "browser [URL]"
		if webapp {
			use = "webapp URL"
		}
		cmd := &cobra.Command{Use: use, Short: "Open an HTTP(S) address in a desktop browser",
			Args: func(cmd *cobra.Command, args []string) error {
				if len(args) > 1 || (webapp && len(args) != 1) {
					return usageError{errors.New("expected one URL (optional for browser)")}
				}
				return nil
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				if opts.json {
					return usageError{errors.New("graphical browser commands do not support --json")}
				}
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				dataHome := os.Getenv("XDG_DATA_HOME")
				if !filepath.IsAbs(dataHome) {
					dataHome = filepath.Join(home, ".local", "share")
				}
				dirs := os.Getenv("XDG_DATA_DIRS")
				if dirs == "" {
					dirs = "/usr/local/share:/usr/share"
				}
				roots := append([]string{dataHome}, strings.Split(dirs, ":")...)
				target := ""
				if len(args) == 1 {
					target = args[0]
				}
				src := newSource()
				argv, err := launch.Browser(src, roots, target, private, webapp)
				if err != nil {
					return err
				}
				return src.Stream(cmd.OutOrStdout(), cmd.ErrOrStderr(), argv[0], argv[1:]...)
			},
		}
		cmd.Flags().BoolVar(&private, "private", false, "open a private browser window")
		group.AddCommand(cmd)
	}
	return group
}
