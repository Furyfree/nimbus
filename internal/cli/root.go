// Package cli is the Cobra command tree. Commands appear only in the phase
// that implements them completely.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/version"
)

// Exit codes.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

type options struct {
	json bool
}

// Envelope is the versioned structure every --json result uses.
type Envelope struct {
	Engine       string `json:"engine"`
	OutputSchema int    `json:"output_schema"`
	Data         any    `json:"data,omitempty"`
	Errors       any    `json:"errors,omitempty"`
}

func writeJSON(w io.Writer, data, errs any) error {
	env := Envelope{Engine: version.Engine, OutputSchema: version.OutputSchema, Data: data, Errors: errs}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// failure carries an exit code out of a command without printing twice.
type failure struct{ code int }

func (f failure) Error() string { return fmt.Sprintf("exit %d", f.code) }

// New builds the command tree.
func New() *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:           "nimbus",
		Short:         "Personal Fedora workstation installer and system manager",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Engine,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("nimbus {{.Version}}\n")
	root.CompletionOptions.DisableDefaultCmd = true
	// Cobra's template lists the help command even when it is hidden; only
	// delivered commands may appear in root help.
	root.SetHelpCommand(&cobra.Command{Use: "help", Hidden: true, RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Root().Help()
	}})
	root.SetUsageTemplate(strings.Replace(root.UsageTemplate(), `(or .IsAvailableCommand (eq .Name "help"))`, `.IsAvailableCommand`, 1))
	root.PersistentFlags().BoolVar(&opts.json, "json", false, "render the result as versioned JSON")
	root.AddCommand(newValidate(opts), newVersion(opts))
	return root
}

// Execute runs the tree and returns the process exit code.
func Execute(args []string, stdout, stderr io.Writer) int {
	root := New()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	if err == nil {
		return ExitOK
	}
	if f, ok := err.(failure); ok {
		return f.code
	}
	fmt.Fprintln(stderr, "error:", err)
	return ExitUsage
}
