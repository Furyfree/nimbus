package cli

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/selector"
)

// DefaultCheckout is where the installer clones the repository.
const DefaultCheckout = "~/.local/share/nimbus"

type initFlags struct {
	checkout, machine, newMachine, dotfiles string
	noDotfiles, onePasswordSSH, plan, yes   bool
}

func newInit(opts *options) *cobra.Command {
	var f initFlags
	cmd := &cobra.Command{
		Use:   "init [--checkout DIR] [--machine ID | --new ID] [--plan] [-y]",
		Short: "Select the machine, write the selector, sync, apply dotfiles, and install user tools",
		Long: `Init installs the machine selected by --machine or an existing selector.
It validates the checkout, previews the selector and system changes, and asks
before applying them. When the manifest names a dotfiles repository, init
initializes Chezmoi and applies its configuration and selected user tools.
Sudo authentication and Chezmoi may still ask for input. Explicit --new opens
the new-machine dialogue. Existing checkout or origin trust cannot change in init.

  --checkout DIR   the Nimbus checkout (default ` + DefaultCheckout + `)
  --machine ID     a tracked machine; otherwise reuse the existing selector
  --new ID         describe a new machine and write its manifest
  --dotfiles URL   the dotfiles repository for a new machine
  --no-dotfiles    a new machine without a Chezmoi handoff
  -p, --plan       preview the setup without writing files or installing anything
  -y, --yes        apply the preview without asking for confirmation`,
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
	cmd.Flags().BoolVarP(&f.plan, "plan", "p", false, "preview the setup and change nothing")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "apply the preview without asking")
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
	if err := inspect.CheckPlatform(src, c.Definitions().Compatibility.Fedora); err != nil {
		return err
	}
	originalDigest := c.Digest()
	hw := inspect.Inspect(src, root).Hardware
	if hw.Known() {
		if _, err := fmt.Fprintf(out, "hardware: %s\n", describeHardware(hw.Value)); err != nil {
			return err
		}
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
		_, _ = fmt.Fprintf(out, "Selector trust change:\n  previous: %s (%s)\n  requested: %s (%s)\n", existingSelector.Checkout, existingSelector.Origin, root, origin)
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
			if _, err := fmt.Fprintf(out, "the selector already names %s for this checkout\n", machine); err != nil {
				return err
			}
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
	trial := &selected{Root: root, Checkout: c, Resolved: r}
	p, _, err := planWithState(trial, src, false)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Select %s in %s (checkout %s; origin %s).\n", machine, selectorPath, root, origin); err != nil {
		return err
	}
	if newManifestPath != "" {
		if _, err := fmt.Fprintf(out, "Create machine manifest:\n%s\n", unifiedDiff(newManifestPath, nil, append(bytes.Clone(newManifest), '\n'))); err != nil {
			return err
		}
	}
	m := c.Machines[machine]
	if m.Dotfiles != nil {
		if _, err := fmt.Fprintf(out, "Initialize Chezmoi from %s if needed, then run chezmoi apply, including its declared user-tool installation scripts.\n", m.Dotfiles.Repo); err != nil {
			return err
		}
	}
	if _, err := out.Write(renderPlan(p, false, false)); err != nil {
		return fmt.Errorf("show installation plan: %w", err)
	}
	if _, err := fmt.Fprintln(out, "\nFrom the local metadata cache. Applying creates installation logs and runs the setup shown above."); err != nil {
		return err
	}
	if !p.Complete {
		return errors.New("the installation plan is incomplete; if package metadata is missing, run dnf5 makecache, then retry init; resolve other blocked operations before applying")
	}
	if f.plan {
		return nil
	}
	if p.Checkout.Origin == "" || p.Checkout.Commit == "" {
		return errors.New("checkout origin and commit could not be inspected; init requires an inspectable Git clone")
	}
	if !f.yes && !approver(in, out, p.Digest) {
		return errors.New("not applied; the selector and manifests are unchanged")
	}
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	log, err := openInstallLog()
	if err != nil {
		return fmt.Errorf("start installation log: %w", err)
	}
	if _, err := fmt.Fprintf(out, "Installation logs: %s\n", log.dir); err != nil {
		return errors.Join(err, log.finish(err))
	}
	oldEnv, hadEnv := os.LookupEnv("NIMBUS_INSTALL_LOG_DIR")
	if err := os.Setenv("NIMBUS_INSTALL_LOG_DIR", log.dir); err != nil {
		return errors.Join(err, log.finish(err))
	}
	previousLog := opts.installLog
	opts.installLog = log
	oldOut, oldErr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	cmd.SetOut(installWriter{oldOut, log})
	cmd.SetErr(installWriter{oldErr, log})
	out = cmd.OutOrStdout()
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
			_, _ = fmt.Fprintf(oldErr, "installation logging failed: %v\n", err)
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
	lockPath, err := apply.LockPath()
	if err != nil {
		return err
	}
	lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: "init", PID: os.Getpid(), Started: time.Now().UTC()})
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
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
	freshPlan, _, err := planWithState(trial, src, false)
	if err != nil {
		return err
	}
	if !sameCheckoutIdentity(freshPlan.Checkout, p.Checkout) || freshPlan.Digest != p.Digest {
		return errors.New("checkout identity or system state changed during approval; run init again")
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
	if m.Dotfiles == nil {
		steps[2].Detail = "no dotfiles repository declared"
	}
	steps[0].DurationMS = time.Since(stageStarted).Milliseconds()
	log.event("stage end name=selection elapsed_ms=%d", steps[0].DurationMS)
	stageStarted = time.Now()
	active = 1
	flags := machineFlags{checkout: root, machine: machine}
	if err := runSyncWith(cmd, opts, flags, syncFlags{yes: true, definitionsDigest: c.Digest(), approvedDigest: p.Digest, approvedCheckout: &p.Checkout}, lock); err != nil {
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
			return err
		}
		steps[2].Status = "succeeded"
	}
	return nil
}
