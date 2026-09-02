# Nimbus roadmap

This file owns implementation order, phase-specific decisions, risks, recovery,
validation, and exit criteria. The complete phase-independent product contract
lives in [SPEC.md](SPEC.md). Current checkboxes and evidence live in
[TASKS.md](TASKS.md). Decision rationale and unresolved questions live in
[DECISIONS.md](DECISIONS.md); this roadmap places the gates that affect each
phase.

Current phase: **1. Configuration resolver**.

## Principles

- Build one observable capability at a time.
- Resolve desired configuration before inspecting the machine.
- Inspect and plan before enabling any mutation.
- Add each mutating resource only with verification, ownership, and recovery.
- Keep native tools visible and independently usable.
- Use the real Fedora workstation and disposable Fedora virtual machines as
  design pressure, not as automatic desired state.
- Keep the complete product contract in SPEC.md and current work only in
  TASKS.md.

## 1. Configuration resolver

### Outcome

Build a read-only Go CLI that validates the complete selected Nimbus checkout
and resolves one selected machine into a deterministic desired graph.

The delivered commands are:

~~~text
nimbus validate
nimbus config resolve
~~~

### Context and decisions

This phase freezes the smallest schema needed by later inspection and planning:

- root nimbus.toml compatibility metadata
- the local selector at ~/.config/nimbus/config.toml
- machines, profiles, components, and exceptional catalog entries
- component requirements and conflicts
- packages, exclusions, exact version constraints, resources, warnings, manual
  task declarations, and runtime command declarations
- source locations and selection provenance

Before the schema is frozen, resolve Q-002 through Q-005 in DECISIONS.md.

Definitions are read from the selected checkout. Live definitions are not
embedded in the engine. The resolver accepts explicit checkout and machine
overrides so tests and development never need the active user configuration.

Machine manifests live under machines/ in this repository. A required manifest
ID matches its filename. Profiles select components and never import profiles.
Components may require components. A bare Fedora package name resolves to the
default DNF lifecycle; catalog entries describe only exceptional behavior.

The canonical definition digest covers path, mode, and content in sorted order.
The definition tree rejects symlinks and path escape. Commit and dirty-worktree
metadata are added when local Git inspection is introduced; the content digest
already identifies the exact Phase 1 input.

Phase 1 may model facts-dependent variants and warnings but never evaluates
them. It performs no hardware or operating-system inspection, executes no
external commands, accesses no network, invokes no privilege escalation, and
writes no state.

Only exact package constraints are accepted initially. Additional comparison
syntax waits until the DNF5 mechanism is proven in a disposable Fedora
environment.

### Risks and recovery

The main risk is freezing a schema around speculative later behavior. Keep
resource declarations typed but small, add only fields exercised by the
initial definitions and fixtures, and reject unknown fields.

The phase is read-only. Recovery consists of correcting the input or reverting
the code and definition change. It must not create ~/.config/nimbus or
/var/lib/nimbus during validation or resolution.

### Validation

- table-driven loader and schema tests
- valid and invalid graph fixtures
- cycle, duplicate, missing reference, conflict, exclusion, and unknown-field
  cases
- package-reference parsing, catalog non-shadowing, canonical identity,
  lifecycle conflict, and constraint-attachment cases
- symlink, traversal, and definition-digest fixtures
- golden human and versioned JSON output
- repeat resolution to prove stable ordering and digest
- tests proving no command execution, network access, privilege request, or
  filesystem write
- gofmt, go vet ./..., go test ./..., and git diff --check

### Exit criteria

- validate and config resolve use the same loader and validator.
- The initial desktop and laptop manifests resolve from repository definitions.
- Equivalent input produces byte-stable structured output and the same digest.
- Errors include actionable source locations.
- No system inspection, state write, Chezmoi invocation, or mutation path
  exists.
- TASKS.md records completed checks, evidence, and residual risk.

Stop after the resolver passes the complete local gate. Starting system
inspection requires a separate request.

## 2. Fedora system facts

### Outcome

Add read-only inspection of Fedora 44 without calculating or applying changes.
The new nimbus facts command produces stable structured observations for
packages, repositories, services, files, mounts, swap, graphics, Secure Boot,
and other facts required by the initial components.

### Context and decisions

All external commands run through an injectable runner. Native tool output is
isolated behind recorded Fedora fixtures. Inspection records local Nimbus Git
origin, commit, and dirty state but performs no network or Git mutation.

Only facts required by scheduled resources enter the model. Unsupported or
unavailable facts remain explicit rather than becoming guessed defaults.

### Risks and recovery

Command output can change across Fedora and DNF versions. Preserve raw evidence
in test fixtures, separate parsing from policy, and fail clearly when required
facts cannot be established. The phase remains read-only and needs no system
recovery.

### Validation and exit criteria

Use fake-runner unit tests, Fedora 44 fixtures, golden JSON, and manual
comparison in a disposable Fedora VM. Exit when the same snapshot is stable and
no inspector writes, invokes sudo, or performs network access.

## 3. Planning and DNF

### Outcome

Compare desired configuration with facts and applied state, then produce a
complete non-mutating plan for a small DNF-backed resource set.

### Context and decisions

Introduce typed resource identities, DNF repository and RPM inspection,
adoption, install, owned removal, verification, risk classes, dependency
ordering, and the nimbus status, plan, managed, unmanaged, and why views.

The planner renders exact native argv, canonicalizes the plan, and calculates
its digest. An explicit plan output path may be written; the system and Nimbus
state remain unchanged. Prune candidates stay informational unless a later
apply explicitly enables pruning.

The first catalog entries use low-risk official Fedora packages. RPM Fusion and
COPR enter only after repository trust and removal can be represented.

### Risks and recovery

DNF transaction previews and installed-reason semantics must match Fedora 44.
Keep parsing behind fixtures and refuse an ambiguous transaction. This phase is
non-mutating, so recovery is deleting an explicitly requested plan output.

### Validation and exit criteria

Use golden plans, changed-input digest tests, package adoption and removal
fixtures, and a disposable Fedora VM comparison. Exit when the plan explains
every selected package without executing a mutating command.

## 4. Controlled DNF apply and receipts

### Outcome

Apply one reviewed unchanged DNF plan, verify each successful operation, and
record complete versioned receipts under /var/lib/nimbus.

### Context and decisions

Add the operation lock, approval boundary, immediate re-inspection and
re-resolution, digest refusal, direct sudo native commands, the narrow atomic
record action, partial-failure behavior, adoption, owned removal, and explicit
pruning.

Bootstrap packages retain correct DNF install reasons. Removal is permitted
only from the lifecycle recorded in the receipt.

### Risks and recovery

This is the first mutating phase. Tests run only in disposable Fedora VMs.
Failed verification never creates a successful receipt. A failed transaction
retains accurate prior receipts and native DNF history needed for recovery.

### Validation and exit criteria

Exercise install, already-present adoption, verification failure, digest
change, interrupted operation, owned removal, and protected dependency cases.
Exit when a small representative DNF component can complete and reverse safely
in a disposable VM.

## 5. Bootstrap, initialization, and Chezmoi handoff

### Outcome

Provide a fresh-install flow that obtains Nimbus and its checkout, selects a
tracked machine, applies the system, and performs one explicit Chezmoi
initialization without taking over Chezmoi's lifecycle.

### Context and decisions

The bootstrap obtains only the compatible Nimbus engine, required Git
transport, and selected Nimbus checkout. Chezmoi is an ordinary Nimbus-managed
system package and may be installed by the reviewed first apply.

Resolve Q-006 and Q-007 in DECISIONS.md before implementing this phase.

nimbus init writes only the local selector and a reviewed new machine manifest
when requested. Existing tracked manifests are loaded unchanged. The handoff
passes the machine ID and ordered profile IDs through tested supported Chezmoi
initialization arguments.

Normal chezmoi diff, apply, edit, and update stay direct. Nimbus performs no
silent Git operation. Before this phase exits, the dotfiles repository must
remove its obsolete Nimbus machine manifests and document the same handoff
without introducing a second profile graph.

### Risks and recovery

Never replace an unrelated checkout or remote. A failed initialization leaves
the system usable and reports direct Git and Chezmoi recovery. Removing Nimbus
must leave Chezmoi usable.

### Validation and exit criteria

Test new, reinstall, missing-dotfiles, existing-compatible, dirty, wrong-origin,
and interrupted cases in disposable homes and a Fedora VM. Exit when direct
Chezmoi use works both with and without Nimbus.

## 6. System resources and desktop recovery

### Outcome

Extend the provider model from packages to services, system files, groups,
system Flatpaks, repositories, triggers, greeter, portals, and the
hyprland-noctalia system integration.

### Context and decisions

System file changes use mirrored sources under system/root, full diffs, and the
narrow atomic file-install action. Triggers are fixed argv and run at most once
per apply.

The desktop component installs a separate system-owned Hyprland recovery
session under /usr. It never writes a seed configuration into the user's home.

### Risks and recovery

Service, greeter, portal, and compositor changes can prevent login. The recovery
session and removal path must be proven before the normal session is managed.
Report .rpmnew and .rpmsave files without modifying them.

### Validation and exit criteria

Use focused provider fixtures and a clean Fedora graphical VM. Verify login,
portal behavior, system-file restoration, component removal, and recovery
session independence from Chezmoi.

## 7. Updates, constraints, and recovery points

### Outcome

Plan controlled upgrades, enforce native constraints, coordinate the
desktop-session group, and create tested recovery points for disruptive
transactions.

### Context and decisions

Verify DNF5 version-lock capabilities before extending constraint syntax.
Define exact desktop-session membership from real Fedora, Hyprland, Noctalia,
greeter, portal, and session packages. Recovery-point creation waits for a
successful restore drill and retention policy.

Resolve Q-008 and Q-009 in DECISIONS.md before claiming release-upgrade or
recovery-point support.

### Risks and recovery

Package and boot changes may make the graphical or complete system unbootable.
Retain a known-good recovery point and document restoration independently of
Nimbus state before enabling automatic creation. Automatic rollback remains
deferred until repeated drills prove it.

### Validation and exit criteria

Exercise compatible and incompatible update sets, lock behavior, low-space
preflight, interrupted upgrades, logout and reboot reporting, retention, and
manual restoration in disposable VMs.

## 8. Manual workflows and runtime capabilities

### Outcome

Add the shared typed task model, post-install presentation, and runtime commands
for already-applied optional components.

Candidate capabilities include 1Password readiness, fingerprint enrollment,
NVIDIA MOK enrollment, and the Windows guest lifecycle. Windows support starts
only after package, service, privilege, receipt, path-containment, and recovery
primitives are mature.

Resolve Q-010 in DECISIONS.md before implementing Windows runtime commands.

Runtime commands never install missing components. Guest data survives normal
component removal; explicit purge names the exact path and requires a second
confirmation.

The interactive dashboard and package browser wait until the underlying
commands have stable structured output. They reuse engine logic and own no
separate state.

## Later

- hibernation and supported boot-resource management
- LUKS2 inspection and reviewed TPM2 auto-unlock
- systemd-boot and unified kernel image provisioning
- a separate Fedora installer image using the same definitions and operations
- managed Secure Boot after a complete key and recovery design
- additional desktop profiles based on real maintained configurations
- Arch and Debian or Ubuntu only after the Fedora resource model is mature
- reviewed candidate imports from a separate package discovery tool

Each later outcome requires its own acceptance, validation, recovery, and stop
point before implementation.

## Done

- Repository purpose, ownership, safety, complete product contract, and
  documentation ownership are reconciled.
- The selected architecture keeps engine, machine manifests, and system
  definitions in Nimbus while keeping user files in the separate dotfiles
  repository.
