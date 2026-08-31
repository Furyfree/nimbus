# Nimbus analysis - GLM-5.3-Flash

Date: 2026-08-31. Scope: review of this repository's foundation, analysis of the
three context repositories (`~/git/niriland`, `~/git/dotfiles`, `~/git/docs`),
inspiration from `neur0map/ryoku-arch`, a survey of Go-based Linux system
installers, and a set of problems and design proposals.

## 1. Summary

The Nimbus foundation is unusually coherent: ownership boundaries, plan/apply
discipline, receipt model, and the Chezmoi split are all stated before any Go
code exists. The analysis below found no architectural flaw. It found:

1. One blocking contract gap: three different profile vocabularies exist across
   `SPEC.md`, `~/git/docs/setup/dotfiles/README.md`, and the dotfiles repo's
   `PROFILES.md` / `.chezmoi.toml.tmpl`, and the dotfiles template has no
   `Profiles` prompt at all - the planned `--promptMultichoice` handoff has no
   consumer. This must be reconciled before Phase 1 freezes profile IDs into
   embedded TOML and golden fixtures.
2. A set of Fedora-specific correctness gaps learned from niriland and the docs
   research: rpmnew/rpmsave drift, install-reason mutation, user-vs-root
   resolution, verification depth, NVIDIA driver variants.
3. A set of cheap design upgrades borrowed from the studied projects:
   provenance-stamped receipts, a `--json` envelope with stable exit codes,
   `nimbus why`, provider quarantine, plan-digest approval, and a standalone
   `nimbus validate`.
4. Confirmation that the "declarative Go workstation manager" niche is empty;
   the validated designs come from infra-grade Go projects (Ignition, kairos)
   and from two Bubble Tea tools by the ryoku author (glazepkg, and ryoku's own
   Go TUI installer).

Note: `containers/bootc`, often cited as the modern image-based installer, is
written in Rust, not Go. It is still included because its design is the closest
production analogue to Nimbus' desired/observed/applied model.

## 2. Nimbus foundation as it stands

Read: `SPEC.md`, `ROADMAP.md`, `TASKS.md`, `OPEN_QUESTIONS.md`, `CLI.md`,
`README.md`, `AGENTS.md`, `justfile`, `tools/package-query/`.

Strengths:

- The ownership matrix (Nimbus vs Chezmoi vs user vs other repos) is explicit
  and repeated consistently across `SPEC.md`, `AGENTS.md`, and `CLI.md`.
- Phase 1 is genuinely small and read-only; the exit criteria in
  `SPEC.md:156-161` are testable as written.
- `CLI.md` already encodes the hard-won rules: prune never implicit
  (`CLI.md:254-255`), apply re-plans and rejects changed state
  (`CLI.md:263-266`), unknown ownership blocks pruning (`CLI.md:275-278`),
  failed operations are never recorded as applied.
- The package-query dev container and COPR distribution story are scoped
  outside the runtime (`SPEC.md:133-137`).

Risks visible now:

- `SPEC.md` and `CLI.md` disagree on manifest richness. `SPEC.md:76-82` shows
  only `profiles` + `[dotfiles]`; `CLI.md:93-102` adds `components`, `packages`,
  and `package_exclusions`. CLI.md is clearly the newer intent; SPEC should be
  updated so the Phase 1 schema work has one definition.
- The docs repo (`setup/nimbus/README.md`) mentions user catalog overrides in
  `~/.config/nimbus/catalog/`. Nothing in this repo mentions them; ROADMAP
  explicitly says "do not add local profile/catalog override rules in this
  phase". Either the docs are ahead of the repo or the feature was dropped -
  decide and record it.
- `TASKS.md` phase 1 depends on the profile vocabulary (see 6.1).
- `OPEN_QUESTIONS.md` is empty while at least four decisions found by this
  analysis qualify as open (see 7).

## 3. Context analysis

### 3.1 niriland - the empirical predecessor

`~/git/niriland` (CachyOS/Arch, Niri + DMS, in maintenance mode per its README)
is a 42-script, ~5,200-line shell installer. Its importance is that it already
hand-rolled much of Nimbus' model, so its code and its 1,011-line
`docs/MIGRATIONS.md` are a requirements document written in failures.

Worth carrying over:

- Plan/apply discipline that already evolved there: the common-baseline
  migration simulates the removal against a copied package DB, protects a
  deny-list (including every `lib32-*`), aborts if any protected package
  appears, re-resolves the transaction immediately before apply and aborts if
  it changed, and defaults to plan-only. This is exactly Nimbus' apply contract,
  proven by use.
- Manifest comments as rationale: `heroic before the meta package`, pinned
  `tesseract-data-eng` to avoid a provider prompt, `zathura-pdf-mupdf` over
  poppler. Nimbus catalog entries need a rationale/notes field, not just
  names.
- User-local mise with pinned version + sha256 verification
  (`ensure_user_mise`) - the pattern for `upstream_packages.toml` trust.
- Concrete Fedora-relevant technical decisions: libvirt needs
  `firewall_backend = "iptables"` to coexist with a firewall; FDE order is
  recovery key first, then TPM2; `crypttab.initramfs` uses `-` not `none`;
  desktop-session PATH gaps solved with `~/.local/bin` shims.
- The layered config model (tracked fragments, engine includes, user
  `override.d`) maps cleanly onto the Chezmoi split.

Pain points Nimbus must eliminate (each documented in niriland or docs):

- Configs edited in place with `sed`; a `.pacnew` overwrite once destroyed
  mirrors and DNS. Fedora equivalent is rpmnew/rpmsave (see 6.3).
- Secrets (`SYSTEM_PASS`, `LUKS_PASSWORD`) exported into the environment for
  the whole run, readable from `/proc/<pid>/environ`.
- Unverified `curl | sh` for Zed and opencode, versus pinned mise - the exact
  gap `upstream_packages.toml` closes.
- No state, no resume, no drift model; updates replay heavy steps blindly.
- Duplicated helpers, three sudo styles, `yay` and `paru` both installed while
  different scripts use different ones - provider ownership must be singular.
- A `$USER` vs `SUDO_USER` bug in the fingerprint tool breaks under sudo (see
  6.3).
- Hardcoded repo path: the niri config `include`s back into the repo checkout,
  so moving the repo breaks the desktop. Chezmoi removes this class entirely.
- Machine-specific values baked into shared artifacts (unit files literally
  named `openwebui.service (CPU - archbook)`).

### 3.2 dotfiles - the Chezmoi side of the contract

`~/git/dotfiles` (-> `~/.local/share/chezmoi`, chezmoi 2.72.0) is a two-commit
scaffold: `.chezmoiroot` = `home`, all content files are 1-byte placeholders,
no `.chezmoiscripts` at all, and explicit rules that Chezmoi scripts must never
install packages, elevate, touch `/etc`, or manage services. That is the
cleanest possible starting point for the Nimbus boundary.

Findings that matter to Nimbus:

- `.chezmoi.toml.tmpl` has no `Profiles` prompt. It uses `promptChoiceOnce`
  for a single `desktopStack` (choices: `none`, `hyprland-noctalia`) and
  derives `profiles` from `.chezmoi.os` (`common`, `unix`, `linux`, ...). The
  `--promptMultichoice 'Profiles=...'` contract in `ROADMAP.md:99-103` has no
  consumer and would be silently ignored.
- `PROFILES.md` forbids `desktop`, `all`, `laptop`, and `nimbus`
  profiles and says "Reconcile these names with Nimbus before Nimbus supplies
  the list." That reconciliation has not happened.
- `machines/` does not exist at the dotfiles repo root yet. Nothing blocks
  creating it; there is also no root `.gitignore`, which is fine because
  `machines/*.toml` is meant to be tracked.
- No `run_` scripts exist, so there is currently zero ownership overlap. The
  docs-repo contract (`setup/dotfiles/README.md`) already assigns 1Password
  installation, greeters, portals, nix daemon, and login shell to Nimbus.
- One ordering trap: the template gates the 1Password SSH prompt on
  `lookPath "op"`. On a fresh install, `chezmoi init` runs before Nimbus' full
  apply (SPEC.md:113-118), so `op` will not exist yet and `onePasswordSsh`
  will default to false with no prompt. Either the gate must not depend on the
  binary at init time, or this becomes an explicit postinstall task.

### 3.3 docs - constraints Nimbus inherits

`~/git/docs` consolidates the active contracts in `setup/nimbus/` and
`setup/dotfiles/`, with research evidence in `research/`. The Nimbus-relevant
decisions it locks in:

- Read-only first; first release implements only `config resolve`.
- Desired state != observed state != last-applied state, with receipts, under
  `~/.local/state/nimbus/`.
- machine.toml is bootstrap input, not a third repository, never deployed by
  Chezmoi.
- Never guess removal; automatic must never be silent; elevation scoped per
  operation; no root daemon, no sudo keepalive (evidence:
  `research/workstations/omarchy-sudo-keepalive.md`).
- Boot/ISO direction (`setup/nimbus/boot-and-iso.md`): systemd-boot, unsigned
  UKIs, Secure Boot off on the ISO, LUKS2 + encrypted swap hibernation, ESP 2
  GiB, Anaconda+Kickstart with a fatal Nimbus boot-provision step, Pungi
  pinned compose. Nimbus never repartitions a live system.
- Windows VM (`setup/nimbus/windows-vm.md`): Dockur 6.05 pinned by digest,
  Docker only, loopback binds, no docker group membership, purge needs a
  second confirmation naming the path.
- Security model (`security/README.md`): layered host model, containers for
  services, `nimbus audit` checklist (undeclared packages, listening ports,
  socket exposure, AI tooling on non-localhost), patch by exploitability.
- Lane rules: DTU Eduroam cert belongs to the dtu-bachelor repo (explicitly
  not Nimbus/Chezmoi); COPR recipes are a separate repo; Package Index is a
  separate Rust tool feeding reviewed candidates into the catalog.

Two documented tensions to keep on the radar:

- Secure Boot: the ISO design disables it, but the real Fedora 44 test machine
  runs Secure Boot enabled with NVIDIA via RPM Fusion akmods + MOK enrollment
  (`research/workstations/fedora-hyprland-test.md`). The `nvidia` profile must
  therefore support the SB-enabled path (MOK enrollment as a manual/postinstall
  step) while the ISO path stays unsigned. The boot provisioner should detect
  Secure Boot and behave accordingly.
- The same research doc records RPM-owned files with `nobody:nobody` ownership
  (fixed with `rpm --setugids`/`--setperms`) and notes `rpm -V` caught it -
  verification should include `rpm -V` for rpm-owned files (see 6.3).

## 4. ryoku-arch - what to borrow

`neur0map/ryoku-arch` (779 stars) is a hand-built Arch distribution: Hyprland
desktop authored in Lua, a Bubble Tea TUI installer, a signed pacman repo, and
its own `ryoku` control CLI. It is not Go-based as a whole, but it is highly
relevant: its installer TUI and control CLI are Go, and its discipline matches
Nimbus' stated principles.

Architecture worth noting:

- Three pillars with one job each: `ryoku/` (desktop), `system/` (machine
  definition: boot chain, hardware policy, package sets), `installation/` (TUI
  + backend + archiso profile). The golden rule is "every path has one purpose
  and appears once."
- Frontend/backend contract: the Go TUI collects choices and drives the
  backend through an explicit `RYOKU_*` environment contract; the backend is
  one file per step (`preflight`, `disk`, `luks`, `pacstrap`, `drivers`,
  `bootloader`, `snapshots`). Nimbus' TUI should drive the exact same resolved
  plan the CLI uses, not a second flow.
- `ryoku update`: snapshot -> package transactions -> re-materialize configs
  -> reload -> paired post-snapshot; a failed package step aborts before
  anything else changes. It orchestrates pacman/yay/snapper and re-implements
  none of them - the same delegation rule Nimbus states for DNF and Chezmoi.
- Recovery ladder: `ryoku doctor` as idempotent convergent reconcilers with
  `--explain`; `ryoku rollback` via snapshots; `ryoku recovery` as a last
  resort that refuses non-Ryoku machines and confirms before mutating. A
  graded recovery surface, not just one hammer.
- Hardware policy as data + small detect scripts (`ryoku-gpu-detect` ranks
  GPUs, picks NVIDIA open modules for Turing+, proprietary for older, pins the
  strongest GPU on desktops and the integrated one on laptops). Nimbus' nvidia
  profile will need this variant story (see 6.3).
- Test harness: `tests/install-vm.py` runs real unattended installs in QEMU,
  `container-install.sh` verifies packaged installs in containers, and
  `iso-stage-check.sh` proves the staged ISO tree is byte-reproducible.

The same author's `neur0map/glazepkg` (622 stars, Bubble Tea) is the closest
cousin to Nimbus' dashboard and packages screens; its patterns are in 6.2.

What NOT to copy: Ryoku config is reconciled by clobbering Ryoku-owned files
on every update with user `user.lua`-style overrides as the only escape hatch.
Nimbus deliberately routes user files through Chezmoi with real diff/apply
ownership instead; do not grow a second "materialize" config layer inside
Nimbus.

## 5. Go-based Linux installers and managers - landscape

Survey result: there is no mature Go project occupying "declarative
workstation state for a mutable Linux desktop". The validated Go designs are
infra-grade; the workstation-grade Go tools are package TUIs. Nimbus is
greenfield and borrows patterns, not competition.

| Project | What it is | Lesson for Nimbus |
|---|---|---|
| coreos/ignition (975 stars, Go) | First-boot declarative provisioning | Versioned spec field, frozen legacy specs, spec-to-release support table, migrators, and a standalone `ignition-validate` binary usable in CI. Highest-value discipline source for Nimbus' TOML formats. |
| coreos/butane (archived) | YAML transpiler to Ignition JSON | Archived into ignition itself once the identity stopped mattering; keep one format with one owner. |
| containers/bootc (2.2k stars, Rust) | Image-based OS; image = desired state | Staged update lifecycle (`--check`/`--download-only`/apply/rollback); `.bootc-aleph.json` install receipt with provenance (digest, timestamp, tool version); delegation to bootupd for bootloader work. |
| kairos-io/kairos (1.8k stars, Go) | Immutable meta-distro installer | Multi-call binary dispatched on argv[0]; consolidated 8 repos into one Go monorepo after per-repo drift hurt; explicit install/upgrade/reset verb set - "reset" belongs in a recovery story. |
| suse/elemental (Go) | OCI-image installer toolkit (rewritten) | Rewrite anti-lesson: rewrites reset adoption; migrate formats instead. Contribution rule worth adopting: "Avoid logging the very same error in multiple places... never a log without details." |
| Vanilla-OS/ABRoot v2 (389 stars, Go) | Transactional A/B root | Clean transaction vocabulary ("atomic if fully applied or not applied"); anti-lesson: shell-string command config with `%s` placeholders is fragile - typed provider structs beat command strings. |
| ublue-os/fleek (archived 2024) | Go wrapper around chezmoi, then nix home-manager | THE anti-lesson for Nimbus' Chezmoi handoff: generated-config indirection surfaced raw nix stack traces users could not escape; opinionated presets owned upstream deprecations (presets broke when `exa` was removed); implicit `autocommit/autopull/autopush` config; two config dialects. Nimbus must invoke Chezmoi as Chezmoi, never auto-commit, and pass errors through un-mangled. |
| moson-mo/pacseek (654 stars, Go) | TUI package browser (tview) | UI never mutates directly - it shells to the user's chosen helper; sortable/cached results; settings form; the TUI is a thin shell over the same operations the CLI exposes. |
| neur0map/glazepkg (622 stars, Go) | Bubble Tea multi-provider package dashboard (46 managers) | `install/remove/... --json` prints the resolved plan (manager, version, exact command) without running; stable `{gpk_version, schema, data}` envelope; exit codes 0 ok / 1 error / 2 "no" / 3 ambiguous; broken providers are skippable but stay listed ("nothing disappears quietly"); confirmation modal groups operations by privilege so the password is entered once; background op queue; timestamped snapshots with `diff`; ownership-respecting self-update. |
| jetify-com/devbox (12.3k stars, Go) | Nix-backed dev environments | Three-layer model: committed declaration -> resolved lockfile pinning exact store paths -> reproducible outputs (shell, container, image). Matches catalog -> plan -> receipt. |
| ublue-os/uupd (Go) | One-shot modular updater (systemd timer, no daemon) | Per-module enable/disable; preflight hardware gates (`bat-min-percent`, `mem-max-percent`, net budget) before auto-updating; scope discipline ("anything outside of updating these core components is out of scope"). |
| clearlinux/clr-installer (101 stars, Go) | Clear Linux OS installer | Proof that a full Go distro installer is viable; largely superseded when Clear Linux wound down. |
| ekristen/cast (139 stars, Go) | Installer for Salt-based distros | Installer-as-distribution-pattern, thin. |
| Vanilla-OS/apx (589 stars, Go) | Multi-package-manager wrapper in containers | Wrapper complexity grows quadratically with wrapped managers; Nimbus' typed providers with one owner each avoid it. |

Also relevant but not Go: `archinstall` (Python) for interactive profile UX,
`ChrisTitusTech/linutil` (Rust) for the "tune-up TUI" genre, Omarchy (bash) for
the sudo keepalive anti-pattern already documented in the docs repo.

## 6. Problems found and design proposals

### 6.1 Resolve before Phase 1 freezes data

P1. Profile vocabulary conflict (blocking).
`SPEC.md:47-54` has `common`, `development`, `gaming`, `hyprland-noctalia`.
`docs/setup/dotfiles/README.md` plans a split namespace with granular IDs
(`desktop`, `hyprland`, `noctalia`, `developer`, ...). The dotfiles
`PROFILES.md` uses platform profiles plus the compound `hyprland-noctalia` and
forbids `desktop`. All three must become one namespace before the TOML types,
fixtures, and golden tests are written, because profile IDs end up embedded in
the binary and asserted in tests. Recommendation: keep the four SPEC profiles
as the machine-manifest surface, document that component IDs
(`windows-vm`, `libvirt`, `nvidia`) are not Chezmoi-relevant, and pass the full
resolved list to Chezmoi unchanged per SPEC - the dotfiles `PROFILES.md` then
documents which IDs it maps to files and ignores the rest. That preserves the
"IDs unchanged, no second list" rule.

P2. The `Profiles` prompt consumer does not exist yet.
The dotfiles template must switch from `promptChoiceOnce desktopStack` to
`promptMultichoiceOnce` on `Profiles`, consuming the choices Nimbus passes.
Two sub-decisions: (a) on reinstall, the manifest already knows the profiles -
Nimbus should not make the user re-select; since `--promptMultichoice` only
defines choices, consider passing the resolved list additionally as an
environment variable (e.g. `NIMBUS_PROFILES`) that the template uses as the
prompt default, or as explicit Chezmoi config data. (b) add a golden test that
asserts the exact `--promptMultichoice` argv built from a resolved profile
list.

P3. SPEC vs CLI manifest schema drift.
Update `SPEC.md`'s `machine.toml` shape to include `components`, `packages`,
and `package_exclusions` (or explicitly defer them), so Phase 1 schema work has
one canonical definition.

P4. User catalog overrides: accepted or dropped?
`docs/setup/nimbus/README.md` promises `~/.config/nimbus/catalog/` overrides;
this repo's docs say embedded-only, no overrides this phase. Decide, and move
the answer into SPEC (per the AGENTS.md rule that accepted answers live in the
owning document).

### 6.2 Design upgrades (borrowed and original)

D1. Provenance-stamped receipts.
Beyond "what was applied", record what definitions were in effect: Nimbus
version, embedded profiles/components/catalog content digest, and per-resource
verification results (bootc's `.bootc-aleph.json` precedent). Stamp the build
in via `ldflags`/`debug.ReadBuildInfo` and surface it in `nimbus version`.
This makes "which catalog version installed this package" auditable and makes
state.json migrations decidable.

D2. Version and migrate every format from day one.
`schema = 1` exists in machine.toml; add the same to `state.json`, receipts,
and each catalog file. Adopt Ignition's rule: never mutate a version's
meaning; add a version plus a migrator; keep a support table of which Nimbus
release reads which schema.

D3. A standalone `nimbus validate`.
Ignition ships `ignition-validate` as a separate, non-mutating binary usable
in CI. Nimbus Phase 1's resolver already validates; expose it as a dedicated
command that accepts a manifest/profile set and exits nonzero on any error, so
dotfiles CI and the future ISO build can gate on it without installing anything
or touching the system.

D4. Scripting contract: JSON envelope + stable exit codes.
All read commands take `--json` and emit `{nimbus_version, schema, data}`;
adopt glazepkg's exit-code convention (0 ok, 1 error, 2 empty/none, 3
ambiguous) so scripts and the future Package Index integration can depend on
it. Document it in CLI.md.

D5. `nimbus why <resource>`.
Answer "which profile/component/machine entry brought this package in".
CLI.md's `packages list` shows provenance per package, but a single-target
explanation is the command people actually reach for when deciding whether a
removal is safe. Cheap to build once the resolution graph exists.

D6. Provider quarantine.
When a provider's native tooling is broken (mise shim gone, nix store damaged -
both real incidents in the docs research), Nimbus should be able to skip that
provider visibly: `status`/`doctor` list it as quarantined, nothing disappears
quietly, and plan marks its resources as blocked-manual (glazepkg's
`managers skip` pattern, combined with the uupd lesson that a failing module
must not fail unrelated modules).

D7. Plan-digest approval guard.
CLI.md requires apply to reject if configuration or observed state changed
while approval was pending. Make it exact and cheap: hash the canonical plan
JSON at review time, recompute at apply start, compare. This also gives
receipts a link to the exact reviewed plan.

D8. Privilege grouping in apply and the TUI.
Keep one `sudo` per command (per SPEC), but order the plan so same-privilege
operations are adjacent and grouped in the review UI, so the user authenticates
the minimum number of times (glazepkg groups by privilege level; Nimbus'
no-keepalive rule makes ordering the only lever).

D9. Drift and repair as doctor reconcilers.
`nimbus doctor` (CLI.md:314-319) should be structured as small, individually
testable, idempotent reconcilers with an `--explain` mode (ryoku's pattern),
including: rpmnew/rpmsave detection on Nimbus-owned config paths, broken
Nimbus-owned symlinks, state vs observed mismatches, and the `.local/bin`
PATH-shim integrity that desktop sessions depend on.

D10. Prune/adopt correctness on Fedora.
The bootstrap adopt flow must mark Nimbus/Chezmoi/Git as user-installed
explicitly (`dnf mark user`), and Nimbus must never mutate install-reason as a
side effect elsewhere - niriland's lib32 incident destroyed orphan detection
by marking 117 packages explicit to protect them. Adoption is a planned,
recorded operation.

D11. Apply-time transaction simulation.
Mirror niriland's proven pattern with DNF: resolve the transaction
(`dnf5 ... --assumeno` semantics), check the protected set (protected = other
selected sources depend on it, or platform-critical like `lib32`/multilib
equivalents), and abort if resolution changed between plan and apply.

D12. `nimbus audit` as the security face of `unmanaged`.
The docs security model already defines the checklist (undeclared packages,
listening ports, container socket exposure, AI services on non-localhost,
model artifacts, secrets in unsafe files). Build it on the same inspection
layer as `unmanaged`/`doctor`, read-only, later phase.

D13. Preflight gates before any scheduled/automatic apply (far future).
uupd's battery/CPU/memory/network gates and per-module toggles are the right
shape for any future unattended apply; record the pattern in ROADMAP's "Later"
so it is not reinvented.

D14. Plan regression tests in disposable VMs.
ryoku proves unattended QEMU install tests are practical. For Phase 2+,
snapshot a baseline Fedora 44 VM, run `nimbus plan --json`, and diff against a
golden plan on every release. This tests the real inspection layer, not just
fixtures, and catches DNF output-format drift early (DNF5 is still churning).

### 6.3 Fedora-specific correctness notes

- rpmnew/rpmsave: Fedora's equivalent of niriland's `.pacnew` disaster. Any
  Nimbus-owned `/etc` file must be installed in a way that survives package
  updates, and doctor must flag `.rpmnew`/`.rpmsave` next to Nimbus-owned
  files instead of ignoring them.
- Verify rpm-owned files with `rpm -V` (ownership/perms bugs were observed on
  real hardware in the docs research and fixed with `rpm --setugids`).
- User resolution: resolve the invoking user explicitly (UID + `SUDO_USER`)
  for user-scoped resources (user units, group membership, `~/.local/bin`);
  niriland's `$USER`-under-sudo bug is the cautionary example.
- NVIDIA: the `nvidia` profile needs variants (open kernel modules for
  Turing+, proprietary for older) selected from Phase 2 inspection, plus the
  Secure Boot/MOK path as a manual step when SB is enabled. ryoku's
  gpu-detect ranking is a useful reference for the hybrid-GPU laptop case.
- libvirt + firewall coexistence: record `firewall_backend = "iptables"` (or
  the nftables-equivalent decision) as a catalog note from niriland's working
  setup.
- The bootstrap package list ("only Nimbus, Chezmoi, Git transport") also
  needs the DNF COPR plugin to enable the COPR repo - worth stating in
  CLI.md's bootstrap section.
- Danish locale/keyboard should be a `common` resource (localectl), not a
  side effect of a desktop profile.
- DNF module/plugin churn (DNF5 transition) argues for provider output
  parsing to be isolated and fixture-tested (D14).

### 6.4 Postinstall residue -> typed tasks

Mapping the still-manual `docs/POSTINSTALL.md` residue onto the postinstall
model shows which tasks are worth typing first:

- Nimbus resources (later phases): WireGuard/OpenConnect VPN profile imports
  (nmcli), Brave managed policy in `/etc/brave/policies/managed/` (docs
  already assign this to Nimbus), greetd/Noctalia greeter verification.
- Postinstall tasks with verification: second-boot user-session check
  (niriland's documented quirk), fingerprint enrollment, MOK enrollment when
  Secure Boot is on, JetBrains dotnet shim (`/usr/share/dotnet` -> mise),
  `chezmoi diff/apply` review.
- Explicitly user-owned, never automated: browser preferences, 1Password
  unlock, WoW addons, DTU Eduroam certificate (dtu-bachelor repo owns it).

## 7. Recommended immediate actions

1. Add to `OPEN_QUESTIONS.md`: profile namespace reconciliation (P1), the
   reinstall prompt-default mechanism (P2), user catalog overrides (P4), and
   rpmnew policy for Nimbus-owned system files (6.3).
2. Update `SPEC.md`'s manifest example to the CLI.md shape (P3), and add the
   DNF COPR plugin to the bootstrap package list.
3. Before Phase 1 types are written, write the shared profile-ID list into
   both repos: the four SPEC profiles as machine-facing IDs, and the dotfiles
   `PROFILES.md` mapping (which IDs change managed files). Then convert the
   dotfiles `.chezmoi.toml.tmpl` to a `Profiles` multichoice consumer.
4. Adopt into `AGENTS.md` code-review rules: one detailed error per failure
   path (elemental's rule), and "receipts must record nimbus version +
   embedded-data digest" (D1/D2).
5. Add D3 (`validate`), D4 (envelope/exit codes), and D5 (`why`) to the
   Phase 1/2 scope in `ROADMAP.md` - all are read-only and cheap, and they
   lock in the scripting contract before TUI work begins.

## 8. Sources

Local: `~/git/nimbus` (all docs), `~/git/niriland` (install, cleanup,
migrations, docs/MIGRATIONS.md), `~/git/dotfiles` (template, PROFILES.md,
AGENTS.md, .chezmoiignore), `~/git/docs` (setup/nimbus/*, setup/dotfiles/*,
PLAN.md, POSTINSTALL.md, security/README.md, research/workstations/*,
SYSTEM-CLEANUP-REPORT-2026-08-18.md).

Remote: github.com/neur0map/ryoku-arch (README + docs/structure.md),
neur0map/glazepkg, coreos/ignition, containers/bootc, kairos-io/kairos,
suse/elemental, Vanilla-OS/ABRoot, ublue-os/fleek, moson-mo/pacseek,
jetify-com/devbox, ublue-os/uupd, clearlinux/clr-installer, Vanilla-OS/apx,
ekristen/cast, plus GitHub API topic searches for Go Linux installers,
dotfiles managers, and workstation setup tools.
