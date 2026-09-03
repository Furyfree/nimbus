package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
)

// Operation kinds, actions, and risk classes.
const (
	KindRepository    = "repository"
	KindPackage       = "package"
	KindFlatpakRemote = "flatpak-remote"
	KindFlatpak       = "flatpak"

	ActionEnable  = "enable"
	ActionInstall = "install"
	ActionAdopt   = "adopt"
	ActionRemove  = "remove"

	RiskLow    = "low"    // adds something; reversible by removal
	RiskMedium = "medium" // removes or changes trust; reviewed with more care
)

// Step is one exact action of an operation. Argv is the native command;
// a nil Argv is an internal Nimbus action described by Description.
type Step struct {
	Description string   `json:"description"`
	Argv        []string `json:"argv,omitempty"`
	Privileged  bool     `json:"privileged,omitempty"`
}

// Operation is one reviewed unit of the plan.
type Operation struct {
	ID          string       `json:"id"`
	Kind        string       `json:"kind"`
	Action      string       `json:"action"`
	Risk        string       `json:"risk"`
	Summary     string       `json:"summary"`
	Paths       []string     `json:"paths,omitempty"`
	Steps       []Step       `json:"steps,omitempty"`
	Transaction *Transaction `json:"transaction,omitempty"`
	// Blocked explains why the operation cannot be planned yet. A blocked
	// operation keeps the plan incomplete.
	Blocked string `json:"blocked,omitempty"`
	// Notes are observations that do not block, such as a package installed
	// from another repository than its prefix names.
	Notes []string `json:"notes,omitempty"`
}

// Prune is an installed, user-requested package that nothing desires. It is
// informational until apply --prune exists.
type Prune struct {
	Name       string `json:"name"`
	EVR        string `json:"evr"`
	Repository string `json:"repository"`
}

// Updates is the known normal-update information, kept apart from apply.
type Updates struct {
	Available   []Upgrade `json:"available"`
	Unavailable string    `json:"unavailable,omitempty"`
}

// Plan is the complete result for one machine.
type Plan struct {
	Machine    string      `json:"machine"`
	Operations []Operation `json:"operations"`
	Prune      []Prune     `json:"prune"`
	Updates    Updates     `json:"updates"`
	// Complete is false while any operation is blocked.
	Complete bool   `json:"complete"`
	Digest   string `json:"digest"`
}

// Inputs are everything the planner reads. Source runs only read-only
// native previews.
type Inputs struct {
	Resolved *definitions.Resolved
	Root     definitions.Root
	Facts    *facts.Facts
	Source   facts.Source
}

// Build produces the plan. It returns an error only when the facts needed
// for any planning are missing; individual problems become blocked
// operations so the whole picture is still shown.
func Build(in Inputs) (*Plan, error) {
	if in.Resolved == nil || in.Facts == nil {
		return nil, fmt.Errorf("planning needs resolved configuration and facts")
	}
	if !in.Facts.Packages.Known() {
		return nil, fmt.Errorf("installed packages are unknown: %s", in.Facts.Packages.Error)
	}
	if !in.Facts.Repositories.Known() {
		return nil, fmt.Errorf("repositories are unknown: %s", in.Facts.Repositories.Error)
	}
	b := &builder{in: in, installed: map[string]facts.Package{}, enabledRepos: map[string]bool{}}
	for _, p := range in.Facts.Packages.Value {
		b.installed[p.Name] = p
	}
	for _, r := range in.Facts.Repositories.Value {
		if r.Enabled {
			b.enabledRepos[r.ID] = true
		}
	}
	p := &Plan{Machine: in.Resolved.Machine, Complete: true}
	p.Operations = append(p.Operations, b.repositories()...)
	p.Operations = append(p.Operations, b.packages()...)
	p.Operations = append(p.Operations, b.flatpaks()...)
	p.Prune = b.prune()
	p.Updates = b.updates()
	for _, op := range p.Operations {
		if op.Blocked != "" {
			p.Complete = false
		}
	}
	p.Digest = digest(p)
	return p, nil
}

type builder struct {
	in           Inputs
	installed    map[string]facts.Package
	enabledRepos map[string]bool
}

// DNFRepoIDs returns the repository IDs a declared repository creates on the
// host. Nimbus writes nimbus-<id>.repo itself; a release package and a COPR
// use the IDs their own tooling creates.
func DNFRepoIDs(id string, r definitions.Repository) []string {
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

// expectedRepos returns the host repository IDs a package with this prefix
// may come from.
func (b *builder) expectedRepos(prefix string) []string {
	if prefix == definitions.PrefixDNF {
		return []string{"fedora", "updates", "updates-testing", "fedora-cisco-openh264"}
	}
	return DNFRepoIDs(prefix, b.in.Root.Repositories[prefix])
}

func (b *builder) repoReady(prefix string) bool {
	if prefix == definitions.PrefixDNF {
		return true
	}
	for _, id := range b.expectedRepos(prefix) {
		if b.enabledRepos[id] {
			return true
		}
	}
	return false
}

func (b *builder) repositories() []Operation {
	var ops []Operation
	for _, id := range b.in.Resolved.Repositories {
		r := b.in.Root.Repositories[id]
		if r.Kind == "flatpak" {
			if op, missing := b.flatpakRemote(id, r); missing {
				ops = append(ops, op)
			}
			continue
		}
		if b.repoReady(id) {
			continue
		}
		op := Operation{
			ID: "repository:" + id, Kind: KindRepository, Action: ActionEnable, Risk: RiskMedium,
			Summary: fmt.Sprintf("enable repository %s (%s)", id, repoLocation(r)),
			Paths:   b.repoPaths(id),
			Steps:   repositorySteps(id, r),
		}
		if foreign := b.foreignRepo(id, r); foreign != "" {
			op.Blocked = foreign
		}
		ops = append(ops, op)
	}
	return ops
}

// foreignRepo reports an enabled repository with the maker's own ID for a
// repository Nimbus would write itself. Unknown ownership blocks: Nimbus
// neither duplicates the source nor takes the file over silently.
func (b *builder) foreignRepo(id string, r definitions.Repository) string {
	if r.Kind != "dnf" || r.ReleasePackage != "" {
		return ""
	}
	for _, have := range b.in.Facts.Repositories.Value {
		if have.Enabled && have.ID == id {
			return fmt.Sprintf("repository %s is already enabled through %s, which Nimbus does not own; remove that file or keep the repository unmanaged", id, have.File)
		}
	}
	return ""
}

func repoLocation(r definitions.Repository) string {
	switch {
	case r.ReleasePackage != "":
		return r.ReleasePackage
	case r.Kind == "copr":
		return "copr " + r.Project
	default:
		return r.BaseURL
	}
}

// repositorySteps renders the exact enabling steps per repository kind.
func repositorySteps(id string, r definitions.Repository) []Step {
	fp := definitions.NormalizeFingerprint(r.Key)
	keyPath := "/etc/pki/rpm-gpg/RPM-GPG-KEY-nimbus-" + id
	switch {
	case r.Kind == "copr":
		return []Step{
			{Description: "enable the COPR through DNF", Argv: []string{"dnf5", "copr", "enable", "-y", r.Project}, Privileged: true},
			{Description: "verify the imported key fingerprint is " + fp, Argv: []string{"gpg", "--batch", "--show-keys", "--with-colons", "/etc/pki/rpm-gpg/RPM-GPG-KEY-copr-" + strings.ReplaceAll(r.Project, "/", "-")}},
			{Description: fmt.Sprintf("set priority=%d in the COPR repository file", *r.Priority)},
		}
	case r.ReleasePackage != "":
		return []Step{
			{Description: "download " + r.ReleasePackage + " and verify sha256 " + r.SHA256},
			{Description: "extract the signing key from the verified package", Argv: []string{"rpm2archive", "<verified package>"}},
			{Description: "verify the extracted key fingerprint is " + fp, Argv: []string{"gpg", "--batch", "--show-keys", "--with-colons", "<extracted key>"}},
			{Description: "import the verified key", Argv: []string{"rpm", "--import", "<extracted key>"}, Privileged: true},
			{Description: "install the release package with signature checking on", Argv: []string{"dnf5", "install", "-y", "<verified package>"}, Privileged: true},
			{Description: fmt.Sprintf("set priority=%d in the repository files it wrote", *r.Priority)},
		}
	default:
		keySource := "download " + r.KeyURL
		if r.KeyFile != "" {
			keySource = "read system/" + r.KeyFile + " from the checkout"
		}
		return []Step{
			{Description: keySource + " and verify its fingerprint is " + fp, Argv: []string{"gpg", "--batch", "--show-keys", "--with-colons", "<verified key>"}},
			{Description: "install the verified key", Argv: []string{"install", "-m", "0644", "<verified key>", keyPath}, Privileged: true},
			{Description: "import the key into the RPM database", Argv: []string{"rpm", "--import", keyPath}, Privileged: true},
			{Description: fmt.Sprintf("write /etc/yum.repos.d/nimbus-%s.repo: baseurl=%s gpgcheck=1 repo_gpgcheck=1 priority=%d", id, r.BaseURL, *r.Priority)},
		}
	}
}

func (b *builder) repoPaths(id string) []string {
	var paths []string
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == id || (p.Prefix == definitions.PrefixFlatpak && b.in.Root.Repositories[id].Kind == "flatpak") {
			paths = append(paths, p.Canonical)
		}
	}
	return paths
}

func (b *builder) flatpakRemote(id string, r definitions.Repository) (Operation, bool) {
	if b.in.Facts.Flatpak.Known() {
		for _, remote := range b.in.Facts.Flatpak.Value.Remotes {
			if remote.Name == id {
				return Operation{}, false
			}
		}
	}
	op := Operation{
		ID: "flatpak-remote:" + id, Kind: KindFlatpakRemote, Action: ActionEnable, Risk: RiskMedium,
		Summary: "add system Flatpak remote " + id, Paths: b.repoPaths(id),
		Steps: []Step{
			{Description: "add the remote", Argv: []string{"flatpak", "remote-add", "--if-not-exists", "--system", id, r.URL}, Privileged: true},
			{Description: "verify the remote key fingerprint is " + definitions.NormalizeFingerprint(r.Key)},
		},
	}
	if !b.in.Facts.Flatpak.Known() {
		op.Blocked = "Flatpak state is unknown: " + b.in.Facts.Flatpak.Error
	}
	return op, true
}

func (b *builder) flatpakReady() bool {
	if !b.in.Facts.Flatpak.Known() {
		return false
	}
	id := flatpakRepoID(b.in.Root)
	for _, remote := range b.in.Facts.Flatpak.Value.Remotes {
		if remote.Name == id {
			return true
		}
	}
	return false
}

func flatpakRepoID(root definitions.Root) string {
	for id, r := range root.Repositories {
		if r.Kind == "flatpak" {
			return id
		}
	}
	return ""
}

func (b *builder) packages() []Operation {
	var adopt, pending []Operation
	var install []definitions.ResolvedPackage
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == definitions.PrefixFlatpak {
			continue
		}
		if inst, ok := b.installed[p.Name]; ok {
			op := Operation{ID: "package:" + p.Canonical, Kind: KindPackage, Action: ActionAdopt, Risk: RiskLow,
				Summary: fmt.Sprintf("adopt %s %s, already installed", p.Name, inst.EVR()), Paths: p.Paths}
			if !contains(b.expectedRepos(p.Prefix), inst.FromRepo) && inst.FromRepo != "" {
				op.Notes = append(op.Notes, fmt.Sprintf("installed from repository %q rather than %s", inst.FromRepo, strings.Join(b.expectedRepos(p.Prefix), " or ")))
			}
			adopt = append(adopt, op)
			continue
		}
		if !b.repoReady(p.Prefix) {
			pending = append(pending, Operation{ID: "package:" + p.Canonical, Kind: KindPackage, Action: ActionInstall, Risk: RiskLow,
				Summary: "install " + p.Name, Paths: p.Paths,
				Blocked: fmt.Sprintf("repository %s is not enabled yet; the exact transaction is planned after it is", p.Prefix)})
			continue
		}
		install = append(install, p)
	}
	ops := adopt
	if len(install) > 0 {
		ops = append(ops, b.installTransaction(install))
	}
	if op, ok := b.removeTransaction(); ok {
		ops = append(ops, op)
	}
	return append(ops, pending...)
}

// installTransaction previews one DNF transaction for every installable
// package and refuses anything the definitions did not ask for.
func (b *builder) installTransaction(pkgs []definitions.ResolvedPackage) Operation {
	names := make([]string, 0, len(pkgs))
	byName := map[string]definitions.ResolvedPackage{}
	var paths []string
	for _, p := range pkgs {
		names = append(names, p.Name)
		byName[p.Name] = p
		paths = append(paths, p.Canonical)
	}
	sort.Strings(names)
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
		Summary: fmt.Sprintf("install %d packages through one DNF transaction", len(names)), Paths: paths,
		Steps: []Step{{Description: "run the reviewed transaction", Argv: append([]string{"dnf5", "-y"}, args...), Privileged: true}}}
	out, err := b.in.Source.Run("dnf5", append([]string{"--assumeno", "--cacheonly"}, args...)...)
	tx, perr := ParsePreview(out)
	if perr != nil {
		if err != nil && len(strings.TrimSpace(string(out))) == 0 {
			op.Blocked = "dnf5 preview failed: " + err.Error()
		} else {
			op.Blocked = perr.Error()
		}
		return op
	}
	op.Transaction = tx
	var problems []string
	for _, row := range tx.Packages {
		switch row.Section {
		case "installing":
			p, wanted := byName[row.Name]
			if !wanted {
				problems = append(problems, "would install "+row.Name+", which nothing selects")
			} else if !contains(b.expectedRepos(p.Prefix), row.Repository) {
				problems = append(problems, fmt.Sprintf("%s would come from repository %s, not %s", row.Name, row.Repository, strings.Join(b.expectedRepos(p.Prefix), " or ")))
			}
		case "installing dependencies", "installing weak dependencies":
		case "removing", "removing dependent packages", "removing unused dependencies", "replacing":
			if !removes[row.Name] {
				problems = append(problems, "would remove "+row.Name+", which no component declares in removes")
			}
		case "upgrading", "downgrading", "reinstalling":
			problems = append(problems, fmt.Sprintf("would %s %s; run nimbus upgrade first", strings.TrimSuffix(row.Section, "ing")+"e", row.Name))
		}
	}
	if len(problems) > 0 {
		op.Blocked = "the transaction goes beyond the definitions: " + strings.Join(problems, "; ")
	}
	if tx.NothingToDo {
		op.Blocked = "dnf5 reports nothing to do although packages are missing"
	}
	return op
}

// removeTransaction previews the removal of declared removes that are still
// installed and not already erased by the install transaction.
func (b *builder) removeTransaction() (Operation, bool) {
	var names, paths []string
	for _, name := range b.in.Resolved.Removes {
		if _, ok := b.installed[name]; ok {
			names = append(names, name)
			paths = append(paths, "removes:"+name)
		}
	}
	if len(names) == 0 {
		return Operation{}, false
	}
	sort.Strings(names)
	op := Operation{ID: "packages:remove", Kind: KindPackage, Action: ActionRemove, Risk: RiskMedium,
		Summary: fmt.Sprintf("remove %s, replaced by declared packages", strings.Join(names, ", ")), Paths: paths,
		Steps: []Step{{Description: "run the reviewed removal", Argv: append([]string{"dnf5", "-y", "remove"}, names...), Privileged: true}}}
	out, err := b.in.Source.Run("dnf5", append([]string{"--assumeno", "--cacheonly", "remove"}, names...)...)
	tx, perr := ParsePreview(out)
	if perr != nil {
		if err != nil && len(strings.TrimSpace(string(out))) == 0 {
			op.Blocked = "dnf5 preview failed: " + err.Error()
		} else {
			op.Blocked = perr.Error()
		}
		return op, true
	}
	op.Transaction = tx
	declared := map[string]bool{}
	for _, n := range names {
		declared[n] = true
	}
	var extra []string
	for _, row := range tx.Packages {
		if !declared[row.Name] {
			extra = append(extra, row.Name)
		}
	}
	if len(extra) > 0 {
		op.Blocked = "removal would also remove " + strings.Join(extra, ", ") + ", which no component declares"
	}
	return op, true
}

func (b *builder) flatpaks() []Operation {
	var ops []Operation
	installed := map[string]facts.FlatpakApp{}
	if b.in.Facts.Flatpak.Known() {
		for _, app := range b.in.Facts.Flatpak.Value.Apps {
			installed[app.ID] = app
		}
	}
	remote := flatpakRepoID(b.in.Root)
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix != definitions.PrefixFlatpak {
			continue
		}
		if app, ok := installed[p.Name]; ok {
			ops = append(ops, Operation{ID: "flatpak:" + p.Name, Kind: KindFlatpak, Action: ActionAdopt, Risk: RiskLow,
				Summary: fmt.Sprintf("adopt Flatpak %s %s from %s, already installed", p.Name, app.Version, app.Origin), Paths: p.Paths})
			continue
		}
		op := Operation{ID: "flatpak:" + p.Name, Kind: KindFlatpak, Action: ActionInstall, Risk: RiskLow,
			Summary: "install Flatpak " + p.Name, Paths: p.Paths,
			Steps: []Step{{Description: "install from the system remote", Argv: []string{"flatpak", "install", "--system", "--noninteractive", remote, p.Name}, Privileged: true}}}
		if !b.flatpakReady() {
			op.Blocked = fmt.Sprintf("Flatpak remote %s is not present yet", remote)
		}
		ops = append(ops, op)
	}
	return ops
}

func (b *builder) prune() []Prune {
	desired := map[string]bool{}
	for _, p := range b.in.Resolved.Packages {
		desired[p.Name] = true
	}
	for _, r := range b.in.Resolved.Removes {
		desired[r] = true
	}
	var out []Prune
	for _, p := range b.in.Facts.Packages.Value {
		if p.Reason == "user" && !desired[p.Name] {
			out = append(out, Prune{Name: p.Name, EVR: p.EVR(), Repository: p.FromRepo})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if out == nil {
		out = []Prune{}
	}
	return out
}

func (b *builder) updates() Updates {
	out, err := b.in.Source.Run("dnf5", "--cacheonly", "check-upgrade")
	ups, perr := ParseCheckUpgrade(out)
	if perr != nil {
		return Updates{Available: []Upgrade{}, Unavailable: perr.Error()}
	}
	if len(ups) == 0 && err != nil && !strings.Contains(string(out), "Repositories loaded") {
		return Updates{Available: []Upgrade{}, Unavailable: "update information unavailable: " + err.Error() + "; run dnf5 makecache or nimbus plan --refresh"}
	}
	if ups == nil {
		ups = []Upgrade{}
	}
	return Updates{Available: ups}
}

// digest hashes the canonical apply section: operations with their steps
// and transactions, not the informational prune and update sections.
func digest(p *Plan) string {
	type canon struct {
		Machine    string      `json:"machine"`
		Operations []Operation `json:"operations"`
	}
	data, _ := json.Marshal(canon{Machine: p.Machine, Operations: p.Operations})
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
