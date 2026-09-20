# Paper Dark boot themes

Original visual reference: [Paper Dark](../../docs/images/paper-dark-reference.png).
Native renders: [GRUB](../../docs/images/paper-dark-grub.png) and
[Plymouth unlock](../../docs/images/paper-dark-plymouth.png).

The theme payload lives in `system/root/boot/grub2/themes/nimbus/` and
`system/root/usr/share/plymouth/themes/nimbus/`. Both use `#101010` backgrounds,
`#eeeeee` foregrounds, gray rules and monospace text. The engine package
installs both trees; the `boot-theme` component owns
`/etc/nimbus/boot-theme.enabled` and activates them through the
`grub-config`, `plymouth-theme` and `initramfs-rebuild` triggers.

Both screens are deliberately bare: the `N I M B U S` wordmark and one row
below it. GRUB shows the menu and a countdown bar. Plymouth shows a small
lock and the passphrase field; after unlock the same box becomes the loading
bar, so unlocking never changes screens. Shutdown shows the wordmark alone.

Plymouth uses its native script plugin for password prompts, ordinary
questions and messages. Password callbacks receive only a bullet count. At
most 18 dots are shown; this does not limit the actual passphrase. The
ordinary "Please enter passphrase" wording is hidden because the lock says
the same; any other native wording (alternate volumes, PINs, recovery keys)
and Plymouth messages stay visible below the field, small and dim. Plymouth
owns input handling and submission. The bar follows Plymouth's own boot
progress estimate with an eased minimum that stops at 70%, because the boot
after unlock is only a few seconds long; it never runs backwards.

Plymouth needs `plymouth-plugin-script`, `plymouth-plugin-label` and
`dejavu-sans-mono-fonts`, including the required renderer/font assets in the
boot image; the `boot-theme` component selects these packages. The
`plymouth-theme` trigger runs `plymouth-set-default-theme nimbus` and records
the previous selection so removal restores it; `initramfs-rebuild` then
rebuilds every installed kernel's image. Each physical machine verifies the
prompt with a typed passphrase; [TASKS.md](../../docs/TASKS.md) tracks that.
Appearance does not change LUKS, MOK or Secure Boot policy.
[GRUB notes](../grub/README.md) cover its separate needs.

## Generate assets

From the repository root:

~~~sh
python3 -I -B tools/boot-theme/assets.py
python3 -I -B tools/boot-theme/assets.py --check
~~~

This generates the small original lock, bullet and pixel PNGs. The reference
image is documentation only; it is not installed as a boot background.

## Preview with native renderers

Requires Docker. Build downloads Fedora packages. Rendering runs offline,
without host mounts, devices or privileges. All fake boot entries, firmware
variables and Plymouth configuration stay in the disposable container. Never
run `render.py` on the workstation or against a real boot disk.

~~~sh
docker build -f tools/boot-theme/Containerfile \
  -t nimbus-boot-theme-preview tools/boot-theme
docker create --name nimbus-paper-dark-preview --network none \
  nimbus-boot-theme-preview python3 /work/preview/render.py
docker cp system nimbus-paper-dark-preview:/work/system
docker cp tools/boot-theme nimbus-paper-dark-preview:/work/preview
docker start -a nimbus-paper-dark-preview
docker cp nimbus-paper-dark-preview:/work/output ./boot-theme-preview
docker rm nimbus-paper-dark-preview
~~~

Choose an unused output directory and container name. Preview output contains
screenshots, temporary VM files and diagnostic logs; keep it outside Git.
The runner generates PF2 fonts from Fedora's DejaVu Sans Mono package before
rendering. To refresh the committed fonts, copy `mono-16.pf2`, `mono-20.pf2`,
`mono-24.pf2` from
`/work/system/root/boot/grub2/themes/nimbus/` and `FONT-LICENSE` from
`/work/system/root/usr/share/licenses/nimbus-boot-theme/` before removing the
container. These are font conversion artifacts, not hand-edited files.

GRUB runs under QEMU/OVMF with a disposable FAT EFI system partition. Plymouth
runs through its X11 renderer in Xvfb; Fedora ships this preview renderer in
`plymouth-devel`. The runner captures boot, password input, long input and a
message. All typed input is synthetic. Inspect the images, not just exit codes.
This verifies rendering, not Fedora kernel boot, real disk unlocking, driver
handoff, firmware enrollment or recovery. Those need disposable Fedora boot
trials followed by desktop and laptop checks before activation.
