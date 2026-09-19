# Current work

Contract: [SPEC.md](SPEC.md). Order: [ROADMAP.md](ROADMAP.md).

Only open work, decisions and the evidence needed to act. Released history
lives in Git and the release notes.

## Release 0.6.1

Release checklist: [#40](https://github.com/Furyfree/nimbus/issues/40).

- [ ] FDE 3.5 hardware run: laptop first, then desktop with both MOKs
  enrolled before TPM enrollment (#34).
- [ ] Channel phase 4 (#72): reverse drill after a `main` release carries
  the channel code and payload; verify the one-time notice and drift
  refusals in a clean VM.
- [ ] Documentation reconciliation (#39), completed last.
- [ ] Package after the release archive exists: `nimbus.spec` version and
  archive checksum together, boot-theme/FDE payload as plain 0755 files,
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
- The one-time channel notice is implemented: the first channel-aware run
  against a schema 1 selector prints it once and records the display under
  `$XDG_STATE_HOME/nimbus` (`internal/cli/channel.go`).

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
  pre-reboot tasks (`nvidia-mok`, `fde`), the reboot or logout instruction
  and the after-reboot `nimbus setup-notes` pointer; session-only checks no
  longer read as verification problems (`internal/cli/final_report.go`).
- [ ] Drill before the package ships: one sudo password prompt, live dnf
  progress, Ctrl+C, terminal resize and a readable `engine.log` in a
  disposable VM, then the bare-metal rerun on Secure Boot where the report
  shows the `nvidia-mok` pre-reboot line.

## Boot theme and menu

- [ ] Rerun the VM boot check for the mkconfig-only kernel hook and the
  unfiltered submenu guard (added 2026-09-18).
- [ ] Dual-booting desktop: verify the Windows entry and boot the older
  kernel end to end; the VM had no Windows entry.
- [ ] Verify Fedora flat-menu fallback after kernel removal and marker
  removal on installed Fedora.
- Accepted limitations live in [SPEC.md](SPEC.md#desktop-and-recovery):
  older initramfs keeps the default Plymouth prompt until rebuild; BIOS-only
  systems untested; `set timeout=5` overrides `GRUB_TIMEOUT` and
  `menu_auto_hide`; a submenu reopened once shows no entries; a kernel
  installed while only one entry existed appears after the next successful
  `grub2-mkconfig`; removal blocks without a recorded usable theme.

## FDE, Secure Boot and TPM

Policy and design: issue #34. Signed shim chain, enrollment, renewal,
status and scoped removal are implemented; VM run passed 2026-09-18/19.

- [ ] VM-only gates: `--tpm2-signature` at enrollment time (omit when
  cryptenroll refuses from the running system), the elected
  `rd.luks.options` unlock path, and the observed clean-boot PCR 12/13
  values.
- [ ] Hardware 3.5: enrollment, unattended unlock, passphrase fallback,
  renewal after a boot change (MOK/akmods/dbx changes PCR 14) and scoped
  removal on the laptop, then the desktop.
- [ ] Desktop: run `nvidia-mok` before FDE enrollment and exercise the
  NVIDIA dracut to UKI reconcile caller.
- Hardware-only gates: firmware OptionalData handling, fall-through to the
  Fedora entry, fwupd/dbx renewal, Windows/NVIDIA entries.
- `systemd-tpm2-setup` fails on swtpm (`Device not a stream`); the unit is
  masked on disposable VMs only. Do not mask it on hardware.

## Postinstall and owner trials

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
