# Nimbus tasks

## Current phase: Planning and DNF

Plan: [Planning and DNF](ROADMAP.md#3-planning-and-dnf). Phases 1 and 2 are
merged. No gate is open; the dotfiles template task below gates Phase 5, and
the first VM run is deferred to Phase 4.

### Planner

- [x] Add `internal/plan` with typed operations (repository, package,
  Flatpak remote, Flatpak) carrying action, risk class, selection paths,
  exact steps, and the parsed DNF transaction.
- [x] Parse the DNF5 `--assumeno` preview table and `check-upgrade` output
  from fixtures recorded in the research container; reject an unknown
  section rather than accept a new DNF behavior silently.
- [x] Plan from the local metadata cache only; `plan --refresh` runs
  `dnf5 makecache` first as the one opt-in network step, and missing cache
  is reported with that command rather than guessed.
- [x] Recognize declared repositories on the host: `nimbus-<id>` for the
  files Nimbus writes, the maker's own IDs for release packages, the COPR
  ID DNF creates, and the Flatpak remote by name; a foreign file that
  already provides a repository blocks rather than duplicates it.
- [x] Render the exact enabling steps per kind: key fetched and fingerprint
  checked before import, `nimbus-<id>.repo` written by Nimbus, a release
  RPM verified by SHA-256 with its key extracted through `rpm2archive` and
  imported before installation, a COPR enabled through DNF and its key
  checked afterwards.
- [x] Adopt installed desired packages, install the rest through one DNF
  transaction, plan declared removals as a separate transaction, and hold
  packages whose repository is not enabled yet as blocked operations.
- [x] Refuse a transaction that goes beyond the definitions: an undeclared
  install, an undeclared removal, a package from the wrong repository, or a
  smuggled upgrade.
- [x] List prune candidates and update information as separate informational
  sections; hash only the apply section into the plan digest.
- [x] Add the `fedora-base` component, generated from Fedora's `core` and
  `standard` comps groups plus the packages Anaconda installs outside comps,
  and select it from `common` so the base is never a prune candidate.

### Commands

- [x] `nimbus plan` and `nimbus status` with `--checkout`, `--machine`, and
  `--json`; an incomplete plan exits 1.
- [x] `nimbus managed`, `nimbus unmanaged`, `nimbus why RESOURCE`,
  `nimbus profiles list`, `nimbus components list`, and
  `nimbus packages installed [QUERY]` as non-interactive lists; the picker
  arrives with Phase 4.

### Validation

- [x] Parser tests against the recorded previews, including protected
  packages, no match, nothing to do, and an unknown section.
- [x] Planner tests against the tracked desktop machine and the Fedora 44
  host fixture: repository detection, adoption, the blocked Docker packages,
  the four refusals, dependency acceptance, removals and an undeclared extra
  removal, digest stability, and unavailable update information.
- [x] CLI tests for plan, status, the views, why, and the selection lists.
- [x] Run `just check`.

### Outside this repository

- [ ] Dotfiles repository, before Phase 5: delete `machines/` and its
  symlink-selector README, add `Machine` and `ManagedByNimbus` prompts to
  `.chezmoi.toml.tmpl`, drop the profile-choice validation so the list is
  stored as sent, deploy every Linux config except Hyprland and Noctalia
  unconditionally, and update PROFILES.md.

## Evidence

- Phase 3 planner, 2026-09-03: `internal/plan` with `nimbus plan`, `status`,
  `managed`, `unmanaged`, `why`, `profiles list`, `components list`, and
  `packages installed`. DNF5's `--store` was probed and found to download
  packages as well as record the transaction, so it is the Phase 4 apply
  mechanism (download, then `dnf5 replay`), not a preview; plan parses the
  `--assumeno` table from the cache. RPM Fusion's release RPM is signed by
  the key it ships and no public key URL exists, so the plan extracts the key
  from the SHA-256-verified RPM with `rpm2archive` and `tar`, both in base
  Fedora. Terra stays a `baseurl` repository because its own installer uses
  that URL.

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

- Q-018 asks whether the base should be read live from DNF history or the
  installed comps groups instead of listed; it is decided after the Phase 4 VM
  run. Until then the `fedora-base` component declares what the installer
  leaves behind, so the base is desired rather than a prune candidate. Its
  comps half is generated evidence; its Anaconda half (kernel, firmware,
  bootloader, release packages) is the known set and is completed from the
  real prune output in the first VM run. Prune stays informational until Phase
  4 adds protected-package and dependency eligibility.
- A non-root `plan` reads DNF's metadata through the system cache when it is
  fresh and otherwise its user cache; apply runs DNF as root and re-resolves,
  so a difference surfaces as a refused digest rather than a silent change.

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

Complete the phase only when every checkbox passes, evidence is recorded, the
plan explains every selected package without executing a mutating command,
and no planner writes, invokes sudo, or uses the network except the explicit
`plan --refresh` metadata step.

Stop after the completed planner and request separate authorization before
starting controlled apply.
