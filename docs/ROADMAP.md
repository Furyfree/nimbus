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

Build the read-only loader and resolver, then expose the configuration authoring
check without exposing raw resolver internals as a user workflow.

The delivered commands are:

~~~text
nimbus validate
nimbus version
~~~

### Context and decisions

This phase freezes the smallest schema needed by later inspection and planning:

- root nimbus.toml schema, supported Fedora releases, and minimum-engine
  compatibility metadata
- the local selector at ~/.config/nimbus/config.toml, including the approved
  normalized checkout origin
- machines, profiles, components, and exceptional catalog entries
- component requirements and conflicts
- packages, exclusions, exact version constraints, resources, warnings, manual
  task declarations, and runtime command declarations
- source locations and selection provenance

Q-002 through Q-005 are resolved. The configuration schema and initial command
surface have no remaining decision gate.

Definitions are read from the selected checkout. Live definitions are not
embedded in the engine. `validate` accepts an explicit checkout override and
resolves every tracked machine. The internal resolver accepts an explicit
machine input so tests and later commands never need the active selector.
Selector-based loading compares the selector's approved origin with local Git
configuration. Equivalent supported SSH and HTTPS locators normalize to one
repository identity; a missing or different origin is an error. The comparison
does not invoke Git or access the network.

Machine manifests live under machines/ in this repository. A required manifest
ID matches its filename. Profiles select components and never import profiles.
Components may require components. A bare Fedora package name resolves to the
default DNF lifecycle; catalog entries describe only exceptional behavior.

The canonical SHA-256 definition digest covers `nimbus.toml` and every regular
file below `machines/`, `profiles/`, `components/`, `catalog/`, and `system/`.
It hashes byte-sorted relative paths, exact contents, and mode normalized to
`100644` or `100755`; unrelated checkout paths and other permission bits are
excluded. A symlinked checkout root is resolved before use. Symlinks, special
files, and path escape inside the definition boundary are rejected. Commit and
dirty-worktree metadata are added when local Git inspection is introduced; the
definition digest already identifies the exact Phase 1 input.

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
- equivalent, missing, invalid, and mismatched selector-origin cases using only
  local Git configuration
- fixtures proving the exact digest boundary, byte-stable ordering, executable
  normalization, and exclusion of unrelated checkout files
- accepted symlinked-checkout-root plus rejected internal-symlink, special-file,
  and traversal cases
- system-file fixtures proving direct `system/root/etc` to `/etc` mapping and
  rejection of every other source or target root
- golden human and versioned JSON output
- version output that requires no selector, checkout, or system inspection
- repeat resolution to prove stable ordering and digest
- tests proving no command execution, network access, privilege request, or
  filesystem write
- gofmt, go vet ./..., go test ./..., and git diff --check

### Exit criteria

- validate and the internal resolver use the same loader and validator.
- Root help lists only validate and version; no future command or mutating stub
  exists.
- The initial desktop and laptop manifests resolve from repository definitions.
- Equivalent input produces byte-stable structured output and the same digest.
- Errors include actionable source locations.
- Selector-based loading rejects a checkout whose local origin does not match
  the reviewed selector identity.
- No system inspection, state write, Chezmoi invocation, or mutation path
  exists.
- TASKS.md records completed checks, evidence, and residual risk.

Stop after the resolver passes the complete local gate. Starting system
inspection requires a separate request.

## 2. Fedora system facts

### Outcome

Add internal read-only inspection of Fedora 44 and the first useful public
health check without calculating or applying changes.

### Context and decisions

All external commands run through an injectable runner. Native tool output is
isolated behind recorded Fedora fixtures. Inspection records local Nimbus Git
origin, commit, and dirty state but performs no network or Git mutation.

Only facts required by scheduled resources enter the model. Unsupported or
unavailable facts remain explicit rather than becoming guessed defaults.
`nimbus doctor` consumes the inspector to check the supported platform, selected
configuration, required native commands, and current provider health. It
explains each failure directly, has no explain mode, and never repairs, invokes
sudo, or accesses the network. Later phases extend its checks without changing
that contract.

### Risks and recovery

Command output can change across Fedora and DNF versions. Preserve raw evidence
in test fixtures, separate parsing from policy, and fail clearly when required
facts cannot be established. The phase remains read-only and needs no system
recovery.

### Validation and exit criteria

Use fake-runner unit tests, Fedora 44 fixtures, golden doctor output, and manual
comparison in a disposable Fedora VM. Exit when the same internal snapshot and
doctor result are stable and no inspector writes, invokes sudo, or performs
network access.

## 3. Planning and DNF

### Outcome

Compare desired configuration with facts and applied state, then produce a
complete non-mutating plan for a small DNF-backed resource set.

### Context and decisions

Introduce typed resource identities, DNF repository and RPM inspection,
adoption, install, owned removal, verification, risk classes, dependency
ordering, and the nimbus status, plan, packages installed, managed, unmanaged,
and why views.

The planner renders exact native argv, canonicalizes the plan, and calculates
its digest. Plan writes nothing. It shows normal apply operations, prune
candidates, and the availability and freshness of update information as
separate sections. Prune candidates stay informational unless a later
`apply --prune` explicitly enables them. Before Phase 7, unavailable upgrade
information is reported rather than guessed.

`packages installed [QUERY]` provides the read-only interactive view of
explicitly installed supported packages and labels desired, managed, unmanaged,
dependency, exclusion, provenance, update, and prune state. Structured output
returns the same data without requiring an interactive picker.

The first catalog entries use low-risk official Fedora packages. RPM Fusion and
COPR enter only after repository trust and removal can be represented.

### Risks and recovery

DNF transaction previews and installed-reason semantics must match Fedora 44.
Keep parsing behind fixtures and refuse an ambiguous transaction. This phase is
non-mutating and writes nothing, so it needs no system or file recovery.

### Validation and exit criteria

Use golden plans, changed-input digest tests, package adoption and removal
fixtures, and a disposable Fedora VM comparison. Exit when the plan explains
every selected package without executing a mutating command.

## 4. Controlled DNF apply and receipts

### Outcome

Apply one reviewed unchanged DNF plan, add the focused package install and
remove workflows, verify each successful operation, and record complete
versioned receipts under /var/lib/nimbus.

### Context and decisions

Add the user-owned kernel operation lock at
`$XDG_RUNTIME_DIR/nimbus/operation.lock`, approval boundary, immediate
re-inspection and re-resolution, digest refusal, direct sudo native commands,
the narrow atomic record action, partial-failure behavior, adoption, owned
removal, and explicit pruning through `nimbus apply --prune`. Acquire the lock
after approval and before the final checks or any write, and hold it through
verification and receipt recording.

`packages install [QUERY]` and `packages remove [QUERY]` use interactive
multi-selection, change only the selected machine manifest, and show both the
manifest diff and complete system plan before approval. They then reuse the
same apply path. Remove is limited to desired or Nimbus-managed packages;
eligible unmanaged packages remain the responsibility of `apply --prune`.
Nimbus leaves every manifest edit as an uncommitted Git change.

Bootstrap packages retain correct DNF install reasons. Removal is permitted
only from the lifecycle recorded in the receipt.

### Risks and recovery

This is the first mutating phase. Tests run only in disposable Fedora VMs.
Failed verification never creates a successful receipt. A failed transaction
retains the reviewed desired manifest, accurate prior receipts, and native DNF
history needed for recovery. Status and plan then expose the remaining drift.

### Validation and exit criteria

Exercise install, package-picker install and remove, profile exclusion,
already-present adoption, verification failure, digest change, interrupted
operation, owned removal, explicit pruning, and protected dependency cases.
Exit when a small representative DNF component and the package shortcuts can
complete and reverse safely in a disposable VM.

## 5. Bootstrap, initialization, and Chezmoi handoff

### Outcome

Provide a fresh-install flow that obtains Nimbus and its checkout, selects a
tracked machine, applies the system, and performs one explicit Chezmoi
initialization without taking over Chezmoi's lifecycle.

### Context and decisions

The supported one-liner pipes the repository's raw `main` `install.sh` into
Bash. That minimal script verifies the platform and normal-user context,
obtains Git through DNF, clones or validates `~/.local/share/nimbus`, and runs
the checkout's versioned `bootstrap` with the child input attached to
`/dev/tty`. The checked-out script enables the reviewed COPR, installs the
compatible Nimbus RPM, and invokes `nimbus init --checkout`.

Neither script updates an existing checkout, provisions the workstation, or
handles Chezmoi. Chezmoi is an ordinary Nimbus-managed system package installed
by the reviewed first apply. The engine remains directly DNF-owned and checkout
updates remain direct user Git operations; Nimbus has no self-update path.

Q-006 and Q-007 are resolved. For the development profile, Chezmoi owns an
onchange-after action that runs `mise install` as the normal user after its
rendered Mise configuration changes. Nimbus installs Mise and selected system
dependencies, while `MISE_SYSTEM_DEPS=warn` prevents the action from taking
over privileged dependency installation. Nimbus only reports missing runtimes
and the direct repair command.

nimbus init writes only the local selector and a reviewed new machine manifest
when requested. Existing tracked manifests are loaded unchanged. The handoff
passes the machine ID and ordered profile IDs through tested supported Chezmoi
initialization arguments.

Normal chezmoi diff, apply, edit, and update stay direct. Nimbus performs no
silent Git operation. Before this phase exits, the dotfiles repository must
remove its obsolete Nimbus machine manifests and document the same handoff
without introducing a second profile graph.

### Risks and recovery

Never replace an unrelated checkout or remote. Require a controlling terminal,
attach only the checked-out child script to `/dev/tty`, and stop on an invalid
existing target. A failed installation leaves native DNF state and the checkout
independently recoverable. A failed initialization leaves the system usable and
reports direct Git and Chezmoi recovery. A failed Mise action remains pending
in Chezmoi; a runtime deleted without a configuration change is recovered with
the reported direct Mise command. Removing Nimbus must leave Chezmoi and Mise
usable.

### Validation and exit criteria

Test piped installation, missing controlling terminal, missing Git and COPR
support, new checkout, accepted symlinked checkout, existing compatible, dirty,
wrong-origin, non-repository, engine-schema mismatch, rerun, missing-dotfiles,
and interrupted cases in disposable homes and a Fedora VM. Test development
profile gating, action ordering, checksum-triggered reruns, unchanged
configuration, failure retry, the script-excluded dry-run flow, and a manually
deleted runtime. Exit when the one-liner reaches init safely, direct Chezmoi use
works both with and without Nimbus, and the Mise handoff preserves all three
ownership boundaries.

## 6. System resources and desktop recovery

### Outcome

Extend the provider model from packages to services, system files, groups,
system Flatpaks, repositories, triggers, greeter, portals, and the
hyprland-noctalia system integration. Deliver the narrow reverse-content
workflow:

~~~text
nimbus files accept /etc/PATH
~~~

### Context and decisions

Generic system file changes use sources below system/root/etc with targets
derived below /etc, full diffs, and the narrow atomic file-install action. They
refuse target-path symlinks, non-regular targets, and ownership that is foreign
or unknown. Triggers are fixed argv and run at most once per apply.

For a deliberate live edit, `files accept` works only on one selected generic
system file whose receipt proves Nimbus ownership. It shows the reverse diff
and exact checkout destination, requires approval, acquires the normal
operation lock, rechecks all inputs, and atomically updates source content as
the normal user. It never invokes sudo, changes the live target, captures
target metadata, writes a receipt, or performs Git operations. The user then
runs validate, plan, and apply so the matching target is verified and adopted
under the new definition identity.

The desktop component installs a separate system-owned Hyprland recovery
session under /usr. It never writes a seed configuration into the user's home.
Those files use a native package or a separately specified typed integration;
they do not widen the generic system-file provider.

### Risks and recovery

Service, greeter, portal, and compositor changes can prevent login. The recovery
session and removal path must be proven before the normal session is managed.
Report .rpmnew and .rpmsave files without modifying them.

Reverse capture can accidentally preserve a bad manual edit or secret. Limit it
to proven-owned, non-symlinked, user-readable files, show the entire reverse
diff and source destination, repeat the no-secrets warning, and leave a normal
recoverable Git working-tree change. Refusal or interruption before the atomic
source replacement leaves both checkout and system unchanged.

### Validation and exit criteria

Use focused provider fixtures and a clean Fedora graphical VM. Verify login,
portal behavior, system-file restoration, component removal, and recovery
session independence from Chezmoi. Test accepted content drift, cancellation,
changed-input refusal, atomic source replacement, subsequent adoption receipt,
and rejection of foreign, unknown, unselected, unreadable, symlinked,
non-regular, multiple, and non-`/etc` targets. Prove that accept uses no sudo,
does not modify the live target or metadata, and leaves Git untouched.

## 7. Updates, constraints, and recovery points

### Outcome

Deliver `nimbus upgrade` for controlled normal updates, enforce native
constraints, coordinate the desktop-session group, and create tested recovery
points for disruptive transactions.

### Context and decisions

Verify DNF5 version-lock capabilities before extending constraint syntax.
Define exact desktop-session membership from real Fedora, Hyprland, Noctalia,
greeter, portal, and session packages. Q-009 fixes the supported UEFI, boot,
LUKS2, and Btrfs subvolume topology. Recovery points contain a read-only root
snapshot, conditional system-Flatpak snapshot and boot archives, plus
checksummed self-contained restore metadata. Home, logs, caches, swap, VM data,
and container data stay outside. Retain the newest three complete points and
protect unresolved failures; after eligible cleanup, less than 20 GiB of
Btrfs-aware usable space blocks mutation.

Upgrade may refresh native metadata, shows its own exact reviewed plan, never
prunes, and does not silently apply unrelated desired-state drift. It blocks
with a direction to run `nimbus apply` when that drift is a prerequisite.
Q-008 permanently delegates Fedora release upgrades to the native DNF5
system-upgrade workflow. Nimbus has no target-release option or release-upgrade
transaction. Doctor reports installed-release compatibility, mutations refuse
an unsupported release, and normal validation, status, plan, and apply expose
Nimbus-owned drift after a user-run native upgrade.

Recovery creation remains disabled until the manual live-environment restore of
root and matching boot data succeeds in a disposable Fedora VM. Nimbus never
claims release-upgrade support or recovery.

### Risks and recovery

Package and boot changes may make the graphical or complete system unbootable.
Retain a known-good recovery point and document restoration independently of
Nimbus state before enabling automatic creation. Automatic rollback remains
deferred until repeated drills prove it. Native Fedora release-upgrade risk and
recovery stay outside Nimbus and must not be represented by a Nimbus receipt.

### Validation and exit criteria

Exercise compatible and incompatible update sets, lock behavior, low-space
preflight, interrupted upgrades, logout and reboot reporting, retention, and
manual restoration in disposable VMs. Cover topology mismatch, nested
subvolume exclusion, conditional Flatpak and boot capture, partial recovery
creation and cleanup, protected failed operations, retention cleanup, delayed
Btrfs deletion, the 20 GiB refusal, checksums, and preservation of home and
guest data. Prove that `nimbus upgrade` remains
within the installed release, exposes no target-release path, and never invokes
DNF5 system-upgrade. Verify that unsupported releases preserve version,
validation, and doctor diagnostics while blocking mutation, and that supported
post-upgrade inspection exposes Nimbus-owned drift.

## 8. Manual workflows and runtime capabilities

### Outcome

Add the shared typed task model, post-install presentation, and runtime commands
for already-applied optional components. Deliver:

~~~text
nimbus postinstall
nimbus windows status|setup|start|connect|stop|purge-data
nimbus launch browser [URL] [--private]
nimbus launch webapp URL
~~~

Postinstall presents typed 1Password, fingerprint, NVIDIA MOK, Windows setup,
logout, and reboot work. It delegates to the same component actions and never
runs arbitrary scripts. Certificates, gaming resources, and virtualization
host setup remain normal plan and apply resources; external application-data
repositories remain outside Nimbus.

Q-010 selects QEMU/KVM through `qemu:///system`. The one supported Windows
guest is contained below `/var/lib/libvirt/images/nimbus/windows/`. Runtime
commands never install a missing component. Normal removal stops and undefines
the owned domain but preserves its data. Purge rejects an active, referenced,
symlinked, foreign, or escaped target, names the exact directory, and requires a
second confirmation. Domain configuration is reproducible; guest disks require
separate VM-aware backup and are not part of Nimbus recovery.

Launch helpers safely resolve the XDG browser, translate supported private-mode
flags, use an explicit Chromium-family webapp fallback, accept only HTTP(S)
URLs, and execute no shell-derived command. Chezmoi retains ownership of the
keybindings and desktop entries that call them.

Validate the typed task list in terminal, non-terminal, and JSON modes. Test the
Windows lifecycle, absent component refusal, native ownership, path containment,
normal removal, preserved data, unknown references, double-confirmed purge, and
domain reconstruction in a disposable VM. Test browser-family private flags,
desktop-entry parsing, fallback selection, argv preservation, invalid schemes,
and missing browsers without writing user configuration.

The interactive dashboard waits until the underlying commands have stable
structured output. In a terminal, bare `nimbus` then opens it; non-terminal use
prints grouped help. Its package screens reuse the established package
workflows and own no separate state.

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
