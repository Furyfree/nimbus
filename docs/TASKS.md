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

## ScrollOverview post-install setup, 2026-09-13

- [x] Select matching COPR `hyprland-devel` through the shared
  `hyprland-plugin-build` component. Its RPM requirements supply the build
  tools and libraries; Chezmoi owns plugin selection, settings and shortcut.
- [x] Confirm the user's local native installation: ScrollOverview is loaded
  on Hyprland 0.56.2 and Super+O works. The user completed authentication and
  installation after the initial failed dependency attempt.
- [x] Implement explicit `hyprland-plugins` setup with read-only inspection,
  preview, approval, ABI checks, native stage verification and retry.
  No privileged Chezmoi hook, custom cache writes or completion receipt.
- [x] Pass `just check`, all three machine definition checks and Chezmoi
  Hyprland profile/Lua/native-config tests. The read-only inspector reports
  complete against the live desktop cache and proposed selection, without
  applying it. The normal CLI preview requests the unapplied selection file.
  Tests cover fresh setup, partial failure/retry, native no-effect results,
  stale approval, foreign cache state, missing IPC and completed no-op.
- [ ] Test a fresh laptop installation through the new task after release.
  Local installation predates the task; fixture tests do not prove a fresh
  download/build on the laptop. Usage lives in README.

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
- [x] Run setup sync before Topgrade and a fresh-engine sync afterwards.
  Keep standalone upgrades and internal system reconciliation separate.
  The 0.4.2 change supersedes 0.4.0's upgrade-first order.
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

### NVIDIA settings-loader autostart

- [x] Attach a root-owned, zero-byte systemd user-unit mask to the NVIDIA
  component. RPM Fusion's `nvidia-settings` package owns the original XDG
  entry; masking its generated unit preserves the package file, graphics
  driver and manual settings application. Non-NVIDIA machines omit the mask.
  Activation is at the next login; this change does not reload or edit the
  live user's service manager.
- [x] Compare the installed Fedora package with systemd's native masking
  contract, [Omarchy's NVIDIA setup][omarchy-nvidia] and
  [CachyOS-Settings][cachyos-nvidia]. Neither reference supplies this Fedora
  autostart fix; no driver or performance settings were imported.
- [x] Pass `just check`, `just validate` and the read-only desktop plan.
  Isolated native systemd lookup recognizes the mask and restores the original
  unit after its removal. Laptop and VM definitions omit this resource.
- [ ] Verify installation and next-login behavior on the NVIDIA workstation.
  The user's existing per-user mask remains independent of Nimbus ownership.

[omarchy-nvidia]: https://github.com/omacom/omarchy/blob/31bd80daa4613ffdee995ac27467fce5a2990806/install/config/hardware/nvidia.sh
[cachyos-nvidia]: https://github.com/CachyOS/CachyOS-Settings/tree/4bcb3e3bf8a0c6763dbd935d14430c60d7ff5a37

### Zeron development integration, 2026-09-12

- [x] Select Zeron's official missing-binary installer and Fedora browser
  dependencies through the development profile. The existing workstation
  installation and both dependencies are already present.
- [x] Disclose native service creation, enablement, restart and user lingering
  before installation. Retain native ownership of updates, removal and state;
  preserve an existing installation without rerunning its installer.
- [x] Prepare Chezmoi's Linux development launcher/icon links and guarded
  `zeron update` step. Account sign-in remains an optional manual step.
- [x] Apply and verify the launcher, icon and updater configuration locally.
  Native desktop and GTK icon lookup pass after refreshing the user's icon
  index; Chezmoi now owns that refresh hook. The engine remains running.
- [x] Run Nimbus's full `just check` gate and isolated installer disclosure,
  profile selection, native updater and asset-link regression checks.
- [x] Release engine 0.4.3 before deploying definitions with installer effects.
- [ ] Exercise fresh Zeron installation and its service/linger effects in a
  disposable Fedora VM. This work does not reinstall or update the host app.

### Version 0.4.3 release verification, 2026-09-12

[Nimbus v0.4.3](https://github.com/Furyfree/nimbus/releases/tag/v0.4.3) selects
commit `e7690ff10073296cad24fe6a6e58a6a63989fe1e`. Its source archive SHA-256 is
`6716cd80d7092a2b7f84a50f71de02b64a7c71dfea45f24e64dec2c4e8d3fd4d`.
All 269 tagged files and executable bits match. Module inputs and 23 vendored
dependency notices are unchanged from 0.4.2. Local checks, CI and the offline
vendored release build/tests passed.

[COPR build 10979911](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10979911/)
published `nimbus-0.4.3-0.1.fc44.x86_64`. The complete offline packaging gate
passed on an isolated Fedora 44 source copy; the direct host bind mount was
blocked by SELinux, so only temporary copies received container labels.
Both prepared and published source RPMs preserve the exact release archive
and selected spec. The package-specific and full COPR CI gates passed.

Native DNF downloaded the new RPM from the configured COPR repository. An
isolated RPM keyring containing only Nimbus's pinned key verified signatures
and digests. Its SHA-256 is
`af8633a919a9a56d05f7fc0128dc9c7ce6bf73c96f37d4e09be7092509c4b7b4`.
The payload contains only the engine and license notices, with no scriptlets
or triggers. The extracted engine reports 0.4.3 and validates every tagged
machine, including the laptop, in a container with networking disabled.
Installation, login and hardware behavior remain a separate trial; this
verification did not install the RPM or run workstation provisioning.

### Version 0.4.4 release verification, 2026-09-13

[Nimbus v0.4.4](https://github.com/Furyfree/nimbus/releases/tag/v0.4.4) selects
commit `c8c0f6137e841eaca718150ff7be5af196e77074`. Its source archive SHA-256 is
`06c3fed54492fa9a41a1985e10ab96a4e1d547d182b86b3180ed8c9758f5176c`.
All 271 tagged files and executable bits match. Module inputs and
23 vendored notices are unchanged from 0.4.3. Local checks, definition
validation and GitHub's offline vendored build/tests passed.

[COPR build 10980508](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10980508/)
published `nimbus-0.4.4-0.1.fc44.x86_64`. The full offline packaging gate passed
in a temporary Fedora 44 source copy. An initial concurrent-container label
conflict was resolved by running the check sequentially. The full and selected
package GitHub checks and publication workflow passed. Both local and COPR
source RPMs preserve the exact release archive and selected spec.

The downloaded RPM's SHA-256 is
`b396c202e3fec01348b1f3898954bbb832e72bbc32a6054e13b05654333e6022`.
Native RPM verification with an isolated keyring containing only Nimbus's
pinned key passed both signatures and digests. The payload contains only the
engine and license notices, without scriptlets or triggers. In an unprivileged,
network-disabled Fedora container, the extracted engine reports 0.4.4,
validates all three tagged machines and recognizes the laptop's
`noctalia-plugins --plan` task. It correctly reports the uninstalled Noctalia
package as blocked, without attempting repair.

The existing definition minimum remains 0.4.3; this optional new task requires
0.4.4. Package installation and actual fresh-desktop plugin repair remain a
separate trial. Release verification did not upgrade the workstation or alter
its Noctalia runtime state.

### Version 0.4.5 release verification, 2026-09-13

[Nimbus v0.4.5](https://github.com/Furyfree/nimbus/releases/tag/v0.4.5) selects
commit `c324a94bb0d1fcf5a65d23f07301f436f7a94716`. Its source archive SHA-256 is
`e6d09a8466f230f16bafbb46c71e6076cd2ebe2371c80aaebaeb754cd6957d3b`.
All 271 tagged files and executable bits match. Dependencies and 23 vendored
notices are unchanged from 0.4.4. Local checks, definition validation and the
GitHub offline vendored build/tests passed.

[COPR build 10980526](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10980526/)
published `nimbus-0.4.5-0.1.fc44.x86_64`. The complete offline Fedora packaging
gate, full and selected-package CI, and publication workflow passed. Prepared
and published source RPMs preserve the exact archive and spec. Binary RPM
SHA-256 is
`6fe32b08a05168e3e68995e388f246d65c16a042235039559616b065f5ac97e8`.

Native RPM signature and digest checks passed with an isolated database
containing only Nimbus's pinned project key. The payload contains only the
engine and licenses, without scriptlets or triggers. The extracted engine
reports 0.4.5, validates all three tagged machines and previews the laptop's
Noctalia task in an unprivileged, network-disabled Fedora container. Missing
Noctalia is correctly reported as blocked. No workstation package was
installed; a fresh desktop repair with the released retry fix remains untested.

### Desktop package and setup audit, 2026-09-12

- Zathura's PDF backend was missing. The desktop profile now selects Fedora's
  `zathura-pdf-poppler`, matching the installed 2026.07.18 core. The package
  supplies `libpdf-poppler.so` and its PDF desktop handler. The owner installed
  2026.07.18-1.fc44; Zathura detects it, native MIME queries select its handler,
  and an isolated viewer opened a sample PDF. Chezmoi keeps Brave as the
  fallback when the plugin is absent. Archive handling stays with Nautilus.
- Native DNF history identifies two manual application/package installs:
  OpenLogi and power-profiles-daemon. OpenLogi is no longer installed at the
  closing inventory. Power-profiles-daemon is installed and is now selected
  by the common power-management component; delivering that component still
  requires the pending engine/definition release and sync.
- Baseline packages, Nimbus receipts and native install reasons account for
  the other inspected packages. RPM Fusion release packages come from
  bootstrap; the NVIDIA kernel module comes from akmods. Those are not new
  manual application requirements. The three installed Flatpak applications
  are already in the selected definitions.
- Bash history records manual SSH server enablement. The owner explicitly
  chose to leave SSH server access unmanaged; do not declare its service.
- The connected speakers are Logitech G560, USB `046d:0a78`. They are absent
  from [OpenLogi's supported list](https://openlogi.org/docs/supported-devices).
  Do not add OpenLogi for a speaker-only requirement without supported
  hardware evidence.
- Files bookmarks, hidden-file preferences, wallpaper shortcuts and Fastmail
  account metadata belong to Chezmoi. AI Usage installation belongs to its
  existing native Mise declarations. Personal sign-ins belong to the apps;
  the dotfiles README owns the first-login checklist.
- The owner deferred the new Dockur Windows 11 VM. No VM or guest storage was
  created. Keep it separate from the pending Nimbus release.

This audit is a local observation, not a claim that the unreleased engine has
applied its pending resources. DNF history and Bash history are incomplete
records of possible out-of-band work and were not copied into the repository.
The closing `just check` gate passes. No system package or service mutation
was performed by this audit.

### Final desktop capture and sync, 2026-09-12

- [x] Reconcile completed desktop preferences into Chezmoi: window behavior,
  monitor-local workspaces, shortcuts, Noctalia, Ghostty, Zed, Files, MIME
  defaults, launcher visibility, wallpapers and project links. Account secrets
  and generated state remain local; the dotfiles checklist owns its evidence.
- [x] Add the four requested native XDG defaults under
  `system/root/etc/xdg/user-dirs.defaults`: Projects, Screenshots, Wallpapers
  and Recordings. Fedora's installed 0.18 updater creates all four correctly
  in an isolated home. Preserve the standard Fedora defaults too.
- [ ] Resolve explicit ownership migration for the existing package-owned
  `/etc/xdg/user-dirs.defaults` before selecting the prepared file resource.
  The file is unselected for 0.4.1: the planner correctly blocks replacement
  without a Nimbus receipt.
  `files accept` requires prior ownership and cannot adopt this file. Chezmoi
  already manages the working per-user paths and creates their folders.
- [ ] Complete system reconciliation after the unreleased engine and local
  changes are ready for delivery. The requested sync stopped at dirty-repo
  preflight; no repository fetch, system mutation or user apply ran through
  Nimbus. The later release request authorizes commits and pushes to main,
  plus COPR publication; workstation installation remains a separate step.
- [x] Publish ble.sh (build 10978681), the compatible LibrePods fork
  (10978678) and Copilot helper 0.3.0/app 1.1.19 (10978679). Verify the new
  project keys and RPM signatures in isolated RPM databases, then select
  ble.sh through common and the LibrePods fork through the desktop profile.
- [ ] Install and exercise the published applications on the workstation.
- [x] Implement the 0.4.2 combined-sync ordering fix. The existing 0.4.0 engine
  needs one direct DNF upgrade to understand the current definitions. New
  source reconciliation must precede Topgrade, followed by a fresh-engine
  sync. `just check` and all three machine validations pass. Native DNF
  resolution in a disposable Fedora container selects both new COPRs;
  the transaction was declined. Workstation installation remains untested.

The native XDG file extends Fedora's installed `user-dirs.defaults`; upstream
[documents its role](https://www.freedesktop.org/wiki/Software/xdg-user-dirs/).
A comparison with
[CachyOS-Settings](https://github.com/CachyOS/CachyOS-Settings)
did not justify extra system policy. Existing user overrides remain owned by
Chezmoi. SSH server access stays unmanaged, and the Windows VM stays deferred.

### Engine and integration work

- [x] Promote the tested minimal greeter appearance into the Hyprland session
  component. Keep canonical configuration in `/etc` and expose that one file
  read-only through systemd; preserve native runtime state and defer activation
  to reboot. Chezmoi owns the matching lockscreen and account image.
- [ ] Check the promoted systemd greeter mount after an approved sync and reboot.
  The earlier local appearance preview did not exercise this deployment path.

- [x] Add account-picture registration through AccountsService after the
  Chezmoi image is available. Use the invoking user's native permission check,
  read-only non-activating inspection, source/current content checks across
  approval, and post-action verification without a completion receipt.
  Fake-native tests cover missing/damaged images, unknown account state,
  cancellation, stale approval, failed/ineffective saves, retry and convergence.
  `just check` and `just validate` pass. On 2026-09-13 the local candidate
  registered the desktop owner's picture through native authorization, verified
  the stored bytes and reported complete on a second run. The greeter
  appearance still needs the owner's next-login check.
- [ ] Publish the account-picture task in a new engine release. This change
  does not release Nimbus or alter greeter wallpaper behavior.

- [x] Simplify completed post-install output to its verified result. Omit setup
  titles and action/recovery guidance for completed checks, and use the same
  concise result after successful actions. CLI tests cover completed Noctalia
  selection and preview without repeating its update. This presentation change
  is unreleased; engine 0.4.5 retains the previous output.
- [x] Add Noctalia plugin inspection and a native post-install repair for
  enabled plugins lacking runtime exports. Read the effective Noctalia config;
  preserve preview, approval, local-only inspection and native ownership.
  Fake-native tests cover source batching, partial results, timeout, retry,
  cancellation, stale approval and convergence. A read-only native preview
  recognizes the current desktop exports as complete.
- [x] Publish the plugin repair task in engine 0.4.4 and its signed COPR RPM.
- [x] Test the Noctalia task on the fresh laptop through normal Nimbus/Chezmoi
  commands. The user's 0.4.4 run installed the plugins, but verification exited
  early on an unrecognized listing during the background update. A later
  read-only plan confirmed all enabled runtime exports were complete.
- [x] Retry post-action Noctalia verification within the existing two-minute
  window, preserving the latest diagnostic at timeout. Regression tests cover
  transient listing and IPC errors, missing exports after catalog recovery,
  persistent errors and cancellation without repeating source updates.
  The new cases fail before the fix and pass afterwards. `just check` and
  `just validate` pass locally.
- [x] Release the verification retry fix in engine 0.4.5 and its signed COPR RPM.
- [ ] Test a fresh plugin repair through the installed 0.4.5 engine. Local tests
  use fake native responses; the exact transient listing from the laptop
  failure was not captured.

- [x] Offer Tailscale operator setup when its native package is selected and
  applied. Preview the current operator, approve the exact native command,
  recheck stale state and verify completion. Keep unrelated preferences private.
  Fake-native tests cover read-only listing, cancellation, failed commands,
  ineffective commands, retries, convergence and explicit revocation.
  `go test ./internal/postinstall ./internal/cli`, `just check` and
  `just validate` pass locally.
- [ ] Publish and test Tailscale operator setup in disposable Fedora; no live
  operator change has been run by this implementation.
- [x] Install Bash and Zsh through common; use the optional machine `shell`
  field for the invoking local user's default login shell. Preview the native
  change, verify UID/shell before and after it, and report the required login.
  Keep both configurations in Chezmoi. Removing the field relinquishes
  ownership without changing the account's current shell.
  Local `just check` and `just validate` pass; fake-native tests cover repair,
  unchanged state, failed verification, changed UID and ownership retirement.
- [ ] Publish the 0.4.1 engine before applying the shell-bearing definitions;
  verify login-shell selection, switching and repair in disposable Fedora.
- [x] Consolidate product docs into SPEC, TASKS and ROADMAP.
- [x] Separate sync and upgrades; add `sync --upgrade` and the narrow
  `upgrade --system` callback for Topgrade.
- [x] Add init, selection and upgrade previews; require init approval and
  explicit `--yes` for JSON mutation.
- [x] Remove dotfiles wrappers; retain initial handoff and profile refresh advice.
- [x] Remove custom Copilot/WoWUp providers and exclusive tests. Preserve old
  application receipts without automatically removing applications.
- [x] Add Copilot's native post-install action and Topgrade updater.
- [x] Offer Proton-CachyOS Latest setup through ProtonPlus after explicit
  post-install approval, with native Steam and package receipts required.
- [ ] Validate ProtonPlus initial download and retry on disposable Fedora.
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
