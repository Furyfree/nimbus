package cli

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

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
		Args: noArgs,
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

// canonical resolves a checkout path once; every later step uses the result.
func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve checkout %s: %w", path, err)
	}
	return root, nil
}

// resolveCheckout returns the canonical explicit override or the selector's
// canonical, origin-verified checkout.
func resolveCheckout(override string) (string, error) {
	if override != "" {
		return canonical(override)
	}
	path, err := selector.DefaultPath()
	if err != nil {
		return "", err
	}
	sel, err := selector.Load(path)
	if err != nil {
		return "", fmt.Errorf("%w (use --checkout to validate a checkout directly)", err)
	}
	root, err := canonical(sel.Checkout)
	if err != nil {
		return "", err
	}
	if err := selector.Verify(sel, root); err != nil {
		return "", err
	}
	return root, nil
}

func runValidate(cmd *cobra.Command, opts *options, root string) error {
	out := cmd.OutOrStdout()
	c, err := definitions.Load(root)
	errs, ok := errors.AsType[definitions.ErrorList](err)
	if err != nil && !ok {
		return err
	}
	if len(errs) == 0 {
		errs = definitions.Validate(c)
	}
	result := validateResult{Checkout: c.Root}
	if len(errs) == 0 {
		result.Digest = c.Digest()
		for _, id := range slices.Sorted(maps.Keys(c.Machines)) {
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
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "checkout: %s\n", result.Checkout)
		if len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(&buf, "error: %s\n", e)
			}
			fmt.Fprintf(&buf, "%d error(s)\n", len(errs))
		} else {
			fmt.Fprintf(&buf, "digest: %s\n", result.Digest)
			for _, r := range result.Machines {
				fmt.Fprintf(&buf, "machine %s: %d profiles, %d components, %d packages, %d removals, %d files, %d repositories\n",
					r.Machine, len(r.Profiles), len(r.Components), len(r.Packages), len(r.Removes), len(r.Files), len(r.Repositories))
			}
			buf.WriteString("ok\n")
		}
		if _, err := out.Write(buf.Bytes()); err != nil {
			return err
		}
	}
	if len(errs) > 0 {
		return reported{}
	}
	return nil
}
