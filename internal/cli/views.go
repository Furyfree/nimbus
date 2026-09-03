package cli

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
)

// Ownership views are provider-independent read-only lists over the same
// resolver and facts the plan uses. Until receipts exist, "managed" means
// desired and installed, which apply would adopt.

type packageView struct {
	Canonical  string   `json:"canonical"`
	Name       string   `json:"name"`
	State      string   `json:"state"` // desired, managed, unmanaged, dependency, excluded
	Installed  string   `json:"installed,omitempty"`
	Repository string   `json:"repository,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Paths      []string `json:"paths,omitempty"`
}

// packageViews joins desired packages with installed ones.
func packageViews(s *selected, f *facts.Facts) []packageView {
	installed := map[string]facts.Package{}
	if f.Packages.Known() {
		for _, p := range f.Packages.Value {
			installed[p.Name] = p
		}
	}
	desired := map[string]bool{}
	var views []packageView
	for _, p := range s.Resolved.Packages {
		desired[p.Name] = true
		v := packageView{Canonical: p.Canonical, Name: p.Name, State: "desired", Paths: p.Paths}
		if p.Prefix == definitions.PrefixFlatpak {
			if f.Flatpak.Known() {
				for _, app := range f.Flatpak.Value.Apps {
					if app.ID == p.Name {
						v.State, v.Installed, v.Repository = "managed", app.Version, app.Origin
					}
				}
			}
		} else if inst, ok := installed[p.Name]; ok {
			v.State, v.Installed, v.Repository, v.Reason = "managed", inst.EVR(), inst.FromRepo, inst.Reason
		}
		views = append(views, v)
	}
	for _, p := range f.Packages.Value {
		if desired[p.Name] {
			continue
		}
		state := "dependency"
		if p.Reason == "user" {
			state = "unmanaged"
		}
		views = append(views, packageView{Canonical: "dnf:" + p.Name, Name: p.Name, State: state, Installed: p.EVR(), Repository: p.FromRepo, Reason: p.Reason})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Canonical < views[j].Canonical })
	return views
}

func renderPackageViews(views []packageView) []byte {
	var b bytes.Buffer
	for _, v := range views {
		line := fmt.Sprintf("%-10s %s", v.State, v.Canonical)
		if v.Installed != "" {
			line += " " + v.Installed
			if v.Repository != "" {
				line += " (" + v.Repository + ")"
			}
		}
		if len(v.Paths) > 0 {
			line += "  <- " + strings.Join(v.Paths, ", ")
		}
		b.WriteString(line + "\n")
	}
	if len(views) == 0 {
		b.WriteString("none\n")
	}
	return b.Bytes()
}

func filterViews(views []packageView, keep func(packageView) bool) []packageView {
	out := []packageView{}
	for _, v := range views {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

func newListCommand(opts *options, use, short string, keep func(query string, v packageView) bool, maxArgs int) *cobra.Command {
	var flags machineFlags
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > maxArgs {
				return usageError{fmt.Errorf("at most %d argument(s)", maxArgs)}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSelected(flags)
			if err != nil {
				return err
			}
			f := facts.Inspect(newSource(), s.Root)
			if !f.Packages.Known() {
				return fmt.Errorf("installed packages are unknown: %s", f.Packages.Error)
			}
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			views := filterViews(packageViews(s, f), func(v packageView) bool { return keep(query, v) })
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), views, nil)
			}
			_, err = cmd.OutOrStdout().Write(renderPackageViews(views))
			return err
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	return cmd
}

func newManaged(opts *options) *cobra.Command {
	return newListCommand(opts, "managed", "List resources Nimbus owns or would adopt",
		func(_ string, v packageView) bool { return v.State == "managed" }, 0)
}

func newUnmanaged(opts *options) *cobra.Command {
	return newListCommand(opts, "unmanaged", "List installed packages Nimbus can identify but does not own",
		func(_ string, v packageView) bool { return v.State == "unmanaged" }, 0)
}

func newPackagesInstalled(opts *options) *cobra.Command {
	list := newListCommand(opts, "installed [QUERY]", "Browse explicitly installed packages with their desired and managed state",
		func(query string, v packageView) bool {
			return v.Installed != "" && v.State != "dependency" && strings.Contains(v.Name, query)
		}, 1)
	packages := &cobra.Command{Use: "packages", Short: "Package views over the selected machine", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
	packages.AddCommand(list)
	return packages
}

func newWhy(opts *options) *cobra.Command {
	var flags machineFlags
	cmd := &cobra.Command{
		Use:   "why RESOURCE",
		Short: "Explain every path that selects a package, component, or profile",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageError{fmt.Errorf("why takes exactly one resource")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSelected(flags)
			if err != nil {
				return err
			}
			result, err := explain(s, args[0])
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), result, nil)
			}
			var b bytes.Buffer
			fmt.Fprintf(&b, "%s %s\n", result.Kind, result.ID)
			for _, p := range result.Paths {
				fmt.Fprintf(&b, "  selected by %s\n", p)
			}
			_, err = cmd.OutOrStdout().Write(b.Bytes())
			return err
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	return cmd
}

type whyResult struct {
	Kind  string   `json:"kind"`
	ID    string   `json:"id"`
	Paths []string `json:"paths"`
}

func explain(s *selected, resource string) (*whyResult, error) {
	r := s.Resolved
	for _, p := range r.Profiles {
		if p == resource {
			return &whyResult{Kind: "profile", ID: p, Paths: []string{"machine"}}, nil
		}
	}
	for _, c := range r.Components {
		if c.ID == resource {
			return &whyResult{Kind: "component", ID: c.ID, Paths: c.Paths}, nil
		}
	}
	ref, err := definitions.ParseRef(resource)
	if err == nil {
		for _, p := range r.Packages {
			if p.Canonical == ref.Canonical() || p.Name == resource {
				return &whyResult{Kind: "package", ID: p.Canonical, Paths: p.Paths}, nil
			}
		}
	}
	return nil, fmt.Errorf("%q is not selected for machine %s", resource, r.Machine)
}

type selectionView struct {
	ID       string   `json:"id"`
	Selected bool     `json:"selected"`
	Paths    []string `json:"paths,omitempty"`
}

func newSelectionList(opts *options, use, short string, list func(*selected) []selectionView) *cobra.Command {
	var flags machineFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: short,
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSelected(flags)
			if err != nil {
				return err
			}
			views := list(s)
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), views, nil)
			}
			var b bytes.Buffer
			for _, v := range views {
				mark := " "
				if v.Selected {
					mark = "*"
				}
				line := fmt.Sprintf("%s %s", mark, v.ID)
				if len(v.Paths) > 0 {
					line += "  <- " + strings.Join(v.Paths, ", ")
				}
				b.WriteString(line + "\n")
			}
			_, err = cmd.OutOrStdout().Write(b.Bytes())
			return err
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	parent := &cobra.Command{Use: use, Short: short, Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
	parent.AddCommand(cmd)
	return parent
}

func newProfiles(opts *options) *cobra.Command {
	return newSelectionList(opts, "profiles", "List every profile and whether the selected machine uses it", func(s *selected) []selectionView {
		selected := map[string]bool{}
		for _, p := range s.Resolved.Profiles {
			selected[p] = true
		}
		var views []selectionView
		for _, id := range sortedIDs(len(s.Checkout.Profiles), func() []string {
			ids := make([]string, 0, len(s.Checkout.Profiles))
			for id := range s.Checkout.Profiles {
				ids = append(ids, id)
			}
			return ids
		}) {
			v := selectionView{ID: id, Selected: selected[id]}
			if v.Selected {
				v.Paths = []string{"machine"}
			}
			views = append(views, v)
		}
		return views
	})
}

func newComponents(opts *options) *cobra.Command {
	return newSelectionList(opts, "components", "List every component and how the selected machine selects it", func(s *selected) []selectionView {
		paths := map[string][]string{}
		for _, c := range s.Resolved.Components {
			paths[c.ID] = c.Paths
		}
		var views []selectionView
		for _, id := range sortedIDs(len(s.Checkout.Components), func() []string {
			ids := make([]string, 0, len(s.Checkout.Components))
			for id := range s.Checkout.Components {
				ids = append(ids, id)
			}
			return ids
		}) {
			p, ok := paths[id]
			views = append(views, selectionView{ID: id, Selected: ok, Paths: p})
		}
		return views
	})
}

func sortedIDs(_ int, collect func() []string) []string {
	ids := collect()
	sort.Strings(ids)
	return ids
}
