package cli

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/plan"
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
		machine = cmp.Or(machine, sel.Machine)
	}
	c, err := loadCheckout(root)
	if err != nil {
		return nil, err
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

// Test hooks for the interactive parts of init.
var (
	// promptLineFn asks one line with a default and returns the answer.
	promptLineFn = func(in io.Reader, out io.Writer, prompt, def string) (string, error) {
		if def != "" {
			prompt += " [" + def + "]"
		}
		if _, err := fmt.Fprintf(out, "%s: ", prompt); err != nil {
			return "", err
		}
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || strings.TrimSpace(line) == "") {
			return "", err
		}
		return cmp.Or(strings.TrimSpace(line), def), nil
	}
)

// loadCheckout loads and validates the definitions of a checkout.
func loadCheckout(root string) (*definitions.Checkout, error) {
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
	return c, nil
}

func describeHardware(hw inspect.Hardware) string {
	parts := []string{}
	if hw.Product != "" {
		parts = append(parts, hw.Product)
	}
	if hw.Board != "" && hw.Board != hw.Product {
		parts = append(parts, "board "+hw.Board)
	}
	if hw.Chassis != "" {
		parts = append(parts, hw.Chassis)
	}
	for _, d := range hw.Display {
		parts = append(parts, "display "+d.Vendor+":"+d.Device)
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, ", ")
}

// newMachineDialog asks for what a manifest needs beyond its ID: profiles,
// components with the detected hardware pre-selected, and the dotfiles
// repository with the one the other manifests share as the default.
func newMachineDialog(in io.Reader, out io.Writer, c *definitions.Checkout, hw inspect.Hardware, f initFlags) (*definitions.Machine, error) {
	id := f.newMachine
	if err := definitions.ValidateID(id); err != nil {
		return nil, fmt.Errorf("machine ID: %w", err)
	}
	var profileItems []pickItem
	for _, pid := range slices.Sorted(maps.Keys(c.Profiles)) {
		profileItems = append(profileItems, pickItem{ID: pid, Selected: pid == "common"})
	}
	profiles, err := pickerFn("profiles for "+id+" (common is always selected)", profileItems)
	if err != nil {
		return nil, err
	}
	profiles = addUnique(profiles, "common")
	proposed := plan.ProposeComponents(c.Components, hw)
	var componentItems []pickItem
	for _, cid := range slices.Sorted(maps.Keys(c.Components)) {
		item := pickItem{ID: cid}
		if c.Components[cid].Detect != nil {
			item.Detail = "hardware"
		}
		if slices.Contains(proposed, cid) {
			item.Selected, item.Detail = true, "detected"
		}
		componentItems = append(componentItems, item)
	}
	components, err := pickerFn("components for "+id+" (detected hardware is pre-selected)", componentItems)
	if err != nil {
		return nil, err
	}
	m := &definitions.Machine{Schema: definitions.CurrentSchema, ID: id, Hardware: hw.Product, Profiles: slices.Sorted(slices.Values(profiles)), Components: slices.Sorted(slices.Values(components)), Packages: []string{}, PackageExclusions: []string{}}
	switch {
	case f.noDotfiles:
	case f.dotfiles != "":
		m.Dotfiles = &definitions.Dotfiles{Repo: f.dotfiles}
	default:
		repo, err := promptLineFn(in, out, "dotfiles repository for Chezmoi (empty for none)", sharedDotfiles(c))
		if err != nil {
			return nil, err
		}
		if repo != "" {
			m.Dotfiles = &definitions.Dotfiles{Repo: repo}
		}
	}
	return m, nil
}

// sharedDotfiles is the dotfiles repository every tracked manifest names,
// or "" when they disagree or none names one.
func sharedDotfiles(c *definitions.Checkout) string {
	repos := map[string]bool{}
	for m := range maps.Values(c.Machines) {
		if m.Dotfiles != nil {
			repos[m.Dotfiles.Repo] = true
		}
	}
	if len(repos) != 1 {
		return ""
	}
	for repo := range maps.Keys(repos) {
		return repo
	}
	return ""
}
