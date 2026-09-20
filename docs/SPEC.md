# Nimbus specification

The accepted product scope. Implementation order lives in
[ROADMAP.md](ROADMAP.md); open work and decisions in [TASKS.md](TASKS.md);
usage in the [wiki](https://github.com/Furyfree/nimbus/wiki).

## Ownership

| Owner | Responsibility |
| --- | --- |
| Nimbus | Workstation setup, system drift, small helpers, post-install tasks |
| Chezmoi | User configuration, its drift, Mise and Topgrade configuration |
| Mise | Installing and updating its declared user tools |
| COPR repository | Packaging Nimbus and the application helpers |
| Topgrade | Coordinating the configured update steps |
| Native tools | Package transactions, services, application lifecycles |

Nimbus bootstraps required tools, performs the first Chezmoi handoff, and
coordinates later repository updates and Chezmoi apply during sync. Mise, Zed
and Zeron use their official user installers; their applications own updates,
services and state. Chezmoi owns their configuration and never installs system
packages or escalates privileges. COPR installer helpers own application
download, verification, installation and removal; Nimbus installs their RPMs
and offers small setup actions without tracking downloaded artifacts.

An installer's optional `effects` list contains nonempty, single-line
descriptions of additional native changes (for example Zeron's generated user
service and `loginctl enable-linger`). Missing-binary operations disclose
these in the approval preview and JSON plan at medium risk. They are
descriptions, never executed commands. Application sign-in stays manual.
Removing a helper RPM leaves the app installed; uninstall the app first.
Deselection does not remove native apps.

## System files

Nimbus stores the root-owned configuration it manages: GRUB inputs and theme
assets, systemd units and drop-ins, system-wide user-unit policy, service
configuration, udev rules and justified settings. Sources live under
`system/`; components declare targets and activation. Prefer native drop-ins
over replacing package-owned files. `/boot` assets need an explicit
supported install/removal path and are not generic copy destinations.

User configuration below `$HOME`, including `~/.config/user-dirs.dirs`,
belongs to Chezmoi. Requiring sudo does not authorize writing a user's
dotfiles. The selector, checkout, diagnostics, note history and setup
evidence are operational state, not user configuration.

The NVIDIA component masks RPM Fusion's generated `nvidia-settings -l`
autostart with an empty root-owned unit under `/etc/systemd/user` for NVIDIA
machines only, effective at the next login. The driver and manual settings
remain; existing per-user masks are separate.

The Hyprland session component owns `/etc/noctalia/greeter.toml` and exposes
only that file to greetd through `BindReadOnlyPaths`; systemd owns the mount
lifecycle and no display-manager restart is automatic. New appearance or
mount definitions require a new greetd namespace after reboot.

For research, start with Fedora and upstream, and compare Omarchy or
CachyOS-Settings for ideas. Check source, revision, compatibility, benefit
and how to undo; record accepted choices beside the component. Never import a
configuration bundle, user files or performance tweaks wholesale.

## Definitions and drift

| Location | Contents |
| --- | --- |
| `nimbus.toml` | Compatibility, repositories and DNF settings |
| `machines/ID.toml` | Shell, profiles, components, packages and constraints |
| `profiles/` | User-facing bundles of packages and components |
| `components/` | Capabilities, dependencies and owned system resources |
| `system/root/etc/` | Sources for generic managed files below `/etc` |
| `~/.config/nimbus/config.toml` | Checkout, machine, origin and channel |
| `/var/lib/nimbus` | Applied-state records and package baseline |
| `$XDG_STATE_HOME/nimbus/` | Private note history and setup evidence |

The selector holds no desired state: only which checkout, machine, origin and
engine channel an installation follows. Definitions are strict, versioned
TOML; invalid references, conflicts and dependency cycles fail validation.
Hardware detection may propose selections during init, never silently change
an existing machine.

Switching channels is an installer action:
`install.sh --channel stable|develop` verifies the target channel's reviewed
COPR key, switches the checkout branch, rewrites the `nimbus-engine`
repository, runs native `distro-sync`, records the channel and validates the
checkout. It refuses a dirty checkout, an unrecognized repository file and a
target engine that cannot read the applied-state schema.

The selector schema is 2 with a required `channel`. Schema 1 is read as
stable and recorded as schema 2 by the next init or channel switch. A rerun
of `nimbus init` keeps the recorded channel unless `--channel` names one, and
its preview and closing line name the channel it writes. The first sync or
upgrade of a channel-aware engine against a schema 1 selector prints a
one-time notice about the tracks; read-only commands never do. `stable`
follows `main` and `furyfree/nimbus`; `develop` follows the `develop` branch
and `furyfree/nimbus-develop`. Each track pins its own COPR key under
`system/keys/`; both use the `nimbus-engine` repository ID. The
`develop-version` file names the next stable release; develop builds use
`<develop-version>~dev.<numeric commit timestamp>`, and a stable tag must
number at or above it, so develop machines return to stable by normal
upgrade. Cutting a release bumps `develop-version` on `develop`.

The optional machine field `shell = "bash"` or `shell = "zsh"` selects the
invoking non-root user's login shell, with `machine:shell` provenance. The
plan shows the old and new shell and the `usermod` command after package
setup. Changes require approval, an executable shell in `/etc/shells`, and
the same observed UID and shell immediately before execution; re-read the
account afterwards. Failed commands or verification get no receipt. Drift is
repaired by the next approved sync. Logout applies a change. Removing the
field retires ownership and keeps the current shell.

Desired state comes from definitions, observed state from native inspection,
and applied state from verified operations. Unknown inspection is not
evidence of absence. Repeating sync converges without reapplying unchanged
resources. Nimbus never commits or pushes definitions.

Selecting an installed package may adopt it; deselection may remove it when
nothing else requires it. Unmanaged software is preserved unless explicitly
included through `--prune`, which is never "delete everything absent".
Deselection generally does not reset native application state.

Selected RPMs are restricted to their declared repository family during
install and upgrade. DNF5 `do` applies `--from-repo` per package group in one
transaction; dependencies keep native sources. Only enabled concrete
repository IDs are passed, never disabled testing sources. Missing candidates
or unavailable inspection block execution; there is no fallback. An installed
RPM with known provenance outside its declared family needs a reviewed source
correction (`distro-sync --from-repo`, or `reinstall` when the version
matches). Show downgrades, replacements and dependencies before approval, and
record old/new source evidence only after success. System upgrades require
corrections to be completed by sync first.

Bare package names mean Fedora. ble.sh comes from the owner's COPR; the
Hyprland profile uses the verified Noctalia LibrePods fork; Copilot and WoWUp
use COPR installer helpers. Updating a helper RPM does not prove the
downloaded app is installed; Copilot helper 0.3.0 and newer queues the app
install after DNF.
The TOML files are the software inventory, not a duplicated Markdown list.
Chezmoi owns user-tool and plugin inventories.

## Installation

Install Fedora 44 per the
[installation guide](https://github.com/Furyfree/nimbus/wiki/Installation):
minimal official media, UEFI/GPT, Fedora GRUB, separate boot mounts, LUKS2
Btrfs root following the documented subvolume table, a password-protected
user in `wheel`, root enabled locally with SSH access disabled, guest agents
for VMs only. Review the exact target disk before partitioning; preserve
Windows data and its EFI loader, and never format a shared EFI partition.

Then bootstrap the chosen machine as the normal user:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash
~~~

The first run asks for `desktop`, `laptop` or `vm`; reruns reuse the saved
machine, and `bash -s -- --machine vm` chooses explicitly. Bootstrap obtains
prerequisites, verifies the Nimbus RPM, obtains the checkout, refreshes the
package cache and runs init. It includes no uncommitted local work and, when
rerun, upgrades an installed engine from its configured channel before
validating. The minimum compatible
engine is declared in `nimbus.toml`. Init shows its plan before approval; a
direct init that reports missing package metadata suggests `dnf5 makecache`.
`init --plan` never refreshes metadata. Reboot when requested and select
Hyprland through UWSM in Noctalia Greeter.

## Everyday commands

| Command | Purpose |
| --- | --- |
| `nimbus` | Interactive dashboard when available; otherwise help |
| `nimbus init` | Select or create a machine and perform initial setup |
| `nimbus status` | Summarize drift and pending work |
| `nimbus sync` | Update repositories, reconcile the system, apply Chezmoi |
| `nimbus upgrade` | Update installed software through Topgrade |
| `nimbus sync --upgrade` | Update Nimbus, sync once, then run Topgrade |
| `nimbus channel` | Show the engine channel and checkout or repository drift |
| `nimbus postinstall` | Show task commands and help |
| `nimbus postinstall status` | Compact machine-specific setup checklist |
| `nimbus setup-notes` | Display all applicable guidance and mark it seen |
| `nimbus postinstall TASK` | Guide setup and verify completion |
| `nimbus doctor` | Report health problems and possible fixes; never repair |

Init reuses the trusted selector or asks for a tracked machine on first use;
there is no default machine. `--machine ID` skips the choice; `--yes`
requires an existing selector, `--machine ID` or explicit `--new ID`.
Selecting a machine does not approve installation. New-machine setup can
select a dotfiles repository with `--dotfiles URL` or skip the handoff with
`--no-dotfiles`. Retry preserves completed work. 1Password SSH integration is
an explicit opt-in.

Ordinary sync:

1. Resolve identity without decoding definitions, validate the platform and
   request sudo. Authentication does not approve a transaction.
2. Refresh the `nimbus-engine` RPM repository in the privileged DNF cache.
   Failed refresh or query stops the run. If a newer RPM exists, stop before
   either Git fetch and direct the user to `sync --upgrade`.
3. Check both repositories for clean, attached, tracking branches without
   local-only commits, fetch both and require fast-forward history. Never
   stash, reset or overwrite local or ignored files.
4. Reload definitions and announce the selected greeter's read-only
   administrator check. Inspect before previewing mutations, and recheck the
   same observations after approval. No-op reconciliation creates no mutation
   prompt or snapshots.
5. Ask before applying Chezmoi once, including its scripts and tools.
   `--yes` approves automated apply stages, never manual GUI confirmations.
6. Inspect remaining setup and effective configuration, summarize new or
   revised guidance with `nimbus setup-notes`, and produce one final report
   without applying again.

The Chezmoi stage uses the already fetched source; it never runs
`chezmoi update`. A machine without dotfiles skips the stage. A blocked
repository check names the repository, path, changed files and the Git repair,
and states that nothing was applied. Later failures report completed stages;
a failed Chezmoi apply may leave partial user configuration. Nimbus does not
promise rollback. Sync may install dependencies those installs require but
does not generally upgrade unrelated software. Reboot and logout notes
describe actual planned changes, not adopted resources.

`sync --upgrade` performs the same fresh engine check first. DNF owns the
transaction confirmation; `--yes` supplies its native assume-yes. The
requested package is restricted to `nimbus-engine` with its effective
enabled/signature/TLS settings; native dependency sources are retained, and
testing sources or erasing are never enabled. The installed RPM and
replacement executable version are verified before restart at the RPM-owned
path with the machine, checkout, upgrade, approval and prune arguments
preserved. A second update during restart stops with retry advice instead of
looping. The updated process performs one sync and one Topgrade run; there is
no second fetch or apply. Failed phases skip later ones and retain
unsuccessful exit status. Standalone `upgrade` delegates to Topgrade without
fetching repositories.

Explicit system upgrades refresh all enabled repositories with unavailable
sources treated as errors; planning and the transaction use the same
privileged cache, and the approved transaction uses
`--setopt=cacheonly=metadata`. Plan-only commands use the unprivileged cache
and disclose that. Before approval and snapshots, inspect the full cached DNF
transaction and fresh update refs from every selected Flatpak remote; an
empty RPM list alone is not a no-op. When both are current, report current and
skip the transactions and snapshot pair; inspection failure stops execution.

Topgrade must not call sync or recursively invoke `nimbus upgrade`. The
callback is `nimbus upgrade --system`, which updates RPMs and system Flatpaks
and handles duplicate repositories. It requires ready sources, version
constraints and selected Snapper configuration; otherwise run sync first. It
does not install missing selected apps, edit files or reconcile services.
Explicit checkout/machine overrides pass through to the callback. Upgrading
software never silently changes machine selections.

## Preview, approval and results

Init, sync, upgrade, selection edits, `files accept` and post-install actions
support `--plan`; read-only commands and launchers do not need it. A preview
performs no writes, downloads, privilege escalation or mutating hooks, and
never fills missing information by doing part of the installation. It
identifies the local checkout revision, describes repository and Chezmoi
stages without running them, and states that fetched definitions may produce
a different plan during sync.

Normal managed-state changes show their plan and ask before applying.
`--yes` skips that question only where supported; output format never
approves mutation. Ordinary sync includes repository pulls before the system
approval; declining later does not undo them. Sync and selection JSON
mutation require `--yes`; their `--plan --json` forms stay read-only. Public
sync emits one aggregate JSON result; `sync --upgrade` rejects JSON because
Topgrade owns its terminal output. An earlier preview never approves a later
changed plan.

Native tools may resolve a different transaction after metadata changes.
Nimbus shows available versions and effects, reports unresolved details, and
reports actual differences afterwards; newly discovered destructive actions
require review. Only one managed-state mutation runs at a time. Elevation is
per operation, results are verified through native state, and only successful
work is recorded. Partial failure preserves accurate completed records, stops
dependent operations and reports what can be retried. Changing definitions
back does not promise package downgrades or reversal of every side effect.

Human output is the default; structured data is versioned JSON. Ordinary exit
codes are 0 success, 1 failure and 2 invalid usage; delegated upgrades keep
native failure status. Authentication and secret-bearing output never enter
logs.

## Setup guidance and local state

Nimbus owns the schema-1 `setup-notes.json` catalog at the checkout root and
reads it directly; no Chezmoi reader or handoff is involved. Profile filters
select applicable notes and `requires_dotfiles` limits configuration guidance.
IDs and revisions stay stable; revisions change when required user action
changes. `nimbus setup-notes` displays every applicable note and consumes the
displayed revisions; JSON output is a machine view and consumes nothing.
Init and sync count unseen revisions and point to the command; only
successfully written output consumes a revision.

The local store uses `$XDG_STATE_HOME/nimbus` (fallback
`~/.local/state/nimbus`). `setup-notes.json` records displayed revisions;
`postinstall.json` records confirmations and verified completion. Schema-1
records are scoped by machine and contain IDs, revisions, timestamps and
evidence sources. Files are private, atomically replaced and protected
against concurrent writers; corrupt state is reported and preserved. Help,
status and previews never create it. Missing state means unseen guidance and
unconfirmed manual steps; native checks already recognize completed automatic
setup and always take precedence over stored completion. User state is never
managed by Chezmoi, committed, or mixed with `/var/lib/nimbus` receipts.

Init retains installation logging and prerequisite ordering; selected
1Password access is checked before secret-backed rendering, and failure
preserves selection and completed work for retry. Public native output is
streamed through a pseudo-terminal in the invoking session when logging
requires it, retaining progress, resizing and cancellation while the sudo
credential cache covers the whole run. Authentication input and
secret-capable output stay outside transcripts; a failed Chezmoi command
names the private hook-log directory beside the terminal diagnostic. No
progress percentage is invented. One closing report distinguishes completed
changes, failed and skipped phases and verification problems. Init closes
with the log directory,
the pending prerequisites for its requested reboot, the reboot or logout
instruction and the after-reboot pointer to `nimbus setup-notes`; sync keeps
the plain lines. Session-dependent checks stay unknown until the desktop
session runs and count as remaining setup instead of verification problems.
An unavailable check is reported, never treated as proof of matching state.

Nimbus output uses one meaning per color, all bright and bold:

- green: success that needs nothing further (succeeded, verified,
  installed, updated, adopt, totals)
- yellow: attention needed, nothing broken yet (pending, unknown, unable
  to check, warning, notice, reboot or logout required, Remaining setup)
- red: stopped or wrong, act before continuing (failed, error, blocked,
  remove, Verification problems)
- cyan: something that runs (`$` commands, `->` operations, Proceed?)
- white: structure and instructions (headings, labels, the finish banner)
- dim: secondary context only (unchanged, paths, durations, previously
  verified, decision reasons)

Color never carries meaning alone. Styling applies only to terminals with
`TERM` not `dumb` and `NO_COLOR` empty; redirected text, JSON and installer
logs stay plain. Native tools keep their own writers and output. Text wraps
to the terminal width with indented continuations; partial writes are
emitted immediately.

Run records are schema-1, mode-0600 files under `$XDG_STATE_HOME/nimbus/runs`
containing only engine, machine, available commit, command kind, timestamps,
phase durations, result counts and activation flags. Native output, errors,
arguments and configuration contents are excluded. Records are limited to
64 KiB with the latest 20 completed or abandoned retained; active runs are
preserved. Record failures are reported and never establish success. Help,
status and previews create none. A hard termination can leave a running
record; it is not evidence of success. Records are diagnostics, never
completion evidence.

## Post-install tasks

Bare `postinstall` and `postinstall --help` list task commands without
inspection. Each named task has its own help and `--plan`; `postinstall
status` shows applicable tasks and session notices. JSON supports status and
previews, not execution. Reboot and logout are notices, not commands.
Inspection reports Pending, Verified, Blocked or Unable to check with a
reason.

Guided tasks show prerequisites, request confirmation where required, preview
native actions, take the operation lock and recheck before execution. They
verify native results and record completion automatically; exit zero alone is
insufficient, and verified tasks need no repeated action. `--mark-done`
confirms a manual prerequisite and verifies existing setup without installing
or applying configuration. `--reset` clears the machine's manual and
completion evidence without undoing configuration. Failed checks explain what
remains and point to the task. Task help documents verification limits; native
details stay in JSON and previews.

1Password first offers SSH/Git integration when not selected, including after
CLI-only setup. The terminal choice defaults to no and `--yes` cannot opt in;
without a terminal the existing selection is kept. An explicit yes saves the
choice through native `chezmoi init --prompt` after rechecking selection and
ownership under the operation lock, then verifies stored values. It neither
applies files nor runs scripts. Later failures keep the choice for retry.
Guided setup covers sign-in/unlock and the desktop CLI integration plus the
SSH agent when selected; manual readiness requires terminal confirmation.
A confirmed GUI with an "account is not signed in" error offers a previewed
`op signin`; stdout is discarded and no transcript is kept. Status and
previews never authenticate. Selected Chezmoi SSH/public-selector/agent/Git
targets are verified; matching files need no apply. Otherwise a concise
create/update summary is approved and
`chezmoi apply --parent-dirs --exclude=scripts` runs with native conflict
prompts. `--diff` streams the private diff without a pager, may authenticate,
and cannot combine with `--plan`, `--mark-done` or `--reset`. SSH never
creates replacement keys or tests remote authentication.

`tailscale-operator` offers guided sign-in and local operator permission for
the invoking non-root user. It requires an installed package, a valid receipt
and readable native state; only operator and recognized backend state are
retained. For `NeedsLogin` it previews
`sudo -- /usr/bin/tailscale up --operator=USER`, which starts native browser
sign-in; approval is required and authentication cannot be automated. Never
use `tailscale login` (its profile switch can clear the operator) or
`--reset`. For signed-in states only the current and requested operator and
`sudo -- /usr/bin/tailscale set --operator=USER` are previewed; stopped
connections stay stopped. `NeedsMachineAuth` blocks on admin-console
approval. After initial sign-in both operator and running state are verified;
operator-only changes do not require a stopped connection to start. Failed or
ineffective actions never record completion. Status, help and previews never
start sign-in or request sudo. Deselection does not reset the preference;
revoke with `sudo tailscale set --operator=`.

`hostname` derives `nimbus-<machine-id>` from the machine ID. A matching
static hostname is complete. Otherwise it previews
`sudo -- /usr/bin/hostnamectl set-hostname NAME` and asks the user to close
browsers first: Chromium can refuse a profile whose lock names another
hostname. Never delete locks or browser data. The static name, never the
transient DHCP name, is the completion check. An invalid derived name offers
no action.

`dtu-network` installs the reviewed DTU CAT CA bundle at
`/etc/NetworkManager/certs/dtu-eduroam.pem` and coordinates one named,
user-restricted `Nimbus DTU eduroam` profile. Inspection is offline,
unprivileged and secret-free, requires a named non-root caller and installed
prerequisites from a valid receipt or the recorded schema-2 baseline, and
never scans or activates. Detected profiles are matched by SSID (`eduroam`,
`DTUsecure`); matching profiles are listed and a default-No keep-or-replace
prompt follows that `--yes` cannot bypass. Declining reads no credentials and
changes nothing. The pinned bundle is SHA-256
`4044ec3c69ea71dade30be85294f22d6a3659cfb9c2b90986cebe3623775a7c1`; all
three CA certificates and signatures are validated, its validity ends
2027-12-02, and only the exact previously pinned bundle may be replaced.
Configuration uses PEAP/MSCHAPv2, anonymous identity `anonymous@dtu.dk`, the
pinned CA and `domain-match` names `ait-pisepsn03.win.dtu.dk` and
`ait-pisepsn04.win.dtu.dk`; server certificate verification and native TLS
validation are never lowered.

Credentials come from hidden manual entry or an optional 1Password item UUID
read through the caller's native `op` CLI, never sudo. `@dtu.dk` is appended
only to a bare username; empty, duplicate or foreign domains are rejected.
Secrets stay in memory or private pipes: no arguments, environment,
temporary files, logs or Git. NetworkManager owns storage with
`password-flags=0` in its root-only system connection file, unencrypted
within that file, disclosed before approval. During approved setup only, the
saved password is read back through `GetSecrets` and compared privately
before activation; inspection never retrieves passwords. Automatic
connection is enabled and verified before the separate connect-now prompt.
An absent eduroam SSID is a normal configured-for-later outcome; failed scans
are unknown. Actual scan or activation errors fail setup even with a valid
profile. Completion means certificate and profile configuration verified;
connection, Internet access and reboot reconnection are separate evidence.
Partial state is preserved for retry. Reset or deselection does not remove
profiles, credentials or the CA.

`account-picture` registers the Chezmoi-supplied JPEG for the invoking
non-root user through AccountsService's `SetIconFile`, after verifying the
package receipt, a readable valid source and the running service. Inspection
reads only user name and icon path through non-activating property requests
and compares image content, not paths. Approval binds to source and current
icon hashes, and saved bytes are verified after execution. No privileged
receipt and no direct daemon-file writes. Missing source blocks; deselection
leaves the preference.

`nvidia-mok` retains native MOK checks and the firmware enrollment procedure.
Inspection distinguishes missing, empty, permission-denied and other
unreadable certificate states. A missing key pair is reported with the native
`sudo kmodgenca -a` instruction; Nimbus never creates or replaces the pair. An
approved run authenticates sudo, inspects and preserves the akmods key pair,
rebuilds NVIDIA modules whose signing
identifiers do not match the certificate, refreshes the running kernel's boot
image and submits the untrusted certificate with `mokutil --import`.
Passwords stay in native prompts; Nimbus never reboots or weakens Secure
Boot. Enrollment checks use `--ignore-keyring --test-key`; for mokutil 0.7.2
an exact enrolled/trusted result with exit 1 is complete, an exact
not-enrolled result with exit 0 or a pending request with exit 1 stays
pending, denylisted keys are blocked, and unexpected output or exit status is
unknown. Stored verification prevented from rechecking by permissions is
shown as "Previously verified" in notices, with JSON retaining native
`unknown` and `previously_verified: true`; it is not current verification.
Status, help and `--plan` never authenticate.

Disk encryption stays Fedora's: an installed LUKS2 root unlocked only by its
passphrase. Nimbus enrolls no TPM policy, generates no key material, builds
no unified kernel image, and owns no firmware entry or kernel-install hook.
GRUB remains the default boot path and the Paper Dark theme shows the
passphrase prompt during boot. The `fde` component, its postinstall task and
their evidence no longer exist.

Login follows the same boundary: greetd starts the owner's session directly
from `[initial_session]` in `/etc/greetd/nimbus.toml` because the disk
passphrase is the authentication. The default session (the Noctalia greeter)
starts again after logout. Nimbus owns that file and leaves the package's
PAM stack untouched; the passwordless default keyring is what keeps
applications from prompting, and it is user configuration under `$HOME`,
owned by Chezmoi and never written by a root process. `nvidia-mok` remains
the only Secure Boot key flow and keeps the rules above.

UWSM starts Hyprland only after `graphical.target`, so with auto-login every
unit ordered before that target delays the desktop. `hyprland-session`
therefore disables `NetworkManager-wait-online.service`: `docker.service` and
`rsyslog.service` order the target behind `network-online.target`, and the
desktop waited 14 seconds for link and DHCP. Nothing selected needs the
network at boot; a component that does must not rely on this target.

`noctalia-lockscreen` is a narrow exception to runtime-state ownership: after
preview and approval it may remove only `lockscreen_widgets` and descendants
from Noctalia's settings, never other preferences. It requires an unlocked
local session with closed panels and editor, and exactly one invoking-user
daemon with supported launch arguments. It previews the shell interruption,
rechecks under lock, and verifies executable, UID and start time through a
pidfd before SIGTERM; it never force-kills, stops Hyprland, logs out or
requests sudo. A ten-second exit timeout leaves settings untouched; otherwise
the shell is restarted even on failure or cancellation. The settings file is
read and hashed again before writing; symlinks, foreign ownership,
shared-writable ancestors, hard links and oversized input are rejected. An
exact 0600 backup is saved under private Nimbus state before atomic
replacement, unrelated values and formatting are preserved, and no backup is
restored automatically. Equivalent screen-adjusted layouts are accepted by
comparing per-widget `cx / placement_width` and `cy / placement_height`
within one millionth, with positive extents and finite numbers; IDs, order,
types, sizes, appearance and other settings stay exact. Missing pairs match
only when both omit them. A mismatching saved or effective layout, changed
managed file or changed unrelated preferences prevents completion.

`noctalia-plugins` verifies enabled plugins against runtime manifests and
readable entry scripts through Noctalia's native configuration and local IPC;
intent and cached catalogs alone do not establish installation. Unknown
config, unavailable IPC or uninitialized catalogs offer no mutation. Approved
runs use native source-update commands for sources with missing runtime
files, which may also update installed plugins from those sources; Noctalia
owns downloads and live refresh. Retries only inspect state, waiting up to
two minutes for the update to settle; at the deadline the latest verification
problem is reported as failure with native partial results preserved. No
second catalog, managed runtime files or privileged receipts are created.
Sync never starts a desktop or downloads plugins.

`hyprland-plugins` reads Chezmoi's strict schema-1 selection at
`~/.config/hypr/plugins.toml`, which is empty or
`enabled = ["scrolloverview"]`. Inspection verifies the invoking user's
HyprPM cache, header marker, build result, ABI, installed/running state and
live plugin list without running `hyprpm list` (which can initialize
privileged state). Matching ABI and an active session are required; an
upgraded but running compositor needs a new login. Unknown cache formats,
foreign repository collisions and unreadable state stop setup. After
approval, only planned native update/add/enable/reload stages and a Hyprland
config reload run; the upstream is fixed in the engine and user input never
supplies commands. Failed builds use native forced update, which bypasses
only the up-to-date optimization. HyprPM keeps native trust and sudo; it
never runs as root. Each stage is verified, including builds whose command
returned zero. Native disable/remove owns removal. Appearance still needs
user testing.

`proton-cachyos` offers
`protonplus install steam-system proton-cachyos latest` after approval when
native Steam and ProtonPlus are selected and recorded. Steam must have been
started once. ProtonPlus owns runner downloads, updates and removal; Nimbus
neither queries releases during inspection nor records completion from an
exit code. This is never an automatic download during init or sync.

`agent-proxy` is selected with GitHub Copilot. Help, plan and status read
only local definitions and registration evidence; execution requires
approval, a running Copilot desktop, Mise-selected Node 24 or newer, Herdr
and authenticated CLIs. Authentication stays a separate native step and the
task never starts sign-in or asks for privileges. Chezmoi owns the
loopback-only proxy configuration; the upstream current-user installer owns
releases, credentials, services and backups. Nimbus builds pinned source
after SHA-256 verification, applies the embedded compatibility patch and
invokes that installer. Nimbus owns `agent-proxy.json` and its lock under
private XDG state; they are never synchronized and contain no credentials.
Copilot's provider API stores the API key. Successful complete discoveries
add, update and prune only owned model entries per provider; failures and
manual records are preserved. Setup opts the machine into a model refresh
after Topgrade in `sync --upgrade`; ordinary sync, standalone `upgrade` and
`upgrade --system` do not run it. A closed Copilot app defers refresh before
any discovery or mutation. Reset disables the opt-in and retains services,
credentials, configuration and model ownership.

`launch browser` and `launch webapp` prefer the default browser from
`xdg-settings`, then installed browsers in fixed order; Firefox and LibreWolf
support regular and private windows, not webapp mode. Private requests always
use the private flag and never fall back to a regular launch. URLs are
validated, arguments never pass through a shell, and no browser is installed
or configured. Chezmoi owns webapp desktop entries. In a local Wayland
session with an active UWSM compositor, launches use `uwsm-app --`; other
sessions launch directly, and a missing wrapper fails clearly.

## Desktop, boot and recovery

Use Hyprland through UWSM with Noctalia Greeter, keeping stable Hyprland
`0.56.*` and Noctalia `5.*` through native DNF policy without freezing
unrelated libraries. Retain Fedora defaults unless a demonstrated need
justifies a change.

The GRUB target is the neutral Paper Dark theme, a visible five-second menu,
Fedora default and native Windows discovery only where Windows exists. The
engine ships the inert `/etc/grub.d/09_nimbus_previous_kernels` drop-in, which
maintains a Nimbus-owned mirror of the one default BLS entry under
`/boot/loader/entries-nimbus` through `nimbus internal boot-menu`, lists it
with `blscfg <entry>`, and lists older kernels and rescue entries under an
unfiltered `blscfg` submenu. The submenu is never filtered by `saved_entry`.
Both blocks are guarded in `grub.cfg` by a check for the live BLS entry and
its mirror, so a kernel removal followed by a failed `grub2-mkconfig` falls
through to Fedora's flat menu. Fedora's flat menu also returns when the
engine, marker or mirror is unavailable. Because Fedora's grub hook
regenerates `grub.cfg` only when BLS is disabled, the engine ships the inert
`/etc/kernel/install.d/96-nimbus-menu.install` hook, which runs after the
boot-entry hook on kernel installs and removals and runs
`grub2-mkconfig --no-grubenv-update`; it warns instead of failing the kernel
transaction. The marker-gated dracut module
`/usr/lib/dracut/modules.d/40nimbus-plymouth` keeps `fc-match`, fontconfig and
the monospace faces in the initramfs while the marker exists, verifying them
before installing so a missing dependency skips the module. It also installs
the script plugin and the complete `themes/nimbus` payload itself:
`plymouth-populate-initrd` reads only the first key of `[Daemon]` to pick the
theme, so any key before `Theme=` would otherwise leave the theme out of the
initramfs. The GRUB drop-in
loads custom faces only when Secure Boot is off, because the shim-lock
verifier refuses `loadfont`; with Secure Boot the theme uses GRUB's built-in
font.

The engine package ships the payload under `/boot/grub2/themes/nimbus` and
`/usr/share/plymouth/themes/nimbus` plus the inert
`/etc/grub.d/36_paper_dark` drop-in. The `boot-theme` component owns only
`/etc/nimbus/boot-theme.enabled`; its `grub-config` and `plymouth-theme`
triggers regenerate `grub.cfg` and reselect the theme after approval. Both
triggers re-run once when their receipt came from another engine version, and
a repeat run keeps the theme recorded before Nimbus took over. Removing the
marker restores Fedora's default configuration: a theme chosen by the user is
kept and rebuilt without the Nimbus payload, while a current Nimbus theme is
restored from the record. Removal blocks when no usable previous theme was
recorded, when it is Nimbus's own, or when it is no longer installed. The
trigger receipt is retired only after a successful run.

Plymouth must only see the GPU that has the monitor. In the initramfs it
binds the first native DRM driver and accepts the firmware framebuffer only
after `DeviceTimeout`. On the desktop the monitor hangs on the NVIDIA card,
whose driver RPM Fusion keeps out of the initramfs, while the Intel iGPU
drives nothing; with `i915` in the initramfs the unlock prompt was invisible
or took no keyboard input. The desktop-only `nvidia-boot-display` component
owns `/etc/dracut.conf.d/90-nimbus-boot-display.conf`, which omits `i915`
and `xe`, so Plymouth draws the Paper Dark prompt on the firmware framebuffer.
The Intel drivers load from the root filesystem after unlock. The file also
omits `nouveau`, which the kernel command line blacklists and which otherwise
adds about 100 MB of firmware to the initramfs. Plymouth accepts the firmware
framebuffer only after `DeviceTimeout`, eight seconds of black, so while that
drop-in exists `40nimbus-plymouth` adds `UseSimpledrm=2` after `Theme=` in
the initramfs copy of `plymouthd.conf`. The package-owned host file is never
edited, a value the owner set there wins, and machines with a native display
driver in the initramfs never get the key, because there it blanks the
prompt when that driver loads. Its `initramfs-rebuild` trigger runs
`dracut --force --regenerate-all` after approval: every installed kernel is
rebuilt and the rescue image is not touched. It runs after the other
triggers of the same run, so the images contain the Plymouth theme that
`plymouth-theme` has just selected. Removing the component removes the file
and rebuilds again. Do not select it where the iGPU drives the
display, and do not put the NVIDIA modules in the initramfs: that ties the
unlock prompt to akmods build order and MOK enrollment. A themed prompt
counts as verified on a machine only after a passphrase was typed there.

When the unlock prompt fails, boot the rescue entry under Previous kernels;
its generic initramfs has no Plymouth DRM renderer and asks on the console.
Alternatively press `e` on the default entry and delete `rhgb` for one boot.

Accepted limitations: `grubby` and `grub2-set-default` changes apply only
after a later `grub2-mkconfig`; a failed boot keeps the forced five-second
timeout; a submenu opened once shows no entries on a second open; a kernel
installed while only one entry existed appears after the next successful
`grub2-mkconfig`; `set timeout=5` overrides `GRUB_TIMEOUT` and
`menu_auto_hide`; without `nvidia-boot-display`, `plymouth-theme` rebuilds
only the running kernel, so older initramfs images keep the default Plymouth
prompt until they are rebuilt; and BIOS-only systems are untested.

TTY repair is the supported recovery direction; no extra recovery desktop is
installed. Owned legacy session files are retired through receipts on a
reviewed sync, while changed or foreign files are preserved and reported.
Use Ctrl+Alt+F3 to attempt TTY login, correct the relevant source and apply.
Boot failure uses Fedora rescue tools or installation media. Nimbus provides
safe retry and owned-resource repair, not automatic rollback or backups.

### Snapper

`hyprland-noctalia` selects the `snapper` component. Nimbus manages the root
template `/etc/snapper/config-templates/nimbus` and uses native Snapper to
create and reconcile the `nimbus` configuration. Setup requires a Btrfs root;
without pre-created storage native Snapper creates it, and the installation
guide's empty `/.snapshots` mount may be adopted after approval when it is a
root-owned Btrfs subvolume not writable by others, rechecked for identity and
emptiness. Interrupted adoption retries only with the exact marked
configuration and still-empty storage. Foreign configurations, populated
directories and unproven storage need explicit migration and are never
deleted to make setup pass. Native calls use `--no-dbus`.

After approval, mutating sync takes a before/after root snapshot pair;
`upgrade --system` does the same, including from Topgrade, and combined
sync/upgrade makes a pair per system phase. User-tool updates are outside.
Previews and unchanged sync create none; the first setup run says it installs
without a before snapshot. A failed before snapshot stops changes. Failure
during a protected operation still attempts the after snapshot and cleanup
while preserving the original error. On interruption Nimbus waits for the
current native command, stops further work and finalizes under the operation
lock; forced termination or power loss cannot.

Retention targets six individual snapshots (about three pairs) with no
grace period or important allowance; timeline and boot snapshots stay off.
Native cleanup runs after each protected operation and through
`snapper-cleanup.timer`; pair preservation and active snapshots may exceed
the target, so it is not a disk-space cap. The root snapshot excludes
separate filesystems and nested subvolumes such as home, boot, EFI and
separate data mounts; it is not a bootable-rollback promise. A restore drill
is required before documenting a restore procedure. Removing the component
stops hooks and retires template/timer ownership while native configuration
and snapshots remain.

## Security

The owner's declared COPRs are approved sources; Nimbus does not re-audit
their contents. Keep native signature checks, HTTPS, pinned repository keys
and source checks; review key changes. Bootstrap verifies Nimbus against the
checked-in COPR key and fingerprint; later engine updates belong to DNF.

Accepted download exceptions are limited to official `github/app` RPMs and
`WowUp/WowUp.CF` AppImages: helpers verify official HTTPS origin, GitHub
SHA-256 and application/version/architecture identity, relying on GitHub's
release channel rather than a publisher signature. The COPR signature
authenticates the helper, not the download. Nimbus previews helper actions
without downloading or parsing artifacts. Any Copilot unsigned-local-RPM
exception is confined to that artifact's transaction.

Nimbus runs as the normal user and elevates only specific native operations
and narrow owned-file/state writes. No root daemon or passwordless sudo.
Reject foreign ownership, path traversal and symlink escape before writes or
deletion. Preserve approved checkout origin checks. Never place secret values
in definitions, arguments, receipts or logs; installation logs are private
and exclude authentication and secret-capable output. Keep the preview,
locking, verification and partial-failure rules during simplification.

Keep Secure Boot enabled on hardware, SELinux enforcing and firewalld active,
and open only needed services and ports; do not weaken these to fix an
application. NVIDIA signing enrollment stays explicit and native. Doctor
permits disabled Secure Boot only for the selected `vm` machine when native
inspection confirms a virtual machine; an unreadable boot or virtualization
check stays unknown, and this is not hardware acceptance. Nimbus never
partitions, encrypts or re-enrolls disks. Docker group membership is
root-equivalent and remains an explicit selection. Nimbus is not a boundary
against an attacker with root.

## Architecture and tests

~~~text
load definitions -> inspect -> plan -> approve -> recheck -> apply -> report
~~~

Public sync checks the installed engine before definitions or fetches, then
uses this flow and offers Chezmoi apply; combined upgrade restarts after an
engine replacement, syncs once and finishes with Topgrade. The future TUI
reuses the same operations.

`internal/definitions` resolves desired state, `inspect` reads native state,
`plan` compares them, `apply` executes native operations and `state` records
verified results; `cli` connects them, and `postinstall`, `launch` and
`snapper` hold their feature helpers. Native calls use argument vectors
without shell evaluation; hidden privileged commands only write approved
files or state. `native` owns command execution and filesystem access;
`inspect` uses it only for read-only queries and re-reads state after
mutation, never substituting cached data for unknown results. Shared fakes
live in `native/nativetest` and never touch the host.

Keep strict schemas, ownership checks and state-reader compatibility. The
current state marker is schema 3 with schema 2 receipts/baselines; ambiguous
legacy identities block removal. Keep Go tests beside their packages and
cross-tool tests in `tests/integration/`; cover preview/cancellation,
ownership, validation, partial failure, retry, convergence, compatibility and
constraints. Share fixtures at their owning boundary, remove tests for
removed scope, and keep an interface only for a real native/test boundary.
Use existing dependencies and the standard library. Run `just check` and
`just validate`; destructive checks use disposable VMs and opt-in native
checks are reported separately.

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
| `tools/boot-theme/` | Generate Plymouth assets and preview both boot themes |
| `licenses/` | Supplemental third-party notices included in releases |

Only the installation handoff participates in normal bootstrap. Bash connects
native commands before Nimbus is installed; Python provides terminal/process
control and small build/test utilities. Keep third-party notices, bootstrap
logging, terminal supervision, signature verification, prompts and
cancellation.

## Out of scope

No custom snapshot or boot-archive manager, automatic restoration, backup
service, Windows VM lifecycle, Home Assistant integration, UKI build pipeline
or broad boot-key management is required for the desktop milestone. The
installation guide's partition layout is operator-owned; Nimbus registers
suitable existing snapshot storage or lets native Snapper create it and never
partitions disks. Fedora installation, partitioning, initial encryption and
major-version upgrades remain native operator workflows. There is no
background reconciliation daemon, generic provider or plugin system, secret
manager, fleet manager or cross-distribution support.
