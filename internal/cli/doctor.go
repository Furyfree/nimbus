package cli

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/doctor"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/selector"
)

// newSource builds the inspector's source. Tests replace it with recorded
// output so nothing reads the host.
var newSource = func() facts.Source { return facts.ExecSource{} }

type doctorResult struct {
	Platform facts.Section[facts.Platform] `json:"platform"`
	Checks   []doctor.Check                `json:"checks"`
	Failed   int                           `json:"failed"`
	Unknown  int                           `json:"unknown"`
}

func newDoctor(opts *options) *cobra.Command {
	var checkout string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the host and the selected checkout; report, never repair",
		Long: `Doctor inspects the running Fedora system and the selected checkout through
read-only native interfaces and explains every problem it finds with its
impact and remediation. It never repairs, never invokes sudo, and never uses
the network.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, opts, checkout)
		},
	}
	cmd.Flags().StringVar(&checkout, "checkout", "", "checkout to inspect instead of the selector's")
	return cmd
}

func runDoctor(cmd *cobra.Command, opts *options, override string) error {
	cfg := doctor.Config{CheckoutOverride: override != ""}
	root := ""
	if override != "" {
		r, err := canonical(override)
		if err != nil {
			return err
		}
		root = r
	} else {
		path, err := selector.DefaultPath()
		if err != nil {
			return err
		}
		sel, err := selector.Load(path)
		if err != nil {
			cfg.SelectorError = err.Error()
		} else {
			cfg.ApprovedOrigin = sel.Origin
			if r, err := canonical(sel.Checkout); err != nil {
				cfg.SelectorError = err.Error()
			} else {
				root = r
			}
		}
	}
	if root == "" {
		cfg.DefinitionsError = "no checkout selected"
	} else {
		c, err := definitions.Load(root)
		var errs definitions.ErrorList
		if err != nil && !errors.As(err, &errs) {
			return err
		}
		if len(errs) == 0 {
			errs = definitions.Validate(c)
		}
		if len(errs) > 0 {
			cfg.DefinitionsError = fmt.Sprintf("%d definition error(s); the first: %s", len(errs), errs[0])
		}
		if c != nil && len(c.Root_.Compatibility.Fedora) > 0 {
			cfg.SupportedReleases = c.Root_.Compatibility.Fedora
		}
	}

	f := facts.Inspect(newSource(), root)
	report := doctor.Run(f, cfg)
	result := doctorResult{Platform: f.Platform, Checks: report.Checks, Failed: report.Failed, Unknown: report.Unknown}
	out := cmd.OutOrStdout()
	if opts.json {
		if err := writeJSON(out, result, nil); err != nil {
			return err
		}
	} else {
		var buf bytes.Buffer
		for _, c := range report.Checks {
			fmt.Fprintf(&buf, "%-7s %s: %s\n", c.Status, c.ID, c.Observation)
			if c.Impact != "" {
				fmt.Fprintf(&buf, "        impact: %s\n", c.Impact)
			}
			if c.Remediation != "" {
				fmt.Fprintf(&buf, "        fix: %s\n", c.Remediation)
			}
		}
		fmt.Fprintf(&buf, "%d failed, %d unknown, %d checks\n", report.Failed, report.Unknown, len(report.Checks))
		if _, err := out.Write(buf.Bytes()); err != nil {
			return err
		}
	}
	if report.Failed > 0 {
		return reported{}
	}
	return nil
}
