// Package cli is the Cobra command tree. Commands appear only in the phase
// that implements them completely.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	json        bool
	showVersion bool
	installLog  *installLog
}

// Envelope is the versioned structure every --json result uses.
type Envelope struct {
	Engine       string `json:"engine"`
	OutputSchema int    `json:"output_schema"`
	Data         any    `json:"data,omitempty"`
	Errors       any    `json:"errors,omitempty"`
}

// EnvelopeError is one structured error in the envelope.
type EnvelopeError struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func writeJSON(w io.Writer, data, errs any) error {
	env := Envelope{Engine: version.Engine, OutputSchema: version.OutputSchema, Data: data, Errors: errs}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// usageError marks invalid invocation: exit 2.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }

// reported marks a failure whose details were already written: exit 1.
type reported struct{}

func (reported) Error() string { return "reported" }

func noArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return usageError{err}
	}
	return nil
}

func newRoot() (*cobra.Command, *options) {
	opts := &options{}
	root := &cobra.Command{
		Use:           "nimbus",
		Short:         "Personal Fedora workstation installer and system manager",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.showVersion {
				return renderVersion(cmd.OutOrStdout(), opts.json)
			}
			return cmd.Help()
		},
	}
	root.PersistentFlags().BoolVarP(&opts.json, "json", "j", false, "render the result as versioned JSON")
	// Nimbus runs as the user and escalates per command; only the hidden
	// record action is meant for root.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if os.Geteuid() == 0 && !strings.HasPrefix(cmd.CommandPath(), "nimbus internal") {
			return usageError{errors.New("nimbus runs as the normal user and uses sudo per command; do not run it as root")}
		}
		return nil
	}
	root.Flags().BoolVarP(&opts.showVersion, "version", "v", false, "same as nimbus version")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return usageError{err} })
	root.CompletionOptions.DisableDefaultCmd = true
	// Cobra's template lists the help command even when it is hidden; only
	// delivered commands may appear in root help.
	root.SetHelpCommand(&cobra.Command{Use: "help", Hidden: true, Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Root().Help()
	}})
	root.SetUsageTemplate(strings.Replace(root.UsageTemplate(), `(or .IsAvailableCommand (eq .Name "help"))`, `.IsAvailableCommand`, 1))
	root.AddCommand(newInternal(), newComponents(opts), newDoctor(opts), newDotfiles(opts), newFiles(opts), newInit(opts), newManaged(opts), newPackages(opts), newProfiles(opts), newStatus(opts), newSync(opts), newUnmanaged(opts), newValidate(opts), newVersion(opts), newWhy(opts))
	return root, opts
}

// New builds the command tree.
func New() *cobra.Command {
	root, _ := newRoot()
	return root
}

// Execute runs the tree and returns the process exit code. Usage errors exit
// 2; every other failure exits 1 and, with --json, is rendered in the
// envelope.
func Execute(args []string, stdout, stderr io.Writer) int {
	root, opts := newRoot()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	if err == nil {
		return ExitOK
	}
	if _, ok := errors.AsType[usageError](err); ok {
		fmt.Fprintln(stderr, "error:", err)
		return ExitUsage
	}
	if errors.Is(err, reported{}) {
		return ExitFailure
	}
	if opts.json {
		if jerr := writeJSON(stdout, nil, []EnvelopeError{{Message: err.Error()}}); jerr == nil {
			return ExitFailure
		}
	}
	fmt.Fprintln(stderr, "error:", err)
	return ExitFailure
}
