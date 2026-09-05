package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Furyfree/nimbus/internal/apply"

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
		Short: "Select the machine, write the selector, sync, apply dotfiles, and install user tools",
		Long: `Init is the first run on a machine. It validates the checkout, asks which
tracked machine this is or describes a new one from the hardware it detects,
writes the selector at ~/.config/nimbus/config.toml, runs the first sync, and
initializes Chezmoi when needed, applies its local source, and installs the
selected user tools when the manifest names a dotfiles repository. It is rerunnable: an existing selector for the same checkout is
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

func runInit(cmd *cobra.Command, opts *options, f initFlags) (retErr error) {
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
	steps := []runStep{{Name: "selection", Status: "skipped"}, {Name: "system installation", Status: "skipped"}, {Name: "dotfiles and tools", Status: "skipped"}}
	active := 0
	defer func() {
		if retErr != nil && steps[active].Status != "failed" {
			steps[active].Status = "failed"
			if !errors.Is(retErr, reported{}) {
				steps[active].Detail = retErr.Error()
			}
		}
		renderRunSummary(out, "init", steps)
	}()
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
	if err := facts.CheckPlatform(src, c.Definitions().Compatibility.Fedora); err != nil {
		return err
	}
	originalDigest := c.Digest()
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
	var newManifest []byte
	var newManifestPath string
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
		newManifest = renderManifest([]byte(header), m)
		newManifestPath = path
		c.Machines[m.ID] = m
		c.Entries = append(c.Entries, definitions.Entry{Path: "machines/" + m.ID + ".toml", Mode: 0o100644, Content: append(append([]byte(nil), newManifest...), '\n')})
		sort.Slice(c.Entries, func(i, j int) bool { return c.Entries[i].Path < c.Entries[j].Path })
		if errs := definitions.Validate(c); len(errs) > 0 {
			return fmt.Errorf("the new manifest does not validate: %s", errs[0])
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
	r, rerrs := definitions.Resolve(c, machine)
	if len(rerrs) > 0 {
		return rerrs[0]
	}
	// Only explicit Nimbus declarations need a second pass. Chezmoi owns
	// the tracked Mise tool installation within its apply stage.
	deferredTools := false
	for _, pkg := range r.Packages {
		deferredTools = deferredTools || pkg.Prefix == definitions.PrefixCargo
	}
	for _, installer := range r.Installers {
		deferredTools = deferredTools || len(installer.Installer.Install) > 0
	}
	if deferredTools {
		steps = append(steps, runStep{Name: "remaining Nimbus user tools", Status: "skipped"})
	}
	lockPath, err := apply.LockPath()
	if err != nil {
		return err
	}
	lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: "init", PID: os.Getpid(), Started: time.Now().UTC()})
	if err != nil {
		return err
	}
	defer lock.Release()
	fresh, err := loadCheckout(root)
	if err != nil {
		return err
	}
	if fresh.Digest() != originalDigest {
		return errors.New("definitions changed during machine selection; run init again")
	}
	if newManifestPath != "" {
		if err := writeManifest(newManifestPath, newManifest); err != nil {
			return err
		}
		fmt.Fprintf(out, "wrote %s; the Git change is yours to commit\n", newManifestPath)
	}
	if err := selector.Write(selectorPath, &selector.Selector{Schema: selector.CurrentSchema, Checkout: root, Machine: machine, Origin: origin}); err != nil {
		return err
	}
	fmt.Fprintf(out, "selected %s; selector written to %s\n\n", machine, selectorPath)

	steps[0].Status = "succeeded"
	m := c.Machines[machine]
	if m.Dotfiles != nil {
		fmt.Fprintf(out, "Installation will initialize Chezmoi from %s if needed, then run chezmoi apply, including its declared user-tool installation scripts.\n", m.Dotfiles.Repo)
	} else {
		steps[2].Detail = "no dotfiles repository declared"
	}
	active = 1
	flags := machineFlags{checkout: root, machine: machine}
	if err := runSyncWith(cmd, opts, flags, syncFlags{yes: f.yes, deferUser: true}, lock); err != nil {
		for i := 2; i < len(steps); i++ {
			steps[i].Detail = "system installation did not complete"
		}
		return err
	}
	steps[1].Status = "succeeded"
	fresh, err = loadCheckout(root)
	if err != nil {
		return err
	}
	if fresh.Digest() != c.Digest() {
		return errors.New("definitions changed during initialization; run init again")
	}
	active = 2
	if m.Dotfiles != nil {
		if err := chezmoiHandoff(src, out, machine, r.Profiles, m.Dotfiles); err != nil {
			if deferredTools {
				steps[3].Detail = "dotfiles or tool installation failed; run nimbus init again after fixing the reported error"
			}
			return err
		}
		steps[2].Status = "succeeded"
	}
	if !deferredTools {
		return nil
	}
	active = 3
	if err := runSyncWith(cmd, opts, flags, syncFlags{yes: true, noUpgrade: true, userOnly: true, definitionsDigest: c.Digest()}, lock); err != nil {
		return err
	}
	steps[3].Status = "succeeded"
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

// chezmoiHandoff initializes a missing source, then applies its local state.
// Chezmoi owns conflict handling and secrets; Nimbus never forces overwrites.
func chezmoiHandoff(src facts.Source, out io.Writer, machine string, profiles []string, dotfiles *definitions.Dotfiles) error {
	if dotfiles == nil {
		fmt.Fprintln(out, "no dotfiles repository is declared; dotfiles skipped")
		return nil
	}
	wantOrigin, err := selector.NormalizeOrigin(dotfiles.Repo)
	if err != nil {
		return fmt.Errorf("dotfiles repository must be an explicit Git URL: %w", err)
	}
	if _, err := src.LookPath("chezmoi"); err != nil {
		return errors.New("chezmoi is not installed yet; run nimbus init again after fixing the system installation")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	flags := []string{"--promptString", "Machine=" + machine, "--promptBool", "ManagedByNimbus=true", "--promptMultichoice", "Profiles=" + strings.Join(profiles, "/")}
	if !facts.ChezmoiInitialized(src, home) {
		argv := append([]string{"init"}, append(flags, "--", dotfiles.Repo)...)
		fmt.Fprintf(out, "-> initialize Chezmoi from %s\n   $ chezmoi %s\n", dotfiles.Repo, strings.Join(argv, " "))
		if err := src.Stream(out, out, "chezmoi", argv...); err != nil {
			return fmt.Errorf("chezmoi init: %w", err)
		}
	} else {
		fmt.Fprintln(out, "Chezmoi is already initialized; applying its existing local source")
		fmt.Fprintf(out, "to refresh its answers: chezmoi init --prompt %s\n", strings.Join(flags, " "))
	}
	sourcePath, err := src.Run("chezmoi", "source-path")
	if err != nil {
		return fmt.Errorf("read Chezmoi source path: %w", err)
	}
	root := strings.TrimSpace(string(sourcePath))
	if !filepath.IsAbs(root) {
		return errors.New("Chezmoi returned no absolute source path")
	}
	origin, err := src.Run("git", facts.GitArgs(root, "config", "--get", "remote.origin.url")...)
	if err != nil {
		return fmt.Errorf("read Chezmoi source origin: %w", err)
	}
	actualOrigin, err := selector.NormalizeOrigin(strings.TrimSpace(string(origin)))
	if err != nil || actualOrigin != wantOrigin {
		return errors.New("the existing Chezmoi source does not match the declared dotfiles repository; inspect it before retrying")
	}
	data, err := src.Run("chezmoi", facts.ChezmoiDataArgs...)
	if err != nil {
		return fmt.Errorf("read Chezmoi selection: %w", err)
	}
	var selection struct {
		Machine         string
		ManagedByNimbus bool
		Profiles        []string
	}
	if err := json.Unmarshal(data, &selection); err != nil {
		return fmt.Errorf("read Chezmoi selection: %w", err)
	}
	if selection.Machine != machine || !selection.ManagedByNimbus || !slices.Equal(sortedCopy(selection.Profiles), sortedCopy(profiles)) {
		return fmt.Errorf("Chezmoi's stored selection differs; refresh it before retrying: chezmoi init --prompt %s", strings.Join(flags, " "))
	}
	fmt.Fprintln(out, "-> apply user configuration and install its declared tools\n   $ chezmoi apply")
	if err := src.Stream(out, out, "chezmoi", "apply"); err != nil {
		return fmt.Errorf("chezmoi apply: %w", err)
	}
	return nil
}
