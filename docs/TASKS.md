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

## DTU eduroam guided setup

Implemented in source; not released as a package. The owner tested the
candidate on the laptop; the installed release remains unchanged.

- [x] Extend the explicit Linux task with manual or 1Password item credentials,
  DTU realm normalization, native profile ownership and separate activation.
- [x] Embed the reviewed CAT CA bundle/profile; retain upstream attribution.
  Permit replacement of the exact previous bundle while refusing unknown files.
- [x] Keep inspection offline and secret-free; preserve unrelated profiles and
  private credentials. Existing eduroam/DTUsecure profiles trigger a default-No
  deletion/recreation prompt, including Nimbus profiles. Keeping them leaves
  certificates untouched; `--yes` cannot bypass replacement approval.
- [x] Fix the laptop prerequisite blocker: accept selected, installed packages
  whose exact native identities are recorded in the schema-2 baseline, without
  adopting them. Keep invalid receipt, missing package and unknown-state blocks.
  Regression tests cover baseline-only setup and unchanged ownership records.
- [x] `just check` (including 19 isolated Python credential/profile tests)
  and `just validate` pass with isolated HOME/XDG state. CLI tests cover
  approval, keep/replace decisions, empty answers, `--yes`, 1Password item
  selection, partial deletion/setup, drift and unapproved reruns.
- [x] Investigate the laptop failure: the owner connects when entering the
  password in Noctalia. Noctalia 5.1.0's `SaveSecrets` is a no-op, so the
  agent-owned password supplied by either input method was not retained.
- [x] Use NetworkManager-owned storage, explained before approval, and privately
  verify the newly saved password before connection. Failed activation reports
  failed setup, retains the profile and records no successful completion.
- [x] A network-disabled Fedora 44 container verifies storage with absent and
  non-saving agents, root:root 0600 native keyfile permissions, password survival
  through settings updates and daemon restart, and credential-file removal on
  native profile deletion. Unrelated profiles and replacement approval remain
  covered. This does not establish desktop-user authorization or Wi-Fi access.
- [x] Both manual and 1Password credential paths pass isolated full-setup tests;
  missing/unreadable saved credentials stop before connection. Connection
  timeout and omitted native autoconnect defaults are covered.
- [x] Owner-tested corrected 1Password setup on the laptop (2026-09-16):
  approved replacement, saved password verification, connection without another
  password prompt, automatic reconnect enabled, and subsequent status Verified.
- [x] Owner-tested manual username/password entry on the laptop (2026-09-16):
  saved password verified, connection succeeded without a second password
  prompt, and automatic reconnect enabled.
- [x] Owner reports Wi-Fi works and still works after reboot on the laptop.
- [x] Support off-campus setup: verify saved credentials, enable automatic
  connection, then offer immediate connection separately. Native Fedora tests
  with no Wi-Fi AP verify the normal not-found outcome; isolated tests cover
  skipping activation, exact SSID matching, scan errors and connection failure.
- [ ] Separately verify any required DTU-only services and account-password
  renewal. The latest off-campus flow has fixture coverage, not a new laptop
  trial.
- [x] Owner authorized committing and pushing DTU setup to main alongside the
  separate Pinta Flatpak change. Package release remains separate.
- [x] Update the RPM licence metadata and include `licenses/GEANT-CAT.txt` when
  packaging this separately licensed CAT adaptation. The copr recipe declares
  `LicenseRef-GEANT-CAT` and ships the notice (copr 086f342, PR #14); an
  offline container build from a Nimbus `main` vendor archive verified the
  licence expression and `/usr/share/licenses/nimbus/GEANT-CAT.txt`. It takes
  effect with the 0.6.0 release archive, which contains the file since commit
  `96ddee1`; the published 0.5.10 package is unchanged.

The following certificate-only evidence predates this addition; it does not
establish that the new guided connection flow works on campus.

## DTU eduroam certificate

- [x] Select explicit `dtu-network` prerequisites on desktop and laptop only.
  Add help, offline preview/status, explicit approval and fresh observation.
- [x] Download the pinned HTTPS bundle with bounded size/time and no redirects.
  Validate SHA-256, all three CA certificates, signatures and validity. Refuse
  expired or changed bundles and unfamiliar target files.
- [x] Reuse atomic system-file installation for the fixed NetworkManager path,
  root ownership and 0644 permissions. Restore only directory/file SELinux
  labels when enabled; verify before completion. No global trust, Wi-Fi profile
  or credential changes. Preserve existing Tailscale candidate work.
- [x] Update ownership, README/help, SPEC and the installation checklist.
- [x] `just check`, `just validate` and focused DTU/Tailscale race tests pass.
  Regression tests cover approval/cancellation, offline inspection, download
  failures, redirects, size limits, checksum/expiry, unsafe/unknown files,
  SELinux drift/failure, verification, idempotence and stale completion.
- [x] Network-disabled Fedora container with the built candidate verifies native
  atomic installation, exact bytes, root ownership, file/directory modes, stale
  approval refusal, symlink refusal and writable-parent refusal. No host mounts.
- [ ] Publication as 0.5.7 is authorized. Native SELinux-enforcing and actual
  campus Wi-Fi validation remain for the released candidate on the laptop.
  No host/laptop installation, sudo or network repair during implementation.
- [ ] Review DTU's renewed CA bundle before the first intermediate expires on
  2027-12-02; see the [README workflow](../README.md#workflow).

## Tailscale initial sign-in

- [x] Inspect operator and recognized backend state without authentication.
  Offer approved native sign-in and connection only for `NeedsLogin`; keep
  operator-only changes for signed-in machines, preserving stopped connections.
- [x] Avoid the upstream `tailscale login` profile-switch permission loss.
  Keep native preferences checks, sign-in prompts and explicit approval;
  never add `--reset`, persist sign-in links or repair during sync/status.
- [x] Require operator and running connection after initial sign-in. Block on
  device approval and unknown state; saved evidence never overrides native
  inspection. Update help, README and SPEC for both paths.
- [x] Isolated regressions cover fresh sign-in, operator loss, canceled/failed
  or ineffective actions, stopped connections, device approval, unknown native
  state, approval drift, private data, read-only preview/status and stale
  completion evidence. Focused race tests, `just check` and `just validate` pass.
- [ ] Publication as 0.5.7 is authorized through the release/COPR workflow;
  then validate native browser sign-in on the laptop through released Nimbus.
  No live sign-in, sudo, laptop repairs or service changes during development.

Upstream diagnosis: [profile-switch operator loss][tailscale-login-bug].

[tailscale-login-bug]: https://github.com/tailscale/tailscale/issues/18294

## Declared RPM sources and help colors, 2026-09-14

- [x] Color every visible help command, including the single-space row for
  `noctalia-lockscreen`; preserve status colors, plain output and wrapping.
- [x] Restrict selected RPMs to enabled declared sources in mixed install and
  system-upgrade transactions. Keep native dependency resolution and signatures.
  Verify previews and actual provenance before successful receipts/reporting.
- [x] Preview wrong-source corrections through native distro-sync or same-version
  reinstall. Block unavailable candidates, conflicting resolved sources and
  unexpected removal of selected packages. Preserve architecture and ownership.
- [x] Run signed synthetic-RPM tests in a network-disabled Fedora 44 container:
  competing priorities, mixed sources, cross-source dependencies, upgrades,
  disabled sources, downgrade/reinstall correction and missing-candidate refusal.
  Test commands are produced by the Go planner; see
  [the native test instructions](../tools/dnf-sources/README.md).
- [x] Nimbus `just check` and `just validate` pass. The desktop's read-only
  cached plan shows only Steam source correction: same-version x86_64 reinstall
  from RPM Fusion Updates, 19 MiB. No transaction was executed.
- [x] Chezmoi secret-skipping managed/status/diff/verify checks: file diff empty;
  verification excluding scripts passes. The five existing run-always scripts
  remain pending. This change adds no Chezmoi configuration.
- [ ] Apply the candidate on the desktop after review. No host package changes,
  sudo, authentication, laptop edits, commits or publication in this work.
  Use the [README workflow](../README.md#workflow) for local previews.
- [ ] UWSM/session follow-up remains research-only.

## Guided 1Password SSH/Git selection, 2026-09-14

- [x] Offer default-no SSH/Git opt-in during the explicit guided task, including
  after CLI-only completion. Save an approved choice through native Chezmoi init
  while preserving machine and ordered profiles. Never infer opt-in from `--yes`.
- [x] Keep verification-only completion and inspection free of selection changes.
  Require fresh SSH GUI confirmation, then preview/apply only selected targets.
  Keep incomplete or failed setup pending, retaining an enabled choice for retry.
- [x] Test opt-in, decline, default, EOF, invalid input, unattended operation,
  selection drift, failed initialization/apply, GUI refusal and completed reruns
  in temporary homes. Nimbus `just check` passes.
- [x] Validate the real Chezmoi init template in a disposable home: preserve
  machine, ordered profiles and existing account metadata; no scripts, target
  file applies or 1Password calls during selection.
- [ ] User desktop authorization and end-to-end guided setup. No live apply,
  authentication, sudo, laptop changes, commits or publication were performed.
  Use the [README workflow](../README.md) for local testing.

## Local agent proxy, 2026-09-13

[Nimbus v0.5.2](https://github.com/Furyfree/nimbus/releases/tag/v0.5.2)
is published from commit `94f448a560dd5e556eb6bb651265a2e29b6eda69`.
The archive SHA-256 is
`d6f99a5b62e35f9b754b0b5b2475456e517ba437a09cc66170289f55f62fbc36`.
All 332 tagged files and executable modes match the archive. The 754 vendored
files and 23 dependency notices are unchanged from 0.5.1; the embedded
upstream patch retains its MIT notice.

Nimbus's complete gate, definition validation, CI and offline vendored release
build/tests passed. Chezmoi commit `14a045d` passed 154 tests with three optional
skips, Bash checks and CI. Its secret-skipping file diff is empty; verification
excluding the five expected run-always scripts passes. The complete COPR gate,
CI and unprivileged offline RPM rebuild passed. Prepared and published source
RPMs retain the exact reviewed archive and spec; the RPM includes the new
upstream license and runs the model-adapter tests with Node available.

[COPR build 10982500](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10982500/)
published `nimbus-0.5.2-0.1.fc44.x86_64`; its
[publication workflow](https://github.com/Furyfree/copr/actions/runs/34786152917)
passed. Repository metadata and the binary checksum match:
`f61a2cec904b4ec70e8ae0dc19c5fd17365f739ff7f2c2ff3093dc2f8282d9c4`.
Signatures and digests passed in an isolated RPM database containing only the
pinned project key. The payload contains only Nimbus and licenses, without
scriptlets or triggers. An unprivileged offline container confirms version
0.5.2, all three machine definitions and help for both new tasks. No workstation
upgrade or laptop changes were performed during release verification.

- [x] Separate concise repeat-refresh output from initial setup and help. Verify
  operation selection on matching/changed/unreadable config, per-provider
  reporting and secret-safe distinction between sign-in skips and real errors.

- [x] Add previewable postinstall, native installer/config ownership, private
  registration evidence and explicit opt-in/reset for upgrade catalog refresh.
- [x] Query four native catalogs independently, preserve failed-provider and
  manual entries, and verify idempotence, stale removal and interrupted retry.
- [x] Replace the trial's hardcoded Copilot provider ID with native registration
  and validated evidence import. Keep keys in native credentials, never Git.
- [x] Validate patched upstream: 474 tests passed, two upstream skips;
  build/typecheck/dead-code checks and production dependency audit passed.
- [x] Exercise native installer install/upgrade/rollback/uninstall and activation
  failure in disposable homes, with simulated systemd lifecycle operations.
- [x] Verify local setup and refresh: 13 existing models retained, no duplicate
  registrations, and real non-streaming replies from Codex, Claude and Grok.
  Confirm both services use Mise Node and stable shims. Remove the temporary
  helper, imported helper inventory and redundant trial service drop-in.
- [x] Pass Nimbus `just check` and `just validate`; pass Chezmoi's full gate
  (154 tests, three optional skips) and focused private-config rendering.
  Chezmoi diff is empty; verification excluding five run-always scripts passes.
- [ ] Antigravity is not signed in locally; live generation remains untested.
  Discovery defers without deleting its inventory or starting authentication.
- [x] Owner authorized committing both repositories and publishing Nimbus
  0.5.2 through COPR. Release verification is recorded after publication.
- [ ] Test the released setup on the laptop; no laptop edits are authorized.

## Managed lockscreen repair, 2026-09-13

- [x] Add a named, previewable lockscreen task with read-only status, locked
  reinspection and explicit approval. Native configuration remains authoritative.
- [x] Back up settings privately and remove only parsed lockscreen-widget
  overrides, preserving other values and formatting. Reject unsafe paths and
  abort when settings change before replacement or during shutdown.
- [x] Gracefully stop the exact invoking-user Noctalia daemon using a pidfd,
  restart after repair/failure, and verify readiness and effective configuration.
  Never stop Hyprland, force-kill, request sudo or trigger the lockscreen.
- [x] Pass `just check`, `just validate`, and race tests for CLI, postinstall
  and native execution. Isolated tests cover approval refusal/drift, private
  backups, TOML preservation, unsafe paths, shutdown writes and restart failure;
  an isolated child process verifies pidfd identity checks and graceful exit.
- [ ] Owner: exercise the task in the laptop desktop session and visually check
  clock, date, avatar and password field. No laptop repair or live-desktop
  mutation is performed during implementation. See the README for commands.

## Completion and output follow-up, 2026-09-13

[Version 0.5.1](https://github.com/Furyfree/nimbus/releases/tag/v0.5.1) is
published from commit `95bb78202344bc90c4e6c4372ffebb552fd9c82b`. This patch
includes verify-only setup, corrected MOK inspection, 1Password sign-in recovery,
no-op upgrade handling and clearer terminal output. Definitions remain
compatible with engine 0.5.0; Chezmoi is unchanged.

[COPR build 10982208](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10982208/)
published `nimbus-0.5.1-0.1.fc44.x86_64`. Local gates, CI, the offline source RPM
rebuild and remote publication passed. Published repository metadata and the
binary checksum match. Signatures and digests passed against the pinned COPR
key in an isolated RPM database. The extracted engine reports 0.5.1, validates
all three tagged machines and shows task help in an unprivileged, offline
Fedora container. The payload contains only the engine and licenses, without
scriptlets or triggers. No live upgrade, vault authorization or laptop change
was performed; installed-session testing remains with the owner.

- [x] Verify existing setup through supported completion commands without
  installing or applying configuration. Preserve explicit GUI acknowledgment,
  SSH opt-in, native verification and resettable local evidence.
- [x] Avoid applying already matching 1Password configuration. Add its numeric
  alias and task suggestions for misspelled names followed by task flags.
- [x] Distinguish MOK certificate failures. Offer narrow read-only privileged
  verification only in an explicitly approved task; status never authenticates
  or hides failed inspection behind stored evidence.
- [x] Group matching ownership refreshes and successful snapshot results; show
  only actual replan changes and separate verification problems from setup.
- [x] Check full DNF transactions and fresh system Flatpak update refs; skip
  empty transactions and snapshots, and fail when inspection is inconclusive.
- [x] Pass `just check`, `just validate` for all three machines, and race tests
  for CLI, postinstall and local state. Regressions cover existing 1Password
  files, failed checks, MOK permission failures, authentication boundaries,
  no-op upgrades and output failures.
- [x] Pass the network-disabled signed-RPM drill: stale metadata refresh,
  engine-first refusal, signed upgrade/restart, preserved selection and later
  failure reporting. Real Flatpak update/no-op behavior remains fixture-tested.
- [x] Leave Chezmoi unchanged. Secret-skipping read-only checks show no file
  diff; verification passes with scripts excluded. Full verification reports
  the five existing after-apply scripts, which were not executed.
- [x] Owner confirmed GUI prerequisites and native CLI behavior. Direct
  sign-in resolved the unsigned-in CLI account; the existing verify-only task
  then completed successfully without changing integration files.
- [x] Owner confirmed native MOK enrollment with exit 1, both with and without
  the kernel-keyring shortcut. Earlier fixture tests assumed the wrong exits.
- [x] Add approved sign-in recovery for the specific unsigned-in CLI error,
  retry verification once and shorten instructions after GUI confirmation.
  Correct MOK exit semantics and bypass the separate kernel-keyring shortcut.
- [x] Pass `just check`, `just validate` and race tests for CLI, postinstall
  and native execution after these corrections. Production-runner subprocess
  tests cover MOK's nonzero enrolled result and other exit/output combinations,
  plus sign-in stdin, visible prompts and discarded account/session stdout.
  Guided-task tests cover sign-in success, decline, cancellation, failed retry,
  existing authentication and completion evidence.
- [x] Owner verified enrollment through the corrected MOK task and recorded
  completion. Ordinary status still could not read the protected certificate.
- [x] Label that historical result "Previously verified" and report it as a
  notice with the command to request a sudo recheck. Preserve native unknown
  status in JSON and retain fresh failures.
- [x] Add shared terminal-palette styling to text commands and help, with plain
  redirected output and JSON, automatic terminal detection and `NO_COLOR`.
  Preserve immediate prompts, native terminal writers and plain installer logs.
- [x] Pass `just check`, `just validate` for all three machines, and race tests
  for output, CLI and native execution after the color pass. Tests cover every
  public command's help, terminal detection, opt-out, JSON, immediate prompts,
  output errors, log separation, native progress, resizing and cancellation.
- [x] Shorten verified checklist descriptions and indent the MOK sudo recheck
  command on its own line. Keep verification limits in help, full detail in
  JSON/previews, and failure reasons in the checklist. Fixture tests cover
  80-column rows, native unknown state and help without authentication.
- [x] Strengthen terminal-palette emphasis and complete selection, explanation,
  preview and outcome styling. Wrap terminal lines with indented continuations
  and bulleted setup notes. Keep native streams, logs and JSON separate.
- [x] Remove duplicate Doctor resource prefixes, indent multiline observations,
  and summarize checks. Order upgrade previews by execution, shorten recurring
  unchanged-plan prose, and use historical MOK wording in task previews.
- [x] Pass focused output/CLI tests and race tests for output, CLI and native
  execution. Terminal tests cover 40/120 columns and color opt-out; fixtures
  cover Doctor formatting, MOK history and preview order. Definition validation
  passes for desktop, laptop and VM.
- [x] Reset output line tracking after confirmation and selection prompts, so
  the first result receives the same styling as subsequent results. Regression
  tests cover accepted, declined, default and exhausted input with colors on
  and off, without adding blank lines or running native setup.
- [ ] Owner reviews terminal colors and retests guided sign-in recovery.
  No live authentication, workstation mutation, release or laptop changes are
  part of automated validation.

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

- [x] Prepare Paper Dark GRUB and Plymouth source themes; retain the selected
  reference in `docs/images/`. Native QEMU/OVMF GRUB and X11 Plymouth previews
  are documented in [the preview tool](../tools/boot-theme/README.md). These
  are rendering checks, not installed Fedora or encrypted-boot validation.
- [x] Implement activation in source: the theme payload and the inert
  `/etc/grub.d/36_paper_dark` drop-in ship in the engine package, the
  `boot-theme` component owns `/etc/nimbus/boot-theme.enabled` with the
  `grub-config` and `plymouth-theme` triggers, and removal restores the theme
  observed before installation. Selected for the test VM only.
- [x] Group older kernels under "Previous kernels" through the engine payload
  `/etc/grub.d/09_nimbus_previous_kernels` and `nimbus internal boot-menu`
  (2026-09-17). VM-verified: the top level listed only the default kernel plus
  the submenu, which held the older kernel and the rescue entry; deleting the
  mirror self-healed at the next mkconfig; a missing engine or marker restored
  Fedora's flat menu, and marker removal deleted the mirror. A later review
  found that Fedora's grub hook regenerates grub.cfg only when BLS is
  disabled, so kernel installs and removals left the mirror stale; the
  marker-gated `/etc/kernel/install.d/96-nimbus-menu.install` hook now runs
  after `95-set-boot-entry` and runs `grub2-mkconfig --no-grubenv-update`; the
  `09` drop-in refreshes the mirror during that run. The earlier VM
  add/remove verification predates the mkconfig-only hook and needs a rerun;
  at that time the mirror, `grub.cfg` and the Nimbus UKI followed the
  remaining kernel without any manual mkconfig. Fedora's
  `95-set-boot-entry.install` only acts on `add`,
  so `saved_entry` still named the removed kernel; the grouped menu boots the
  explicit mirror entry and does not depend on `saved_entry`, but a boot after
  a removal and the Fedora flat-menu fallback are still part of the bare-metal
  validation in #40. The 2026-09-18 boot audit replaced the submenu's
  `blscfg non-default` filter with an unfiltered `blscfg` (a rescue
  `saved_entry` hid the rescue entry), added a GRUB-side check for the live
  BLS entry and its mirror copy so a failed mkconfig after a kernel removal
  falls back to Fedora's flat menu, and made the mirror write sync and remove
  its temporary files. Fedora's `grub2-script-check` rejects `if ... && ...`
  and accepts the `-a` form, which the guard uses. The submenu and guard still
  need the same VM rerun. The image and
  drop-ins still need the 0.6.0 `nimbus.spec` packaging step below.
- [ ] Ship the payload and drop-in in the 0.6.0 engine package (`nimbus.spec`),
  including `/etc/grub.d/09_nimbus_previous_kernels`,
  `/etc/kernel/install.d/96-nimbus-menu.install` and
  `/usr/lib/dracut/modules.d/40nimbus-plymouth`, all as `0755` plain files and
  not `%config(noreplace)` so engine updates land, and select `boot-theme` in
  the `common` profile in the release change. The
  component stays deselected on physical machines until then, so an older
  engine is never asked to run triggers whose payload it lacks.
- [x] Verify the VM boot path (2026-09-17, disposable Fedora 44 with Secure
  Boot and LUKS): the themed GRUB menu rendered the NIMBUS header, monospace
  fonts and the five-second countdown with both installed kernels listed; the
  Plymouth disk-unlock screen rendered the theme (owner-verified); removing
  `/etc/nimbus/boot-theme.enabled` and rerunning the triggers restored
  Fedora's default menu, the `text` Plymouth theme and a theme-free initramfs;
  re-activation was repeatable. The payload was copied from the candidate
  checkout because the 0.5.10 package does not ship it yet.
- [x] Remove the redundant `insmod` lines from the drop-in (2026-09-17): the
  signed GRUB refused them under Secure Boot and printed
  `shim_lock_verifier_init: prohibited by secure boot policy` on every boot,
  while the built-in modules rendered the theme anyway. The fixed VM boot
  console is clean and the menu still renders.
- [ ] Verify the Windows-present menu on the dual-booting desktop and boot the
  older kernel end to end; the VM had no Windows entry.
- Accepted limitations: an older kernel whose initramfs was built before
  activation keeps the default Plymouth prompt until its next rebuild; the
  GRUB menu stays themed. The Paper Dark text rendering needs the label plugin
  and fontconfig inside the initramfs; the engine's marker-gated dracut module
  `40nimbus-plymouth` adds `label-freetype.so`, `fc-match`, fontconfig and the
  monospace font, and creates the writable xdg fontconfig cache on the initrd
  root (`/usr` is read-only there). Since the 2026-09-18 audit the module
  verifies the label plugin and `fc-match` before installing, so a broken
  dependency skips it instead of failing a kernel transaction, and the
  removal plan keeps a theme the user selected after installation and rebuilds
  its initramfs without the Nimbus payload. Verified 2026-09-17 in the VM: the
  previously intermittent text-mode unlock prompt rendered the themed screen,
  and the boot journal showed zero `fc-match` and `Fontconfig` errors. BIOS-only
  systems are untested (EFI verified). The
  explicit `set timeout=5` intentionally overrides `GRUB_TIMEOUT`,
  `menu_auto_hide` and `systemctl reboot --boot-loader-menu`, including
  Fedora's longer recordfail timeout after a failed boot; `grubby` and
  `grub2-set-default` changes take effect only after a later
  `grub2-mkconfig`; and a submenu opened once shows no entries on a second
  open, because the blscfg module never clears its loaded entries; a kernel
  installed while only one entry existed appears after the next successful
  `grub2-mkconfig` when that generation failed. A missing engine
  payload blocks installation; a removal restores the recorded previous theme
  without the Nimbus payload and blocks only when no usable theme was
  recorded, when it is Nimbus's own theme, or when it is no longer installed.
  Both payload triggers re-run once, shown in the plan, when their receipt was
  written by another engine version, so an engine update reaches the initramfs
  and `grub.cfg`; a repeat run keeps the recorded previous theme. GRUB's
  shim-lock verifier refuses `loadfont` under Secure Boot, so the drop-in
  loads the custom faces only when `shim_lock` is unset; with it the theme
  uses GRUB's built-in font without errors. The trigger
  operations carry plan notes for the mirror rewrite, initramfs rebuild and
  installed kernel-install hook, and a successful removal retires the
  `grub-config` receipt like the Plymouth one, only at the end of the run, so
  a failed removal replans from the install receipt.
- [x] Inspect the Fedora/LUKS2 boot path, TPM and Secure Boot support; choose
  the native auto-unlock method and boot-change policy before implementation.
  Read-only inspection covered the laptop and the disposable VM: LUKS2 with a
  password-only keyslot, GRUB with Secure Boot enabled, TPM2 present and ample
  EFI space. The accepted policy lives in issue #34: Dracut, ukify,
  kernel-install and systemd-cryptenroll with Fedora's shim/GRUB retained, and
  PCR 7 + PCR 14 + signed PCR 11 with shim, PCR 7 + signed PCR 11 without.
- [x] Implement the optional `fde` component and the read-only
  `postinstall fde` detection: the mounted LUKS2 root, the TPM2 device class
  and the selected tools are inspected without privilege, and unsupported or
  unknown setups are reported clearly. Keyslots, EFI space and enrollment
  state need an approved privileged check. Detection stays read-only; the
  approved setup action is implemented in the next item.
- [x] Implement approved setup: the inert kernel-install hook, the marker,
  ukify build and the `Nimbus UKI` firmware entry, then prove passphrase UKI
  boot and a kernel update in a disposable VM (2026-09-17). A real
  `nimbus sync` on a drill machine recorded the package receipts; the approved
  `postinstall fde` action wrote `/etc/nimbus/fde-uki.enabled`, built
  `/boot/efi/EFI/Linux/nimbus.efi`, replaced an earlier hand-made entry that
  pointed at another path, and verified the image read-only. A stale
  same-label firmware duplicate is reported as a repair and replaced, so
  exactly one `Nimbus UKI` entry remains. Reboots loaded
  `Boot0009 "Nimbus UKI"` through systemd-stub (Secure Boot disabled) with the
  embedded command line; `kernel-install add` for the second installed kernel
  rebuilt the image through the hook, and the next boot ran that kernel.
  Accepted limitation: the image is unsigned; the signed shim chain and MOK
  enrollment arrive with the enrollment milestone.
- [ ] Ship the inert kernel-install hook in the 0.6.0 engine package
  (`nimbus.spec`); the marker stays action-written. Kernel-install skips a
  non-executable plugin, so the task checks the installed bit and blocks with
  an upgrade hint.
- [ ] Add enrollment, policy renewal, status and scoped removal with explicit
  previews; reduced protection and removal default to No and a blanket `--yes`
  cannot accept them. Preserve the passphrase, unrelated keys and enrollment
  slots. In source: the signed shim-chained image and MOK enrollment, TPM
  enrollment with its root-observed ownership record, boot-time status and
  explicit scoped removal (`nimbus postinstall fde --remove`, which wipes only
  the recorded keyslot and keeps the passphrase). Renewal, scoped removal and
  the ownership record were exercised end to end in the VM on 2026-09-18
  (issue #34). The Plymouth reconcile is VM-verified; the NVIDIA caller uses
  the same approved rebuild and waits for hardware. Deselecting `fde`
  while the marker exists still blocks the plan; removal clears the marker
  first.
- [ ] Test auto-unlock enrollment, booting, passphrase fallback, boot-change
  fallback and removal on real hardware. This scoped test precedes the TUI;
  it is separate from the later full desktop trial. No working claim yet.

Milestone 3 design approved 2026-09-18 after research validated with external
reviewers (issue #34): the signed path starts Fedora's installed shim with the
UKI path as its load option; a private shim copy is only the documented
recovery for firmware that ignores load options. One `ukify` invocation signs
the PE and embeds the PCR 11 policy; one `ukify genkey` creates both key pairs
under `/var/lib/nimbus/fde/`. The image is signed for `enter-initrd` only and
its embedded command line carries the per-volume `rd.luks.options` unlock
option plus `rd.shell=0 rd.emergency=reboot`. Enrollment binds PCR 7 + 14 +
signed 11 and the observed clean-boot PCR 12/13 values, only while
`BootCurrent` is the Nimbus entry, with `--tpm2-pcrlock=` empty. Phases:

- [x] 3.1 signed image through Fedora's shim, passphrase-only, Secure Boot on:
  packages (`systemd-ukify`, `sbsigntools`, `systemd-boot-unsigned`,
  `mokutil`), keys, signed build, `sbverify`, shim entry with the UKI path as
  load option, MOK enrollment, passphrase boot, Fedora GRUB fallback, kernel
  add/remove rebuild. VM 2026-09-18: the setup built and signed the image,
  queued the MOK request, created `Boot0002 "Nimbus UKI"` from Fedora's shim
  with the UKI path as its load option and promoted it first; after MOK
  enrollment the image verified against `mok.crt` and booted through
  systemd-stub; `kernel-install add <kver> <vmlinuz>` rebuilt and re-signed
  the image through the hook, and Fedora's GRUB entry booted after removal.
- [x] 3.2 VM enrollment: marker-gated dracut module (`tpm2-tss`,
  `systemd-pcrphase`), embedded cmdline options, TPM keyslot, unattended boot,
  passphrase fallback on the GRUB path, PCR 12/13 measurement, injected ESP
  credential refuses to unseal. VM 2026-09-18: enrollment recorded token 0 in
  keyslot 1 with PCR 7/14/12/13 and signed 11, reboots unlocked unattended,
  the LUKS passphrase remained the fallback on the GRUB path after removal,
  and a changed PCR 14 refused to unseal. A full disk copy booted with a
  fresh TPM also refused to unseal; that drill showed status claimed
  automatic unlock on a different TPM, so the record now captures the TPM SRK
  fingerprint and verification offers renewal instead.
- [x] 3.3 renewal, scoped removal, status and the failure matrix in the VM.
  In source: status offers renewal when the recorded literal PCR 7/12/13/14
  values are missing or no longer match, the offered task adds the new slot
  before wiping the recorded one, and the record is rewritten from
  root-observed state; a different or unidentified TPM removes the recorded
  slot before adding the new token because an identical policy is refused as
  already enrolled, and a no-op enrollment whose record proves the TPM
  refreshes the record metadata alone. VM 2026-09-18: enrolling a throwaway
  MOK changed PCR 14
  so the next boot refused TPM unseal and offered the passphrase; verification
  then offered renewal, which enrolled token 1 in keyslot 2, wiped slot 1 and
  rewrote the record, and the next boot unlocked unattended again. Removal
  wiped only the recorded slot, deleted the entry, image, marker and key
  material, kept passphrase slot 0 and the MOK certificates, and Fedora's GRUB
  booted with the passphrase. A root-only `/etc/crypttab` read broke
  post-setup verification until fixed (`3dc574c`).
- [x] 3.4 rebuild the UKI after Plymouth and NVIDIA initramfs refreshes,
  plan-visible. In source: the Plymouth trigger and the NVIDIA dracut refresh
  invoke the approved internal rebuild for the running kernel when the marker
  exists, disclosed by a plan note or the postinstall preview, and skipped
  when the image already targets another kernel. VM 2026-09-18: sync re-ran
  the Plymouth trigger with the marker present, carried the plan note, ran
  `internal fde-uki add <kver> --only-if-current`, rebuilt and re-signed the
  image; the rebuilt image still verified and the next boot unlocked
  unattended. The NVIDIA caller shares that helper and its preview/test rows;
  it waits for NVIDIA hardware.
- [ ] 3.5 laptop, then desktop with both MOKs enrolled before TPM enrollment.

VM-only gates still open: `--tpm2-signature` at enrollment time (omit if
cryptenroll refuses from the running system), the elected
`rd.luks.options` unlock path, and the observed PCR 12/13 values. Hardware-only
gates: firmware OptionalData handling, fall-through to the Fedora entry,
fwupd/dbx renewal, and the desktop's Windows/NVIDIA entries.

## NVIDIA MOK helper

Implemented in source; the helper itself is not installation-tested yet. The
manual native flow below is verified; the helper still needs its own trial.

- [x] Add approved signing and enrollment to `postinstall nvidia-mok`, with
  read-only preview, native password prompts and preservation of existing keys.
- [x] Verify module signing identifiers, stop on failed builds or boot-image
  updates, preserve pending requests and distinguish enrollment from boot proof.
- [x] Confirm the manual native flow on the desktop: signed NVIDIA modules,
  MOK enrollment, reboot, working RTX 3080 and Secure Boot still enabled.
  Hyprland, Noctalia and all three portal services work after this repair.
- [ ] Test the new Nimbus helper itself on an installation needing enrollment;
  manual commands validate the approach, not the new implementation.

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
- [x] Reject the Chezmoi rc-file guard (Furyfree/dotfiles#21, closed
  2026-09-17). The mask owns the generated unit, so a guarded
  `~/.config/autostart` override would never start; the mask stays the single
  owner of the behavior.
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

### Version 0.4.6 release verification, 2026-09-13

[Nimbus v0.4.6](https://github.com/Furyfree/nimbus/releases/tag/v0.4.6) selects
commit `9e529807149cec86123bfe07ec97aa88f69df00f`. Its source archive SHA-256 is
`2d891a105e85b2ffb8ff291ede5e38163f05531144f504dccdeaeb141c845a95`.
All 279 tagged files and executable bits match. Dependencies and
23 vendored notices are unchanged from 0.4.5. Nimbus local checks, all three
machine validations, CI and the GitHub offline vendored build/tests passed.

[COPR build 10980604](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10980604/)
published `nimbus-0.4.6-0.1.fc44.x86_64`. The full offline Fedora packaging gate,
full and selected-package CI, and publication workflow passed. Prepared and
published source RPMs retain the exact archive and reviewed spec. Binary RPM
SHA-256 is
`e833600b0315f04e18dd4514517f3f339e68cf7a2c013e304062aa5510b4cf39`.

Native signature and digest checks passed with an isolated RPM database
containing only the pinned Nimbus key. The payload contains only the engine
and licenses, with no scripts or triggers. In an unprivileged, network-disabled
Fedora container, the extracted engine reports 0.4.6, validates all three tagged
machines and recognizes both new post-install tasks. Missing development headers
and AccountsService correctly block their setup previews. No workstation
package was installed. Fresh laptop plugin setup and the promoted greeter
mount after sync/reboot remain separate installed-session checks.

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
- [x] Resolve the `/etc/xdg/user-dirs.defaults` ownership question (2026-09-17):
  Chezmoi owns the per-user file and directory creation, so the unselected
  prepared asset is removed instead of migrated. `~/.config/user-dirs.dirs`
  carries the standard entries plus Projects, Screenshots, Wallpapers and
  Recordings, and the Files hook creates every declared directory before
  login-time `xdg-user-dirs-update` can reset missing paths to home. Fedora
  44's xdg-user-dirs also no longer ships the system defaults path, so no
  package-owned file exists to adopt. `files accept` keeps requiring prior
  ownership for every other managed file.
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

The native XDG user directories are owned by Chezmoi, including their folders
and the per-user file; Nimbus ships no system defaults. Upstream
[documents the file's role](https://www.freedesktop.org/wiki/Software/xdg-user-dirs/).
A comparison with
[CachyOS-Settings](https://github.com/CachyOS/CachyOS-Settings)
did not justify extra system policy. SSH server access stays unmanaged, and the
Windows VM stays deferred.

### Engine and integration work

- [x] Implement the approved
  [maintenance/setup proposal](ROADMAP.md#review-draft-maintenance-and-setup-workflows),
  including independent local note/task state and consolidated final reporting.
  Candidate validation and remaining operator trials are recorded below.

- [x] Enable native greeter auto-sync through Chezmoi and refresh the desktop
  wallpaper. On 2026-09-13, the effective setting is true and the native sync
  helper completed with exit status 0 through run0. No login session was ended.
- [x] Make constrained greeter authorization part of ordinary init/sync with
  approval, package prerequisites, numeric account identity, native enablement
  and verified system receipts. No separate postinstall. Noctalia 5.1.0 and
  Greeter 1.5.0 are installed on the desktop as of 2026-09-14.
- [x] Check the exact secure helper capability and constrained packaged action;
  reject legacy authorization and unknown status. Resolve protected status
  after approval and before snapshots; preserve other native users/rules.
- [x] Override Fedora's service timeout abort behavior for Noctalia and udiskie
  through Chezmoi-owned per-unit user drop-ins. Lockscreen repair checks
  and restores the same terminate timeout policy.
- [ ] Verify prompt-free wallpaper updates and the next greeter display on
  desktop and laptop after applying the candidate. The laptop stays untouched.

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
- [x] Publish the account-picture task in engine 0.4.6 and its signed COPR RPM.
  Greeter wallpaper behavior remains unchanged.

- [x] Simplify completed post-install output to its verified result. Omit setup
  titles and action/recovery guidance for completed checks, and use the same
  concise result after successful actions. CLI tests cover completed Noctalia
  selection and preview without repeating its update. This presentation change
  ships in engine 0.4.6; engine 0.4.5 retains the previous output.
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

- [x] Add standalone install/update commands to the WoWUp COPR helper, then
  connect the small post-install action and Topgrade updater. No artifact
  adapter in Nimbus. Helper 0.2.0 (COPR build 10989485) queues installation
  and updates through DNF and exposes `install --assumeyes`; the `wowup`
  postinstall task offers it after approval and reads the helper's JSON
  status. The owner tested the COPR installation on the desktop.
- [x] Confirm published helper interfaces and package sources. The signed
  helper RPM and its COPR project key are available.
- [x] Enable WoWUp's gaming selection (2026-09-17). `nimbus.toml` declares
  the `wowup-installer` COPR with its pinned key and priority 134,
  `components/wowup.toml` selects the helper, and only
  `profiles/gaming.toml` includes it, so the laptop keeps its smaller
  selection. Validation reports the desktop set at 22 components and 15
  repositories; a read-only VM plan on a candidate checkout lists
  `enable repository wowup-installer` and the package, and
  `postinstall wowup --plan` reaches the task. Native signatures stay
  enabled; no separate audit of the owner's package contents.
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

## Engine channels, 2026-09-18

Tracked by issue #72. The selector records which engine track an installation
follows; both tracks share the COPR key and repository ID.

- [x] Phase 1: selector schema 2 with a required `channel`, schema 1 read as
  stable, read-only `nimbus channel` with branch and repository drift, and
  `init --channel` recording the field. No silent selector writes on read or
  update paths.
- [x] Phase 2: bootstrap `--channel stable|develop` switching (branch fetch,
  repository rewrite, distro-sync, schema and compatibility gates), and the
  develop curl entry (`install-develop.sh`) that installs the develop track.
  The schema gate reads the target checkout's state constant against
  `/var/lib/nimbus/schema`; the same COPR key covers both channels.
- [x] Phase 3a: the rolling develop source and packaging. The develop
  workflow republishes `nimbus-<version>-vendor.tar.gz` (dotted asset name,
  tilde RPM version) under the `develop` release on every push, and the copr
  repository gained the `nimbus-develop` project entry, recipe, prepare
  resolver and native scope with a locally built SRPM
  (`0.6.0~dev.<date>git<sha>`).
- [ ] Phase 3b: create the `furyfree/nimbus-develop` COPR project and run
  its first build, then install from it in a VM.
- [ ] Phase 4: clean-VM drills for both directions, drift refusal paths and a
  one-time notice on devices upgrading to a channel-aware engine.

## Maintenance and setup candidate, 2026-09-13

- [x] Engine check/update precedes definitions and Git fetches. Fresh root DNF
  metadata, enabled trusted source checks, native RPM ordering and replacement
  verification fail closed. Restart preserves selection and flags. Configuration
  sync and Topgrade run once, with the system callback's report merged at the end.
- [x] Init keeps visible native progress with logging, checks selected secret
  prerequisites before rendering, and reports partial work, differences, notes
  and remaining setup together. Bootstrap still validates the obtained engine
  before init; direct init retains compatibility rejection, without self-update.
- [x] Named postinstall help/status/plan, guided confirmations, verification,
  reset and manual acknowledgment. Complete 1Password flow applies only selected
  Chezmoi SSH/Git targets and rechecks them. SSH opt-in changes require renewed
  GUI confirmation; read-only inspection never authenticates to 1Password.
- [x] Private atomic per-machine state separates displayed note revisions from
  task acknowledgments and verified completion. Missing, corrupt, concurrent,
  reset and changed-native-state cases are covered. Chezmoi remains standalone.
- [x] Nimbus `just check`, `just validate`, and focused Go race checks pass.
  Real terminal subprocesses verify incremental logged output, hidden-input
  privacy, resizing, cancellation and restoration. No host sudo or 1Password
  operation was invoked.
- [x] Disposable network-disabled Fedora verifies signed fresh RPM installation,
  an update hidden by a still-valid seven-day root cache, plain-sync refusal,
  replacement/restart with explicit selection, and completed-engine reporting
  after incompatible definitions stop later work. The repeatable test lives
  under `tools/vm/maintenance`; usage is in the README.
- [x] Chezmoi `just check`: 147 tests pass with three optional skips
  (Niri unavailable, opt-in Neovim downloads and Zathura GUI). Secret-skipping
  managed/status/diff/verify checks run without applying. The five always-run
  hooks remain the expected status/verify differences; no managed file drift.
- [ ] Run full fresh init/login and repeated maintenance on a disposable Fedora
  VM using the candidate. Fixtures exercise init handoff/retry; RPM and terminal
  drills do not establish end-to-end desktop installation or snapshot recovery.
- [ ] After local review, exercise actual GUI confirmation, vault authorization,
  SSH/signing configuration and postinstall retry. Remote login, signing-key
  registration and physical-device setup remain manual verification.
- [ ] Test the released RPM on the laptop only after separate authorization.
  No laptop edits or live apply occurred during implementation.

Effective-configuration reporting currently recognizes Noctalia's lockscreen
widget disable override; it is not a general desktop preference comparison.
Native task inspectors remain authoritative. Session/reboot notices cover
Nimbus's managed activation requirements, not a guarantee that every vendor
application has restarted. Greeter authorization is now implemented in normal
init/sync; its desktop and laptop wallpaper checks remain outstanding.

Owner smoke-test follow-up: Session notices now
separate login-shell changes from group changes: current groups are checked
directly, and a later boot clears other recorded session activation requirements.
An uninspected session start remains explicitly unknown. Regressions cover mixed
receipt cases. Matching file adoptions after unrelated
definition changes refresh receipts; they do not rewrite matching file bytes.

Owner clarification: setup notes belong entirely to Nimbus. The schema-1
catalog now lives at the Nimbus checkout root and is loaded without invoking
Chezmoi or Git. All seven notes share this catalog, with unchanged IDs/revisions
and explicit dotfiles applicability. Chezmoi's catalog, reader and catalog test
are removed. Nimbus tests cover the shipped catalog, revision delivery and
operation without Chezmoi, including missing-catalog errors.

Release preparation targets Nimbus 0.5.0, with the matching minimum engine in
these definitions. Commit, push, GitHub release and COPR publication are now
authorized. Full graphical init/login, hardware and real-account trials remain
open; publication does not close those gates.

Release evidence, 2026-09-13: [Nimbus v0.5.0][maintenance-release] is published
from commit `a3ec141c5578ebd26d61e61e458568157d0446b1`. Chezmoi commit
`7aa5957` and COPR recipe commit `96e71d7` are pushed to main. All three
repositories' GitHub checks passed, as did the release workflow's Go 1.26.7
offline vendored build/tests. The archive matches all 296 tagged regular files
and executable modes; its 754 vendored files and 23 notices are unchanged.
Archive SHA-256:
`2722499273598eb23f5f1bef2768d295c05254400750a1eb52ea1236ca63ca6b`.

The complete offline Fedora packaging gate and an actual source-RPM rebuild
pass. The first extra rebuild used an anonymous container UID and failed
current-user fixture checks; repeating with a normal unprivileged build account
passes without source or test changes. Prepared and COPR-published source RPMs
contain the exact archive and reviewed spec.

[COPR build 10981938][maintenance-copr] is submitted through the
[publication workflow][maintenance-publish]. At handoff it is waiting for
Copr DistGit source import; the public import queue also contains dozens of
other waiting builds. The hosted workflow continues automatically. The signed
binary RPM is not yet available: signature/payload verification and an extracted
binary smoke test remain pending. No desktop or laptop package update was run.

[maintenance-release]: https://github.com/Furyfree/nimbus/releases/tag/v0.5.0
[maintenance-copr]: https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10981938/
[maintenance-publish]: https://github.com/Furyfree/copr/actions/runs/34770315892

## UWSM session integration

The local implementation preserves browser argv while routing launches through
an active UWSM session. Lockscreen repair recognizes Chezmoi's managed Noctalia
service, checks its graceful-stop policy and restores it through UWSM.
Chezmoi owns environment files, app shortcuts, shell startup and bar preferences.
Fresh-login/logout and visual desktop validation remain operator checks; see
[README](../README.md#uwsm-session-integration). No laptop changes or release
are part of this implementation.

Validation: `just check` and `just validate` passed. Fake-source tests cover
literal browser arguments, direct/UWSM launch, missing wrappers, changed
supervisors, failed stops, surviving children and restart failure. A temporary
native UWSM service confirmed the selected slice, graceful-stop properties and
inactive state after collection. No running desktop shell was restarted.

## Greeter authorization and timeout follow-up, 2026-09-14

Candidate definitions require engine 0.5.3 for `greeter_passwordless_sync`.
Both init and sync use the same approved system operation, including private
installation logging without losing native status diagnostics. Repeated runs
recheck native state; receipts alone cannot claim authorization.

- [x] Pass Nimbus `just check`, `just validate`, and race tests for CLI, apply,
  plan and postinstall. Tests cover preview/refusal without privileged status,
  init with logs, apply, repeat, repair, failed verification, changed account
  identity, unsafe policy and retained native authorization on deselection.
- [x] Exercise the installed Greeter 1.5.0 binaries in an isolated Fedora
  container: enable, status, byte-identical repeated enable, two-user removal,
  last-user removal and refusal to overwrite an unfamiliar rule. No host
  Polkit state was changed; no active graphical authorization was exercised.
- [x] Reproduce Fedora's abort timeout overriding transient launch properties.
  Use service-specific user drop-ins instead. A temporary native service
  ignored SIGTERM, survived the timeout without SIGABRT and exited normally;
  all temporary unit files were removed.
- [x] Apply those two Chezmoi drop-ins locally and reload the user manager.
  Both running services now report `TimeoutStopFailureMode=terminate`; neither
  was restarted. Noctalia retains its ten-second timeout and `SendSIGKILL=no`.
- [x] Preview the desktop with the candidate. It includes the pending Steam
  source correction and greeter authorization. No system sync was applied.
- [ ] Activate the desktop's native greeter authorization and visually verify
  prompt-free wallpaper updates. Noninteractive sudo reports that a password
  is required; no authentication prompt was opened.
- [x] Commit and push Nimbus, Chezmoi and the updated COPR recipe after the
  owner's authorization. Publish Nimbus v0.5.3 and submit COPR build 10984155.
  No workstation package upgrade or laptop changes were performed.

### Version 0.5.3 release verification

[Nimbus v0.5.3](https://github.com/Furyfree/nimbus/releases/tag/v0.5.3)
is published from commit `4635c6d22e940e6264f084fde03a930d782c3555`.
Matching Chezmoi changes are published at `18c4364`; the COPR recipe is at
`6deb1f8`. All three repositories' CI checks passed, as did Nimbus's offline
vendored release build/tests and the complete offline Fedora packaging gate.

The source archive SHA-256 is
`4885f72e5bdc6e8f31514d92be9bdc27277b15e6f5998fcbbc42675539163537`.
All 348 tagged files and executable bits match. The 754 vendored files and
dependency notices are unchanged from v0.5.2. Prepared, submitted and published
source RPMs preserve the exact archive and reviewed spec. An unprivileged,
network-disabled source RPM rebuild passed all tests and definition validation.
Its payload contains only the engine and licenses, without scripts or triggers.

[COPR build 10984155](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10984155/)
published `nimbus-0.5.3-0.1.fc44.x86_64` through the successful
[publication workflow](https://github.com/Furyfree/copr/actions/runs/34846942114).
Repository metadata and both package checksums match. Published binary and
rebuilt source RPM signatures and digests pass in an isolated database with
only the pinned project key. The rebuilt source RPM preserves the exact archive
and spec. Binary RPM SHA-256 is
`19795d3e2a1cc1bba735df5b7927131341af826cbea6dfcfc592371639f473f4`.
The payload contains only Nimbus and licenses, without scripts or triggers.
An unprivileged offline container confirms version 0.5.3, all three machine
definitions and postinstall help. Desktop greeter authorization, visual
wallpaper updates and laptop installation remain operator checks.

One documentation-only CI run timed out in the terminal interruption test.
Its unchanged rerun passed; ten consecutive focused local runs and COPR's
package tests also passed. Investigate if this intermittent timeout recurs.

## Status count correction, 2026-09-14

The local fix classifies system checks and configuration changes as pending
instead of package installations. The greeter's permission-protected recheck
therefore reports zero installations and one pending operation after setup.
No approval, native inspection, package transaction or Chezmoi behavior changes.
Regression checks cover mixed package/resource work and human/JSON greeter
status after init and sync, without privilege escalation or mutation.
Validation: focused regressions, `just check` and `just validate` passed.

## CLI simplification, 2026-09-14

Owner-approved local implementation after version-attributed desktop/laptop
analysis. Original install logs came from 0.4.0 and 0.4.3; current command
observations and DNF transactions came from 0.5.3. Routine sync had no retained
Nimbus transcript, so old installation output was not treated as current output.

- [x] Compact default previews with full `--verbose` detail; preserve mutations,
  source restrictions, blocked reasons, native streams and file conflicts.
- [x] Delegate engine transaction approval once to DNF; retain separate Chezmoi
  approval and propagate explicit combined-upgrade approval to Topgrade/callback.
- [x] Announce read-only administrator greeter inspection before sync mutation
  approval; keep previews unprivileged and reject unrelated init replan drift.
- [x] Skip unrelated update queries during configuration replans; retain full
  source policies and transaction verification. Scope named task inspection.
- [x] Remove the positional postinstall adapter; render result status explicitly
  and consolidate routine success reporting without hiding failures.
- [x] Replace duplicate lockscreen override parsing with authoritative task
  inspection. Keep historical MOK details in explicit/verbose inspection.
- [x] Add private bounded metadata records; exclude native/error/configuration
  contents. Include the final Nimbus report in init's existing engine log.
- [x] Quiet unchanged Chezmoi hook messages while retaining native Mise output,
  extension repair and verification, icon refresh and standalone behavior.
- [x] Finish the full regression gates and final independent Claude Fable 5.1
  and Grok 4.6 validation; record exact outcomes below.
- [ ] Owner trial of interactive desktop maintenance after review. No live
  installation, desktop apply, laptop changes or release was performed.

The implementation deliberately retains existing native lifecycle boundaries
and the legacy prose formatter; this is not a new updater or UI framework.

Validation evidence for this candidate:

- Nimbus `just check` and `just validate` pass. CLI, output and postinstall
  packages also pass with Go's race detector.
- The disposable signed-RPM maintenance drill passes, including stale root
  metadata, engine-first replacement/restart and incompatible definitions.
  Its synthetic 0.4.6/0.4.7 labels exercise this candidate code, not old releases.
- Native Fedora DNF constraint/source fixtures pass: competing providers,
  source-bound installs and upgrades, wrong-source repair, signature checks,
  dependencies and unavailable selected providers. Container fixtures use stdin
  transfer rather than relabeling or mounting workstation files.
- Chezmoi's complete gate passes (156 tests; optional Neovim download, Niri
  parser and Zathura GUI checks skipped). Its read-only diff is empty; status
  lists five always-run hooks, and verification passes with scripts excluded.
- Test isolation initially allowed mocked upgrade diagnostics into the live
  state directory. Only the identified synthetic records were removed. CLI
  TestMain now isolates HOME and XDG paths; subsequent tests leave no live run
  records. No real upgrade or application authentication occurred.
- Independent review caught diagnostic-retention validation, error-reporting
  and approval-detail issues; regression tests cover their corrections. The
  suggested removal of the greeter snapshot-policy drift check was rejected:
  inspected snapshot policy is independent of greeter action, and must still
  match approval.

Final validator results: Claude Fable 5.1 (`claude-fable-5-1`) and Grok 4.6
(`grok-4.6-build`) completed source-only reviews and follow-ups with no remaining
blockers. A separate read-only validator also confirmed the error-reporting and
phase-classification fixes. Reviews did not run authentication or native setup.
The observed desktop preview is 15 lines by default versus 24 with `--verbose`.
Optional Chezmoi Markdown lint retains the same 81 pre-existing findings and no
new findings. Interactive approvals and desktop behavior remain owner trials.

### Version 0.5.4 publication

The owner authorized publication after reviewing the local implementation.
[Nimbus v0.5.4](https://github.com/Furyfree/nimbus/releases/tag/v0.5.4) selects
commit `b9df8a081d0f60d6dadfa51fb1f94076c6e21e64`; matching Chezmoi changes are
on main at `b4851a6`. All 353 tagged files match the vendored release archive;
the 754 vendored files and dependency notices are unchanged from 0.5.3.
Archive SHA-256:
`545f42b8231256c866310e34cfcc568f7f9ee6a5cb9f7326657ec31d0f717d48`.

The release checks, vendored build/tests, Chezmoi CI, and packaging checks pass.
Push CI encountered the previously recorded terminal-interruption timeout once;
the unchanged rerun and ten focused local repetitions passed. This intermittent
test remains a follow-up, not a demonstrated fix in 0.5.4.

[COPR build 10984708](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10984708/)
and the
[publication workflow](https://github.com/Furyfree/copr/actions/runs/34857112822)
succeeded. Repository metadata exposes `nimbus-0.5.4-0.1.fc44.x86_64` and matches
the downloaded RPM checksum:
`18dff1db76097a9678ca241df58a8fdf4fbc71a53ed5a0727ba9ae32adf22c8a`.

Binary and source signatures/digests pass in an isolated RPM database containing
only the pinned COPR key. The source RPM retains the exact reviewed archive and
spec; the binary contains only Nimbus and licenses, without scripts or triggers.
An offline unprivileged container verifies the released engine version, all
three machine definitions and command help. A separate offline source-RPM
rebuild passes with Fedora tools and a native build-user account. Initial local
container mount and missing-user/group failures were environment setup errors;
no host packages, permissions or accounts were modified to resolve them.
Desktop/laptop upgrade and interactive maintenance remain operator trials.

### OnePassword first install and preview

The laptop had no `~/.config/1Password/ssh/` directory. Targeted Chezmoi apply
omitted managed parents, so preview could succeed while the first apply failed.
The task now includes parents, prints native create/update status before approval,
and offers `--diff` for a private preview without a pager. Scripts stay excluded;
local-conflict prompts, explicit SSH opt-in and final verification remain intact.

Validation includes a native Chezmoi trial in disposable directories: missing
parents are created with managed permissions, unrelated files stay untouched,
and an unanswered conflict preserves a local edit. Fixture tests cover guided
setup, optional diff, incomplete verification and completion evidence. No desktop
configuration, authentication or laptop changes are part of this release work.

`just check`, `just validate`, and focused 1Password race tests pass. Native
Chezmoi coverage runs where the executable is available; it passed locally.
Source-only CI may skip that native trial while retaining the fixture tests.

### Version 0.5.5 publication

[Nimbus v0.5.5](https://github.com/Furyfree/nimbus/releases/tag/v0.5.5) selects
commit `d0687535653e0045f818f62d2609cd4140e54d16`. All 353 tagged files match
the source archive; vendored dependencies remain unchanged from 0.5.4.
Archive SHA-256:
`de3b682f01b867a0c7ba2ff4fb9a0c4743db6bc6e9dd37e2cec58d93cc3a6a30`.
Nimbus push CI, release checks and the vendored build/tests passed.

Packaging commit `5457a9ef0884206c323b161b8346a1d867bdf43c` passed the full
local offline gate and hosted checks.
[COPR build 10984870](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10984870/)
and the
[publication workflow](https://github.com/Furyfree/copr/actions/runs/34861313985)
succeeded. Repository metadata exposes `nimbus-0.5.5-0.1.fc44.x86_64`, whose
SHA-256 matches the downloaded package:
`8996d7207a40328e614848c2ef2c03383627972fbe985e487ab7497c59ad86a0`.

The binary and source RPM pass signature/digest checks in an isolated keyring
containing only the pinned COPR key. Source archive and spec match the reviewed
inputs. The payload contains only Nimbus and licenses, with no scripts/triggers.
An offline unprivileged container verified version, all three machine definitions
and the new task help. Desktop/laptop update and interactive 1Password setup
remain owner trials; neither machine was changed during release validation.

### Lockscreen screen-adjustment verification

The owner confirmed that the laptop's repaired lockscreen looks correct on
Noctalia 5.1.0 with Nimbus 0.5.5. Native startup changed the layout reference
height from 1080 to 1200 and proportionally adjusted all four widget centers.
The effective layout matched those adjusted values; unrelated preferences were
unchanged. Exact-byte and exact-coordinate verification falsely rejected it.

Status and post-restart verification now share relative-position comparison
with bounded rounding tolerance. Saved and effective layouts must both match;
widget identities, order, appearance and box sizes remain exact. Native settings
reserialization is accepted only when unrelated parsed values remain unchanged.
Pre-write digests, shutdown-flush protection, private backups and managed-file
checks are unchanged. Already equivalent saved layouts need no restart.

Synthetic fixtures reproduce the observed laptop coordinates and cover width
adjustments, wrong positions, sizes, styles, order, missing/extra widgets, invalid
numbers, restart rewrites, unrelated edits and effective-layout disagreement.
No laptop changes or live desktop repair are part of this implementation.

Validation: `just check`, `just validate`, and focused lockscreen race tests pass.
The owner authorized publication as a patch release. The laptop was not repaired
again; its earlier visual confirmation remains the live result. Testing through
the installed Nimbus package remains an owner trial.

### Version 0.5.6 publication

[Nimbus v0.5.6](https://github.com/Furyfree/nimbus/releases/tag/v0.5.6) selects
commit `91df8a9c272e0734f44d01e490f0234752bac117`. All 353 tagged files match
the source archive; vendored dependencies are unchanged from 0.5.5.
Archive SHA-256:
`c67b8f4265755a376d37a566880389de785ab85dd2371f3d87858625d6aee286`.
Local checks, definition validation, focused race tests and the release workflow
passed. Push CI hit the previously recorded terminal-cancellation timeout; its
unchanged rerun passed.

Packaging commit `eff80d4664e03fc9acd0e21213cdd5286a87b962` passed the full
local offline gate and hosted checks. Initial COPR build 10984943 failed in
`test_foreground_restored` after printing `RESTORED`. The unchanged test performs
one nonblocking wait immediately after terminal closure, consistent with a
process-exit race. Three local handoff-suite repetitions passed. The handoff
implementation and tests are unchanged from 0.5.5; no tests were disabled.

[COPR retry build 10984982](https://copr.fedorainfracloud.org/coprs/furyfree/nimbus/build/10984982/)
and the retried
[publication workflow](https://github.com/Furyfree/copr/actions/runs/34863980532)
succeeded with the same source. Repository metadata exposes
`nimbus-0.5.6-0.1.fc44.x86_64`, matching the downloaded RPM SHA-256:
`c71e3f552869b01c6b41693f9550306b661947d5d27fa8b1d79f5336e125855e`.

Binary and source RPM signatures/digests pass with only the pinned COPR key
trusted. Source archive/spec match reviewed inputs; the payload contains only
Nimbus and licenses, without scripts/triggers. Offline unprivileged checks
verify the version, all machine definitions and command help. The owner will
update and test the laptop through Nimbus; release work made no laptop or live
desktop configuration changes.

- [x] Review terminal-cancellation and foreground-restoration test timing and
  reaping (2026-09-17). Production `StreamLogged` now subscribes to SIGWINCH
  before starting the child, closing a window where a resize arriving right
  after start could be lost; the PTY test asserts that the parent terminal
  returns to canonical input with echo, that no child remains unreaped, and it
  keeps the privacy, resize and exit-130 checks. Removing `term.Restore` fails
  the restoration assertion deterministically, so the check has teeth. 300
  repetitions and 60 with `-race` passed, on top of the earlier 500-repetition
  evidence for the unbuffered readiness writes. No retry-based evidence is
  used.

## Version 0.5.7 publication

[Nimbus v0.5.7](https://github.com/Furyfree/nimbus/releases/tag/v0.5.7) selects
`631f96a077dfd972e0c69ea5fbf6623986605734` for guided Tailscale initial sign-in
and the DTU eduroam certificate task. All 358 tagged files and executable bits
match the release archive; modules are unchanged from 0.5.6. Archive SHA-256:
`a1729743a1d47776888895744d9c4cece9f74e99fa86dbfbb6982ba2ddb135e0`.

The complete local gate, definition validation, focused DTU/Tailscale race
tests, push CI and Go 1.26.7 offline vendored release tests passed. COPR recipe
`07a027c` selects that version and checksum. The packaging gate passed in an
offline Fedora container using a copied checkout after the host-mounted
wrapper was denied access; host permissions and SELinux were not changed.

[Publication workflow](https://github.com/Furyfree/copr/actions/runs/34882682523)
was dispatched for Nimbus only. The owner requested handoff after COPR
submission, without monitoring the build. A queued build is not proof of a
published RPM; signed-package verification and the native browser sign-in,
enforcing SELinux and campus Wi-Fi trials remain pending. No Nimbus setup
was applied to either workstation during release preparation.

## Version 0.5.8 terminal-test correction

COPR build 10985459 failed the 0.5.7 RPM check in
`test_foreground_restored`: it captured `RESTORED` but treated terminal closure
as proof that a single nonblocking process wait must succeed. The same race
had already affected the initial 0.5.6 build. The 0.5.7 source release remains
published; that failed build did not publish its RPM.

The test now waits for process exit within its original five-second deadline
and always reaps its child on failure. It still requires successful exit and
the terminal-restoration marker. A regression deliberately closes the terminal
before exiting; restoring the old single-wait check makes that case fail.
The installer supervisor and all application behavior remain unchanged.

Publication of 0.5.8 is authorized. It also includes the 0.5.7 Tailscale and
DTU setup changes. Native account sign-in, enforcing SELinux and campus Wi-Fi
remain user trials; successful build checks do not establish those results.

Validation: `just check` and `just validate` pass. Both foreground cases passed
30 local repetitions and 10 more in a network-disabled Fedora container with
no host mounts; its complete 10-test handoff suite also passed. The deliberately
restored old waiting logic fails the delayed-exit regression after `RESTORED`.
No tests were skipped and no live installer, sign-in or system repair ran.

## Version 0.5.9 terminal-interrupt test correction

The 0.5.8 source workflow passed the foreground-restoration suite but failed
`TestLoggedTTYProgressPrivacyResizeAndCancellation`: the shell printed WAITING
before starting sleep, allowing Ctrl+C to race with child startup. The 0.5.8
tag remains immutable; no source release or COPR build was published for it.

The fixture now execs a worker which installs its interrupt handler before
printing WAITING. It requires the worker's INTERRUPTED marker and exit 130,
so an unrelated command failure cannot satisfy the cancellation assertion.
The existing eight-second test deadline remains unchanged. This modifies test
synchronization only; native terminal relay behavior is unchanged.

Release 0.5.9 includes both terminal-test corrections and all 0.5.7 setup work.

Validation: `just check` and `just validate` pass. The interrupt test passed
40 consecutive local runs and 30 in an offline Fedora container using Go
1.26.8. Its integration suite also passed with vendored modules and no host
mounts. No production terminal code, test deadline or live setup was changed.

[Release v0.5.9](https://github.com/Furyfree/nimbus/releases/tag/v0.5.9)
is published from `4be55e2b3825ebfd91dbb9095ba21bfb99a0e700`. Push CI and
[source preparation](https://github.com/Furyfree/nimbus/actions/runs/34897443298)
passed. All 358 tagged files and executable bits match the release archive;
vendored files and license notices are unchanged from 0.5.7. Archive SHA-256:
`4da5ba421be8bc5ea8c9212dbbdad7935c26c142a8cab0510c2f9ee2b6414619`.

COPR recipe `2b9cb2d` selects that archive. Its full packaging gate passed in
an offline Fedora container with a copied checkout and no host mounts.
[Publication](https://github.com/Furyfree/copr/actions/runs/34897812125)
was dispatched for Nimbus only. Handoff is after submission, without monitoring
build completion; RPM availability and signature checks remain pending.
Neither workstation's Nimbus installation was changed during this release.

## DTU native SELinux verification correction

Nimbus 0.5.9 installed the desktop certificate correctly but reported failure:
its full-context equality check rejected `unconfined_u` where policy defaults
use `system_u`, although ordinary restorecon preserves this user field.
Native `matchpathcon -V` verified both the directory and certificate; checksum,
root ownership and mode 0644 matched.

Use native label verification, retaining complete observations for approval.
Only a recognized native mismatch permits repair; failed or unrecognized
inspection remains unknown and has no action. Report specific ownership,
permissions, content, missing-file or label failures. Preserve pinned download,
certificate validation and nonrecursive restorecon behavior.

Regressions cover preserved user fields during inspection, installation and
repeat commands, genuine directory/file label mismatches, inspection errors,
unrecognized results, metadata failures and changed certificate content.
The local source command and its preview both verified the existing desktop
certificate without download, sudo or file changes. Normal execution recorded
completion through the existing user-state mechanism. Wi-Fi was not tested.

The owner confirmed the local command works and authorized commit, push and
publication as version 0.5.10.

Validation: focused DTU tests and their race-detector run pass. The full
`just check` gate passes after correcting an extra blank line in this note's
roadmap link. Existing installation and failure paths remain covered by
isolated fixtures; no fresh privileged installation was run on the desktop.

[Release v0.5.10](https://github.com/Furyfree/nimbus/releases/tag/v0.5.10)
is published from `a36618114b1c5287dfe626d9eec0c372f7c4d899`.
[Source preparation](https://github.com/Furyfree/nimbus/actions/runs/34899326543)
passed its full checks and vendored tests. All 358 tagged files and executable
bits match the archive; vendor files and license notices match 0.5.9.
Archive SHA-256:
`4f0959aef43e994fb63a59f12f87e6d2c2255785b6abeab60f6d3cd9257e29d3`.

COPR recipe `9bc0221` selects the reviewed archive. The complete packaging gate
passed in an offline Fedora container using a copied checkout with no host
mounts. [Publication](https://github.com/Furyfree/copr/actions/runs/34899644892)
was dispatched for Nimbus only. The owner's workstation installed
`nimbus-0.5.10-0.1.fc44.x86_64` from the COPR project; the RPM signature key
`DD1D48E2CA0E9F6A` matches the pinned fingerprint
`8FF8E546C3ABE44146CFA411DD1D48E2CA0E9F6A`, and `nimbus version` reports
0.5.10. No workstation package installation was changed during publication.

## Terminal interrupt buffered-output correction

The interrupt fixture could re-enter Python's buffered stdout: Ctrl+C could
arrive after WAITING reached the parent but before its `print` finished, and
the handler called `print` again. Both markers now use `os.write`. Terminal
I/O, resize, private-input exclusion, INTERRUPTED and exit 130 assertions,
and the existing deadlines are unchanged. No production code changed.

Validation on 2026-09-15:

- A controlled reproducer delays the raw readiness write's return while the
  buffered writer holds its lock. The original fixture fails with a reentrant
  call in all 10 trials; the correction returns INTERRUPTED and exit 130 in
  all 10. The original also passed 200 ordinary repetitions, illustrating why
  repetition alone is insufficient evidence for this race.
- `TestLoggedTTYProgressPrivacyResizeAndCancellation` passes 500 repetitions
  locally with Go 1.27.1 and 500 with Fedora's Go 1.26.8; both use Python 3.14.7.
- `GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 just check` and
  `just validate` pass. The first unisolated gate stalled in the VM-staging
  fixture's Git signing through 1Password; no signing settings were changed.
- Fedora's vendored `go test -mod=vendor ./...`, binary build and checkout
  validation pass as a dedicated unprivileged test user, with networking
  disabled, copied source and no host mounts. Initial container checks lacked
  the test user's identity/group; supplying them resolved those failures.
  Native DNF opt-in tests and the native Chezmoi first-install trial were
  skipped; subprocess-only helpers run through their parent tests.

This is an unpublished source correction; no release or installed engine was
changed.

## Voxtype postinstall setup, 2026-09-17

Implemented in source; not released as a package.

- [x] Add the `voxtype` postinstall task, selected with the Voxtype package.
  Read-only inspection runs `voxtype info models --json --engine whisper`,
  reads the model from the applied `~/.config/voxtype/config.toml`
  (`[whisper].model`) and reads `systemctl --user` unit state; Nimbus never
  reads model files, recordings or other home data. Package presence without a
  verified receipt stays blocked.
- [x] Offer one approved native workflow: `voxtype setup --download --model
  <configured>` when the catalog reports that model missing, then `systemctl
  --user enable --now voxtype.service`. An existing model only needs
  enablement. `--plan` stays read-only; the digest recheck and operation lock
  cover execution; completion is verified by re-inspection, never by the
  commands.
- [x] Read the model from the managed config so the desktop and laptop
  download different models (Furyfree/nimbus#69); a missing, model-less or
  invalid config blocks with guidance instead of guessing.
- [x] Reject forged or reordered workflows. The model name is
  shape-validated and no model paths are guessed.
- [x] Tests cover a missing package, a missing binary, an unreadable catalog,
  an unlisted model, disabled/enabled unit states, forged actions, a missing
  or invalid config, a per-machine model, and a CLI preview plus approved run
  where the native catalog and unit state change. `just check` passes.
- [ ] Release the task in a package and run it on the laptop (owner present).

## Persistent hostname, 2026-09-17

Implemented in source; not released as a package.

- [x] Every machine exposes a `hostname` postinstall task. The intended name
  derives from the machine ID as `nimbus-<id>` (`nimbus-laptop`,
  `nimbus-desktop`). Keeping it out of the machine manifest avoids a strict
  decoding break: the installed 0.5.10 engine would reject an unknown field
  and require a min-engine bump.
- [x] Inspection runs `hostnamectl --static` only, shows the observed and
  intended name, and offers one approved change:
  `sudo -- /usr/bin/hostnamectl set-hostname NAME`. Forged or reordered
  commands are rejected. A matching static name is complete; the transient
  DHCP name is never used or altered, and no browser data or profile locks
  are touched. Browsers are asked to be closed first.
- [x] Tests cover matching, differing, invalid derivation, missing tool, read
  failure, unrecognized names, forged actions, and single-task inspection
  parity. `just check` and `just validate` pass.
- [ ] Set the hostname on the laptop and desktop (owner present) and confirm
  Brave reopens the same profile after a reboot.
