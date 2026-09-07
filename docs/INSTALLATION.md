# Fedora 44 base installation

This is the operator guide for installing the minimal Fedora base expected by
Nimbus. [SPEC.md](SPEC.md) remains the product contract. The earlier test draft
is superseded evidence in commit
`c0bb8a4660732e6e9556297da2c15ba0f286ee98`, not an active guide.

## Target result

Install Fedora 44 on x86_64 as a small bootable base with:

- UEFI, GPT, and GRUB 2
- separate unencrypted `/boot/efi` and `/boot`
- LUKS2 containing the `fedora` Btrfs filesystem
- the exact Btrfs subvolume layout below
- networking and standard command-line utilities
- one normal password-protected user with `wheel` access
- the root account disabled

Fedora owns installation of the base system. Nimbus installs and manages the
desktop, applications, services, hardware integration, and other declared
system resources after first boot.

## Installer choices

Use the official Fedora 44 Everything/netinstall image and custom partitioning.

Select only:

~~~text
Custom Operating System
Standard
Common NetworkManager Submodules
Guest Agents                 # disposable test VM only
~~~

Do not select a desktop environment, development tools, containers, Hyprland,
Noctalia, or desktop applications. Nimbus installs the selected workstation
composition later.

Enable networking. Disable the root account. Create the normal user with a
password and administrative privileges through `wheel`.

Do not disable Secure Boot merely for Nimbus. Keep the machine's intended
Secure Boot policy; Nimbus never disables it. A test VM without Secure Boot may
remain without it. NVIDIA MOK enrollment, when required, is reviewed manual
post-install work.

## Storage layout

Confirm the exact target disk before changing partitions. Use:

~~~text
UEFI/GPT disk
|- 1 GiB       FAT32   /boot/efi
|- 2 GiB       ext4    /boot
`- remaining  LUKS2
   `- Btrfs filesystem labeled "fedora"
~~~

`/boot/efi` and `/boot` remain unencrypted. The remaining partition is the
LUKS2 container; Btrfs is created inside it. Verify that encryption is LUKS2
before starting installation.

Create these Btrfs subvolumes without `@` prefixes:

- `root` -> `/`: included in every supported recovery point.
- `home` -> `/home`: excluded; user files are synchronized or stored
  externally.
- `snapshots` -> `/.snapshots`: stores recovery points and is excluded.
- `log` -> `/var/log`: excluded so failure logs survive restoration.
- `cache` -> `/var/cache`: excluded because it is regeneratable.
- `swapfile` -> `/var/swap`: excluded and reserved for later hibernation work.
- `flatpak` -> `/var/lib/flatpak`: snapshotted only when system Flatpaks
  change.
- `windows` -> `/var/lib/nimbus/windows`: excluded; the Windows guest may be
  lost and recreated.
- `docker` -> `/var/lib/docker`: excluded; container data may be lost and
  recreated.
- `containerd` -> `/var/lib/containerd`: excluded; container data may be lost
  and recreated.

Do not create a separate `/var` subvolume. Ordinary `/var` state, including
Nimbus state in `/var/lib/nimbus`, must remain in `root`; only its `windows`
subdirectory is a mountpoint. Do not create `/var/lib/containers`; this system
uses Docker rather than rootful Podman.

Keep Fedora zram enabled. Create the `swapfile` subvolume and mountpoint during
installation, but do not configure disk-backed swap or hibernation as part of
the base install. That remains a later separately tested capability.

## Before confirming installation

Check:

- the correct physical disk or test-VM disk is selected
- firmware boot mode is UEFI and the partition table is GPT
- `/boot/efi` is 1 GiB FAT32 and `/boot` is 2 GiB ext4
- the remaining system volume is LUKS2 with Btrfs label `fedora`
- every subvolume name and mountpoint matches the table
- there is no separate `/var` subvolume
- the normal user belongs to `wheel` and direct root login is disabled
- networking is enabled
- Guest Agents is selected only for a disposable test VM

## First boot

Sign in as the normal user and inspect the installed layout before bootstrap:

~~~sh
lsblk -f
findmnt -t btrfs,ext4,vfat
sudo btrfs subvolume list /
~~~

Confirm that networking and `sudo` work. Do not manually install the desktop or
duplicate resources that Nimbus will own.

Continue with:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash
~~~

The engine RPM channel and its reviewed signing-key pin are not available
yet. The one-liner is not a complete fresh-install path until that release
gate is met. It obtains Git when necessary and clones or validates the
checkout; bootstrap then verifies and installs the signed COPR engine. This
route remains blocked until the real project key and fingerprint are shipped.
With that prerequisite met, Nimbus installs the system through one reviewed
`nimbus sync`, initializes Chezmoi, and runs `chezmoi apply`, including its
user-tool installation. Failures appear in the closing stage report.

Btrfs recovery points stay on the same disk and are not backups. Nimbus
provides no backup; home, guest, and container data are excluded and may be
lost and recreated from external sources.
