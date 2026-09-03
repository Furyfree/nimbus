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
- machines, profiles, components, and the repositories declared in
  nimbus.toml
- component requirements and conflicts
- packages, exclusions, removals, and system-file sources; exact version
  constraints wait for Phase 7
- source locations and selection provenance

Q-002 through Q-005 resolve the foundational behavior. Q-013, Q-014, and Q-016
were resolved on 2026-09-03: explicit `common`, ordered profiles with every
other list canonical, repositories declared once in nimbus.toml and named by
prefix, and PACKAGES.md as the list of what Nimbus installs. The fixtures must
exercise those rules before Go types are frozen.

Definitions are read from the selected checkout. Live definitions are not
embedded in the engine. `validate` accepts an explicit checkout override and
resolves every tracked machine. The internal resolver accepts an explicit
machine input so tests and later commands never need the active selector.
Selector-based loading compares the selector's approved origin with local Git
configuration. Equivalent supported SSH and HTTPS locators normalize to one
repository identity; a missing or different origin is an error. The comparison
does not invoke Git or access the network.

Machine manifests live under machines/ in this repository. A required manifest
ID matches its filename. Profiles list packages, select components, and never
import profiles. Components may require components. A bare Fedora package name
resolves to the default DNF lifecycle; any other prefix names a repository
declared in nimbus.toml.

The canonical SHA-256 definition digest covers `nimbus.toml` and every regular
file below `machines/`, `profiles/`, `components/`, and `system/`.
It hashes byte-sorted relative paths, exact contents, and mode normalized to
`100644` or `100755`; unrelated checkout paths and other permission bits are
excluded. A symlinked checkout root is resolved before use. Symlinks, special
files, and path escape inside the definition boundary are rejected. Commit and
dirty-worktree metadata are added when local Git inspection is introduced; the
definition digest already identifies the exact Phase 1 input.

Q-013 keeps facts-dependent variants, warnings, manual tasks, and runtime
commands out of the initial schema. Phase 1 performs no hardware or
operating-system inspection, executes no external commands, accesses no
network, invokes no privilege escalation, and writes no state.

The tracked desktop and laptop fixtures select explicit hardware components.
They do not run the later `nimbus init` detector. This keeps Phase 1 resolution
static while exercising the component shape that initialization will record.

Package constraints are not part of the Phase 1 schema. A manifest that
carries `package_constraints` is rejected as an unknown field until Phase 7
proves DNF5 version locking in a disposable Fedora environment and defines
the accepted grammar.

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
- package-reference parsing, undeclared-prefix rejection, canonical identity,
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

Use fake-runner unit tests, Fedora 44 fixtures recorded from the research
container, and golden doctor output. Exit when the same internal snapshot and
doctor result are stable and no inspector writes, invokes sudo, or performs
network access. The first disposable-VM run waits for Phase 4 and covers
doctor, plan, and apply together, because the read-only phases carry little
risk a VM would expose earlier.

## 3. Planning and DNF

### Outcome

Compare desired configuration with facts and applied state, then produce a
complete non-mutating plan for a small DNF-backed resource set.

### Context and decisions

Introduce typed resource identities, DNF repository and RPM inspection,
adoption, install, owned removal, verification, risk classes, dependency
ordering, and the nimbus status, plan, packages installed, profiles list,
components list, managed, unmanaged, and why views.

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

The first planned packages are low-risk official Fedora packages. Declared
repositories enter planning only after their pinned key or release-package
identity, priority, and removal can be represented.

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

Apply one reviewed unchanged DNF plan, add the focused package, profile, and
component selection workflows, verify each successful operation, and record
complete versioned receipts under /var/lib/nimbus.

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
`profiles add|remove` and `components add|remove` edit the manifest's
selection lists through the same diff, plan, approval, and apply path; a
profile change prints the direct Chezmoi re-initialization command once
Phase 5 exists. Nimbus leaves every manifest edit as an uncommitted Git change.

Bootstrap packages retain correct DNF install reasons. Removal is permitted
only from the lifecycle recorded in the receipt.

### Risks and recovery

This is the first mutating phase. Tests run only in disposable Fedora VMs,
and the phase opens with the first VM run, which also compares doctor and
plan output with the recorded fixtures before any apply is attempted.
Failed verification never creates a successful receipt. DNF may refresh
metadata between approval and execution; the re-resolution refusal covers
that, and this phase decides whether download-then-cache-only execution is
worth adding on top. A failed transaction
retains the reviewed desired manifest, accurate prior receipts, and native DNF
history needed for recovery. Status and plan then expose the remaining drift.

### Validation and exit criteria

Exercise install, package-picker install and remove, profile and component
add and remove including the resulting owned removals, profile exclusion,
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

Q-006 and Q-007 are resolved. For the development profile, apply downloads
the official Mise installer, shows its digest, runs it as the normal user
before the handoff, and verifies the user-owned binary. After Chezmoi has
written the Mise configuration, which carries `auto_update = true`, apply
runs `mise install` under
`MISE_SYSTEM_DEPS=warn` and the declared `cargo install` steps, again as the
user. Nimbus installs selected system dependencies itself and reinstalls a
missing runtime or Cargo tool through the same steps on the next apply.

nimbus init writes only the local selector and a reviewed new machine manifest
when requested. For a new machine it inspects DMI and PCI facts, proposes the
known hardware components, and writes only the accepted IDs into that manifest.
Existing tracked manifests are loaded unchanged. The first detector fixtures
cover the MSI Z690 desktop with Intel and NVIDIA graphics and the HP EliteBook X
G1a with AMD graphics, including the `ddcutil` and `brightnessctl` split. The
handoff passes `machine`, `managed_by_nimbus`, and the ordered profile IDs
through the Chezmoi prompt flags specified in SPEC.md; hardware components do
not cross it.

Normal chezmoi diff, apply, edit, and update stay direct. Nimbus performs no
silent Git operation. The handoff runs once; later profile changes print the
refresh command and doctor reports a stale Chezmoi selection. The dotfiles
repository adopted the profile handoff on 2026-09-02 and holds no machine
manifest. Q-015 resolved the refresh as `chezmoi init --prompt` with every
value supplied, including the current 1Password answer. Before this phase
exits, the dotfiles template must declare `Machine` and `ManagedByNimbus` and
store the profile list without validating it, and both the initial and
refresh flags are verified against the real template in isolated homes.

### Risks and recovery

Never replace an unrelated checkout or remote. Require a controlling terminal,
attach only the checked-out child script to `/dev/tty`, and stop on an invalid
existing target. A failed installation leaves native DNF state and the checkout
independently recoverable. A failed initialization leaves the system usable and
reports direct Git and Chezmoi recovery. A failed user-scope step leaves the
plan drifted and is retried by the next apply; nothing below home has a
recovery point. Removing Nimbus must leave Chezmoi and Mise usable.

### Validation and exit criteria

Test piped installation, missing controlling terminal, missing Git and COPR
support, new checkout, accepted symlinked checkout, existing compatible, dirty,
wrong-origin, non-repository, engine-schema mismatch, rerun, missing-dotfiles,
and interrupted cases in disposable homes and a Fedora VM. Test known,
ambiguous, and unknown DMI and PCI facts without letting resolution inspect
hardware. Test development profile gating, Mise presence and user ownership,
step ordering around the handoff, refusal to run any user-scope step as root,
failure retry, and a manually deleted runtime or Cargo tool. Exit
when the one-liner reaches init safely, direct Chezmoi use works both with and
without Nimbus, and the Mise handoff preserves all three ownership boundaries.

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
LUKS2, and Btrfs subvolume topology and the recovery requirements; on
2026-09-02 the owner replaced its custom snapshot manager with Snapper. Nimbus
declares the Snapper configurations, creates pre and post snapshot pairs around
reviewed operations, and owns the boot and EFI archives, manifest, restore
guide, retention, and space preflight. Home, logs, caches, swap, VM data, and
container data stay outside. Retain the newest three complete points and
protect unresolved failures; after eligible cleanup, less than 20 GiB of
Btrfs-aware usable space blocks mutation.

Run this Snapper fit check in a disposable Fedora VM with the exact layout
before freezing the implementation, and record the results in TASKS.md:

- `snapper create-config` on a root whose `/.snapshots` is already a separate
  mounted subvolume, since Fedora's Snapper expects to create it
- pre and post pairs with the `number` cleanup algorithm, `NUMBER_LIMIT`, and
  `important` userdata as the protection mechanism for failed operations
- timeline and background cleanup disabled so only Nimbus creates and retires
  points
- a second configuration for `flatpak` and how its pairs are tied to a root
  point
- manual restore on this layout without `snapper rollback`: create a writable
  snapshot from `/.snapshots/<n>/snapshot` and swap `root`
- Btrfs-aware free-space measurement as a Nimbus preflight, separate from
  Snapper's own cleanup limits
- confirmation that no excluded subvolume nests below `root`

Upgrade may refresh native metadata, shows its own exact reviewed system plan,
never prunes, and does not silently apply unrelated desired-state drift. It
blocks with a direction to run `nimbus apply` when that drift is a prerequisite.
After system verification it offers a separately approved Topgrade phase that
reads the user's Chezmoi-owned configuration and runs only the declared
allowlist through `--only` plus `--no-self-update`, so system, Flatpak,
firmware, Nix, Chezmoi, and Git repository steps never run from it. The plan
labels this as command-level review
because Topgrade dry-run does not resolve downstream versions, and reports
that the home subvolume is outside the recovery point.
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
guest data. Exercise the Topgrade `--only` allowlist, command preview,
refusal of sudo and system managers, partial user-step failure, and the
explicit lack of home rollback. Prove that `nimbus upgrade` remains
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
nimbus windows setup|status|start|connect [--keep-alive]|stop|remove|purge-data
nimbus launch browser [URL] [--private]
nimbus launch webapp URL
~~~

Postinstall presents typed 1Password, fingerprint, NVIDIA MOK, Windows setup,
logout, and reboot work. It delegates to the same component actions and never
runs arbitrary scripts. Certificates, gaming resources, and virtualization
host setup remain normal plan and apply resources; external application-data
repositories remain outside Nimbus.

Q-010, as amended, selects the `dockurr/windows` container on Docker with KVM,
following Omarchy's `omarchy-windows-vm` as the reference implementation, on
top of the `docker` component. The one supported guest
is contained below `/var/lib/nimbus/windows/` on the `windows` subvolume with a
root-owned Compose definition, a user-owned `0600` credentials file, and
loopback-only ports. `setup` is the entry point: it stages the `windows-vm`
profile through the normal diff, plan, and approval when missing, then handles
credentials and the unattended install; `remove` is the profile removal with
the same review. The other runtime commands never install a missing component.
`connect` starts the guest when needed, waits for Windows, opens FreeRDP, and
stops the guest afterwards unless `--keep-alive` is set. Removal stops and
removes the container but preserves the data root. Purge rejects a
running, symlinked, foreign, or escaped target, names the exact directory, and
requires a second confirmation. The Compose definition is reproducible; the
guest disk is not part of Nimbus recovery and may be lost and recreated from
external sources. Phase 8 also decides how FreeRDP receives the password
without exposing it in process arguments, and adopts and verifies Dockur's
own verification of the downloaded Windows installation media before the
`windows-vm` profile is accepted.

Launch helpers safely resolve the XDG browser, translate supported private-mode
flags, use an explicit Chromium-family webapp fallback, accept only HTTP(S)
URLs, and execute no shell-derived command. Chezmoi retains ownership of the
keybindings and desktop entries that call them.

Validate the typed task list in terminal, non-terminal, and JSON modes. Test the
Windows lifecycle, absent component refusal, root-owned Compose integrity,
credentials-file mode and non-disclosure, path containment, normal removal,
preserved data, keep-alive, double-confirmed purge, and container
reconstruction in a disposable VM. Test browser-family private flags,
desktop-entry parsing, fallback selection, argv preservation, invalid schemes,
and missing browsers without writing user configuration.

## 9. Interactive dashboard

### Outcome

Deliver the dashboard behind bare `nimbus`: overview, profile and component
selection, package pickers, review and apply, and post-install tasks, as one
presentation layer over the commands delivered in Phases 1 through 8.

### Context and decisions

Every screen renders existing structured output and calls existing command
paths; the dashboard adds no resolver, planner, state, or approval logic.
Staged manifest edits live only in the session until the review screen's
approval writes and applies them through the Phase 4 path. `nimbus init`
reuses the selection screens for a new machine. Q-012 selects the widget
library, shared with the command-line pickers introduced in Phase 4, so the
pickers are the first delivered pieces of the dashboard.

### Risks and recovery

A dashboard can hide what a command would have shown. Every screen names the
equivalent command, every mutation goes through the same review screen, and
nothing runs on navigation. Terminal size and keyboard-only operation are
tested. The phase adds no new mutation, so recovery is the underlying
command's recovery.

### Validation and exit criteria

Test each screen against golden structured output, staged-edit discard on
exit, approval parity with the command line, non-terminal fallback to grouped
help, and keyboard-only operation in a disposable VM. Exit when the dashboard
can select profiles, components, and packages, review and apply the resulting
plan, and present tasks without any behavior the commands lack.

## 10. Performance and size

This track never closes. It starts once Phase 9 is delivered and takes one
measured improvement at a time, each as its own bounded task with a number
before and after.

### Outcome

Nimbus stays fast enough that reviewing a plan feels immediate and applying
one is bounded by DNF and the network, not by Nimbus, and the binary stays
as small as a stripped static Go program can be.

### Candidates

- Binary size: `just build` already strips symbols and debug data; watch
  the dependency count, keep the Charm libraries the only interactive
  dependency, and refuse a module that a standard-library call would cover.
- Inspection: one `dnf5 repoquery` per run rather than one per check,
  facts cached inside a command instead of re-inspected between steps,
  and the DNF preview reused between plan and the apply digest check when
  nothing changed.
- Apply: independent operations such as key downloads and Flatpak installs
  run concurrently only where ordering does not matter and the plan shows
  the same steps; DNF transactions stay single and sequential because DNF
  owns that lock.
- Startup: no work before the command is parsed, so `nimbus --version` and
  help stay instant.

### Rules

- Measure first with the disposable VM and record the numbers in TASKS.md;
  an optimization without a measurement is not merged.
- Correctness, review, and verification are never traded for speed: every
  mutation still appears in the plan and still gets its receipt.
- The user-visible command surface does not change for performance work.

## Later

- hibernation and supported boot-resource management
- LUKS2 inspection and reviewed TPM2 auto-unlock bound to PCR 7
- locally built unified kernel images through `systemd-ukify`, signed with
  the machine owner key, with the TPM2 slot rebound to a signed PCR 11
  policy; after recovery points, with its own restore drill
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
