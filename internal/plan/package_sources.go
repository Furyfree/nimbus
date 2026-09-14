package plan

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
)

// PackageSources maps native names (optionally architecture-qualified) to the
// repository family permitted by the selected definitions.
type PackageSources map[string][]string

func (s PackageSources) add(name string, repos []string) {
	if old, ok := s[name]; ok {
		repos = slices.DeleteFunc(slices.Clone(repos), func(repo string) bool { return !slices.Contains(old, repo) })
	}
	s[name] = repos
}

func (s PackageSources) Check(name, arch, repo string) error {
	for _, key := range []string{name, inspect.PackageID(name, arch)} {
		if allowed, ok := s[key]; ok && !slices.Contains(allowed, repo) {
			return fmt.Errorf("%s would come from repository %s, not %s", key, repo, strings.Join(allowed, " or "))
		}
	}
	return nil
}

func (s PackageSources) CheckTransaction(tx *Transaction) error {
	if tx == nil {
		return fmt.Errorf("package source verification requires a transaction preview")
	}
	for _, row := range tx.Packages {
		if strings.HasPrefix(row.Section, "removing") || row.Section == SectionReplaced {
			_, named := s[row.Name]
			_, qualified := s[inspect.PackageID(row.Name, row.Arch)]
			if (named || qualified) && !slices.ContainsFunc(tx.Packages, func(incoming TxPackage) bool {
				return incoming.Name == row.Name && incoming.Arch == row.Arch && incoming.Section != SectionReplaced && !strings.HasPrefix(incoming.Section, "removing")
			}) {
				return fmt.Errorf("DNF would remove selected package %s without a replacement from its declared source", inspect.PackageID(row.Name, row.Arch))
			}
			continue
		}
		if err := s.Check(row.Name, row.Arch, row.Repository); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) packageSources() PackageSources {
	rules := PackageSources{}
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == definitions.PrefixFlatpak {
			continue
		}
		name := p.Name
		if inst, ok := InstalledPackage(name, b.in.Applied.Receipts["package:"+p.Canonical], b.in.Facts.Packages.Value); ok {
			name = inst.ID()
		}
		ids, err := b.enabledPackageRepos(p.Prefix)
		if err != nil {
			// Pending source preparation must replan before execution.
			ids = b.expectedRepos(p.Prefix)
		}
		rules.add(name, ids)
	}
	return rules
}

// --from-repo also enables repositories. Pass only currently enabled IDs,
// never wildcards or a disabled updates/testing repository.
func (b *builder) enabledPackageRepos(prefix string) ([]string, error) {
	var ids []string
	for _, id := range b.expectedRepos(prefix) {
		if slices.ContainsFunc(b.repos[id], func(r inspect.Repository) bool { return r.Enabled }) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no enabled repository for package source %s; run sync to prepare its declared repositories", prefix)
	}
	return ids, nil
}

func (b *builder) installSourceArgs(pkgs []definitions.ResolvedPackage) ([]string, error) {
	groups := map[string][]string{}
	for _, p := range pkgs {
		ids, err := b.enabledPackageRepos(p.Prefix)
		if err != nil {
			if len(b.repoChanges) == 0 {
				return nil, err
			}
			// Explanatory argv only: this operation waits for source setup
			// and must be replanned against enabled repositories before use.
			ids = b.expectedRepos(p.Prefix)
		}
		key := strings.Join(ids, ",")
		groups[key] = append(groups[key], p.Name)
	}
	args := []string{"do", "--action=install"}
	if len(b.in.Resolved.Removes) > 0 {
		args = append(args, "--allowerasing")
	}
	for _, repo := range slices.Sorted(maps.Keys(groups)) {
		slices.Sort(groups[repo])
		args = append(args, "--from-repo="+repo)
		args = append(args, groups[repo]...)
	}
	return args, nil
}

func (b *builder) repairPackageSource(p definitions.ResolvedPackage, inst inspect.Package) Operation {
	op := Operation{ID: "package:" + p.Canonical, Kind: KindPackage, Action: ActionRepair, Risk: RiskMedium,
		Summary: fmt.Sprintf("correct %s source: %s -> %s", inst.ID(), inst.FromRepo, strings.Join(b.expectedRepos(p.Prefix), " or ")),
		Paths:   p.Paths, Items: []string{p.Canonical}, ItemPaths: map[string][]string{p.Canonical: p.Paths},
		Resolved: map[string]string{p.Name: inst.ID()}, PackageSources: b.packageSources()}
	if b.waitsForRepositories(&op) {
		return op
	}
	ids, err := b.enabledPackageRepos(p.Prefix)
	if err != nil {
		op.Blocked = err.Error()
		return op
	}
	// Distro-sync handles a newer or older allowed version; a same-version
	// source correction needs reinstall to acquire fresh provenance.
	for _, action := range []string{"distro-sync", "reinstall"} {
		args := []string{action, "--from-repo=" + strings.Join(ids, ","), inst.ID()}
		out, runErr := b.in.Source.Run("dnf5", append([]string{"--assumeno", "--cacheonly"}, args...)...)
		tx, err := ParsePreview(out)
		if err != nil {
			op.Blocked = previewFailure(out, runErr, err)
			return op
		}
		if tx.NothingToDo && action == "distro-sync" && runErr == nil {
			continue
		}
		op.Transaction = tx
		if tx.Download == "" && runErr != nil {
			tx.Download = DownloadSize(runErr.Error())
		}
		op.Steps = []Step{{Description: "correct the package source through DNF", Privileged: true, Argv: append([]string{"dnf5", "-y"}, args...)}}
		if tx.NothingToDo || len(tx.Packages) == 0 {
			op.Blocked = "DNF did not resolve the source correction"
			return op
		}
		if err := op.PackageSources.CheckTransaction(tx); err != nil {
			op.Blocked = err.Error()
		}
		found := slices.ContainsFunc(tx.Packages, func(row TxPackage) bool {
			return row.Name == inst.Name && row.Arch == inst.Arch && row.Section != SectionReplaced && !strings.HasPrefix(row.Section, "removing")
		})
		if !found {
			op.Blocked = "DNF did not provide the requested package from its declared source"
		}
		return op
	}
	return op
}

func (b *builder) sourceUpgrade() Updates {
	u := Updates{Available: []Upgrade{}, PackageSources: b.packageSources(), Command: []string{"do", "--action=upgrade"}}
	groups := map[string][]string{}
	selected := map[string]bool{}
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == definitions.PrefixFlatpak {
			continue
		}
		inst, ok := InstalledPackage(p.Name, b.in.Applied.Receipts["package:"+p.Canonical], b.in.Facts.Packages.Value)
		if !ok {
			continue // upgrades do not install missing selections
		}
		ids, err := b.enabledPackageRepos(p.Prefix)
		if err != nil {
			u.Unavailable = err.Error()
			return u
		}
		key := strings.Join(ids, ",")
		groups[key] = append(groups[key], inst.ID())
		selected[inst.ID()] = true
	}
	// Unselected packages keep native sources. Put these first, before any
	// --from-repo option; '*' there would also enable disabled repositories.
	other := map[string]bool{}
	for _, inst := range b.in.Facts.Packages.Value {
		if !selected[inst.ID()] {
			other[inst.ID()] = true
		}
	}
	u.Command = append(u.Command, slices.Sorted(maps.Keys(other))...)
	for _, repo := range slices.Sorted(maps.Keys(groups)) {
		slices.Sort(groups[repo])
		u.Command = append(u.Command, "--from-repo="+repo)
		u.Command = append(u.Command, slices.Compact(groups[repo])...)
	}
	if len(groups) == 0 {
		u.Command = []string{"upgrade"}
	}
	out, runErr := b.in.Source.Run("dnf5", append([]string{"--cacheonly", "--assumeno"}, u.Command...)...)
	tx, err := ParsePreview(out)
	if err != nil {
		u.Unavailable = "DNF update check failed: " + previewFailure(out, runErr, err)
		return u
	}
	if runErr != nil && len(tx.Packages) == 0 {
		u.Unavailable = "DNF update check failed: " + runErr.Error()
		return u
	}
	if !tx.NothingToDo && len(tx.Packages) == 0 {
		u.Unavailable = "DNF update check returned no usable transaction"
		return u
	}
	u.Transaction = tx
	if err := u.PackageSources.CheckTransaction(tx); err != nil {
		u.Unavailable = err.Error()
	}
	for _, row := range tx.Packages {
		if row.Section != SectionReplaced && !strings.HasPrefix(row.Section, "removing") {
			u.Available = append(u.Available, Upgrade{Name: row.Name, Arch: row.Arch, EVR: row.EVR, Repository: row.Repository})
		}
	}
	return u
}
