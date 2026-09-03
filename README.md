# Nimbus

Nimbus is a personal, opinionated Fedora workstation installer and system
manager written in Go.

It turns an installed Fedora system into the workstation defined by this
repository, detects drift in the resources it owns, shows a complete plan, and
applies only reviewed operations. Chezmoi remains the separate owner of user
configuration below the home directory.

The first target is Fedora 44 on x86_64. Nimbus does not initially install the
operating system, repartition disks, or configure full-disk encryption.

Nimbus is at Phase 3 of its roadmap: read-only planning. No command changes
the system yet.

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
selected profiles; normal diff, apply, edit, and update operations remain
direct Chezmoi commands.

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

The local gate is:

~~~sh
just check
~~~

It runs `gofmt`, `go vet`, `go test`, `git diff --check`, and markdownlint.
`just validate` runs `nimbus validate` against this checkout.

The delivered commands are `validate`, `doctor`, `plan`, `status`, `managed`,
`unmanaged`, `why`, `profiles list`, `components list`, `packages installed`,
and `version`. All of them are read-only: `validate` checks the definitions,
`doctor` inspects the host, and `plan` shows every operation apply would run
without running any. The one exception is `plan --refresh`, which runs
`dnf5 makecache` to refresh DNF's metadata cache before planning. Nothing
mutates the managed system yet.
