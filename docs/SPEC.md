# Nimbus behavior

Usage: [wiki](https://github.com/Furyfree/nimbus/wiki).
Unfinished work: [TASKS](TASKS.md).

## Ownership

| Owner | Responsibility |
| --- | --- |
| Nimbus | System definitions, drift, approved setup and verified receipts |
| Chezmoi | User configuration, Mise and Topgrade configuration |
| Mise | Declared user tools |
| COPR | Engine and application-helper packaging |
| Topgrade | Coordinating configured update steps |
| Native tools | Transactions, services, credentials and application state |

TOML definitions hold the software inventory and compatibility requirements.
Nimbus manages declared root-owned files under `system/`, preferring native
drop-ins to replacement of package files. Boot assets require a supported
activation and removal path. User configuration belongs to Chezmoi; sudo
permission does not authorize editing dotfiles. Nimbus's selector, checkout,
private diagnostics and setup evidence are operational state.

Native installers own their applications, updates and removal. Removing a
helper RPM or deselecting it does not uninstall its application. Installer
`effects` describe additional changes in the approval preview and JSON;
they are plain descriptions, never executable input. Sign-in stays native.

## Definitions and compatibility

Definitions use strict, versioned TOML. Reject unknown fields, invalid
references, conflicting resources and dependency cycles. Hardware detection
may propose selections during init, never change an existing machine itself.

Keep desired definitions, observed native state and verified applied state
separate. Unknown inspection is not absence. Repeat sync converges without
reapplying unchanged resources. Nimbus never commits or pushes definitions.

The selector holds checkout, machine, origin and channel, not desired state.
Schema 1 reads as stable; sync migrates it to schema 2 without changing its
identity. Announce migration in the preview and closing report; `--plan`
writes nothing. Unreadable selectors stop before mutation. Init keeps the
recorded channel unless explicitly changed.

Channel switching verifies the destination's pinned COPR key, changes the
checkout branch and engine repository, uses native distro-sync, records the
channel and validates the checkout. Refuse dirty checkouts, foreign repository
files and engines unable to read the applied-state schema. Stable follows
`main`; develop follows `develop`. Each has its own pinned key. Versioning
and release preparation are in [tools/release](../tools/release/README.md).

`channel` reports status without mutation. `channel switch stable|develop`
uses the saved, origin-verified checkout and asks before invoking its
installer under the operation lock. An aligned channel is a no-op. Switching
does not run workstation reconciliation; run sync separately afterwards.

Selecting an installed package may adopt it. Deselection may remove owned
packages no longer required elsewhere. Preserve unmanaged software unless an
explicit, bounded `--prune` plan includes it. Deselection does not generally
reset native application state.

Selected RPMs install and upgrade from their declared repository family.
Use only enabled concrete sources, retain native dependency resolution and
never fall back when the declared source is unavailable. Preview source
corrections, downgrades, replacements and dependencies before approval.
Record provenance after verification. System upgrades require source
corrections through sync first.

## Approval and execution

~~~text
load -> inspect -> plan -> approve -> recheck -> apply -> verify -> report
~~~

Previews perform no writes, downloads, authentication, service activation or
privilege escalation. They use local definitions and disclose that fetching
may change the eventual plan. Never fill an inspection gap by doing part of
an installation. Status and doctor diagnose without repairing.

Show managed changes before approval. `--yes` approves supported automation,
never a file conflict, manual GUI readiness, SSH opt-in or credential choice.
Output format does not grant approval; JSON mutation requires `--yes`.
An earlier preview cannot approve a later changed plan. New destructive
actions require review even if a native transaction changes after planning.

Serialize managed-state mutations with an operation lock. Recheck approved
inputs, ownership and observations before execution. Elevate per operation,
verify through native state, and record only successful work. Failed work
stops dependent operations and preserves accurate partial results for retry.
Changing definitions back does not promise reversal of every side effect.

Keep removal bound to verified ownership. Reject ambiguous identities,
traversal, symlink escapes and unsafe writable paths. Preserve changed or
foreign files and report conflicts. File acceptance must be explicit; a
matching filename does not prove ownership. Retire receipts only after
successful removal or the defined ownership-release operation.

Login-shell changes bind approval to the user UID and observed shell, require
an executable shell in `/etc/shells`, and verify the account afterwards.
Removing the selection relinquishes ownership without changing the shell.

## Install, sync and upgrade

Bootstrap runs as the normal user, obtains prerequisites, verifies the
engine signature and starts init. A rerun updates the installed engine from
its channel before validating definitions. Init needs an explicit or saved
machine; choosing a machine is not approval to install. Failed setup retains
completed work for retry. Missing metadata is reported, never refreshed by a
preview.

Ordinary sync:

1. Resolve identity and check the platform before decoding definitions.
   Authenticate sudo; authentication does not approve a transaction.
2. Refresh the engine repository metadata. On failure, stop. If a newer
   engine exists, stop before Git fetch and direct the user to combined
   sync/upgrade.
3. Check both Nimbus and Chezmoi repositories for clean, attached, tracking
   branches without local-only commits. Fetch and require fast-forward
   history. Never stash, reset or overwrite local or ignored files.
4. Reload definitions, inspect, preview, approve and recheck system changes.
   Unchanged reconciliation creates no mutation prompt or snapshot.
5. Offer one Chezmoi apply, including its scripts and tools, using the
   already fetched source. Skip it when no dotfiles are selected.
6. Report results and remaining setup without applying again.

Repository pulls precede system approval; declining later does not undo
those pulls. A failed Chezmoi apply may leave partial user configuration.
Sync installs needed dependencies but does not generally upgrade unrelated
software. Reboot/logout notices describe actual changes, not adoption.

Combined sync/upgrade verifies the installed RPM and replacement executable,
then restarts at the RPM-owned path with selection and approval arguments
preserved. Restrict the engine request to its signed repository, retain
native dependencies and do not enable testing sources or erasing. A second
engine update after restart stops instead of looping. Perform one sync and
one Topgrade run; failed phases skip later phases and remain failures.

Standalone upgrade delegates to Topgrade without fetching definitions.
Its system callback updates RPMs and system Flatpaks; it never calls sync or
recursively invokes Topgrade. Carry explicit checkout/machine overrides into
the callback. Require ready sources, constraints and snapshot configuration.
Do not install missing selected apps, edit files or reconcile services there.

System upgrades refresh enabled sources and reject unavailable metadata.
Preview the full DNF transaction and selected Flatpak updates before approval
and snapshots, using the same privileged cache for apply. Plan-only commands
use the unprivileged cache and disclose it. Only when both are current may
transactions and their snapshots be skipped.

## Local state and output

The setup-note catalog lives in the selected Nimbus checkout. Stable IDs and
revisions identify guidance; revise a note when the user's required action
changes. Human `setup-notes` marks successfully displayed revisions seen;
JSON does not. Init/sync count unseen guidance without consuming it.

Private XDG state records notes, task confirmations and verification.
Scope records by machine, replace atomically, protect concurrent writers and
preserve/report corrupt state. Help, status and previews create none.
Native observations take precedence over historical completion. User state
is never synchronized by Chezmoi or mixed with privileged receipts.

Keep installation logs private. Authentication input, secret-capable output
and private diffs stay outside transcripts. Terminal handoff preserves live
progress, resize, cancellation and the invoking sudo session. Report logging
failures; do not invent progress or silently claim successful recording.

The closing report distinguishes completed, failed and skipped work,
verification problems and remaining setup. Session-dependent checks stay
unknown until a session exists. Show pending pre-reboot work before the
reboot instruction. Color supplements words, never replaces them; respect
`NO_COLOR`, plain pipes and JSON. Native tools retain their own output.
Ordinary exit codes are success 0, failure 1 and usage 2; delegated upgrades
retain native failure status. JSON is a versioned interface.

Run records contain only engine/selection identity, commit when available,
timings, counts and outcomes. Exclude command output, errors, arguments and
configuration contents. Keep them private and bounded; preserve active runs.
A record left running after termination is not success or completion evidence.

## Guided setup

Tasks inspect local facts without authentication or incidental activation.
Separate service activity from startup enablement and setup completion from
current availability. A historical verification cannot establish current
state. Unknown checks remain unknown; an idle fingerprint daemon needs
explicit activation approval.

Each task previews effects, checks prerequisites, takes the operation lock,
rechecks selection and verifies native results. Exit zero alone does not
establish completion. `--mark-done` confirms a manual prerequisite and still
verifies; `--reset` clears evidence without undoing setup. Native tools own
removal. JSON supports inspection and previews, not task execution.

### Credentials and identity

- Fingerprint status uses non-activating D-Bus calls. Approved setup may
  activate fprintd and enroll only when needed; mark-done never enrolls.
  Preserve password login and never read biometric templates.
- 1Password SSH/Git integration defaults to no; `--yes` cannot select it.
  Save an approved choice through Chezmoi's native prompts, with no apply
  at that stage. Guided GUI readiness needs confirmation. Sign-in output
  stays private. Apply only selected integration targets without scripts,
  retaining conflict prompts; never create replacement SSH keys or test
  remote authentication. Private `--diff` is separate from previews.
- Tailscale sign-in requires approval and uses its native browser flow.
  Operator-only changes preserve a stopped connection. Verify initial
  sign-in and operator state; never use profile reset as repair.
- Hostname setup asks the user to close browsers and verifies the static
  name; never delete browser locks or profile data.
- DTU setup preserves existing eduroam/DTUsecure profiles unless their
  exact replacement is approved; keep is the default and `--yes` cannot
  bypass it. Read credentials only afterwards, through hidden input or the
  caller's native 1Password CLI. Validate DTU identity and the pinned CA
  bundle; never weaken server-name or TLS checks or install global trust.
  Secrets use memory/private pipes, never arguments, environment or logs.
  Disclose NetworkManager's root-only, unencrypted password storage before
  approval. Verify saved credentials privately only during setup. Configure
  autoconnect before a separate connect-now prompt. Off-campus absence is
  valid configuration, not proof of connectivity; failed scans stay unknown.
  Reset/deselection preserves profiles, credentials and the CA.
- Account-picture approval binds the source and current icon hashes;
  register through AccountsService and verify content. Never write its
  daemon files directly.

### Desktop state

Lockscreen repair is an explicit exception to runtime ownership: remove
only Noctalia lockscreen-widget overrides, after a private exact backup.
Require an unlocked local session, closed editors and a verified invoking-user
process. Recheck process identity and file hashes under lock. Reject unsafe
paths and hard links; preserve unrelated values and formatting. If orderly
shutdown times out, leave settings untouched. Restart after failure or
cancellation; never force-kill or restore backups automatically. Verify saved
and effective layouts, allowing only equivalent screen-coordinate scaling.
Ordinary sync never performs this repair.

Plugin setup verifies actual runtime files and active-session compatibility;
intent or cached catalogs alone are insufficient. Unreadable state and foreign
cache ownership block changes. Use Noctalia/HyprPM native update and activation
flows, verify each stage, preserve native trust prompts and report timeouts
with partial results. Sync never starts a desktop to download plugins.

Browser launches validate URLs and use argument vectors without a shell.
Private mode cannot fall back to an ordinary window. Active local UWSM
sessions use `uwsm-app`; a missing wrapper fails. Other sessions launch
directly. Launching never installs or configures a browser.

### Local agents

Agent-proxy setup verifies pinned upstream source and uses the native
installer and Copilot provider API. Chezmoi owns static proxy configuration;
native stores own credentials. Nimbus owns only private registration and
model ownership. Inspect without provider calls or credentials. Discovery
may prune only Nimbus-owned entries after complete success; preserve manual
entries and failures. A closed Copilot app defers refresh without mutation.
Only opted-in combined sync/upgrade refreshes after Topgrade.

Reset preserves services, credentials, configuration and model ownership.
Uninstall remains available after deselection, rechecks local unit ownership,
stops verified units and uses the pinned installer without purge. Foreign
units, overrides or provider identities block deletion. Remove only the
recorded loopback provider; an unavailable app leaves an explicit manual
cleanup instruction. Preserve Copilot, its credentials and Mise tools.

Zeron's daemon defaults off without opt-in. Sync and combined upgrade may
preview its native removal; standalone upgrade does not reconcile it.
Setup/disable records the choice only after native service verification.
Corrupt evidence blocks changes. Preserve GUI state, credentials and linger.
Both agent removals retain Fedora's validated root-owned timeout drop-in;
other unexpected overrides block service changes.

## Desktop, boot and recovery

Fedora owns partitioning, encryption, boot keys and major-version upgrades.
Nimbus does not enroll TPM policies or manage disk key material. Greetd
initial-session auto-login follows the disk passphrase; logout returns to
the greeter. Keep package PAM policy intact. Chezmoi creates the passwordless
default keyring only when absent; never overwrite a live keyring from a
template. Greeter appearance authorization uses its native CLI and Polkit
rule after approval; Nimbus does not duplicate that rule.

NVIDIA MOK setup preserves the akmods key pair. Missing keys require native
creation by the user. After approval, rebuild mismatched modules, refresh
the boot image and submit enrollment through native prompts. Nimbus never
receives the MOK password, reboots or weakens Secure Boot. Interpret native
enrollment results, including denylisted and unreadable states, without
turning prior verification into current success.

The boot-theme marker gates the engine's inert GRUB/Plymouth payload.
Preserve Fedora BLS entries and the EFI stub. Menu rebuild failure falls
back to Fedora entries; kernel hooks must not fail a kernel transaction.
The default entry and Previous kernels use native identities, not titles.
Secure Boot uses the built-in GRUB font. Activation selects the theme before
rebuilding every installed kernel's initramfs; preserve the rescue image.
When engine payload changes, rerun the relevant activation once.

Theme removal preserves a user-selected replacement, or restores the usable
previous theme recorded before Nimbus took over. Unknown, missing or Nimbus
previous themes block removal. Retire activation receipts only on success.

The NVIDIA display workaround applies only when the discrete GPU drives the
monitor. Preserve early passphrase input and the host Plymouth configuration;
do not couple the prompt to NVIDIA module signing/build order. Physical
verification requires typing a passphrase on that machine.

Accepted boot limits: default-entry changes need a later menu rebuild;
the forced timeout also applies after a failed boot; reopening the submenu
can show no entries; a newly added kernel may await a successful rebuild;
the theme overrides native timeout/auto-hide preferences; BIOS is untested.
Rescue or a one-boot removal of `rhgb` provides a console unlock fallback.
TTY repair and owned-resource retry do not constitute automatic rollback.

### Snapper

Use native Snapper with a Btrfs root. Adoption of empty snapshot storage
requires verified ownership, mode, identity and emptiness, rechecked before
mutation. Retry interrupted adoption only with the exact marked configuration
and still-empty storage. Never delete foreign configurations or populated
storage to make setup succeed.

Approved mutating system phases take before/after snapshot pairs. Previews
and no-ops create none; first setup discloses the missing before snapshot.
A failed before snapshot stops mutation. Later failure still attempts the
after snapshot and cleanup while preserving the original error. Interruption
waits for the current native command and finalizes under lock; forced
termination or power loss cannot guarantee this.

Retention is a target, not a disk-space cap; preserve native pair/active
snapshot rules. Boot, EFI, home and other separate mounts/subvolumes are
outside root recovery. Require a restore drill before documenting a restore
procedure. Removal retires hooks and owned policy, preserving native
configuration and snapshots.

## Security and verification

Keep native signatures, HTTPS, pinned keys and approved origins. Review key
changes. Official Copilot RPM and WowUp AppImage helper exceptions verify
origin, GitHub checksum and artifact identity; any unsigned-RPM exception
is confined to that artifact's transaction. The helper signature does not
sign its downloaded application. Inspection never downloads artifacts.

Run as the normal user, with narrow native elevation and no root daemon or
passwordless sudo. Never put secrets in definitions, arguments, receipts or
logs. Keep SELinux enforcing, firewalld active and Secure Boot enabled on
hardware. The VM exception requires positive virtualization detection;
unreadable checks stay unknown. Docker membership is root-equivalent.

Tests exercise behavior with controlled examples, not current package
preferences. Preserve preview/cancellation, ownership, compatibility,
partial-failure, retry and convergence checks. Use isolated HOME/XDG state
and native substitutes; opt-in native tests and disposable-VM trials remain
separate. Run the gates in [AGENTS](../AGENTS.md#verify).
