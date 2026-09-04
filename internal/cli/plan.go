package cli

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/plan"
)

func buildPlan(s *selected, src facts.Source) (*plan.Plan, error) {
	p, _, err := planWithState(s, src, false)
	return p, err
}

// planWidth is the column at which rendered lists wrap. Terminals differ,
// so the plan wraps at the conventional width rather than measuring one.
const planWidth = 80

// renderPlan writes the plan the way an installer shows its work: what
// will be prepared, installed, upgraded, and removed, then problems. The
// update list is plan's information; apply passes listUpdates false and
// gets one line.
func renderPlan(p *plan.Plan, prune, listUpdates bool) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "plan for %s", p.Machine)
	if p.Checkout.Commit != "" {
		state := "clean"
		if p.Checkout.Dirty {
			state = "dirty"
		}
		fmt.Fprintf(&b, " (checkout %s at %.12s, %s)", p.Checkout.Origin, p.Checkout.Commit, state)
	}
	b.WriteString("\n")

	var sources, problems, notes, userTools []string
	var installTx *plan.Operation
	var pendingNames, flatpaks, removals []string
	adopted, kept := 0, 0
	noted := map[string]bool{}
	for i := range p.Operations {
		op := &p.Operations[i]
		if op.Blocked != "" {
			problems = append(problems, op.Summary+": "+op.Blocked)
			continue
		}
		// The same note from several operations, such as every crate
		// waiting for the Rust runtime, is shown once.
		for _, n := range op.Notes {
			if !noted[n] {
				noted[n] = true
				notes = append(notes, n)
			}
		}
		switch op.Action {
		case plan.ActionKeep:
			kept++
			continue
		case plan.ActionAdopt:
			adopted++
			continue
		}
		switch {
		case op.Kind == plan.KindDNFConfig || op.Kind == plan.KindRepository || op.Kind == plan.KindFlatpakRemote:
			summary := op.Summary
			if op.Action == plan.ActionEnable {
				// The location is in nimbus.toml; the line is for scanning.
				if i := strings.Index(summary, " ("); i > 0 {
					summary = summary[:i]
				}
			}
			sources = append(sources, summary)
		case op.ID == "packages:install":
			installTx = op
		case op.Kind == plan.KindPackage && op.Action == plan.ActionInstall:
			pendingNames = append(pendingNames, plan.PackageName(op.ID))
		case op.Kind == plan.KindFlatpak && op.Action == plan.ActionInstall:
			flatpaks = append(flatpaks, strings.TrimPrefix(op.ID, "flatpak:"))
		case op.Kind == plan.KindUser:
			line := op.Summary
			if op.After != "" {
				line += " (after " + describeAfter(p, op.After) + ")"
			}
			userTools = append(userTools, line)
		case op.Action == plan.ActionRemove || op.Action == plan.ActionPrune || op.Action == plan.ActionRetire:
			removals = append(removals, op.Summary)
			if op.Transaction != nil {
				writeTransactionInto(&removals, op.Transaction)
			}
		}
	}
	if len(sources) > 0 {
		b.WriteString("\nsources to prepare:\n")
		for _, src := range sources {
			writeWrapped(&b, "  ", strings.Fields(src), " ", "", "    ")
		}
	}
	names := append([]string(nil), pendingNames...)
	if installTx != nil {
		for _, item := range installTx.Items {
			names = append(names, plan.PackageName(item))
		}
	}
	sort.Strings(names)
	switch {
	case installTx != nil && installTx.Transaction != nil:
		tx := installTx.Transaction
		size := ""
		if tx.Download != "" {
			size = ", " + tx.Download + " to download"
		}
		fmt.Fprintf(&b, "\ninstall %d packages%s:\n", len(names), size)
		rows := make([]string, 0, len(names))
		for _, r := range tx.Rows("installing") {
			rows = append(rows, r.Name+"-"+r.EVR+" ("+r.Repository+")")
		}
		for _, n := range pendingNames {
			rows = append(rows, n+" (after its repository is prepared)")
		}
		writeWrapped(&b, "  ", rows, ", ", ",", "  ")
		deps, weak := len(tx.Rows("installing dependencies")), len(tx.Rows("installing weak dependencies"))
		if deps+weak > 0 {
			fmt.Fprintf(&b, "  plus %d dependencies and %d weak dependencies\n", deps, weak)
		}
		if up := tx.Rows("upgrading"); len(up) > 0 {
			items := make([]string, 0, len(up))
			for _, r := range up {
				items = append(items, r.Name+"-"+r.EVR)
			}
			writeWrapped(&b, "  upgrades needed: ", items, ", ", ",", "    ")
		}
		var removing []string
		for _, sec := range []string{"removing", "removing dependent packages", "removing unused dependencies"} {
			for _, r := range tx.Rows(sec) {
				removing = append(removing, r.Name+"-"+r.EVR)
			}
		}
		if len(removing) > 0 {
			writeWrapped(&b, "  removes on the way: ", removing, ", ", ",", "    ")
		}
	case len(names) > 0:
		fmt.Fprintf(&b, "\ninstall %d packages (exact versions once sources are prepared):\n", len(names))
		writeWrapped(&b, "  ", names, ", ", ",", "  ")
	}
	if len(flatpaks) > 0 {
		sort.Strings(flatpaks)
		writeWrapped(&b, fmt.Sprintf("\ninstall %d Flatpaks: ", len(flatpaks)), flatpaks, ", ", ",", "  ")
	}
	if len(userTools) > 0 {
		b.WriteString("\nuser tools, as the user without sudo:\n")
		for _, line := range userTools {
			writeWrapped(&b, "  ", strings.Fields(line), " ", "", "    ")
		}
	}
	if len(removals) > 0 {
		b.WriteString("\n")
		for _, r := range removals {
			fmt.Fprintf(&b, "%s\n", r)
		}
	}
	if len(notes) > 0 {
		b.WriteString("\n")
		for _, n := range notes {
			writeWrapped(&b, "note: ", strings.Fields(n), " ", "", "  ")
		}
	}
	if adopted+kept > 0 {
		b.WriteString("\n")
	}
	if adopted > 0 {
		fmt.Fprintf(&b, "adopt %d already installed\n", adopted)
	}
	if kept > 0 {
		fmt.Fprintf(&b, "%d managed and unchanged\n", kept)
	}
	if prune && p.PruneUnavailable != "" {
		fmt.Fprintf(&b, "\nprune: %s\n", p.PruneUnavailable)
	} else if prune {
		fmt.Fprintf(&b, "\nprune %d unmanaged packages:\n", len(p.Prune))
		items := make([]string, 0, len(p.Prune))
		for _, pr := range p.Prune {
			items = append(items, pr.Name+"-"+pr.EVR+" ("+pr.Repository+")")
		}
		writeWrapped(&b, "  ", items, ", ", ",", "  ")
	}
	writeUpdates(&b, p.Updates, listUpdates)
	if len(problems) > 0 {
		b.WriteString("\nproblems:\n")
		for _, pr := range problems {
			writeWrapped(&b, "  ", []string{pr}, " ", "", "    ")
		}
		b.WriteString("incomplete: fix the problems above\n")
	}
	return b.Bytes()
}

// describeAfter names what an operation waits for in the plan's own words:
// the Chezmoi handoff, a component's runtimes, a component's installer, or
// the operation ID when it is none of those.
func describeAfter(p *plan.Plan, after string) string {
	if after == plan.AfterHandoff {
		return "the Chezmoi handoff"
	}
	if rest, ok := strings.CutPrefix(after, "user:"); ok {
		if component, ok := strings.CutSuffix(rest, ":install"); ok {
			return "the " + component + " runtimes"
		}
		return rest + " is installed"
	}
	return after
}

// writeTransactionInto renders a removal preview's rows as lines of the
// removal section.
func writeTransactionInto(lines *[]string, tx *plan.Transaction) {
	for _, sec := range []string{"removing", "removing dependent packages", "removing unused dependencies"} {
		rows := tx.Rows(sec)
		if len(rows) == 0 {
			continue
		}
		items := make([]string, 0, len(rows))
		for _, r := range rows {
			items = append(items, r.Name+"-"+r.EVR)
		}
		var b bytes.Buffer
		writeWrapped(&b, "  "+sec+": ", items, ", ", ",", "    ")
		*lines = append(*lines, strings.TrimSuffix(b.String(), "\n"))
	}
}

// writeUpdates renders the system updates: with upgrade on, as the step sync
// will run; with it off, as a count the owner can act on later.
func writeUpdates(b *bytes.Buffer, u plan.Updates, upgrade bool) {
	switch {
	case !upgrade && u.Unavailable != "":
		fmt.Fprintf(b, "\nupdates: %s\n", u.Unavailable)
	case !upgrade && len(u.Available) > 0:
		fmt.Fprintf(b, "\n%d updates are available; sync without -n installs them\n", len(u.Available))
	case !upgrade:
	case u.Unavailable != "":
		fmt.Fprintf(b, "\nupgrade the system (dnf5 upgrade, flatpak update): %s\n", u.Unavailable)
	case len(u.Available) == 0:
		fmt.Fprintln(b, "\nupgrade the system (dnf5 upgrade, flatpak update): no package updates known")
	default:
		items := make([]string, 0, len(u.Available))
		for _, up := range u.Available {
			items = append(items, up.Name+"-"+up.EVR)
		}
		fmt.Fprintf(b, "\nupgrade the system (dnf5 upgrade, flatpak update), %d package updates:\n", len(u.Available))
		writeWrapped(b, "  ", items, ", ", ",", "  ")
	}
}

// writeWrapped writes prefix followed by items, breaking the line before an
// item that would pass planWidth. Items on one line are joined by sep; a
// broken line ends with brk instead, so a comma list keeps its comma and a
// shell command keeps its continuation backslash. Continuation lines start
// with cont. Nothing is written when items is empty.
func writeWrapped(b *bytes.Buffer, prefix string, items []string, sep, brk, cont string) {
	if len(items) == 0 {
		return
	}
	b.WriteString(prefix)
	col := len(prefix)
	for i, item := range items {
		if i > 0 {
			if col+len(sep)+len(item)+len(brk) > planWidth {
				b.WriteString(brk + "\n" + cont)
				col = len(cont)
			} else {
				b.WriteString(sep)
				col += len(sep)
			}
		}
		b.WriteString(item)
		col += len(item)
	}
	b.WriteString("\n")
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
				fmt.Fprintln(&b, "plan complete; nimbus sync -p shows it")
			} else {
				fmt.Fprintln(&b, "plan incomplete; nimbus sync -p shows the problems")
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
	Managed      int    `json:"managed"`
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
		case op.Action == plan.ActionRemove || op.Action == plan.ActionPrune || op.Action == plan.ActionRetire:
			st.ToRemove++
		case op.Kind == plan.KindRepository || op.Kind == plan.KindFlatpakRemote:
			st.Repositories++
		case op.Action == plan.ActionKeep:
			st.Managed++
		case op.ID == "packages:install":
			st.ToInstall += len(op.Items)
		default:
			st.ToInstall++
		}
	}
	return st
}
