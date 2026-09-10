package apply

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/plan"
	"github.com/Furyfree/nimbus/internal/state"
)

func (ex *executor) snapshotPackages() error {
	pkgs, err := ex.installed()
	if err != nil {
		return err
	}
	ex.seen = inspect.PackageMap(pkgs)
	// The baseline is what existed before this run, frozen here: a
	// transaction later in the run updates seen, never before.
	ex.before = make([]string, 0, len(ex.seen))
	for p := range maps.Values(ex.seen) {
		ex.before = append(ex.before, p.ID())
	}
	slices.Sort(ex.before)
	ex.before = slices.Compact(ex.before)
	return nil
}

func (ex *executor) baseline() []string {
	return slices.Clone(ex.before)
}

// installed re-reads the RPM inventory so verification sees the real result
// of a transaction.
func (ex *executor) installed() ([]inspect.Package, error) {
	packages := inspect.Packages(ex.opts.Source)
	if !packages.Known() {
		return nil, errors.New("installed packages are unknown: " + packages.Error)
	}
	return packages.Value, nil
}

// packageTransaction always inspects the result, including a native failure
// after partial work. Unknown verification never becomes an empty success.
func (ex *executor) packageTransaction(argv []string, tx *plan.Transaction) ([]inspect.Package, error) {
	nativeErr := ex.sudo(argv...)
	installed, verifyErr := ex.installed()
	if verifyErr != nil {
		ex.differences = append(ex.differences, "DNF verification incomplete: "+verifyErr.Error())
		return nil, errors.Join(nativeErr, fmt.Errorf("verification: %w", verifyErr))
	}
	after := inspect.PackageMap(installed)
	for _, c := range ex.opts.Constraints {
		for key, p := range after {
			if _, existed := ex.seen[key]; !existed && p.Name == c.Name && !c.Matches(p.EVR()) {
				nativeErr = errors.Join(nativeErr, fmt.Errorf("verification: DNF installed %s %s outside constraint %s", p.Name, p.EVR(), c.Family))
			}
		}
	}
	ex.differences = append(ex.differences, transactionDifferences(tx, ex.seen, after)...)
	ex.seen = after
	return installed, nativeErr
}

func (ex *executor) installTransaction(op plan.Operation) ([]state.Receipt, []string, error) {
	if op.Transaction == nil || len(op.Steps) < 1 {
		return nil, nil, errors.New("the install operation carries no previewed transaction")
	}
	installed, err := ex.packageTransaction(op.Steps[0].Argv, op.Transaction)
	if err != nil {
		return nil, nil, err
	}
	var receipts []state.Receipt
	for _, canonical := range op.Items {
		name := plan.PackageName(canonical)
		name = cmp.Or(op.Resolved[name], name)
		inst, ok := inspect.FindPackage(installed, name)
		if !ok {
			return nil, nil, fmt.Errorf("verification: %s is not installed after the transaction", name)
		}
		sub := plan.Operation{ID: "package:" + canonical, Action: plan.ActionInstall, Paths: op.ItemPaths[canonical]}
		r := ex.receipt(sub, "dnf", "absent", "installed "+inst.ID()+" "+inst.EVR(), "dnf5 repoquery --installed lists "+inst.ID()+" "+inst.EVR())
		r.Package = inst.ID()
		receipts = append(receipts, r)
	}
	return receipts, nil, nil
}

// transactionDifferences compares complete RPM sets, retaining both multilib
// architectures and simultaneous installonly versions. A preview describes
// changes to the initial set; every other package must remain as observed.
func transactionDifferences(tx *plan.Transaction, before, after map[string]inspect.Package) []string {
	before = inspect.PackageMap(slices.Collect(maps.Values(before)))
	after = inspect.PackageMap(slices.Collect(maps.Values(after)))
	expected := maps.Clone(before)
	shown := map[string]bool{}
	if tx != nil {
		for _, row := range tx.Packages {
			id := inspect.PackageID(row.Name, row.Arch)
			shown[id] = true
			if strings.HasPrefix(row.Section, "removing") || row.Section == plan.SectionReplaced {
				delete(expected, id+" "+strings.TrimPrefix(row.EVR, "0:"))
				continue
			}
			// Upgrade and downgrade replace the installed version. Explicit
			// replacing rows also cover obsoletes and installonly changes.
			if row.Section == "upgrading" || row.Section == "downgrading" {
				maps.DeleteFunc(expected, func(_ string, p inspect.Package) bool {
					return p.ID() == id
				})
			}
			version, release, _ := strings.Cut(strings.TrimPrefix(row.EVR, "0:"), "-")
			epoch := ""
			if e, v, ok := strings.Cut(version, ":"); ok {
				epoch, version = e, v
			}
			p := inspect.Package{Name: row.Name, Arch: row.Arch, Epoch: epoch, Version: version, Release: release, FromRepo: row.Repository}
			expected[id+" "+p.EVR()] = p
		}
	}
	ids := map[string]bool{}
	for _, set := range []map[string]inspect.Package{before, expected, after} {
		for p := range maps.Values(set) {
			ids[p.ID()] = true
		}
	}
	versions := func(set map[string]inspect.Package, id string) []string {
		var out []string
		for p := range maps.Values(set) {
			if p.ID() == id {
				out = append(out, p.EVR())
			}
		}
		slices.Sort(out)
		return out
	}
	var diffs []string
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		want, got := versions(expected, id), versions(after, id)
		if slices.Equal(want, got) {
			continue
		}
		was := versions(before, id)
		switch {
		case !shown[id] && len(was) == 0:
			diffs = append(diffs, fmt.Sprintf("DNF also installed %s %s", id, strings.Join(got, ", ")))
		case !shown[id] && len(got) == 0:
			diffs = append(diffs, fmt.Sprintf("DNF also removed %s %s", id, strings.Join(was, ", ")))
		case !shown[id]:
			diffs = append(diffs, fmt.Sprintf("DNF also changed %s from %s to %s", id, strings.Join(was, ", "), strings.Join(got, ", ")))
		default:
			show := func(v []string) string {
				if len(v) == 0 {
					return "absent"
				}
				return strings.Join(v, ", ")
			}
			diffs = append(diffs, fmt.Sprintf("%s is %s after DNF; the preview expected %s", id, show(got), show(want)))
		}
	}
	if tx != nil {
		for _, row := range tx.Packages {
			if strings.HasPrefix(row.Section, "removing") || row.Section == plan.SectionReplaced || row.Repository == "" {
				continue
			}
			id := inspect.PackageID(row.Name, row.Arch)
			if p, ok := after[id+" "+strings.TrimPrefix(row.EVR, "0:")]; ok && p.FromRepo != row.Repository {
				diffs = append(diffs, fmt.Sprintf("%s %s came from %s; the preview showed %s", id, p.EVR(), p.FromRepo, row.Repository))
			}
		}
	}
	slices.Sort(diffs)
	return diffs
}

func (ex *executor) removeTransaction(op plan.Operation) ([]state.Receipt, []string, error) {
	if len(op.Steps) == 0 || len(op.Steps[0].Argv) < 5 {
		return nil, nil, errors.New("the removal command names no packages")
	}
	argv := op.Steps[0].Argv
	if argv[3] != "--no-autoremove" {
		return nil, nil, errors.New("the removal command must use --no-autoremove")
	}
	installed, err := ex.packageTransaction(argv, op.Transaction)
	if err != nil {
		return nil, nil, err
	}
	for _, name := range argv[4:] {
		if p, ok := inspect.FindPackage(installed, name); ok {
			return nil, nil, fmt.Errorf("verification: %s is still installed after the removal", p.ID())
		}
	}
	var remove []string
	if op.ID == "packages:remove-owned" {
		for _, path := range op.Paths {
			if strings.HasPrefix(path, "package:") {
				remove = append(remove, path)
			}
		}
	}
	return nil, remove, nil
}
