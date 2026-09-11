# Current work

Contract: [SPEC.md](SPEC.md). Order: [ROADMAP.md](ROADMAP.md).
The cleanup is merged and released as
[v0.3.0](https://github.com/Furyfree/nimbus/releases/tag/v0.3.0).
[Version 0.3.1](https://github.com/Furyfree/nimbus/releases/tag/v0.3.1) adds
the installation fixes below and is available in COPR.
The 0.3.1 existing-layout VM install and login passed. Version 0.4.0 releases
repository updates during sync, Zed installation, browser fallbacks and
reporting fixes. The signed Fedora 44 COPR package is available; its new
installation and reboot checks remain pending.

## Next: GRUB, then FDE auto-unlock

Follow the [priority order](ROADMAP.md#next-steps) before the shared-operation
refactor and TUI.

- [ ] Finish the minimal dark GRUB integration. Check appearance, Fedora-only
  and Windows-present menus, Fedora default, five-second timeout, older kernels
  and theme removal through actual boots.
- [ ] Inspect the Fedora/LUKS2 boot path, TPM and Secure Boot support; choose
  the native auto-unlock method and boot-change policy before implementation.
- [ ] Add explicit post-install preview and approval for FDE auto-unlock,
  preserving passphrase access, with status and enrollment-removal instructions.
  Unsupported setups must retain manual unlock.
- [ ] Test auto-unlock enrollment, booting, passphrase fallback, boot-change
  fallback and removal on real hardware. This scoped test precedes the TUI;
  it is separate from the later full desktop trial. No working claim yet.

## NVIDIA MOK helper

- [x] Add approved signing and enrollment to `postinstall nvidia-mok`, with
  read-only preview, native password prompts and preservation of existing keys.
- [x] Verify module signing identifiers, stop on failed builds or boot-image
  updates, preserve pending requests and distinguish enrollment from boot proof.
- [x] Confirm the manual native flow on the desktop: signed NVIDIA modules,
  MOK enrollment, reboot, working RTX 3080 and Secure Boot still enabled.
  Hyprland, Noctalia and all three portal services work after this repair.
- [ ] Test the new Nimbus helper itself on an installation needing enrollment;
  manual commands validate the approach, not the new implementation.
- [ ] In Chezmoi, skip NVIDIA Settings autostart when no saved
  `~/.nvidia-settings-rc` exists. Its failed settings restore does not mean the
  driver failed. Keep this separate from Nimbus's signing helper.

## Installer follow-up

- [x] Recover the Fedora installation guide and replace obsolete storage and
  recovery instructions. The old guide required the snapshot mount that
  engine 0.3.0 rejects; the VM followed that earlier layout.
- [x] Add reviewed reuse of empty, root-owned snapshot storage, with approval
  rechecks, native verification and retry after interrupted registration.
- [x] Publish the storage-reuse fix and machine prompt as Nimbus 0.3.1.
  Explicit machine arguments, selector reuse and separate approval remain.
- [x] Install 0.3.1 on the existing VM layout, reuse the empty snapshot mount,
  reboot into UWSM and inspect the result.
- [x] Restore the full installation layout in three focused tables and keep
  filesystem labels distinct from subvolume names.
- [ ] Test a fresh installation of the documented layout, setup without
  pre-created snapshot storage, native snapshot pairs, cleanup, retention
  and restoration. Updating the checkout is not an engine upgrade.
- [x] Correct the Secure Boot test-VM exception and stale sync session notes.
- [x] Guard LibrePods autostart on Bluetooth adapter presence in Chezmoi.
- [x] Record the owner's VM-only `mcelog` disablement; no Nimbus workaround.
- [x] Deliver the Nimbus reporting fixes in 0.4.0.
- [ ] Verify the matching Chezmoi configuration and next login.

## Repository updates in sync

- [x] Add clean-repository checks and fast-forward updates for Nimbus and the
  configured Chezmoi source; report paths and repair advice without discarding
  local work.
- [x] Run system reconciliation from updated definitions, then offer Chezmoi
  apply and shared-profile refresh while preserving the 1Password SSH choice.
- [x] Run Topgrade before combined sync and start a fresh engine afterwards.
  Keep standalone upgrades and internal system reconciliation separate.
- [x] Keep previews local and read-only; document approvals, failure stages
  and the distinction between checkout updates and engine updates.
- [x] Pass `go test ./internal/cli ./internal/checkout`, `just check` and
  `just validate`. Temporary repositories exercise fast-forward updates and
  local-work protection; CLI tests cover failure stages, read-only previews,
  profile refresh and starting the replacement executable. No VM test yet.
- [x] Complete local CodeRabbit review. Its JSON restriction finding was
  already enforced by the command entry point; extend the existing regression
  test to cover combined sync, including `--yes` and `--plan`.
- [x] Publish the new engine source as Nimbus 0.4.0.
- [x] Verify the signed 0.4.0 COPR package and its source archive.
- [ ] Test repository updates, Chezmoi apply, dirty-tree errors and the
  upgraded-engine handoff in an installed system with 0.4.0.

## Release 0.4.0

[Release preparation](https://github.com/Furyfree/nimbus/actions/runs/34617967413)
and [COPR publication](https://github.com/Furyfree/copr/actions/runs/34618451367)
passed. [Build 10976629][copr-040] produced `nimbus-0.4.0-0.1.fc44.x86_64`.
The source archive matches all 254 tagged files and executable bits at
`dde39045b9276281fd5338aac885a27ce5a96324`. Dependencies and bundled license
notices are unchanged from 0.3.1. Archive SHA-256:
`a8cdf5b06bf59742d6bdaec529d625105f806299de91d5f45cbd5e8ae8c9b720`.

Nimbus `just check` and `just validate`, GitHub vendored build/tests, and COPR's
offline `just check-container` passed. Local and published SRPMs preserve the
exact archive. RPM signatures and digests passed against only the pinned COPR
key; the payload contains the engine and license notices, with no scriptlets.
The extracted engine reports 0.4.0 and validates all three machines in an
unprivileged, network-disabled Fedora container. No VM or desktop installation
was performed during release verification. UKI/TPM auto-unlock is not included.

[copr-040]: https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10976629/

## Completed locally

- [x] Consolidate product docs into SPEC, TASKS and ROADMAP.
- [x] Separate sync and upgrades; add `sync --upgrade` and the narrow
  `upgrade --system` callback for Topgrade.
- [x] Add init, selection and upgrade previews; require init approval and
  explicit `--yes` for JSON mutation.
- [x] Remove dotfiles wrappers; retain initial handoff and profile refresh advice.
- [x] Remove custom Copilot/WoWUp providers and exclusive tests. Preserve old
  application receipts without automatically removing applications.
- [x] Add Copilot's native post-install action and Topgrade updater.
- [x] Remove unused Cargo and installer follow-up lifecycles; keep Mise bootstrap.
- [x] Remove graphical recovery and the old layout inspection.
  Retain ownership-checked retirement of the old session files.
- [x] Restore Snapper through `hyprland-noctalia`: native root configuration,
  sync/system-upgrade pairs, six-snapshot retention and automatic cleanup.
- [x] Review bootstrap supervision and keep cancellation, terminal and logging
  behavior. Correct approval help and populate the cache before first init.
- [x] Group large planner/executor files by topic; separate CLI initialization,
  handoff and reporting. Rename `tasks` to `postinstall`, move repository-tool
  tests into `tests/integration/`, remove empty directories and document the
  package map in README. Preserve behavior and existing tests.
- [x] Separate `native` execution and test support from `inspect`; use focused
  verification queries in `apply`. Include new integration tests in candidate
  staging.
- [x] Fix interrupt finalization, remove orphaned package constraints within
  selection previews, and report Snapper setup/settings drift in status.
- [x] Require engine 0.3.0 for the new definitions and document upgrade order.
- [x] Share default-browser selection, installed fallbacks and private flags
  across browser and webapp launchers, including Brave Origin.
- [ ] Check regular, private and webapp windows in an installed session,
  including private webapps. Local tests verify arguments, not browser UI.

## After boot work: shared operations

- [ ] Extract reconciliation from Cobra, preserving approval and shared locks.
- [ ] Move setup, selection changes and file capture to their operation owners;
  keep prompts and rendering in CLI. Complete post-install execution ownership.
- [ ] Replace execution's description parsing with explicit approved data.

Start these after the GRUB and FDE auto-unlock steps above. The existing
native/inspection split does not complete the CLI/TUI operation boundary.

## Before the desktop trial

See [integration](ROADMAP.md#3-finish-workstation-integration) and
[delivery](ROADMAP.md#5-prepare-the-desktop-trial).

- [ ] Add standalone install/update commands to the WoWUp COPR helper, then
  connect the small post-install action and Topgrade updater. No artifact
  adapter in Nimbus.
- [ ] Confirm published helper interfaces and package sources. Enable WoWUp's
  gaming selection after its helper and repository key are available. Native
  signatures stay enabled; no separate audit of the owner's package contents.
- [x] Confirm matching Nimbus/Topgrade delivery in the installed VM: engine
  0.3.1 and a custom `nimbus upgrade --system` step are present.
- [ ] Verify installed update/constraint behavior: allowed family updates,
  compatible dependencies and rejected conflicts.
- [ ] Test TTY repair with absent/broken dotfiles and legacy session retirement.
- [ ] Test native Snapper setup, failed updates, repeat sync and retention on
  installed Fedora. Check root/boot coverage with an actual restore drill;
  local fake-tool tests do not prove restoration.
- [ ] Test managed-file acceptance/removal, service/group restoration, failed
  activation, receipt retirement and relevant legacy-state compatibility.
- [ ] Verify post-install actions and browser/webapp integration; enable waiting
  Chezmoi entries after compatible delivery.
- [ ] Check clean init, cancellation, retry, keyring setup and repeat sync.
  Test affected native defaults, not every application's features.
- [ ] Build the [dashboard](ROADMAP.md#4-build-the-dashboard) over shared
  operations; check CLI parity, keyboard use, resizing and cancellation.
- [ ] Run candidate RPM/VM installation and update checks. Release and COPR
  publication require separate authorization.

Additional root settings are optional: research Fedora/upstream first, then
compare Omarchy or CachyOS-Settings. Adopt only justified system changes;
Chezmoi owns user files.

## Hardware-only checks

These wait for a proper installation and do not block independent local work.

- [ ] GPU, suspend/power, audio, Bluetooth/Librepods, fingerprint and NVIDIA MOK.
- [ ] Portal file picking/screen sharing, UWSM logout/relogin and failure
  cleanup. Change startup only for a reproduced problem.
- [ ] Appearance, keyring unlocking and application integration; recheck
  Fastmail email links and notifications.
- [ ] Network Epson ET-5800 printing, trays, duplex and feeder scanning.
- [ ] Voxtype on laptop and RTX 3080 desktop; choose model/backend after the
  owner's Omarchy comparison and check required input permissions.

## Deferred

The [roadmap](ROADMAP.md#deferred-beyond-the-desktop-milestone) retains custom
boot archives and automatic restoration, UKI generation, broader boot-key
management, hibernation, Windows VM commands, Home Assistant, optional
desktops and measured performance work. They are not desktop release gates. AI
Usage needs no separate Nimbus implementation. FDE auto-unlock is now active
work, not deferred.

- [ ] Revisit browser/webapp launching through UWSM; check terminal independence,
  session environment, logout cleanup and operation outside UWSM. See the
  deferred roadmap entry.

## Evidence and limits

2026-09-10 storage fix: focused Snapper/CLI tests, `just check` and
`just validate` pass. Tests cover empty-mount reuse, unsafe or populated
storage, changed approval, interrupted writes/verification and convergence.
The later VM trial below covers existing-storage setup, not restoration.
Local CodeRabbit review covered all 14 changed files and returned no findings
on its second run. The first run's request to show the actual snapshot
subvolume and filesystem in the approval plan is addressed.

2026-09-10: Nimbus `just check` and `just validate` pass. The gate includes
formatting, vet, Go tests, generated GRUB assets, Markdownlint, ShellCheck and
diff checks. Tests cover separate/combined updates, native failure propagation,
read-only previews, explicit approval, legacy receipt preservation and helper
invocation. Bootstrap tests use fake package tools and temporary paths.

Snapper tests cover native setup/settings calls, ownership and registration,
read-only previews, unchanged sync, approval rechecks, and before/after/cleanup
failures without losing an update error. Actual Btrfs retention and restoration
remain untested; no live snapshots were created or removed.

Release preparation includes subprocess interrupt regressions, preservation of
completed receipts on cancellation, and selection/status regressions. The
local review's three findings are fixed. CodeRabbit could not review the full
change because it exceeded its file limit; the local Codex review supplied the
full review, followed by focused verification of the fixes.

Dotfiles `just check` passes: 130 Python tests, three skips, plus Bash and diff
checks. Its Topgrade tests run the real parser with fake updaters. The skips
are unavailable Hyprland, opt-in Neovim plugin integration and opt-in Zathura
GUI checks. Isolated Chezmoi previews passed; verify reports expected drift
against an empty temporary home. No live configuration was applied.

The terminal handoff's nine Python tests pass, including cancellation and job
control. Native DNF solver, VM and hardware checks were not run. Previous
0.2.3 VM login/repeat-sync observations do not close these gates.

[PR #20](https://github.com/Furyfree/nimbus/pull/20) merged with passing CI.
The v0.3.0 release workflow passed its checks and offline vendored build/tests.
Archive checksums passed; all 246 tagged files and executable bits match the
merged commit. Module versions and license notices are unchanged from 0.2.3.

[COPR packaging PR #10](https://github.com/Furyfree/copr/pull/10) merged after
the complete offline Fedora gate and CI passed. Source-RPM preparation passed
and retained the exact release archive. Local CodeRabbit review of the recipe
and release notes completed without findings. The
[publication run](https://github.com/Furyfree/copr/actions/runs/34504051972)
tracks the signed Fedora 44 build. The later 0.3.1 VM trial below supersedes
this release's pending installation evidence.

Release 0.3.1: [source preparation](https://github.com/Furyfree/nimbus/actions/runs/34511660409)
and [COPR publication](https://github.com/Furyfree/copr/actions/runs/34512058767)
passed. [Build 10972507](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10972507/)
succeeded for `nimbus-0.3.1-0.1.fc44.x86_64`. All 249 tagged files match the
reviewed source archive, and the prepared SRPM preserves that archive exactly.
The signed binary RPM passed native signature/digest verification against only
the pinned project key. Its extracted engine reports 0.3.1 and validates all
three machines in a network-disabled Fedora container. It has no scriptlets.
Nimbus `just check`/`just validate` and COPR's full offline `just check` passed.
The release verification did not change the VM; the owner then installed it.

2026-09-10 VM trial: installer exit 0 in 394 seconds with engine 0.3.1 and
checkout `9783b59c69ca`. All 167 managed items are unchanged, with no pending
or blocked operations. Chezmoi applied successfully and all 28 Mise tools
installed; offline inspection reported none missing. Topgrade's system step
calls `nimbus upgrade --system`; the upgrade itself has not been tested.

After reboot, greetd, UWSM-managed Hyprland, Noctalia and the Hyprland/GTK
portals are running. Hyprland reports no config errors; the keyring unlocked.
PipeWire sees audio input/output, but playback and screen sharing remain
untested. Native Snapper reads the reused mount and registered config, with
six-snapshot retention and no timeline/boot snapshots. Its list contains only
`0 / current`; creation and cleanup have not yet been exercised.

The owner disabled the preexisting, unsupported `mcelog` service on this VM
and cleared its failure; there are no failed system units. The Bluetooth-less
VM exposed LibrePods' unconditional startup. Doctor's Secure Boot wording and
unchanged sync's reboot/logout notes are corrected locally, together with the
Chezmoi startup guard. These changes are not installed in the VM.

Local follow-up validation: Nimbus `just check` and `just validate` pass;
CodeRabbit reviewed all ten changed Nimbus files with no findings. Existing
test files cover VM detection/exception boundaries and session notes for
changed, adopted, unchanged and blocked resources. Dotfiles' isolated
`just check` passes all 130 tests with three skips: unavailable native Hyprland,
opt-in Neovim downloads and opt-in Zathura GUI checks. Isolated Chezmoi
managed/status/diff pass; verify reports expected drift against an empty home.
No apply was run. The startup guard is exercised with zero, one and multiple
Bluetooth adapters; the next real login remains untested.
