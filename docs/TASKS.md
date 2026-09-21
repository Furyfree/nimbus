# Current work

Contract: [SPEC.md](SPEC.md). Order: [ROADMAP.md](ROADMAP.md).

Only open work, decisions and the evidence needed to act. Released history
lives in Git and the release notes.

## Release 0.6.1

Release checklist: [#40](https://github.com/Furyfree/nimbus/issues/40).

- [x] Released 2026-09-20: tag `v0.6.1` at `94821f2`, GitHub release with
  written notes, stable COPR build 11008140 (`nimbus-0.6.1-0.1.fc44`). All
  407 tagged files match the archive; the RPM is signed by the pinned key,
  its payload matches the tag and it carries no scriptlets; the stable
  repository serves it. `develop-version` is 0.6.2.
- [ ] Laptop and real desktop, both on 0.5.10: `nimbus sync --plan` first.
  The old engine upgrades itself and restarts before it reads definitions.
  The first apply selects the boot theme and rebuilds every initramfs; type
  the passphrase at the prompt afterwards. The rescue entry under Previous
  kernels is the fallback.
- [ ] Desktop: install the released package on the clean Fedora image and
  verify passphrase prompt, auto-login, GRUB 5 seconds, `nvidia-mok` and
  `nvidia-smi` (#34).
- [ ] Channel phase 4 (#72): reverse drill after a `main` release carries
  the channel code and payload; verify the schema 1 selector migration
  and drift refusals in a clean VM.
- [ ] Documentation reconciliation (#39), completed last.
- [ ] Package after the release archive exists: `nimbus.spec` version and
  archive checksum together, boot-theme payload as plain 0755 files,
  `boot-theme` selected in `common`.
- [ ] Bare metal: install the released package and run the #40 checklist.

Decisions:

- Versioning (2026-09-19): `develop-version` names the next stable release
  and a stable tag must number at or above it, so develop builds always sort
  below the release that follows them; the release source step refuses a
  lower tag. The next release is 0.6.1, skipping 0.6.0.
- The `nimbus.toml` `min_engine` bump waits until `develop-version` moves to
  0.6.2, because a develop build sorts below its own base and would reject
  itself.
- Schema 1 selectors migrate on sync (2026-09-20): the first sync of a
  schema 2 engine records the selector as schema 2 on stable, shown in the
  preview and the closing report (`internal/cli/channel.go`). It replaced
  the one-time notice and its stored display record. Open: the laptop and
  the real desktop, both schema 1, migrate with the 0.6.2 engine.

## Installation output and logging

- [x] Closing report trimmed to summary, differences, failures, reboot and
  logout lines, verification problems, a pre-reboot line for pending tasks
  that must run before the reboot (`nvidia-mok`), and one `nimbus
  setup-notes` pointer (`internal/cli/final_report.go`).
- [x] `nimbus setup-notes` is the guidance hub: it points at `nimbus
  postinstall status` and consumes displayed revisions; init and sync only
  count unseen notes (`internal/cli/setup_notes.go`).
- [x] Public install commands stream through a same-session logging pty so
  dnf5 shows live progress while sudo keeps its credential cache; the pty
  never owns stdin and changes no terminal modes
  (`internal/native/terminal.go`).
- [x] `engine.log` drops ANSI sequences and carriage-return overdraws at the
  log boundary; the terminal keeps the original bytes
  (`internal/cli/install_log.go`).
- [x] Init closes with the Nimbus banner, the single log path, the pending
  pre-reboot task (`nvidia-mok`), the reboot or logout instruction and the
  after-reboot `nimbus setup-notes` pointer; session-only checks no longer
  read as verification problems (`internal/cli/final_report.go`).
- [x] The shared-MOK paragraph, FDE step labels and the whole signed-image
  path were removed for passphrase-only encryption plus greetd auto-login
  (2026-09-20).
- [ ] Drill before the package ships: one sudo password prompt, live dnf
  progress, Ctrl+C, terminal resize and a readable `engine.log` in a
  disposable VM, then the bare-metal rerun on Secure Boot where the report
  shows the `nvidia-mok` pre-reboot line, one MokManager screen enrolls the
  NVIDIA key and a skipped enrollment still boots.

## Boot theme and menu

- [ ] Rerun the VM boot check for the mkconfig-only kernel hook and the
  unfiltered submenu guard (added 2026-09-18).
- [ ] Dual-booting desktop: verify the Windows entry and boot the older
  kernel end to end; the VM had no Windows entry.
- [ ] Verify Fedora flat-menu fallback after kernel removal and marker
  removal on installed Fedora.
- [ ] Typed-passphrase gate: the themed unlock prompt counts as verified
  on a physical machine only after a passphrase was typed at it there. The
  VM pass and the TPM unlock never exercised keyboard input.
  - [x] Desktop, manual 2026-09-20: a hand-written
    `90-nimbus-boot-display.conf` omitting `i915` plus
    `dracut --force --kver`. Paper Dark prompt after about eight seconds of
    black, keyboard worked, unlock, greetd auto-login into Hyprland, no
    failed units. `xe` loaded in the initramfs and declined the iGPU.
  - [x] Desktop, manual 2026-09-20: `UseSimpledrm=2` added by hand to the
    host `plymouthd.conf`. Prompt drawn at once (password request 4.2 s,
    accepted 8.2 s), clean handover to NVIDIA, greetd at 15.1 s.
  - [x] Desktop through sync: the owned drop-in (`i915 xe nouveau`) replaces
    the manual file, `initramfs-rebuild` runs, `lsinitrd` shows none of the
    three drivers and `UseSimpledrm=2` in the image's `plymouthd.conf` while
    the host file has none (remove the hand-added line first), the image is
    about 100 MB smaller, the rescue image checksum is unchanged, and the
    passphrase is typed again.
  - [x] Desktop: with `NetworkManager-wait-online.service` disabled,
    `graphical.target` no longer waits for DHCP (14 s measured 2026-09-20),
    Hyprland starts right after auto-login, and Docker and Tailscale work.
  - [x] Simplified theme (2026-09-20): rendered in the preview container
    only. On a real boot check the full-screen GRUB terminal box, the
    countdown number, the field and lock, and the loading bar after unlock.
  - [ ] Menu order and plain title (2026-09-20): on a real boot the menu
    reads Fedora Linux, Windows, Previous kernels, UEFI Firmware Settings;
    Previous kernels opens and lists the versioned entries and rescue; the
    default entry boots after the countdown. Verified so far by the preview
    (function-defined submenu, order) and a desktop dry run: a generated
    `grub.cfg` passed `grub2-script-check` with the live file untouched.
  - [x] Fresh desktop install through sync, 2026-09-20, engine
    `0.6.1~dev.20260920185636`: the owned drop-in has its receipt, the
    image holds no `i915`, `xe` or `nouveau` (63 MB) and carries
    `UseSimpledrm=2` while the host `plymouthd.conf` does not,
    `plymouth-theme` ran before `initramfs-rebuild`, `grub.cfg` orders
    Fedora, Windows, Previous kernels, UEFI, and the mirror title is
    `Fedora Linux`. The passphrase was typed at the pure black prompt and
    the session came up; `NetworkManager-wait-online` is disabled and
    userspace took 6.0 s. The lock icon was illegible at 1024x768 and was
    redrawn solid afterwards. `plymouthd` crashed at quit again (2 of 6
    boots, same backtrace in the script plugin, no visible effect).
  - [x] Owner's photos of that boot, 2026-09-20: the menu reads Fedora Linux,
    Windows Boot Manager, Previous kernels, UEFI Firmware Settings with the
    countdown number and the key hints; the prompt shows the wordmark, lock
    and field with bullets; the loading bar follows in the same place. Still
    to look at: opening Previous kernels, and the redrawn lock.
  - [ ] Laptop: type the passphrase at the themed prompt, and boot an
    older kernel from Previous kernels to confirm it is themed too, now that
    `initramfs-rebuild` rebuilds every kernel for `boot-theme`.
- [ ] GRUB text size on high-resolution panels (#74): the theme leaves
  GRUB's mode on `auto` on purpose, and under Secure Boot only the built-in
  fixed-size font loads. Look at the laptop's 2880x1800 menu and run
  `videoinfo` at the GRUB console on both machines before deciding anything.
- Accepted limitations live in [SPEC.md](SPEC.md#desktop-boot-and-recovery):
  BIOS-only systems untested; `set timeout=5` overrides `GRUB_TIMEOUT` and
  `menu_auto_hide`; a submenu reopened once shows no entries; a kernel
  installed while only one entry existed appears after the next successful
  `grub2-mkconfig`; removal blocks without a recorded usable theme.

## Disk encryption and login

Decision (2026-09-20): passphrase-only LUKS with greetd auto-login, matching
Omarchy and Ryoku. The signed UKI, TPM policy and `fde` component are removed.

- [ ] VM: auto-login reaches Hyprland, `secret-tool` reads the passwordless
  default keyring without a prompt, logout returns to the Noctalia greeter,
  and a manual password login cannot create an encrypted keyring.
- [ ] VM: with the NVIDIA key pending, skipping the MokManager screen leaves
  a bootable system (GRUB default, SSH reachable) and the next boot offers
  enrollment again.
- [ ] Bare metal: passphrase prompt in the Paper Dark theme, auto-login,
  GRUB menu visible for 5 seconds, `nvidia-mok` then `nvidia-smi`.
- [ ] Chezmoi: ship the passwordless `Default_keyring.keyring` and the
  `default` alias (user files; Nimbus never writes `$HOME`).
- Accepted limit: a rebooted machine waits at the passphrase prompt, so
  unattended remote boot is not available.

## Postinstall and owner trials

- [ ] Owner trial: compare postinstall service activity and startup state
  against systemd, including stopped/removed services and a deselected proxy.
- [ ] Fingerprint (#75): approve the local candidate with fprintd idle,
  check existing enrollment or complete native enrollment, then confirm
  passive status never starts the daemon. No release until requested.
- [ ] Laptop: after delivery of the service-ownership fix and explicit
  approval, sync, uninstall agent-proxy and reboot. Verify proxy services
  absent, Zeron off unless opted in, Wi-Fi connected, and Copilot, proxy config
  and Mise Herdr retained.
- [ ] Zeron daemon: installed opt-in, update, sync and opt-out trial. Fixtures
  verify the choice and failure paths; native desktop behavior is untested.
- [ ] NVIDIA MOK helper (#48 steps in #40): installed trial on a machine
  needing signing/enrollment; never enter a MOK password into Nimbus.
- [ ] Hostname: set `nimbus-<machine-id>` on laptop and desktop, then confirm
  Brave reopens the same profile after reboot.
- [ ] Voxtype: release the task in a package, then run it on the laptop and
  desktop; each machine downloads the model named in its managed config.
- [ ] DTU eduroam: verify DTU-only services and password renewal; campus
  Wi-Fi and SELinux-enforcing validation on the released candidate; review
  DTU's renewed CA bundle before the first intermediate expires 2027-12-02.
- [ ] Tailscale: validate native browser sign-in on the laptop through a
  released Nimbus; disposable-Fedora check of the operator setup.
- [ ] Greeter appearance: activate desktop authorization and verify
  prompt-free wallpaper sync on desktop and laptop, including the promoted
  systemd mount after sync and reboot.
- [ ] Lockscreen repair: owner visual check in a laptop desktop session
  (clock, date, avatar, password field).
- [ ] 1Password: desktop end-to-end guided setup and authorization;
  review terminal colors and guided sign-in recovery.
- [ ] Agent proxy: laptop trial; live Antigravity generation is untested
  because it is not signed in locally.
- [ ] ScrollOverview: fresh laptop installation through the task; fixtures
  do not prove a fresh download/build.
- [ ] Zeron: fresh installation and service/linger effects in a disposable
  VM; do not install or update it on the host during testing.
- [ ] Proton-CachyOS: initial ProtonPlus download and retry on disposable
  Fedora.
- [ ] Installed session: regular, private and webapp windows; published
  helper applications (ble.sh, LibrePods, Copilot helper); declared RPM
  source corrections on the desktop.
- [ ] NVIDIA X11 settings-loader mask: verify next-login behavior during
  the NVIDIA hardware trial.
- [ ] CLI simplification: owner trial of interactive desktop maintenance.

## Maintenance, Snapper and recovery

- [ ] Run full fresh init/login and repeated maintenance on a disposable
  Fedora VM with the candidate; RPM and terminal drills do not prove
  desktop installation or snapshot recovery.
- [ ] Snapper: fresh-layout setup without pre-created snapshot storage,
  snapshot pairs, retention, cleanup and a root restore. Existing-mount
  setup and native config listing already passed. No automatic-rollback
  claim; keep boot/EFI coverage explicit.
- [ ] TTY repair with absent or broken dotfiles; retirement of owned legacy
  session files on installed Fedora.
- [ ] Test repository updates, dirty-tree refusal and the upgraded-engine
  handoff on an installed system.
- [ ] Managed-file acceptance and removal, service/group restoration,
  failed activation, receipt retirement and legacy-state compatibility.
- [ ] Shell selection: apply shell-bearing definitions only after the
  matching engine is installed; verify switching and repair in disposable
  Fedora.

## Shared operations and dashboard

- [ ] Extract reconciliation, then setup, selection and file capture from
  Cobra; keep approvals, prompts and locks shared with the CLI.
- [ ] Replace description-based execution decisions with explicit approved
  data.
- [ ] Build the TUI over those operations (ROADMAP step 5, #36).

## Hardware-only checks

These wait for a proper installation and do not block independent local
work.

- [ ] GPU, suspend/resume and power behavior; audio, Bluetooth and
  Librepods; fingerprint.
- [ ] Portal file picking and screen sharing; UWSM logout, relogin and
  cleanup. Change startup only for a reproduced problem.
- [ ] Appearance, keyring unlock and application integration; Fastmail
  email links and notifications.
- [ ] Epson ET-5800 printing, trays, duplex and feeder scanning.
- [ ] Voxtype models and CPU/GPU backend on laptop and desktop after the
  owner's Omarchy comparison; check input permissions.

## Deferred

See [ROADMAP.md](ROADMAP.md#deferred-beyond-the-desktop-milestone): Windows
VM lifecycle (#33), TUI (#36), Snapper and session-recovery drills (#38),
UWSM browser launching, hibernation, boot archives, Home Assistant,
additional profiles and performance work.
