# Nimbus

@AGENTS.md

AGENTS.md owns the contributor rules. This file adds the facts Claude needs to
work here without rediscovering them.

## State

- Docs-only repository as of 2026-09-02. No go.mod, Go code, definitions,
  install.sh, or bootstrap exist yet.
- Current phase: 1, configuration resolver. Scope, gates, and the exit rule
  live in docs/ROADMAP.md and docs/TASKS.md. Phase 1 code executes no external
  command, touches no network, never escalates privilege, and writes no file;
  tests must prove that.
- Branch docs/project-foundation carries the foundation documents and has an
  open draft pull request against main.
- docs/REVIEW-2026-09-02.md holds audit findings and owner decisions not yet
  moved into SPEC, ROADMAP, or TASKS. Check it before editing those files.

## Commands

~~~sh
just check                 # git diff --check HEAD; markdownlint when installed
just package-search QUERY  # dnf5 search against Fedora 44 + RPM Fusion in Docker
~~~

Once Go code exists the gate also runs gofmt, go vet ./..., and go test ./....
The package-search result is research input, never desired state.

## Document ownership

| Change | File |
| --- | --- |
| Accepted behavior, schema, invariant, safety rule | docs/SPEC.md |
| Rationale, rejected alternatives, open question | docs/DECISIONS.md |
| Phase order, gates, risks, exit criteria | docs/ROADMAP.md |
| Current checklist, evidence, blockers | docs/TASKS.md |
| Fedora base install steps | docs/INSTALLATION.md |
| Short entry point | README.md |

Write a behavior once, in SPEC.md, and link to it elsewhere. Superseded drafts
live only in commit c0bb8a4660732e6e9556297da2c15ba0f286ee98; read them there,
never restore them.

## Planned layout

- Definition boundary, hashed into the definition digest: nimbus.toml plus
  every regular file below machines/, profiles/, components/, catalog/, and
  system/. system/root/etc/PATH maps to /etc/PATH and nowhere else.
- Engine: cmd/ and internal/. Entry points: install.sh and bootstrap.
- Runtime state is outside the repository: ~/.config/nimbus/config.toml is the
  selector, /var/lib/nimbus holds receipts. Never create either from tests.

## Hazards

- This checkout is on the owner's live Fedora workstation. Do not run nimbus
  mutation, dnf, chezmoi apply, or sudo here. Destructive tests run only in a
  disposable Fedora VM.
- Schemas are strict and versioned. Unknown fields are errors. Add a field only
  with a fixture that exercises it.
- Bare package names are Fedora packages. catalog/ is an exception list, not a
  registry.
- The SPEC example paths contain the owner's home directory; do not spread that
  pattern.

## Related repositories

- ~/git/dotfiles: Chezmoi source state. Owns everything below $HOME except the
  Nimbus selector. Standalone on every platform; only targets that call Nimbus
  gate on managed_by_nimbus. Handoff keys live in its PROFILES.md.
- ~/git/docs: history and earlier Nimbus designs, not a source of truth.
- ~/git/niriland: reference configuration, never imported wholesale.

## Style

- Markdown wraps at 80 columns, uses ASCII punctuation, and uses ~~~ fences in
  the top-level docs.
- Keep changes proportional: one bounded outcome per change, no speculative
  fields, commands, or abstractions.
