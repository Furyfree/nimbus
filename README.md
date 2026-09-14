# Nimbus

Nimbus sets up and maintains the owner's Fedora workstation. It installs the
selected software, stores and manages root-owned system files, shows drift,
and applies changes after a preview. The target is Fedora 44 on x86_64, with
Hyprland and Noctalia.

Chezmoi owns user configuration and Mise tools. COPR owns packaging. Topgrade
runs the configured update workflow. Nimbus connects these tools where needed.

## Start here

- [What Nimbus does and its commands](docs/SPEC.md)
- [Install Fedora and run Nimbus](docs/INSTALLATION.md)
- [Next work and deferred scope](docs/ROADMAP.md)
- [Current implementation gaps and checks](docs/TASKS.md)

## Local agents in Copilot

Open the GitHub Copilot desktop app and sign into the desired agent CLIs first.
Chezmoi selects Node, Herdr and the CLIs through Mise. Then preview and run:

~~~sh
nimbus postinstall agent-proxy --plan
nimbus postinstall agent-proxy
~~~

The task applies only the managed proxy config, builds the checksum-pinned
upstream source with a reviewed compatibility patch, and uses its native user
installer. It registers a localhost provider through Copilot's own API and
credential store. No keys are copied into dotfiles; no sign-in flow is started.
Existing trial ownership is adopted only when its native record IDs match.

After setup, `nimbus sync --upgrade` refreshes models after successful Topgrade
updates. Plain sync and the Topgrade system callback do not refresh them.
Rerunning the postinstall task checks the managed configuration: an already
configured installation refreshes models only, without reapplying dotfiles or
registering credentials again. Missing/changed setup gets a separate preview.
Codex, Claude, Grok and Antigravity are queried separately: a complete
successful list replaces only Nimbus-owned entries for that provider. Failure
preserves its previous list; manual entries are preserved. Copilot being closed
defers the refresh. Antigravity currently supports text only through this proxy;
external tool execution has been verified with the other three backends.

~~~sh
nimbus postinstall agent-proxy --reset --plan
nimbus postinstall agent-proxy --reset
~~~

Reset disables automatic refresh without deleting models, credentials or
services. Rerun setup to enable it. To remove the application, use the upstream
installer's `uninstall` action from the pinned source, after reviewing its
documentation, and remove the provider in Copilot. Preserve Nimbus's
ownership file until deciding whether to reinstall or discard that integration.
The upstream installer owns release backups and rollback. Nimbus never edits
Copilot's database directly. Catalog synchronization depends on Copilot's native
local IPC, which may change with future app releases.

## Install

On Fedora 44 x86_64, run as your normal user:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash
~~~

On first installation, enter the machine name when asked:

- `desktop`: the MSI Z690 desktop with NVIDIA RTX 3080.
- `laptop`: the HP EliteBook with AMD graphics and laptop power settings.
- `vm`: the disposable test VM, without physical-machine hardware settings.

Nimbus shows the installation plan and asks before applying it. Reruns reuse
the saved machine. To choose explicitly, replace `bash` with
`bash -s -- --machine vm` (or `desktop` or `laptop`).

The machine prompt requires Nimbus 0.3.1 or newer. With 0.3.0, use the
explicit argument.

The installer obtains Nimbus and starts setup. See the
[installation workflow](docs/SPEC.md#installation-workflow) for the Fedora base
requirements.

## Workflow

Nimbus supports:

| Task | Command |
| --- | --- |
| Set up a machine | `nimbus init --machine desktop` |
| See what needs attention | `nimbus status` |
| Preview system changes | `nimbus sync --plan` |
| Update definitions, system setup and user configuration | `nimbus sync` |
| Update installed software | `nimbus upgrade` |
| Update Nimbus, sync once, then upgrade software | `nimbus sync --upgrade` |
| List setup commands | `nimbus postinstall --help` |
| Check remaining setup | `nimbus postinstall status` |
| Review setup guidance | `nimbus setup-notes` |
| Diagnose a problem | `nimbus doctor` |
| Preview user configuration | `chezmoi diff` |
| Apply user configuration | `chezmoi apply` |
| Fetch and apply dotfiles updates | `chezmoi update` |

Normal previews show changes and problems. Add `--verbose` for full policy,
resource and diagnostic report details:

~~~sh
nimbus sync --plan
nimbus sync --plan --verbose
nimbus sync --upgrade
~~~

Engine upgrades use DNF's transaction confirmation. System changes and Chezmoi
apply retain separate approvals; unchanged system checks need no mutation
approval. Executing sync announces a read-only administrator check for selected
greeter authorization before planning changes. Previews never request sudo.
`sync --upgrade --yes` approves automated Nimbus and Topgrade updates, including
its Nimbus system callback. It does not force Chezmoi file conflicts, confirm
manual setup, or opt into SSH integration. Native tool prompts and progress
remain visible. Historical MOK verification remains in `postinstall status`
and verbose maintenance reports.

Init, sync and upgrades keep private metadata records under
`$XDG_STATE_HOME/nimbus/runs` (default `~/.local/state/nimbus/runs`). Records
identify the engine, machine, available definitions commit, timings and outcome;
they exclude command output, error bodies, arguments, credentials and rendered
configuration. The latest 20 completed or abandoned records are retained;
active runs are preserved. A record is limited to 64 KiB. Failures and verbose
sync reports show the path. Init also keeps its existing installation logs,
now including the final Nimbus report. An abruptly terminated run can remain
marked running; it is not evidence of success.

Status counts package installations/source repairs separately from pending
system checks and configuration changes. A greeter authorization recheck that
requires sudo is pending work, not a missing package.

Nimbus highlights important text with bold, bright terminal colors: green for
success,
yellow for pending work and notices, red for failures, and cyan for actions and
previously verified results. Headings and outcome totals stand out; secondary
metadata is subdued. Text wraps to your terminal width with indented
continuations, and setup notes use bullets.
Pipes, redirected output and JSON stay plain. Set `NO_COLOR=1` to disable colors,
for example `NO_COLOR=1 nimbus status`. Native tools retain their own output;
Nimbus styling is excluded from installer logs. Doctor prints each resource
name once and closes with a check summary. Upgrade previews show the engine
step first, system sync next and Topgrade last.

Selected RPMs install and upgrade only from their declared sources. A package
already installed from another repository gets a reviewed source-correction
plan, including a downgrade or reinstall when necessary. Preview it with
`nimbus sync --plan`; `nimbus sync` applies approved corrections. Preview system
updates with `nimbus upgrade --system --plan`. Nimbus stops if the declared
source cannot provide the package instead of falling back to another source.

## Guided setup

~~~sh
nimbus postinstall
nimbus postinstall status
nimbus postinstall onepassword --help
nimbus postinstall onepassword --plan
nimbus postinstall onepassword
nimbus setup-notes
~~~

Bare postinstall and its help list task commands. Status uses short verified
descriptions; task help explains verification limits and JSON retains full
inspection details. Sudo recheck commands appear on their own indented line.
Status is a compact checklist;
reboot and logout appear as notices. Each task previews native changes and checks
completion. `--yes` approves automation, never manual GUI readiness. Successful
work is recorded automatically. For supported tasks, `--mark-done` confirms
manual prerequisites and verifies existing setup without installing or applying
configuration. Completion is recorded only when verification passes. `--reset`
clears that task's local evidence without undoing configuration.

The 1Password task guides GUI prerequisites and verifies the selected SSH/Git
integration. Matching configuration needs no apply. Changes show a concise list
of files and managed parent directories to create or update, followed by approval.
Chezmoi creates missing parent directories, excludes scripts, and retains native
file-conflict prompts. Skip manual SSH/Git file edits. For a full private diff
during guided setup, use `nimbus postinstall onepassword --diff`; it prints
directly without a pager and is never logged. This option still runs guided
setup and may request authentication; `--plan` remains the read-only overview.
The guided task offers SSH/Git integration when it is not selected, even after
CLI-only setup is complete. Answering yes saves the choice through Chezmoi;
the default is no, and `--yes` does not opt in. `--mark-done` checks the existing
selection without changing it. Chezmoi and init's `--onepassword-ssh` also remain
available for explicit opt-in.
Remote SSH access and signing-key registration are separate checks.

For setup you already completed:

~~~sh
nimbus postinstall onepassword --mark-done
nimbus postinstall nvidia-mok --mark-done
~~~

`1password` is also accepted as an alias for `onepassword`. Explicit 1Password
verification may request authorization through 1Password. After GUI confirmation,
if the CLI reports that the account is not signed in, Nimbus offers `op signin`
and retries verification. Already authenticated sessions skip sign-in. This also
works with `--mark-done` and does not apply configuration files. Sign-in stdout
is discarded; native prompts stay in your terminal and are not logged.

MOK verification may
offer read-only sudo commands when the public certificate cannot be read as your
user; it never enrolls a key. Missing certificates remain blocked. Status never
requests authentication. When a previously verified certificate requires sudo to
recheck, status labels it "Previously verified" and final reports show a notice.
Both show `nimbus postinstall nvidia-mok` to request the sudo check. Run that
command as your normal user so completion stays in your own local state.
Missing certificates or observed enrollment failures still appear as setup or
verification problems.

To restore Chezmoi's lockscreen layout after GUI experiments:

~~~sh
nimbus postinstall noctalia-lockscreen --plan
nimbus postinstall noctalia-lockscreen
~~~

Run inside the unlocked Noctalia desktop session, with its panels and lockscreen
editor closed. This explicit repair briefly stops only Noctalia with SIGTERM,
backs up its settings privately, removes only `lockscreen_widgets` overrides,
and starts Noctalia again. The bar and shell services briefly disappear;
Hyprland and applications remain running. No sudo or automatic lock is used.
Matching layout files must already be applied through Chezmoi. Normal sync
never performs this repair.

The task prints a backup path under `${XDG_STATE_HOME:-~/.local/state}/nimbus/`.
Backups are private, exact copies and may contain personal preferences; never
commit or share them. They remain for manual review/removal. On restart failure,
run `noctalia --daemon` from the desktop terminal. A failed repair retains its
backup and does not record completion. Review a backup locally before restoring
anything; copying the whole file back would overwrite later preferences.
Verification covers effective configuration, not visual placement. Test the
result yourself with `noctalia msg session lock`. `--reset` only clears task
evidence, not configuration or backups.

Nimbus owns the complete catalog in `setup-notes.json` at the checkout root;
viewing it does not require Chezmoi. Init shows setup notes together. Later syncs
show only new or revised guidance;
`setup-notes` always shows all applicable notes without changing state. Display
history and task evidence are separate private files under
`${XDG_STATE_HOME:-~/.local/state}/nimbus/`, scoped by machine and never synced.
Status rechecks native configuration, so old completion cannot hide missing files.

## Current checkout

Each machine manifest selects the invoking user's default login shell with
`shell = "bash"` or `shell = "zsh"`. Both shells are common packages, and
Chezmoi keeps both configurations. All tracked and newly created manifests
choose Bash initially. Change the field and preview with `nimbus sync --plan`
before syncing. The plan shows the old and new shell and the native `usermod`
command; an approved change takes effect after logout and login. Removing
the field stops managing the preference and keeps the current login shell.
The field requires the 0.4.1 engine; update the Nimbus RPM before
using these definitions. `nimbus why login-shell` explains its selection.

The maintenance workflow requires Nimbus 0.5.0 or newer. Both sync forms
request sudo first and check fresh Nimbus RPM metadata before fetching either
repository. Plain `sync` stops when an engine update is available and directs
you to `sync --upgrade`. Failed checks stop clearly.

`sync --upgrade` upgrades Nimbus first when needed, verifies and restarts the
executable, syncs configuration once, and runs Topgrade once. Its callback stays
`nimbus upgrade --system`; there is no second configuration apply. Explicit
system upgrades use refreshed metadata for both preview and execution. One final
Nimbus report lists changes, failures, remaining setup and session notices.
Before starting an upgrade transaction, Nimbus checks the full DNF transaction
and fresh system Flatpak update metadata. When both are current, it skips the
transaction and its snapshots. Failed checks stop the upgrade.

Routine output groups matching-file ownership refreshes and snapshot results.
Replanning shows only changes to remaining operations; informational notices
stay separate from differences. Native transaction progress and errors remain
visible. Use `sync --plan` for the complete local plan, including matching paths.

Common installs ble.sh from the owner's COPR; the desktop profile selects the
Noctalia-compatible LibrePods fork from its own COPR. Copilot's helper upgrade
queues the app installation. No separate COPR-enable or package-install
commands are needed after the engine is current. Bash loads ble.sh in a new
terminal; LibrePods starts at the next graphical login when Bluetooth exists.

Older releases may need a one-time `sudo dnf upgrade --refresh nimbus` to obtain
this workflow once published. After that transition, `nimbus sync --upgrade`
owns the fresh engine check and update; no recurring manual DNF command is needed.

Both repositories must be clean and track an approved origin. Local edits,
local-only commits or diverged history stop the run with the repository path
and repair advice. Nimbus never stashes, resets or commits for you.
Profile and system-file changes need only a checkout update; Go command
changes need a new Nimbus RPM. The repository-update workflow requires 0.4.0.

`--plan` uses local definitions without pulling or applying anything. Status,
doctor and lists also stay read-only. JSON mutation requires `--yes`; the
combined upgrade does not support JSON. Chezmoi commands still work directly.
Bare `nimbus` prints help. [Next steps](docs/ROADMAP.md#next-steps): GRUB,
FDE auto-unlock post-install, shared operations, then the dashboard.

COPR helpers own application downloads and removal. Copilot initial setup is
available through `nimbus postinstall copilot`. WoWUp still needs standalone
install/update commands in its COPR helper before integration can finish.
Repair uses TTY; old owned graphical recovery files retire on sync. The
Hyprland/Noctalia profile uses Snapper around system changes with six-snapshot
retention. See [snapshot behavior and limits](docs/SPEC.md#snapper).

With native Steam and ProtonPlus selected, `nimbus postinstall proton-cachyos`
offers the Proton-CachyOS Latest download. Start Steam once first. ProtonPlus
owns installation and rolling updates; init and sync do not download runners
automatically. This action requires engine 0.4.1 or newer.

With Noctalia selected and installed, `nimbus postinstall noctalia-plugins`
checks the effective enabled selection against the running shell's local
catalog and exported runtime files. Run it in the desktop session after
Chezmoi apply. Affected sources are updated through native
`noctalia msg plugins update SOURCE` commands. Noctalia exports their enabled
plugins and refreshes the live registry and bar; already-installed plugins from
those sources may also update. Uninitialized source catalogs remain unknown:
let Noctalia initialize its sources at desktop startup, then retry.
Nimbus waits up to two minutes for readable manifests and entry scripts;
queued background work alone is not success. Retry after a failed download.
Plugin selections, sources, bar aliases and overrides remain owned by
Chezmoi/Noctalia; Nimbus stores no plugin list or privileged completion receipt.
A complete task proves runtime files are present, not account readiness or widget
behavior.
Use engine 0.4.5 or newer so verification retries while background updates
settle. The task first appeared in 0.4.4.

With Hyprland's development package selected and recorded, run
`nimbus postinstall hyprland-plugins --plan` to inspect ScrollOverview setup,
then `nimbus postinstall hyprland-plugins` to approve it. Apply Chezmoi first:
`~/.config/hypr/plugins.toml` supplies schema 1 and
`enabled = ["scrolloverview"]`. Run inside the active Hyprland session as your
normal user; HyprPM keeps its native trust and administrator prompts.
This task requires engine 0.4.6 or newer.

Nimbus shows only the missing build/install/enable/load steps. A completed
setup is a no-op. If installed and running Hyprland differ, log out and back
in before retrying. HyprPM owns downloads, builds and its privileged cache;
updates may rebuild other registered plugins and reload their enabled set.
Failed builds retain native state for retry. Nimbus verifies the build and
live plugin before reporting success, then reloads Lua configuration. Init,
sync and Chezmoi apply do not silently install plugins.

The current task supports Hyprland 0.56 and ScrollOverview. It repairs local
readiness rather than checking remote releases on every run. Use `hyprpm update`
for routine plugin updates. For removal, first set `enabled = []` in the
Chezmoi selection, then use `hyprpm disable yayuuu/scrolloverview` or
`hyprpm remove yayuuu/hyprland-scroll-overview`. Nimbus does not remove plugins
when they are deselected or write a privileged completion receipt.

The `hyprland-noctalia` profile selects a minimal greeter appearance from
`/etc/noctalia/greeter.toml`: Inter, no logo or theme selector, power controls
at bottom-right, and the Synced color scheme. The selected personal machines
use `pby` as the default account. The greetd systemd drop-in exposes that file
read-only at `/var/lib/noctalia-greeter/greeter.toml` inside the service's mount
namespace. Nimbus manages the two `/etc` files; it does not replace Noctalia's
`sync.toml`, wallpaper files or other runtime state. There is no blur or
wallpaper-generation pipeline.

Use `nimbus sync --plan` to review, then `nimbus sync` to apply after approval.
Reboot when ready to activate a new greetd mount namespace. A simple logout or
daemon-reload does not refresh the bind mount after an atomic file replacement;
Nimbus never restarts the active display manager. Existing standalone greeter
configuration outside the service namespace stays untouched. Removing this
integration follows the existing console-only desktop retirement path and
retains Noctalia runtime state. Chezmoi enables Noctalia's native greeter
auto-sync for wallpaper and colors; Noctalia owns the generated copies.
Settings > Security > Noctalia Greeter > Sync Now refreshes the current image.
The Hyprland session component also selects `greeter_passwordless_sync`.
Nimbus 0.5.3 and newer include this in the ordinary `init`/`sync` plan; no
postinstall is needed. After package installation and approval, Nimbus checks
Noctalia 5.1.0 or newer, the greeter's `secure-sync-v1` helper capability and
its constrained packaged Polkit action. The native Greeter 1.5.0+ command
then enables appearance sync for the invoking non-root local user only when
missing, preserving other users and administrator rules. Native status is
verified before recording success. Preview never requests authentication;
protected status is resolved after approval, before taking snapshots.

The greeter owns its generated Polkit rule. Nimbus never writes that rule or
grants access to legacy sync. This follows the
[native authorization contract](https://docs.noctalia.dev/greeter/sync/).
For an older installed shell, run `nimbus upgrade` first, then retry sync.
Manual inspection and revocation use the same upstream interface:

~~~sh
sudo noctalia-greeter passwordless-sync status "$USER"
sudo noctalia-greeter passwordless-sync disable "$USER"
~~~

A later sync restores selected authorization. To stop managing it, remove
`greeter_passwordless_sync` from the component before syncing; Nimbus retires
its receipt and retains the native rule. Revoke explicitly if desired. Failed
or unfamiliar native status never counts as completion. After setup, change
wallpaper or use Sync Now; verify the next login visually.

With AccountsService selected and installed, `nimbus postinstall account-picture`
registers `~/.config/noctalia/assets/profile-picture.jpg` for the invoking user.
Apply the Chezmoi image first and run from your desktop session. Use `--plan`
to inspect only. The task calls AccountsService's `SetIconFile` through
`busctl`, preserving native authorization, and compares the resulting icon
bytes with the source. Matching pictures need no action; a missing or invalid
source blocks setup. AccountsService owns its copy and the greeter reads it
next time it starts. Re-run after changing the source, or replace/clear it in
your desktop's account settings. No privileged completion receipt or automatic wallpaper
processing is added. This task requires engine 0.4.6 or newer.

With Tailscale selected and installed, `nimbus postinstall tailscale-operator`
offers permission for the invoking user to manage it through the CLI or
Noctalia. It previews `sudo -- /usr/bin/tailscale set --operator=USER`, then
verifies the daemon's operator preference. Use `--plan` to inspect only.
Sign-in remains manual. This action also requires engine 0.4.1 or newer.

User preferences and account setup belong to the
[dotfiles first-login checklist](https://github.com/Furyfree/dotfiles#first-login-and-setup-ownership).
Chezmoi installs declared Mise tools after applying their configuration,
including AI Usage and the agent CLIs. Nimbus does not keep another tool list
or inspect their credentials. Native application sign-in remains separate
from system installation and local Tailscale operator permission.

Use the installed command's `--help` to check its available options. Local
candidate features are not proof of a published COPR release. See
[TASKS.md](docs/TASKS.md) for delivery gaps.

## How the repository fits together

~~~text
nimbus.toml       supported release, repositories, package-manager settings
machines/         each machine's selections
profiles/         workstation bundles
components/       system capabilities and their resources
system/           owned system-file sources and assets
cmd/nimbus/       executable entry point
internal/         Go packages private to Nimbus (see below)
tests/integration/ tests for bootstrap, release and staging tools
tools/            development, packaging and installation tools
install.sh        bootstrap entry point
~~~

Nimbus reads a selected checkout and updates it during ordinary sync. Each
machine matches its own selections while sharing definitions. Nimbus does not
commit or push changes.

`internal/` is Go's convention for packages other projects cannot import.
Each directory is one package; files inside it group related code by topic.

| Package | Job |
| --- | --- |
| `cli` | Commands, prompts, orchestration and output |
| `output` | Shared terminal styling; native writer preservation |
| `definitions` | Load and resolve the TOML selections |
| `inspect` | Read installed state together or by package/source family |
| `native` | Execute commands and access files; callers decide what is allowed |
| `native/nativetest` | Replay commands and files in tests without host access |
| `plan` | Compare desired and installed state; describe changes |
| `apply` | Execute planned changes through native tools |
| `state` | Store receipts for verified changes |
| `selector` | Locate the chosen checkout and machine; check its origin |
| `checkout` | Check repository state and fast-forward approved sources |
| `doctor` | Diagnose setup problems |
| `postinstall`, `launch`, `snapper` | Small helpers for their named features |
| `rpm`, `version` | Shared package-name parsing and engine version data |

Start with `cli/maintenance.go` for repository updates and coordination, and
`cli/sync.go` for system reconciliation. Planner and executor files use
topics such as packages, repositories and Flatpak. Their unit tests stay beside
them; tests of repository scripts live in `tests/integration/`.

## Development

Use the Go version in `go.mod` and run:

~~~sh
just check
just validate
~~~

`just check` runs formatting, vet, tests, asset checks, diff checks, and the
available Markdown and shell linters. `just validate` checks the definitions.
Tests must not modify the workstation. Native system trials use a disposable
VM; see the [candidate guide](tools/vm/README.md).

[SPEC.md](docs/SPEC.md) includes security and a short architecture/test policy.
The product docs are SPEC, TASKS and ROADMAP; TOML files own the package list.
Releases and COPR publication are separate manual actions, described in the
[release guide](tools/release/README.md).

## Test an unpublished maintenance candidate

Run the local gates, then the signed-RPM regression in disposable Fedora:

~~~sh
just check
just validate
python3 -I -B tools/vm/maintenance/run.py
~~~

The last command builds a local Docker image and two test RPMs, then uses a
network-disabled container with an ephemeral signing key and package source.
It leaves the developer's packages, repositories and desktop untouched. It tests
stale-cache detection, signed engine replacement, restart, argument preservation
and reporting after a later failure. Docker images/build cache remain local;
no artifacts are published. Full init/login and real GUI setup still need a
[disposable VM trial](tools/vm/README.md).

### UWSM session integration

The Hyprland profile already includes the UWSM login entry. Chezmoi configures
session environment, app launches and the Noctalia/udiskie user services.
Log out and back in after applying these startup changes. The topbar clock
shows seconds and has wider widget spacing. Existing GUI preferences can
override managed Noctalia settings; reset only the conflicting preferences.

Browser and webapp helpers use UWSM in an active local Wayland session and
retain direct launch elsewhere. Lockscreen repair recognizes the managed
Noctalia service, previews its stop/restart and checks it before editing.
Inspect the session without changing it:

~~~sh
systemctl --user status app-noctalia.service app-udiskie.service
journalctl --user -u app-noctalia.service -u app-udiskie.service -b
nimbus postinstall noctalia-lockscreen --plan
~~~
