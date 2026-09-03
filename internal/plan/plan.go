package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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
	ActionRepair  = "repair"
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
	// After names the operation this one waits for. Apply runs the earlier
	// operation, then re-plans so the exact transaction can be shown; a
	// pending operation does not make the plan incomplete.
	After string `json:"after,omitempty"`
	// Blocked explains a problem the owner must resolve. A blocked
	// operation keeps the plan incomplete.
	Blocked string `json:"blocked,omitempty"`
	// Notes are observations that do not block.
	Notes []string `json:"notes,omitempty"`
}

// Prune is an installed, user-requested package that nothing desires. It is
// shown only with --prune and removed only by apply --prune.
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
	Machine string `json:"machine"`
	// Definitions is the definition digest the plan was built from, and
	// Checkout the Git identity of the checkout; both bind the plan digest.
	Definitions string         `json:"definitions"`
	Checkout    facts.Checkout `json:"checkout"`
	Operations  []Operation    `json:"operations"`
	Prune       []Prune        `json:"prune"`
	Updates     Updates        `json:"updates"`
	// Complete is false while any operation is blocked.
	Complete bool   `json:"complete"`
	Digest   string `json:"digest"`
}

// Inputs are everything the planner reads. Source runs only read-only
// native previews.
type Inputs struct {
	Resolved    *definitions.Resolved
	Root        definitions.Root
	Definitions string // the checkout's definition digest
	Facts       *facts.Facts
	Source      facts.Source
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
	b := &builder{in: in, installed: map[string]facts.Package{}, repos: map[string][]facts.Repository{}, ready: map[string]bool{}, blockedRepo: map[string]string{}}
	for _, p := range in.Facts.Packages.Value {
		b.installed[p.Name] = p
	}
	for _, r := range in.Facts.Repositories.Value {
		b.repos[r.ID] = append(b.repos[r.ID], r)
	}
	p := &Plan{Machine: in.Resolved.Machine, Definitions: in.Definitions, Checkout: in.Facts.Checkout.Value, Complete: true}
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
	in        Inputs
	installed map[string]facts.Package
	repos     map[string][]facts.Repository
	// ready records declared repositories the host already provides
	// correctly; blockedRepo records why one cannot be used.
	ready       map[string]bool
	blockedRepo map[string]string
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

// fedoraRepos are the host repository IDs a bare Fedora package may come
// from.
var fedoraRepos = []string{"fedora", "updates", "updates-testing", "fedora-cisco-openh264"}

// expectedRepos returns the host repository IDs a package with this prefix
// may come from.
func (b *builder) expectedRepos(prefix string) []string {
	if prefix == definitions.PrefixDNF {
		return fedoraRepos
	}
	return DNFRepoIDs(prefix, b.in.Root.Repositories[prefix])
}

// declaredRepoIDs returns every host repository ID a declared non-Fedora
// repository would use, for recognizing a package installed from one.
func (b *builder) declaredRepoIDs() map[string]string {
	out := map[string]string{}
	for id, r := range b.in.Root.Repositories {
		for _, host := range DNFRepoIDs(id, r) {
			out[host] = id
		}
		if r.Kind == "dnf" && r.ReleasePackage == "" {
			out[id] = id // the maker's own ID, which Nimbus does not own
		}
	}
	return out
}

// inspectRepo decides whether the host already provides a declared DNF or
// COPR repository as declared. It returns ready, a repair description for
// a Nimbus-owned file that drifted, or a blocking problem.
func (b *builder) inspectRepo(id string, r definitions.Repository) (ready bool, repair string, blocked string) {
	ids := DNFRepoIDs(id, r)
	var enabled []facts.Repository
	for _, host := range ids {
		for _, have := range b.repos[host] {
			if have.Enabled {
				enabled = append(enabled, have)
			}
		}
	}
	if len(enabled) == 0 {
		if r.Kind == "dnf" && r.ReleasePackage == "" {
			for _, have := range b.repos[id] {
				if have.Enabled {
					return false, "", fmt.Sprintf("repository %s is already enabled through %s, which Nimbus does not own; remove that file or keep the repository unmanaged", id, have.File)
				}
			}
		}
		return false, "", ""
	}
	var drift []string
	for _, have := range enabled {
		if have.GPGCheck != "1" {
			drift = append(drift, have.ID+" has gpgcheck="+have.GPGCheck)
		}
		if r.Priority != nil && have.Priority != strconv.Itoa(*r.Priority) {
			drift = append(drift, fmt.Sprintf("%s has priority %q, declared %d", have.ID, have.Priority, *r.Priority))
		}
		if r.Kind == "dnf" && r.ReleasePackage == "" {
			if have.File != "nimbus-"+id+".repo" {
				return false, "", fmt.Sprintf("repository %s is provided by %s, not by nimbus-%s.repo, which Nimbus would own; remove that file first", have.ID, have.File, id)
			}
			if have.BaseURL != r.BaseURL {
				drift = append(drift, fmt.Sprintf("%s has baseurl %q, declared %q", have.ID, have.BaseURL, r.BaseURL))
			}
		}
	}
	if len(drift) > 0 {
		if r.Kind == "dnf" && r.ReleasePackage == "" {
			return false, "rewrite nimbus-" + id + ".repo: " + strings.Join(drift, "; "), ""
		}
		return false, "correct the repository file: " + strings.Join(drift, "; "), ""
	}
	return true, "", ""
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
		ready, repair, blocked := b.inspectRepo(id, r)
		if ready {
			b.ready[id] = true
			continue
		}
		op := Operation{
			ID: "repository:" + id, Kind: KindRepository, Action: ActionEnable, Risk: RiskMedium,
			Summary: fmt.Sprintf("enable repository %s (%s)", id, repoLocation(r)),
			Paths:   b.repoPaths(id),
			Steps:   repositorySteps(id, r),
		}
		switch {
		case blocked != "":
			op.Blocked = blocked
			b.blockedRepo[id] = blocked
		case repair != "":
			op.Action, op.Summary = ActionRepair, fmt.Sprintf("repair repository %s: %s", id, repair)
			if r.Kind == "dnf" && r.ReleasePackage == "" {
				op.Steps = repositorySteps(id, r)[3:]
			} else {
				op.Steps = []Step{{Description: repair}}
			}
		}
		ops = append(ops, op)
	}
	return ops
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

// flatpakRemote returns the operation for a missing or mismatched system
// remote. A remote with the declared name but another URL blocks.
func (b *builder) flatpakRemote(id string, r definitions.Repository) (Operation, bool) {
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
		b.blockedRepo[id] = op.Blocked
		return op, true
	}
	for _, remote := range b.in.Facts.Flatpak.Value.Remotes {
		if remote.Name != id {
			continue
		}
		if remote.URL == r.URL || strings.TrimSuffix(remote.URL, "/") == strings.TrimSuffix(strings.TrimSuffix(r.URL, "flathub.flatpakrepo"), "/") {
			b.ready[id] = true
			return Operation{}, false
		}
		op.Blocked = fmt.Sprintf("system remote %s points to %s, not the declared %s; remove or fix it first", id, remote.URL, r.URL)
		b.blockedRepo[id] = op.Blocked
		return op, true
	}
	return op, true
}

func flatpakRepoID(root definitions.Root) string {
	for id, r := range root.Repositories {
		if r.Kind == "flatpak" {
			return id
		}
	}
	return ""
}

// waitOrBlock returns the After or Blocked value for a package whose
// repository is not ready.
func (b *builder) waitOrBlock(repoID, opKind string) (after, blocked string) {
	if msg, ok := b.blockedRepo[repoID]; ok {
		return "", "repository " + repoID + " cannot be used: " + msg
	}
	return opKind + ":" + repoID, ""
}

func (b *builder) packages() []Operation {
	declared := b.declaredRepoIDs()
	var adopt, pending, blocked []Operation
	var install []definitions.ResolvedPackage
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == definitions.PrefixFlatpak {
			continue
		}
		if inst, ok := b.installed[p.Name]; ok {
			op := Operation{ID: "package:" + p.Canonical, Kind: KindPackage, Action: ActionAdopt, Risk: RiskLow,
				Summary: fmt.Sprintf("adopt %s %s, already installed", p.Name, inst.EVR()), Paths: p.Paths}
			if reason := b.adoptionProblem(p, inst, declared); reason != "" {
				op.Blocked = reason
				blocked = append(blocked, op)
				continue
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
	ops := append(adopt, blocked...)
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

// adoptionProblem explains why an installed desired package cannot simply
// be adopted: it came from a repository other than its prefix names. A
// package whose recorded source is the installer, a build, or unknown is
// adoptable, since that is how a fresh Fedora records its base.
func (b *builder) adoptionProblem(p definitions.ResolvedPackage, inst facts.Package, declared map[string]string) string {
	expected := b.expectedRepos(p.Prefix)
	if contains(expected, inst.FromRepo) {
		return ""
	}
	if p.Prefix == definitions.PrefixDNF {
		if owner, ok := declared[inst.FromRepo]; ok {
			return fmt.Sprintf("installed from repository %s (%s), but the definitions select it from Fedora; remove it or change its prefix", inst.FromRepo, owner)
		}
		if inst.FromRepo != "" && !contains(fedoraRepos, inst.FromRepo) && strings.Contains(inst.FromRepo, ":") {
			return fmt.Sprintf("installed from repository %s, which the definitions do not declare; remove it or declare the source", inst.FromRepo)
		}
		return ""
	}
	if inst.FromRepo == "" {
		return ""
	}
	return fmt.Sprintf("installed from repository %s, not from %s; remove it or declare that source", inst.FromRepo, strings.Join(expected, " or "))
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
// installed and that the install transaction does not already erase.
func (b *builder) removeTransaction(installTx *Transaction) (Operation, bool) {
	erased := map[string]bool{}
	if installTx != nil {
		for _, row := range installTx.Packages {
			if strings.HasPrefix(row.Section, "removing") || row.Section == "replacing" {
				erased[row.Name] = true
			}
		}
	}
	var names, paths []string
	for _, name := range b.in.Resolved.Removes {
		if _, ok := b.installed[name]; ok && !erased[name] {
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
			op := Operation{ID: "flatpak:" + p.Name, Kind: KindFlatpak, Action: ActionAdopt, Risk: RiskLow,
				Summary: fmt.Sprintf("adopt Flatpak %s %s from %s, already installed", p.Name, app.Version, app.Origin), Paths: p.Paths}
			if app.Origin != remote {
				op.Blocked = fmt.Sprintf("installed from remote %s, not %s; remove it or declare that remote", app.Origin, remote)
			}
			ops = append(ops, op)
			continue
		}
		op := Operation{ID: "flatpak:" + p.Name, Kind: KindFlatpak, Action: ActionInstall, Risk: RiskLow,
			Summary: "install Flatpak " + p.Name, Paths: p.Paths,
			Steps: []Step{{Description: "install from the system remote", Argv: []string{"flatpak", "install", "--system", "--noninteractive", remote, p.Name}, Privileged: true}}}
		if !b.ready[remote] {
			op.After, op.Blocked = b.waitOrBlock(remote, KindFlatpakRemote)
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

// digest hashes the canonical apply section bound to its inputs: the
// machine, the definition digest, and the operations with their steps and
// transactions. Prune and update information are informational and
// excluded; the checkout commit is reported beside the digest, not inside
// it, so a documentation-only commit does not invalidate an approved plan
// while the definition digest still does.
func digest(p *Plan) string {
	type canon struct {
		Machine     string      `json:"machine"`
		Definitions string      `json:"definitions"`
		Operations  []Operation `json:"operations"`
	}
	data, _ := json.Marshal(canon{Machine: p.Machine, Definitions: p.Definitions, Operations: p.Operations})
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
