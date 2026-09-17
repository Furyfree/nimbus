#!/bin/sh
# shellcheck disable=SC2154
# `$initdir` and the inst_* helpers are provided by dracut at build time.
#
# Nimbus Paper Dark Plymouth support.
#
# Fedora's 45plymouth dracut module installs only the plugin named by the
# theme's ModuleName (script.so). The Paper Dark script renders its text
# through Image.Text with the theme's Font= setting, which needs the label
# plugin and fontconfig's fc-match inside the initramfs; without them the
# unlock screen falls back to text or loses its labels. This module adds the
# plugin, fc-match, fontconfig's configuration and the monospace font only
# while /etc/nimbus/boot-theme.enabled exists, so deselection and removal
# simply rebuild the initramfs without them.

check() {
    [ -f /etc/nimbus/boot-theme.enabled ] || return 1
    require_binaries plymouthd || return 1
    return 0
}

depends() {
    echo plymouth
}

install() {
    inst_libdir_file "plymouth/label-freetype.so"
    inst_multiple fc-match
    inst_multiple /etc/fonts/fonts.conf
    inst_multiple -o \
        "/usr/share/fonts/dejavu-sans-mono-fonts/DejaVuSansMono.ttf" \
        "/usr/share/fonts/dejavu-sans-mono-fonts/DejaVuSansMono-Bold.ttf"
    # /usr is read-only in Fedora's initrd, so the fontconfig cache directory
    # from fonts.conf cannot be used there. Fontconfig falls back to the xdg
    # cache, which is /.cache/fontconfig while HOME is unset; create it on the
    # writable initrd root so label rendering never logs cache errors.
    mkdir -p "$initdir/.cache/fontconfig"
}
