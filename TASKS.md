# Nimbus tasks

## Current phase: Configuration resolver

Plan: [Configuration resolver](ROADMAP.md#1-configuration-resolver).

- [ ] Create the Go module and Cobra-based `nimbus config resolve` command.
- [ ] Define versioned machine, profile, component, and catalog types.
- [ ] Add the initial four profiles and their component references.
- [ ] Define the first package-specific catalog entries separately from
  profiles: a few seed entries per source; the full profile composition is
  filled in later.
- [ ] Embed and load built-in profile, component, and catalog TOML.
- [ ] Load `machine.toml`, including a symlinked manifest, with `--config FILE`
  as an override.
- [ ] Resolve imports and ordered profile IDs deterministically.
- [ ] Reject cycles, conflicts, duplicates, and missing references.
- [ ] Emit stable human and JSON output with source-aware errors.
- [ ] Add a standalone read-only `nimbus validate` reusing the resolver loader.
- [ ] Add `nimbus why` provenance output over the resolved desired graph.
- [ ] Add valid, invalid, and golden fixtures.
- [ ] Run `gofmt`, `go vet ./...`, `go test ./...`, and `git diff --check`.
- [ ] Add the `gomod` ecosystem to `.github/dependabot.yml` when `go.mod`
  lands.
- [ ] Inspect the final diff and record residual risk below.

## Evidence

- Repository planning foundation exists; implementation has not started.
- The Fedora 44 package-query image builds and returns DNF5 results from Fedora,
  RPM Fusion Free, and RPM Fusion Nonfree.
- The 2026-08-31 cross-repo review decisions are folded into SPEC, CLI, and
  ROADMAP; the desktop-session group analysis remains open in
  [OPEN_QUESTIONS.md](OPEN_QUESTIONS.md).

## Blockers and residual risk

- None currently known for Phase 1.

## Completion rule

Complete the phase only when every task passes, evidence is recorded, and the
resolver has no system-inspection or mutation path.
