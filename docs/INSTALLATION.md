# Fedora installation

Install **Fedora 44 Everything/netinstall, x86_64**, from official Fedora
media. Boot in **UEFI** mode and keep Secure Boot enabled on hardware.

Follow the installer sections left to right, then move down.

## 1. Keyboard

**Danish**, then **English (US)**. Test the keys used in your passwords.

## 2. Installation Source

Network source: **Closest mirror**. Leave additional repositories empty.
If the source cannot load, enable networking in step 6 first.

## 3. Installation Destination

Choose **Advanced Custom (Blivet-GUI)** and use **GPT**.

### Boot partitions

Device type: **Partition**. Leave labels empty.

| Filesystem | Mountpoint | Size | Encrypt |
| --- | --- | --- | --- |
| EFI System Partition (FAT32) | `/boot/efi` | 1 GiB | No |
| ext4 | `/boot` | 2 GiB | No |

### Encrypted system volume

Device type: **Btrfs Volume**. Leave **Mountpoint** empty.

| Filesystem | Name / label | Size | Encrypt | Sector size |
| --- | --- | --- | --- | --- |
| btrfs | `fedora` | Remaining | Yes, LUKS2 | Automatic |

### Subvolumes

Device type: **Btrfs Subvolume**, filesystem **btrfs**. Create all entries
directly inside `fedora`, as siblings, using these names without `@` prefixes.

| Name | Mountpoint | Size | Encrypt |
| --- | --- | --- | --- |
| `root` | `/` | Shared | Inherited |
| `home` | `/home` | Shared | Inherited |
| `snapshots` | `/.snapshots` | Shared | Inherited |
| `log` | `/var/log` | Shared | Inherited |
| `cache` | `/var/cache` | Shared | Inherited |
| `swapfile` | `/var/swap` | Shared | Inherited |
| `flatpak` | `/var/lib/flatpak` | Shared | Inherited |
| `windows` | `/var/lib/nimbus/windows` | Shared | Inherited |
| `docker` | `/var/lib/docker` | Shared | Inherited |
| `containerd` | `/var/lib/containerd` | Shared | Inherited |

## 4. Language Support

**English (Denmark)**.

## 5. Software Selection

Base environment: **Fedora Custom Operating System**. Select only:

- **Standard**
- **Common NetworkManager Submodules**
- **Guest Agents** for a VM only

## 6. Network & Host Name

Enable Ethernet or Wi-Fi and set the hostname.

## 7. Time & Date

**Europe/Copenhagen**, with network time enabled.

## 8. Root Account

Enable the root account and set a password. Leave root SSH access disabled.

## 9. User Creation

Create your normal user, require a password and select **Make this user
administrator**.

## 10. Begin Installation

Review the partition changes, then choose **Begin Installation**. When done,
reboot and remove the installation media.

Continue with [Nimbus setup in README](../README.md#install).
