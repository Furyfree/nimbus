package cli

import (
	"bytes"
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

type packageView struct {
	Canonical  string   `json:"canonical"`
	Name       string   `json:"name"`
	State      string   `json:"state"`
	Installed  string   `json:"installed,omitempty"`
	Repository string   `json:"repository,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Blocked    string   `json:"blocked,omitempty"`
	Paths      []string `json:"paths,omitempty"`
}

// packageViews joins desired packages with installed ones and receipts.
func packageViews(s *selected, f *inspect.Facts, applied *state.Applied) []packageView {
	managedRPMs := map[string]bool{}
	blockedRPMs := map[string]string{}
	blockedReceipts := map[string]string{}
	for _, id := range slices.Sorted(maps.Keys(applied.Receipts)) {
		r := applied.Receipts[id]
		if r.Provider != "dnf" || !r.Verified {
			continue
		}
		r.Resource = id
		native, err := plan.ReceiptPackage(r, f.Packages.Value)
		if err == nil {
			managedRPMs[native] = true
			continue
		}
		blockedReceipts[id] = err.Error()
		for _, p := range f.Packages.Value {
			if p.Matches(plan.PackageName(id)) {
				blockedRPMs[p.ID()] = err.Error()
			}
		}
	}
	desired := map[string]bool{}
	var views []packageView
	for _, p := range s.Resolved.Packages {
		desired[p.Name] = true
		v := packageView{Canonical: p.Canonical, Name: p.Name, State: "desired", Paths: p.Paths}
		id := "package:" + p.Canonical
		if p.Prefix == definitions.PrefixFlatpak {
			id = "flatpak:" + p.Name
			if f.Flatpak.Known() {
				for _, app := range f.Flatpak.Value.Apps {
					if app.ID == p.Name {
						v.Installed, v.Repository = app.Version, app.Origin
						v.State = "adopt"
					}
				}
			}
		} else if inst, ok := plan.InstalledPackage(p.Name, applied.Receipts[id], f.Packages.Value); ok {
			desired[inst.ID()] = true
			v.Installed, v.Repository, v.Reason = inst.EVR(), inst.FromRepo, inst.Reason
			v.State = "adopt"
			if managedRPMs[inst.ID()] {
				v.State = "managed"
			}
			if reason := blockedReceipts[id]; reason != "" {
				v.State, v.Blocked = "blocked", reason
			}
		}
		if p.Prefix == definitions.PrefixFlatpak {
			if _, ok := applied.Receipts[id]; ok && v.State == "adopt" {
				v.State = "managed"
			}
		}
		views = append(views, v)
	}
	for _, p := range f.Packages.Value {
		if desired[p.ID()] {
			continue
		}
		st := "dependency"
		blocked := ""
		switch {
		case managedRPMs[p.ID()]:
			st = "managed"
		case blockedRPMs[p.ID()] != "":
			st, blocked = "blocked", blockedRPMs[p.ID()]
		case applied.InBaseline(p.ID()) || applied.InBaseline(p.Name):
			st = "pre-existing"
		case plan.IsReleasePackage(s.Checkout.Definitions(), p.Name):
			st = "managed" // installed by Nimbus while enabling its repository
		case p.Reason != "user":
		default:
			st = "unmanaged"
		}
		views = append(views, packageView{Canonical: "dnf:" + p.ID(), Name: p.ID(), State: st, Installed: p.EVR(), Repository: p.FromRepo, Reason: p.Reason, Blocked: blocked})
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
	slices.SortFunc(views, func(a, b packageView) int { return cmp.Compare(a.Canonical, b.Canonical) })
	return views
}

func renderPackageViews(views []packageView) []byte {
	var b bytes.Buffer
	for _, v := range views {
		line := fmt.Sprintf("%-10s %s", v.State, v.Canonical)
		if v.Installed != "" {
			line += " " + v.Installed
		}
		if v.Repository != "" {
			line += " (" + v.Repository + ")"
		}
		if len(v.Paths) > 0 {
			line += "  <- " + strings.Join(v.Paths, ", ")
		}
		if v.Blocked != "" {
			line += "  " + v.Blocked
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
			src := newSource()
			f := inspect.Inspect(src, s.Root)
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
			views := packageViews(s, f, applied)

			views = filterViews(views, func(v packageView) bool { return keep(query, all, v) })
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
		func(_ string, _ bool, v packageView) bool {
			return slices.Contains([]string{"managed", "adopt", "repair", "remove", "retire"}, v.State)
		}, 0, "")
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
			return v.State != "desired" && v.State != "dependency" && strings.Contains(v.Name, query)
		}, 1, "")
	packages := &cobra.Command{Use: "packages", Short: "Package views over the selected machine", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
	packages.AddCommand(list)
	return packages
}

func newWhy(opts *options) *cobra.Command {
	var flags machineFlags
	cmd := &cobra.Command{
		Use:   "why RESOURCE",
		Short: "Explain the selection paths for a package, system resource, component, or profile",
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
	owned := func(kind, id, component string) *whyResult {
		paths := []string{"component:" + component}
		for _, c := range r.Components {
			if c.ID == component {
				paths = append(paths, c.Paths...)
			}
		}
		return &whyResult{Kind: kind, ID: id, Paths: paths}
	}
	for _, file := range r.Files {
		if resource == "file:"+file.Target {
			return owned("system-file", resource, file.Component), nil
		}
		for _, trigger := range file.Triggers {
			if resource == "trigger:"+trigger {
				var paths []string
				for _, f := range r.Files {
					for _, id := range f.Triggers {
						if id == trigger {
							paths = addUnique(paths, append([]string{"file:" + f.Target}, owned("", "", f.Component).Paths...)...)
						}
					}
				}
				return &whyResult{Kind: "trigger", ID: resource, Paths: paths}, nil
			}
		}
	}
	if i := slices.IndexFunc(r.Services, func(service definitions.ResolvedService) bool { return resource == "service:"+service.Unit }); i >= 0 {
		return owned("service", resource, r.Services[i].Component), nil
	}
	if i := slices.IndexFunc(r.Groups, func(group definitions.ResolvedGroup) bool {
		return resource == "group:"+group.Name || resource == "group:"+group.Name+":"+group.User
	}); i >= 0 {
		group := r.Groups[i]
		return owned("group", "group:"+group.Name+":"+group.User, group.Component), nil
	}
	if resource == "default-target" && r.DefaultTarget != "" {
		if i := slices.IndexFunc(r.Components, func(c definitions.ResolvedComponent) bool { return s.Checkout.Components[c.ID].DefaultTarget != "" }); i >= 0 {
			return owned("default-target", resource, r.Components[i].ID), nil
		}
	}
	if slices.Contains(r.Profiles, resource) {
		return &whyResult{Kind: "profile", ID: resource, Paths: []string{"machine"}}, nil
	}
	if i := slices.IndexFunc(r.Components, func(c definitions.ResolvedComponent) bool { return c.ID == resource }); i >= 0 {
		c := r.Components[i]
		return &whyResult{Kind: "component", ID: c.ID, Paths: c.Paths}, nil
	}
	ref, err := definitions.ParseRef(resource)
	if err == nil {
		if i := slices.IndexFunc(r.Packages, func(p definitions.ResolvedPackage) bool { return p.Canonical == ref.Canonical() || p.Name == resource }); i >= 0 {
			p := r.Packages[i]
			return &whyResult{Kind: "package", ID: p.Canonical, Paths: p.Paths}, nil
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
