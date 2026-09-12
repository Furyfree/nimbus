# Desktop BIOS settings

MSI Click BIOS 5, MPG Z690 EDGE WIFI. Start from defaults and press **F7** for
Advanced Mode. These are the settings to change or check; leave matching values
alone. Menu names can vary with BIOS version.

## 1. Boot mode

Menu: **Settings > Advanced**

| Setting | Value |
| --- | --- |
| BIOS CSM/UEFI Mode | UEFI |
| MSI Driver Utility Installer | Disabled |

## 2. Secure Boot

Menu: **Settings > Security > Secure Boot**

| Setting | Value |
| --- | --- |
| Secure Boot | Enabled |
| Secure Boot Mode | Standard |
| Secure Boot Preset | Maximum Security |

Keep the existing factory keys and leave **Key Management** unchanged. If the
preset is unavailable in Standard mode, inspect it before changing key settings.

## 3. TPM

Menu: **Settings > Security > Trusted Computing**

| Setting | Value |
| --- | --- |
| Security Device Support | Enable |
| TPM Device Selection | fTPM 2.0 |
| SHA256 PCR Bank | Enabled |
| Pending operation | None |

Leave other PCR banks and hierarchy settings at defaults. Do not clear the TPM.
PCR bindings and disk auto-unlock are configured later in Fedora.

## 4. Virtualization

Menu: **OC > CPU Features**

| Setting | Value |
| --- | --- |
| Intel Virtualization Tech | Enabled |
| Intel VT-D Tech | Enabled |

These are virtualization settings, despite being inside the OC menu.

## 5. GPU support

Menu: **Settings > Advanced > PCIe/PCI Sub-system Settings**

| Setting | Value |
| --- | --- |
| Above 4G memory/Crypto Currency mining | Enabled |
| Re-Size BAR Support | Enabled |

Leave PCIe generation settings on Auto and ASPM settings at defaults for now.

## 6. Boot order and fast boot

Menu: **Settings > Boot**

| Setting | Value |
| --- | --- |
| MSI Fast Boot | Disabled |
| Fast Boot | Disabled |
| Boot Option #1 | UEFI Hard Disk |

Keep fast boot disabled while finishing installation and testing.

Menu: **Settings > Boot > UEFI Hard Disk Drive BBS Priorities**

Set **Boot Option #1** to the Fedora entry for the intended installation.
Keep Windows Boot Manager available afterward. If there are multiple Fedora
entries, identify the correct one before choosing.

## 7. Overclocking and memory

| Location | Setting | Value |
| --- | --- | --- |
| Main screen > Game Boost | CPU Game Boost | Off |
| Main screen > XMP control | XMP | Disabled initially |
| OC | CPU ratios and voltages | Defaults |

XMP is also under **OC > Extreme Memory Profile (X.M.P.)**. Consider enabling
the RAM's rated profile after the base installation works reliably.

## 8. Cooling and power limits: inspect first

| Location | What to inspect |
| --- | --- |
| Main screen > Hardware Monitor | CPU/system fan modes, speeds and curves |
| OC > CPU Cooler Tuning | Cooler/power preset, if present |
| OC > Advanced CPU Configuration | Long/short duration CPU power limits |

Power-limit locations depend on BIOS version. Leave these settings unchanged
until the cooler is known. Selecting a water-cooler preset can raise power
limits substantially; it is not automatically the right choice.

## 9. Storage: preserve the working configuration

Menu: **Settings > Advanced > Integrated Peripherals**

| Location within this menu | Setting |
| --- | --- |
| RAID Configuration (Intel VMD) | Enable VMD controller |
| Main menu | External SATA 6GB/s Controller Mode |

Preserve the storage mode the installed systems use. Changing VMD/RAID/AHCI
can prevent an existing Windows installation from booting.

## Save and test

Press **F10**, review the changes and confirm. Test Fedora and Windows before
saving the final profile through **OC PROFILE > OC Profile Save to USB**.
Use a FAT/FAT32 USB drive and record the BIOS version with the export.

Menu reference: [MSI Intel 600 BIOS guide][bios-guide].

[bios-guide]: https://download.msi.com/manual/mb/Intel600BIOS.pdf
