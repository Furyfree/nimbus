# Repository instructions

## Purpose

Nimbus installs and manages Linux workstation state that does not belong in a
Chezmoi dotfiles repository. The first target is post-install Fedora 44 setup
on x86_64.

## Working rules

- Read `SPEC.md`, `ROADMAP.md`, `TASKS.md`, and `OPEN_QUESTIONS.md` before
  changing architecture or scope.
- Keep one bounded outcome per change and preserve unrelated work.
- Prefer native system tools and existing Go dependencies over custom
  replacements. Do not add abstractions for hypothetical future backends.
- Keep package-specific data in versioned catalog files, not package-name
  branches in Go.
- Implement inspection and planning before mutation.
- Do not commit, push, create or update a pull request, install, deploy, or run
  privileged operations without explicit permission.
- Treat `~/git/docs` as history, not a source of truth: past decisions,
  superseded plans, and previous implementations such as niriland. The active
  contracts live in this repository and in the dotfiles repository.

## Ownership

- Nimbus owns Linux packages, repositories, services, system files, hardware,
  boot policy, profiles, planning, applied state, and recovery.
- Chezmoi owns user files, templates, and its diff, apply, and update
  lifecycle. Nimbus may install and bootstrap Chezmoi explicitly; it must not
  reimplement or hide Chezmoi.
- Nimbus owns `machines/` manifests outside the Chezmoi source state; Chezmoi
  owns the checkout and all Git operations.

## Repository map

- `profiles/`, `components/`, and `catalog/` will own embedded definitions.
- `history/` holds dated analysis records, not active foundation.
- `justfile` exposes development commands; `just check` runs the local gate.
- `tools/package-query/` is a development-only Fedora and RPM Fusion image.

## Validation

`just check` runs the available local checks: `git diff --check` today, plus
`gofmt -w .`, `go vet ./...`, and `go test ./...` once Go code exists. Run
destructive integration tests only in disposable environments.

## Safety

- Never store credentials, private keys, tokens, machine secrets, or user
  runtime state in the repository, plans, logs, or Nimbus state.
- Keep desired configuration, observed state, and last-applied state separate.
- Never guess removal commands or delete paths Nimbus does not own.
- Scope privilege elevation to the operation that needs it: direct `sudo` per
  command, no root daemon, no privileged helper, no sudo keepalive.
- Define verification and recovery before enabling a mutating resource.

## Documentation

- `SPEC.md`: accepted requirements and boundaries.
- `ROADMAP.md`: phase order, risks, validation, recovery, exit criteria.
- `TASKS.md`: current checklist and evidence.
- `OPEN_QUESTIONS.md`: unresolved decisions; move accepted answers into the
  owning document.
- `README.md`: short user-facing entry point.

## Code review rules

- Treat commands, paths, permissions, ownership, and recovery guidance as
  behavior that must match the implementation.
- Reject mutations that are absent from the reviewed plan or lack ownership,
  verification, and recovery.
- Errors gain context as they propagate and render once at the command
  boundary; never duplicate or swallow an error along the way.
- Every mutating resource records complete receipt provenance: Nimbus version,
  embedded-definition digest, exact lifecycle, and verification result.
- Report `.rpmnew` and `.rpmsave` files next to Nimbus-owned configuration;
  never merge or delete them automatically.
