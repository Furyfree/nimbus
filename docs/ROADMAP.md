# Roadmap

[SPEC.md](SPEC.md) owns behavior; [TASKS.md](TASKS.md) owns the open
checklist. This file owns implementation order, deferred scope and exit
criteria.

## Next steps

1. Finish 0.6.1: boot payload and boot validation, installation output and
   logging drill, documentation reconciliation (#39), package and publish.
2. Channel phase 4 (#72): reverse drill after the release.
3. Shared operations, then the TUI dashboard (#36) for 0.7.x.
4. Desktop trial after delivery.

## 1. Simplify the documentation

Give each subject one authoritative location and keep it current: README and
the wiki for usage, SPEC for contracts, this file and TASKS for planning.
Remove stale research narratives and completed task logs; keep deferred work
explicit without making it a release requirement.

Done when commands, ownership, recovery scope and implementation gaps agree
across the documents, links work, and the local checks pass. Tracked by #39;
completed last before publication.

## 2. Finish workstation integration

### Boot theme first

Paper Dark activation is implemented in source: the engine package ships the
GRUB/Plymouth payload and the inert `/etc/grub.d/36_paper_dark` drop-in, and
the `boot-theme` component owns the `/etc/nimbus` marker plus the
`grub-config`, `plymouth-theme` and `initramfs-rebuild` triggers. The VM,
desktop and laptop manifests select the component; an engine without the
payload blocks the trigger instead of applying it. The desktop also selects
`nvidia-boot-display`, which keeps the Intel drivers out of the initramfs so
the unlock prompt reaches the monitor on the NVIDIA card.

Done when Fedora-only and Windows-present menus, the five-second timeout,
Fedora default, older-kernel selection, encrypted-boot prompting, kernel
add/remove mirror rebuilds and theme removal pass on a real boot, with BLS
entries and the EFI stub preserved. Use a disposable VM snapshot as an
independent safeguard. Accepted limitations are recorded in
[TASKS.md](TASKS.md#boot-theme-and-menu).

### Disk encryption and login (decided 2026-09-20)

Passphrase-only LUKS with greetd auto-login, matching Omarchy and Ryoku.
TPM auto-unlock, the signed UKI path and the `fde` component were removed:
the disk passphrase is the authentication boundary, the Paper Dark theme
shows its prompt, and `[initial_session]` starts the owner's session without
a greeter. The passwordless default keyring is Chezmoi-owned user
configuration; Nimbus owns `/etc/greetd/nimbus.toml` and leaves PAM alone.
`nvidia-mok` stays the only Secure Boot key flow. Exit criteria live in
[TASKS.md](TASKS.md#disk-encryption-and-login).

### Remaining integration

- NVIDIA MOK helper: test the installed helper on a machine needing
  signing/enrollment. The manual desktop repair passed with Secure Boot
  enabled. Preserve native prompts and keys; reboot confirmation stays with
  the owner. The NVIDIA Settings autostart condition lives in Chezmoi.
- Greeter passwordless sync: implemented in the ordinary approved init/sync
  plan through the native helper. Done when wallpaper changes sync without
  prompts and the next login shows the new wallpaper on desktop and laptop.
- Noctalia plugin repair: validate from a fresh desktop session through the
  supported command. Native Noctalia owns downloads; Chezmoi owns selection.
- Hyprland plugin task: Chezmoi selects ScrollOverview; Nimbus supplies
  matching development packages and coordinates native HyprPM setup. Validate
  a fresh laptop installation before closing its hardware gate.
- COPR application selection: verify published helper interfaces and package
  sources, retain the Hyprland and Noctalia version families, and use native
  dependency solving rather than coordinating library versions manually.
- Recovery: test TTY repair with broken user configuration and retirement of
  owned legacy session files on installed Fedora. Enable waiting Chezmoi
  launchers only when the installed Nimbus supports them.
- Proton-CachyOS: validate the ProtonPlus download and retry on disposable
  Fedora; unit tests cover selection, prerequisites and the absence of
  user-data or network reads.
- Snapper: native setup, bounded retention and sync/system-upgrade hooks are
  implemented. Test fresh-layout setup, snapshot pairs, cleanup and a root
  restore on installed Fedora, with boot/EFI coverage explicit. Never claim
  automatic rollback.

Done when the affected installation, upgrade, removal and retry paths pass
in a disposable Fedora VM. GRUB requires an actual boot and restoration of
its previous configuration; other physical-device behavior remains the later
hardware trial.

## 3. Release 0.6.1 and engine channels

Package the engine-owned payload in the RPM, bump the recipe version and
archive checksum together after the release archive exists, select
`boot-theme` in `common`, and publish through COPR. The `nimbus.toml`
`min_engine` bump waits until `develop-version` moves to 0.6.2. Engine
channels are tracked by #72: selector schema 2, read-only `nimbus channel`,
bootstrap switching and the `furyfree/nimbus-develop` COPR track.

Versioning rule: `develop-version` names the next stable release and a stable
tag must number at or above it, so develop builds always sort below the
release that follows them. The release source step refuses a lower tag.

Done when the released package passes the #40 bare-metal checklist, the
develop-to-stable reverse drill passes, and the selector migration from
schema 1 is verified on a machine that predates channels.

## 4. Shared operations, then the dashboard

After boot work, extract reconciliation from Cobra, then setup, selection and
file capture. Preserve the lock held across a selection write and sync; keep
presentation in CLI and reuse the same operations for the dashboard. Replace
description-based execution decisions with explicit data covered by the
approved plan.

Bare `nimbus` opens the TUI in a terminal and otherwise prints help. Provide
status, sync, upgrade, software selection, post-install and diagnostics. Use
the existing Bubble Tea stack; do not build another state model.

Done when these workflows work through either interface with the same
effects, and keyboard navigation, small/resized terminals, cancellation,
native command handoff and CLI/TUI preview parity are tested. Tracked by #36
for 0.7.x.

## 5. Prepare the desktop trial

Run the local gate and a clean disposable-VM installation from the release
candidate. Check repeat sync, upgrades, failed-operation retry, owned
removal, TTY repair and normal login. Test the new integration, not every
application's feature set.

Commit, publication and COPR builds require their own authorization. Record
the exact release and packaging results before calling the candidate ready.
Run the full desktop trial after the dashboard and delivery are ready.

## Deferred beyond the desktop milestone

| Work | Reason to keep it separate |
| --- | --- |
| Boot archives and automatic whole-system restore | Beyond native Snapper |
| Hibernation and disk-backed swap | Needs hardware and storage validation |
| Windows VM setup and lifecycle | Planned as #33 for 0.7.x |
| Home Assistant integration | Owner wants to understand it first |
| Additional desktop profiles | Add only for a maintained, demonstrated need |
| Performance changes | Measure an actual problem before adding machinery |
| Browser app launching through UWSM | Check session lifecycle separately |

Revisit [UWSM app launching](https://github.com/Vladimir-csp/uwsm) for browser
and webapp commands: terminal independence, session environment, logout
cleanup and behavior outside UWSM. This is separate from choosing the UWSM
login session.

Windows VM work is planned in #33; retain the owner's preferences: official
Microsoft media, capacity shown before setup, editable defaults of Windows 11
Pro with 4 vCPUs, 8 GiB RAM and 128 GiB disk, and separate data deletion.
Revalidate the backend then; the previous container/media research is not a
current implementation requirement.

## Hardware trial

The installed laptop and desktop are needed to check GPU/power/suspend,
audio/Bluetooth, fingerprint and NVIDIA MOK enrollment, portal file picking
and screen sharing, UWSM session cleanup, and appearance/keyring behavior.

Test the network Epson ET-5800 with native printing and scanning before adding
vendor drivers. Choose Voxtype models and CPU/GPU backend after trials on
both machines and the owner's Omarchy comparison. Recheck Fastmail's known
email-link limitation on Fedora. These are explicit remaining checks, not
reasons to block independent local work.

## Not planned

Nimbus is not becoming a multi-distribution framework, fleet manager, general
AppImage manager, backup service or custom Fedora installer image. Package
discovery results are input for owner review, never automatically desired
state. Fedora major-release upgrades stay with native Fedora tools.
