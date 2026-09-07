# Nimbus tasks

## Current phase: Bootstrap, initialization, and Chezmoi handoff

Plan: [Bootstrap, initialization, and Chezmoi handoff](ROADMAP.md#5-bootstrap-initialization-and-chezmoi-handoff).
Phases 1 to 4 are merged. Phase 4's checklist is in the evidence section.

### Integration readiness

- [x] Consolidate local branch histories into `integrate/phase5`, based on
  current main, in Nimbus and dotfiles. Preserve the reviewed working trees
  byte for byte while resolving the Phase 5/Fastmail merge conflicts.
- [x] Remove obsolete local branch names and eight clean dotfiles worktrees.
  Preserve the unfinished Ghostty worktree in detached state; its old
  Noctalia-template approach conflicts with the accepted GUI-managed setup.
- [x] Verify real Chezmoi initial and refresh prompts, stored machine/profile
  data, the SSH opt-in, and apply/repair with a fake Mise in a disposable home.
Local validation passes: `just check` in both repositories, `just validate`,
and the new native Chezmoi handoff regression. Dotfiles runs 115 Python tests
with three skips, plus its Bash suite. The skipped checks are native Hyprland,
optional Neovim downloads, and the optional Zathura GUI test; the prior audit
ran the Neovim download test separately. The first handoff fixture run failed
on EOF at the optional SSH prompt; supplying the user's empty answer fixed
that fixture without changing production behavior.

Phase 5's installation and failure-retry milestone passed on the Fedora VM.
Release completion still requires the signed engine distribution, its reviewed
key pin, and a clean one-liner drill with the final changes. All three
repositories are now public. The COPR key URL still returned HTTP 404 on
2026-09-07. Earlier checks that day found Nimbus and dotfiles private, before
the owner changed visibility. Commit and push are authorized; the final
published changes still need current CI and review.

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
  DNF, clone or validate the checkout, init with a separately verified engine
  and its input on the terminal; shellcheck runs in `just check` and CI.
- [ ] Publish the engine RPM channel and review its signing-key pin before
  enabling automated engine installation. Bootstrap fails closed without an
  existing engine; the intended COPR key URL returned HTTP 404 on 2026-09-07.
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

### Manual source release

- [x] Add a manual-only workflow accepting an existing stable version tag on
  main. Test the tagged source and generate the vendored archive, source
  identity, module inventory, and checksums in temporary storage.
- [x] Separate read-only build/test permissions from draft creation; require
  manual publication and a separate COPR dispatch. Preserve existing releases.
- [x] Document the operator steps and first signing-key/VM prerequisites in
  [the release guide](../tools/release/README.md).
- [ ] Exercise the hosted workflow after merge with the first approved tag,
  inspect and publish its draft, then obtain the signed COPR build.

### Outside this repository

- [x] Read the current dotfiles template and PROFILES.md: the three handoff
  prompts and unvalidated stored profile list are present.
- [x] Validate the populated dotfiles Mise configuration and Linux Cargo
  fragment together. The owner's runtime selections now include Rust; the
  seven Cargo tools moved to `home/dot_config/mise/conf.d/cargo.toml`.
- [x] Verify initial and refreshed Chezmoi prompts and apply/repair together
  in an isolated home, with the actual template and a fake Mise installer.
- [x] Verify real user-tool installation and the handoff in a disposable
  Fedora VM.
- [x] Publish Nimbus and dotfiles through the owner's GitHub visibility change
  after reviewing current files and reachable Git history with Gitleaks.
- [ ] Create the COPR project, verify its real public signing key, publish the
  approved release source, and obtain a successful signed engine build.
- [ ] Ship the verified COPR key and fingerprint, then repeat the public
  one-liner from the restored VM without a manually supplied engine.

## Evidence

### Manual release preparation, 2026-09-07

The complete local gate passes with Go 1.26.7, including nine source-export
scenarios. Actionlint and Gitleaks pass. Four local executions of the draft
job with a fake GitHub CLI cover success, changed tags, checksum failure, and
an existing release. They perform no GitHub mutation.

A disposable Git snapshot of Nimbus produced a 2.85 MB vendored source archive
with 23 dependency notice files. Its real offline Go tests, versioned engine
build, and definitions validation passed; Fedora RPM tools also produced an
SRPM without network access. This fixture is not a published release.
CodeRabbit was unavailable because of its rate limit. The local Codex fallback
found that Git's `push.followTags` setting could publish extra tags. The helper
now disables tag following explicitly; a native Git regression verifies only
the requested tag reaches the remote. Targeted verification and the complete
local gate pass. GitHub CI passed at `421344b`; the first release draft remains
pending. The final license audit identified Unicode 15.0.0 tables bundled by
uniseg under Unicode-DFS-2016. `licenses/Unicode-DFS-2016.txt` preserves their
separate notice; COPR packaging installs it alongside dependency notices.

### COPR bootstrap preparation, 2026-09-07

The owner made Nimbus, dotfiles, and COPR public. Both current-file and
all-ref Git-history Gitleaks scans found no secrets. Nimbus PR 9 integrates
current main, fixes the ShellCheck SC2015 guards, and passes `just check` with
Go 1.26.7. GitHub CI passed at `421344b`.

Bootstrap now uses the DNF-owned engine path and verifies a fresh COPR RPM
with the pinned public key in an isolated RPM keyring before any engine/source
installation. Tests exercise missing/invalid trust, a wrong fingerprint,
foreign or symlinked repository files, download/signature/identity failures,
DNF failure, PATH shadowing, successful handoff, and temporary-file cleanup.
A Fedora 44 native RPM check accepts an ephemeral fixture signature with its
key, and rejects both an unknown signer and the previously built unsigned RPM
when signature and digest verification are both required. The test key existed
only inside the disposable offline container and is not a release key.

COPR PR 1 is merged. Manual Actions run `34154509727` created the public
`furyfree/nimbus` project for Fedora 44 x86_64 with build networking disabled.
The project API succeeds; its public-key URL still returns 404. COPR exports
the signing key during a build. Release publication and the signed build
therefore precede adding the verified bootstrap pin and the final VM drill.
No placeholder pin has been added.

The restored Fedora 44 VM contains no Nimbus receipts or Chezmoi state. Its
old manual engine and checkout were moved, without deletion, to
`~/phase5-pre-copr-20260907T180840Z/`. The new `~/start-phase5` launcher checks
that COPR metadata exists, then runs the public installer through a terminal
with output-only logging, exit-code propagation, and elapsed time. It was
syntax-checked but not run. The owner starts the drill only after the release
and pinned bootstrap are available on main.

### Fedora build-toolchain compatibility, 2026-09-07

The engine's minimum Go version is now 1.26.7, matching Fedora 44's native
compiler. The existing dependency graph requires at most Go 1.25; no
dependency versions or Go source files changed for this adjustment. CI reads
the minimum from `go.mod`. The full `just check` gate passes with Go 1.26.7
and automatic toolchain switching disabled.

The separate COPR recipe now requires `golang >= 1.26.7`. An offline Fedora
44 container built both the source and binary RPMs from a private local
working-tree snapshot. Its native Go compiler ran the full vendored Go test
suite and the resulting engine validated every machine definition. The RPM
contains the engine and license notices, no install scriptlets or compiler
runtime dependency. This is local packaging evidence, not a signed release,
COPR build, or VM installation of the RPM.

The first host test mixed the 1.26.7 compiler with the inherited 1.27.1
GOROOT; selecting both compiler and GOROOT explicitly fixed the test setup.
The first RPM build exposed the draft's incorrect Go license path; it now
uses Fedora's native license directory and the complete RPM build passes.

### Completed VM installation and retry, 2026-09-07

After the final retry, `nimbus status` reports all 140 desired packages
present, with no pending or blocked changes. `mise ls` reports no missing
tools. Typst 0.15.1 runs from Terra's RPM; DNF transaction 7 completed with
status `Ok`. Tinymist's selected `v0.15.6` source build runs successfully.
The earlier Cargo failures were recovered without reinstalling completed tools.

`chezmoi --skip-secrets verify --exclude=scripts` passes, and doctor's
Chezmoi selection check passes. Full verify returns 1 because the always-run
Mise after-script is scheduled on every apply; status lists only that script.
This does not indicate file drift or another failed tool installation.

Doctor still reports disabled Secure Boot in the VM. The existing mcelog
service failed at boot because it does not support the virtual AMD CPU.
These remain VM limitations; no security policy was relaxed.

This evidence closes installation and failure retry on the prepared VM.
It does not prove the final public one-liner on a fresh snapshot: the drill
used staged checkouts and a separately supplied engine, with fixes applied
between attempts. The signed engine and public-source gates remain open.

### Typst source and temporary build storage, 2026-09-07

The next VM retry installed the development prerequisites, cargo-update,
Sheldon, and VM Curator. Typst and Tinymist were still missing. Their retained
compiler diagnostics report `Disk quota exceeded (os error 122)` under
`/tmp`: Fedora mounted it as a 3.9 GiB tmpfs with user quotas, while the disk
containing `/home` and `/var/tmp` had about 48 GiB free. This is a temporary
build-storage failure, not another missing header or an observed OOM kill.

At the owner's request, the common profile now selects `terra:typst`, and
dotfiles removes `cargo:typst-cli`. The VM's cached Terra metadata provides
`typst-0.15.1-1.fc44.x86_64`. Tinymist's native Mise `install_env` selects
`TMPDIR=/var/tmp` for installation and upgrades; other tools and system mount
settings are unchanged. A native Mise probe with fake Cargo verified the
temporary directory, CLI selection, and failure propagation without installing
tools. The successful real retry is recorded above.

### Cargo prerequisites and closing setup notes, 2026-09-07

The VM installed most Mise tools, including Caligula and resvg, but left five
Cargo tools missing. The retained builds show OpenSSL probing and a missing
`zlib.h`; VM Curator depends on libudev, whose pkg-config metadata is absent.
The Mise component now explicitly selects Make, pkg-config, and OpenSSL,
curl, zlib, and libudev development packages. The Fedora cache resolves their
providers. Source builds remain selected; no binstall setting was weakened.

Tinymist's published crate is library-only. Dotfiles now selects `tinymist-cli`
and its `tinymist` binary from upstream release `v0.15.6` through Mise's Cargo
Git backend with the lockfile enabled. The later retry and remaining
temporary-storage failure are recorded above.

Init forwards native output and repeats marked setup notes after its summary,
including on failure. Dotfiles prints applicable 1Password, shell, and Noctalia
instructions before running Mise. Regressions cover streaming fragments,
duplicate notes, bounded capture, and successful and failed handoffs. No
installation transcript is saved or interpreted as a command.

### VM installation and Chezmoi selection, 2026-09-07

The retry started at 17:19:37 CEST; DNF finished its upgrade at 17:25:06,
about 5 minutes 29 seconds later. This excludes the previous failed attempt
and repair wait; the final init exit timestamp was not recorded. DNF history
marks both the installation and upgrade `Ok`. All 133 desired packages,
including three Flatpaks, are present; status reports no pending operations
or upgrades.

Five Fedora RPM downloads returned 404 from `mirror.accum.se`; DNF retried
other mirrors and installed every affected version. A concurrent native
`dnf-makecache.service` reported an RPM lock error while Nimbus's transaction
held the lock, but completed its metadata refresh. Neither stopped the system
installation.

Nimbus then falsely rejected the Chezmoi selection. The actual config stores
the correct three machine profiles, but Go's case-insensitive struct decoding
let lowercase `profiles` replace uppercase `Profiles` with the platform list.
Init and inspection now share an exact-key parser. Regressions cover both key
orders, missing and malformed machine selection, and the full handoff fixture
with both lists. The later successful dotfiles apply and real Mise installation
are recorded above.

### VM repository-key failure, 2026-09-07

The first run installed Git, wrote the selector and DNF configuration receipt,
then stopped at Brave before enabling any third-party repository. The staged
`key-brave.asc` contains three primary keys; the executor correctly requires
exactly the declared key. The definition now uses `system/keys/brave.asc`,
containing only the existing pinned release signer from the maker's bundle.
The other downloaded DNF/COPR keys each have one matching primary key. Brave
Origin 1.94.121's RPM header names the existing pinned signer; this header
inspection is not a full package-signature or installation verification.

### Repository passthrough, 2026-09-07

- Started from Phase 5 PR #9 and carried forward main's Fastmail selection.
  Local fixes cover lock-file ownership and links, strict definition IDs and
  DNF values, effective repository overrides, absent Flatpak receipt retirement,
  explicit removal without dependency cleanup, visible replanned erasures,
  checkout identity before and after approval, explicit init trust approval,
  and complete Chezmoi refresh prompts preserving the SSH preference.
- `just check` and `just validate` pass locally. The original PR #9 CI head
  failed ShellCheck 0.9.0 on compound shell guards; those guards are corrected
  locally. This does not change or establish CI for the unpushed fixes.
- Dotfiles `just check` completes 114 Python tests with three native/opt-in skips
  and its Bash suite. The separate disposable-home Neovim integration test
  passes. Isolated Chezmoi `managed`, `status`, and `diff` pass; full `verify`
  reports the deliberately pending installer script, while
  `verify --exclude=scripts` confirms all rendered files and permissions.
- Claude Code (`claude-fable-5-1`) completed two full read-only audits.
  Accepted findings were reproduced or traced and fixed. Exact managed
  profile matching and adopted-package removal remain intentional; stopping
  sudo renewal does not promise to revoke unrelated cached credentials.
- The third, targeted peer pass hit Claude's session limit, reported to reset
  at 20:50 Copenhagen time. Its closing verdict is unavailable. The main
  agent inspected the final fix diff and verified it with regressions and the
  full local gate; no other provider was substituted.
- Source-only Gitleaks scans are clean in both repositories. No live install,
  apply, privileged command, fresh-VM drill, or GitHub write was performed.
- Fedora 44 disposable-container `repoquery` finds Chezmoi 2.72.0 in updates,
  meeting the dotfiles source minimum. This is availability evidence, not a
  fresh-install or restore drill.

### Desktop application selection

- [x] Select stable Fastmail (`flatpak:com.fastmail.Fastmail`) in the desktop
  profile instead of a Chezmoi webapp; retain the existing system Flathub
  source and normal Nimbus package lifecycle.
- [x] Owner smoke test on CachyOS, 2026-09-07: installed the system Flathub
  package and opened the signed-in inbox. The desktop default-handler query
  returned `com.fastmail.Fastmail.desktop`, but email links opened only the
  inbox, without a compose window or recipient. Direct `flatpak run` tests
  with `mailto:` URLs, both with and without a subject, failed the same way.
  Notifications were not confirmed. This does not verify installation through
  Nimbus or behavior on Fedora.
- [x] Owner accepts the `mailto:` limitation for this package-selection change
  and keeps Fastmail selected. No custom launcher workaround or default-mail
  configuration is added to Nimbus; further investigation is deferred to the
  new Fedora installation.
- [ ] On the new Fedora system, install through a reviewed Nimbus sync and
  verify sign-in, notifications, and email-link composition, including the
  recipient and subject. Record the installed version and whether the failure
  persists there before attributing it to Fastmail or its Flatpak packaging.

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
- Superseded pre-handoff observation: dotfiles once had four fixed profile
  choices, its own machine manifests, and a symlink selector. The current
  template preserves Nimbus-supplied IDs and the machine manifests and old
  selector model are gone; the Phase 5 evidence above supersedes this blocker.
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
