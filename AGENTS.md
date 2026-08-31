# Repository instructions

## Purpose

Nimbus installs and manages Linux workstation state that does not belong in a
Chezmoi dotfiles repository.

## Working rules

- Read `SPEC.md`, `ROADMAP.md`, `TASKS.md`, and `OPEN_QUESTIONS.md` before
  changing architecture or scope.
- Keep one bounded outcome per change and preserve unrelated work.
- Prefer native system tools and existing Go dependencies over custom
  replacements.
- Keep package-specific data in versioned catalog files, not package-name
  branches in Go.
- Implement inspection and planning before mutation.
- Do not add compatibility or abstractions for hypothetical future backends.
- Do not commit, push, create or update a pull request, install, deploy, or run
  privileged operations without explicit permission.
- Treat `~/git/docs` as history, not a source of truth. It records past
  decisions, superseded plans, and previous implementations such as niriland.
  The active contracts live in this repository and in the dotfiles repository.

## Ownership

- Nimbus owns Linux packages, repositories, services, system files, hardware,
  boot policy, profiles, planning, applied state, and recovery.
- Chezmoi owns user files, templates, and its normal diff, apply, and update
  lifecycle.
- Nimbus owns `machines/` manifests stored outside the Chezmoi source root;
  Chezmoi owns the surrounding checkout and all Git operations.
- Nimbus may install Chezmoi and perform an explicit first bootstrap. It must
  not implement another dotfiles engine or hide Chezmoi operations.

## Repository map

- `profiles/` will own built-in profile composition.
- `catalog/` will own package and resource definitions.
- `history/` holds dated analysis records; it is not active foundation.
- `justfile` exposes development commands.
- `tools/package-query/` is a development-only Fedora and RPM Fusion package
  research image.

## Validation

For documentation-only changes, run:

```sh
git diff --check
```

Once Go code exists, the complete local gate is:

```sh
gofmt -w .
go vet ./...
go test ./...
git diff --check
```

Run destructive integration tests only in disposable environments.

## Safety

- Never store credentials, private keys, tokens, machine secrets, or user
  runtime state in the repository, plans, logs, or Nimbus state.
- Keep desired configuration, observed state, and last-applied state separate.
- Never guess removal commands or delete paths Nimbus does not own.
- Scope privilege elevation to the operation that needs it.
- Use direct `sudo` for each privileged native command. Do not add a root
  daemon, privileged helper, or custom sudo keepalive without a new decision.
- Define verification and recovery before enabling a mutating resource.

## Documentation

- `SPEC.md` owns accepted requirements and boundaries.
- `ROADMAP.md` owns phase order, risks, validation, recovery, and exit criteria.
- `TASKS.md` owns the current checklist and evidence.
- `OPEN_QUESTIONS.md` owns unresolved decisions. Move accepted answers into the
  relevant owning document.
- `README.md` is the short user-facing entry point.

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
