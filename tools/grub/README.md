# Nimbus GRUB theme

`system/grub/theme.txt` and its three PNG slices are the source assets for
`/boot/grub2/themes/nimbus`. They provide a dark menu, a blue selected row,
a scrollbar for additional kernels, a native countdown and keyboard help.
They use GRUB's already loaded default font; no font download or icon set is
required. Percentage placement supports different framebuffer sizes. The
layout is intended for 640x480 and larger; firmware rendering still needs a
disposable VM trial.

The PNG files are solid-color slices generated from the color values in
`assets.py`, using Python's standard library. Regenerate or check them with:

~~~sh
python3 -I -B tools/grub/assets.py
python3 -I -B tools/grub/assets.py --check
~~~

The [native GRUB theme format][theme] permits missing edge and corner slices;
these center-only slices stretch into plain rectangles. Normal text has more
than 10:1 contrast against the background; selected text has more than 9:1
contrast against its blue row. No background photograph needs scaling.

The theme only presents GRUB's existing entries and countdown. Integration
must separately preserve Fedora's BLS entries and EFI stub, select Fedora by
its native entry identity, discover Windows only where its EFI loader exists,
set a visible five-second timeout and reconcile Fedora's `menu_auto_hide`
environment setting. It must retain older kernels and restore previous
configuration on removal. The assets contain no boot commands or installer.

For readable use of the default font, try native `GRUB_GFXMODE` modes
`1024x768,800x600,640x480,auto` in order. Keep Fedora's console fallback when
graphics or font loading fails; the theme cannot implement this fallback.
Leave kernel graphics handoff unchanged. The font and theme must be readable
from the separate `/boot` before unlocking the root filesystem.

Check real rendering, navigation beyond the first page, stopping the timeout,
the command/edit screens and fallback without the theme in a disposable UEFI
VM. Asset reproducibility and static layout checks are not boot verification.

[theme]: https://www.gnu.org/software/grub/manual/grub/html_node/Theme-file-format.html
