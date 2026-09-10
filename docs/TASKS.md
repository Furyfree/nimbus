# Current work

Contract: [SPEC.md](SPEC.md). Order: [ROADMAP.md](ROADMAP.md).
The cleanup is merged and released as
[v0.3.0](https://github.com/Furyfree/nimbus/releases/tag/v0.3.0).
Version 0.3.1 adds the installation fixes below; publication is in progress.
Existing installation evidence does not validate this candidate.

## Installer follow-up

- [x] Recover the Fedora installation guide and replace obsolete storage and
  recovery instructions. The old guide required the snapshot mount that
  engine 0.3.0 rejects; the VM followed that earlier layout.
- [x] Add reviewed reuse of empty, root-owned snapshot storage, with approval
  rechecks, native verification and retry after interrupted registration.
- [ ] Publish the storage-reuse fix, then test the existing VM layout and a
  fresh root/home installation. Local tests do not prove Btrfs/SELinux behavior.
- [ ] Deliver the first-install machine prompt and the minimal README command.
  Keep explicit machine arguments, selector reuse and separate plan approval.
  The prompt is not in the published 0.3.0 engine.

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

## Next code cleanup

- [ ] Extract reconciliation from Cobra, preserving approval and shared locks.
- [ ] Move setup, selection changes and file capture to their operation owners;
  keep prompts and rendering in CLI. Complete post-install execution ownership.
- [ ] Replace execution's description parsing with explicit approved data.

These are subsequent changes; the native/inspection split does not complete
the CLI/TUI operation boundary.

## Before the desktop trial

See [integration](ROADMAP.md#3-finish-workstation-integration) and
[delivery](ROADMAP.md#5-prepare-the-desktop-trial).

- [ ] Add standalone install/update commands to the WoWUp COPR helper, then
  connect the small post-install action and Topgrade updater. No artifact
  adapter in Nimbus.
- [ ] Confirm published helper interfaces and package sources. Enable WoWUp's
  gaming selection after its helper and repository key are available. Native
  signatures stay enabled; no separate audit of the owner's package contents.
- [ ] Deliver the matching Nimbus engine before applying the new Chezmoi
  Topgrade configuration. The old engine lacks `upgrade --system`.
- [ ] Verify installed update/constraint behavior: allowed family updates,
  compatible dependencies and rejected conflicts.
- [ ] Test TTY repair with absent/broken dotfiles and legacy session retirement.
- [ ] Test native Snapper setup, failed updates, repeat sync and retention on
  installed Fedora. Check root/boot coverage with an actual restore drill;
  local fake-tool tests do not prove restoration.
- [ ] Test managed-file acceptance/removal, service/group restoration, failed
  activation, receipt retirement and relevant legacy-state compatibility.
- [ ] Finish the minimal GRUB integration. Boot Fedora-only and Windows-present
  layouts; check Fedora default, five-second menu, old kernels and restoration.
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
Chezmoi owns user files. Zed is not currently selected in Nimbus; configuration
alone does not install it.

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

The [roadmap](ROADMAP.md#deferred-beyond-the-desktop-milestone) retains
custom boot archives and automatic restoration, TPM/UKI automation, hibernation,
Windows VM commands, Home Assistant, optional desktops and measured performance
work. They are not desktop release gates. AI Usage needs no separate Nimbus
implementation.

## Evidence and limits

2026-09-10 storage fix: focused Snapper/CLI tests, `just check` and
`just validate` pass. Tests cover empty-mount reuse, unsafe or populated
storage, changed approval, interrupted writes/verification and convergence.
Native Btrfs and SELinux checks still require the installation trial.
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
tracks the signed Fedora 44 build; installed-package testing is still pending.
The matching Chezmoi/Topgrade changes remain local in the dotfiles repository.
