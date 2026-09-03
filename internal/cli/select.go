package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/apply"
	"github.com/Furyfree/nimbus/internal/definitions"
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
			for _, id := range sortedKeysOf(s.Checkout.Profiles) {
				items = append(items, pickItem{ID: id, Selected: contains(s.Resolved.Profiles, id)})
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
			if !contains(s.Resolved.Profiles, id) {
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
			for _, id := range sortedKeysOf(s.Checkout.Components) {
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
			if !contains(explicit, id) {
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
	packages.AddCommand(newEditCommand(opts, "install [QUERY]", "Add packages to the selected machine and apply", func(s *selected, args []string) (*selectionEdit, error) {
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
	}))
	packages.AddCommand(newEditCommand(opts, "remove [QUERY]", "Remove desired packages from the selected machine and apply", func(s *selected, args []string) (*selectionEdit, error) {
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
			if !contains(m.Packages, p.Canonical) && strings.Contains(p.Name, query) && !contains(m.Packages, p.Name) {
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
					if contains(m.Packages, c) || contains(m.Packages, strings.TrimPrefix(c, "dnf:")) {
						m.Packages = removeAll(m.Packages, c, strings.TrimPrefix(c, "dnf:"))
					} else {
						exclusions = append(exclusions, c)
					}
				}
				m.PackageExclusions = addUnique(m.PackageExclusions, exclusions...)
			}}, nil
	}))
	return packages
}

// pickAvailable offers the packages DNF's cache knows that match the query.
func pickAvailable(s *selected, query string) ([]string, error) {
	if strings.TrimSpace(query) == "" {
		return nil, usageError{errors.New("packages install needs a query to search the package cache")}
	}
	out, err := newSource().Run("dnf5", "-q", "--cacheonly", "repoquery", "--available", "--qf", "%{name}|%{evr}|%{reponame}\n", "*"+query+"*")
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("search the package cache: %w (run nimbus refresh if it is empty)", err)
	}
	seen := map[string]bool{}
	var items []pickItem
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
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
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
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
		for _, host := range dnfRepoIDs(id, r) {
			if host == repoID {
				return id
			}
		}
	}
	return ""
}

func dnfRepoIDs(id string, r definitions.Repository) []string {
	switch r.Kind {
	case "dnf":
		if r.ReleasePackage != "" {
			return []string{id, id + "-updates"}
		}
		return []string{"nimbus-" + id}
	case "copr":
		owner, project, _ := strings.Cut(r.Project, "/")
		return []string{"copr:copr.fedorainfracloud.org:" + owner + ":" + project}
	}
	return nil
}

// newEditCommand wraps one manifest edit in the shared flow.
func newEditCommand(opts *options, use, short string, build func(*selected, []string) (*selectionEdit, error)) *cobra.Command {
	var flags machineFlags
	var approve string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.json && approve == "" {
				return usageError{errors.New("--json needs --approve DIGEST; JSON output cannot answer the approval prompt")}
			}
			s, err := loadSelected(flags)
			if err != nil {
				return err
			}
			edit, err := build(s, args)
			if err != nil {
				return err
			}
			return runEdit(cmd, opts, flags, s, edit, approve)
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	cmd.Flags().StringVar(&approve, "approve", "", "approve exactly this plan digest without a prompt")
	return cmd
}

// runEdit shows the manifest diff and the plan for the edited manifest,
// asks for approval, writes the manifest, and applies.
func runEdit(cmd *cobra.Command, opts *options, flags machineFlags, s *selected, edit *selectionEdit, approve string) error {
	out := cmd.OutOrStdout()
	path := manifestPath(s.Root, s.Resolved.Machine)
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	original := s.Checkout.Machines[s.Resolved.Machine]
	edited := *original
	edited.Profiles = append([]string(nil), original.Profiles...)
	edited.Components = append([]string(nil), original.Components...)
	edited.Packages = append([]string(nil), original.Packages...)
	edited.PackageExclusions = append([]string(nil), original.PackageExclusions...)
	edit.edit(&edited)
	after := renderManifest(before, &edited)
	if string(after) == strings.TrimRight(string(before), "\n") {
		fmt.Fprintf(out, "%s: the manifest already says that\n", edit.cmdName)
		return nil
	}

	// Validate and plan the edited manifest in memory before touching it.
	// The trial checkout also carries the edited bytes so its definition
	// digest, and therefore the plan digest, is the one the written
	// manifest will produce.
	trialCheckout := *s.Checkout
	trialCheckout.Machines = map[string]*definitions.Machine{}
	for id, m := range s.Checkout.Machines {
		trialCheckout.Machines[id] = m
	}
	trialCheckout.Machines[s.Resolved.Machine] = &edited
	trialCheckout.Entries = append([]definitions.Entry(nil), s.Checkout.Entries...)
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
	p, _, err := planWithState(trial, src, false)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s: %s\n\n%s\n", edit.cmdName, edit.summary, unifiedDiff(path, before, append(after, '\n')))
	out.Write(renderPlan(p, false))
	if !p.Complete {
		return errors.New("the plan for the edited manifest is incomplete; resolve the blocked operations first")
	}
	switch {
	case approve != "" && approve == p.Digest:
	case approve != "":
		return fmt.Errorf("--approve %s does not match the plan %s", approve, p.Digest)
	default:
		if !approver(cmd.InOrStdin(), out, p.Digest) {
			return errors.New("not approved; the manifest is unchanged")
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
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(before) {
		lock.Release()
		return errors.New("the manifest changed while the plan was being reviewed; run the command again")
	}
	if err := writeManifest(path, after); err != nil {
		lock.Release()
		return err
	}
	fmt.Fprintf(out, "wrote %s; the Git change is yours to commit\n", path)
	if nothingToRun(p) {
		lock.Release()
		return nil
	}
	flags.machine = s.Resolved.Machine
	return runApplyWith(cmd, opts, flags, false, p.Digest, lock)
}

func listProfiles(s *selected) []selectionView {
	selectedIDs := map[string]bool{}
	for _, p := range s.Resolved.Profiles {
		selectedIDs[p] = true
	}
	var views []selectionView
	for _, id := range sortedKeysOf(s.Checkout.Profiles) {
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
	for _, id := range sortedKeysOf(s.Checkout.Components) {
		p, ok := paths[id]
		views = append(views, selectionView{ID: id, Selected: ok, Paths: p})
	}
	return views
}

func sortedKeysOf[V any](m map[string]V) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
