package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/facts"
	"github.com/Furyfree/nimbus/internal/state"
)

// Operation kinds, actions, and risk classes.
const (
	KindFile          = "system-file"
	KindService       = "service"
	KindGroup         = "group"
	KindTarget        = "default-target"
	KindTrigger       = "trigger"
	KindDNFConfig     = "dnf-config"
	KindUser          = "user" // a user-scope tool: installer, runtimes, crate
	KindRepository    = "repository"
	KindPackage       = "package"
	KindFlatpakRemote = "flatpak-remote"
	KindFlatpak       = "flatpak"

	ActionEnable  = "enable"
	ActionRepair  = "repair"
	ActionInstall = "install"
	ActionAdopt   = "adopt"
	ActionKeep    = "keep" // managed and unchanged; a receipt already exists
	ActionRemove  = "remove"
	ActionRetire  = "retire" // the package is already gone; only its receipt is retired
	ActionPrune   = "prune"  // removal of an unmanaged package, only with --prune

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
	Source   *state.SourceOwnership `json:"source,omitempty"`
	File     *FileChange            `json:"file,omitempty"`
	Resource *ResourceChange        `json:"resource,omitempty"`
	ID       string                 `json:"id"`
	Kind     string                 `json:"kind"`
	Action   string                 `json:"action"`
	Risk     string                 `json:"risk"`
	Summary  string                 `json:"summary"`
	Paths    []string               `json:"paths,omitempty"`
	// Items are the package references a merged transaction installs, one
	// receipt each; Paths then explains the transaction as a whole.
	Items []string `json:"items,omitempty"`
	// Resolved maps requested names to native RPM name.arch identities from
	// the preview or installed state, including resolved provides.
	Resolved    map[string]string `json:"resolved,omitempty"`
	Steps       []Step            `json:"steps,omitempty"`
	Transaction *Transaction      `json:"transaction,omitempty"`
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

// Prune is an installed, user-requested package that nothing desires, that
// Nimbus did not install, and that existed after Nimbus took over. It is
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
	// RepositoryReconciliation covers vendor repository files that RPM
	// scriptlets may create after the reviewed install or upgrade.
	RepositoryReconciliation string `json:"repository_reconciliation,omitempty"`
	// PruneUnavailable says why prune candidates cannot be known yet.
	PruneUnavailable string  `json:"prune_unavailable,omitempty"`
	Updates          Updates `json:"updates"`
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
	Applied     *state.Applied // receipts and baseline; nil before any apply
	Source      facts.Source
	// Prune promotes the prune candidates into removal operations, as apply
	// --prune does.
	Prune bool
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
	if in.Applied == nil {
		in.Applied = &state.Applied{Receipts: map[string]state.Receipt{}}
	}
	b := &builder{in: in, repos: map[string][]facts.Repository{}, ready: map[string]bool{}, blockedRepo: map[string]string{}, pendingRepo: map[string]bool{}, duplicates: map[string][]string{}}
	for _, r := range in.Facts.Repositories.Value {
		b.repos[r.ID] = append(b.repos[r.ID], r)
	}
	p := &Plan{Machine: in.Resolved.Machine, Definitions: in.Definitions, Checkout: in.Facts.Checkout.Value, Complete: true}
	for _, id := range in.Resolved.Repositories {
		r := in.Root.Repositories[id]
		if r.Kind == "dnf" && r.ReleasePackage == "" {
			p.RepositoryReconciliation = "After package transactions, disable new duplicate providers of declared baseurls through DNF overrides; preserve vendor files and keys, and verify the declared sources."
			break
		}
	}
	p.Operations = append(p.Operations, b.dnfConfig()...)
	p.Operations = append(p.Operations, b.repositories()...)
	var declaredRemovals []Operation
	for _, op := range b.packages() {
		if op.Kind == KindPackage && op.Action == ActionRemove {
			declaredRemovals = append(declaredRemovals, op)
		} else {
			p.Operations = append(p.Operations, op)
		}
	}
	p.Operations = append(p.Operations, b.flatpaks()...)
	p.Operations = append(p.Operations, b.userTools()...)
	p.Operations = append(p.Operations, b.systemResources(p.Operations)...)
	p.Operations = append(p.Operations, declaredRemovals...)
	p.Operations = append(p.Operations, b.ownedRemovals()...)
	p.Operations = append(p.Operations, b.sourceRetirements(p.Operations)...)
	p.Prune = b.prune()
	if in.Prune && (in.Applied == nil || in.Applied.Baseline == nil) {
		p.PruneUnavailable = "prune needs the baseline the first sync records; run sync once first"
	}
	if in.Prune && len(p.Prune) > 0 {
		p.Operations = append(p.Operations, b.pruneTransaction(p.Prune))
	}
	deferPackageRemovalForResources(p.Operations)
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
	in    Inputs
	repos map[string][]facts.Repository
	// ready records declared repositories the host already provides
	// correctly; blockedRepo records why one cannot be used; pendingRepo
	// records one that an earlier operation of this plan provides.
	ready       map[string]bool
	blockedRepo map[string]string
	pendingRepo map[string]bool
	// repoChanges lists the DNF repository operations of this round. A
	// transaction previewed before they run would resolve against other
	// metadata than the one downloaded after them, so every DNF
	// transaction waits for them.
	repoChanges []string
	// duplicates lists, per declared repository, the enabled host
	// repositories under other IDs that serve the declared baseurl, such
	// as the file a maker's package writes at install; each is disabled
	// through an override so the declared one stays the only provider.
	duplicates map[string][]string
}

// installsPackage reports whether the plan installs a bare Fedora package
// that is desired and not yet present.
func (b *builder) installsPackage(name string) bool {
	if _, installed := facts.FindPackage(b.in.Facts.Packages.Value, name); installed {
		return false
	}
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == definitions.PrefixDNF && p.Name == name {
			return true
		}
	}
	return false
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
		// An earlier retirement may have retained the native files while
		// disabling them. Check their identity through the normal inspector
		// before proposing an explicit enable/key reconciliation.
		virtual := *b
		virtual.repos = make(map[string][]facts.Repository, len(b.repos))
		found := false
		for host, repositories := range b.repos {
			virtual.repos[host] = slices.Clone(repositories)
			if slices.Contains(ids, host) {
				for i := range virtual.repos[host] {
					virtual.repos[host][i].Enabled = true
					found = true
				}
			}
		}
		if found {
			_, _, blocked := virtual.inspectRepo(id, r)
			if blocked != "" {
				return false, "", blocked
			}
			return false, "reconcile signing key: repository is disabled", ""
		}
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
	var keyDrift []string
	for _, have := range enabled {
		for _, key := range []string{"baseurl", "metalink", "mirrorlist", "sslverify"} {
			if value, overridden := have.OverrideOptions[key]; overridden {
				want := have.Options[key]
				if r.Kind == "dnf" && r.ReleasePackage == "" {
					want = ""
					if key == "baseurl" {
						want = r.BaseURL
					}
				}
				if key == "sslverify" {
					want = "1"
					switch strings.ToLower(value) {
					case "true", "yes":
						value = "1"
					}
				}
				if value != want {
					return false, "", fmt.Sprintf("repository %s has a conflicting %s override in %s; correct the native override before retrying", have.ID, key, strings.Join(have.Overrides, ", "))
				}
			}
		}
		if r.Kind == "dnf" && r.ReleasePackage == "" {
			if have.File != "nimbus-"+id+".repo" {
				return false, "", fmt.Sprintf("repository %s is provided by %s, not by nimbus-%s.repo, which Nimbus would own; remove that file first", have.ID, have.File, id)
			}
			if reason := RepositoryKeyDrift(id, r, have); reason != "" {
				keyDrift = append(keyDrift, have.ID+": "+reason)
			}
			if fileDrift := ownedFileDrift(id, r, have); len(fileDrift) > 0 {
				drift = append(drift, fileDrift...)
				continue
			}
		}
		if r.Kind != "dnf" || r.ReleasePackage != "" {
			if reason := RepositoryKeyDrift(id, r, have); reason != "" {
				keyDrift = append(keyDrift, have.ID+": "+reason)
			}
		}
		// The effective values include overrides below repos.override.d,
		// which is where a maker's file is corrected and where a file
		// Nimbus owns could still be changed from outside.
		if have.GPGCheck != "1" {
			drift = append(drift, have.ID+" has gpgcheck="+have.GPGCheck)
		}
		if r.Priority != nil && have.Priority != strconv.Itoa(*r.Priority) {
			drift = append(drift, fmt.Sprintf("%s has priority %q, declared %d", have.ID, have.Priority, *r.Priority))
		}
	}
	if r.Kind == "dnf" && r.ReleasePackage == "" {
		for _, host := range sortedKeys(b.repos) {
			if contains(ids, host) {
				continue
			}
			for _, have := range b.repos[host] {
				if have.Enabled && have.BaseURL == r.BaseURL {
					b.duplicates[id] = append(b.duplicates[id], host)
					drift = append(drift, fmt.Sprintf("%s from %s also serves the declared baseurl and is disabled through an override", host, have.File))
				}
			}
		}
	}
	if len(keyDrift) > 0 {
		return false, "reconcile signing key: " + strings.Join(append(keyDrift, drift...), "; "), ""
	}
	if len(drift) > 0 {
		switch {
		case len(b.duplicates[id]) == len(drift):
			return false, strings.Join(drift, "; "), ""
		case r.Kind == "dnf" && r.ReleasePackage == "":
			return false, "rewrite nimbus-" + id + ".repo: " + strings.Join(drift, "; "), ""
		}
		return false, "correct the repository file: " + strings.Join(drift, "; "), ""
	}
	return true, "", ""
}

// disableDuplicateSteps renders the override that disables each host
// repository serving a declared baseurl under another ID.
func (b *builder) disableDuplicateSteps(id string) []Step {
	var opts []string
	for _, host := range b.duplicates[id] {
		opts = append(opts, host+".enabled=0")
	}
	if len(opts) == 0 {
		return nil
	}
	return []Step{{Description: DisableDuplicateDescription, Argv: append([]string{"dnf5", "config-manager", "setopt"}, opts...), Privileged: true}}
}

// DisableDuplicateDescription marks the override step that disables a
// duplicate host repository; apply runs it after the repository's own steps.
const DisableDuplicateDescription = "disable the duplicate repository through a DNF override"

// IsReleasePackage reports whether an installed package is the release
// package of a declared repository, which Nimbus installs while enabling
// it and which therefore belongs to that repository, never to prune.
func IsReleasePackage(root definitions.Root, name string) bool {
	for _, r := range root.Repositories {
		if r.ReleasePackage != "" && ReleasePackageName(r.ReleasePackage) == name {
			return true
		}
	}
	return false
}

// ReleasePackageName derives the package name from a release package URL
// such as .../rpmfusion-free-release-44.noarch.rpm: the file name without
// .rpm, the architecture, and the version and release fields, which are
// the trailing dash-separated fields that start with a digit.
func ReleasePackageName(url string) string {
	name := strings.TrimSuffix(path.Base(url), ".rpm")
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	for range 2 {
		i := strings.LastIndexByte(name, '-')
		if i <= 0 || i+1 >= len(name) || name[i+1] < '0' || name[i+1] > '9' {
			break
		}
		name = name[:i]
	}
	return name
}

// DNFDropIn renders the [dnf] table of nimbus.toml as the libdnf5 drop-in
// Nimbus owns: sorted keys, booleans as DNF spells them. It is empty when
// nothing is declared.
func DNFDropIn(root definitions.Root) string {
	if len(root.DNF) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Written by Nimbus from the [dnf] table of nimbus.toml; edit that instead.\n[main]\n")
	for _, key := range sortedKeys(root.DNF) {
		fmt.Fprintf(&b, "%s=%s\n", key, dnfValue(root.DNF[key]))
	}
	return b.String()
}

func dnfValue(v any) string {
	switch v := v.(type) {
	case bool:
		if v {
			return "True"
		}
		return "False"
	default:
		return fmt.Sprint(v)
	}
}

// DNFDropInPlaceholder stands for the rendered drop-in in the plan; apply
// writes the file to its stage directory and fills the path in.
const DNFDropInPlaceholder = "<rendered drop-in>"

// dnfConfig plans the libdnf5 drop-in first, so every transaction that
// follows downloads with the declared settings.
func (b *builder) dnfConfig() []Operation {
	const id = "dnf:config"
	want := DNFDropIn(b.in.Root)
	_, managed := b.in.Applied.Receipts[id]
	op := Operation{ID: id, Kind: KindDNFConfig, Risk: RiskLow, Paths: []string{"nimbus.toml:dnf"}}
	if !b.in.Facts.DNFDropIn.Known() {
		op.Action, op.Summary = ActionInstall, "configure DNF through "+facts.DNFDropInPath
		op.Blocked = "the DNF drop-in cannot be read: " + b.in.Facts.DNFDropIn.Error
		return []Operation{op}
	}
	have := b.in.Facts.DNFDropIn.Value
	switch {
	case want == "" && have == "":
		return nil
	case want == "" && !managed:
		// A file Nimbus never wrote is not Nimbus's to remove.
		return nil
	case want == "":
		op.Action, op.Summary = ActionRemove, "remove "+facts.DNFDropInPath+", no longer declared"
		op.Steps = []Step{{Description: "remove the drop-in", Argv: []string{"rm", "-f", facts.DNFDropInPath}, Privileged: true}}
		return []Operation{op}
	case have == want && managed:
		op.Action, op.Summary = ActionKeep, facts.DNFDropInPath+" is managed and as declared"
		return []Operation{op}
	case have == want:
		op.Action, op.Summary = ActionAdopt, "adopt "+facts.DNFDropInPath+", already as declared"
		return []Operation{op}
	case have == "":
		op.Action, op.Summary = ActionInstall, "configure DNF through "+facts.DNFDropInPath
	default:
		op.Action, op.Summary = ActionRepair, "rewrite "+facts.DNFDropInPath+", which differs from the declared options"
	}
	for _, key := range sortedKeys(b.in.Root.DNF) {
		op.Steps = append(op.Steps, Step{Description: "set " + key + "=" + dnfValue(b.in.Root.DNF[key])})
	}
	op.Steps = append(op.Steps, Step{Description: "write the drop-in", Argv: []string{"install", "-m", "0644", DNFDropInPlaceholder, facts.DNFDropInPath}, Privileged: true})
	return []Operation{op}
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
			switch {
			case strings.HasPrefix(repair, "reconcile signing key:"):
				op.Steps = repositorySteps(id, r)
				if r.Kind == "dnf" && r.ReleasePackage == "" {
					op.Steps[len(op.Steps)-1] = AddRepoStep(id, r, true)
					op.Steps = append(op.Steps, PrioritySteps(id, r)...)
				} else {
					var steps []Step
					for _, step := range op.Steps {
						if len(step.Argv) > 1 && step.Argv[0] == "dnf5" && (step.Argv[1] == "install" || step.Argv[1] == "copr") {
							continue
						}
						steps = append(steps, step)
					}
					op.Steps = steps
				}
			case strings.HasPrefix(repair, "rewrite "):
				op.Steps = []Step{AddRepoStep(id, r, true)}
			case strings.HasPrefix(repair, "correct "):
				op.Steps = PrioritySteps(id, r)
			default:
				op.Steps = nil
			}
			op.Steps = append(op.Steps, b.disableDuplicateSteps(id)...)
		}
		if op.Blocked == "" {
			b.repoChanges = append(b.repoChanges, op.ID)
		}
		op.Source = b.sourceOwnership(op, DNFRepoIDs(id, r))
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

// KeyPath is where a verified key for a Nimbus-owned repository lives.
func KeyPath(id string) string { return "/etc/pki/rpm-gpg/RPM-GPG-KEY-nimbus-" + id }

// PrioritySteps renders dnf5 config-manager setopt for every host ID of a
// repository, which writes an override file instead of touching the
// maker's repository file. The override enables signature checking and
// points to the verified local key that the enable or key repair installed.
func PrioritySteps(id string, r definitions.Repository) []Step {
	var opts []string
	for _, host := range DNFRepoIDs(id, r) {
		opts = append(opts, host+".enabled=1", host+".gpgcheck=1", host+".gpgkey=file://"+KeyPath(id))
		if r.Priority != nil {
			opts = append(opts, fmt.Sprintf("%s.priority=%d", host, *r.Priority))
		}
	}
	return []Step{{Description: "set signature checking, the verified local key and priority through a DNF override", Argv: append([]string{"dnf5", "config-manager", "setopt"}, opts...), Privileged: true}}
}

// CheckRepository decides whether the host provides a declared DNF or COPR
// repository as declared. Apply uses it to verify an enable or repair.
func CheckRepository(root definitions.Root, id string, repos []facts.Repository) (ready bool, repair string, blocked string) {
	b := &builder{in: Inputs{Root: root}, repos: map[string][]facts.Repository{}, duplicates: map[string][]string{}}
	for _, r := range repos {
		b.repos[r.ID] = append(b.repos[r.ID], r)
	}
	return b.inspectRepo(id, root.Repositories[id])
}

// RepoOption is one key of the repository file Nimbus owns.
type RepoOption struct{ Key, Value string }

// OwnedRepoOptions is the complete content of nimbus-<id>.repo: what
// AddRepoStep writes and what inspectRepo verifies, so the two cannot
// drift apart. A key the file has beyond these is drift too.
func OwnedRepoOptions(id string, r definitions.Repository) []RepoOption {
	opts := []RepoOption{
		{"name", id + " (Nimbus)"},
		{"enabled", "1"},
		{"baseurl", r.BaseURL},
		{"gpgcheck", "1"},
		{"gpgkey", "file://" + KeyPath(id)},
	}
	if r.Priority != nil {
		opts = append(opts, RepoOption{"priority", strconv.Itoa(*r.Priority)})
	}
	return opts
}

// ownedFileDrift compares a section of nimbus-<id>.repo with what Nimbus
// would write.
func ownedFileDrift(id string, r definitions.Repository, have facts.Repository) []string {
	var drift []string
	expected := map[string]bool{}
	for _, o := range OwnedRepoOptions(id, r) {
		expected[o.Key] = true
		switch got, ok := have.Options[o.Key]; {
		case !ok:
			drift = append(drift, fmt.Sprintf("%s lacks %s=%s", have.File, o.Key, o.Value))
		case got != o.Value:
			drift = append(drift, fmt.Sprintf("%s has %s=%s, declared %s", have.File, o.Key, got, o.Value))
		}
	}
	for _, key := range sortedKeys(have.Options) {
		if !expected[key] {
			drift = append(drift, fmt.Sprintf("%s has %s=%s, which Nimbus does not write", have.File, key, have.Options[key]))
		}
	}
	return drift
}

// AddRepoStep renders the native command that writes nimbus-<id>.repo.
func AddRepoStep(id string, r definitions.Repository, overwrite bool) Step {
	argv := []string{"dnf5", "config-manager", "addrepo", "--id=nimbus-" + id}
	for _, o := range OwnedRepoOptions(id, r) {
		argv = append(argv, "--set="+o.Key+"="+o.Value)
	}
	if overwrite {
		argv = append(argv, "--overwrite")
	}
	return Step{Description: "write /etc/yum.repos.d/nimbus-" + id + ".repo through DNF", Argv: argv, Privileged: true}
}

// repositorySteps renders the exact enabling steps per repository kind.
// Placeholders in angle brackets are paths apply fills in from its stage
// directory after the internal verification step succeeds.
func repositorySteps(id string, r definitions.Repository) []Step {
	fp := definitions.NormalizeFingerprint(r.Key)
	switch {
	case r.Kind == "copr":
		return append([]Step{
			{Description: "download the COPR key and verify its fingerprint is " + fp, Argv: append([]string{"gpg"}, facts.KeyInspectArgs("<verified key>")...)},
			{Description: "install the verified key", Argv: []string{"install", "-m", "0644", "<verified key>", KeyPath(id)}, Privileged: true},
			{Description: "import the verified key into the RPM database", Argv: []string{"rpm", "--import", KeyPath(id)}, Privileged: true},
			{Description: "enable the COPR through DNF", Argv: []string{"dnf5", "copr", "enable", "-y", r.Project}, Privileged: true},
		}, PrioritySteps(id, r)...)
	case r.ReleasePackage != "":
		return append([]Step{
			{Description: "download " + r.ReleasePackage + " and verify sha256 " + r.SHA256},
			{Description: "unpack the verified package to find its key", Argv: []string{"rpm2archive", "<verified package>"}},
			{Description: "verify the extracted key fingerprint is " + fp, Argv: append([]string{"gpg"}, facts.KeyInspectArgs("<extracted key>")...)},
			{Description: "install the verified key", Argv: []string{"install", "-m", "0644", "<extracted key>", KeyPath(id)}, Privileged: true},
			{Description: "import the verified key", Argv: []string{"rpm", "--import", KeyPath(id)}, Privileged: true},
			{Description: "install the release package with signature checking on", Argv: []string{"dnf5", "install", "-y", "<verified package>"}, Privileged: true},
		}, PrioritySteps(id, r)...)
	default:
		keySource := "download " + r.KeyURL
		if r.KeyFile != "" {
			keySource = "read system/" + r.KeyFile + " from the checkout"
		}
		return []Step{
			{Description: keySource + " and verify its fingerprint is " + fp, Argv: append([]string{"gpg"}, facts.KeyInspectArgs("<verified key>")...)},
			{Description: "install the verified key", Argv: []string{"install", "-m", "0644", "<verified key>", KeyPath(id)}, Privileged: true},
			{Description: "import the key into the RPM database", Argv: []string{"rpm", "--import", KeyPath(id)}, Privileged: true},
			AddRepoStep(id, r, false),
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
			{Description: "download " + r.URL + " and verify its sole primary key fingerprint is " + definitions.NormalizeFingerprint(r.Key)},
			{Description: "add the verified remote", Argv: []string{"flatpak", "remote-add", "--if-not-exists", "--system", "--from", id, "<verified remote definition>"}, Privileged: true},
			{Description: "verify the installed remote URL, signature checking and key fingerprint"},
		},
	}
	op.Source = b.sourceOwnership(op, []string{id})
	if !b.in.Facts.Flatpak.Known() {
		if b.in.Facts.Commands["flatpak"] == "" && b.installsPackage("flatpak") {
			// flatpak itself arrives with this plan's install transaction;
			// the remote and its applications wait for it.
			op.After = "packages:install"
			b.pendingRepo[id] = true
			return op, true
		}
		op.Blocked = "Flatpak state is unknown: " + b.in.Facts.Flatpak.Error
		b.blockedRepo[id] = op.Blocked
		return op, true
	}
	for _, remote := range b.in.Facts.Flatpak.Value.Remotes {
		if remote.Name != id {
			continue
		}
		if remote.URL == r.URL || strings.TrimSuffix(remote.URL, "/") == strings.TrimSuffix(strings.TrimSuffix(r.URL, "flathub.flatpakrepo"), "/") {
			if reason := FlatpakKeyDrift(r, remote); reason != "" {
				op.Blocked = "system remote " + id + ": " + reason + "; inspect and correct its signing keys with the native Flatpak tools before retrying"
				b.blockedRepo[id] = op.Blocked
				return op, true
			}
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
	var adopt, pending, blocked []Operation
	var install []definitions.ResolvedPackage
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == definitions.PrefixFlatpak || p.Prefix == definitions.PrefixCargo {
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
				if _, err := receiptPackage(receipt, b.in.Facts.Packages.Value); err != nil {
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

// PackageName returns the native package name of a package operation ID
// such as package:terra:ghostty.
func PackageName(id string) string {
	return id[strings.LastIndexByte(id, ':')+1:]
}

// sourceNote says when an installed desired package comes from a
// repository other than the one its prefix names. The package is adopted
// or kept either way; the owner sees where it came from.
func (b *builder) sourceNote(p definitions.ResolvedPackage, inst facts.Package) string {
	from := inst.FromRepo
	// The installer and local files record no repository worth noting.
	if from == "" || from == "anaconda" || strings.HasPrefix(from, "@") || contains(b.expectedRepos(p.Prefix), from) {
		return ""
	}
	return fmt.Sprintf("%s is installed from %s, not from %s", p.Name, from, strings.Join(b.expectedRepos(p.Prefix), " or "))
}

// installTransaction previews one DNF transaction for every installable
// package and refuses anything the definitions did not ask for.
func (b *builder) installTransaction(pkgs []definitions.ResolvedPackage) Operation {
	names := make([]string, 0, len(pkgs))
	byName := map[string]definitions.ResolvedPackage{}
	// The merged operation is explained by the profiles and components
	// that selected its packages; the packages themselves are its command.
	pathSet := map[string]bool{}
	items := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		names = append(names, p.Name)
		byName[p.Name] = p
		items = append(items, p.Canonical)
		for _, path := range p.Paths {
			pathSet[path] = true
		}
	}
	sort.Strings(names)
	sort.Strings(items)
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)
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
		Summary: fmt.Sprintf("install %d packages through one DNF transaction", len(names)), Paths: paths, Items: items,
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
	// A requested name may be a provide that DNF resolves to a package with
	// another name; such rows are accepted only while a requested name is
	// still unaccounted for, and each substitution is noted for review.
	seen := map[string]bool{}
	for _, row := range tx.Packages {
		if row.Section == "installing" {
			for _, n := range names {
				if (facts.Package{Name: row.Name, Arch: row.Arch}).Matches(n) {
					seen[n] = true
				}
			}
		}
	}
	var unmatched []string
	for _, n := range names {
		if !seen[n] {
			unmatched = append(unmatched, n)
		}
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
		case "installing":
			p, wanted := byName[facts.PackageID(row.Name, row.Arch)]
			if !wanted {
				p, wanted = byName[row.Name]
			}
			if !wanted {
				if len(unmatched) == 0 {
					problems = append(problems, "would install "+row.Name+", which nothing selects")
					continue
				}
				requested := unmatched[0]
				unmatched = unmatched[1:]
				p = byName[requested]
				if op.Resolved == nil {
					op.Resolved = map[string]string{}
				}
				op.Resolved[requested] = facts.PackageID(row.Name, row.Arch)
				op.Notes = append(op.Notes, fmt.Sprintf("%s resolves to the package %s", requested, row.Name))
			}
			if op.Resolved == nil {
				op.Resolved = map[string]string{}
			}
			op.Resolved[p.Name] = facts.PackageID(row.Name, row.Arch)
			if !contains(b.expectedRepos(p.Prefix), row.Repository) {
				problems = append(problems, fmt.Sprintf("%s would come from repository %s, not %s", row.Name, row.Repository, strings.Join(b.expectedRepos(p.Prefix), " or ")))
			}
		case "installing dependencies", "installing weak dependencies":
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
			if strings.HasPrefix(row.Section, "removing") || row.Section == SectionReplaced {
				erased[facts.PackageID(row.Name, row.Arch)] = true
			}
		}
	}
	var names []string
	for _, name := range b.in.Resolved.Removes {
		for _, inst := range b.in.Facts.Packages.Value {
			if inst.Matches(name) && !erased[inst.ID()] {
				names = append(names, name)
				break
			}
		}
	}
	if len(names) == 0 {
		return Operation{}, false
	}
	op := b.previewRemoval("packages:remove", names, fmt.Sprintf("remove %s, replaced by declared packages", strings.Join(names, ", ")))
	for _, n := range names {
		op.Paths = append(op.Paths, "removes:"+n)
	}
	return op, true
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
	for _, problem := range strings.Split(strings.TrimPrefix(blocked, "dnf5 could not resolve the transaction: "), "; ") {
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
	sort.Strings(repos)
	return repos
}

// previewFailure turns a failed dnf5 preview into one readable reason. DNF5
// prints its resolution problems on stderr, which the command error
// carries, so that text is parsed before falling back to the raw error.
func previewFailure(out []byte, runErr, parseErr error) string {
	if runErr != nil {
		if _, err := ParsePreview([]byte(runErr.Error())); err != nil {
			var resolve *ResolveError
			if errors.As(err, &resolve) {
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
			if _, managed := b.in.Applied.Receipts[op.ID]; managed {
				op.Action, op.Summary = ActionKeep, fmt.Sprintf("Flatpak %s %s is managed and unchanged", p.Name, app.Version)
			} else if app.Origin != remote {
				op.Notes = append(op.Notes, fmt.Sprintf("installed from remote %s, not %s", app.Origin, remote))
			}
			ops = append(ops, op)
			continue
		}
		op := Operation{ID: "flatpak:" + p.Name, Kind: KindFlatpak, Action: ActionInstall, Risk: RiskLow,
			Summary: "install Flatpak " + p.Name, Paths: p.Paths,
			Steps: []Step{{Description: "install from the system remote", Argv: []string{"flatpak", "install", "--system", "--noninteractive", remote, p.Name}, Privileged: true}}}
		// The command is exact without a preview, so the application runs
		// in the same round as its remote; it waits only when the remote
		// itself waits for the flatpak package, or is blocked.
		if !b.ready[remote] && (b.pendingRepo[remote] || b.blockedRepo[remote] != "") {
			op.After, op.Blocked = b.waitOrBlock(remote, KindFlatpakRemote)
		}
		ops = append(ops, op)
	}
	return ops
}

// InstalledPackage also recognizes a provide resolved by an earlier verified
// installation. The receipt binds that native identity to this reference.
func InstalledPackage(request string, receipt state.Receipt, packages []facts.Package) (facts.Package, bool) {
	if receipt.Package != "" {
		return facts.FindPackage(packages, receipt.Package)
	}
	return facts.FindPackage(packages, request)
}

func receiptPackage(r state.Receipt, packages []facts.Package) (string, error) {
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
	for id := range matches {
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

// ownedRemovals uses the verified native identity, or an unambiguous legacy
// receipt, to remove packages the definitions no longer select.
func (b *builder) ownedRemovals() []Operation {
	desired := map[string]bool{}
	desiredNative := map[string]bool{}
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == definitions.PrefixFlatpak {
			desired["flatpak:"+p.Name] = true
			continue
		}
		if p.Prefix == definitions.PrefixCargo {
			continue
		}
		id := "package:" + p.Canonical
		desired[id] = true
		if inst, ok := InstalledPackage(p.Name, b.in.Applied.Receipts[id], b.in.Facts.Packages.Value); ok {
			desiredNative[inst.ID()] = true
		}
	}
	var names, receiptIDs []string
	var ops []Operation
	for _, id := range sortedKeys(b.in.Applied.Receipts) {
		r := b.in.Applied.Receipts[id]
		if desired[id] {
			continue
		}
		switch r.Provider {
		case "dnf":
			r.Resource = id
			name, err := receiptPackage(r, b.in.Facts.Packages.Value)
			if err != nil {
				// Explicit selections of every matching architecture transfer
				// ownership without guessing which one the old receipt meant.
				matched, covered := false, true
				for _, inst := range b.in.Facts.Packages.Value {
					if inst.Matches(PackageName(id)) {
						matched = true
						covered = covered && desiredNative[inst.ID()]
					}
				}
				if matched && covered {
					ops = append(ops, Operation{ID: id, Kind: KindPackage, Action: ActionRetire, Risk: RiskLow, Summary: "retire legacy receipt after explicit architecture selections", Paths: []string{"receipt"}})
					continue
				}
				ops = append(ops, Operation{ID: id, Kind: KindPackage, Action: ActionRemove, Risk: RiskMedium, Summary: "resolve ownership of " + PackageName(id), Blocked: err.Error()})
				continue
			}
			_, present := facts.FindPackage(b.in.Facts.Packages.Value, name)
			if desiredNative[name] || !present {
				ops = append(ops, Operation{ID: id, Kind: KindPackage, Action: ActionRetire, Risk: RiskLow, Summary: "retire the receipt of " + name + ", which is absent or selected by another reference", Paths: []string{"receipt"}})
				continue
			}
			names = append(names, name)
			receiptIDs = append(receiptIDs, id)
		case "flatpak":
			name := strings.TrimPrefix(id, "flatpak:")
			if !b.in.Facts.Flatpak.Known() {
				ops = append(ops, Operation{ID: id, Kind: KindFlatpak, Action: ActionRemove, Risk: RiskMedium, Summary: "inspect Flatpak " + name + " before removal", Blocked: b.in.Facts.Flatpak.Error})
				continue
			}
			present := false
			for _, app := range b.in.Facts.Flatpak.Value.Apps {
				present = present || app.ID == name
			}
			if !present {
				ops = append(ops, Operation{ID: id, Kind: KindFlatpak, Action: ActionRetire, Risk: RiskLow, Summary: "retire the receipt of Flatpak " + name + ", which is absent", Paths: []string{"receipt"}})
				continue
			}
			ops = append(ops, Operation{ID: id, Kind: KindFlatpak, Action: ActionRemove, Risk: RiskMedium, Summary: "remove Flatpak " + name + ", no longer selected", Paths: []string{"receipt"}, Steps: []Step{{Description: "remove the application", Argv: []string{"flatpak", "uninstall", "--system", "--noninteractive", name}, Privileged: true}}})
		}
	}
	if len(names) > 0 {
		sort.Strings(names)
		names = slices.Compact(names)
		op := b.previewRemoval("packages:remove-owned", names, "remove "+strings.Join(names, ", ")+", installed by Nimbus and no longer selected")
		op.Paths = receiptIDs
		ops = append([]Operation{op}, ops...)
	}
	return ops
}

// previewRemoval previews the removal of exactly these packages and blocks
// when DNF would remove anything else.
func (b *builder) previewRemoval(id string, names []string, summary string) Operation {
	sort.Strings(names)
	op := Operation{ID: id, Kind: KindPackage, Action: ActionRemove, Risk: RiskMedium, Summary: summary,
		Steps: []Step{{Description: "run the reviewed removal", Argv: append([]string{"dnf5", "-y", "remove", "--no-autoremove"}, names...), Privileged: true}}}
	if b.waitsForRepositories(&op) {
		return op
	}
	out, err := b.in.Source.Run("dnf5", append([]string{"--assumeno", "--cacheonly", "remove", "--no-autoremove"}, names...)...)
	tx, perr := ParsePreview(out)
	if perr != nil {
		op.Blocked = previewFailure(out, err, perr)
		return op
	}
	op.Transaction = tx
	declared := map[string]bool{}
	for _, n := range names {
		declared[n] = true
	}
	var extra []string
	for _, row := range tx.Packages {
		matched := false
		for name := range declared {
			matched = matched || (facts.Package{Name: row.Name, Arch: row.Arch}).Matches(name)
		}
		if !matched {
			extra = append(extra, row.Name)
		}
	}
	if len(extra) > 0 {
		op.Blocked = "removal would also remove " + strings.Join(extra, ", ") + ", which nothing declares"
	}
	return op
}

// pruneTransaction promotes the prune candidates into one removal.
func (b *builder) pruneTransaction(cands []Prune) Operation {
	names := make([]string, 0, len(cands))
	for _, c := range cands {
		names = append(names, c.Name)
	}
	op := b.previewRemoval("packages:prune", names, fmt.Sprintf("prune %d unmanaged packages", len(names)))
	op.Action = ActionPrune
	for _, n := range names {
		op.Paths = append(op.Paths, "unmanaged:"+n)
	}
	return op
}

// prune lists the third bucket: installed with the user reason, not
// desired, not managed by a receipt, and not in the baseline of packages
// that existed before Nimbus took over. Before the first apply there is no
// baseline, so every such package is a candidate.
func (b *builder) prune() []Prune {
	if b.in.Applied == nil || b.in.Applied.Baseline == nil {
		// Without the baseline every pre-existing package would look
		// unmanaged; the first sync records it and prune waits for that.
		return []Prune{}
	}
	desired := map[string]bool{}
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix != definitions.PrefixFlatpak && p.Prefix != definitions.PrefixCargo {
			if inst, ok := InstalledPackage(p.Name, b.in.Applied.Receipts["package:"+p.Canonical], b.in.Facts.Packages.Value); ok {
				desired[inst.ID()] = true
			}
		}
	}
	for _, r := range b.in.Resolved.Removes {
		desired[r] = true
	}
	managed := map[string]bool{}
	for id, r := range b.in.Applied.Receipts {
		if r.Provider == "dnf" {
			if r.Package != "" {
				managed[r.Package] = true
			} else {
				managed[PackageName(id)] = true
			}
		}
	}
	var out []Prune
	for _, p := range b.in.Facts.Packages.Value {
		if p.Reason != "user" || desired[p.ID()] || desired[p.Name] || b.in.Applied.InBaseline(p.ID()) || b.in.Applied.InBaseline(p.Name) || managed[p.ID()] || managed[p.Name] || IsReleasePackage(b.in.Root, p.Name) {
			continue
		}
		out = append(out, Prune{Name: p.ID(), EVR: p.EVR(), Repository: p.FromRepo})
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
		return Updates{Available: []Upgrade{}, Unavailable: "update information unavailable: " + err.Error() + "; sync refreshes it"}
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
		Machine                  string      `json:"machine"`
		Definitions              string      `json:"definitions"`
		Operations               []Operation `json:"operations"`
		RepositoryReconciliation string      `json:"repository_reconciliation,omitempty"`
	}
	data, _ := json.Marshal(canon{Machine: p.Machine, Definitions: p.Definitions, Operations: p.Operations, RepositoryReconciliation: p.RepositoryReconciliation})
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// InstallerScript stands for the downloaded installer in a plan step; apply
// writes the script to its stage directory and fills the path in. HomeDir
// stands for the home directory in a declared install command.
const (
	InstallerScript = "<installer script>"
	HomeDir         = "<home>"
	AfterHandoff    = "chezmoi"
)

// userTools plans the user-scope steps: a maker's installer, the command
// that installs its runtimes once Chezmoi has written their configuration,
// and the crates cargo install builds with the Rust runtime. They run as the
// user, never through sudo, and write no receipt: presence is the record.
func (b *builder) userTools() []Operation {
	var ops []Operation
	if !b.in.Facts.User.Known() {
		// The desired tools are not dropped silently: one blocked
		// operation says why they cannot be planned.
		if len(b.in.Resolved.Installers) > 0 || b.hasPrefix(definitions.PrefixCargo) {
			ops = append(ops, Operation{ID: "user:tools", Kind: KindUser, Action: ActionInstall, Risk: RiskLow,
				Summary: "plan the user-scope tools", Blocked: "the user-scope tool state is unknown: " + b.in.Facts.User.Error})
		}
		return ops
	}
	u := b.in.Facts.User.Value
	var runtimeOps []string
	for _, in := range b.in.Resolved.Installers {
		id := "user:" + in.Component
		binary := b.userFile(u.Home, in.Installer.Binary)
		op := Operation{ID: id, Kind: KindUser, Risk: RiskLow, Paths: in.Paths}
		if binary {
			op.Action, op.Summary = ActionKeep, fmt.Sprintf("%s is installed at ~/%s", in.Component, in.Installer.Binary)
		} else {
			op.Action, op.Summary = ActionInstall, fmt.Sprintf("install %s from %s as the user", in.Component, in.Installer.URL)
			op.Steps = []Step{
				{Description: "download " + in.Installer.URL + " to the stage directory and show its sha256"},
				{Description: "run the installer as the user", Argv: []string{"sh", InstallerScript}},
				{Description: "verify ~/" + in.Installer.Binary + " exists"},
			}
		}
		ops = append(ops, op)
		if len(in.Installer.Install) == 0 {
			continue
		}
		rt := Operation{ID: id + ":install", Kind: KindUser, Action: ActionInstall, Risk: RiskLow, Paths: in.Paths,
			Summary: fmt.Sprintf("install the %s runtimes declared in ~/%s", in.Component, in.Installer.Config),
			Steps:   []Step{{Description: "run as the user, repeatable", Argv: append([]string(nil), in.Installer.Install...)}}}
		switch {
		case !binary:
			rt.After = id
		case !b.userFile(u.Home, in.Installer.Config):
			rt.After = AfterHandoff
			rt.Notes = append(rt.Notes, fmt.Sprintf("~/%s does not exist yet; the Chezmoi handoff writes it", in.Installer.Config))
		}
		runtimeOps = append(runtimeOps, rt.ID)
		ops = append(ops, rt)
	}
	installed := map[string]bool{}
	for _, crate := range u.Crates {
		installed[crate] = true
	}
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix != definitions.PrefixCargo {
			continue
		}
		op := Operation{ID: "package:" + p.Canonical, Kind: KindUser, Risk: RiskLow, Paths: p.Paths}
		switch {
		case installed[p.Name]:
			op.Action, op.Summary = ActionKeep, "crate "+p.Name+" is installed"
		default:
			op.Action, op.Summary = ActionInstall, "cargo install "+p.Name+" as the user"
			op.Steps = []Step{{Description: "build and install the crate", Argv: []string{HomeDir + "/.cargo/bin/cargo", "install", p.Name}}}
			if !u.Cargo {
				if len(runtimeOps) > 0 {
					op.After = runtimeOps[0]
					op.Notes = append(op.Notes, "cargo comes with the Rust runtime Mise installs")
				} else {
					op.Blocked = "cargo is not installed and no component installs a Rust runtime"
				}
			}
		}
		ops = append(ops, op)
	}
	return ops
}

// hasPrefix reports whether any desired package uses the prefix.
func (b *builder) hasPrefix(prefix string) bool {
	for _, p := range b.in.Resolved.Packages {
		if p.Prefix == prefix {
			return true
		}
	}
	return false
}

// userFile reports whether a file relative to the home directory exists,
// read through the source so a test can say what the home holds.
func (b *builder) userFile(home, rel string) bool {
	path := filepath.Join(home, rel)
	names, err := b.in.Source.ReadDir(filepath.Dir(path))
	return err == nil && contains(names, filepath.Base(path))
}
