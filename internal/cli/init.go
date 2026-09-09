package cli

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/Furyfree/nimbus/internal/apply"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/doctor"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/selector"
)

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
		if err != nil && !(errors.Is(err, io.EOF) && strings.TrimSpace(line) != "") {
			return "", err
		}
		return cmp.Or(strings.TrimSpace(line), def), nil
	}
)

// DefaultCheckout is where the installer clones the repository.
const DefaultCheckout = "~/.local/share/nimbus"

type initFlags struct {
	checkout, machine, newMachine, dotfiles string
	noDotfiles, onePasswordSSH              bool
}

func newInit(opts *options) *cobra.Command {
	var f initFlags
	cmd := &cobra.Command{
		Use:   "init [--checkout DIR] [--machine ID | --new ID] [--dotfiles URL | --no-dotfiles] [-y]",
		Short: "Select the machine, write the selector, sync, apply dotfiles, and install user tools",
		Long: `Init installs the machine selected by --machine or an existing selector.
It validates the checkout, writes ~/.config/nimbus/config.toml, shows and runs
the complete system plan without a confirmation, then initializes and applies
Chezmoi and its selected user tools when the manifest names a dotfiles repository.
Sudo authentication and Chezmoi may still ask for input. Explicit --new opens
the new-machine dialogue. Existing checkout or origin trust cannot change in init.

  --checkout DIR   the Nimbus checkout (default ` + DefaultCheckout + `)
  --machine ID     a tracked machine; otherwise reuse the existing selector
  --new ID         describe a new machine and write its manifest
  --dotfiles URL   the dotfiles repository for a new machine
  --no-dotfiles    a new machine without a Chezmoi handoff
  -y, --yes        accepted for compatibility; init already proceeds without confirmation`,
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
	cmd.Flags().BoolVar(&f.onePasswordSSH, "onepassword-ssh", false, "enable 1Password SSH integration during initial Chezmoi setup")
	cmd.Flags().BoolP("yes", "y", false, "accepted for compatibility; init already proceeds without confirmation")
	return cmd
}

func runInit(cmd *cobra.Command, opts *options, f initFlags) (retErr error) {
	out, in := cmd.OutOrStdout(), cmd.InOrStdin()
	if opts.json {
		return usageError{errors.New("init has no JSON form; sudo and Chezmoi may require a terminal")}
	}
	if f.machine != "" && f.newMachine != "" {
		return usageError{errors.New("--machine and --new exclude each other")}
	}
	if f.onePasswordSSH && f.noDotfiles {
		return usageError{errors.New("--onepassword-ssh and --no-dotfiles exclude each other")}
	}
	if f.dotfiles != "" && f.noDotfiles {
		return usageError{errors.New("--dotfiles and --no-dotfiles exclude each other")}
	}
	if f.newMachine == "" && (f.dotfiles != "" || f.noDotfiles) {
		return usageError{errors.New("--dotfiles and --no-dotfiles require --new; tracked machines use their manifest")}
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
	if err := facts.CheckPlatform(src, c.Definitions().Compatibility.Fedora); err != nil {
		return err
	}
	log, err := openInstallLog()
	if err != nil {
		return fmt.Errorf("start installation log: %w", err)
	}
	fmt.Fprintf(out, "Installation logs: %s\n", log.dir)
	previousLog := opts.installLog
	opts.installLog = log
	oldOut, oldErr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	cmd.SetOut(installWriter{oldOut, log})
	cmd.SetErr(installWriter{oldErr, log})
	out = cmd.OutOrStdout()
	oldEnv, hadEnv := os.LookupEnv("NIMBUS_INSTALL_LOG_DIR")
	if err := os.Setenv("NIMBUS_INSTALL_LOG_DIR", log.dir); err != nil {
		log.finish(err)
		return err
	}
	defer func() {
		if hadEnv {
			_ = os.Setenv("NIMBUS_INSTALL_LOG_DIR", oldEnv)
		} else {
			_ = os.Unsetenv("NIMBUS_INSTALL_LOG_DIR")
		}
		opts.installLog = previousLog
		cmd.SetOut(oldOut)
		cmd.SetErr(oldErr)
		if _, err := fmt.Fprintf(oldOut, "Installation time: %s; logs: %s\n", time.Since(log.started).Round(time.Second), log.dir); err != nil {
			if errors.Is(retErr, reported{}) {
				retErr = err
			} else {
				retErr = errors.Join(retErr, err)
			}
		}
		if err := log.finish(retErr); err != nil {
			fmt.Fprintf(oldErr, "installation logging failed: %v\n", err)
			retErr = errors.Join(retErr, err)
		}
	}()
	src = installSource{src, log, oldErr}
	stageStarted := time.Now()
	steps := []runStep{{Name: "selection", Status: "skipped"}, {Name: "system installation", Status: "skipped"}, {Name: "dotfiles and tools", Status: "skipped"}}
	notes := &setupNoteWriter{out: oldOut}
	active := 0
	defer func() {
		if retErr != nil && steps[active].Status != "failed" {
			steps[active].Status = "failed"
			if !errors.Is(retErr, reported{}) {
				steps[active].Detail = retErr.Error()
			}
		}
		steps[active].DurationMS = time.Since(stageStarted).Milliseconds()
		log.event("stage end name=%s status=%s elapsed_ms=%d", steps[active].Name, steps[active].Status, steps[active].DurationMS)
		if err := errors.Join(renderRunSummary(out, "init", steps), notes.render(out)); err != nil {
			if errors.Is(retErr, reported{}) {
				retErr = err
			} else {
				retErr = errors.Join(retErr, err)
			}
		}
	}()
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
	existingSelector, selectorErr := selector.Load(selectorPath)
	if selectorErr != nil && !errors.Is(selectorErr, os.ErrNotExist) {
		return selectorErr
	}
	if existingSelector != nil && (existingSelector.Checkout != root || existingSelector.Origin != origin) {
		fmt.Fprintf(out, "Selector trust change:\n  previous: %s (%s)\n  requested: %s (%s)\n", existingSelector.Checkout, existingSelector.Origin, root, origin)
		return fmt.Errorf("selector trust change refused; %s is unchanged; use the existing trusted checkout, or inspect and explicitly update the selector's checkout and origin before retrying", selectorPath)
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
		description := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || unicode.IsSpace(r) {
				return ' '
			}
			return r
		}, describeHardware(hw.Value))
		header := fmt.Sprintf("# %s: %s.\n", m.ID, description)
		newManifest, err = renderManifest([]byte(header), m)
		if err != nil {
			return err
		}
		newManifestPath = path
		c.Machines[m.ID] = m
		c.Entries = append(c.Entries, definitions.Entry{Path: "machines/" + m.ID + ".toml", Mode: 0o100644, Content: append(bytes.Clone(newManifest), '\n')})
		slices.SortFunc(c.Entries, func(a, b definitions.Entry) int { return cmp.Compare(a.Path, b.Path) })
		if errs := definitions.Validate(c); len(errs) > 0 {
			return fmt.Errorf("the new manifest does not validate: %s", errs[0])
		}
		machine = m.ID
	}
	if machine == "" {
		if existingSelector != nil && existingSelector.Checkout == root {
			machine = existingSelector.Machine
			fmt.Fprintf(out, "the selector already names %s for this checkout\n", machine)
		}
	}
	if machine == "" {
		return usageError{fmt.Errorf("no machine selected; pass --machine ID (available: %s), or explicitly create one with --new ID", strings.Join(slices.Sorted(maps.Keys(c.Machines)), ", "))}
	}
	if f.onePasswordSSH && c.Machines[machine] != nil && c.Machines[machine].Dotfiles == nil {
		return usageError{errors.New("--onepassword-ssh requires a machine with dotfiles")}
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
	deferredTools := slices.ContainsFunc(r.Packages, func(pkg definitions.ResolvedPackage) bool { return pkg.Prefix == definitions.PrefixCargo }) ||
		slices.ContainsFunc(r.Installers, func(installer definitions.ResolvedInstaller) bool { return len(installer.Installer.Install) > 0 })
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
	latestOrigin, err := selector.CheckoutOrigin(root)
	if err != nil {
		return err
	}
	if normalized, err := selector.NormalizeOrigin(latestOrigin); err != nil || normalized != origin {
		return errors.New("checkout origin changed during initialization; run init again")
	}
	latestSelector, err := selector.Load(selectorPath)
	if existingSelector == nil {
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("selector changed during initialization; run init again")
		}
	} else if err != nil || latestSelector == nil || *latestSelector != *existingSelector {
		return errors.New("selector changed during initialization; run init again")
	}
	if newManifestPath != "" {
		if err := writeManifest(newManifestPath, newManifest); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "wrote %s; the Git change is yours to commit\n", newManifestPath); err != nil {
			return fmt.Errorf("report written manifest %s: %w", newManifestPath, err)
		}
	}
	if err := selector.Write(selectorPath, &selector.Selector{Schema: selector.CurrentSchema, Checkout: root, Machine: machine, Origin: origin}); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "selected %s; selector written to %s\n\n", machine, selectorPath); err != nil {
		return fmt.Errorf("report written selector %s: %w", selectorPath, err)
	}

	steps[0].Status = "succeeded"
	log.event("selection machine=%s profiles=%s definitions=%s", machine, strings.Join(r.Profiles, ","), c.Digest())
	m := c.Machines[machine]
	if m.Dotfiles != nil {
		if _, err := fmt.Fprintf(out, "Installation will initialize Chezmoi from %s if needed, then run chezmoi apply, including its declared user-tool installation scripts.\n", m.Dotfiles.Repo); err != nil {
			return err
		}
	} else {
		steps[2].Detail = "no dotfiles repository declared"
	}
	steps[0].DurationMS = time.Since(stageStarted).Milliseconds()
	log.event("stage end name=selection elapsed_ms=%d", steps[0].DurationMS)
	stageStarted = time.Now()
	active = 1
	flags := machineFlags{checkout: root, machine: machine}
	if err := runSyncWith(cmd, opts, flags, syncFlags{yes: true, deferUser: true}, lock); err != nil {
		for i := 2; i < len(steps); i++ {
			steps[i].Detail = "system installation did not complete"
		}
		return err
	}
	steps[1].Status = "succeeded"
	steps[1].DurationMS = time.Since(stageStarted).Milliseconds()
	log.event("stage end name=system-installation elapsed_ms=%d", steps[1].DurationMS)
	fresh, err = loadCheckout(root)
	if err != nil {
		return err
	}
	if fresh.Digest() != c.Digest() {
		return errors.New("definitions changed during initialization; run init again")
	}
	stageStarted = time.Now()
	active = 2
	if m.Dotfiles != nil {
		if err := chezmoiHandoff(src, notes, machine, r.Profiles, m.Dotfiles, f.onePasswordSSH); err != nil {
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
	steps[2].DurationMS = time.Since(stageStarted).Milliseconds()
	log.event("stage end name=dotfiles-and-tools elapsed_ms=%d", steps[2].DurationMS)
	stageStarted = time.Now()
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

// newMachineDialog asks for what a manifest needs beyond its ID: profiles,
// components with the detected hardware pre-selected, and the dotfiles
// repository with the one the other manifests share as the default.
func newMachineDialog(in io.Reader, out io.Writer, c *definitions.Checkout, hw facts.Hardware, f initFlags) (*definitions.Machine, error) {
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

// chezmoiHandoff initializes a missing source, then applies its local state.
// Chezmoi owns conflict handling and secrets; Nimbus never forces overwrites.
func chezmoiHandoff(src facts.Source, out io.Writer, machine string, profiles []string, dotfiles *definitions.Dotfiles, onePasswordSSH bool) error {
	if dotfiles == nil {
		_, err := fmt.Fprintln(out, "no dotfiles repository is declared; dotfiles skipped")
		return err
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
	initialized, err := facts.ChezmoiInitialized(src, home)
	if err != nil {
		return fmt.Errorf("inspect Chezmoi source before handoff: %w", err)
	}
	flags := []string{"--promptString", "Machine=" + machine, "--promptBool", "ManagedByNimbus=true", "--promptMultichoice", "Profiles=" + strings.Join(profiles, "/")}
	if !initialized {
		flags = append(flags, "--promptBool", fmt.Sprintf("Enable 1Password SSH integration=%t", onePasswordSSH))
		argv := append([]string{"init"}, append(flags, "--", dotfiles.Repo)...)
		if _, err := fmt.Fprintf(out, "-> initialize Chezmoi from %s\n   $ chezmoi %s\n", dotfiles.Repo, strings.Join(argv, " ")); err != nil {
			return err
		}
		if err := src.Stream(out, out, "chezmoi", argv...); err != nil {
			return fmt.Errorf("chezmoi init: %w", err)
		}
	} else {
		if _, err := fmt.Fprintln(out, "Chezmoi is already initialized; applying its existing local source"); err != nil {
			return err
		}
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
	if logged, ok := src.(installSource); ok {
		logged.log.checkoutIdentity(logged.Source, "dotfiles", root, actualOrigin)
	}
	data, err := src.Run("chezmoi", facts.ChezmoiDataArgs...)
	if err != nil {
		return fmt.Errorf("read Chezmoi selection: %w", err)
	}
	selection, err := facts.ParseChezmoiData(data)
	if err != nil {
		return fmt.Errorf("read Chezmoi selection: %w", err)
	}
	if selection.Machine != machine || !selection.ManagedByNimbus || !slices.Equal(slices.Sorted(slices.Values(selection.Profiles)), slices.Sorted(slices.Values(profiles))) {
		return fmt.Errorf("Chezmoi's stored selection differs; refresh it before retrying: %s", doctor.ChezmoiRefresh(machine, profiles, selection.OnePasswordSSH))
	}
	if onePasswordSSH && !selection.OnePasswordSSH {
		return fmt.Errorf("the existing Chezmoi configuration has 1Password SSH disabled; enable it explicitly with: %s", doctor.ChezmoiRefresh(machine, profiles, true))
	}
	if _, err := fmt.Fprintf(out, "Setup note: To change your Chezmoi answers, run: %s\n", doctor.ChezmoiRefresh(machine, profiles, selection.OnePasswordSSH)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "-> apply user configuration and install its declared tools\n   $ chezmoi apply"); err != nil {
		return err
	}
	if err := src.Stream(out, out, "chezmoi", "apply"); err != nil {
		return fmt.Errorf("chezmoi apply: %w", err)
	}
	return nil
}
