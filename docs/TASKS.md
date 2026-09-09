# Nimbus tasks

## Current phase: 6. System resources and desktop recovery

Plan: [System resources and desktop recovery](ROADMAP.md#6-system-resources-and-desktop-recovery).
Phase 6 is merged on main and published as `v0.2.0`. The owner completed the
first candidate installation on the clean VM and confirmed reboot through
Noctalia into
Hyprland. The signed COPR build and its VM installation trial passed; the
remaining recovery/portal drills are separate, uncompleted gates.
Phase 5 is complete; its prompt improvements are included below.

### First milestone: login and recovery

- [x] Inspect the VM's boot target, greetd unit/configuration, greeter launch
  command, session entries, portals, and journal. The new candidate boots
  successfully; the earlier installation's exact failure remains unproven.
- [x] Resolve the packaged greeter launch contract, exact recovery-file
  integration, and ownership rules. [Research](PHASE6-RESEARCH.md) records the
  package evidence and retained defaults; console restoration needs the VM.
- [x] Extend strict definitions, facts, plans, apply, verification, and receipts
  for the required system units, memberships, files, and fixed-argv triggers.
  Preserve existing memberships and reject unknown or foreign removal.
- [ ] Test a system-owned recovery session independent of Chezmoi and a manual
  TTY restoration route. Normal graphical login has already been enabled;
  independent recovery remains a release follow-up risk.
- [x] Declare greetd/Noctalia activation, session discovery, portal selection,
  and required memberships through the normal resource lifecycle. Show any
  boot-target or display-manager change; preserve foreign configuration.
- [x] Extend doctor for the new resources, including actionable explanations
  of missing activation, effective configuration, and owned drift.
- [x] Align ownership views and retained-source counts with the planner.
  Go 1.26.7 fixtures cover Flatpak adoption across origins, native RPM receipt
  identities, multilib ambiguity, and legacy receipts.
- [x] Reject colliding receipt ownership and unencodable stages, retain legacy
  receipt access, and preserve each merged-install package's provenance.
  State schema 3 blocks older writers; receipt and baseline schemas stay 2.
  Go 1.26.7 regressions also cover commented Git includes, native service
  enablement, and installed Flatpaks without version metadata.
- [ ] Pass reboot to greeter, normal login, logout/relogin, file-picker and
  screen-sharing portals, and recovery with absent/broken user configuration.
- [ ] Prove second-sync convergence, failed-activation recovery, and component
  removal without losing console access. Run focused provider tests and
  `just check`; record the actual graphical VM evidence before proceeding.

### PR 18 follow-up

Plan: [Modern Go PR validation](ROADMAP.md#modern-go-pr-validation).

- [x] Preserve verified file receipts and retirement when temporary payload
  cleanup fails; surface the failure in the closing differences report.
- [x] Normalize CRLF repository lines before parsing blank-line boundaries.
  Both regressions failed before their fixes and pass with Go 1.26.7. The file
  regression records real temporary state and checks install, retry, removal,
  and retry; verification failures still reject successful receipts.
- [x] Pass `just check` with Go 1.26.7 and inspect the final fix diff. Formatting,
  vet, all Go tests, diff checks, markdownlint, and shellcheck passed.
- [ ] Confirm the disposable VM and restore snapshot, then stage and identify
  the candidate. Record read-only validation, doctor, and first-plan results.
- [ ] Apply the reviewed first plan and prove second-sync convergence.
- [ ] Prove managed-file/component removal, activation, receipt retirement,
  and retry without losing console access; restore the snapshot.
- [ ] Test legacy schema 1 and 2 baselines separately, including the schema 3
  write boundary and older-engine refusal. Record recovery and all results.
- [x] Obtain owner authorization for commits, push, marking PR 18 ready,
  merging it after final checks, and closing superseded Voxtype PR 17.

Native apply and lifecycle validation remain incomplete. The read-only VM
comparison below verifies the package-resolution fix; existing Phase 6 desktop,
portal, and recovery gaps remain open.

### Version 0.2.2 fresh-install blocker, 2026-09-09

The public bootstrap downloaded the signed COPR engine, verified its signature,
and installed `0.2.2-0.1.fc44.x86_64`. Repository preparation completed, but the
workstation transaction stopped at `gcc-c++` provider resolution. DNF listed
the explicitly requested package under `Installing dependencies`; Nimbus only
matched `Installing` rows. Desktop packages and the dotfiles/tools stage did
not complete.

- [x] Match direct names and evidenced providers across all three installation
  sections. Preserve exact provider identity checks and report a selected
  dependency's unexpected repository.
- [x] Reproduce the failure with the VM's reduced DNF preview and extend the
  provider, multilib, receipt, and deselection tests to dependency sections.
  The regressions fail before the fix and pass afterwards with Go 1.26.7.
  `go test ./internal/plan ./internal/apply` and `just check` pass.
- [x] Stage an isolated candidate and validate its definitions. Against the
  same installed checkout and cached VM metadata, released 0.2.2 exits 1 with
  an incomplete plan; the candidate exits 0 with a complete plan and all 118
  package requests resolved, including `gcc-c++.x86_64`.
- [ ] Retry installation with the corrected engine, then verify convergence
  and complete the remaining desktop and recovery drills.

Candidate base: `406f47a1e90a` plus the local package-resolution fix.
Binary SHA-256:
`1df16ed0109c6460a468affff4c334effc27c569a4d57e36a53b2237e5269f0e`.
Staging and read-only validation did not replace the installed engine,
checkout, or selector, or apply the workstation plan.

### Installer prompt polish carried from Phase 5

- [x] Make the already-key-verified isolated DNF engine download noninteractive.
  Use `git-core` for initial transport instead of the full Git package.
- [x] Remove the confirmed `Proceed?` prompt from init by default, preserving
  full plan rendering and ordinary sync confirmation. Accept existing `--yes`
  commands without requiring the flag.
- [x] Require `--machine ID` or reuse an existing trusted selector instead of
  opening a machine picker. Keep `--new ID` explicitly interactive. Refuse
  unexpected checkout/origin changes without prompting or replacing trust.
- [ ] Verify the final prompt count/order on a clean VM: sudo authentication
  and any Chezmoi questions, without an init confirmation or implicit picker.
  Local tests do not prove native UX.

### Remaining Phase 6 work

- [x] Implement remaining service/group/file ownership, repository/remote
  retirement, system Flatpak lifecycle, and trigger verification/deduplication.
- [x] Deliver `nimbus files accept /etc/PATH` with reverse diff, approval,
  proven ownership, changed-input refusal, and atomic source replacement;
  verify that the live target is untouched and no Git commands run.
- [x] Evaluate the [workstation-default candidates](ROADMAP.md#workstation-defaults)
  against Fedora's shipped defaults. Record each retained default, accepted
  override, or deferred candidate with its reason and machine scope.
- [x] Declare Docker's native rotating `local` log driver through the managed
  file lifecycle and planned restart; retain other defaults pending evidence.
- [ ] Verify effective defaults, Docker's running logging policy and a new
  container, and selected laptop services on matching hardware. Prove removal
  and recovery on the VM before closing the phase.

### Unpublished candidate and validation

- [x] Replace destructive `vm-push` with `just vm-stage`: isolated source
  snapshot, candidate binary/checksum, fresh private destination, and no
  stable binary, checkout, selector, or COPR replacement.
- [x] Use Claude Code `claude-fable-5-1` for recovery-file writing and scoped
  validation alongside native subagents and primary review.
- [x] Complete the full Fable review, integrate findings, and pass the final
  `just check` gate plus local candidate build/validation.
- [x] Stage the candidate and complete its first installation after owner
  authorization; follow [the candidate guide](../tools/vm/README.md).
- [ ] Complete the graphical and ownership-recovery drills. A direct binary drill
  does not validate RPM distribution. A separate beta COPR can test that
  later without altering stable publication.

Local validation uses Go 1.26.7: `just check`, resource/CLI race tests, and a
static candidate build that validates desktop, laptop, and VM definitions.
Fable's full review produced five findings, all addressed and covered by
focused checks: unrelated Docker restarts, missing-unit retirement, partial
file-write diagnostics, deferred-receipt failure reporting, and the internal
helper's displayed arguments. Further regressions cover activation after
`files accept`, retry after a newly associated trigger fails, and greeter
removal that retains runtime data without rerunning its creation trigger.

Native package inspection and Lua syntax checks support the recovery session;
the generic desktop-file validator rejects its session-specific `DesktopNames`
key. Only the graphical VM drill can establish session discovery and login.

### Candidate installation, 2026-09-08: before reboot

The owner ran the unpublished candidate based on `e3298e5efc3f` in the clean
Fedora 44 VM. Its binary SHA-256 is
`5977076cd408474f8a87c885498758986a759b0a8cb4833a8cf8ef46cc1fe7e4`;
definition digest is
`sha256:61210719235d4fa7c0ece12a8ebc64711af483091e88887113314c3845864494`.
The launcher completed with status 0 in 7m56s, including operator input:
selection 22.637s, system installation 6m23.124s, dotfiles/tools 51.949s.
All 20 Mise tools installed; Typst is a Terra RPM. Four Fedora mirror 404s
recovered, and all four affected packages are installed. DNF's local-package
OpenPGP warning concerns the RPM Fusion release RPM; Nimbus checked its pinned
archive checksum and bundled key fingerprint before installation.

Read-only status and `sync --plan --no-upgrade` report all 141 desired packages
satisfied, 160 managed operations unchanged, no pending or blocked changes,
no prune candidates, and no cached updates. All three Flatpaks are installed.
The seven system files match content and metadata. Docker/containerd,
Tailscale, Avahi, and CUPS activation pass; Docker reports the `local` log
driver and a fresh SSH session has Docker group membership. State schema is 2.

Greetd is enabled through the display-manager alias with the intended config;
its state directory is `greetd:greetd` mode 0750 and the next boot target is
graphical. It has not started yet, as intended before reboot. The actual
Hyprland 0.56.2 verifier accepts both normal and recovery Lua configurations.
Doctor passes 32 of 34 checks: Secure Boot remains disabled, and graphical
login awaits reboot. The pre-existing failed `mcelog` unit remains separate.
Audio and portals are installed; their session behavior remains untested.

`validate`, status/list JSON, `managed`, `unmanaged`, package/resource `why`,
`files accept --plan`, and `dotfiles diff` exit successfully. Chezmoi
`verify --exclude=scripts` passes; its remaining diff is the rerunnable Mise
script. Logs have a 0700 directory, 0600 files, stage timings, and closing
setup notes. This proves pre-reboot convergence, not a second mutating sync,
graphical login, file-capture mutation, or destructive ownership recovery.

### Candidate reboot and desktop, 2026-09-08

The owner confirmed successful greeter login. Read-only inspection verifies
kernel `7.1.13-200.fc44.x86_64`, enabled/running greetd, the graphical boot
target, and an active Hyprland Wayland session. A fresh
`sync --plan --no-upgrade` reports 160 managed operations unchanged. Doctor
passes 33 of 34 checks; Secure Boot is disabled. No user units have failed;
the pre-existing `mcelog.service` failure remains separate. PipeWire and
WirePlumber run, but audio and portal functionality still need manual tests.

The first desktop use prompted to create a "Default keyring". The installed
`gnome-keyring` package did not supply `pam_gnome_keyring.so`, although the
packaged greetd PAM stack already references it for authentication and session
startup. The desktop component now selects `gnome-keyring-pam`. This package
addition has not yet been applied to the VM. The existing default keyring
requires a separate migration/unlock check; never delete it or weaken its
password protection to hide the prompt.

- [x] Confirm reboot into Noctalia and normal Hyprland login.
- [x] Record post-reboot native health and read-only plan convergence.
- [ ] Test creation and automatic unlocking of the login keyring on a fresh
  password login, then logout/relogin; inspect the existing default keyring
  without reading or publishing its contents.
- [ ] Verify keyring dialog colors after the Noctalia GTK integration applies.
- [ ] Test file-picker and screen-sharing portals and actual audio playback.
- [ ] Complete recovery-session, failed-activation/removal, file capture, and
  second mutating-sync drills retained above.
- [x] Test the signed 0.2.0 COPR engine on the disposable VM; see the
  published-install follow-up below.

The owner authorized publication with these residual tests visible. Normal
login success does not establish recovery-session independence or a restore
claim. Noctalia preferences and application theme integration belong to the
separate dotfiles repository; its root `NOCTALIA.md` records the mapping.

### Published 0.2.0, 2026-09-08

[Nimbus PR #14](https://github.com/Furyfree/nimbus/pull/14) and
[dotfiles PR #17](https://github.com/Furyfree/dotfiles/pull/17) merged after
passing their final CI. No hosted review was requested. Nimbus's final local
CodeRabbit review had no findings; dotfiles' test-helper finding was corrected
and its full gate passed. Fable's final scoped follow-up hit its session limit;
primary validation continued with the existing independent review evidence.

[Release v0.2.0](https://github.com/Furyfree/nimbus/releases/tag/v0.2.0) targets
`12781bcc2673123b0f97dd7b4f9ae1216d648815`. Its full release gate and offline
vendored build/tests passed. Archive SHA-256:
`b52ecbab8e3b7d197ba449f986c062cf7c3fcf4c4e801be33d46a3037ed5adc2`.
[COPR build 10961922](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10961922/)
succeeded for Fedora 44 x86_64 with build networking disabled. The SRPM's
source archive matches the release byte for byte.

Native RPM verification with only the pinned project key passed signatures
and digests. The RPM has no scriptlets and contains only the engine and license
notices. Its extracted engine reports `nimbus 0.2.0` and validates all three
machine definitions in an isolated, unprivileged container. The final VM
manifest has 142 packages, including the added keyring PAM dependency. This is
artifact verification, not a new VM installation or keyring/login drill.

Phase 7 remains uncommitted in its existing separate worktree. Merged topic
branches were removed; the unrelated detached Ghostty worktree was preserved.

### Published-install follow-up, 2026-09-08

The signed `0.2.0-0.1.fc44.x86_64` engine completed the fresh VM installation
with status 0 in 459 seconds. After reboot the VM runs
`7.1.13-200.fc44.x86_64`, greetd is active, and PAM reports that the login
keyring started and unlocked successfully. The owner saw no keyring prompt.
The subsequent plain-Hyprland logout/relogin also unlocked the keyring.
Password-change and passwordless authentication remain separate tests.

The remaining light application windows expose incomplete toolkit adoption.
Fedora 44 cache-only repoquery verifies `adw-gtk3-theme` and `qt5ct`; both are
now declared alongside the existing Qt6ct package in `hyprland-noctalia`.
Chezmoi owns GTK and Qt settings for that profile, while Noctalia owns generated
palettes. The `hyprland-guiutils` warning occurred despite the package being
installed; verify executable discovery rather than adding a duplicate entry.

- [x] Verify the native GTK theme and Qt5 configuration package providers.
- [x] Record successful kernel, greetd, and password-login keyring activation.
- [ ] Verify Chezmoi's toolkit settings and all selected application theme
  consumers on a fresh login, including Brave Origin and dark/light changes.
- [ ] Research native Hyprland session integration and UWSM before adopting
  either lifecycle policy. The owner deferred UWSM on 2026-09-09; its proposed
  package selection, greeter default and doctor requirement were removed.
- [ ] Test portal activation, orderly logout/relogin, compositor-failure cleanup
  and the independent recovery session. Plain Hyprland remains the release
  session; its inactive graphical target is a known limitation.
- [ ] Verify the Hyprland dependency warning is absent after session startup.
- [ ] Complete the promptless-init VM drill described above before publication.
- [x] Replace the recovery session's Foot dependency with Ghostty, bypassing
  user terminal, shell, and GTK configuration. The VM terminal smoke test
  opened Bash, wrote its success marker, and exited with status 0. Full recovery
  login with broken user configuration remains untested.
- [x] Declare removal of Foot, Kitty, and nwg-panel. RPM metadata identifies
  Kitty as a Hyprland recommendation and nwg-panel as a Hyprland supplement.
  The VM's DNF removal preview removes exactly those three packages with
  automatic dependency cleanup disabled.
- [x] Apply the replacement recovery configuration and remove those packages
  through the unpublished candidate on the VM (2026-09-09).

The final candidate apply completed with status 0. It installed
`adw-gtk3-theme` and `qt5ct`, configured the UWSM greeter default, replaced the
recovery terminal, and removed Foot, Kitty, and nwg-panel. Noctalia's native
GTK hook selected `adw-gtk3-dark` and `prefer-dark`; the owner confirmed the
result works. Selected Chezmoi desktop files had no drift. A subsequent
`sync --plan --no-upgrade` reported 163 managed resources unchanged.

The owner selected plain Hyprland after login, so the graphical session target
and desktop portal remain inactive. UWSM login, portal interactions, recovery
login, failure/removal drills, and the clean-init prompt drill remain pending.
The owner subsequently deferred UWSM; the release retains plain Hyprland.
Do not mark these checks passed from the successful package/configuration apply.
The candidate binary SHA-256 is
`1faf2992f120df6e23f0520df22b82c6f305fc32ebcd69e110da953e823016cd`.
Candidate metadata and the apply log are retained privately outside the VM.
The owner authorized merging the reviewed follow-up and publishing 0.2.1
through COPR, with the deferred session research and remaining tests recorded.

Google Maps and FotMob are still intentionally excluded by Chezmoi. Their
prepared desktop entries call `nimbus launch webapp`, which the installed
0.2.0 engine and current source do not implement. They cannot be enabled until
the Phase 8 launcher is available; this is not a Noctalia discovery problem.

## Next phase: 7. Application delivery and unified updates

Plan: [First milestone](ROADMAP.md#first-milestone-application-delivery-and-unified-updates).
Runtime implementation is deferred; Phase 6 VM and recovery gates remain open.

- [ ] Reconcile the existing Phase 7 branch with the accepted direction.
- [ ] Define and implement the official GitHub Copilot RPM lifecycle: trusted
  artifact verification, release discovery, approval, native identity,
  receipts, retry, removal, and downgrade refusal. Keep read-only paths offline.
- [ ] Retain ChatGPT's official OpenAI repository and verify update ownership.
- [ ] Add Voxtype source/version/build handling in the COPR repository; verify
  a signed Fedora 44 x86_64 build and pin the accepted repository key before
  selecting it in Nimbus. Publication remains a separate authorized action.
- [ ] Resolve Voxtype dependencies, user configuration, typing backend, and
  actual daemon unit with the dotfiles handoff.
- [ ] Make Chezmoi-owned Topgrade configuration invoke Nimbus first on managed
  hosts, then the explicit native user-manager allowlist. Cover standalone
  hosts, disable duplicate system steps, and prevent recursion.
- [ ] Test cancellation, Nimbus fail-stop, user-step continuation with final
  failure status, and no privileged user updates against native Topgrade.
- [ ] Pass focused lifecycle tests and every changed repository's local gate,
  then an approved disposable Fedora VM install/update/removal/retry trial.
  Record recovery limits; do not close the separate Snapper restore gate.

## Phase 5 evidence

Plan: [Bootstrap, initialization, and Chezmoi
handoff](ROADMAP.md#5-bootstrap-initialization-and-chezmoi-handoff).
Phases 1 to 4 are merged. Phase 4's checklist is in the evidence section.
The following records retain Phase 5 implementation and validation history.

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
All three repositories are public and their integration PRs are merged. The
manual `v0.1.0` release and signed COPR build passed; the verified public key
is supplied by this checkout. The clean one-liner drill with the COPR engine
completed. The 0.1.1 follow-up also passed package and user-configuration
installation on the recovered VM; graphical login remains a Phase 6 gate.

### Installation follow-up, 2026-09-07

- [x] Remove redundant Git, GPG, and engine DNF yes prompts; supervise one
  early sudo session through normal-user init and forward validated flags.
- [x] Default fresh Chezmoi SSH integration to false without a question;
  preserve stored opt-ins and support explicit `--onepassword-ssh`.
- [x] Move five CLI tools to verified native Mise release backends, remove
  `cargo-update`, and retain Terra Typst as its sole desired provider.
- [x] Reconcile transaction-created duplicate repositories before subsequent
  upgrades and at completion, without changing vendor files or signing keys.
- [x] Add private bootstrap, engine, and Mise logs, command/stage timings,
  bounded retention, and explicit secret-output exclusions.
- [x] Complete integrated validation and the requested Fable 5.1 review.
- [x] Publish engine 0.1.1 and reviewed definitions, rebuild COPR, and verify
  the recovered VM installation. The compatibility floor rejects 0.1.0.

Validation: `just check` passes in Nimbus with Go 1.26.7 and in dotfiles
(121 tests, three existing optional skips, plus the Bash gate). Nimbus also
passes `go test -race ./internal/cli ./internal/facts`. Real isolated Mise
installations and version checks pass for all five upstream tools. The actual
init regression excludes fake rendered secrets and native secret errors from
engine logs while preserving terminal diagnostics; the actual sync regression
requires duplicate overrides before upgrade and a clean subsequent plan.
Engine builds labelled 0.1.0 and 0.1.1 respectively reject and validate this
checkout, exercising the compatibility floor.

Claude Code's exact `claude-fable-5-1` model completed a full read-only review
and focused follow-ups alongside primary validation; the final verdict is
clean. Probe noise, missing native failure diagnostics, path normalization,
early failure logging, and integration-test gaps were addressed. Strict
refusal of symlinked log ancestors remains intentional. The delivery PRs are
merged. Hosted review found an ARM-only asset selection in dotfiles,
PID-only shell cancellation, and protected logs consuming retention slots.
All three findings are fixed and their threads resolved. Dotfiles, Nimbus,
and COPR CI passed the final PR heads and merged main commits.

The handoff supervisor uses system Python 3 to preserve the terminal and reap
cancelled descendants. Nine isolated regression tests pass locally and in a
Fedora 44 container, including nested cancellation, terminal input, Ctrl-Z,
background/foreground resume, initial background launch, and successful native
background services. Retention tests preserve 20 completed runs alongside
active or protected directories. `just check` and ShellCheck 0.9.0 pass.
The manual 0.1.1 release, COPR build, and installation evidence are below;
they do not validate graphical login or desktop recovery.

The previous VM run installed all system packages and 22 Mise tools. Native
DNF logs show three recovered mirror 404s; they did not leave those packages
missing. Observable timestamps span approximately 33 minutes, including the
source builds, without an exact complete-run transcript. The read-only plan
then requested two duplicate repository repairs from ChatGPT and 1Password
RPM scriptlets. New tests cover that convergence and the next run will have
native timing evidence. This describes the earlier run, before the recovered
VM retry recorded below.

### Initialization

- [x] Hardware facts: DMI product and board names, the chassis kind from the
  SMBIOS chassis type, and display adapters by PCI vendor and device, read
  from sysfs through the source.
- [x] Typed detection: `[detect]` on the hardware components, `hardware` on
  the tracked machines, `plan.ProposeComponents` with laptop, desktop, and VM
  fixtures. The old machine matcher was retired with the implicit init picker.
- [x] `nimbus init`: validates the checkout, reads its origin, uses an explicit
  machine or existing trusted selector, runs the new-machine dialog
  for `--new`, writes the manifest and the selector, syncs, hands off to
  Chezmoi once, and reports its configuration and tool installation together.
  A second user-only pass runs only for explicit Nimbus-owned declarations.
- [x] `install.sh` and `bootstrap`: platform and user checks, Git through
  DNF, clone or validate the checkout, init with a separately verified engine
  and its input on the terminal; shellcheck runs in `just check` and CI.
- [x] Publish the engine RPM channel and verify its signing-key pin before
  enabling automated engine installation. The manual `v0.1.0` release and
  signed COPR build passed; bootstrap verifies the supplied public key.
  Publishing 0.1.1 for the follow-up changes remains tracked above.
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
- [x] Preserve native RPM architecture identity and conservative legacy
ownership;
  compare install, removal, and upgrade transactions before and after execution.
- [x] Verify actual active repository keys; reconcile DNF trust and refuse
unsafe
  existing Flatpak trust changes.
- [x] Document public HTTPS bootstrap as the intended path. GitHub visibility
  has not been changed by these local edits.

### Validation

- [x] Tests: hardware parsing, proposals and matches, the init dialog with
  the picker and prompt hooks, the handoff command, the doctor check, the
  user-scope planner and executor.
- [x] Run `just check`.
- [x] Recovered-VM public one-liner reaches system sync, the Chezmoi handoff,
  and all selected user tools with the signed 0.1.1 COPR engine. The stale
  checkout and manually built engine were preserved before retrying.

### Manual source release

- [x] Add a manual-only workflow accepting an existing stable version tag on
  main. Test the tagged source and generate the vendored archive, source
  identity, module inventory, and checksums in temporary storage.
- [x] Separate read-only build/test permissions from draft creation; require
  manual publication and a separate COPR dispatch. Preserve existing releases.
- [x] Document the operator steps and first signing-key/VM prerequisites in
  [the release guide](../tools/release/README.md).
- [x] Exercise the hosted workflow after merge with the first approved tag,
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
- [x] Create the COPR project, verify its real public signing key, publish the
  approved release source, and obtain a successful signed engine build.
- [x] Ship the verified COPR key and fingerprint, then repeat the public
  one-liner from the restored VM without a manually supplied engine.

## Evidence

### Version 0.1.1 delivery and VM installation, 2026-09-08

[Nimbus 0.1.1](https://github.com/Furyfree/nimbus/releases/tag/v0.1.1) was
published from `58ba0d215f01ca5b1242536e01a9d8939a83d6bf` after the full gate
and vendored offline build/tests. The source archive SHA-256 is
`ce57889bbc3ed03569d76e469b0716161727ae0eb5fa77f09f9a3dccb50c109d`.
[COPR build 10958410](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10958410/)
passed. Its source RPM contains that archive; native signature/digest checks
against the pinned key passed, and the packaged engine reports 0.1.1 and
validates all three machine definitions. The RPM contains the engine and
license notices, with no scriptlets.

The recovered VM initially retained a modified Phase 4 checkout without
bootstrap and a manually built engine on PATH. Both were moved to a private
backup without discarding changes. The retry completed with status 0 in
7m02s: selection 8.5s, system installation 5m23s, and dotfiles/tools 57.8s.
All 140 desired packages, three Flatpaks, and 20 Mise tools were present.
Typst came from Terra; no Cargo tool declarations remained. Status and the
read-only plan reported no pending/blocked changes, repairs, or cached updates.
All 17 mirror 404s recovered, with affected packages confirmed installed.
Setup notes appeared at completion; the run directory was 0700 and logs 0600.

Doctor passed nine of ten checks; Secure Boot was disabled in the VM.
The pre-existing mcelog failure reports an unsupported AMD CPU and predates
installation. The user later reported no Noctalia greeter after boot.
Package delivery therefore passed, but graphical login and desktop recovery
remain unverified. The last attempted SSH inspection was refused; the exact
live greeter failure has not been diagnosed. Prompt count and timing remain
the Phase 6 installer prompt tasks above.

### First source release and signed COPR engine, 2026-09-07

Dotfiles PR 15, Nimbus PR 9, and COPR PRs 1-2 are merged. Their final heads
passed CI. The manual Nimbus release workflow `34155168739` passed its full
local gate and offline vendored build, then created the reviewed `v0.1.0`
draft. Its published source archive comes from
`e1c081bb31ddc8d9c3823a8859b43c6188fe50d3` using Go 1.26.7, with SHA-256
`54e113b54c11d09def622c639a59e4ad7d30378020e3bd8d77c57c4072950808`.
The archive retains 23 dependency notices and the separate Unicode data notice.

COPR workflow `34155340456` submitted build `10958015`, which succeeded for
Fedora 44 x86_64 with network access disabled. The hosted source RPM contains
the exact published source archive. The binary RPM reports `nimbus 0.1.0`,
contains only the engine and license notices, and has no install scriptlets.
Native RPM verification with `_pkgverify_level all` in an isolated keyring
reports `digests signatures OK` using the official project's public key:
`8FF8 E546 C3AB E441 46CF A411 DD1D 48E2 CA0E 9F6A`. Repository metadata is
published. The public key and fingerprint are now supplied by the checkout;
the source-release tag remains unchanged. A normal-user Fedora container
successfully downloaded the engine with bootstrap's isolated DNF repository
settings, verified its signature with the shipped pin, and used that binary
to validate this checkout. No package was installed by this check.

The subsequent public one-liner completed on the restored VM. The temporary
`~/start-phase5` launcher was removed; it is not part of the supported entry
path. See the installation follow-up above for the new drill requirements.

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

At that preparation checkpoint, the restored VM held no receipts or Chezmoi
state. Its
old manual engine and checkout were moved, without deletion, to
`~/phase5-pre-copr-20260907T180840Z/`. The temporary `~/start-phase5` launcher
checked
that COPR metadata exists, then runs the public installer through a terminal
with output-only logging, exit-code propagation, and elapsed time. It was
syntax-checked but not run. It was later removed before the successful public
one-liner drill; it is
not required by the supported installer.

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
- Dotfiles `just check` completes 114 Python tests with three native/opt-in
skips
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
- D-018's 2026-09-09 amendment makes Topgrade the planned update entry point,
  calling Nimbus for system updates before its allowed native user managers.
  Chezmoi owns the conditional configuration. Dry-run shows commands rather
  than resolved downstream versions; recovery excludes home mutations.
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
- Source retirement now requires proven native ownership and no remaining
  consumers. Legacy receipts without that evidence block retirement; an
  adopted package can still need the source for updates. Native removal and
  retry behavior remain disposable-VM gates.
- The VM installation exercised selection, but its prompt order and redundant
  confirmation remain unresolved in the Phase 6 installer prompt tasks above.

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
  the installation evidence above covers the selected native sources.
- User-scope steps support `[installer]` and `cargo:` references. The current
  dotfiles tool configuration uses release binaries instead of Cargo builds.
  Service integration is implemented locally; native activation and recovery
  remain Phase 6 VM gates.
- Definition validation checks armored key-file form; planning checks observed
  active trust and execution verifies downloaded or declared keys before import.
- Development engine builds (`0.0.0-dev`) skip the `min_engine` comparison;
  release builds enforce it.

- The current repository declarations include their key URLs or files and pins.
  Native reconciliation passed the 0.1.1 VM run; adverse repair and ownership
  retirement paths still need their own disposable-VM evidence.
- A maker's installer script cannot be pinned to a version; Nimbus downloads
  it, shows the file's digest, and runs that file.
- Exact DNF5 constraint and desktop-session update behavior remains deliberately
  outside this phase.
- Exact hardware probes belong to Phase 5 and Topgrade orchestration belongs to
  Phase 7; neither may leak system inspection or mutation into Phase 1.

## Completion rule

This work stops before commits, pushes, or publication. VM testing is now
authorized; its remaining gates are recorded above. Complete
the first Phase 6 milestone only with the graphical login and recovery evidence
listed above; package installation alone is insufficient. Close the full phase
after the remaining resource ownership, `files accept`, selected-default, and
installer prompt gates pass. Native system ownership requires verified
receipts; user tools retain their explicit no-receipt lifecycle.
