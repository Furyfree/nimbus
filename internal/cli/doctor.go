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
	"github.com/Furyfree/nimbus/internal/state"
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
	var checkout, machine string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the host and the selected checkout; report, never repair",
		Long: `Doctor inspects the running Fedora system and the selected checkout through
read-only native interfaces and explains every problem it finds with its
impact and remediation. It never repairs, never invokes sudo, and never uses
the network.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, opts, checkout, machine)
		},
	}
	cmd.Flags().StringVar(&checkout, "checkout", "", "checkout to inspect instead of the selector's")
	cmd.Flags().StringVar(&machine, "machine", "", "machine whose system resources to inspect")
	return cmd
}

func runDoctor(cmd *cobra.Command, opts *options, override, machine string) error {
	cfg := doctor.Config{CheckoutOverride: override != ""}
	root := ""
	var sel *selector.Selector
	var resolved *definitions.Resolved
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
		sel, err = selector.Load(path)
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
		errs, ok := errors.AsType[definitions.ErrorList](err)
		if err != nil && !ok {
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
		// The Chezmoi check needs the selected machine's profiles.
		if machine == "" && sel != nil {
			machine = sel.Machine
		}
		if c != nil && len(errs) == 0 && machine != "" {
			if m, ok := c.Machines[machine]; ok {
				if r, rerrs := definitions.Resolve(c, machine); len(rerrs) == 0 {
					cfg.Machine, cfg.Profiles, cfg.Dotfiles = machine, r.Profiles, m.Dotfiles != nil
					resolved = r
				}
			} else {
				return usageError{fmt.Errorf("unknown machine %q", machine)}
			}
		}
	}

	src := newSource()
	f := facts.Inspect(src, root)
	report := doctor.Run(f, cfg)
	if resolved != nil && (len(resolved.Files) > 0 || len(resolved.Services) > 0 || len(resolved.Groups) > 0 || resolved.DefaultTarget != "") {
		applied, err := state.Read(stateRoot)
		checks := []doctor.Check{}
		if err != nil {
			checks = append(checks, doctor.Check{ID: "system-resources", Status: doctor.Unknown, Observation: "cannot read ownership receipts: " + err.Error()})
		} else {
			checks = doctor.SystemResources(src, resolved, applied, f.User.Value.Name)
		}
		for _, check := range checks {
			report.Checks = append(report.Checks, check)
			if check.Status == doctor.Fail {
				report.Failed++
			} else if check.Status == doctor.Unknown {
				report.Unknown++
			}
		}
	}
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
