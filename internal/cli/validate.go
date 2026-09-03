package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/selector"
)

type validateResult struct {
	Checkout string                  `json:"checkout"`
	Digest   string                  `json:"digest,omitempty"`
	Machines []*definitions.Resolved `json:"machines,omitempty"`
}

func newValidate(opts *options) *cobra.Command {
	var checkout string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate every definition in the checkout and resolve every machine",
		Long: `Validate reads the selected Nimbus checkout, checks every definition, and
resolves every tracked machine. It inspects configuration only: no system
inspection, no command execution, no network, and no writes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveCheckout(checkout)
			if err != nil {
				return err
			}
			return runValidate(cmd, opts, root)
		},
	}
	cmd.Flags().StringVar(&checkout, "checkout", "", "checkout to validate instead of the selector's")
	return cmd
}

// resolveCheckout returns the explicit override or the selector's verified
// checkout.
func resolveCheckout(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	path, err := selector.DefaultPath()
	if err != nil {
		return "", err
	}
	sel, err := selector.Load(path)
	if err != nil {
		return "", fmt.Errorf("%w (use --checkout to validate a checkout directly)", err)
	}
	if err := selector.Verify(sel, sel.Checkout); err != nil {
		return "", err
	}
	return sel.Checkout, nil
}

func runValidate(cmd *cobra.Command, opts *options, root string) error {
	out := cmd.OutOrStdout()
	c, err := definitions.Load(root)
	var errs definitions.ErrorList
	if err != nil {
		if !errors.As(err, &errs) {
			return err
		}
	}
	if len(errs) == 0 {
		errs = definitions.Validate(c)
	}
	result := validateResult{Checkout: c.Root}
	if len(errs) == 0 {
		result.Digest = c.Digest()
		for _, id := range sortedMachineIDs(c) {
			r, rerrs := definitions.Resolve(c, id)
			if len(rerrs) > 0 {
				errs = append(errs, rerrs...)
				continue
			}
			result.Machines = append(result.Machines, r)
		}
	}
	if opts.json {
		var jsonErrs any
		if len(errs) > 0 {
			jsonErrs = errs
		}
		if err := writeJSON(out, result, jsonErrs); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(out, "checkout: %s\n", result.Checkout)
		if len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(out, "error: %s\n", e)
			}
			fmt.Fprintf(out, "%d error(s)\n", len(errs))
		} else {
			fmt.Fprintf(out, "digest: %s\n", result.Digest)
			for _, r := range result.Machines {
				fmt.Fprintf(out, "machine %s: %d profiles, %d components, %d packages, %d removals, %d files, %d repositories\n",
					r.Machine, len(r.Profiles), len(r.Components), len(r.Packages), len(r.Removes), len(r.Files), len(r.Repositories))
			}
			fmt.Fprintln(out, "ok")
		}
	}
	if len(errs) > 0 {
		return failure{code: ExitFailure}
	}
	return nil
}

func sortedMachineIDs(c *definitions.Checkout) []string {
	ids := make([]string, 0, len(c.Machines))
	for id := range c.Machines {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}
