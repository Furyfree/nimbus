# Nimbus decisions

This file records architectural decision rationale and unresolved questions.
It does not replace the accepted contract in [SPEC.md](SPEC.md), phase order in
[ROADMAP.md](ROADMAP.md), or current work in [TASKS.md](TASKS.md).

When a question is resolved, move the accepted behavior into its owning
contract, record the answer here, and mark the question resolved. ROADMAP.md
references question IDs when they gate a phase.

## 2026-09-02 foundation passthrough

Three independent read-only passes used Claude Fable 5.1 High, Grok 4.6 High,
and GLM 5.3 Flash Max. The final adjudication also inspected the active files,
repository support files, archived drafts, and the relevant dotfiles contracts.
The agents made no repository changes.

The overall architecture is accepted: Nimbus owns system state, Chezmoi owns
user files, Mise owns user runtimes, live definitions come from the selected
Nimbus checkout, and desired, observed, and last-applied state remain separate.
The following decisions tighten the foundation before implementation.

### Accepted decisions

#### D-001: SPEC remains complete and phase-independent

SPEC.md must contain the accepted schema semantics, command behavior, ownership,
safety, and system invariants. It must not contain phase status, delivery order,
or temporary implementation shortcuts.

ROADMAP.md decides when the contract is delivered. TASKS.md records only the
current work and evidence.

#### D-002: Schema contracts precede the resolver implementation

Before the first Go types are frozen, SPEC.md must define the required and
optional semantics for:

- nimbus.toml compatibility metadata
- the local selector
- machines, profiles, components, and exceptional catalog entries
- package references, exclusions, and enforceable constraints
- resources and system-file declarations
- warnings, manual tasks, and runtime command groups
- source locations and selection provenance

The schema stays strict and versioned. Unknown fields remain errors. Fields are
added only when the accepted definitions or validation fixtures exercise them.

#### D-003: Static validation and observed-system validation are separate

`nimbus validate` and configuration resolution inspect only the selected
checkout. They may reject structural errors, but they cannot claim to validate
facts that require DNF, RPM, hardware, filesystem, or operating-system
inspection.

Provider availability, installed dependency safety, protected-package state,
and native transaction behavior belong to later facts and planning validation.

#### D-004: Nimbus never disables Secure Boot

The rule is absolute for Nimbus operations, whether automatic, prompted, or
otherwise explicit. Secure Boot key and MOK enrollment may be represented only
as reviewed manual work with verification and recovery guidance.

#### D-005: The catalog remains an exception list

Bare Fedora package names use the default typed DNF lifecycle. Catalog entries
exist only for non-default providers, repositories, coordinated update groups,
special verification or removal, pinned external artifacts, and other explicit
exceptions.

The package-query container produces research evidence. Its results do not
automatically become catalog entries or desired state.

#### D-006: History must identify itself as superseded evidence

Every file under history/ must state near its beginning that it is not an
active contract. Archived wording may contradict the current design and must
not be copied without revalidation against the active documents.

History remains useful and is not deleted merely because it contains rejected
designs. Dated filenames are required for new records; the undated 2026-08-31
GLM record should be renamed consistently.

#### D-007: The dotfiles reconciliation remains a bootstrap gate

The dotfiles repository still contains obsolete Nimbus machine manifests, the
old symlink-based selector contract, and references to the archived Nimbus
CLI draft. This does not block the isolated read-only resolver, but bootstrap and
Chezmoi handoff cannot be accepted until the dotfiles repository uses the same
ownership and selector model as Nimbus.

#### D-008: Low-value cleanup is not part of the correction pass

The foundation pass does not need to remove the existing Dependabot entry,
rewrite the stock Go gitignore, add a README license link, delete additional
history, or make the local and raw package-query commands identical. Those
items do not affect the product contract or the current phase gate.

### Resolved questions

#### Q-001: Package reference grammar and catalog precedence (resolved)

Resolved 2026-09-02.

Bare names always mean Fedora packages and canonicalize to `dnf:<name>`.
Explicit `dnf:`, `flatpak:`, and `catalog:` forms are accepted. Catalog entries
are selected only through `catalog:<id>` and never shadow bare names. Unknown or
empty prefixes and invalid provider-native identifiers are errors.

Canonical identity is the provider-qualified native package target. Duplicate
selection paths merge provenance only when their technical lifecycle agrees;
otherwise validation fails. Constraint keys use canonical provider-qualified
identities and attach after selection resolution. SPEC.md owns the normative
grammar.

### Open questions

#### Q-002: Selector trust and checkout origin

Does the selector record an expected normalized Git origin, or is origin derived
and trusted through another reviewed mechanism? The answer must support the
wrong-origin bootstrap test without making network access part of resolution.

Required before: selector schema freeze.

#### Q-003: Definition digest boundary and mode normalization

Which exact paths participate in the canonical definition digest? Decide
whether it covers nimbus.toml, every definition directory, and all system-source
files, and whether modes use full permission bits or only meaningful executable
state. Also decide whether a symlinked checkout root is accepted.

Required before: resolver digest fixtures.

#### Q-004: Supported system-file target roots

Which absolute target roots may a system-file resource address? The accepted
set must be narrow enough to validate traversal, symlink escape, ownership, and
recovery before system resources are represented.

Required before: resource schema freeze.

#### Q-005: Complete command semantics and phase ownership

Define the behavior and common flags for every command listed in SPEC.md,
including structured output, explicit plan output, pruning, upgrades, and
checkout or machine overrides. Decide the non-mutating meaning and delivery
phase of `doctor`, `packages`, and `version`.

Required before: freezing affected command contracts. The first command tree
must contain no mutating stubs.

#### Q-006: Bootstrap distribution and actor

How are the compatible Nimbus engine, Git transport, and trusted checkout
obtained before `nimbus init` can run? Decide the bootstrap actor and supported
distribution path without giving Nimbus a self-update lifecycle.

Required before: bootstrap implementation.

#### Q-007: Mise installation handoff

Nimbus may install the Mise system package and report missing runtimes, while
Chezmoi owns the Mise configuration and Mise owns runtime installation. Decide
whether the user runs `mise install` directly or Chezmoi presents a reviewed
user-scope action.

Required before: bootstrap and manual-workflow completion.

#### Q-008: Fedora release upgrades

Are Fedora release upgrades a supported Nimbus workflow, a later explicit
outcome, or permanently delegated to native Fedora tooling? Define preflight,
compatibility, recovery, and the required Nimbus version before support.

Required before: claiming upgrade coverage beyond normal package updates.

#### Q-009: Recovery layout, retention, and operation lock

Define the supported Btrfs and boot-data layout, recovery-point retention,
low-space behavior, manual restore procedure, and operation-lock location and
ownership. Recovery creation remains disabled until a restore drill passes.

Required before: recovery-point implementation.

#### Q-010: Windows backend and guest-data location

Choose the supported backend and exact guest-data location. Define containment,
ownership, normal removal, explicit purge, verification, and recovery before a
Windows component is accepted.

Required before: Windows runtime commands.

### Consequences for the next documentation pass

The next pass should resolve Q-002 through Q-005, expand the corresponding
phase-independent contracts in SPEC.md, align ROADMAP.md exit criteria and
TASKS.md with those answers, and then perform the smaller history, README,
package-query, Phase 8, and pull-request-template corrections. Go implementation
starts only after the configuration-resolver gates are closed.
