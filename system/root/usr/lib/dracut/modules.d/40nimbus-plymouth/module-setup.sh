#!/bin/sh
# shellcheck disable=SC2154
# `$initdir` and the inst_* helpers are provided by dracut at build time.
#
# Nimbus Paper Dark Plymouth support.
#
# Fedora's plymouth-populate-initrd picks the theme to install with an awk
# parser that only reads the first key of [Daemon] in plymouthd.conf, while
# plymouthd itself reads the whole section. Any key before Theme= therefore
# leaves the selected theme out of the initramfs and the daemon falls back to
# text. This module installs the script plugin and the complete theme itself,
# so the payload never depends on that parser. The Paper Dark script renders
# its text through the label plugin, which needs fontconfig's fc-match and
# the monospace faces inside the initramfs. All of it is added only while
# /etc/nimbus/boot-theme.enabled exists, so deselection and removal simply
# rebuild the initramfs without them. Everything it requires is checked
# before installation: a broken dependency skips the module and leaves the
# plain Plymouth prompt instead of failing a kernel transaction.

check() {
    [ -f /etc/nimbus/boot-theme.enabled ] || return 1
    require_binaries plymouthd fc-match || return 1
    [ -f /usr/lib64/plymouth/label-freetype.so ] ||
        [ -f /usr/lib/plymouth/label-freetype.so ] || return 1
    [ -f /usr/lib64/plymouth/script.so ] ||
        [ -f /usr/lib/plymouth/script.so ] || return 1
    [ -f /usr/share/plymouth/themes/nimbus/nimbus.plymouth ] || return 1
    return 0
}

depends() {
    echo plymouth
}

install() {
    inst_libdir_file "plymouth/script.so" "plymouth/label-freetype.so"
    inst_multiple /usr/share/plymouth/themes/nimbus/*
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
    # Plymouth accepts the firmware framebuffer only after DeviceTimeout, an
    # eight second black wait. UseSimpledrm=2 draws the prompt at once, but it
    # is only safe while no native display driver is in the initramfs, which
    # is exactly what the nvidia-boot-display drop-in states. The key goes
    # into the initramfs copy after Theme=, never into the package-owned host
    # file. This module sorts before 45plymouth, whose populate script keeps
    # a plymouthd.conf that already exists in the image.
    if [ -f "${dracutsysrootdir-}/etc/dracut.conf.d/90-nimbus-boot-display.conf" ] &&
        ! grep -q '^UseSimpledrm' "${dracutsysrootdir-}/etc/plymouth/plymouthd.conf" 2>/dev/null &&
        inst_simple /etc/plymouth/plymouthd.conf; then
        sed -i '/^Theme[[:blank:]]*=/a UseSimpledrm=2' "$initdir/etc/plymouth/plymouthd.conf"
    fi
}
