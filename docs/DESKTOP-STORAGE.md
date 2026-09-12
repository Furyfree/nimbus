# Desktop storage plan

Linux is the primary system. Everything that works on Linux should run there;
native Windows covers the remaining applications and games. Keep a separate
SSD for testing Linux installations on real hardware.

Exact space requirements are not yet known. This allocation is the starting
plan, not a record of the current partition layout. The owner reports that
everything has been backed up to the NAS.

The imported [Windows/WSL setup report](Windows_Setup_Spec.md) records the
application and development plan for the Windows installation.

## Drive allocation

| Drive | Role | Filesystem and layout |
| --- | --- | --- |
| Samsung 990 PRO 2 TB NVMe | Main Linux | Encrypted Btrfs + boot |
| Samsung 990 PRO 1 TB NVMe | Windows and apps | NTFS `C:` + boot/recovery |
| WD WDS100T1X0E-00AFY0 1 TB NVMe | Windows games/data | NTFS `D:` |
| WD WDS500G1X0E-00AFY0 500 GB NVMe | Linux testing | Per installation |
| Seagate ST2000DM008-2UB102 2 TB HDD | Bulk storage | NTFS |

## Everyday use

- Give Linux the largest SSD because it is the main working environment.
  Keep projects, Linux games, containers and VMs there.
  Follow the [Fedora installation guide](INSTALLATION.md) for its disk layout.
- Keep Windows and ordinary applications on `C:`. Put large game libraries
  and selected data on `D:` using the application's supported settings.
  Let Windows create its boot and recovery partitions on its own SSD.
- Keep the two Windows SSDs independent. Do not combine them into a striped
  or spanned volume.
- Keep `Users`, `AppData`, `ProgramData` and `Program Files` in their normal
  Windows locations. Do not try to redirect every installation to `D:`.
  Applications on `D:` may still need reinstalling after a Windows reinstall.
- Use the HDD for bulk storage. Keep active VMs, containers, builds and
  demanding games on SSDs. This Seagate model uses SMR recording.
  Choose an available Windows drive letter during setup.
- Keep important files off the testing SSD. Use VMs for ordinary experiments
  and the physical SSD for graphics, suspend, boot and other hardware tests.

## Boot, encryption and backups

Keep each operating system's EFI boot files on its own disk, including test
installations. Fedora is the default; native Windows remains selectable.
A test installation should not replace the main Fedora boot files.

Linux encryption, signed UKIs and TPM auto-unlock follow the existing
[FDE plan](SPEC.md#fde-auto-unlock-planned). This storage plan does not change
the enrollment policy or authorize repartitioning or installation.

The NAS is the backup destination. Set up ongoing backups for important files
from both operating systems; the existing backup covers the current move.
The internal HDD is bulk storage, not the sole backup destination.

## References

- [Microsoft: relocating user profiles and ProgramData][windows-folders]
- [Seagate: BarraCuda specifications, including ST2000DM008][seagate]
- [Fedora: UEFI and EFI system partitions][uefi]

[windows-folders]: https://learn.microsoft.com/en-us/troubleshoot/windows-server/user-profiles-and-logon/relocation-of-users-and-programdata-directories
[seagate]: https://www.seagate.com/www-content/datasheets/pdfs/3-5-barracudaDS1900-14-2007US-en_US.pdf
[uefi]: https://fedoraproject.org/wiki/Unified_Extensible_Firmware_Interface
