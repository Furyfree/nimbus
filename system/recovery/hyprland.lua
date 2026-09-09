-- Nimbus recovery session: no home includes, no Noctalia.
local term = "/usr/bin/env -u GTK_THEME -u GTK_PATH -u GTK_MODULES "
    .. "XDG_CONFIG_HOME=/usr/local/lib/nimbus/recovery /usr/bin/ghostty "
    .. "--config-default-files=false --gtk-single-instance=false "
    .. "--shell-integration=none -e /usr/bin/bash --noprofile --norc"

hl.monitor({ output = "", mode = "preferred", position = "auto", scale = "auto" })

hl.on("hyprland.start", function()
    hl.exec_cmd(term)
end)

hl.bind("SUPER + RETURN", hl.dsp.exec_cmd(term), { description = "Open recovery terminal" })
hl.bind("SUPER + SHIFT + Q", hl.dsp.window.close(), { description = "Close recovery window" })
hl.bind("SUPER + SHIFT + E", hl.dsp.exit(), { description = "Exit recovery session" })
