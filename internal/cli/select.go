package cli

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/doctor"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
)

// The selection commands edit the two manifest lists a user would otherwise
// change by hand and then reuse the normal review and apply path: show the
// manifest diff and the complete plan, require approval, write the
// manifest atomically, apply, and leave the Git change to the user.

// pickerFn opens the interactive multi-select picker. Tests replace it.
var pickerFn = func(title string, items []pickItem) ([]string, error) {
	return runPicker(title, items)
}

type pickItem struct {
	ID       string
	Detail   string
	Selected bool
}

type selectionEdit struct {
	cmdName string
	summary string
	edit    func(m *definitions.Machine)
}

func newProfiles(opts *options) *cobra.Command {
	parent := newSelectionList(opts, "profiles", "List, add, or remove the selected machine's profiles", listProfiles)
	parent.AddCommand(newEditCommand(opts, "add [ID...]", "Add profiles to the selected machine", func(s *selected, ids []string) (*selectionEdit, error) {
		if len(ids) == 0 {
			var items []pickItem
			for _, id := range slices.Sorted(maps.Keys(s.Checkout.Profiles)) {
				items = append(items, pickItem{ID: id, Selected: slices.Contains(s.Resolved.Profiles, id)})
			}
			chosen, err := pickerFn("profiles to select", items)
			if err != nil {
				return nil, err
			}
			ids = chosen
		}
		for _, id := range ids {
			if _, ok := s.Checkout.Profiles[id]; !ok {
				return nil, fmt.Errorf("unknown profile %q", id)
			}
		}
		return &selectionEdit{cmdName: "profiles add", summary: "add profiles " + strings.Join(ids, ", "),
			edit: func(m *definitions.Machine) { m.Profiles = addUnique(m.Profiles, ids...) }}, nil
	}))
	parent.AddCommand(newEditCommand(opts, "remove [ID...]", "Remove profiles from the selected machine", func(s *selected, ids []string) (*selectionEdit, error) {
		if len(ids) == 0 {
			var items []pickItem
			for _, id := range s.Resolved.Profiles {
				if id != "common" {
					items = append(items, pickItem{ID: id})
				}
			}
			chosen, err := pickerFn("profiles to remove", items)
			if err != nil {
				return nil, err
			}
			ids = chosen
		}
		for _, id := range ids {
			if id == "common" {
				return nil, errors.New("common cannot be removed; every machine selects it")
			}
			if !slices.Contains(s.Resolved.Profiles, id) {
				return nil, fmt.Errorf("profile %q is not selected", id)
			}
		}
		return &selectionEdit{cmdName: "profiles remove", summary: "remove profiles " + strings.Join(ids, ", "),
			edit: func(m *definitions.Machine) { m.Profiles = removeAll(m.Profiles, ids...) }}, nil
	}))
	return parent
}

func newComponents(opts *options) *cobra.Command {
	parent := newSelectionList(opts, "components", "List, add, or remove the selected machine's explicit components", listComponents)
	parent.AddCommand(newEditCommand(opts, "add [ID...]", "Add explicit components to the selected machine", func(s *selected, ids []string) (*selectionEdit, error) {
		explicit := map[string]bool{}
		for _, c := range s.Checkout.Machines[s.Resolved.Machine].Components {
			explicit[c] = true
		}
		if len(ids) == 0 {
			var items []pickItem
			for _, id := range slices.Sorted(maps.Keys(s.Checkout.Components)) {
				items = append(items, pickItem{ID: id, Selected: explicit[id]})
			}
			chosen, err := pickerFn("components to select", items)
			if err != nil {
				return nil, err
			}
			ids = chosen
		}
		for _, id := range ids {
			if _, ok := s.Checkout.Components[id]; !ok {
				return nil, fmt.Errorf("unknown component %q", id)
			}
		}
		return &selectionEdit{cmdName: "components add", summary: "add components " + strings.Join(ids, ", "),
			edit: func(m *definitions.Machine) { m.Components = addUnique(m.Components, ids...) }}, nil
	}))
	parent.AddCommand(newEditCommand(opts, "remove [ID...]", "Remove explicit components from the selected machine", func(s *selected, ids []string) (*selectionEdit, error) {
		explicit := s.Checkout.Machines[s.Resolved.Machine].Components
		if len(ids) == 0 {
			var items []pickItem
			for _, id := range explicit {
				items = append(items, pickItem{ID: id})
			}
			chosen, err := pickerFn("components to remove", items)
			if err != nil {
				return nil, err
			}
			ids = chosen
		}
		for _, id := range ids {
			if !slices.Contains(explicit, id) {
				return nil, fmt.Errorf("component %q is not listed explicitly in the manifest; a component selected through a profile is removed by removing the profile", id)
			}
		}
		return &selectionEdit{cmdName: "components remove", summary: "remove components " + strings.Join(ids, ", "),
			edit: func(m *definitions.Machine) { m.Components = removeAll(m.Components, ids...) }}, nil
	}))
	return parent
}

func newPackages(opts *options) *cobra.Command {
	packages := newPackagesInstalled(opts)
	install := newEditCommand(opts, "install [QUERY]", "Add packages to the selected machine and apply", func(s *selected, args []string) (*selectionEdit, error) {
		query := ""
		if len(args) > 0 {
			query = args[0]
		}
		refs, err := pickAvailable(s, query)
		if err != nil {
			return nil, err
		}
		return &selectionEdit{cmdName: "packages install", summary: "install " + strings.Join(refs, ", "),
			edit: func(m *definitions.Machine) { m.Packages = addUnique(m.Packages, refs...) }}, nil
	})
	remove := newEditCommand(opts, "remove [QUERY]", "Remove desired packages from the selected machine and apply", func(s *selected, args []string) (*selectionEdit, error) {
		query := ""
		if len(args) > 0 {
			query = args[0]
		}
		m := s.Checkout.Machines[s.Resolved.Machine]
		var items []pickItem
		for _, ref := range m.Packages {
			if strings.Contains(ref, query) {
				items = append(items, pickItem{ID: ref, Detail: "machine package"})
			}
		}
		for _, p := range s.Resolved.Packages {
			if !slices.Contains(m.Packages, p.Canonical) && strings.Contains(p.Name, query) && !slices.Contains(m.Packages, p.Name) {
				items = append(items, pickItem{ID: p.Canonical, Detail: "selected by " + strings.Join(p.Paths, ", ") + "; removing adds an exclusion"})
			}
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("no desired package matches %q", query)
		}
		chosen, err := pickerFn("packages to remove", items)
		if err != nil {
			return nil, err
		}
		if len(chosen) == 0 {
			return nil, errors.New("nothing selected")
		}
		return &selectionEdit{cmdName: "packages remove", summary: "remove " + strings.Join(chosen, ", "),
			edit: func(m *definitions.Machine) {
				var exclusions []string
				for _, c := range chosen {
					if slices.Contains(m.Packages, c) || slices.Contains(m.Packages, strings.TrimPrefix(c, "dnf:")) {
						m.Packages = removeAll(m.Packages, c, strings.TrimPrefix(c, "dnf:"))
					} else {
						exclusions = append(exclusions, c)
					}
				}
				m.PackageExclusions = addUnique(m.PackageExclusions, exclusions...)
			}}, nil
	})
	for _, cmd := range []*cobra.Command{install, remove} {
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		}
	}
	packages.AddCommand(install, remove)
	return packages
}

// pickAvailable offers the packages DNF's cache knows that match the query.
func pickAvailable(s *selected, query string) ([]string, error) {
	if strings.TrimSpace(query) == "" {
		return nil, usageError{errors.New("packages install needs a query to search the package cache")}
	}
	out, err := newSource().Run("dnf5", "-q", "--cacheonly", "repoquery", "--available", "--qf", "%{name}|%{evr}|%{reponame}\n", "*"+query+"*")
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("search the package cache: %w (nimbus sync refreshes it)", err)
	}
	seen := map[string]bool{}
	var items []pickItem
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, "|")
		if len(fields) != 3 || seen[fields[0]] {
			continue
		}
		seen[fields[0]] = true
		ref := fields[0]
		if prefix := prefixForRepo(s.Checkout.Definitions(), fields[2]); prefix != "" {
			ref = prefix + ":" + fields[0]
		}
		items = append(items, pickItem{ID: ref, Detail: fields[1] + " (" + fields[2] + ")"})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no available package matches %q", query)
	}
	slices.SortFunc(items, func(a, b pickItem) int { return cmp.Compare(a.ID, b.ID) })
	chosen, err := pickerFn("packages to install", items)
	if err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		return nil, errors.New("nothing selected")
	}
	for _, c := range chosen {
		if _, err := definitions.ParseRef(c); err != nil {
			return nil, err
		}
	}
	return chosen, nil
}

// prefixForRepo maps a host repository ID back to the declared prefix, or
// "" for Fedora.
func prefixForRepo(root definitions.Root, repoID string) string {
	for id, r := range root.Repositories {
		if slices.Contains(plan.DNFRepoIDs(id, r), repoID) {
			return id
		}
	}
	return ""
}

// newEditCommand wraps one manifest edit in the shared flow.
func newEditCommand(opts *options, use, short string, build func(*selected, []string) (*selectionEdit, error)) *cobra.Command {
	var flags machineFlags
	var yes bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSelected(flags)
			if err != nil {
				return err
			}
			edit, err := build(s, args)
			if err != nil {
				return err
			}
			return runEdit(cmd, opts, flags, s, edit, yes)
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "write the manifest and apply without asking")
	return cmd
}

// runEdit shows the manifest diff and the plan for the edited manifest,
// asks for approval, writes the manifest, and applies.
func runEdit(cmd *cobra.Command, opts *options, flags machineFlags, s *selected, edit *selectionEdit, yes bool) error {
	out := cmd.OutOrStdout()
	path := manifestPath(s.Root, s.Resolved.Machine)
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	original := s.Checkout.Machines[s.Resolved.Machine]
	edited := *original
	edited.Profiles = slices.Clone(original.Profiles)
	edited.Components = slices.Clone(original.Components)
	edited.Packages = slices.Clone(original.Packages)
	edited.PackageExclusions = slices.Clone(original.PackageExclusions)
	edit.edit(&edited)
	after := renderManifest(before, &edited)
	if string(after) == strings.TrimRight(string(before), "\n") {
		if opts.json {
			return writeJSON(out, map[string]string{"manifest": "unchanged"}, nil)
		}
		_, err := fmt.Fprintf(out, "%s: the manifest already says that\n", edit.cmdName)
		return err
	}

	// Validate and plan the edited manifest in memory before touching it.
	// The trial checkout also carries the edited bytes so its definition
	// digest, and therefore the plan digest, is the one the written
	// manifest will produce.
	trialCheckout := *s.Checkout
	trialCheckout.Machines = maps.Clone(s.Checkout.Machines)
	trialCheckout.Machines[s.Resolved.Machine] = &edited
	trialCheckout.Entries = slices.Clone(s.Checkout.Entries)
	rel := "machines/" + s.Resolved.Machine + ".toml"
	for i := range trialCheckout.Entries {
		if trialCheckout.Entries[i].Path == rel {
			trialCheckout.Entries[i].Content = append(after, '\n')
		}
	}
	if errs := definitions.Validate(&trialCheckout); len(errs) > 0 {
		return fmt.Errorf("the edited manifest is invalid: %s", errs[0])
	}
	r, rerrs := definitions.Resolve(&trialCheckout, s.Resolved.Machine)
	if len(rerrs) > 0 {
		return fmt.Errorf("the edited manifest does not resolve: %s", rerrs[0])
	}
	trial := &selected{Root: s.Root, Checkout: &trialCheckout, Resolved: r}
	src := newSource()
	if err := facts.CheckPlatform(src, s.Checkout.Definitions().Compatibility.Fedora); err != nil {
		return err
	}
	if _, err := src.Run("dnf5", "makecache"); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "metadata not refreshed: %v; using cached metadata\n", err)
	}
	p, _, err := planWithState(trial, src, false)
	if err != nil {
		return err
	}
	if p.Checkout.Origin == "" || p.Checkout.Commit == "" {
		return errors.New("checkout origin and commit could not be inspected; selection changes require an inspectable Git clone")
	}
	// In JSON mode the review text goes to stderr and the envelope from the
	// sync that follows is the only thing on stdout; JSON asks nothing.
	review := out
	if opts.json {
		review, yes = cmd.ErrOrStderr(), true
	}
	if _, err := fmt.Fprintf(review, "%s: %s\n\n%s\n", edit.cmdName, edit.summary, unifiedDiff(path, before, append(after, '\n'))); err != nil {
		return fmt.Errorf("write selection diff: %w", err)
	}
	if _, err := review.Write(renderPlan(p, false, false)); err != nil {
		return fmt.Errorf("write selection plan: %w", err)
	}
	if !p.Complete {
		return errors.New("the plan for the edited manifest is incomplete; resolve the blocked operations first")
	}
	if !yes {
		if _, err := fmt.Fprintln(review, "proceeding writes the manifest change shown and then applies the plan"); err != nil {
			return fmt.Errorf("write selection approval notice: %w", err)
		}
		if !approver(cmd.InOrStdin(), out, p.Digest) {
			return errors.New("not applied; the manifest is unchanged")
		}
	}
	// The lock covers the manifest write and the apply that follows, so a
	// concurrent edit cannot slip in between.
	lockPath, err := apply.LockPath()
	if err != nil {
		return err
	}
	lock, err := apply.Acquire(lockPath, apply.LockInfo{Command: edit.cmdName, Operation: p.Digest, PID: os.Getpid(), Started: time.Now().UTC()})
	if err != nil {
		return err
	}
	defer lock.Release()
	fresh, err := loadSelected(flags)
	if err != nil {
		return err
	}
	if fresh.Root != s.Root || fresh.Resolved.Machine != s.Resolved.Machine || fresh.Checkout.Digest() != s.Checkout.Digest() {
		return errors.New("definitions or selection changed while the plan was being reviewed; run the command again")
	}
	freshPlan, _, err := planWithState(trial, src, false)
	if err != nil {
		return err
	}
	if !sameCheckoutIdentity(freshPlan.Checkout, p.Checkout) {
		return errors.New("checkout identity changed while the plan was being reviewed; run the command again")
	}
	if freshPlan.Digest != p.Digest {
		return errors.New("the system changed while the plan was being reviewed; run the command again")
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(before) {
		lock.Release()
		return errors.New("the manifest changed while the plan was being reviewed; run the command again")
	}
	if err := writeManifest(path, after); err != nil {
		lock.Release()
		return err
	}
	if _, err := fmt.Fprintf(review, "wrote %s; the Git change is yours to commit\n", path); err != nil {
		return err
	}
	if strings.HasPrefix(edit.cmdName, "profiles ") && edited.Dotfiles != nil {
		selection := facts.Inspect(src, "").Chezmoi
		if selection.Known() && selection.Value.Initialized {
			_, err = fmt.Fprintf(review, "Chezmoi keeps its own copy of the profiles; refresh it with:\n  %s\n", doctor.ChezmoiRefresh(edited.ID, r.Profiles, selection.Value.OnePasswordSSH))
		} else {
			_, err = fmt.Fprintln(review, "Chezmoi selection is unavailable; run nimbus doctor after initializing Chezmoi to obtain the refresh command.")
		}
		if err != nil {
			return err
		}
	}
	if nothingToRun(p) {
		lock.Release()
		if opts.json {
			return writeJSON(out, syncResult{Digest: p.Digest, Executed: []string{}, Differences: []string{}}, nil)
		}
		return nil
	}
	flags.machine = s.Resolved.Machine
	// The edit is the request; it runs without system updates, which are
	// a plain sync's job.
	return runSyncWith(cmd, opts, flags, syncFlags{yes: true, noUpgrade: true, approvedDigest: p.Digest, approvedCheckout: &p.Checkout}, lock)
}

func listProfiles(s *selected) []selectionView {
	selectedIDs := map[string]bool{}
	for _, p := range s.Resolved.Profiles {
		selectedIDs[p] = true
	}
	var views []selectionView
	for _, id := range slices.Sorted(maps.Keys(s.Checkout.Profiles)) {
		v := selectionView{ID: id, Selected: selectedIDs[id]}
		if v.Selected {
			v.Paths = []string{"machine"}
		}
		views = append(views, v)
	}
	return views
}

func listComponents(s *selected) []selectionView {
	paths := map[string][]string{}
	for _, c := range s.Resolved.Components {
		paths[c.ID] = c.Paths
	}
	var views []selectionView
	for _, id := range slices.Sorted(maps.Keys(s.Checkout.Components)) {
		p, ok := paths[id]
		views = append(views, selectionView{ID: id, Selected: ok, Paths: p})
	}
	return views
}
