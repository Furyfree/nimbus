# Fedora 44 test installation

## Purpose

This is the minimal Fedora 44 base for Nimbus:

- Fedora provides a bootable base, networking, and standard utilities.
- Nimbus manages system packages, services, hardware, and recovery.
- Chezmoi manages user dotfiles.
- Btrfs provides local recovery points. It is not a backup.

## Installer

Use Fedora 44 Everything/netinstall on x86_64 with UEFI, GPT, GRUB 2, and:

```text
Custom Operating System
Standard
Common NetworkManager Submodules
Guest Agents                 # test VM only
```

Do not select a desktop environment, development tools, containers, Hyprland,
or desktop applications. Nimbus installs them after first boot.

Secure Boot is disabled for the initial installation. On a machine where it is
enabled, Nimbus treats NVIDIA MOK enrollment as a manual post-install task and
never disables Secure Boot automatically.

Disable the root account. Create the normal user `pby` with a password and
administrative privileges through `wheel`.

## Disk

The test VM uses:

```text
vda
|- vda1   1 GiB       FAT32   /boot/efi
|- vda2   2 GiB       ext4    /boot
|- vda3   remaining   Btrfs
```

The physical workstation uses the same structure with the remaining disk
space assigned to Btrfs.

The system volume is:

```text
LUKS2
|- Btrfs "fedora"
```

`/boot` and `/boot/efi` remain unencrypted in this layout. Verify LUKS2 before
finishing the installation.

## Btrfs layout

Use these subvolumes without `@` prefixes:

```text
fedora
|- root         /
|- home         /home
|- snapshots    /.snapshots
|- log          /var/log
|- cache        /var/cache
|- swapfile     /var/swap
|- flatpak      /var/lib/flatpak
|- libvirt      /var/lib/libvirt/images
|- docker       /var/lib/docker
|- containerd   /var/lib/containerd
```

| Subvolume | Recovery policy |
| --- | --- |
| `root` | Snapshot for every Nimbus recovery point. Contains Fedora, `/etc`, `/usr`, normal `/var`, and `/var/lib/nimbus`. |
| `home` | Snapshot separately with the same recovery-point ID. Never restore automatically during a system rollback. |
| `snapshots` | Stores recovery points and is not included in them. |
| `log` | Excluded so failure logs survive rollback. |
| `cache` | Excluded because it is regeneratable. |
| `swapfile` | Excluded because active swap must not be snapshotted. |
| `flatpak` | Snapshot when Nimbus changes system-wide Flatpaks. |
| `libvirt` | Excluded. VM disks require VM-aware backup or snapshots. |
| `docker` and `containerd` | Excluded. Container data requires its own backup policy. |

Do not create a separate `/var` subvolume. The remaining `/var` state must stay
with the operating system. Do not create `/var/lib/containers`; this system
uses Docker rather than rootful Podman.

## Recovery points

A Nimbus recovery point groups related snapshots and boot archives under one
ID:

```text
recovery point
|- root snapshot
|- home snapshot
|- Flatpak snapshot       # only when changed
|- /boot archive          # only when changed
|- /boot/efi archive      # only when changed
```

Btrfs does not recursively snapshot nested subvolumes, and separate subvolume
snapshots are not one atomic operation. Nimbus creates the set immediately
before apply and records every included item.

A normal system rollback restores `root` and matching boot data. It does not
restore `home`. Home can be restored explicitly as part of a full rollback, or
mounted to recover selected files. This prevents a system rollback from
discarding newer documents and source code.

Boot archives are required when an operation changes the kernel, initramfs,
bootloader, NVIDIA boot integration, or EFI files. A root snapshot alone cannot
restore `/boot` or `/boot/efi` because they are separate non-Btrfs filesystems.

Authoritative Nimbus state and receipts live under `/var/lib/nimbus` so they
roll back with the system. The desired machine manifest remains in the
Chezmoi repository under `machines/` at the checkout root, outside the Chezmoi
source state.

Snapshots remain on the same disk and do not protect against disk loss.
Automatic recovery points require tested restore procedures and a retention
policy before they are enabled.

## Swap and hibernation

Keep Fedora zram enabled and add a disk-backed Btrfs swapfile under
`/var/swap` for hibernation:

```text
memory pressure -> zram -> disk swap
hibernation     -> /var/swap/swapfile
```

After first boot:

1. Create a Btrfs-compatible swapfile.
2. Add it to `/etc/fstab` and enable it.
3. Determine the Btrfs resume offset.
4. Configure the kernel and initramfs resume parameters.
5. Test hibernation.

Size the disk swap roughly to physical RAM, with some headroom when reliable
hibernation is required.

## Ownership

```text
Fedora installer
|- bootable encrypted base
|- networking
|- standard utilities
|- VM guest integration when applicable

Nimbus
|- packages and repositories
|- services and system configuration
|- Docker, libvirt, and system Flatpaks
|- Hyprland and Noctalia
|- development and gaming resources
|- applied state
|- recovery points

Chezmoi
|- user dotfiles
```

Administration runs through the normal user and operation-scoped `sudo`. Direct
root login remains disabled.
