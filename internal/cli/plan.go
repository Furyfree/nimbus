package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
)

func newPlan(opts *options) *cobra.Command {
	var flags machineFlags
	var prune bool
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show the complete plan for the selected machine without changing anything",
		Long: `Plan compares the desired configuration with the installed system and shows
every operation apply would run, plus the known update information. With
--prune it also shows the unmanaged packages apply --prune would remove. It
writes nothing, uses no network, and runs no mutating command. DNF previews
come from the local metadata cache; run nimbus refresh first when it is
stale.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSelected(flags)
			if err != nil {
				return err
			}
			p, err := buildPlan(s, newSource())
			if err != nil {
				return err
			}
			if !prune {
				p.Prune = nil
			}
			if opts.json {
				err = writeJSON(cmd.OutOrStdout(), p, nil)
			} else {
				_, err = cmd.OutOrStdout().Write(renderPlan(p, prune))
			}
			if err != nil {
				return err
			}
			if !p.Complete {
				return reported{}
			}
			return nil
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	cmd.Flags().BoolVar(&prune, "prune", false, "also show the unmanaged packages apply --prune would remove")
	return cmd
}

func buildPlan(s *selected, src facts.Source) (*plan.Plan, error) {
	f := facts.Inspect(src, s.Root)
	return plan.Build(plan.Inputs{Resolved: s.Resolved, Root: s.Checkout.Definitions(), Definitions: s.Checkout.Digest(), Facts: f, Source: src})
}

func renderPlan(p *plan.Plan, prune bool) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "plan for %s\n", p.Machine)
	fmt.Fprintf(&b, "definitions %s\n", p.Definitions)
	if p.Checkout.Commit != "" {
		state := "clean"
		if p.Checkout.Dirty {
			state = "dirty"
		}
		fmt.Fprintf(&b, "checkout %s at %s (%s)\n", p.Checkout.Origin, p.Checkout.Commit, state)
	}
	b.WriteString("\n")
	adopted := 0
	fmt.Fprintln(&b, "apply:")
	for _, op := range p.Operations {
		if op.Action == plan.ActionAdopt && op.Blocked == "" && len(op.Notes) == 0 {
			adopted++
			continue
		}
		status := op.Risk
		switch {
		case op.Blocked != "":
			status = "blocked"
		case op.After != "":
			status = "pending"
		}
		fmt.Fprintf(&b, "  [%s] %s\n", status, op.Summary)
		if len(op.Paths) > 0 {
			fmt.Fprintf(&b, "        because: %s\n", strings.Join(op.Paths, ", "))
		}
		for _, n := range op.Notes {
			fmt.Fprintf(&b, "        note: %s\n", n)
		}
		if op.Blocked != "" {
			// Nothing below a blocked operation is runnable yet, so its
			// steps and transaction are not shown as if it were.
			fmt.Fprintf(&b, "        blocked: %s\n", op.Blocked)
			continue
		}
		if op.After != "" {
			fmt.Fprintf(&b, "        after %s; the exact transaction is shown once that has run\n", op.After)
			continue
		}
		for _, st := range op.Steps {
			if st.Argv != nil {
				prefix := ""
				if st.Privileged {
					prefix = "sudo "
				}
				fmt.Fprintf(&b, "        $ %s%s\n", prefix, strings.Join(st.Argv, " "))
			} else {
				fmt.Fprintf(&b, "        - %s\n", st.Description)
			}
		}
		if op.Transaction != nil {
			for _, sec := range []string{"installing", "installing dependencies", "installing weak dependencies", "removing", "removing dependent packages", "removing unused dependencies", "replacing", "upgrading", "downgrading", "reinstalling"} {
				rows := op.Transaction.Rows(sec)
				if len(rows) == 0 {
					continue
				}
				names := make([]string, 0, len(rows))
				for _, r := range rows {
					names = append(names, r.Name+"-"+r.EVR+" ("+r.Repository+")")
				}
				fmt.Fprintf(&b, "        %s (%d): %s\n", sec, len(rows), strings.Join(names, ", "))
			}
		}
	}
	if adopted > 0 {
		fmt.Fprintf(&b, "  %d installed packages are adopted unchanged\n", adopted)
	}
	if prune {
		fmt.Fprintf(&b, "\nprune (%d unmanaged packages apply --prune would remove):\n", len(p.Prune))
		for _, pr := range p.Prune {
			fmt.Fprintf(&b, "  %s-%s (%s)\n", pr.Name, pr.EVR, pr.Repository)
		}
	}
	fmt.Fprintln(&b, "\nupdates (apply never installs these; use nimbus upgrade):")
	if p.Updates.Unavailable != "" {
		fmt.Fprintf(&b, "  %s\n", p.Updates.Unavailable)
	} else if len(p.Updates.Available) == 0 {
		fmt.Fprintln(&b, "  none known from the local metadata cache")
	}
	for _, u := range p.Updates.Available {
		fmt.Fprintf(&b, "  %s %s (%s)\n", u.Name, u.EVR, u.Repository)
	}
	if p.Complete {
		fmt.Fprintf(&b, "\ncomplete; digest %s\n", p.Digest)
	} else {
		fmt.Fprintf(&b, "\nincomplete: blocked operations above must be resolved first; digest %s\n", p.Digest)
	}
	return b.Bytes()
}

func newStatus(opts *options) *cobra.Command {
	var flags machineFlags
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Summarize desired, observed, and pending state for the selected machine",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSelected(flags)
			if err != nil {
				return err
			}
			p, err := buildPlan(s, newSource())
			if err != nil {
				return err
			}
			st := summarize(s, p)
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), st, nil)
			}
			var b bytes.Buffer
			fmt.Fprintf(&b, "machine %s: %d profiles, %d components, %d desired packages\n", st.Machine, st.Profiles, st.Components, st.Desired)
			fmt.Fprintf(&b, "adopted %d, to install %d, to remove %d, repositories to enable %d, pending %d, blocked %d\n", st.Adopted, st.ToInstall, st.ToRemove, st.Repositories, st.Pending, st.Blocked)
			fmt.Fprintf(&b, "prune candidates %d, updates available %d\n", st.Prune, st.Updates)
			if st.Complete {
				fmt.Fprintln(&b, "plan complete; run nimbus plan to review it")
			} else {
				fmt.Fprintln(&b, "plan incomplete; run nimbus plan to see what blocks it")
			}
			_, err = cmd.OutOrStdout().Write(b.Bytes())
			return err
		},
	}
	addMachineFlags(&flags, cmd.Flags())
	return cmd
}

type statusResult struct {
	Machine      string `json:"machine"`
	Profiles     int    `json:"profiles"`
	Components   int    `json:"components"`
	Desired      int    `json:"desired_packages"`
	Adopted      int    `json:"adopted"`
	ToInstall    int    `json:"to_install"`
	ToRemove     int    `json:"to_remove"`
	Repositories int    `json:"repositories_to_enable"`
	Pending      int    `json:"pending"`
	Blocked      int    `json:"blocked"`
	Prune        int    `json:"prune_candidates"`
	Updates      int    `json:"updates_available"`
	Complete     bool   `json:"complete"`
	Digest       string `json:"digest"`
}

func summarize(s *selected, p *plan.Plan) statusResult {
	st := statusResult{Machine: p.Machine, Profiles: len(s.Resolved.Profiles), Components: len(s.Resolved.Components), Desired: len(s.Resolved.Packages),
		Prune: len(p.Prune), Updates: len(p.Updates.Available), Complete: p.Complete, Digest: p.Digest}
	for _, op := range p.Operations {
		switch {
		case op.Blocked != "":
			st.Blocked++
		case op.After != "":
			st.Pending++
		case op.Action == plan.ActionAdopt:
			st.Adopted++
		case op.Action == plan.ActionRemove:
			st.ToRemove++
		case op.Kind == plan.KindRepository || op.Kind == plan.KindFlatpakRemote:
			st.Repositories++
		case op.ID == "packages:install":
			st.ToInstall += len(op.Paths)
		default:
			st.ToInstall++
		}
	}
	return st
}
