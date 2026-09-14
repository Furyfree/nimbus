# Nimbus specification

This is the accepted product scope.
[Remaining delivery work](#remaining-delivery-work) lists the gaps.
Implementation order belongs in [ROADMAP.md](ROADMAP.md).

## Purpose and ownership

Nimbus configures an already installed Fedora workstation and keeps its
system setup consistent with the selected machine definition. The first target
is Fedora 44 on x86_64, using Hyprland and Noctalia.

| Owner | Responsibility |
| --- | --- |
| Nimbus | Workstation setup, system drift, small helpers, post-install tasks |
| Chezmoi | User configuration, its drift, Mise and Topgrade configuration |
| Mise | Installing and updating its declared user tools |
| COPR repository | Packaging Nimbus, Voxtype, Copilot and WoWUp helpers |
| Topgrade | Coordinating the configured update steps |
| Native tools | Package transactions, services, application lifecycles |

Nimbus bootstraps required tools, performs the first Chezmoi handoff and
coordinates later repository updates and Chezmoi apply during sync.
Mise and the development profile's Zed and Zeron use their official user
installers when missing. Chezmoi owns their configuration; Zed owns its
application updates and removal through `zed --uninstall`. Zeron owns its
versioned application, generated user service and runtime state. Its component
selects Fedora's WebKitGTK 4.1 and JSON-GLib browser dependencies. Chezmoi links
the bundled launcher/icon and selects `zeron update` through Topgrade.
It does not keep a second list of Mise tools or manage ordinary user files.
Chezmoi works independently of Nimbus and does not install system packages or
escalate privileges. COPR installer helpers own application-specific download,
verification, installation and removal logic. Nimbus installs their RPMs and
offers small initial-setup actions; it does not track downloaded app artifacts.

An installer's optional `effects` list contains nonempty, single-line plain
descriptions of additional native changes. Missing-binary operations include
these disclosures in the approval preview and JSON plan and use medium risk.
They are descriptions, never independently executed commands. Existing
binaries remain untouched. Zeron's installer writes, enables and restarts
`zeron.service` and attempts `loginctl enable-linger`, with `sudo -n` as a
fallback. Lingering lets user services continue after logout. These effects
must be disclosed before approval; they do not authorize unrelated changes.
Application sign-in stays manual. Zeron's native updater owns engine restarts;
its daemon uninstall removes the service, not application data. Nimbus keeps
no installer receipt and does not remove native apps when deselected.

## System files and research

Nimbus stores the root-owned system configuration we choose to change: GRUB
inputs and theme assets, systemd system units/drop-ins, system-wide user-unit
policy, service configuration, udev rules and justified system settings.
Sources belong under `system/`, with targets and activation steps declared by
components. This is part of system drift management, not just package
installation.

Use `system/root/etc/` for the existing generic `/etc` file provider. Other
locations, such as GRUB assets under `/boot`, need an explicit supported
installation/removal path; they are not arbitrary file-copy destinations.
Prefer native configuration drop-ins over replacing package-owned files.
The prepared XDG user-directory defaults extend the native Fedora file with
Projects, Screenshots, Wallpapers and Recordings. Because Fedora already owns
that path, the prepared file remains unselected until explicit ownership
migration is supported. The generic provider must continue to reject an
unowned existing file.

Shell, editor, browser, desktop, Noctalia and per-user systemd configuration
below the home directory belong to Chezmoi. Vendor-generated service units,
such as Zeron's native installer output, stay with that vendor's lifecycle.
Requiring sudo for a system change does not authorize writing a user's dotfiles.
Nimbus's selector, checkout, private diagnostics, note display history and setup
evidence are operational state, not a second user-configuration collection.

The NVIDIA component masks RPM Fusion's `nvidia-settings -l` login loader
through an empty, root-owned unit file under `/etc/systemd/user`. This is a
[native systemd mask][systemd-unit-mask] for the generated autostart unit;
the NVIDIA driver and manual settings application remain available. It applies
to NVIDIA selections only and takes effect at the next login. The normal file
ownership rules govern repair and removal; existing per-user masks are separate.
The package-owned XDG desktop entry is preserved.

[systemd-unit-mask]: https://www.freedesktop.org/software/systemd/man/latest/systemd.unit.html

For system-setting research, start with Fedora and the relevant upstream
project. Compare [Omarchy](https://github.com/omacom/omarchy) and
[CachyOS-Settings](https://github.com/CachyOS/CachyOS-Settings) for ideas; other
maintained repositories can be useful when they address the same need.
Check the actual source and revision, Fedora compatibility, benefit and how
to undo the change. Record accepted choices beside the component or here;
record pending work in TASKS. Keep required attribution when adapting code.
Do not import a repository's configuration bundle, user files or performance
tweaks wholesale. These references do not add new package sources or defaults.

The Hyprland session component owns `/etc/noctalia/greeter.toml` and exposes
only that file through greetd's `BindReadOnlyPaths` setting at the greeter's
required declarative path. The service keeps its normal writable runtime
state directory for native sync. This uses systemd's documented per-unit
[bind mount](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html#BindPaths=)
without extending Nimbus's file provider beyond `/etc`. Systemd handles the
mount lifecycle; stopping the service removes its namespace, not the retained
host runtime files. New appearance or mount definitions require a new greetd
namespace after reboot and are reported in the plan. Unchanged adoption does
not request another reboot. No display-manager restart is automatic. The
[README](../README.md) documents activation, scope and account setup. The
[upstream greeter configuration](https://docs.noctalia.dev/greeter/configuration/)
keeps declarative appearance separate from mutable sync state.

The component's typed `greeter_passwordless_sync` flag selects constrained
appearance authorization for the invoking non-root local account in normal
init/sync. Resolution requires the shell and greeter packages and a single
component owner. The operation waits for package installation, checks stable
Noctalia >= 5.1.0, the helper's `secure-sync-v1` capability and the packaged
Polkit action's exact helper path and `--sync` argument. Native enablement
also checks executable trust and ownership of its own rule; Nimbus does not
write Polkit JavaScript, add a service or create a postinstall task.

Preview is unprivileged. An unreadable Polkit directory means a pending
administrator check, never disabled or verified authorization. After normal
approval and sudo acquisition, replan using only the fixed native status query
for that approved account. This permits already-enabled authorization to
converge without a rule rewrite or unnecessary snapshot. Unknown native errors
block; only the specific unprivileged directory permission failure is deferred.
If access is missing, delegate enablement to the native greeter CLI, then
verify its managed-rule status before recording a normal system receipt.
Receipts bind the machine, user and numeric UID; they never replace native
inspection. Installation logs allow only these fixed public greeter commands,
not arbitrary greeter or helper invocations. Chezmoi owns auto-sync preferences;
Noctalia owns generated wallpaper and sync state.

Deselection retires valid tracking without revoking native authorization,
preserving pre-existing and shared administrator choices. Explicit revocation
uses the native CLI; subsequent sync restores access while it remains selected.
Failures leave no completion record. Retry checks native state first, including
partial enablement. No automatic rollback overwrites another administrator's
rule. Old installed shells must be upgraded through the normal upgrade flow
before authorization; preview does not silently substitute legacy permission.
See README for commands and native removal.

## What Nimbus manages

- Selected RPMs and system Flatpaks, their declared sources and constraints.
- Owned system files, services, timers, group membership and boot target.
- Selected desktop, greeter, portal and hardware integration.
- The machine selector and records needed to explain and safely retry changes.
- Explicit setup actions such as fingerprint enrollment.

The owner chooses software and its source when reviewing definitions. Nimbus
uses native verification and checks that requested operations succeeded. It
does not audit application source or test every installed application's UI.

## Definitions and drift

| Location | Contents |
| --- | --- |
| `nimbus.toml` | Compatibility, repositories and DNF settings |
| `machines/ID.toml` | Shell, profiles, components, packages and constraints |
| `profiles/` | User-facing bundles of packages and components |
| `components/` | Capabilities, dependencies and owned system resources |
| `system/root/etc/` | Sources for generic managed files below `/etc` |
| `~/.config/nimbus/config.toml` | Checkout, machine ID and approved origin |
| `/var/lib/nimbus` | Applied-state records and package baseline |
| `$XDG_STATE_HOME/nimbus/` | Private note history and setup evidence |

The selector holds no desired package or configuration state. Definitions are
strict, versioned TOML. Invalid references, conflicts and dependency cycles
fail validation. Hardware detection proposes selections during init; it does
not silently change an existing machine later.

The optional machine field `shell = "bash"` or `shell = "zsh"` selects the
invoking user's default login shell, with `machine:shell` provenance. Common
installs both shells; Chezmoi keeps both configurations. New manifests choose
Bash. Package exclusions cannot remove the chosen shell. The plan shows the
old and new shell and `usermod --shell PATH -- USER`, after package setup.
Read-only inspection uses `getent --service=files passwd USER`, restricting
this feature to the non-root local invoking account. Changes require the
normal plan approval, an executable shell listed in `/etc/shells`, and the
same observed account UID and shell immediately before execution. Re-read
the local account afterwards; a failed command or verification never gets
a successful receipt. Drift is repaired through the next approved sync.
`/bin` and `/usr/bin` spellings of the same supported shell do not cause a
change. Log out and back in to use a changed default; running shells remain.
Removing the field retires verified ownership for that account while keeping
its current login shell. It does not restore an older shell or remove packages.

Desired state comes from definitions, observed state from native inspection,
and applied state from verified operations. Unknown inspection results are
not evidence of absence. Repeating sync after success should converge without
reapplying unchanged resources.

Each machine may select different hardware and software. Ordinary sync updates
the selected checkout before loading its new definitions. Profile, component
and system-file changes do not require a new engine build unless they need
new engine functionality. The executable updates through DNF/COPR. Nimbus
never commits or pushes definitions. Local edits can be previewed with
`sync --plan`; commit and publish them before ordinary sync.

Selecting an already installed package can adopt it. Deselection may remove
it once no remaining selection requires it. Unmanaged software is preserved
unless explicitly included through the separate prune option. The original
package baseline protects pre-existing packages from unmanaged pruning.

ble.sh comes from the owner's verified COPR. The desktop uses the verified
Noctalia LibrePods fork; its priority precedes Terra while retaining Fedora
as the preferred source. Copilot keeps the existing installer-helper source.

The TOML files are the software inventory, not a duplicated Markdown list.
Browse [profiles](../profiles), [components](../components) and
[machines](../machines). Bare package names mean Fedora; other package sources
use their declared repository prefix. Chezmoi owns the user-tool and plugin
inventories, including AI Usage and webapp desktop entries.

Selected RPMs are restricted to their declared repository family during install
and system upgrade. DNF5 `do` applies `--from-repo` per package group in one
transaction; dependencies and unselected packages retain native sources. Pass
only enabled concrete repository IDs, never enable a disabled testing source
through this option. Repository priority cannot override an explicit selection.
Bind resolved provides and architecture-qualified identities to the same policy.
Reject a preview that violates it, and verify changed native provenance before
recording success. Missing candidates or unavailable inspection block execution;
there is no fallback to another source.

An installed RPM with known provenance outside its declared family needs a
reviewed source correction during sync, even if Nimbus already owns a receipt.
Preview native `distro-sync --from-repo` for its existing architecture, or
`reinstall` when the version already matches. Show downgrades, replacements and
dependencies before approval. Record the verified old/new source evidence only
after success. Preserve historical installer/local provenance when no repository
can be established; new transactions still require verifiable allowed sources.
System upgrades require these corrections to be completed by sync first. This
policy governs Nimbus transactions, not package-manager commands run separately.

ChatGPT uses its official DNF repository. Voxtype is packaged in the owner's
COPR. Copilot uses the official RPM and WoWUp CurseForge the official AppImage,
both through COPR installer helpers. Updating the helper RPM alone does not
prove that the downloaded app is installed. Copilot helper 0.3.0 and newer
queues a separate app installation job after DNF, using its pinned release;
earlier versions need the explicit post-install action. Topgrade invokes the
helper's update command. App status and uninstall belong to the helper.
Removing the helper RPM leaves the app installed; uninstall the app first.
Nimbus retains old app receipts without using them to remove applications.
See [Security](#security) for the download exceptions.

## Installation workflow

See [the Fedora installation guide](INSTALLATION.md) for installer choices
and existing disk layouts.

1. Install a minimal Fedora 44 base from official media: networking, standard
   utilities and a normal password-protected user in `wheel`. Enable the root
   account with a password for local use; keep root SSH access disabled. Add
   guest agents only for the test VM. Nimbus installs the selected desktop
   and applications after first boot.
2. Use UEFI/GPT, Fedora GRUB, separate boot mounts and a LUKS2-encrypted Btrfs
   system volume. Retain the passphrase. Follow the subvolume table in the
   installation guide, including separate snapshot and data mounts. This is
   the recommended operator layout, not a partition scheme Nimbus enforces.
   Existing working layouts need not change.
3. Review the exact target disk before partitioning. Preserve Windows data and
   its EFI loader on dual-boot machines; do not format a shared EFI partition.
   Preserve irreplaceable data independently. Keep Fedora zram; hibernation
   remains deferred. FDE auto-unlock is planned as an optional post-install
   action; installation starts with passphrase unlock.
4. Sign in, check networking and sudo access, and inspect `lsblk -f` and
   `findmnt -t btrfs,ext4,vfat`. Then bootstrap the chosen machine.

Run the installer as the normal user:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash
~~~

On first installation, enter `desktop`, `laptop` or `vm` when asked. Reruns
reuse the saved machine. An explicit choice uses `bash -s -- --machine vm`
instead of `bash`. The prompt requires Nimbus 0.3.1 or newer; use the explicit
choice with 0.3.0.

Bootstrap obtains prerequisites, verifies the Nimbus RPM, obtains the checkout,
refreshes the user package cache and invokes init. It does not include
uncommitted local work. Use the separate
[candidate guide](../tools/vm/README.md) for an unpublished VM trial. Check
[TASKS.md](TASKS.md) before desktop installation; the documented base still
needs its clean-install trial.

The minimum compatible engine is declared in `nimbus.toml`. Engines from 0.5.0
check their configured RPM repository before fetching definitions; use the
combined upgrade command from the README. Older engines need the one-time
native RPM upgrade before using that workflow. Bootstrap validates an installed
engine but does not upgrade it automatically.

Reboot when requested and select Hyprland through UWSM in Noctalia Greeter.
The owner confirmed that the greeter remembers the selection. Run status,
doctor and postinstall where supported by the installed engine. Use installed
`--help` to check availability. Init shows its plan before asking for approval.
If a direct init reports
missing package metadata, run `dnf5 makecache` and retry. `init --plan` never
refreshes metadata.

## Everyday commands

| Command | Purpose |
| --- | --- |
| `nimbus` | Interactive dashboard when available; otherwise help |
| `nimbus init` | Select or create a machine and perform initial setup |
| `nimbus status` | Summarize drift and pending work |
| `nimbus sync` | Update repositories, reconcile the system, apply Chezmoi |
| `nimbus upgrade` | Update installed software through Topgrade |
| `nimbus sync --upgrade` | Update Nimbus, sync once, then run Topgrade |
| `nimbus postinstall` | Show task commands and help |
| `nimbus postinstall status` | Compact machine-specific setup checklist |
| `nimbus setup-notes` | Display all applicable guidance, read-only |
| `nimbus postinstall TASK` | Guide setup and verify completion |
| `nimbus doctor` | Report health problems and possible fixes; never repair |

Init reuses the trusted selector or asks for a tracked machine on first use.
There is no default machine. `--machine ID` skips the choice; `--yes` requires
an existing selector, `--machine ID` or explicit `--new ID`. Selecting a machine
does not approve installation; the plan and approval still follow. New-machine
setup can select a dotfiles repository with `--dotfiles URL` or omit the
handoff with `--no-dotfiles`. The normal flow is system prerequisites, system
setup, then Chezmoi initialization and apply. Retry preserves completed work.
Optional 1Password SSH integration is an explicit opt-in.

Ordinary sync follows this order:

1. Resolve selector/argument identity without decoding definitions. Validate the
   platform and request sudo credentials. Authentication does not approve a
   transaction.
2. Refresh the configured `nimbus-engine` RPM repository in the privileged DNF
   cache. DNF identifies upgrades using RPM ordering and configured exclusions.
   A failed refresh or query stops the run. If a newer RPM exists, stop before
   either Git fetch and direct the user to `sync --upgrade`.
3. Check both approved repositories for clean, attached, tracking branches
   without local-only commits. Fetch both and require fast-forward history.
   Update without stashing, resetting or overwriting local or ignored files.
4. Reload definitions and announce the selected greeter's read-only
   administrator verification. Inspect before previewing mutations, using the
   fixed native status command only for the invoking account. A missing
   authorization becomes a reviewed change; an unreadable state remains a
   problem. Recheck the same observations after approval. No-op reconciliation
   creates no mutation prompt or system snapshots.
5. Ask before applying Chezmoi once, including its scripts and tools. Refresh
   shared profile IDs while preserving the explicit 1Password SSH choice.
   `--yes` approves automated apply stages, never manual GUI confirmations.
6. Inspect remaining setup and effective configuration, display new or revised
   guidance, and produce one final report. This inspection does not apply again.

The Chezmoi stage uses the source already fetched; it does not run
`chezmoi update` and fetch a second revision. A machine without a dotfiles
selection skips that stage. Chezmoi retains ownership of user files.

A blocked repository check names the repository, path, changed files where
applicable and the required Git repair. It states that no system or user
configuration changes were applied. Later failures report completed stages;
a failed Chezmoi apply may leave partial user-configuration changes. Correct
the reported problem and retry; Nimbus does not promise rollback.

Sync may install dependencies and perform upgrades those installs require,
but does not generally upgrade unrelated software. `sync --prune` adds
eligible unmanaged removals to the visible plan; it never means deleting
everything absent from the definitions.

Reboot and logout notes in a plan describe actual planned system changes.
Unchanged or merely adopted resources do not repeat them. `postinstall` reports
requirements still outstanding from earlier changes.

When native Tailscale is selected, `postinstall tailscale-operator` offers
local operator permission for the invoking non-root user. Require an installed
package with a valid receipt and readable daemon preferences. Preview the
current and requested operator and `sudo -- /usr/bin/tailscale set
--operator=USER`; recheck after approval, then verify the requested operator
after execution. A failed or ineffective command never counts as complete.
Only the operator is retained from the local preference response; other
network and account preferences are not rendered, hashed or stored.
Sign-in, connection state and other settings remain unchanged. Unknown daemon
state offers no action. An already matching operator needs no command.
This is an explicit post-install action, not a sync resource: Tailscale owns
the preference and package deselection does not reset it. Revoke it through
`sudo tailscale set --operator=` or explicitly choose another operator.

When AccountsService is selected, the account-picture post-install task
registers the Chezmoi-supplied JPEG for the invoking non-root user. Require a
verified package receipt, readable valid source and a running service that
confirms the account identity. Inspection reads only the user name and icon
path through non-activating, non-authorizing D-Bus property requests. Compare
image content rather than paths, since AccountsService owns its stored copy.
Bind approval to the source and current icon hashes; re-inspect before the
fixed `SetIconFile` call through busctl with native authorization enabled.
Verify the saved bytes after execution; failed or ineffective calls do not
complete the task. Store no privileged resource receipt and never write the daemon's
files directly. Missing source blocks setup; unknown account state offers no
action. Package deselection leaves the account preference in place. See
[README](../README.md) for the dotfiles handoff, command and
recovery; no image-generation or wallpaper-sync lifecycle is introduced.
The native contract is AccountsService's
[SetIconFile API](https://gitlab.freedesktop.org/accountsservice/accountsservice/-/blob/23.13.9/data/org.freedesktop.Accounts.User.xml);
[Noctalia documents](https://docs.noctalia.dev/greeter/sync/) its independent
AccountsService avatar lookup. No additional system policy is needed.

Upgrade uses Chezmoi-owned Topgrade configuration. Updating that configuration
changes the chosen native update steps without editing Go. Extra Topgrade
arguments follow `--`. System and application updates run once, and failure
or cancellation of the system phase stops dependent work. Independent user
steps may continue after a failure, but the final result remains unsuccessful.

`sync --upgrade` performs the same fresh engine check first. When an update
exists, announce the engine update, dependencies and subsequent restart. DNF
owns the exact transaction confirmation; there is no duplicate Nimbus question.
`--yes` supplies its native assume-yes option. Restrict the requested
Nimbus package to `nimbus-engine`, require its effective enabled/signature/TLS
settings, retain native dependency
sources, and never enable testing or permit erasing to satisfy an update.
Verify the installed RPM and replacement executable version, release the engine
operation lock, and restart at the RPM-owned executable path. Preserve machine,
checkout, upgrade, approval and prune arguments. A second available update during
restart stops with retry advice instead of looping.

The updated process performs one configuration sync and one Topgrade run. There
is no second Git fetch or Chezmoi apply. A failed engine update or sync skips
later phases. The Topgrade callback uses a private temporary report to return
system changes and failures to the parent. The parent combines these with the
configuration result; native Topgrade output remains visible. Combined
`sync --upgrade --yes` passes Topgrade's native `--yes` and explicit approval
to the Nimbus system callback. It never forces Chezmoi conflicts or confirms
manual setup. Normal execution retains the separate Chezmoi decision.
Unresolved native failures retain unsuccessful exit status. Standalone
`upgrade` still delegates to Topgrade without fetching repositories.

Explicit system upgrades refresh all enabled repositories with unavailable
sources treated as errors. Cached planning queries and the transaction use the
same privileged cache. Package downloads remain enabled. The approved
transaction uses `--setopt=cacheonly=metadata`, so it cannot
silently refresh to different metadata during execution. Plan-only commands use
the existing unprivileged cache, disclose that limitation, and never authenticate
or refresh. Native solver changes and actual differences remain reported.
Before upgrade approval and snapshots, inspect the full cached DNF transaction,
including dependencies and obsoletes, and fresh update refs from every system
Flatpak remote when Flatpak updates are selected. An empty RPM upgrade list
alone does not establish a no-op. If both have no work, report current and skip
the native transactions and snapshot pair. Inspection failure stops execution;
unknown state never counts as current.

Topgrade must not call sync or recursively invoke `nimbus upgrade`. The
callback is `nimbus upgrade --system`. It updates RPMs and system Flatpaks and
handles repository duplicates created by package transactions. It requires
ready sources, version constraints and selected Snapper configuration;
otherwise run sync first. It does not
install missing selected apps, edit files or reconcile services. Other drift
remains for sync. Explicit checkout/machine overrides pass through the wrapper
to this callback. Upgrading software must not silently change machine
selections.

Routine execution groups matching-file receipt refreshes without hiding content
changes. Verbose plan inspection retains individual paths and metadata. Replans
show only changes to outstanding operations; resolved dependencies and unchanged
policy notes are not differences. The closing report groups successful receipt
refreshes and snapshot results, retaining individual failures and skipped work.
Pending/blocked setup and problems verifying native state have separate lists.

## Setup guidance, local state and reports

Nimbus owns the schema-1 `setup-notes.json` catalog at its checkout root and
reads it directly from the selected checkout. No Chezmoi installation, external
command or catalog handoff is required. Profile filters select applicable notes;
`requires_dotfiles` limits configuration guidance to machines declaring dotfiles.
IDs and revisions stay stable when catalog storage changes; revisions change
when required user action changes. The explicit command displays applicable
notes without changing display history or task completion.
Init shows applicable notes together; subsequent successful syncs show only
unseen revisions. Only successfully written output consumes a note revision.
Failed runs preserve unseen guidance for retry.

The independent local store uses `$XDG_STATE_HOME/nimbus`, falling back to
`~/.local/state/nimbus`. `setup-notes.json` records displayed revisions;
`postinstall.json` records manual confirmations and verified completion evidence.
Schema-1 records are scoped by selected machine and contain IDs, revisions,
timestamps and evidence sources. The 1Password completion record also holds a
digest of verified local files and the SSH selection, never their contents.
Enabling SSH requires a new confirmation of its additional GUI prerequisites.
Changes invalidate that evidence without contacting the vault during status.
Files are private, atomically replaced and protected against concurrent writers;
corrupt or unreadable state is reported and preserved. Help, status and previews
never create it. Missing state means unseen guidance and unconfirmed manual
steps; native checks can already recognize completed automatic setup. Native
state always takes precedence over stored completion. User state is never
managed by Chezmoi or committed, and is separate from `/var/lib/nimbus` receipts.

Init retains installation logging and prerequisite ordering. Selected 1Password
access is checked before secret-backed rendering; failure preserves selection
and completed installation work for retry. Public native output is streamed
through a pseudo-terminal when interactive logging requires it, retaining
resizing, cancellation and terminal restoration. The Mise component supplies
Fedora's `util-linux-script` for Chezmoi's native tool-log relay. Authentication
input and
secret-capable Chezmoi output stay outside transcripts. Quiet logged inspection
and repository fetches report elapsed time. No progress percentage is invented.
Direct init retains its compatible-definition validation before installation;
it does not silently upgrade itself or fetch repositories.

Configuration planning and replanning omit unrelated software-update solver
queries, explicitly identifying update information as uninspected in JSON.
Status still queries update availability; explicit system upgrades always
perform the source-constrained native preview. Install transaction source
policies remain independent, and before/after transaction checks remain fresh.
Single-task postinstall workflows inspect only the selected task; checklist
and final maintenance inspection retain all applicable tasks.

One closing report distinguishes completed changes, failed and skipped phases,
remaining guided tasks, new guidance and session activation notices. It includes
known Noctalia GUI overrides that disable managed lockscreen widgets without
modifying preferences. An unavailable check is reported, never treated as proof
of a matching effective desktop. File checks do not certify visual or hardware
behavior. Native Topgrade summaries are not rewritten as Nimbus output.

The postinstall checklist uses concise descriptions for verified tasks and puts
historical MOK verification's sudo recheck command on a separate indented line.
Task help explains verification limits. Native details remain in JSON and task
previews; pending, blocked and failed checks retain their full explanations.
Historical MOK previews describe the approved read-only sudo recheck, rather
than asking for enrollment again. Native JSON status remains unknown.

Nimbus text output uses shared bright terminal-palette colors for status labels,
commands, help, plans and reports. Success is green, pending work and notices
are yellow, failures are red, and actions and historical verification are cyan.
Headings, statuses and outcome totals are bold; secondary metadata is dimmed.
Selected entries and resource explanations receive the same emphasis. Labels
remain explicit;
color is never the only way to identify a result. Each output stream enables
styling only when it is a terminal, `TERM` is not `dumb`, and `NO_COLOR` is empty.
Redirected text, JSON and Nimbus installer logs receive no added ANSI styling.
Native tools retain their original terminal writers and their own output.
Complete text lines wrap to the current terminal width, with indented
continuations and bullets for setup notes. Redirected text and logs retain
their line structure. Partial prompts and progress writes are emitted
immediately without waiting for a newline.

Doctor removes duplicate resource prefixes and indents multiline observations;
its closing summary distinguishes passed, failed and unknown checks. Compact preview
output follows execution order: engine check/update, system sync and Chezmoi,
then Topgrade. Unchanged plans omit recurring snapshot/source policy prose;
help documents it, while relevant planned actions and warnings stay visible.

## Software selection

| Command | Purpose |
| --- | --- |
| `nimbus profiles list` | Show profiles and selection paths |
| `nimbus profiles add [ID...]` | Select profiles |
| `nimbus profiles remove [ID...]` | Remove selected profiles |
| `nimbus components list` | Show capabilities and selection paths |
| `nimbus components add [ID...]` | Select components directly |
| `nimbus components remove [ID...]` | Remove direct component selections |
| `nimbus packages installed [QUERY]` | Browse explicitly installed packages |
| `nimbus packages install [QUERY]` | Pick packages to select and install |
| `nimbus packages remove [QUERY]` | Pick eligible packages to deselect |

Omitting profile or component IDs opens a picker. Package queries filter a
picker. These commands edit the machine definition, show its diff and system
plan, then apply after approval. They leave Git changes uncommitted. A failed
apply leaves the approved definition in place and reports remaining drift.
A component required by a profile must be removed through that selection.
Required dependencies and protected packages cannot be excluded arbitrarily.

Selection commands and init keep their existing system reconciliation and
explicit handoff behavior; they do not automatically pull repositories.
Selection commands leave local Git changes for review and report Chezmoi
refresh advice. Commit and publish the changed selection before ordinary sync,
which refreshes shared profile IDs during its approved Chezmoi stage.

## Configuration and inspection

Status counts runnable RPM and Flatpak installations/source repairs under
`to_install`; other system checks and configuration changes count as `pending`.
Blocked operations and dependency waits retain their separate classification.
A permission-protected greeter authorization check never implies a missing
package or verified current authorization. Status remains unprivileged.

Chezmoi also works directly, independently of the sync workflow:

~~~sh
chezmoi diff
chezmoi apply
chezmoi update
~~~

| Nimbus command | Purpose |
| --- | --- |
| `nimbus validate` | Check all checkout definitions and machines |
| `nimbus managed` | List owned packages or packages sync would adopt |
| `nimbus unmanaged [--all]` | List unmanaged packages, optionally baseline |
| `nimbus why RESOURCE` | Explain why a resource is selected |
| `nimbus files accept /etc/PATH` | Capture an intentional owned-file edit |
| `nimbus version` | Report engine and supported schema versions |

`files accept` captures one eligible, readable, already-owned system file into
its existing checkout source. It shows the diff, writes no live system file,
does not change ownership or permissions, and leaves the Git change for review.
Accidental system drift is repaired in the forward direction with sync.

Configuration-loading commands support `--checkout` and `--machine` where
applicable. These overrides do not rewrite the selector. Validate accepts only
`--checkout` because it checks every machine. Help lists available flags.

## Small helpers and post-install

| Command | Purpose |
| --- | --- |
| `nimbus launch browser [URL] [--private]` | Open the selected browser |
| `nimbus launch webapp URL [--private]` | Open a site in browser app mode |

All browser launch commands prefer the default browser from `xdg-settings`.
If it is unset, missing or cannot handle the requested mode, try installed
browsers in this order: Brave (including Brave Origin), Chromium, Chrome,
Edge, Opera, Vivaldi, Helium, then Firefox/LibreWolf. Firefox and LibreWolf
support regular and private windows, not webapp mode. An unrecognized default
browser can still open regular windows. Private requests always pass the
selected browser's private flag; they never fall back to a regular launch.
Invalid or unreadable desktop entries and discovery errors are reported.

Launchers accept HTTP(S) URLs, preserve arguments without shell evaluation,
and do not install browsers or write their configuration. They need no plan
or extra confirmation. Chezmoi owns webapp desktop entries. Use native
`xdg-terminal-exec` for terminal desktop entries rather than another wrapper.

Both bare `postinstall` and `postinstall --help` show task commands without
inspection. Each named task has its own help and `--plan`; `postinstall status`
shows applicable tasks and session notices. JSON is read-only: status and task
previews are supported, execution is not. Reboot/logout remain notices rather
than commands. Inspection reports Pending, Verified, Blocked or Unable to check
with a specific reason.

Guided tasks show prerequisites, request explicit confirmation where required,
preview native actions, acquire the operation lock and recheck before execution.
They verify native results and record successful completion automatically. Exit
zero alone is insufficient. Existing verified tasks need no repeated native
action. Supported `--mark-done` tasks confirm manual prerequisites and verify
existing setup, recording completion only after all required checks pass. They
never install or apply configuration. Failed checks explain what remains and
point to the guided task. `--reset` clears the selected machine's manual and
completion evidence without undoing configuration.

1Password first offers SSH/Git integration when it is not selected, including
after completed CLI-only setup. The terminal choice defaults to no; `--yes`
cannot opt in. Without a terminal it retains the existing selection. An explicit
yes approves saving the choice through native `chezmoi init --prompt`, preserving
the machine, managed flag and ordered profile list. Nimbus rechecks the selection
and task ownership under its operation lock before saving, then verifies the
stored values. This step neither applies files nor runs scripts. A later failure
keeps the choice enabled for retry, with completion requiring fresh verification.
`--mark-done` never offers or changes this choice. Help, status and plan stay
read-only. Existing selected SSH integrations do not need another opt-in.

The task guides sign-in/unlock and desktop CLI integration, plus its SSH agent
when selected. It tells the user to skip manual SSH/Git file edits.
Manual readiness requires terminal confirmation; `--yes` cannot supply it.
The guided task checks CLI access without displaying account output. After GUI
confirmation, the specific native error "account is not signed in" offers a
previewed `op signin`, approved interactively or with `--yes`. Native terminal
input and stderr prompts remain available, with stdout discarded and no sign-in
transcript. Recheck authentication once after sign-in; cancellation, other errors
and an account that remains signed out stop completion. An authenticated session
skips sign-in. Shared init prerequisites report sign-in advice without starting
this postinstall recovery flow. Status and previews never authenticate.

The task then verifies selected Chezmoi SSH/public-selector/agent/Git targets;
matching files need no apply. Otherwise native Chezmoi status supplies a concise
create/update summary for those targets and their managed parent directories.
After approval, `chezmoi apply --parent-dirs --exclude=scripts` creates missing
directories with their managed permissions and applies the selected files.
Native file-conflict prompts remain enabled. Nimbus rechecks rendered content,
file/directory status and task selection before applying, then verifies files
and agent readiness. Guided `--diff` additionally streams the private Chezmoi
diff with `--no-pager`, including managed parents; no diff is shown by default.
It cannot combine with `--plan`, `--mark-done` or `--reset`, and may authenticate
as part of guided setup. Diff and rendered content remain outside logs.
Verify-only completion checks the file fingerprint before and after verification
and rechecks the selection. GUI acknowledgment persists after a failed check so
retry can address the remaining failure. The `1password` alias selects this task;
unknown task names receive task suggestions even when followed by task flags.
SSH remains explicitly opt-in. It never creates replacement keys or tests remote
authentication or account-side signing registration. Native inspection checks
required packages, CLI availability and the current local configuration;
status never opens the vault. It explicitly distinguishes previously confirmed
GUI readiness from current unlock state, which it does not inspect.

Copilot uses its helper's read-only status and standalone install command.
ProtonPlus lists native Steam runners before and after installation. These
native results override old completion evidence. WoWUp remains blocked until
its helper supplies standalone installation. Fingerprints and MOK retain native
checks; MOK enrollment still requires the firmware procedure. MOK inspection
distinguishes missing, empty, permission-denied and other unreadable certificate
states. Only an explicit task may offer approved read-only sudo verification of
the fixed public certificate and its enrollment; no enrollment or permission
changes are performed. Status/help/plan never authenticate. Stored verification
is displayed as "Previously verified" when a permission-protected certificate
prevents rechecking. Final reports put this historical result in notices, outside
pending setup and verification problems. JSON retains the native `unknown`
status and adds `previously_verified: true`; this is evidence of an earlier check,
not current verification. Missing, unenrolled or otherwise failed native checks
retain their normal status, and explicit verification clears the historical
presentation before checking again.
Enrollment checks use `--ignore-keyring --test-key` to bypass the kernel-keyring
shortcut, which does not establish MOK/firmware enrollment. For the supported
mokutil 0.7.2 behavior, an exact enrolled/trusted result with exit 1 is complete;
an exact not-enrolled result with exit 0 or a pending request with exit 1 remains
pending. Denylisted keys are blocked. Unexpected output or exit status remains
unknown. The certificate path must match, and process errors retain their typed
exit status through the native runner. This follows the native
[mokutil test-key implementation][mok-test-key].

[mok-test-key]: https://github.com/lcp/mokutil/blob/0.7.2/src/mokutil.c#L1448

When native Steam and ProtonPlus are selected and recorded, `proton-cachyos`
offers `protonplus install steam-system proton-cachyos latest` after approval.
Steam must have been started once to create its user directories. ProtonPlus
owns runner downloads, updates and removal; Nimbus neither queries releases
during inspection nor records runner completion from an exit code. This is a
post-install action, not an automatic download during init or sync.

When Noctalia is selected and recorded, `noctalia-lockscreen` offers an explicit
repair of GUI overrides hiding Chezmoi's managed schema-2 widget layout. This
is a narrow exception to native runtime-state ownership: Chezmoi retains the
layout; Nimbus may remove only `lockscreen_widgets` and its descendants after
preview and approval. Sync never removes GUI preferences. Help and previews
remain read-only, and status rechecks configuration rather than trusting saved
completion. Missing, invalid or nonmatching managed configuration blocks repair.

Require an unlocked local Noctalia session, closed panels/editor, and exactly
one invoking-user daemon with supported launch arguments. Preview the temporary
shell interruption. After locked reinspection, use a pidfd and verify executable,
UID and start time before SIGTERM; do not force-kill, stop Hyprland, log out, or
request sudo. A ten-second exit timeout leaves settings untouched. If the shell
exits later, the operator restarts it. Otherwise restart the shell even when
subsequent repair fails or cancellation arrives. No automatic locking occurs.

For Chezmoi's UWSM-managed `app-noctalia.service`, inspection verifies the
process cgroup against the user manager, active state, disabled auto-restart,
and the ten-second stop timeout with forced killing disabled. The service must
use a per-unit user drop-in with `TimeoutStopFailureMode=terminate`;
Fedora's inherited `abort` setting is
rejected even when `SendSIGKILL=no`. Preview shows
`systemctl --user stop` and the exact `uwsm app` restart. Recheck this evidence
after approval and verify the service is inactive before editing settings.
Other service supervisors block repair. A shell launched directly by Hyprland
retains the pidfd path. Service stop failure leaves settings unchanged.

Read and hash the reviewed configuration again before writing; a shutdown flush
or external edit aborts replacement. Reject symlinks, foreign ownership,
shared-writable ancestors, hard links and oversized input. Save an exact 0600
backup in the private Nimbus state directory before atomically replacing the
settings file. Remove parsed TOML expressions rather than matching text inside
strings. Preserve unrelated values, comments and formatting. Backups are local
and never synchronized; operators review/remove them explicitly. Do not restore
a whole backup automatically over newer preferences.

Noctalia 5.0.1's explicit reload rebuilds from in-memory overrides; its filesystem
watcher reloads external settings separately. Restart ensures it reads the
repaired file without a stale editor writing back. This behavior was checked in
upstream `src/config/config_service.cpp` at tag `v5.0.1`. After restart, check
shell readiness, unchanged managed configuration and native merged configuration.
Noctalia 5.1.0 can persist an equivalent screen-adjusted layout during startup.
For each widget, compare `cx / placement_width` and `cy / placement_height`
within an absolute tolerance of one millionth; placement extents must be positive
and all four values finite numbers. Missing pairs match only when both layouts
omit them. Widget IDs, order, types, box sizes, appearance and other settings
remain exact comparisons. Apply the same equivalence check to saved and effective
layouts during status inspection; matching persisted layouts need no repair.

After restart, accept either absent widget overrides or a saved equivalent
layout. Compare unrelated preferences as parsed TOML values, allowing native
reserialization without accepting changed preferences. All pre-write digest,
shutdown-flush, ownership and backup checks remain strict. A mismatching saved
or effective layout, changed managed file or changed unrelated preferences
prevents completion; never restore the backup automatically.
Record completion only on success; errors retain the backup and explain retry
or native restart. These checks do not certify visual placement or unlock/PAM
behavior. See the [README](../README.md#guided-setup) for commands and recovery.

When Noctalia is selected and recorded, `noctalia-plugins` inspects its native
full effective configuration and local IPC plugin listing. Enabled intent and
cached catalogs do not establish installation: verify matching runtime
manifests and readable entry scripts. Unknown config, unavailable IPC or
incompatible plugins or uninitialized source catalogs remain unknown, with no
offered mutation. Inspection
never fetches plugins and never retains or renders unrelated exported settings.

After approval, the task uses native source-update commands for sources with
enabled plugins missing runtime files. These may also update already-installed
plugins from those sources. Noctalia owns downloads and live registry/bar
refresh. Wait up to two minutes for the missing runtime exports, retrying
unreadable or incomplete verification results while the update settles.
Retries only inspect state; they never repeat source-update commands. At the
deadline, report the latest verification problem as failure, preserving native
partial results for retry. No second plugin catalog, managed runtime files or
privileged completion receipts are created. Run this action inside the desktop session
after Chezmoi apply; init and sync do not start a desktop or silently download
plugins.
Installation verification does not claim account readiness or widget behavior.

When Hyprland development headers are selected and recorded, the
`hyprland-plugins` task reads Chezmoi's strict schema-1 selection at
`~/.config/hypr/plugins.toml`. The supported selection is empty or
`enabled = ["scrolloverview"]`. This is user intent, not native installed
state. Missing or unknown input offers no mutation. Empty selection preserves
existing plugins and does not offer automatic removal.

For Hyprland 0.56, inspect the invoking user's `/var/cache/hyprpm/<user>` state,
header marker, build result and binary, installed/running ABI, and live plugin
list and configured option. Never run `hyprpm list` during inspection: it can
initialize privileged state. Require the active session and matching ABI;
an upgraded but still-running compositor requires a new login. Unknown cache
formats, foreign repository collisions and unreadable state stop setup.

After approval and the normal locked reinspection, execute only the planned
native HyprPM update/add/enable/reload stages and Hyprland config reload.
The supported ScrollOverview upstream is fixed in the engine; user input does
not supply arbitrary commands. Rebuild failed or missing binaries with native
forced update; this bypasses the up-to-date optimization, not ABI validation.
Disclose that update/reload can affect other registered plugins. Preserve
native trust and sudo authorization; never run HyprPM as root. Verify each stage
before continuing, including builds whose native command returned zero.
Failure retains HyprPM's partial state for retry. Completion requires build,
enablement, live loading and applied overview configuration; appearance still
needs user testing. No automatic setup during sync, custom plugin copies,
cache edits or privileged completion receipts. Native disable/remove owns removal.

FDE auto-unlock is a planned optional post-install action, before the dashboard.
It must inspect the existing encryption and boot setup, show the proposed
native enrollment and ask for approval. Preserve working passphrase access;
provide status and enrollment-removal instructions. Unsupported setups retain
manual unlock. Sync and upgrades must not enroll automatically. The native
method and boot-change policy must be settled before implementation. Enrollment,
booting, passphrase fallback and removal need real-hardware validation before
claiming support. UKI generation and broader boot-key management stay deferred.

## Preview, approval and results

Init, sync, upgrade, selection edits, files accept and post-install actions
support `--plan`. Read-only commands and launchers do not need it.

A preview performs no writes, downloads, privilege escalation or mutating
hooks. Interactive choices stay in memory. Missing cache data or unavailable
helper information is reported as unknown. It must not be filled in by doing
part of the installation during planning. `sync --plan` identifies the local
checkout revision and describes repository and Chezmoi stages without running
them; fetched definitions may produce a different plan during ordinary sync.

Normal managed-state changes show their plan and ask before applying it.
`--yes` explicitly skips that question where supported. Output format alone
must not approve a mutation. Ordinary sync includes the announced repository
updates before the system approval; declining later does not undo those pulls.
Sync and selection JSON mutation require `--yes`; their `--plan --json` forms
remain read-only. Public sync emits one aggregate JSON result, while
`sync --upgrade` rejects JSON because Topgrade owns its terminal output.
A fresh execution checks its inputs again; an earlier preview does not approve
a later changed plan.

Native tools may resolve a different transaction after metadata changes.
Nimbus shows available versions and effects, reports unresolved details, and
reports actual differences afterwards. Newly discovered destructive actions
require review. A delegated update preview can show commands without exact
future versions; do not execute custom hooks merely because a caller requested
Topgrade dry-run.

Only one managed-state mutation runs at a time. Nimbus elevates specific
operations, verifies their results through native state, and records only
successful work. Partial failure preserves accurate completed records, stops
dependent operations and reports what can be retried. Changing definitions
back does not promise package downgrades or reversal of every side effect.

Human output is the default. Structured Nimbus data uses a versioned JSON
format. Launchers and delegated terminal output need not support JSON.
Post-install JSON inspects status or a task preview only. Ordinary exit codes
are 0 for success, 1 for failure and 2 for invalid usage; delegated upgrades
preserve native failure
status. Authentication and secret-bearing output must not enter logs.

## Desktop and recovery

Use Hyprland through UWSM with Noctalia Greeter. Keep stable Hyprland `0.56.*`
and Noctalia `5.*` through native DNF policy; do not freeze unrelated libraries
into a shared version group. Retain Fedora defaults unless a demonstrated
workstation need justifies a change.

The GRUB target is a minimal dark theme, a visible five-second menu, Fedora as
default, and native Windows discovery only where Windows exists. Preserve
existing kernels and Fedora boot integration. This is not a bootloader rewrite.

TTY repair is the supported direction; no extra Hyprland recovery desktop is
installed. Existing session files are retired through their ownership receipts
on a reviewed sync. Changed or foreign files are preserved and reported.
TTY access requires a sufficiently booted system.
Boot failure uses Fedora rescue tools or installation media. Nimbus provides
safe retry and owned-resource repair, not automatic rollback or backups.

Use Ctrl+Alt+F3 to attempt TTY login, correct the relevant source and apply it.
The broken-dotfiles drill remains open. A same-disk snapshot does not protect
against disk loss. Keep independent copies of irreplaceable data.

### Snapper

`hyprland-noctalia` selects the `snapper` component. Nimbus manages the root
template at `/etc/snapper/config-templates/nimbus` and uses native Snapper to
create the `nimbus` configuration and reconcile its settings. Setup requires
a Btrfs root. Without pre-created storage, native Snapper creates it. An empty
`/.snapshots` mount from the installation guide can be reused after approval
if it is a Btrfs subvolume on the root filesystem, owned by root and not
writable by other users. Nimbus rechecks its identity and emptiness, writes
the marked configuration, preserves other Snapper registrations and verifies
the result through native Snapper. It does not unmount, delete or recreate
storage or edit `/etc/fstab`.

Adoption interrupted during configuration, registration or verification can be
retried only with the exact marked configuration and still-empty mounted
storage. Foreign root configurations, populated directories and unproven
storage need explicit migration. Nimbus does not delete them to make setup
pass. Native Snapper calls use `--no-dbus` so configuration changes are read
directly.

After approval, mutating sync takes a before/after root snapshot pair.
`upgrade --system` does the same, including when called by Topgrade. Combined
sync/upgrade makes a pair for each system phase. User-tool updates are outside
this boundary. Previews and unchanged sync create no snapshots. The first
setup run installs/configures Snapper without a before snapshot and says so.
A failed before snapshot stops changes. Failure during a protected operation
still attempts the after snapshot and cleanup, preserving the original error.
On interruption, Nimbus waits for the current native command to return, stops
further work and attempts the same finalization while holding the operation
lock. Forced termination or power loss cannot run this cleanup.

Retention targets six individual snapshots, roughly three pairs, with no
minimum-age grace period or separate important-snapshot allowance. Timeline
and boot snapshots are not enabled. Native number cleanup runs after each
protected operation; `snapper-cleanup.timer` also runs it periodically.
Pair preservation and active/default snapshots can exceed the target. This is
not a disk-space cap. Snapshots without the number cleanup tag are unaffected.
[Snapper cleanup rules](https://snapper.io/manpages/snapper-configs.html)
remain authoritative; adjust the template in this repository to change policy.

The root snapshot excludes separate filesystems and nested subvolumes, such as
home, boot, EFI and the guide's separate data mounts. It does not promise a
bootable rollback. A restore drill
remains required before documenting a supported restore procedure. Removing
the component stops Nimbus snapshot hooks and retires its template/timer
ownership; the native configuration and snapshots remain for manual review.

## Security

The owner's declared COPRs are approved sources; Nimbus does not re-audit
their package contents at installation. Keep native signature checks to verify
that downloaded packages belong to those sources. Owner approval and download
authentication are different checks. Native managers verify normal packages;
Nimbus checks the resulting state rather than auditing application
code. Prefer Fedora and accepted official sources, with other sources declared
explicitly. Keep HTTPS, pinned repository keys and native signature checks;
review key changes. Unsigned repository metadata is not a reason to disable
RPM verification. Bootstrap verifies Nimbus against the checked-in COPR key
and fingerprint. Later engine updates belong to DNF.

The accepted download exceptions are limited to official `github/app` RPMs
and `WowUp/WowUp.CF` AppImages. The COPR signature authenticates the helper,
not those downloads. Helpers check official HTTPS origin, GitHub SHA-256 and
application/version/architecture identity, rejecting missing or mismatched
information. This relies on GitHub's release channel, not an independent
publisher signature. The helper owns artifact verification and its native
installation prompt.
Nimbus previews the helper action without downloading or parsing artifacts.
Any Copilot unsigned-local-RPM
exception is confined to that artifact's transaction, never global.

Helpers own this verification and app lifecycle. Keep updates explicit,
preserve WoWUp's sandbox and user/game data, and disable its app self-updater
while retaining addon updates. Remove an owned app before removing a helper
needed for its removal. Source-build work is not a reason to extract upstream
private credentials. A bootstrap maker script runs as the user from a file
with source and digest shown; the printed digest alone does not authenticate it.

Nimbus runs as the normal user and elevates only specific native operations
and narrow owned-file/state writes. No root daemon or passwordless sudo.
Reject foreign ownership, path traversal and symlink escape before writes or
deletion. Preserve approved checkout origin checks. Keep the preview, locking,
verification and partial-failure rules above during simplification.

Keep Secure Boot enabled on hardware, SELinux enforcing and firewalld active.
Open only needed services and ports. Do not weaken these settings to fix an
application. NVIDIA signing-key enrollment stays an explicit native procedure;
doctor permits disabled Secure Boot only for the selected `vm` machine when
native inspection confirms a virtual machine. An unreadable boot or required
virtualization check stays unknown. This exception is not hardware acceptance.
Nimbus does not partition,
encrypt or re-enroll disks. Boot partitions remain outside encrypted root.
Docker group membership is root-equivalent and remains an explicit selection.

Secrets and SSH configuration belong to 1Password and Chezmoi. Never place
secret values in definitions, command arguments, receipts or logs. Installation
logs are private and exclude authentication input, Chezmoi output and other
secret-capable output. Nimbus is not a boundary against an attacker with root.

## Architecture and tests

The CLI follows this flow; the future TUI must reuse the same operations:

~~~text
load definitions -> inspect -> plan -> approve -> recheck -> apply -> report
~~~

Public sync refreshes and checks the installed engine before reading
definitions or fetching repositories, then uses this system flow and offers
Chezmoi apply. Combined upgrade
restarts after an engine replacement, syncs once, and finishes with Topgrade.

`internal/definitions` resolves desired state, `inspect` reads native state,
`plan` compares them, `apply` executes native operations, and `state` records
verified results. `cli` connects the steps; `postinstall`, `launch` and
`snapper` hold their specific helpers. `internal/` makes these packages private
to Nimbus. Files inside each package group code by topic, without extra layers.
`cmd/nimbus` is the entry point. Native calls use argument vectors without shell
evaluation. Hidden privileged commands only write approved system files/state.

`native` owns command execution and filesystem access; `inspect` uses it only
for read-only queries. Verification requests the state it needs and reads it
again after mutation. It never substitutes cached or empty data for an unknown
result. Shared test fakes live in `native/nativetest` and never call the host.

Keep strict schemas, ownership checks and existing state-reader compatibility.
Preserve definition-digest behavior unless explicitly migrating it; docs and
code are outside that digest. The current state marker is schema 3, with
schema 2 receipts/baselines; ambiguous legacy identities block removal.
Tests in the owning packages specify these formats. Preserve terminal handoff
and cancellation when simplifying init or delegated commands.

Keep Go unit tests beside their packages. Tests of bootstrap, release and
staging tools live in `tests/integration/` and run with `go test ./...`.
Test preview/cancellation, ownership, input validation, partial failure, retry,
convergence, compatibility and version
constraints. Share repeated fixtures and test helpers at their owning boundary.
Do not duplicate application feature tests or keep tests for removed scope.
An interface can still justify itself through a native/test boundary with one
production implementation.

Use existing dependencies and the standard library. Apply Ponytail to scope
and abstractions, and read Modern Go Guidelines for `go.mod` before Go changes.
Run `just check` and `just validate`. Tests use temporary state and fake native
tools; destructive integration checks use disposable VMs. Native solver/VM
checks are opt-in and must be reported separately from the ordinary test gate.

## Repository tools

| Files | Purpose |
| --- | --- |
| `install.sh`, `bootstrap` | Get the checkout and engine, then run init |
| `tools/install/` | Interactive terminal handoff and cancellation tests |
| `tools/release/` | Tagging and vendored source release preparation |
| `tools/vm/` | Stage unpublished candidates in disposable VMs |
| `tools/package-query/` | Query Fedora package sources in a container |
| `tools/dnf-constraints/` | Test version-family rules against native DNF |
| `tools/dnf-sources/` | Test declared sources with signed fixture RPMs |
| `tools/grub/` | Generate and check the small GRUB theme images |
| `licenses/` | Supplemental third-party notices included in releases |

Only the installation handoff participates in normal bootstrap; the other
`tools/` groups support development and delivery. Bash connects native commands
before Nimbus is installed. Python provides terminal/process control and small
build/test utilities without another compiled bootstrap dependency. Test files
exercise these helpers; they are not extra installation steps.

Keep required third-party notices. Bootstrap's logging must also work before
Nimbus is installed. Keep terminal supervision, native signature verification,
prompts and cancellation.
Do not replace them merely to reduce the number of files or languages.

## Remaining delivery work

The repository-update workflow and installer effect disclosures are published
in engine 0.4.3 and its Fedora 44 COPR RPM. Current definitions require that
engine version; upgrade older engines before syncing these definitions.
The Noctalia plugin repair task first appeared in engine 0.4.4; use 0.4.5 for
verification retries during background updates. The laptop trial confirmed
installation; a fresh repair with the retry fix remains to be tested.
The hidden `sync --no-upgrade` alias remains compatible;
ordinary sync omits general software updates. Installed tests must cover
repository updates, Chezmoi apply and the fresh-engine upgrade handoff.

WoWUp's helper still needs standalone install/update commands and a published
package source. Copilot helper publication and native app behavior also need
verification. Nimbus no longer has custom app providers or Cargo/user-tool
installation lists; Mise, Zed and Zeron use native binary bootstraps.

Bare `nimbus` prints help until the dashboard is built. GRUB assets are present
but inactive; FDE auto-unlock is not implemented. These boot features come
before shared-operation extraction and the TUI. Installed TTY repair, legacy
session retirement, clean install, upgrade and hardware trials remain open.
[TASKS.md](TASKS.md) records evidence.

## Out of scope

No custom snapshot/boot-archive manager, automatic restoration, backup service,
Windows VM lifecycle, Home Assistant integration, UKI build pipeline or broad
boot-key management is required for the desktop milestone. The installation
guide's partition layout is operator-owned. Nimbus registers suitable existing
snapshot storage or lets native Snapper create it; it does not partition
disks or maintain a boot-archive scheme.

Fedora installation, partitioning, initial encryption and Fedora major-version
upgrades remain native operator workflows. Nimbus reports current compatibility
and repairs its own drift afterwards; it does not manage those transactions.
There is no background reconciliation daemon, generic provider/plugin system,
secret manager, fleet manager or cross-distribution support commitment.

## Local agent proxy integration

`postinstall agent-proxy` is selected with GitHub Copilot. Help, plan and status
read only local definitions and registration evidence. Execution requires
approval, a running Copilot desktop, Mise-selected Node 24 or newer, Herdr and
the desired authenticated CLIs. Authentication remains a separate native step.
The task never starts sign-in or asks for privileged access.

Chezmoi owns the loopback-only authenticated proxy configuration. The upstream
current-user installer owns application releases, credentials, services and
release backups. Nimbus builds pinned source after SHA-256 verification, applies
the embedded compatibility patch, and invokes that installer. The patch fixes
unit path quoting and completed-request disconnect detection, updates vulnerable
locked dependencies, prefers stable Mise shims in generated service PATH, and
preserves the trial's Herdr PTY access. The proxy keeps
private devices; Herdr needs login-session devices to create terminal panes.
Neither service gains privileges. Setup reports restart requirements and refuses
to replace unrecognized service files or interrupt active proxy requests.

Nimbus owns `agent-proxy.json` and its operation lock under the private XDG state
directory. These are never synchronized. They record machine identity, provider
and model IDs, catalog results and pending creation intents, without credentials.
The adapter is embedded in the engine, extracted privately for an operation and
removed afterwards. No mutable helper is installed in the user's PATH.

Copilot's own provider API stores the API key in its credential store. Model
mapping changes go through the proxy admin API, not either runtime database.
Successful complete discoveries for Codex, Claude, Grok and Antigravity add and
update owned entries and remove stale owned entries. A failed or unrecognized
list preserves that provider's entries. Manual records and identity collisions
are never adopted implicitly. Creation intents allow interrupted requests to be
reconciled on retry; deletion retains ownership until both endpoints verify it.
The explicitly approved trial handoff imports private evidence only after native
IDs and provider settings match.

Successful setup opts the machine into a model refresh after Topgrade completes
in `sync --upgrade`. Ordinary sync, standalone `upgrade`, and `upgrade --system`
do not run it. A closed Copilot app defers refresh before any CLI discovery or
model mutation. Refresh does not install software, apply configuration, restart
services or sign in. Results and per-provider deferrals join the final report;
an earlier upgrade failure skips refresh. Reset disables the opt-in and retains
all services, credentials, configuration and model ownership for later reuse.
Local status verifies installation and registration files, not live account or
service readiness. Antigravity's current adapter has no external tool bridge.

Repeat postinstall runs check the selected proxy config with secret-free Chezmoi
status inspection. A current registration, expected release and matching config
select the refresh-only path. Changed configuration or missing setup selects the
setup preview; an unreadable check stops before approval. Detailed prerequisites
and recovery stay in help. Routine results show provider counts and specific
skip reasons. Known missing authentication or CLIs are skips, not a partial
setup failure. Genuine discovery errors remain visible and preserve their
inventories.

## UWSM application launch

Chezmoi owns session environment, shortcuts, Noctalia's launcher command and
named Noctalia/udiskie user-service startup through UWSM. Nimbus supplies the
existing packaged UWSM session entry; no additional postinstall is needed.
`launch browser` and `launch webapp` use `uwsm-app --` when invoked in a local
Wayland environment and UWSM reports an active compositor. Other sessions keep
direct browser launch. A missing wrapper in an active UWSM session fails clearly.
Browser discovery, URL validation, native arguments and user activation remain
unchanged; URLs never pass through a shell. No environment or service mutation
occurs during browser discovery.

## Concise maintenance output and diagnostics

Default previews retain proposed mutations, package names and sources, file
content differences, removals, blocked reasons and activation notices. They omit
stable policy prose and group matching-file receipt work. `--verbose` restores
full resource and policy details and follows an engine restart. Native progress
and conflict prompts are never filtered. Final result statuses carry explicit
presentation semantics; the existing text formatter remains for legacy help
and prose. JSON retains structured operation detail and historical verification.
The additive `current` step status identifies unchanged repositories;
`historical_verification` contains earlier administrator-only verification.

Concise final reports group current repositories, omit repeated successful
DNF/Flatpak rows when the combined software phase succeeded, and group snapshot
retention. Failures, skipped phases, actual differences and pending tasks remain
visible. Historical administrator-only MOK rechecks remain in explicit task
status and verbose reports, without being described as fresh verification.
The lockscreen task is the single source of effective-widget inspection; the
final report does not parse a second runtime-settings path.

Execution creates schema-1, mode-0600 metadata records in the private local
`nimbus/runs` directory. Records contain only engine, machine, available commit,
command kind, timestamps, phase durations, result counts and activation flags.
Native output, error bodies, arbitrary arguments, environment and configuration
contents are excluded. Init's existing private engine log also contains its
closing Nimbus report; secret-capable child output still bypasses that log.
Records are limited to 64 KiB; retention preserves active processes and the
latest 20 completed or abandoned records. Invalid or foreign files are not
retention targets. Record failures are reported; they never establish success.
Help, status and previews create no run records. A hard termination can leave a
running record and does not establish the actual result. Engine replacement
creates separate records for the old and restarted process, identifying their
respective versions; a delegated Topgrade run can also have a separate system
callback record. Records are diagnostics, never completion evidence.

Init retains approval of its provisional greeter check when packages are not
yet installed. Resolving that announced conditional operation must not introduce
unrelated changes: reject such drift and require a fresh preview. Neither init
nor sync may treat a saved greeter receipt as a replacement for native inspection.
