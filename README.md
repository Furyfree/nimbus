# Nimbus

Nimbus is a personal, opinionated Fedora workstation installer and system
manager written in Go.

It turns an installed Fedora system into the workstation defined by this
repository, detects drift in the resources it owns, shows a complete plan, and
applies only reviewed operations. Chezmoi remains the separate owner of user
configuration below the home directory.

The first target is Fedora 44 on x86_64. Nimbus does not initially install the
operating system, repartition disks, or configure full-disk encryption.

Nimbus 0.2.0 delivers Phase 6: owned system resources, Noctalia login, and
a separate recovery session. The candidate installation, reboot, and normal
login passed. Recovery/portal validation and the signed package trial remain
in [TASKS.md](docs/TASKS.md).

## Repository model

This repository owns both the Go engine and the personal system definitions:

~~~text
nimbus.toml     definition schema, engine compatibility, and repositories
machines/       selected workstation compositions
profiles/       user-facing system bundles
components/     reusable system capabilities
system/         Nimbus-owned system files and migrations
cmd/, internal/ Go implementation
install.sh      remote entry point; bootstrap is the checkout-owned handoff
~~~

An installed Nimbus engine reads definitions from an explicitly selected
checkout of this repository. Receipts identify both the engine version and the
exact definition commit and tree digest.

The separate dotfiles repository contains Chezmoi source state only and works
without Nimbus on every platform. Nimbus may perform the explicit first Chezmoi
initialization, passing the machine ID, a managed-by-Nimbus flag, and the
selected profiles. On Linux and macOS, a full Chezmoi apply writes user
configuration and invokes Mise to install its declared runtimes and tools.
Standalone use requires Mise already installed; normal diff, apply, edit, and
update operations remain direct Chezmoi commands.

Fresh installation asks for sudo, selects the machine as soon as the engine
is ready, then asks once after showing the workstation plan. Optional
1Password SSH integration is off for fresh init. To opt in explicitly:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash -s -- --onepassword-ssh
~~~

The installer prints its private log directory and elapsed time. Logs live
below `~/.local/state/nimbus/install` (or `XDG_STATE_HOME`) and retain the latest
20 completed runs. Native package and Mise output is saved; keyboard input,
Chezmoi template output, and secret-capable commands are excluded.

## Project documents

- [SPEC.md](docs/SPEC.md) defines the complete accepted system contract.
- [ARCHITECTURE.md](docs/ARCHITECTURE.md) explains how the code is
  organized and how a command flows through it.
- [DECISIONS.md](docs/DECISIONS.md) records decision rationale and unresolved
  questions.
- [SECURITY.md](docs/SECURITY.md) is the workstation security policy.
- [PACKAGES.md](docs/PACKAGES.md) lists what Nimbus installs, by application.
- [ROADMAP.md](docs/ROADMAP.md) defines implementation phases and their order.
- [TASKS.md](docs/TASKS.md) tracks the current phase and its evidence.
- [AGENTS.md](AGENTS.md) defines durable repository working rules.
- [INSTALLATION.md](docs/INSTALLATION.md) is the concise Fedora 44 base-install
  operator guide.

Superseded designs and research records were removed from the active tree. Their
last complete snapshot is commit
`c0bb8a4660732e6e9556297da2c15ba0f286ee98`; they are not active contracts.

## Development

Building requires Go 1.26.7 or newer, matching Fedora 44's native toolchain.
CI reads that minimum from `go.mod`.

The local gate is:

~~~sh
just check
~~~

It runs `gofmt`, `go vet`, `go test`, `git diff --check`, markdownlint, and
ShellCheck for the installation and release scripts.
`just validate` runs `nimbus validate` against this checkout, `just build`
produces a static `nimbus` binary that runs on any x86_64 Linux, and
`just vm-stage` builds and stages an isolated unpublished candidate on the
drill VM without replacing its installed Nimbus. See the
[candidate testing guide](tools/vm/README.md).

The engine is distributed through the signed
[`furyfree/nimbus` COPR](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/).
Bootstrap verifies its RPM against the checked-in public key and fingerprint
before asking DNF to install it. Phase 6 requires engine 0.2.0 or a local
development candidate. The signed 0.2.0 build is published in COPR.

The delivered commands are `init`, `sync`, `validate`, `doctor`, `status`,
`managed`, `unmanaged`, `why`, the `profiles`, `components`, and `packages`
groups, `files accept`, and `version`. `sync` changes the managed system: it
shows what it will do, asks once, prepares the declared sources, installs and
removes what the definitions say, upgrades the system, verifies, records
receipts under `/var/lib/nimbus`, and reports what differed from the plan.
`init` is the first run: it picks or describes the machine, writes the selector,
syncs, then initializes and applies Chezmoi, including its user-tool installs.
`install.sh` gets a fresh Fedora there using public HTTPS clones.
`sync -p` shows the plan and changes nothing, `-y` skips the question, `-n`
leaves out the system upgrade, and `-r` also removes unmanaged packages. The
`packages`, `profiles`, and `components` edit commands change the machine
manifest and then run the same sync for it. `nimbus dotfiles diff`, `apply`,
and `update` delegate to Chezmoi; apply includes its user-tool installs, and
update explicitly pulls and applies the source. Ordinary sync leaves those
installs to Chezmoi.
Failures end with a summary of completed, failed, and skipped work.
The remaining commands are read-only:
`validate` checks the definitions, `doctor` inspects the host, and the views
list what Nimbus manages. The first VM drills are recorded in TASKS.md.

## Releases

Releases are manual. After merging reviewed changes, run `just tag v0.2.0`
from clean, up-to-date main, then `just release v0.2.0`. The workflow tests
and packages vendored source, then creates a draft release for you to inspect
and publish. Pushes to main or tags never start it. The COPR build is a separate
manual action after publication.

See the [release guide](tools/release/README.md) for exact steps, artifacts,
retry behavior, and the first COPR/VM handoff.
