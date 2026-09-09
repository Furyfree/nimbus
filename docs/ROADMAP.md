# Nimbus roadmap

This file owns implementation order, phase-specific decisions, risks, recovery,
validation, and exit criteria. The complete phase-independent product contract
lives in [SPEC.md](SPEC.md). Current checkboxes and evidence live in
[TASKS.md](TASKS.md). Decision rationale and unresolved questions live in
[DECISIONS.md](DECISIONS.md); this roadmap places the gates that affect each
phase.

Phase 6 is published as **Nimbus 0.2.0** after successful candidate
installation, reboot, and normal login. Recovery, portals, keyring unlocking,
and the signed-package VM trial remain explicit follow-up gates. Phase 7 is
prepared on its separate branch and is not included in this release.
Phase 5 is complete following the 0.1.1 installation. Its remaining prompt
order and redundant-confirmation improvements now belong to Phase 6.

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
its digest. Planning writes nothing. It shows the operations, prune
candidates, and the availability and freshness of update information as
separate sections. Prune candidates stay informational unless
`sync --prune` explicitly enables them. Before Phase 7, unavailable upgrade
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
fixtures, and a disposable Fedora VM comparison. Check ownership views against
native receipt identities and ambiguous legacy receipts, and count retained
sources as unchanged. Exit when the plan explains
every selected package without executing a mutating command.

## 4. Controlled DNF apply and receipts

### Outcome

Run the shown plan after one question, report what differed, add the
focused package, profile, and component selection workflows, verify each
successful operation, and record complete versioned receipts under
/var/lib/nimbus.

### Context and decisions

Add the user-owned kernel operation lock at
`$XDG_RUNTIME_DIR/nimbus/operation.lock`, the one question, direct sudo
native commands with their output visible, the narrow atomic record action,
partial-failure behavior, adoption, owned removal, the differences report,
and explicit pruning through `nimbus sync --prune`. Acquire the lock after
the answer and before any write, and hold it through verification and
receipt recording.

`packages install [QUERY]` and `packages remove [QUERY]` use interactive
multi-selection, change only the selected machine manifest, and show both the
manifest diff and complete system plan before the question. They then reuse
the sync path without the system upgrade. Remove is limited to desired or
Nimbus-managed packages; eligible unmanaged packages remain the
responsibility of `sync --prune`. `profiles add|remove` and
`components add|remove` edit the manifest's selection lists through the same
diff, plan, question, and sync path; a
profile change prints the direct Chezmoi re-initialization command. Nimbus
leaves every manifest edit as an uncommitted Git change.

Bootstrap packages retain correct DNF install reasons. Removal is permitted
only from the lifecycle recorded in the receipt.

### Risks and recovery

This is the first mutating phase. Tests run only in disposable Fedora VMs,
and the phase opens with the first VM run, which also compares doctor and
plan output with the recorded fixtures before any apply is attempted.
Failed verification never creates a successful receipt. DNF resolves again
at install time, so the result may differ from the preview; the closing
report names such differences, and the receipts record what is actually
installed. A failed transaction
retains the reviewed desired manifest, accurate prior receipts, and native DNF
history needed for recovery. Status and plan then expose the remaining drift.

### Validation and exit criteria

Exercise install, package-picker install and remove, profile and component
add and remove including the resulting owned removals, profile exclusion,
already-present adoption, verification failure, digest change, interrupted
operation, owned removal, explicit pruning, and protected dependency cases.
Check colliding legacy receipt names, rejected state encodings, and each
package's provenance through a merged plan and recorded transaction.
Exit when a small representative DNF component and the package shortcuts can
complete and reverse safely in a disposable VM.

## 5. Bootstrap, initialization, and Chezmoi handoff

### Outcome

Provide a fresh-install flow that obtains Nimbus and its checkout, selects a
tracked machine, applies the system and Chezmoi source, and installs the
selected user tools. Each stage reports success, failure, or its unmet
dependency; required incomplete work exits unsuccessfully.

### Context and decisions

The supported one-liner pipes the repository's raw `main` `install.sh` into
Bash. That minimal script verifies the platform and normal-user context,
obtains Git through DNF, clones or validates `~/.local/share/nimbus`, and runs
the checkout's versioned `bootstrap` with the child input attached to
`/dev/tty`. The engine is distributed through the signed `furyfree/nimbus`
COPR. Release `v0.1.0` and COPR build `10958015` succeeded; the checked-in
project key verifies that RPM. Bootstrap stops before engine installation if
its reviewed key files are absent. It verifies the key fingerprint and RPM
signature with an isolated keyring before native DNF installation. Existing
engines must be DNF-owned at `/usr/bin/nimbus`, with no other engine shadowing
that path. The public one-liner and the 0.1.1 logging, binary-provider, and
repository-convergence follow-up passed on the restored VM. Prompt order and
redundant-confirmation follow-ups move to Phase 6; TASKS.md records their gates
and the installation evidence.

Source release preparation is a manually dispatched workflow on main, taking
an existing version tag on that branch. It runs the tagged commit's local gate
and verifies a vendored offline build before creating a draft release. The
owner publishes the inspected source assets, then separately dispatches the
COPR build. Pushes never release; no existing tag or release is replaced.

Neither script updates an existing checkout, provisions the workstation, or
handles Chezmoi. Chezmoi is an ordinary Nimbus-managed system package installed
by the reviewed first sync. The engine remains directly DNF-owned and checkout
updates remain direct user Git operations; Nimbus has no self-update path.

Q-006 and Q-007 are resolved. The common profile selects Mise and its build
prerequisites because the dotfiles tool configuration is global. Sync downloads
the official Mise installer, shows its digest, runs it as the normal user
before the handoff, and verifies the user-owned binary. Chezmoi owns the
native tool configuration and an after script that runs `mise install` on
every full Linux or macOS apply, under `MISE_SYSTEM_DEPS=warn` and
`MISE_AUTO_UPDATE=false`. Its Linux `~/.config/mise/conf.d/` fragments
declare user CLI tools for native Mise release backends, with VM Curator
selected only on x86_64. The development profile does not repeat them. Nimbus
installs selected system dependencies itself.
The common profile selects Terra's Typst RPM instead of a Cargo build.
Tinymist, Sheldon, and resvg use Aqua release entries; Caligula and VM Curator
use GitHub release binaries. All follow stable versions through Mise. The
after script verifies replacements before targeted obsolete Cargo cleanup;
`cargo-update` is removed. Fresh init disables the optional SSH integration
without prompting unless `--onepassword-ssh` is supplied. Bootstrap gathers
sudo before prerequisites, keeps the credential alive through init, and
shows and applies the init plan without confirmation. Private logs retain the
20 completed runs, with stage timings and secret-capable output excluded.
System Python 3 supervises handoff process groups while preserving the terminal;
cancellation must reap descendants before completion or temporary-file cleanup.
Successful native commands may leave their own background services running.
Active and protected logs do not consume completed-run retention slots.
Post-package repository reconciliation must leave no duplicate repairs for
the next read-only plan.
A later Chezmoi apply restores missing user tools; upgrades remain explicit.
Standalone dotfiles use requires Mise already installed. Missing Mise and
native install failures fail apply, while diff and preview install nothing.

nimbus init writes only the local selector and a new machine manifest when
requested. It uses `--machine ID` or an existing trusted selector; missing
selection stops with available IDs. For an explicit `--new ID` it
proposes the components whose `[detect]` rules match the chassis kind and
the display adapters, asks for profiles and the dotfiles repository, and
writes only the accepted answers into that manifest. Existing tracked
manifests are loaded unchanged. The first detector fixtures
cover the MSI Z690 desktop with Intel and NVIDIA graphics and the HP EliteBook X
G1a with AMD graphics, including the `ddcutil` and `brightnessctl` split. The
handoff passes `machine`, `managed_by_nimbus`, and the ordered profile IDs
through the Chezmoi prompt flags specified in SPEC.md; hardware components do
not cross it.

Nimbus and dotfiles use public HTTPS clones as the intended bootstrap path;
repository publication remains a separate owner action. Init initializes a
missing Chezmoi source and runs apply, including Mise runtimes and Cargo tools.
The tracked manifests need no second Nimbus user-tool pass; ordinary sync does
not invoke Chezmoi or install tools from its configuration.
The `dotfiles diff`, `apply`, and `update` convenience commands delegate to
Chezmoi; update explicitly uses its native Git pull and apply behavior. Direct
Chezmoi commands remain supported. Later profile changes print the
refresh command and doctor reports a stale Chezmoi selection. The dotfiles
repository adopted the profile handoff on 2026-09-02 and holds no machine
manifest. Q-015 resolved the refresh as `chezmoi init --prompt` with every
value supplied, including the current 1Password answer. Before this phase
exits, the dotfiles template must declare `Machine` and `ManagedByNimbus` and
store the profile list without validating it, and both the initial and
refresh flags are verified against the real template in isolated homes.
The dotfiles handoff regression now covers initial input, unknown future
profile IDs, a refreshed machine/profile selection, preservation of the SSH
opt-in, and apply/repair through the native Chezmoi lifecycle with fake Mise.
Real Fedora tool installation remains a separate VM exit gate.

### Risks and recovery

Never replace an unrelated checkout or remote. Require a controlling terminal,
attach only the checked-out child script to `/dev/tty`, and stop on an invalid
existing target. A failed installation leaves native DNF state and the checkout
independently recoverable. A failed initialization leaves the system usable and
reports direct Git and Chezmoi recovery. Failed dotfiles tool installations
are retried by the next Chezmoi apply; explicitly Nimbus-owned user steps are
retried by sync. Nothing below home has a recovery point. Removing Nimbus
must leave Chezmoi and Mise usable.

Stabilization includes native RPM name and architecture identity, conservative
legacy receipt removal, complete DNF transaction reporting, current repository
key verification, EOF refusal, platform checks, and reloading approved
selection and definitions under the operation lock. Init holds that lock across
its stages. A failed dotfiles apply skips its dependents, while independent
user tools may continue after another tool fails. A retry preserves completed
native work and checks the current state again.
Validation also covers ignored init flags, malformed definition IDs, DNF value
line injection, conflicting native repository overrides, safe lock targets,
and receipt-only retirement of already absent Flatpaks. The installer must
prove it can open its controlling terminal before proceeding. Origin and
commit changes invalidate approval separately from the plan digest. Explicit
removals disable dependency cleanup; source replans show newly resolved
erasures before execution and retain their notes in the closing report.

### Validation and exit criteria

Test changed definitions during approval, concurrent init, invalid new manifests
without writes, EOF, unsupported platforms without metadata refresh, multilib
install and removal, collateral DNF changes, key mismatch, and partial summaries.
Verify a stored single-key file can prepare a repository whose maker publishes
a multi-key bundle, while the bundle itself remains rejected.
Verify Chezmoi and native Mise failures reach the stage summary, a retry
restores missing tools without changed configuration, and ordinary sync does
not repeat the dotfiles tool installation. Verify standalone apply, missing
Mise, Windows exclusion, and read-only preview without installation.
Exercise Chezmoi data with both `Profiles` and `profiles`, in either order,
through inspection and init so derived platform IDs cannot replace selection.
Keep explicit setup instructions visible after the final summary on successful
and failed handoffs. Verify that ordinary compiler output is not repeated and
that source-build prerequisites are selected before Cargo tools run.
The dotfiles Mise configuration must declare the intended runtimes, including
Rust, before a successful end-to-end installation can be claimed.

Test piped installation, missing controlling terminal, missing Git and COPR
support, new checkout, accepted symlinked checkout, existing compatible, dirty,
wrong-origin, non-repository, engine-schema mismatch, rerun, missing-dotfiles,
and interrupted cases in disposable homes and a Fedora VM. Test known,
ambiguous, and unknown DMI and PCI facts without letting resolution inspect
hardware. Test Mise bootstrap without development, presence and user ownership,
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

The first milestone is a reboot into Noctalia greeter, a working normal
desktop, and an independent recovery session. Workstation defaults follow
that milestone. Installing the greeter package alone does not meet it.
Retain Phase 5's installer and ownership architecture; these resources belong
in the existing inspect, plan, apply, verify, and remove lifecycle.

### Delivery order

1. Inspect the Fedora VM's boot target, greetd unit, display-manager selection,
   greeter command, package-provided session entries, and journal. Compare them
   with the shipped package files and upstream documentation. Record the actual
   activation failure before selecting configuration.
2. Extend the strict definitions, resolver, facts, planner, executor, and
   receipts for the system units, memberships, files, and triggers the desktop
   needs. Build on the existing file provider. Define enabled and running unit
   state separately, preserve pre-existing memberships, and report logout or
   reboot needs. Unknown ownership blocks removal; failed verification writes
   no successful receipt. Read-only commands never start services or use sudo.
3. Provide and test the system-owned recovery session and a documented TTY
   restoration route before activating the normal graphical login. Choose a
   native package or narrow typed integration for files under /usr; generic
   system-file declarations remain limited to /etc.
4. Declare greetd/Noctalia activation, session discovery, required memberships,
   and portal selection in the desktop component. Include any boot-target or
   display-manager changes in the plan and verification. Chezmoi continues to
   own normal Hyprland/Noctalia user configuration. Do not silently displace a
   foreign display manager or interrupt an active graphical session.
5. Extend doctor for these resources and prove login, logout, portals, repeat
   sync, failure recovery, and removal on the graphical VM. Stop at this first
   milestone to record evidence before adding workstation tuning.
6. Deliver remaining service/group/file ownership and retirement behavior,
   fixed-argv trigger deduplication, and `files accept`. Then evaluate the
   workstation-default candidates below through the same resource lifecycle.

### Login keyring and desktop appearance

The first graphical VM login exposed a missing `gnome-keyring-pam` package.
Use Fedora's packaged greetd PAM integration to create/unlock the login
keyring during password authentication; preserve the packaged PAM files.
Verify fresh initialization and subsequent logout/login, including an existing
user-created default keyring. Do not store a login password in configuration,
remove keyrings, or disable their encryption. Passwordless authentication is a
separate integration question and must not be claimed from this test.

Chezmoi owns the selected Noctalia preferences and application theme hooks for
`hyprland-noctalia`; Noctalia owns generated palettes and theme outputs. Track
the white keyring dialog as a GTK appearance issue independently of unlocking.
Dotfiles' root `NOCTALIA.md` records template IDs, paths, application variants,
and visual checks. Validate the dialog and selected applications after a new
session rather than assuming that a generated CSS file proves adoption.

The signed-install follow-up boots kernel `7.1.13-200.fc44.x86_64`, runs
greetd, and reports successful PAM login-keyring unlocking. Complete repeat
login and appearance checks independently. The `hyprland-noctalia` profile
supplies Fedora's verified `adw-gtk3-theme` and `qt5ct` alongside Qt6ct.
Chezmoi supplies native GTK/Qt settings scoped to that profile. A fresh login
must prove toolkit selection, Brave Origin adoption, and dark/light switching
for the selected applications before the appearance follow-up is complete.

Fedora's portal service requires an active user `graphical-session.target`.
The owner accepted the tested plain Hyprland session for this release and
explicitly deferred UWSM adoption on 2026-09-09. Research native Hyprland session
integration and UWSM before choosing a lifecycle manager or adding a
session-specific doctor check. The tested VM's plain session left its graphical
target and portal inactive; retain this limitation without weakening Fedora's
portal dependencies. The final candidate package/configuration apply and repeat
no-upgrade plan passed. Recovery and portal interaction drills remain recorded
follow-ups rather than completed tests.

### Installer prompt polish

Phase 6 carries the final installation prompt improvements from Phase 5.
Init must show and apply its complete plan without a yes/no confirmation.
Use `--machine ID` or an existing trusted selector; missing selection stops
instead of opening a picker. Only explicit `--new ID` opens a machine/profile
dialogue. Sudo authentication and Chezmoi questions remain. Refuse unexpected
selector trust changes without prompting or overwriting trust. Ordinary sync
keeps its confirmation. Verify the complete prompt order and count on a clean
VM installation before closing Phase 6.
The bootstrap obtains only Git transport before the checkout.

### Candidate testing

Use a locally built candidate and its matching uncommitted definitions in a
new private VM directory. `just vm-stage` leaves stable delivery and the
selector intact; explicit checkout/machine flags select the candidate. A
candidate sync still changes the VM and shared receipts, so snapshot recovery
is the reset boundary. Test login/recovery before claiming the first milestone.
The [candidate guide](../tools/vm/README.md) records commands and limitations.
A separate beta COPR is reserved for later native RPM delivery drills, with
its own reviewed key and explicit opt-in; stable publication stays manual.

### Modern Go PR validation

Finish PR 18 with the two reviewed corrections: retain verified file ownership
and retirement when temporary payload cleanup fails, reporting that failure in
the closing differences; normalize CRLF repository lines before interpreting
blank lines and continuations. Prove both with focused regressions and the
complete local gate. Keep further modernization outside this change.

Native validation remains a follow-up on a disposable Fedora 44 x86_64 VM,
using the candidate guide above:

1. Confirm the target VM, working console access, and a restorable clean
   snapshot before staging or applying. Record the candidate's commit, dirty
   state, definition digest, binary checksum, and VM baseline with the results.
2. Stage the matching binary and definitions with `just vm-stage`. Run candidate
   validation, doctor, and `sync -n` with explicit checkout and machine inputs;
   inspect every planned mutation before the first approved sync.
3. Compare the first sync's closing report, native state, and verified receipts
   with its plan. Run a second preview and sync; require convergence without
   repeated mutations or lost ownership.
4. Exercise removal of a small candidate-only component with a managed file
   and activation trigger. Require previewed removal, verified receipt
   retirement, a converged retry, and continued console access. Restore the
   snapshot before testing another baseline.
5. From separate snapshots with valid legacy schema 1 and 2 state, verify reads,
   planning, and owned-resource handling. After a candidate write, check schema
   3 and confirm an older engine refuses it without writes. Restore the whole
   snapshot for rollback; never downgrade the marker by hand.

Stop and preserve evidence on unexplained mutations, blocked ownership,
verification failures, or loss of console access. Restore the VM snapshot
before retrying. Exit with recorded commands and results for all four lifecycle
checks: first sync, convergence, removal, and legacy compatibility. This does
not close the outstanding graphical recovery and portal drills. Merging the
rewrite does not establish native validation or authorize a release.

### Workstation defaults

Inspect Fedora's shipped and effective defaults before declaring overrides.
Candidates are ZRAM, justified sysctl settings, inotify and file-descriptor
limits, `vm.max_map_count`, systemd-oomd policy, journald limits, SSD trim,
hardware-specific udev/storage rules, laptop power/suspend behavior, Docker
defaults/log rotation, and selected gaming settings with a demonstrated need.

Each accepted setting needs an observed problem or owner requirement, evidence
for its value, machine/component scope, verification, and a removal/recovery
path. Record retained Fedora defaults and rejected candidates as decisions;
the list is not a requirement to override every setting. No global tuning
bundle or numeric values are approved by this plan. Hardware-specific changes
need checks on matching hardware, not just the VM. Doctor explains effective
state and owned drift without applying a setting.

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
Reuse Ghostty with user configuration disabled instead of installing Foot.
Explicitly remove Foot, Kitty, and nwg-panel; verify the recovery terminal
before removing Foot on an existing installation.
Those files use a native package or a separately specified typed integration;
they do not widen the generic system-file provider.

Before implementation, resolve the package-provided greeter/session launch
contract, recovery-session packaging, adoption of existing units and group
memberships, and how activation preserves a working console. Bind each choice
to inspected files and tests rather than guessing service names or commands.
Snapper recovery points, update orchestration, runtime launchers, the dashboard,
TPM unlock, UKIs, and owner Secure Boot keys are outside this phase.

### Risks and recovery

Service, greeter, portal, and compositor changes can prevent login. The recovery
session and removal path must be proven before the normal session is managed.
Report .rpmnew and .rpmsave files without modifying them.
Record the prior boot target, unit state, and owned file contents needed for
manual restoration; exercise that route from a console after a failed greeter
activation. Do not claim Snapper rollback before Phase 7's restore drill.

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

The first milestone must prove reboot to greeter, normal login, logout and
relogin, portal file picking and screen sharing, recovery with absent or broken
user configuration, and a second sync with no pending changes. Exercise failed
activation and removal without losing console access. Run the complete
`just check` gate and the native checks for each provider. VM success does not
prove laptop suspend or hardware-specific defaults. Close the full phase only
after the remaining ownership, `files accept`, selected-default, and installer
prompt gates pass.

## 7. Updates, constraints, and recovery points

### Outcome

Deliver the recovery and coordination around the upgrade step `nimbus sync`
already runs: enforce native constraints, coordinate the desktop-session
group, and create tested recovery points for disruptive transactions.

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

The upgrade step runs inside `nimbus sync`, after the definition changes of
the same run, so drift and updates are one decision; `--no-upgrade` leaves it
out. After system verification sync offers a Topgrade phase that
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
explicit lack of home rollback. Prove that the upgrade step remains
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

Desktop entries for terminal applications such as VM Curator, btop, and
lazydocker need a way to open the user's terminal with one command. Q-019
decides between these options before this phase starts:

- `Exec=xdg-terminal-exec COMMAND` in the Chezmoi-owned desktop entry.
  Fedora 44 ships `xdg-terminal-exec`, the freedesktop terminal-exec
  implementation, and the `hyprland-session` component already installs it.
  No Nimbus code; the terminal is whatever the user configured for the spec.
- `Terminal=true` in the desktop entry alone. Standard, but launchers differ
  in whether they honour it.
- A Chezmoi-owned script like Omarchy's `omarchy-launch-tui`, which
  hardcodes the terminal and builds the command line.
- `nimbus launch terminal COMMAND [ARG...]`, a typed wrapper that validates
  the arguments and hands the exact argv to `xdg-terminal-exec`, alongside
  the browser and webapp helpers.

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
  and facts cached inside a command instead of re-inspected between
  passes when nothing changed.
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
