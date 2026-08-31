# Nimbus

Nimbus is a Linux workstation installer and system manager.

It owns packages, repositories, services, hardware integration, boot policy,
system configuration, and the explicit bootstrap of a Chezmoi dotfiles
repository. Chezmoi owns selected user configuration and remains usable
without Nimbus.

The first target is post-install Fedora setup. A bootable Fedora image is later
work and must reuse the same profiles and operations.

Nimbus is currently in the planning and repository-foundation stage. No install
or system-management commands are implemented yet.

## Project documents

- [SPEC.md](SPEC.md): accepted scope and boundaries
- [CLI.md](CLI.md): commands and fresh-install and reinstall flows
- [ROADMAP.md](ROADMAP.md): implementation order
- [TASKS.md](TASKS.md): current work
- [OPEN_QUESTIONS.md](OPEN_QUESTIONS.md): decisions not yet accepted
- [AGENTS.md](AGENTS.md): repository working rules

## Fedora package queries

Search Fedora 44 and RPM Fusion packages without changing the host:

```sh
just package-search ripgrep
```

See [tools/package-query/README.md](tools/package-query/README.md) for direct
DNF5 queries.
