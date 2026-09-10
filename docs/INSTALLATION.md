# Fedora base installation

Install Fedora first, then let Nimbus set up the workstation. Product behavior
and commands are in [SPEC.md](SPEC.md#installation-workflow).

## Fedora installer

Use official Fedora 44 Everything/netinstall media for x86_64. Select a
minimal installation with networking and standard command-line utilities:

~~~text
Custom Operating System
Standard
Common NetworkManager Submodules
Guest Agents                 # test VM only
~~~

Create a normal password-protected user with administrative (`wheel`) access.
Disable direct root login and enable networking. Nimbus installs the selected
desktop, applications and services after first boot.

Keep Secure Boot enabled on hardware. The disposable test VM may run without
it; NVIDIA MOK enrollment is a separate post-install step where needed.

## Disk layout

Check the target disk before confirming any partition changes. Use UEFI/GPT,
Fedora GRUB and this layout:

~~~text
/boot/efi    EFI system partition, unencrypted
/boot        separate boot filesystem, unencrypted
LUKS2        encrypted Btrfs filesystem
  root       mounted at /
  home       mounted at /home
~~~

Keep the encryption passphrase. Nimbus does not require fixed partition sizes,
a specific filesystem label or additional subvolumes. Keep Fedora zram;
disk-backed swap, hibernation and TPM unlock are deferred.

For native Windows dual boot, preserve Windows partitions and its EFI loader.
Do not format a shared EFI partition. Keep independent copies of irreplaceable
data before partitioning.

For a new installation, do not pre-create `/.snapshots`. Nimbus installs
Snapper for `hyprland-noctalia` and lets it create the snapshot storage after
approval. The first setup run has no before snapshot.

## Existing installations from the old guide

The [previous guide](https://github.com/Furyfree/nimbus/blob/f7cd2eaba36e0de64595f74fe778669dbe9528d2/docs/INSTALLATION.md)
required separate subvolumes for snapshots, logs, caches, containers and other
data. Those requirements are retired. Existing layouts can stay; do not delete
subvolumes or reinstall Fedora just to match the simpler layout above.

Nimbus supports reusing an empty `/.snapshots` mount when it is
a root-owned Btrfs subvolume on the same filesystem as root, with no conflicting
Snapper configuration. Nimbus shows that reuse before approval, registers the
storage and verifies it with native Snapper. It preserves the mount, contents
and `/etc/fstab`. This requires Nimbus 0.3.1 or newer; the 0.3.0 engine
refuses this layout.

Populated, foreign-owned, unreadable or ambiguous snapshot storage needs an
explicit migration. Do not delete it merely to get past an installation error.

Root snapshots exclude other subvolumes and filesystems, including `/home`,
`/boot` and EFI. On the old layout they also exclude system Flatpaks and
container storage. Nimbus does not take separate Flatpak snapshots or promise
whole-system rollback. Snapshots on the same disk are not backups.

## First boot and Nimbus

Sign in as the normal user. Check networking, sudo access and the mounts:

~~~sh
lsblk -f
findmnt -t btrfs,ext4,vfat
~~~

Run:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash
~~~

Nimbus 0.3.1 or newer asks for `desktop`, `laptop` or `vm` on first use.
With 0.3.0, use `bash -s -- --machine vm` instead of `bash`, replacing
`vm` with the intended machine. Reruns reuse the saved selection. The installer
shows its plan and asks before applying it. An existing Nimbus RPM must be
updated through DNF to obtain engine fixes; updating the checkout is not enough.

For unpublished changes, use the [candidate guide](../tools/vm/README.md).
Check [TASKS.md](TASKS.md) for remaining installation and hardware trials.

Reboot when requested and choose Hyprland through UWSM in Noctalia Greeter.
Use `nimbus status`, `nimbus doctor` and `nimbus postinstall` to inspect the
result. For a broken desktop, try Ctrl+Alt+F3 and repair from a TTY. A system
that cannot boot needs Fedora rescue or installation media. There is no
separate Nimbus recovery desktop.
