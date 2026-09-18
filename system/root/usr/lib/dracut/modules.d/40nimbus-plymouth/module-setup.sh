#!/bin/sh
# shellcheck disable=SC2154
# `$initdir` and the inst_* helpers are provided by dracut at build time.
#
# Nimbus Paper Dark Plymouth support.
#
# Fedora's plymouth-populate-initrd already installs the label plugin and the
# fonts its fc-match resolves; the Paper Dark script still renders its text
# through that plugin, which needs fontconfig's fc-match and the monospace
# faces inside the initramfs. This module adds them only while
# /etc/nimbus/boot-theme.enabled exists, so deselection and removal simply
# rebuild the initramfs without them. Everything it requires is checked
# before installation: a broken dependency skips the module and leaves the
# plain Plymouth prompt instead of failing a kernel transaction.

check() {
    [ -f /etc/nimbus/boot-theme.enabled ] || return 1
    require_binaries plymouthd fc-match || return 1
    [ -f /usr/lib64/plymouth/label-freetype.so ] ||
        [ -f /usr/lib/plymouth/label-freetype.so ] || return 1
    return 0
}

depends() {
    echo plymouth
}

install() {
    inst_libdir_file "plymouth/label-freetype.so"
    inst_multiple fc-match
    inst_multiple -o /etc/fonts/fonts.conf
    inst_multiple -o \
        "/usr/share/fonts/dejavu-sans-mono-fonts/DejaVuSansMono.ttf" \
        "/usr/share/fonts/dejavu-sans-mono-fonts/DejaVuSansMono-Bold.ttf"
    # /usr is read-only in Fedora's initrd, so the fontconfig cache directory
    # from fonts.conf cannot be used there. Fontconfig falls back to the xdg
    # cache, which is /.cache/fontconfig while HOME is unset; create it on the
    # writable initrd root so label rendering never logs cache errors.
    mkdir -p "$initdir/.cache/fontconfig"
}
