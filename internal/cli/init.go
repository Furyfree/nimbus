package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/selector"
)

// Test hooks for the interactive parts of init.
var (
	pickOneFn = runPickOne
	// promptLineFn asks one line with a default and returns the answer.
	promptLineFn = func(in io.Reader, out io.Writer, prompt, def string) string {
		if def != "" {
			fmt.Fprintf(out, "%s [%s]: ", prompt, def)
		} else {
			fmt.Fprintf(out, "%s: ", prompt)
		}
		line, _ := bufio.NewReader(in).ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return def
		}
		return line
	}
)

// DefaultCheckout is where the installer clones the repository.
const DefaultCheckout = "~/.local/share/nimbus"

type initFlags struct {
	checkout, machine, newMachine, dotfiles string
	noDotfiles, yes                         bool
}

func newInit(opts *options) *cobra.Command {
	var f initFlags
	cmd := &cobra.Command{
		Use:   "init [--checkout DIR] [--machine ID | --new ID] [--dotfiles URL | --no-dotfiles] [-y]",
		Short: "Select the machine, write the selector, sync, and hand off to Chezmoi",
		Long: `Init is the first run on a machine. It validates the checkout, asks which
tracked machine this is or describes a new one from the hardware it detects,
writes the selector at ~/.config/nimbus/config.toml, runs the first sync, and
performs the one Chezmoi initialization when the manifest names a dotfiles
repository. It is rerunnable: an existing selector for the same checkout is
reused.

  --checkout DIR   the Nimbus checkout (default ` + DefaultCheckout + `)
  --machine ID     a tracked machine; otherwise init asks
  --new ID         describe a new machine and write its manifest
  --dotfiles URL   the dotfiles repository for a new machine
  --no-dotfiles    a new machine without a Chezmoi handoff
  -y, --yes        answer the sync question with yes`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, opts, f)
		},
	}
	cmd.Flags().StringVar(&f.checkout, "checkout", "", "the Nimbus checkout")
	cmd.Flags().StringVar(&f.machine, "machine", "", "a tracked machine ID")
	cmd.Flags().StringVar(&f.newMachine, "new", "", "describe a new machine with this ID")
	cmd.Flags().StringVar(&f.dotfiles, "dotfiles", "", "the dotfiles repository for a new machine")
	cmd.Flags().BoolVar(&f.noDotfiles, "no-dotfiles", false, "a new machine without a Chezmoi handoff")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "answer the sync question with yes")
	return cmd
}

func runInit(cmd *cobra.Command, opts *options, f initFlags) error {
	out, in := cmd.OutOrStdout(), cmd.InOrStdin()
	if opts.json {
		return usageError{errors.New("init is interactive; it has no JSON form")}
	}
	if f.machine != "" && f.newMachine != "" {
		return usageError{errors.New("--machine and --new exclude each other")}
	}
	if f.dotfiles != "" && f.noDotfiles {
		return usageError{errors.New("--dotfiles and --no-dotfiles exclude each other")}
	}
	checkout := f.checkout
	if checkout == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		checkout = filepath.Join(home, ".local", "share", "nimbus")
	}
	root, err := canonical(checkout)
	if err != nil {
		return err
	}
	c, err := loadCheckout(root)
	if err != nil {
		return err
	}
	rawOrigin, err := selector.CheckoutOrigin(root)
	if err != nil {
		return fmt.Errorf("the checkout must be a Git clone: %w", err)
	}
	origin, err := selector.NormalizeOrigin(rawOrigin)
	if err != nil {
		return err
	}
	src := newSource()
	hw := facts.Inspect(src, root).Hardware
	if hw.Known() {
		fmt.Fprintf(out, "hardware: %s\n", describeHardware(hw.Value))
	}

	// The machine: a tracked manifest, or a new one from the dialog.
	selectorPath, err := selector.DefaultPath()
	if err != nil {
		return err
	}
	machine := f.machine
	if f.newMachine != "" {
		m, err := newMachineDialog(in, out, c, hw.Value, f)
		if err != nil {
			return err
		}
		path := manifestPath(root, m.ID)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("machine %s already exists at %s; pick it with --machine or choose another ID", m.ID, path)
		}
		header := fmt.Sprintf("# %s: %s.\n", m.ID, describeHardware(hw.Value))
		if err := writeManifest(path, renderManifest([]byte(header), m)); err != nil {
			return err
		}
		fmt.Fprintf(out, "wrote %s; the Git change is yours to commit\n", path)
		if c, err = loadCheckout(root); err != nil {
			return fmt.Errorf("the new manifest does not validate: %w", err)
		}
		machine = m.ID
	}
	if machine == "" {
		if existing, err := selector.Load(selectorPath); err == nil && existing.Checkout == root {
			machine = existing.Machine
			fmt.Fprintf(out, "the selector already names %s for this checkout\n", machine)
		}
	}
	if machine == "" {
		machine, err = chooseMachine(c, hw.Value)
		if err != nil {
			return err
		}
		if machine == "new" {
			return usageError{errors.New("describe the new machine with --new ID; the dialog then asks the rest")}
		}
	}
	if _, ok := c.Machines[machine]; !ok {
		return fmt.Errorf("machine %s is not tracked in %s", machine, root)
	}
	if err := selector.Write(selectorPath, &selector.Selector{Schema: selector.CurrentSchema, Checkout: root, Machine: machine, Origin: origin}); err != nil {
		return err
	}
	fmt.Fprintf(out, "selected %s; selector written to %s\n\n", machine, selectorPath)

	// The first sync, then the one Chezmoi initialization.
	if err := runSyncWith(cmd, opts, machineFlags{checkout: root, machine: machine}, syncFlags{yes: f.yes}, nil); err != nil {
		return err
	}
	m := c.Machines[machine]
	r, rerrs := definitions.Resolve(c, machine)
	if len(rerrs) > 0 {
		return rerrs[0]
	}
	fmt.Fprintln(out)
	// A failed handoff leaves the system usable: it is reported with the
	// command to retry, and the rest of init still runs.
	handoffErr := chezmoiHandoff(src, out, machine, r.Profiles, m.Dotfiles)
	if handoffErr != nil {
		fmt.Fprintf(out, "the Chezmoi handoff did not complete: %v\nrun nimbus init again once the repository is reachable\n", handoffErr)
	}
	// The user-scope steps that waited for the Chezmoi-written files run
	// now, under the answer already given.
	fmt.Fprintln(out)
	if err := runSyncWith(cmd, opts, machineFlags{checkout: root, machine: machine}, syncFlags{yes: true, noUpgrade: true}, nil); err != nil {
		return err
	}
	if handoffErr != nil {
		return reported{}
	}
	return nil
}

// loadCheckout loads and validates the definitions of a checkout.
func loadCheckout(root string) (*definitions.Checkout, error) {
	c, err := definitions.Load(root)
	var errs definitions.ErrorList
	if err != nil && !errors.As(err, &errs) {
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

func describeHardware(hw facts.Hardware) string {
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

// chooseMachine lists the tracked machines with the one the hardware
// matches pre-selected, plus "new".
func chooseMachine(c *definitions.Checkout, hw facts.Hardware) (string, error) {
	match := plan.MatchMachine(c.Machines, hw)
	var items []pickItem
	for _, id := range sortedKeysOf(c.Machines) {
		items = append(items, pickItem{ID: id, Detail: manifestComment(c, id), Selected: id == match})
	}
	items = append(items, pickItem{ID: "new", Detail: "describe a new machine", Selected: match == ""})
	return pickOneFn("which machine is this?", items)
}

// manifestComment is the first comment line of a manifest, its description.
func manifestComment(c *definitions.Checkout, id string) string {
	entry, ok := c.Entry("machines/" + id + ".toml")
	if !ok {
		return ""
	}
	first, _, _ := strings.Cut(string(entry.Content), "\n")
	return strings.TrimSpace(strings.TrimPrefix(first, "#"))
}

// newMachineDialog asks for what a manifest needs beyond its ID: profiles,
// components with the detected hardware pre-selected, and the dotfiles
// repository with the one the other manifests share as the default.
func newMachineDialog(in io.Reader, out io.Writer, c *definitions.Checkout, hw facts.Hardware, f initFlags) (*definitions.Machine, error) {
	id := f.newMachine
	if err := definitions.ValidateID(id); err != nil {
		return nil, fmt.Errorf("machine ID: %w", err)
	}
	var profileItems []pickItem
	for _, pid := range sortedKeysOf(c.Profiles) {
		profileItems = append(profileItems, pickItem{ID: pid, Selected: pid == "common"})
	}
	profiles, err := pickerFn("profiles for "+id+" (common is always selected)", profileItems)
	if err != nil {
		return nil, err
	}
	profiles = addUnique(profiles, "common")
	proposed := plan.ProposeComponents(c.Components, hw)
	var componentItems []pickItem
	for _, cid := range sortedKeysOf(c.Components) {
		item := pickItem{ID: cid}
		if c.Components[cid].Detect != nil {
			item.Detail = "hardware"
		}
		for _, p := range proposed {
			if p == cid {
				item.Selected, item.Detail = true, "detected"
			}
		}
		componentItems = append(componentItems, item)
	}
	components, err := pickerFn("components for "+id+" (detected hardware is pre-selected)", componentItems)
	if err != nil {
		return nil, err
	}
	m := &definitions.Machine{Schema: definitions.CurrentSchema, ID: id, Hardware: hw.Product, Profiles: sortedCopy(profiles), Components: sortedCopy(components), Packages: []string{}, PackageExclusions: []string{}}
	switch {
	case f.noDotfiles:
	case f.dotfiles != "":
		m.Dotfiles = &definitions.Dotfiles{Repo: f.dotfiles}
	default:
		repo := promptLineFn(in, out, "dotfiles repository for Chezmoi (empty for none)", sharedDotfiles(c))
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
	for _, m := range c.Machines {
		if m.Dotfiles != nil {
			repos[m.Dotfiles.Repo] = true
		}
	}
	if len(repos) != 1 {
		return ""
	}
	for repo := range repos {
		return repo
	}
	return ""
}

// chezmoiHandoff performs the one permitted Chezmoi initialization, as the
// user, with the machine, the Nimbus flag, and the profiles passed through
// Chezmoi's prompt flags. An initialized Chezmoi is left alone and the
// refresh command is printed instead.
func chezmoiHandoff(src facts.Source, out io.Writer, machine string, profiles []string, dotfiles *definitions.Dotfiles) error {
	if dotfiles == nil {
		fmt.Fprintln(out, "no dotfiles repository is declared for this machine; the Chezmoi handoff stays pending")
		return nil
	}
	if _, err := src.LookPath("chezmoi"); err != nil {
		fmt.Fprintln(out, "chezmoi is not installed yet; run nimbus init again after the next sync for the handoff")
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	flags := []string{"--promptString", "Machine=" + machine, "--promptBool", "ManagedByNimbus=true", "--promptMultichoice", "Profiles=" + strings.Join(profiles, "/")}
	if facts.ChezmoiInitialized(src, home) {
		fmt.Fprintln(out, "Chezmoi is already initialized; to refresh its answers run:")
		fmt.Fprintf(out, "  chezmoi init --prompt %s\n", strings.Join(flags, " "))
		return nil
	}
	argv := append([]string{"init"}, append(flags, dotfiles.Repo)...)
	fmt.Fprintf(out, "-> initialize Chezmoi from %s\n   $ chezmoi %s\n", dotfiles.Repo, strings.Join(argv, " "))
	if err := src.Stream(out, out, "chezmoi", argv...); err != nil {
		return fmt.Errorf("chezmoi init: %w", err)
	}
	fmt.Fprintln(out, "Chezmoi is initialized. Review and apply the user configuration yourself:")
	fmt.Fprintln(out, "  chezmoi diff\n  chezmoi apply")
	return nil
}
