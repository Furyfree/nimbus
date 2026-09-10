package plan

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/state"
)

// installsPackage reports whether the plan installs a bare Fedora package
// that is desired and not yet present.
func (b *builder) installsPackage(name string) bool {
	if _, installed := inspect.FindPackage(b.in.Facts.Packages.Value, name); installed {
		return false
	}
	return slices.ContainsFunc(b.in.Resolved.Packages, func(p definitions.ResolvedPackage) bool {
		return p.Prefix == definitions.PrefixDNF && p.Name == name
	})
}

func (b *builder) packages() []Operation {
	var adopt, pending []Operation
	var install []definitions.ResolvedPackage
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == definitions.PrefixFlatpak {
			continue
		}
		if inst, ok := InstalledPackage(p.Name, b.in.Applied.Receipts["package:"+p.Canonical], b.in.Facts.Packages.Value); ok {
			op := Operation{ID: "package:" + p.Canonical, Kind: KindPackage, Action: ActionAdopt, Risk: RiskLow,
				Summary: fmt.Sprintf("adopt %s %s, already installed", p.Name, inst.EVR()), Paths: p.Paths, Resolved: map[string]string{p.Name: inst.ID()}}
			if receipt, managed := b.in.Applied.Receipts[op.ID]; managed && receipt.Package == inst.ID() {
				op.Action, op.Summary = ActionKeep, fmt.Sprintf("%s %s is managed and unchanged", p.Name, inst.EVR())
			}
			if receipt, managed := b.in.Applied.Receipts[op.ID]; managed && receipt.Package == "" {
				receipt.Resource = op.ID
				if _, err := ReceiptPackage(receipt, b.in.Facts.Packages.Value); err != nil {
					op.Blocked = err.Error()
				}
			}
			if note := b.sourceNote(p, inst); note != "" {
				op.Notes = append(op.Notes, note)
			}
			adopt = append(adopt, op)
			continue
		}
		if p.Prefix != definitions.PrefixDNF && !b.ready[p.Prefix] {
			op := Operation{ID: "package:" + p.Canonical, Kind: KindPackage, Action: ActionInstall, Risk: RiskLow,
				Summary: "install " + p.Name, Paths: p.Paths}
			op.After, op.Blocked = b.waitOrBlock(p.Prefix, KindRepository)
			pending = append(pending, op)
			continue
		}
		install = append(install, p)
	}
	ops := adopt
	var installTx *Transaction
	if len(install) > 0 {
		op := b.installTransaction(install)
		installTx = op.Transaction
		ops = append(ops, op)
	}
	if op, ok := b.removeTransaction(installTx); ok {
		ops = append(ops, op)
	}
	return append(ops, pending...)
}

// PackageName returns the native package name of a package operation ID
// such as package:terra:ghostty.
func PackageName(id string) string {
	return id[strings.LastIndexByte(id, ':')+1:]
}

// sourceNote says when an installed desired package comes from a
// repository other than the one its prefix names. The package is adopted
// or kept either way; the owner sees where it came from.
func (b *builder) sourceNote(p definitions.ResolvedPackage, inst inspect.Package) string {
	from := inst.FromRepo
	// The installer and local files record no repository worth noting.
	if from == "" || from == "anaconda" || strings.HasPrefix(from, "@") || slices.Contains(b.expectedRepos(p.Prefix), from) {
		return ""
	}
	return fmt.Sprintf("%s is installed from %s, not from %s", p.Name, from, strings.Join(b.expectedRepos(p.Prefix), " or "))
}

// installTransaction previews one DNF transaction for installable packages
// and records effects beyond the requested selections as notes.
func (b *builder) installTransaction(pkgs []definitions.ResolvedPackage) Operation {
	names := make([]string, 0, len(pkgs))
	byName := map[string]definitions.ResolvedPackage{}
	// The merged operation is explained by the profiles and components
	// that selected its packages; the packages themselves are its command.
	pathSet := map[string]bool{}
	items := make([]string, 0, len(pkgs))
	itemPaths := make(map[string][]string, len(pkgs))
	for _, p := range pkgs {
		names = append(names, p.Name)
		byName[p.Name] = p
		items = append(items, p.Canonical)
		itemPaths[p.Canonical] = p.Paths
		for _, path := range p.Paths {
			pathSet[path] = true
		}
	}
	slices.Sort(names)
	slices.Sort(items)
	paths := slices.Sorted(maps.Keys(pathSet))
	args := []string{"install"}
	removes := map[string]bool{}
	for _, r := range b.in.Resolved.Removes {
		removes[r] = true
	}
	if len(removes) > 0 {
		args = append(args, "--allowerasing")
	}
	args = append(args, names...)
	op := Operation{ID: "packages:install", Kind: KindPackage, Action: ActionInstall, Risk: RiskLow,
		Summary: fmt.Sprintf("install %d packages through one DNF transaction", len(names)), Paths: paths, Items: items, ItemPaths: itemPaths,
		Steps: []Step{{Description: "install through DNF", Argv: append([]string{"dnf5", "-y"}, args...), Privileged: true}}}
	if b.waitsForRepositories(&op) {
		return op
	}
	out, err := b.in.Source.Run("dnf5", append([]string{"--assumeno", "--cacheonly"}, args...)...)
	tx, perr := ParsePreview(out)
	if perr != nil {
		op.Blocked = previewFailure(out, err, perr)
		if repos := uncachedRepositories(op.Blocked, byName); len(repos) > 0 {
			// The repository is enabled, so the preview ran with its
			// packages; nothing matched because the local cache has never
			// held its metadata, as after an apply that stopped before
			// its refresh. The blocked reason must name the fix.
			op.Blocked = fmt.Sprintf("the enabled repositories %s have no cached metadata; sync again to refresh it (%s)", strings.Join(repos, ", "), op.Blocked)
		}
		return op
	}
	op.Transaction = tx
	if tx.Download == "" && err != nil {
		// The size summary is on stderr, which Run folds into the error.
		tx.Download = DownloadSize(err.Error())
	}
	if tx.NothingToDo {
		op.Blocked = "dnf5 reports nothing to do although packages are missing"
		return op
	}
	// DNF can list requested packages as dependencies. Direct names bind
	// to any installing row; provides still need exact native evidence.
	op.Resolved = map[string]string{}
	var installing []inspect.Package
	for _, row := range tx.Packages {
		if strings.HasPrefix(row.Section, "installing") {
			installing = append(installing, inspect.Package{Name: row.Name, Arch: row.Arch})
		}
	}
	for _, n := range names {
		if direct, ok := inspect.FindPackage(installing, n); ok {
			op.Resolved[n] = direct.ID()
			continue
		}
		provider, err := b.resolveProvide(n, tx)
		if err != nil {
			op.Blocked = err.Error()
			return op
		}
		op.Resolved[n] = inspect.PackageID(provider.Name, provider.Arch)
		op.Notes = append(op.Notes, fmt.Sprintf("%s resolves to the package %s", n, provider.Name))
	}
	var problems, needed []string
	upgraded := map[string]bool{}
	for _, row := range tx.Packages {
		if row.Section == "upgrading" {
			upgraded[row.Name] = true
		}
	}
	for _, row := range tx.Packages {
		switch row.Section {
		case "installing", "installing dependencies", "installing weak dependencies":
			wanted := false
			for _, n := range names {
				if op.Resolved[n] != inspect.PackageID(row.Name, row.Arch) {
					continue
				}
				wanted = true
				p := byName[n]
				if !slices.Contains(b.expectedRepos(p.Prefix), row.Repository) {
					problems = append(problems, fmt.Sprintf("%s would come from repository %s, not %s", row.Name, row.Repository, strings.Join(b.expectedRepos(p.Prefix), " or ")))
				}
			}
			if !wanted && row.Section == "installing" {
				problems = append(problems, "would install "+row.Name+", which nothing selects")
			}
		case "removing", "removing dependent packages", "removing unused dependencies":
			if !removes[row.Name] {
				problems = append(problems, "would remove "+row.Name+", which no component declares in removes")
			}
		case SectionReplaced:
			// The old version of an upgrade is fine; a package replaced by
			// another name is a removal nothing declared.
			if !upgraded[row.Name] && !removes[row.Name] {
				problems = append(problems, "would replace "+row.Name+", which no component declares in removes")
			}
		case "upgrading":
			// An install transaction upgrades an installed package only
			// when a requested package needs the newer version; the update
			// of everything else is nimbus upgrade's.
			needed = append(needed, row.Name)
		case "downgrading", "reinstalling":
			problems = append(problems, fmt.Sprintf("would %s %s", strings.TrimSuffix(row.Section, "ing")+"e", row.Name))
		}
	}
	if len(needed) > 0 {
		op.Notes = append(op.Notes, fmt.Sprintf("%d installed packages are upgraded because the requested packages need the newer versions: %s", len(needed), strings.Join(needed, ", ")))
	}
	// DNF's resolution is what will run; anything beyond the definitions is
	// shown for the review rather than refused, since the owner wrote the
	// definitions and DNF's own rules already bound the sources.
	for _, problem := range problems {
		op.Notes = append(op.Notes, "beyond the definitions: "+problem)
	}
	return op
}

func (b *builder) resolveProvide(request string, tx *Transaction) (TxPackage, error) {
	out, err := b.in.Source.Run("dnf5", "--cacheonly", "repoquery", "--available", "--whatprovides", request, "--queryformat", "%{name}|%{arch}|%{evr}|%{repoid}\\n")
	if err != nil {
		return TxPackage{}, fmt.Errorf("resolve provider for %s: %w", request, err)
	}
	var matches []TxPackage
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) != 4 || slices.Contains(fields, "") {
			return TxPackage{}, fmt.Errorf("resolve provider for %s: invalid provider row %q", request, line)
		}
		for _, row := range tx.Packages {
			if strings.HasPrefix(row.Section, "installing") && row.Name == fields[0] && row.Arch == fields[1] &&
				strings.TrimPrefix(row.EVR, "0:") == strings.TrimPrefix(fields[2], "0:") && row.Repository == fields[3] &&
				!slices.Contains(matches, row) {
				matches = append(matches, row)
			}
		}
	}
	if len(matches) == 0 {
		return TxPackage{}, fmt.Errorf("resolve provider for %s: no reviewed package matches the native provider evidence", request)
	}
	if len(matches) != 1 {
		return TxPackage{}, fmt.Errorf("resolve provider for %s: multiple reviewed packages match the native provider evidence", request)
	}
	return matches[0], nil
}

// waitsForRepositories makes a DNF transaction pending when this round
// changes a repository: the preview would run against metadata the
// download no longer sees. Apply runs the repository operations, refreshes
// the cache, and plans the transaction again.
func (b *builder) waitsForRepositories(op *Operation) bool {
	if len(b.repoChanges) == 0 {
		return false
	}
	op.After = strings.Join(b.repoChanges, ", ")
	return true
}

// uncachedRepositories returns the non-Fedora repositories of the packages
// DNF reported no match for. Such a repository is enabled, or the preview
// would not have included its packages, so the miss means its metadata is
// not in the local cache yet.
func uncachedRepositories(blocked string, byName map[string]definitions.ResolvedPackage) []string {
	seen := map[string]bool{}
	var repos []string
	for problem := range strings.SplitSeq(strings.TrimPrefix(blocked, "dnf5 could not resolve the transaction: "), "; ") {
		name, ok := strings.CutPrefix(problem, "No match for argument: ")
		if !ok {
			continue
		}
		p, known := byName[strings.TrimSpace(name)]
		if !known || p.Prefix == definitions.PrefixDNF || seen[p.Prefix] {
			continue
		}
		seen[p.Prefix] = true
		repos = append(repos, p.Prefix)
	}
	slices.Sort(repos)
	return repos
}

// previewFailure turns a failed dnf5 preview into one readable reason. DNF5
// prints its resolution problems on stderr, which the command error
// carries, so that text is parsed before falling back to the raw error.
func previewFailure(out []byte, runErr, parseErr error) string {
	if runErr != nil {
		if _, err := ParsePreview([]byte(runErr.Error())); err != nil {
			if resolve, ok := errors.AsType[*ResolveError](err); ok {
				return resolve.Error()
			}
		}
		if len(strings.TrimSpace(string(out))) == 0 {
			lines := strings.Split(strings.TrimSpace(runErr.Error()), "\n")
			return "dnf5 preview failed: " + lines[len(lines)-1]
		}
	}
	return parseErr.Error()
}

// InstalledPackage also recognizes a provide resolved by an earlier verified
// installation. The receipt binds that native identity to this reference.
func InstalledPackage(request string, receipt state.Receipt, packages []inspect.Package) (inspect.Package, bool) {
	if receipt.Package != "" {
		return inspect.FindPackage(packages, receipt.Package)
	}
	return inspect.FindPackage(packages, request)
}

// ReceiptPackage resolves recorded RPM ownership without guessing a legacy
// receipt's architecture or the native package behind a provide.
func ReceiptPackage(r state.Receipt, packages []inspect.Package) (string, error) {
	if r.Package != "" {
		return r.Package, nil
	}
	request := PackageName(r.Resource)
	matches := map[string]bool{}
	for _, p := range packages {
		if p.Matches(request) {
			matches[p.ID()] = true
		}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("legacy receipt for %s does not identify which installed architecture Nimbus owns; select each installed architecture explicitly to establish ownership", request)
	}
	for id := range maps.Keys(matches) {
		return id, nil
	}
	// Old provide receipts recorded a native name only in prose. That is
	// not an ownership identity from which deletion can safely be inferred.
	fields := strings.Fields(r.Intended)
	if len(fields) >= 3 && fields[0] == "installed" && fields[1] != request {
		return "", fmt.Errorf("legacy receipt for %s may refer to %s; its native ownership must be established before removal", request, fields[1])
	}
	return request, nil
}
