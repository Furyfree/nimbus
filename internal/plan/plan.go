package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Furyfree/nimbus/internal/definitions"
	"github.com/Furyfree/nimbus/internal/inspect"
	"github.com/Furyfree/nimbus/internal/native"
	"github.com/Furyfree/nimbus/internal/snapper"
	"github.com/Furyfree/nimbus/internal/state"
)

// Operation kinds, actions, and risk classes.
const (
	KindFile          = "system-file"
	KindService       = "service"
	KindGroup         = "group"
	KindShell         = "login-shell"
	KindTarget        = "default-target"
	KindTrigger       = "trigger"
	KindDNFConfig     = "dnf-config"
	KindUser          = "user" // a user-scope installer
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
	Privileged  bool     `json:"privileged,omitzero"`
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
	// ItemPaths preserves each requested package's selection provenance in a
	// merged transaction; Paths explains the transaction as a whole.
	ItemPaths map[string][]string `json:"item_paths,omitempty"`
	// Resolved maps requested names to native RPM name.arch identities from
	// the preview or installed state, including resolved provides.
	Resolved    map[string]string `json:"resolved,omitempty"`
	Steps       []Step            `json:"steps,omitempty"`
	Transaction *Transaction      `json:"transaction,omitempty"`
	// AbsentPackage is the native identity whose absence authorizes receipt
	// retirement. Ownership transfers leave it empty.
	AbsentPackage string `json:"absent_package,omitempty"`
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
	Machine   string        `json:"machine"`
	Snapshots *snapper.Plan `json:"snapshots,omitempty"`
	// Definitions is the definition digest used for planning. Checkout
	// records Git identity, which approval rechecks separately.
	Definitions string           `json:"definitions"`
	Checkout    inspect.Checkout `json:"checkout"`
	Operations  []Operation      `json:"operations"`
	Prune       []Prune          `json:"prune"`
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
	Facts       *inspect.Facts
	Applied     *state.Applied // receipts and baseline; nil before any apply
	Source      native.Source
	// Prune promotes the prune candidates into removal operations, as apply
	// --prune does.
	Prune bool
}

// Build produces the plan. Missing required facts and digest encoding failures
// return errors; individual problems become blocked operations so the whole
// picture is still shown.
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
	b := &builder{in: in, repos: map[string][]inspect.Repository{}, ready: map[string]bool{}, blockedRepo: map[string]string{}, pendingRepo: map[string]bool{}, duplicates: map[string][]string{}}
	for _, r := range in.Facts.Repositories.Value {
		b.repos[r.ID] = append(b.repos[r.ID], r)
	}
	p := &Plan{Machine: in.Resolved.Machine, Definitions: in.Definitions, Checkout: in.Facts.Checkout.Value, Complete: true}
	if slices.ContainsFunc(in.Resolved.Components, func(c definitions.ResolvedComponent) bool { return c.ID == "snapper" }) {
		var template []byte
		for _, file := range in.Resolved.Files {
			if file.Target == snapper.Template {
				template = file.Content
			}
		}
		var err error
		p.Snapshots, err = snapper.Inspect(in.Source, template)
		if err != nil {
			return nil, fmt.Errorf("inspect Snapper: %w", err)
		}
	}
	if slices.ContainsFunc(in.Resolved.Repositories, func(id string) bool {
		r := in.Root.Repositories[id]
		return r.Kind == "dnf" && r.ReleasePackage == ""
	}) {
		p.RepositoryReconciliation = "After package transactions, disable new duplicate providers of declared baseurls through DNF overrides; preserve vendor files and keys, and verify the declared sources."
	}
	p.Operations = append(p.Operations, b.dnfConfig()...)
	for _, op := range p.Operations {
		if op.Kind == KindDNFConfig && op.Action != ActionKeep && op.Action != ActionAdopt {
			b.repoChanges = append(b.repoChanges, op.ID)
		}
	}
	p.Operations = append(p.Operations, b.constraintOperations()...)
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
	if in.Prune && in.Applied.Baseline == nil {
		p.PruneUnavailable = "prune needs the baseline the first sync records; run sync once first"
	}
	if in.Prune && len(p.Prune) > 0 {
		p.Operations = append(p.Operations, b.pruneTransaction(p.Prune))
	}
	deferPackageRemovalForResources(p.Operations)
	p.Updates = b.updates()
	if slices.ContainsFunc(p.Operations, func(op Operation) bool { return op.Blocked != "" }) {
		p.Complete = false
	}
	planDigest, err := digest(p)
	if err != nil {
		return nil, err
	}
	p.Digest = planDigest
	return p, nil
}

type builder struct {
	in    Inputs
	repos map[string][]inspect.Repository
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

func (b *builder) updates() Updates {
	if len(b.in.Resolved.Constraints) > 0 {
		return b.constraintUpdates()
	}
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
func digest(p *Plan) (string, error) {
	type canon struct {
		Machine                  string        `json:"machine"`
		Definitions              string        `json:"definitions"`
		Operations               []Operation   `json:"operations"`
		RepositoryReconciliation string        `json:"repository_reconciliation,omitempty"`
		Snapshots                *snapper.Plan `json:"snapshots,omitempty"`
	}
	data, err := json.Marshal(canon{Machine: p.Machine, Definitions: p.Definitions, Operations: p.Operations, RepositoryReconciliation: p.RepositoryReconciliation, Snapshots: p.Snapshots})
	if err != nil {
		return "", fmt.Errorf("encode plan digest: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
