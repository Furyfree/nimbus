# Nimbus tasks

## Current phase: Fedora system facts

Plan: [Fedora system facts](ROADMAP.md#2-fedora-system-facts). Phase 1 is
merged. No gate is open; the dotfiles template task below gates Phase 5, and
the first VM run is deferred to Phase 4.

### Inspector

- [x] Add `internal/facts` with a `Source` that abstracts every command,
  file, directory, and PATH read, an `ExecSource` for the host, and a
  `FakeSource` that fails on anything not recorded.
- [x] Record Fedora 44 output in the research container as fixtures:
  os-release, the DNF5 installed-package query, and every repository file.
- [x] Collect only the facts later phases need: platform, installed RPMs
  with source repository and install reason, repository files, Flatpak
  remotes and apps, Secure Boot, SELinux, firewalld, checkout origin,
  commit, and dirty state, and required-command presence.
- [x] Report a fact that cannot be established as unknown with its reason;
  never substitute a default.
- [x] Keep parsing separate from policy: parsers in `facts`, checks in
  `doctor`.

### Doctor

- [x] Add `nimbus doctor` with `--checkout` and `--json`: platform and
  supported release, selector and origin, definitions, required commands,
  package database, repository signature checking, Secure Boot, SELinux,
  and firewalld.
- [x] Explain every failure with observation, impact, and remediation; no
  explain mode; exit 1 on any failure and 0 when only unknowns remain.
- [x] Never repair, invoke sudo, or use the network.

### Validation

- [x] Fake-runner tests for every fact family, the unknown paths, and the
  malformed-output rejections.
- [x] Doctor tests for the healthy host, every failure, unknowns, and the
  override.
- [x] CLI tests for doctor against the repository checkout in human and
  JSON form and with a broken checkout.
- [x] Run `just check`.
- Deferred to the start of Phase 4: the first disposable Fedora 44 VM run
  compares doctor and plan output with the fixtures before any apply.

### Outside this repository

- [ ] Dotfiles repository, before Phase 5: delete `machines/` and its
  symlink-selector README, add `Machine` and `ManagedByNimbus` prompts to
  `.chezmoi.toml.tmpl`, drop the profile-choice validation so the list is
  stored as sent, deploy every Linux config except Hyprland and Noctalia
  unconditionally, and update PROFILES.md.

## Evidence

- Phase 2 inspector, 2026-09-03: `internal/facts` and `internal/doctor` with
  `nimbus doctor`. Fixtures were recorded from the Fedora 44 research
  container; DNF5 prints `\t` in a query format literally, so the query uses
  `|` separators, and a container's `from_repo` is a build hash rather than
  a repository ID. Doctor runs nine checks. Tests run only against recorded
  output; nothing in the suites reads the host.

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
help exposes only doctor, validate, and version, every host read goes through
`facts.Source`, and no inspector writes, invokes sudo, or uses the network.

Stop after the completed inspector and request separate authorization before
starting planning.
