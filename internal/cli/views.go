package cli

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func flatpakRemote(root definitions.Root) string {
	for id, r := range root.Repositories {
		if r.Kind == "flatpak" {
			return id
		}
	}
	return ""
}

// Ownership views are provider-independent read-only lists over the same
// resolver, facts, and applied state the plan uses. Every package is in one
// state: managed (a receipt exists), adopt (desired and installed from an
// acceptable source, no receipt yet), blocked (desired and installed from
// another source, which plan refuses to adopt), desired (not installed),
// pre-existing (in the baseline recorded when Nimbus took over), unmanaged
// (installed by hand since), or dependency.

type packageView struct {
	Canonical  string   `json:"canonical"`
	Name       string   `json:"name"`
	State      string   `json:"state"`
	Installed  string   `json:"installed,omitempty"`
	Repository string   `json:"repository,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Paths      []string `json:"paths,omitempty"`
}

// packageViews joins desired packages with installed ones and receipts.
func packageViews(s *selected, f *facts.Facts, applied *state.Applied) []packageView {
	desired := map[string]bool{}
	var views []packageView
	for _, p := range s.Resolved.Packages {
		desired[p.Name] = true
		v := packageView{Canonical: p.Canonical, Name: p.Name, State: "desired", Paths: p.Paths}
		id := "package:" + p.Canonical
		remote := flatpakRemote(s.Checkout.Definitions())
		if p.Prefix == definitions.PrefixFlatpak {
			id = "flatpak:" + p.Name
			if f.Flatpak.Known() {
				for _, app := range f.Flatpak.Value.Apps {
					if app.ID == p.Name {
						v.Installed, v.Repository = app.Version, app.Origin
						v.State = "adopt"
						if app.Origin != remote {
							v.State = "blocked"
						}
					}
				}
			}
		} else if p.Prefix == definitions.PrefixCargo {
			if f.User.Known() && contains(f.User.Value.Crates, p.Name) {
				v.Installed, v.Repository, v.State = "installed", "cargo", "adopt"
			}
		} else if inst, ok := plan.InstalledPackage(p.Name, applied.Receipts[id], f.Packages.Value); ok {
			desired[inst.ID()] = true
			v.Installed, v.Repository, v.Reason = inst.EVR(), inst.FromRepo, inst.Reason
			v.State = "adopt"
		}
		if _, ok := applied.Receipts[id]; ok && v.Installed != "" {
			v.State = "managed"
		}
		views = append(views, v)
	}
	for _, p := range f.Packages.Value {
		if desired[p.ID()] {
			continue
		}
		st := "dependency"
		switch {
		case applied.InBaseline(p.ID()) || applied.InBaseline(p.Name):
			st = "pre-existing"
		case plan.IsReleasePackage(s.Checkout.Definitions(), p.Name):
			st = "managed" // installed by Nimbus while enabling its repository
		case p.Reason != "user":
		default:
			st = "unmanaged"
		}
		views = append(views, packageView{Canonical: "dnf:" + p.ID(), Name: p.ID(), State: st, Installed: p.EVR(), Repository: p.FromRepo, Reason: p.Reason})
	}
	if f.Flatpak.Known() {
		for _, app := range f.Flatpak.Value.Apps {
			if desired[app.ID] {
				continue
			}
			st := "unmanaged"
			if _, ok := applied.Receipts["flatpak:"+app.ID]; ok {
				st = "managed"
			}
			views = append(views, packageView{Canonical: "flatpak:" + app.ID, Name: app.ID, State: st, Installed: app.Version, Repository: app.Origin})
		}
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

func newListCommand(opts *options, use, short string, keep func(query string, all bool, v packageView) bool, maxArgs int, allFlag string) *cobra.Command {
	var flags machineFlags
	var all bool
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
			applied, err := state.Read(stateRoot)
			if err != nil {
				return err
			}
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			views := filterViews(packageViews(s, f, applied), func(v packageView) bool { return keep(query, all, v) })
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), views, nil)
			}
			_, err = cmd.OutOrStdout().Write(renderPackageViews(views))
			return err
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	if allFlag != "" {
		cmd.Flags().BoolVar(&all, "all", false, allFlag)
	}
	return cmd
}

func newManaged(opts *options) *cobra.Command {
	return newListCommand(opts, "managed", "List packages Nimbus owns through a receipt or would adopt",
		func(_ string, _ bool, v packageView) bool { return v.State == "managed" || v.State == "adopt" }, 0, "")
}

func newUnmanaged(opts *options) *cobra.Command {
	return newListCommand(opts, "unmanaged", "List packages installed by hand since Nimbus took over",
		func(_ string, all bool, v packageView) bool {
			return v.State == "unmanaged" || (all && v.State == "pre-existing")
		}, 0,
		"also list the pre-existing packages recorded when Nimbus took over")
}

func newPackagesInstalled(opts *options) *cobra.Command {
	list := newListCommand(opts, "installed [QUERY]", "Browse explicitly installed packages with their desired and managed state",
		func(query string, _ bool, v packageView) bool {
			return v.Installed != "" && v.State != "dependency" && strings.Contains(v.Name, query)
		}, 1, "")
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
