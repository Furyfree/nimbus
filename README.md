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

The local candidate supports:

| Task | Command |
| --- | --- |
| Set up a machine | `nimbus init --machine desktop` |
| See what needs attention | `nimbus status` |
| Preview system changes | `nimbus sync --plan` |
| Update definitions, system setup and user configuration | `nimbus sync` |
| Update installed software | `nimbus upgrade` |
| Upgrade software, then sync everything | `nimbus sync --upgrade` |
| Finish manual setup | `nimbus postinstall` |
| Diagnose a problem | `nimbus doctor` |
| Preview user configuration | `chezmoi diff` |
| Apply user configuration | `chezmoi apply` |
| Fetch and apply dotfiles updates | `chezmoi update` |

## Current checkout

The repository-update workflow is local and unreleased. `nimbus sync` updates
the Nimbus and configured Chezmoi repositories, shows the system plan, applies
approved changes, then asks to apply Chezmoi configuration and its scripts.
`sync --upgrade` runs Topgrade first, then starts the updated Nimbus executable
for sync. Topgrade's system callback remains `nimbus upgrade --system`.

Both repositories must be clean and track an approved origin. Local edits,
local-only commits or diverged history stop the run with the repository path
and repair advice. Nimbus never stashes, resets or commits for you.
Profile and system-file changes need only a checkout update; Go command
changes, including this new workflow, need a new Nimbus RPM.

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
