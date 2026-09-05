# Nimbus tasks

## Current phase: Bootstrap, initialization, and Chezmoi handoff

Plan: [Bootstrap, initialization, and Chezmoi handoff](ROADMAP.md#5-bootstrap-initialization-and-chezmoi-handoff).
Phases 1 to 4 are merged. Phase 4's checklist is in the evidence section.

### Initialization

- [x] Hardware facts: DMI product and board names, the chassis kind from the
  SMBIOS chassis type, and display adapters by PCI vendor and device, read
  from sysfs through the source.
- [x] Typed detection: `[detect]` on the hardware components, `hardware` on
  the tracked machines, `plan.ProposeComponents` and `plan.MatchMachine`
  with laptop, desktop, and VM fixtures.
- [x] `nimbus init`: validates the checkout, reads its origin, asks which
  machine with the matching one pre-selected, runs the new-machine dialog
  for `--new`, writes the manifest and the selector, syncs, hands off to
  Chezmoi once, and reports its configuration and tool installation together.
  A second user-only pass runs only for explicit Nimbus-owned declarations.
- [x] `install.sh` and `bootstrap`: platform and user checks, Git through
  DNF, clone or validate the checkout, the engine from the COPR, init with
  its input on the terminal; shellcheck runs in `just check` and CI.
- [x] Doctor's `chezmoi` check compares Chezmoi's stored machine and
  profiles with the manifest; `profiles add|remove` print the refresh
  command.

### User scope

- [x] `[installer]` on components and the `cargo:` prefix; the `mise`
  component. The tracked development profile now delegates its Cargo tools to
  Chezmoi's native Mise configuration instead of declaring duplicate crates.
- [x] Planned as the user: the maker installer with its digest shown and
  binary verified, no receipts. Chezmoi's after-script invokes Mise for the
  configured runtimes and Cargo tools on each full apply; ordinary sync does
  not install or repair those tools.
- [x] Select Mise and its existing build prerequisites through the common
  profile so a non-development machine can apply the global tool configuration.

### Installer stabilization

- [x] Apply Chezmoi during init, including its user-tool scripts; retain native
  lifecycle commands through `dotfiles diff`, `apply`, and `update`. Script
  failures fail the dotfiles-and-tools stage and remain retryable.
- [x] Show completed, failed, and skipped work with reasons, including partial
  native changes; fail the exit status for required incomplete work.
- [x] Recheck approved selection and definitions, carry selection approval,
  validate new manifests before writing, and hold init's lock through its run.
- [x] Refuse empty EOF approval and unsupported or unknown platforms before
  mutation.
- [x] Preserve native RPM architecture identity and conservative legacy ownership;
  compare install, removal, and upgrade transactions before and after execution.
- [x] Verify actual active repository keys; reconcile DNF trust and refuse unsafe
  existing Flatpak trust changes.
- [x] Document public HTTPS bootstrap as the intended path. GitHub visibility
  has not been changed by these local edits.

### Validation

- [x] Tests: hardware parsing, proposals and matches, the init dialog with
  the picker and prompt hooks, the handoff command, the doctor check, the
  user-scope planner and executor.
- [x] Run `just check`.
- [ ] Fresh-VM drill of the one-liner, owner's action: restore the
  snapshot, run `install.sh` from a served copy of the branch, reach the
  first sync, the Chezmoi handoff, and the user-scope steps.

### Outside this repository

- [x] Read the current dotfiles template and PROFILES.md: the three handoff
  prompts and unvalidated stored profile list are present.
- [x] Validate the populated dotfiles Mise configuration and Linux Cargo
  fragment together. The owner's runtime selections now include Rust; the
  seven Cargo tools moved to `home/dot_config/mise/conf.d/cargo.toml`.
- [ ] Verify initial and refreshed Chezmoi prompts, apply, and user-tool
  installation together in an isolated home and disposable Fedora VM.
- [ ] Publish Nimbus and dotfiles through a separately authorized GitHub
  visibility change after reviewing their content and history.

## Evidence

- Chezmoi-owned installation, 2026-09-05: focused Nimbus tests cover a single
  Chezmoi handoff, native tool failure and retry in init and dotfiles summaries,
  and ordinary sync leaving dotfiles tools alone. The real common definitions
  provide Mise without development. Dotfiles lifecycle fixtures run native
  Chezmoi against a tiny temporary source and fake Mise: configs precede
  installation, unchanged apply repairs a missing tool, errors propagate and
  retry, missing prerequisites and root are refused, Windows renders no action,
  and previews do not install. Real downloads and platform execution remain
  part of the VM and native platform gates.
- Cargo handoff, 2026-09-05: native `mise config ls --json` loads the main
  configuration and all seven Cargo tools from the fragment in an isolated
  home; `mise settings get cargo.binstall` returns false. Isolated Chezmoi
  `managed`, `status`, `diff`, and `verify` pass for the rendered Mise targets;
  platform checks exclude the Cargo fragment on macOS and Windows. Offline
  `mise ls --json` cannot resolve uncached latest versions; downloads and real
  installation remain part of the disposable VM gate.
- Installer stabilization, 2026-09-05: focused tests cover Chezmoi apply
  before Mise and Cargo, skipped dependencies and retry, unrelated Chezmoi
  source or selection refusal, lock continuity, invalid manifests without
  writes, changed definitions during approval, EOF without an answer, and
  platform refusal before metadata refresh. Native package and key tests cover
  multilib ownership, legacy migration, collateral and partial transactions,
  active key replacement, bundled keys, and existing Flatpak trust.
  `just check`, `just validate`, and `gitleaks dir --redact --no-banner .`
  passed. No live installation, VM drill, commit, push, or visibility change
  was performed.
- Local CodeRabbit CLI 0.7.6 reviewed the tracked uncommitted diff once. Its two
  minor findings were addressed: the dotfiles group in the public CLI list and
  accepting an explicit final yes at EOF while refusing empty or whitespace-only
  EOF. New untracked files and subsequent integration changes were reviewed by
  the main agent and covered by focused checks.

- Phase 5 drill, first run, 2026-09-04, on the restored VM with the `vm`
  manifest: `nimbus init --machine vm` wrote the selector, the first sync
  ran, and the handoff reached `chezmoi init` with the three prompt flags;
  the private clone failed on a password typed at git's prompt and init
  reported it and went on, as designed. The second sync then primed sudo
  for a run that held no privileged step, reported "0 operations applied"
  when everything waited for the handoff, and repeated the cargo note once
  per crate. Fixed the same day: sudo is primed only when a system
  operation, a receipt, or the upgrade needs it; a run whose operations all
  wait says so and runs nothing; a note is shown once; a pending user step
  names what it waits for in words. Read back over ssh afterwards: 142
  receipts under one dnf-config, nine repositories, 130 packages, one remote,
  and two Flatpaks; a baseline of 635 packages; DNF history holding exactly
  the release RPM, one install of 901 packages, and one upgrade; the two
  maker repository files disabled through the override; Mise 2026.9.1 in
  `~/.local/bin`; no sudo credential left cached. Two more fixes from that
  reading: doctor called chezmoi absent because only the required commands
  were looked up, and the empty source directory the failed clone left
  behind counted as an initialized Chezmoi for both the handoff and the
  facts. The retry with GitHub authentication is next.
- Phase 5 passthrough, 2026-09-04, main agent only: the executor ran a
  full inspection before every user-scope step and discarded it; the plan
  dropped the user tools silently when their facts were unknown, now one
  blocked operation; the Mise runtimes command named `mise` although
  `~/.local/bin` joins PATH only at the next login, now the full path;
  bootstrap hid the validation output on failure; SPEC denied the sudo
  renewal that sync performs and placed key import in planning; the Phase 5
  sections still called the removed command apply; INSTALLATION.md said
  Nimbus was not implemented; ROADMAP.md named Phase 1 as current.
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
- The Fedora 44 package-query tool is development evidence only and does not
  define desired state.

## Blockers and residual risk

- The scripted tests prove the control flow, not DNF's behavior; the VM
  drills above are the evidence for that, and the sync of D-028 has had one
  full fresh-host run.
- Sync refreshes the user metadata cache with `dnf5 makecache`; root's
  cache is refreshed by `dnf5 install` itself. The two can differ for a
  moment, which surfaces in the differences report, never silently.
- A repository or Flatpak remote whose last selected package leaves keeps
  its receipt and stays enabled at its declared low priority; retiring it
  needs the repository ownership rule Phase 6 defines, since an adopted
  package may still take updates from it.
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
- User-scope steps are implemented through `[installer]` and `cargo:` references.
  Services remain a later phase.
- Definition validation checks armored key-file form; planning checks observed
  active trust and execution verifies downloaded or declared keys before import.
- Development engine builds (`0.0.0-dev`) skip the `min_engine` comparison;
  release builds enforce it.

- The current repository declarations include their key URLs or files and pins.
  Real native reconciliation and Flatpak repair still need disposable-VM drills.
- A maker's installer script cannot be pinned to a version; Nimbus downloads
  it, shows the file's digest, and runs that file.
- Exact DNF5 constraint and desktop-session update behavior remains deliberately
  outside this phase.
- Exact hardware probes belong to Phase 5 and Topgrade orchestration belongs to
  Phase 7; neither may leak system inspection or mutation into Phase 1.

## Completion rule

Complete the phase only when the required installation and validation tasks
pass, the evidence is recorded, and a disposable Fedora VM reaches applied
user configuration and the selected user tools. Native system ownership needs
verified receipts; user tools retain their explicit no-receipt lifecycle.
Public repository visibility is a separate publication action.

Stop before starting the services and remaining system resources phase.
