# Nimbus tasks

## Current phase: Controlled DNF apply and receipts

Plan: [Controlled DNF apply and receipts](ROADMAP.md#4-controlled-dnf-apply-and-receipts).
Phases 1 to 3 are merged. The first disposable-VM run belongs to this phase
and is the owner's; its steps are in the evidence below.

### State

- [x] Add `internal/state`: `/var/lib/nimbus` with a schema file, receipts
  per resource, a journal, and the baseline of packages that existed before
  Nimbus first applied anything. The normal user reads it; only the hidden
  `nimbus internal record` action, run through sudo, writes it, and only a
  stage bound to the plan digest with verified receipts.
- [x] Record the baseline with the first receipt of the first apply; a later
  record never replaces it.

### Executor

- [x] Add `internal/apply`: the kernel operation lock under
  `$XDG_RUNTIME_DIR/nimbus/operation.lock`, mode 0700 and 0600, content
  diagnostic only.
- [x] Enable repositories natively: key downloaded or read from the
  checkout, fingerprint checked with `gpg` before any privileged command,
  `install` and `rpm --import`, then `dnf5 config-manager addrepo` for
  Nimbus-owned repositories; a release RPM verified by SHA-256 with its key
  extracted through `rpm2archive`; `dnf5 copr enable`; priorities through
  `dnf5 config-manager setopt`, which writes a DNF override instead of the
  maker's file; a Flatpak remote added from the verified `.flatpakrepo`.
- [x] Install packages with one `dnf5 install`; afterwards the installed
  set is compared with the preview and the differences are reported, not
  refused (D-027).
- [x] Verify every operation by re-inspecting the host; a failed
  verification gets no receipt and stops the run; earlier receipts stay.
- [x] Owned removal of packages with a receipt that the definitions no
  longer select; `sync --prune` removes the third-bucket candidates.

### Commands

- [x] `nimbus sync [-p] [-y] [-n] [-r]`: refreshes metadata, shows the plan,
  asks once, prepares the declared sources, runs the native commands with
  their output visible, upgrades the system, verifies, records receipts, and
  reports differences from the plan. `-p` is the read-only plan.
- [x] `packages install [QUERY]`, `packages remove [QUERY]`,
  `profiles add|remove [ID...]`, `components add|remove [ID...]`: edit the
  manifest in memory, show the diff and plan, require approval, write the
  manifest atomically, apply, and leave the Git change to the user. Without
  IDs the Bubble Tea picker opens; it needs a terminal.
- [x] `unmanaged --all` adds the pre-existing bucket with its marker;
  `managed` distinguishes receipts from adoptable packages.
- [x] Delete `components/fedora-base.toml`; the baseline replaces it and
  resolves Q-018.

### Validation

- [x] State tests: atomic writes, refusal of unbound or unverified stages,
  baseline immutability, schema rejection.
- [x] Executor tests with a scripted host whose state changes as commands
  run: the full happy path with receipts and baseline, stop at the first
  failure with earlier receipts kept, key mismatch before any privileged
  command, verification failure without a receipt, incomplete plan refused,
  owned removal retiring its receipt.
- [x] CLI tests: the one question and its refusal, incomplete plan, stop at
  the first failed operation with the lock held, manifest rendering and
  diff, the selection flows, the picker hook.
- [x] Run `just check`.
- [x] First disposable-VM runs, 2026-09-04: see the evidence section.

### Outside this repository

- [ ] Dotfiles repository, before Phase 5: delete `machines/` and its
  symlink-selector README, add `Machine` and `ManagedByNimbus` prompts to
  `.chezmoi.toml.tmpl`, drop the profile-choice validation so the list is
  stored as sent, deploy every Linux config except Hyprland and Noctalia
  unconditionally, and update PROFILES.md.

## Evidence

- Phase 4 apply, 2026-09-03: `internal/state`, `internal/apply`, the hidden
  `internal record` action, `nimbus sync`, the selection commands, and the
  picker. Probed in the research container: `dnf5 config-manager addrepo
  --id=... --set=...` writes exactly the repository file Nimbus wants, and
  `dnf5 config-manager setopt <repo>.priority=100` writes
  `/etc/dnf/repos.override.d/99-config_manager.repo` instead of touching the
  maker's file, so no Nimbus-owned file-writing action is needed for
  repositories. Facts now read the override directory so effective values
  are compared.
- VM drill, 2026-09-04, on the Fedora 44 VM from INSTALLATION.md with the
  `vm` manifest: five fresh-snapshot runs, each finding conditions the unit
  fixtures had not modelled (rpm2archive writing to stdout, `repo_gpgcheck`
  against DNF's own keyring, Terra's `$releasever` key URL, Fedora carrying
  `unrar`, the 1Password and ChatGPT packages writing repository files,
  `dnf5 replay` recording `@stored_transaction(...)` sources and skipping
  signature checks, and openssl-libs needing an upgrade on a fresh base).
  Each is recorded in DECISIONS.md D-022 through D-027 with its fix. The
  final shape, one `nimbus sync` that shows, asks once, runs, and reports,
  installed 900 packages and 2 Flatpaks and left `sync -p` with nothing to
  do. The next drill after D-027 is the owner's.
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
  Noctalia in Fedora, `golang-github-jesseduffield-lazygit` and `librepods`
  in Terra, Hyprland in no accepted repository; the ChatGPT RPM was
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
  keeps the upgrade step within one release, and limits Nimbus to compatibility
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

- The scripted tests prove the control flow, not DNF's behavior; the VM
  drills above are the evidence for that, and the sync of D-028 has had one
  full fresh-host run.
- Sync refreshes the user metadata cache with `dnf5 makecache`; root's
  cache is refreshed by `dnf5 install` itself. The two can differ for a
  moment, which surfaces in the differences report, never silently.
- The picker is untested interactively; its model logic is small and the
  commands accept explicit IDs without it.

- The hosted Codex review of pull request 7 found eight problems on
  2026-09-03; seven are fixed in the third commit (pending operations so a
  fresh host gets a complete plan, removal dedup, repository and remote
  verification, adoption source checks, a digest bound to the definition
  digest), and the eighth, `plan --refresh` as network access inside a
  planning path, in the fourth: the flag is gone; sync now refreshes as its
  first step, since it is the command that acts.
- A non-root plan reads DNF's metadata through the system cache when it is
  fresh and otherwise its user cache; sync runs DNF as root and re-resolves,
  so a difference surfaces in the differences report rather than silently.

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

Complete the phase only when every checkbox passes, evidence is recorded, a
representative DNF component and the selection commands complete and reverse
safely in the disposable VM, and every mutation runs through the reviewed
plan with a receipt.

Stop after the VM run is recorded and request separate authorization before
starting bootstrap and initialization.
