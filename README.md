# Nimbus

Nimbus is a personal, opinionated Fedora workstation installer and system
manager written in Go.

It turns an installed Fedora system into the workstation defined by this
repository, detects drift in the resources it owns, shows a complete plan, and
applies only reviewed operations. Chezmoi remains the separate owner of user
configuration below the home directory.

The first target is Fedora 44 on x86_64. Nimbus does not initially install the
operating system, repartition disks, or configure full-disk encryption.

Nimbus is at Phase 5 of its roadmap: bootstrap, `nimbus init`, and the
Chezmoi handoff.

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
`just vm-push` copies that binary and the definitions to the drill VM.

The engine RPM channel and reviewed signing-key pin remain a release blocker.
Bootstrap is prepared to verify and install the COPR RPM, but refuses a fresh
installation until the real public key and its reviewed fingerprint are shipped.

The delivered commands are `init`, `sync`, `validate`, `doctor`, `status`,
`managed`, `unmanaged`, `why`, the `profiles`, `components`, and `packages`
groups, and `version`. `sync` is the one command that changes the system: it
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

Releases are manual. After merging reviewed changes, run `just tag v0.1.0`
from clean, up-to-date main, then `just release v0.1.0`. The workflow tests
and packages vendored source, then creates a draft release for you to inspect
and publish. Pushes to main or tags never start it. The COPR build is a separate
manual action after publication.

See the [release guide](tools/release/README.md) for exact steps, artifacts,
retry behavior, and the first COPR/VM handoff.
