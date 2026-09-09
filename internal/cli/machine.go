package cli

import (
	"errors"
	"fmt"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/selector"
)

// machineFlags are the invocation-local overrides commands that load desired
// configuration accept. An omitted value comes from the selector; overrides
// never rewrite it.
type machineFlags struct {
	checkout string
	machine  string
}

// selected is one machine's desired configuration, ready for inspection.
type selected struct {
	Root     string
	Checkout *definitions.Checkout
	Resolved *definitions.Resolved
}

// loadSelected resolves the checkout and machine from the overrides or the
// selector and validates the definitions.
func loadSelected(flags machineFlags) (*selected, error) {
	root := ""
	machine := flags.machine
	if flags.checkout != "" {
		r, err := canonical(flags.checkout)
		if err != nil {
			return nil, err
		}
		root = r
	}
	if root == "" || machine == "" {
		path, err := selector.DefaultPath()
		if err != nil {
			return nil, err
		}
		sel, err := selector.Load(path)
		if err != nil {
			return nil, fmt.Errorf("%w (use --checkout and --machine to name them directly)", err)
		}
		if root == "" {
			root, err = canonical(sel.Checkout)
			if err != nil {
				return nil, err
			}
			if err := selector.Verify(sel, root); err != nil {
				return nil, err
			}
		}
		if machine == "" {
			machine = sel.Machine
		}
	}
	c, err := definitions.Load(root)
	errs, ok := errors.AsType[definitions.ErrorList](err)
	if err != nil && !ok {
		return nil, err
	}
	if len(errs) == 0 {
		errs = definitions.Validate(c)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("checkout has %d definition error(s); run nimbus validate. First: %s", len(errs), errs[0])
	}
	r, rerrs := definitions.Resolve(c, machine)
	if len(rerrs) > 0 {
		return nil, fmt.Errorf("machine %s: %s", machine, rerrs[0])
	}
	return &selected{Root: root, Checkout: c, Resolved: r}, nil
}

func addMachineFlags(flags *machineFlags, set interface {
	StringVar(p *string, name, value, usage string)
}) {
	set.StringVar(&flags.checkout, "checkout", "", "checkout to use instead of the selector's")
	set.StringVar(&flags.machine, "machine", "", "machine manifest to use instead of the selector's")
}
