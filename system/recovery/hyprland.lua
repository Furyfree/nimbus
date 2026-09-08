-- Nimbus recovery session: no home includes, no Noctalia.
local term = "/usr/bin/foot --config=/dev/null /usr/bin/bash --noprofile --norc"

hl.monitor({ output = "", mode = "preferred", position = "auto", scale = "auto" })

hl.on("hyprland.start", function()
    hl.exec_cmd(term)
end)

hl.bind("SUPER + RETURN", hl.dsp.exec_cmd(term))
hl.bind("SUPER + SHIFT + Q", hl.dsp.window.close())
hl.bind("SUPER + SHIFT + E", hl.dsp.exit())
