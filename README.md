# Nimbus

Nimbus is a personal, opinionated Fedora workstation installer and system
manager written in Go.

It turns an installed Fedora system into the workstation defined by this
repository, detects drift in the resources it owns, shows a complete plan, and
applies only reviewed operations. Chezmoi remains the separate owner of user
configuration below the home directory.

The first target is Fedora 44 on x86_64. Nimbus does not initially install the
operating system, repartition disks, or configure full-disk encryption.

Nimbus is currently in the planning and repository-foundation stage. No system
management commands are implemented.

## Repository model

This repository owns both the Go engine and the personal system definitions:

~~~text
machines/       selected workstation compositions
profiles/       user-facing system bundles
components/     reusable system capabilities
catalog/        package definitions with non-default lifecycle
system/         Nimbus-owned system files and migrations
cmd/, internal/ Go implementation
~~~

An installed Nimbus engine reads definitions from an explicitly selected
checkout of this repository. Receipts identify both the engine version and the
exact definition commit and tree digest.

Under the accepted model, the separate dotfiles repository contains Chezmoi
source state only. Nimbus may perform the explicit first Chezmoi initialization,
but normal diff, apply, edit, and update operations remain direct Chezmoi
commands. A follow-up change in that repository still needs to remove its old
Nimbus machine manifests and align the handoff contract.

## Project documents

- [SPEC.md](docs/SPEC.md) defines the complete accepted system contract.
- [DECISIONS.md](docs/DECISIONS.md) records decision rationale and unresolved
  questions.
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

Today it runs git diff --check. It will also run formatting, vetting, and tests
after Go code exists.
