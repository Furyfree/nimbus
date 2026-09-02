# Nimbus tasks

## Current phase: Configuration resolver

Plan: [Configuration resolver](ROADMAP.md#1-configuration-resolver).
Open gates: [Q-002 through Q-005](DECISIONS.md#open-questions).

### Foundation and entry points

- [ ] Create the Go module and the Cobra command tree.
- [ ] Implement nimbus validate and nimbus config resolve with explicit
  checkout and machine overrides.
- [ ] Add nimbus.toml with the first definition schema and engine compatibility
  metadata.

### Configuration model

- [ ] Define strict versioned types for the local selector, machine, profile,
  component, resource declaration, catalog exception, manual task, and runtime
  command metadata.
- [ ] Add tracked desktop and laptop machine manifests under machines/.
- [ ] Add the initial common, development, gaming, and hyprland-noctalia
  profiles and only the components needed to exercise their real graph.
- [ ] Add representative package references that exercise bare and explicit DNF,
  direct system Flatpak, and explicit exceptional catalog paths without
  implementing package operations.

### Loading, validation, and resolution

- [ ] Load definitions only from the selected Nimbus checkout.
- [ ] Calculate the canonical definition-tree digest from sorted paths, modes,
  and contents.
- [ ] Reject symlinks, path escape, unsupported schemas, unknown fields,
  duplicate IDs, missing references, component cycles, conflicts, invalid
  package references, invalid exclusions, and duplicate lifecycle ownership.
- [ ] Test catalog non-shadowing, canonical package deduplication, lifecycle
  conflicts, and constraint attachment to canonical provider identities.
- [ ] Resolve profiles, explicit components, component requirements, packages,
  resources, manual tasks, and runtime command groups deterministically.
- [ ] Preserve ordered profile IDs and selection provenance in the resolved
  model for the later Chezmoi handoff and why command.

### Output and evidence

- [ ] Emit stable human output and a versioned JSON envelope from the same
  resolved data.
- [ ] Add valid, invalid, digest, and golden-output fixtures.
- [ ] Prove with tests that validation and resolution execute no external
  command, access no network, invoke no privilege escalation, and write no
  files or state.
- [ ] Run gofmt, go vet ./..., go test ./..., and just check.
- [ ] Add the gomod ecosystem to .github/dependabot.yml when go.mod lands.
- [ ] Inspect the final diff and untracked files and record evidence and
  residual risk below.

## Evidence

- The 2026-09-02 documentation reconciliation establishes one architecture and
  one active document hierarchy.
- No Go implementation exists yet.
- The Fedora 44 package-query tool is development evidence only and does not
  define desired state.

## Blockers and residual risk

- Q-002 through Q-005 in DECISIONS.md must be resolved before the corresponding
  schemas and command contracts are frozen. They block implementation, not
  further documentation work.
- The dotfiles repository still contains obsolete Nimbus machine manifests and
  an older profile handoff contract. Reconcile it before implementing the
  bootstrap phase; it does not block an isolated resolver.
- Exact DNF5 constraint and desktop-session update behavior remains deliberately
  outside this phase.

## Completion rule

Complete the phase only when every checkbox passes, evidence is recorded, and
the resolver contains no system inspection, state write, Git mutation, Chezmoi
invocation, or apply path.

Stop after the completed resolver and request separate authorization before
starting Fedora inspection.
