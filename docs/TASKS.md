# Nimbus tasks

## Current phase: Configuration resolver

Plan: [Configuration resolver](ROADMAP.md#1-configuration-resolver).
Open gates: none.

### Foundation and entry points

- [ ] Create the Go module and the Cobra command tree.
- [ ] Implement nimbus validate with an explicit checkout override and nimbus
  version without selector, checkout, or system dependencies.
- [ ] Keep machine resolution internal and expose no future or mutating command
  stubs in root help.
- [ ] Add nimbus.toml with the first definition schema, supported Fedora
  releases, and minimum-engine compatibility metadata.

### Configuration model

- [ ] Define strict versioned types for the local selector, machine, profile,
  component, resource declaration, catalog exception, manual task, and runtime
  command metadata.
- [ ] Represent generic system files only through sources below
  `system/root/etc`, with their absolute `/etc` targets derived rather than
  independently configurable.
- [ ] Parse the selector's required normalized origin and compare it with local
  Git configuration without command execution or network access.
- [ ] Add tracked desktop and laptop machine manifests under machines/.
- [ ] Add the initial common, development, gaming, and hyprland-noctalia
  profiles and only the components needed to exercise their real graph.
- [ ] Add representative package references that exercise bare and explicit DNF,
  direct system Flatpak, and explicit exceptional catalog paths without
  implementing package operations.

### Loading, validation, and resolution

- [ ] Load definitions only from the selected Nimbus checkout.
- [ ] Calculate the versioned SHA-256 definition digest from `nimbus.toml` and
  every regular file below the five definition directories using byte-sorted
  relative paths, exact contents, and `100644` or `100755` mode.
- [ ] Resolve a symlinked checkout root, then reject internal symlinks, special
  files, path escape, unsupported schemas, unknown fields, duplicate IDs,
  missing references, component cycles, conflicts, invalid package references,
  invalid exclusions, and duplicate lifecycle ownership.
- [ ] Test catalog non-shadowing, canonical package deduplication, lifecycle
  conflicts, and constraint attachment to canonical provider identities.
- [ ] Resolve profiles, explicit components, component requirements, packages,
  resources, manual tasks, and runtime command groups deterministically.
- [ ] Preserve ordered profile IDs and selection provenance in the resolved
  model for the later Chezmoi handoff and why command.

### Output and evidence

- [ ] Emit stable human and versioned JSON validation output from the same
  result, plus stable human and JSON version output.
- [ ] Add valid, invalid, digest, and golden-output fixtures.
- [ ] Prove that unrelated checkout files and non-executable permission changes
  do not alter the digest while definition, content, path, or executable changes
  do.
- [ ] Test equivalent SSH and HTTPS origins plus missing, invalid, and
  mismatched selector origins.
- [ ] Test valid `/etc` system-file mappings and reject empty, traversing,
  symlinked, special-file, independently targeted, and non-`/etc` cases.
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
- Q-002 establishes the selector as a regular Nimbus-owned local file that
  records the reviewed checkout origin and selects a tracked machine manifest.
- Q-003 fixes the complete definition-digest boundary, executable-mode
  normalization, and safe support for a symlinked checkout root.
- Q-004 restricts the generic system-file provider to deterministic
  `system/root/etc` to `/etc` mappings; other roots require native packages or
  separately specified typed resources.
- Q-005 fixes the task-oriented CLI, keeps raw resolution and facts internal,
  separates apply, pruning, upgrades, and package workflows, and later adds
  typed post-install, Windows, browser, and webapp commands without arbitrary
  script execution.
- The system-file drift amendment assigns `nimbus files accept /etc/PATH` to
  Phase 6 as a single proven-owned live-to-checkout content operation. Phase 1
  models its source mapping but implements no reverse capture or mutation.
- Q-006 selects the raw `install.sh` one-liner, a minimal Git/bootstrap handoff,
  a versioned checkout script with `/dev/tty`, direct DNF engine ownership, and
  user-owned checkout updates.
- Q-007 assigns the development-profile Mise action to Chezmoi, keeps runtime
  installation with Mise under `MISE_SYSTEM_DEPS=warn`, and limits Nimbus to
  system dependencies plus missing-runtime reporting.
- Q-008 permanently delegates Fedora release upgrades to native DNF5 tooling,
  keeps `nimbus upgrade` within one release, and limits Nimbus to compatibility
  reporting plus post-upgrade drift inspection.
- Q-009 fixes the supported recovery topology, three-point retention, 20 GiB
  low-space refusal, manual restore drill, and user runtime operation lock.
- Q-010 selects system libvirt for one Windows guest, fixes its persistent data
  root and preservation boundary, and defines its runtime and purge lifecycle.
- No Go implementation exists yet.
- The Fedora 44 package-query tool is development evidence only and does not
  define desired state.

## Blockers and residual risk

- No unresolved decision blocks the configuration-resolver implementation.
- The dotfiles repository still contains obsolete Nimbus machine manifests and
  an older profile handoff contract. Reconcile it before implementing the
  bootstrap phase; it does not block an isolated resolver.
- Exact DNF5 constraint and desktop-session update behavior remains deliberately
  outside this phase.

## Completion rule

Complete the phase only when every checkbox passes, evidence is recorded, root
help exposes only validate and version, and the resolver contains no system
inspection, state write, Git mutation, Chezmoi invocation, or apply path.

Stop after the completed resolver and request separate authorization before
starting Fedora inspection.
