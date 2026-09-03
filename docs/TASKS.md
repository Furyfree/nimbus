# Nimbus tasks

## Current phase: Configuration resolver

Plan: [Configuration resolver](ROADMAP.md#1-configuration-resolver).
No Phase 1 gate is open. Q-015 is resolved but needs a dotfiles template
change before the Phase 5 Chezmoi handoff.

### Foundation and entry points

- [x] Resolve Q-013: explicit `common`, ordered profiles, canonical lists,
  and only the fields current definitions use.
- [x] Resolve Q-014: repositories declared once in nimbus.toml and named by
  package prefix; the catalog directory is gone.
- [x] Resolve Q-016: PACKAGES.md lists what Nimbus installs, including the
  user-scope steps Nimbus runs as the user.
- [x] Resolve Q-017: fix the six PACKAGES.md source conflicts and accept or
  reject each listed omission.
- [x] Replace the source tiers with the fixed five-step order in SECURITY.md
  and reconcile PACKAGES.md with it (D-015).
- [x] Apply the 2026-09-03 passthrough corrections (D-019).
- [x] Create the Go module and the Cobra command tree.
- [x] Implement nimbus validate with an explicit checkout override and nimbus
  version without selector, checkout, or system dependencies.
- [x] Keep machine resolution internal and expose no future or mutating command
  stubs in root help.
- [x] Add nimbus.toml with the first definition schema, supported Fedora
  releases, minimum-engine compatibility metadata, and the declared
  repositories with key URLs and pinned fingerprints.
- [x] Reject `package_constraints` as an unknown field in Phase 1.

### Configuration model

- [x] Define strict versioned types for the local selector, machine, profile,
  component, repository declaration, and only the fields retained by Q-013.
- [x] Represent generic system files only through sources below
  `system/root/etc`, with their absolute `/etc` targets derived rather than
  independently configurable.
- [x] Parse the selector's required normalized origin and compare it with local
  Git configuration without command execution or network access.
- [x] Add tracked desktop and laptop machine manifests under machines/, with
  the desktop selecting gaming and the laptop selecting laptop-gaming.
- [x] Add the initial common, development, gaming, laptop-gaming,
  hyprland-noctalia, virtualization, and windows-vm profiles and only the
  components needed to exercise their real graph.
- [x] Add representative package references that exercise bare and explicit DNF,
  system Flatpak, declared-repository prefixes, and a component `removes`
  entry without implementing package operations.

### Loading, validation, and resolution

- [x] Load definitions only from the selected Nimbus checkout.
- [x] Calculate the versioned SHA-256 definition digest from `nimbus.toml` and
  every regular file below the four definition directories using byte-sorted
  relative paths, exact contents, and `100644` or `100755` mode.
- [x] Resolve a symlinked checkout root, then reject internal symlinks, special
  files, path escape, unsupported schemas, unknown fields, duplicate IDs,
  missing references, component cycles, conflicts, invalid package references,
  invalid exclusions, and duplicate lifecycle ownership.
- [x] Test undeclared-prefix rejection, missing `common`, canonical package
  deduplication, and prefix conflicts. Constraint attachment waits with the
  constraint field for Phase 7.
- [x] Resolve profiles, explicit components, component requirements, packages,
  and every declaration retained by Q-013 deterministically.
- [x] Preserve ordered profile IDs and selection provenance in the resolved
  model for the later Chezmoi handoff and why command.
- [ ] Dotfiles repository, before Phase 5: delete `machines/` and its
  symlink-selector README, add `Machine` and `ManagedByNimbus` prompts to
  `.chezmoi.toml.tmpl`, drop the profile-choice validation so the list is
  stored as sent, deploy every Linux config except Hyprland and Noctalia
  unconditionally, and update PROFILES.md.

### Output and evidence

- [x] Emit stable human and versioned JSON validation output from the same
  result, plus stable human and JSON version output.
- [x] Add valid, invalid, and digest fixtures; the CLI tests check the
  human and JSON output shape against the tracked definitions rather than a
  golden file.
- [x] Prove that unrelated checkout files and non-executable permission changes
  do not alter the digest while definition, content, path, or executable changes
  do.
- [x] Test equivalent SSH and HTTPS origins plus missing, invalid, and
  mismatched selector origins.
- [x] Test valid `/etc` system-file mappings and reject empty, traversing,
  symlinked, special-file, independently targeted, and non-`/etc` cases.
- [x] Prove with tests that validation and resolution execute no external
  command, access no network, invoke no privilege escalation, and write no
  files or state.
- [x] Run gofmt, go vet ./..., go test ./..., and just check.
- [x] Add the gomod ecosystem to .github/dependabot.yml and a GitHub Actions
  workflow that runs `just check` when go.mod lands.
- [ ] Inspect the final diff and untracked files and record evidence and
  residual risk below.

## Evidence

- Phase 1 scaffold, 2026-09-03: `go.mod` at Go 1.27 with Cobra and go-toml
  v2; `cmd/nimbus`, `internal/cli` (`validate`, `version`, `--json`),
  `internal/definitions` (strict loader, validator, resolver, digest),
  `internal/selector` (origin normalization and `.git/config` reader), and
  `internal/version`. The tracked `nimbus.toml` declares ten repositories
  with fingerprints computed from the makers' published keys in the research
  container; OpenAI's key is stored as `system/keys/chatgpt.asc` because no
  key URL exists. Two machines, seven profiles, and fourteen components
  resolve: desktop 160 packages, laptop 144. `just check` passes with
  `gofmt`, `go vet`, `go test`, `git diff --check`, and markdownlint; the
  test suites cover the invalid-tree cases, digest boundary and executable
  normalization, symlinked root, origin equivalence and mismatch, the
  worktree pointer, include rejection, a read-only tree, and CLI exit codes.
  Root help lists only `validate` and `version`.

- The 2026-09-03 pre-implementation review identified the concrete schema,
  repository trust representation, and package inventory as unresolved Phase 1
  inputs. The same day D-015 through D-018 resolved them: a fixed source
  order, repositories in nimbus.toml with prefix references, Nimbus running
  the user-scope installs as the user, and Topgrade with a Chezmoi-owned
  configuration. Fedora 44 package-query evidence confirmed Tailscale,
  Noctalia, and `golang-github-jesseduffield-lazygit` in Fedora, Hyprland in
  no accepted repository, and `librepods` in Terra; the ChatGPT RPM was
  inspected and registers OpenAI's DNF repository, and Brave's repository
  carries `brave-origin`.
- The dotfiles template currently prompts only for `onePasswordSsh` and
  `Profiles` and fails on any profile outside its four choices, and the
  repository still holds `machines/desktop.toml`, `machines/laptop.toml`, and
  a README describing the old symlink selector; the handoff cannot succeed
  until the template task above is done.
- The 2026-09-03 passthrough ran three passes: the main agent, Codex
  GPT-5.6 Sol at high reasoning (24 findings), and GLM 5.3 Flash at max
  reasoning through opencode (15 findings). Grok did not run because its CLI
  was not authenticated. Two findings were disproved by test in a scratch
  home: Chezmoi's `--promptMultichoice` splits on slashes, not commas, and it
  accepts flag-supplied values outside the template's choice list. Verified
  the same way: `mise settings set` writes `~/.config/mise/config.toml`,
  Topgrade 17.9 exposes over 150 steps to `--only` and `--disable`, and
  `mise implode` and `zed --uninstall` exist. D-019 records the accepted
  corrections; the worktree was unchanged by every pass.
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
- Q-007, as amended by D-017, has Nimbus run the Mise installer before the
  handoff and `mise install` under `MISE_SYSTEM_DEPS=warn` plus the Cargo
  steps after it, all as the normal user.
- Q-008 permanently delegates Fedora release upgrades to native DNF5 tooling,
  keeps `nimbus upgrade` within one release, and limits Nimbus to compatibility
  reporting plus post-upgrade drift inspection.
- Q-009 fixes the supported recovery topology, three-point retention, 20 GiB
  low-space refusal, manual restore drill, and user runtime operation lock. On
  2026-09-02 the snapshot mechanism became Snapper; the requirements stand.
- D-012 keeps hardware out of profiles and static resolution. A later
  `nimbus init` inspection proposes explicit components from DMI and PCI facts;
  the Phase 1 desktop and laptop fixtures select those components directly.
- D-013, as amended by D-018, keeps exact system updates in Nimbus and runs
  Topgrade with the user's Chezmoi-owned configuration and a command-line
  `--only` allowlist. Topgrade dry-run shows command invocations rather than
  resolved downstream versions, and the recovery topology excludes those
  home-directory mutations.
- Current upstream installation evidence accepts Mise's recommended user-scope
  installer and self-update setting. Tailscale comes from Fedora 44, which
  carries it. Nix remains Fedora-owned because the upstream multi-user
  installer documents disabled SELinux as a Linux prerequisite.
- D-014 records the verified Fedora 44 sources for Hyprland, the stable
  Noctalia greeter, Yazi, gaming helpers, WireGuard, FreeRDP, Typst, Tinymist,
  and Go tooling. WoWUp and Yazi SVG preview remain blocked on accepted package
  sources rather than introducing AppImages or untracked binaries.
- Q-010 defined one Windows guest with a fixed data root, preservation
  boundary, and purge lifecycle. On 2026-09-02 the backend became the
  `dockurr/windows` container below `/var/lib/nimbus/windows/`. Nimbus provides
  no backup for the excluded guest disk; loss and reconstruction from external
  sources is accepted.
- On 2026-09-02 the Chezmoi handoff gained `managed_by_nimbus`, the `windows-vm`
  profile joined the vocabulary, and the dotfiles repository was reconciled to
  the handoff (D-007), removing its machine manifests and symlink selector.
- No Go implementation exists yet.
- The Fedora 44 package-query tool is development evidence only and does not
  define desired state.

## Blockers and residual risk

- The declared repository URLs for 1Password, Brave, VSCodium, and Terra are
  taken from the makers' documentation and the container's repository files;
  they are exercised only when Phase 3 planning reads them.
- User-scope steps (Mise, Cargo, `mise install`) and services are not yet
  schema fields, so PACKAGES.md still lists what the definitions cannot yet
  express.
- A `key_file` is checked only for its armored public-key form; Phase 3
  verifies the fingerprint when the key is imported through the command
  runner.
- Development engine builds (`0.0.0-dev`) skip the `min_engine` comparison;
  release builds enforce it.

- The dotfiles template change for Q-015 is outside this repository and
  blocks only the Phase 5 handoff.
- Key URLs and fingerprints for RPM Fusion, 1Password, Brave, VSCodium, the
  Hyprland COPR, Flathub, and the owner's COPRs must be recorded in
  nimbus.toml from the makers' published keys before those repositories are
  used; Docker's, OpenAI's, and Terra's are already recorded.
- A maker's installer script cannot be pinned to a version; Nimbus downloads
  it, shows the file's digest, and runs that file.
- Exact DNF5 constraint and desktop-session update behavior remains deliberately
  outside this phase.
- Exact hardware probes belong to Phase 5 and Topgrade orchestration belongs to
  Phase 7; neither may leak system inspection or mutation into Phase 1.

## Completion rule

Complete the phase only when every checkbox passes, evidence is recorded, root
help exposes only validate and version, and the resolver contains no system
inspection, state write, Git mutation, Chezmoi invocation, or apply path.

Stop after the completed resolver and request separate authorization before
starting Fedora inspection.
