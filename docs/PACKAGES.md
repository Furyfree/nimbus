# Packages

The workstation software, including dotfiles-declared user tools. Sources appear
in parentheses: `DNF/Fedora` is a bare package name, every other DNF source is a
repository declared in `nimbus.toml`, and `user:` marks installation as the
normal user without sudo. Nimbus runs its declared installers; Chezmoi invokes
Mise for tools declared in dotfiles. An installer script is downloaded to a file
and shown with its digest before it runs. [SECURITY.md](SECURITY.md) owns the
source order. This list feeds the definitions and shrinks as they land.

## Session

- Hyprland (`DNF/COPR: lionheartp/Hyprland`)
- Noctalia (`DNF/Fedora`)
- Noctalia Greeter (`DNF/Terra: noctalia-greeter`)
- Session dependencies
  - `hyprland-guiutils`, `xdg-desktop-portal-hyprland`
    (`DNF/COPR: lionheartp/Hyprland`)
  - `greetd`, `xdg-desktop-portal-gtk`, `xorg-x11-server-Xwayland`
    (`DNF/Fedora`)
  - `qt6-qtwayland`, `qt5-qtwayland` (`DNF/Fedora`)
  - `xdg-utils`, `xdg-user-dirs`, `xdg-terminal-exec`, `gnome-keyring`
    (`DNF/Fedora`)
  - `accountsservice`, `udiskie`, `udisks2` (`DNF/Fedora`)
  - `playerctl`, `pipewire-pulseaudio`, `pipewire-alsa` (`DNF/Fedora`)

## GUI Apps

- Nautilus and Sushi (`DNF/Fedora: nautilus sushi`)
  - Phone and network shares (`DNF/Fedora: gvfs gvfs-mtp gvfs-smb`)
- Zed (`user: installer from https://zed.dev/install.sh`; updates itself)
- Spotify (`Flatpak/Flathub: com.spotify.Client`)
- T3 Code (`DNF/Terra: t3code`)
- ChatGPT Desktop (`DNF/OpenAI repository: chatgpt`; the RPM's own script
  would add the repository, Nimbus declares it directly)
- GitHub Desktop (own COPR, pending)
- Vesktop (`DNF/Terra: vesktop`)
- VSCodium (`DNF/VSCodium repository: codium`)
- 1Password and 1Password CLI
  (`DNF/1Password repository: 1password 1password-cli`)
- Obsidian (`Flatpak/Flathub: md.obsidian.Obsidian`)
- Fastmail (`Flatpak/Flathub: com.fastmail.Fastmail`, stable)
  - [Official Linux download](https://www.fastmail.com/download/) points to
    [Flathub](https://flathub.org/en/apps/com.fastmail.Fastmail).
    Selected in `hyprland-noctalia`; the package owns its launcher and icons.
    Retained with a known `mailto:` composition failure in the CachyOS test.
    Recheck on the new Fedora installation; see the desktop application
    evidence in [TASKS.md](TASKS.md#desktop-application-selection).
- Signal (`DNF/Terra: signal-desktop`)
- Brave Origin (`DNF/Brave repository: brave-origin`)
- Zathura (`DNF/Fedora: zathura`)
- Ghostty (`DNF/Terra: ghostty`)
- OBS Studio (`DNF/Fedora: obs-studio`)
- Pinta (`DNF/Fedora: pinta`)
- GIMP, added per machine (`DNF/Fedora: gimp`)
- Loupe (`DNF/Fedora: loupe`)
- Celluloid and mpv (`DNF/Fedora: celluloid mpv`)
- LibreOffice (`DNF/Fedora: libreoffice`)
- Flatpak (`DNF/Fedora: flatpak`) with the Flathub system remote declared in
  `nimbus.toml`
- GNOME Disks (`DNF/Fedora: gnome-disk-utility`)
- Flatseal (`DNF/Fedora: flatseal`)

## Gaming

- Proton-GE, managed through ProtonPlus (`DNF/Terra: protonplus`)
- Steam (`DNF/RPM Fusion Nonfree: steam`)
- Lutris (`DNF/Fedora: lutris`)
- Heroic Games Launcher (`DNF/Terra: heroic-games-launcher`)
- Wine (`DNF/Fedora: wine`)
- Prism Launcher (`DNF/Terra: prismlauncher`; `DNF/Fedora: java-25-openjdk`)
- WoWUp (own COPR, pending)
- umu-launcher (`DNF/Terra: umu-launcher`)
- Winetricks and Protontricks (`DNF/Fedora: winetricks protontricks`)
- Gamescope (`DNF/Fedora: gamescope`)
- MangoHud (`DNF/Fedora: mangohud`)
- GOverlay (`DNF/Fedora: goverlay`)
- GameMode (`DNF/Fedora: gamemode`)

## Extra

- Printer support
  - CUPS, Avahi, and Ghostscript (`DNF/Fedora: cups avahi ghostscript`)
  - Epson ESC/P-R (`epson-inkjet-printer-escpr`; packaging pending)
  - Epson ESC/P-R 2 (`epson-inkjet-printer-escpr2`; packaging pending)
  - Scanner over eSCL (`DNF/Fedora: sane-airscan simple-scan`)
- Hardware support (components that `nimbus init` records in the manifest)
  - Intel and AMD graphics and firmware (`DNF/Fedora: mesa-dri-drivers
    mesa-vulkan-drivers intel-gpu-firmware amd-gpu-firmware`)
  - NVIDIA driver (`DNF/RPM Fusion Nonfree: akmod-nvidia`)
  - NVIDIA CUDA and 64-bit libraries
    (`DNF/RPM Fusion Nonfree: xorg-x11-drv-nvidia-cuda`)
  - NVIDIA 32-bit libraries
    (`DNF/RPM Fusion Nonfree: xorg-x11-drv-nvidia-libs.i686`)
  - NVIDIA VA-API (`DNF/Fedora: libva-nvidia-driver`)
  - AMD video decoding (`DNF/RPM Fusion Free: mesa-va-drivers-freeworld`,
    replacing Fedora's `mesa-va-drivers`)
  - Intel video decoding (`DNF/RPM Fusion Nonfree: intel-media-driver`)
  - Secure Boot module support (`DNF/Fedora: akmods mokutil`)
  - Laptop brightness (`DNF/Fedora: brightnessctl`)
  - Desktop monitor brightness (`DNF/Fedora: ddcutil`)
  - Fingerprint reader (`DNF/Fedora: fprintd`)
  - Power and battery
    (`DNF/Fedora: power-profiles-daemon upower`)
  - Audio and Bluetooth (`DNF/Fedora: pipewire wireplumber bluez`)
  - Firmware updates (`DNF/Fedora: fwupd`)
- Media codecs
  (`DNF/RPM Fusion Free: ffmpeg gstreamer1-plugins-bad-freeworld`; `ffmpeg`
  replaces Fedora's `ffmpeg-free`)
- Snapper (`DNF/Fedora: snapper`)
- NetworkManager OpenConnect (`DNF/Fedora: NetworkManager-openconnect`)
- WireGuard (`DNF/Fedora: wireguard-tools`)
- Tailscale (`DNF/Fedora: tailscale`)
- Compression
  - `zip`, `unzip`, `7zip`, `unrar` (`DNF/Fedora`)
- Nimbus system helpers
  (`DNF/Fedora: dnf5-plugins policycoreutils-python-utils`)

## Theming

- Papirus Icon Theme (`DNF/Fedora: papirus-icon-theme`)
- Bibata Cursor Theme (`DNF/Terra: bibata-cursor-theme`)
- Qt and GTK theming (`DNF/Fedora: qt6ct`; configuration through Chezmoi)
- Fonts
  - JetBrains Mono (`DNF/Fedora: jetbrains-mono-fonts-all`)
  - JetBrains Mono Nerd Font (`DNF/Terra: jetbrainsmono-nerd-fonts`)
  - Inter (`DNF/Fedora: rsms-inter-fonts`)
  - Terminus (`DNF/Fedora: terminus-fonts`)
  - Noto Sans (`DNF/Fedora: google-noto-sans-fonts`)
  - Noto Emoji (`DNF/Fedora: google-noto-color-emoji-fonts`)
  - Noto CJK (`DNF/Fedora: google-noto-sans-cjk-fonts`)

## CLI

- Mise (`user: installer from https://mise.run`, before the Chezmoi handoff;
  declared as the `mise` component)
  - Auto-update (`auto_update = true` in the Chezmoi-managed Mise config;
    disabled during apply with `MISE_AUTO_UPDATE=false`)
  - Runtimes (`user: mise install`, invoked by Chezmoi after writing
    `~/.config/mise/config.toml` on each full apply; upgrades remain explicit)
    - Codex, Claude Code, OpenCode, Pi, Grok (`Mise/npm`)
    - Go, .NET, uv, Python, Java Corretto, Node.js (`Mise`)
    - Rust (`Mise`, using rustup underneath; provides Cargo for the tools
      below)
    - markdownlint-cli (`Mise/npm`)
    - gopls, golangci-lint (`Mise/Go`)
    - lazydocker (`Mise/Go: github.com/jesseduffield/lazydocker`)
- Cargo tools (`user: mise install`, using Mise's Cargo backend after Rust;
  declared in Chezmoi's `~/.config/mise/conf.d/cargo.toml`)
  - Caligula (`caligula`)
  - Typst (`typst-cli`)
  - Tinymist (`tinymist`)
  - cargo-update (`cargo-update`)
  - Sheldon (`sheldon`)
  - SVG preview for Yazi (`resvg`)
  - VM Curator (`vm-curator`)
- Just (`DNF/Fedora: just`)
- Topgrade (`DNF/Terra: topgrade`; configuration through Chezmoi)
- Herdr (`user: installer from https://herdr.dev/install.sh`; `herdr update`
  runs from the Chezmoi-owned Topgrade configuration)
- ShellCheck and gitleaks (`DNF/Fedora: ShellCheck gitleaks`)
- Neovim (`DNF/Fedora: neovim`)
- Chezmoi (`DNF/Fedora: chezmoi`)
- Bash completion (`DNF/Fedora: bash-completion`)
- C and C++ toolchain
  (`DNF/Fedora: gcc gcc-c++ clang cmake ninja-build`)
- Docker and Compose (`DNF/Docker repository: docker-ce docker-ce-cli
  containerd.io docker-buildx-plugin docker-compose-plugin`)
- Git (`DNF/Fedora: git`)
- Git LFS (`DNF/Fedora: git-lfs`)
- GitHub CLI (`DNF/Fedora: gh`)
- lazygit (`DNF/Terra: golang-github-jesseduffield-lazygit`)
- Yazi (`DNF/Terra: yazi`)
  - Required: `file` (`DNF/Fedora`)
  - Preview support: `ffmpeg`, `7zip`, `jq`, `poppler-utils`,
    `ImageMagick` (`DNF/Fedora` or RPM Fusion as listed elsewhere)
  - Search and navigation: `fd-find`, `ripgrep`, `fzf`, `zoxide`
    (`DNF/Fedora`)
  - Clipboard: `wl-clipboard` (`DNF/Fedora`)
- Nix (`DNF/Fedora: nix nix-daemon`; enables `nix-daemon.service`)

## Shell

- Zsh (`DNF/Fedora: zsh`)
- Starship (`DNF/Terra: starship`)
- eza (`DNF/Fedora: eza`)
- fzf (`DNF/Fedora: fzf`)
- fd (`DNF/Fedora: fd-find`)
- bat (`DNF/Fedora: bat`)
- fastfetch (`DNF/Fedora: fastfetch`)
- wl-clipboard (`DNF/Fedora: wl-clipboard`)
- zoxide (`DNF/Fedora: zoxide`)
- ripgrep (`DNF/Fedora: ripgrep`)
- tokei (`DNF/Fedora: tokei`)
- btop (`DNF/Fedora: btop`)
- tldr (`DNF/Fedora: tealdeer`)
- yq (`DNF/Fedora: yq`)
- duf (`DNF/Fedora: duf`; `df` alias through Chezmoi)

## Virtualization

- VM Curator (`user: Mise Cargo backend`, listed with the Cargo tools)
- QEMU, TPM, and SPICE viewer
  (`DNF/Fedora: qemu-system-x86 qemu-img swtpm virt-viewer`)
- Windows container (`Docker: dockurr/windows`, pinned by digest)
- FreeRDP (`DNF/Fedora: freerdp`)

## Noctalia plugins

Nimbus installs the listed dependencies. Enabling a plugin is user state,
done with `noctalia msg plugins enable <id>` from the built-in `official` and
`community` sources.

### Official Noctalia Plugins

- Notes (`noctalia/notes`)
- Timer (`noctalia/timer`)
- Translator (`noctalia/translator`)
- Wallhaven (`noctalia/wallhaven`)
- Screen Recorder (`DNF/Terra: gpu-screen-recorder`;
  `noctalia/screen_recorder`)

### Community Plugins

- Keybind Cheatsheet (`kenn/keybind-cheatsheet`)
- File Search (`nightwatch75/file-search`)
- OCR (`DNF/Fedora: grim slurp tesseract tesseract-langpack-eng`; `fel/ocr`)
- QR Code (`DNF/Fedora: qrencode`; `yocraft/qrcode`)
- Zed Provider (`DNF/Fedora: sqlite`; `cleboost/zed-provider`)
- AI Usage (collector source pending; `felipeartur/ai-usagebar`)
- GitHub Pull Requests (`raycursive/github-prs`)
- SSH Launcher (`cleboost/ssh-launcher`)
- Portctl (`rxtsel/portctl`)
- Crashes (`DNF/Fedora: jq libnotify`; `coredumpctl` from systemd;
  `umedbazarov/crashes`)
- Tailnet (`rylos/tailnet`)
- Udiskie Manager (`aristides/udiskie`)
- Lid Guard (`8bury/lid-guard`)
- Hypr-Screen-Mirror (`DNF/Fedora: socat`; `profidev/hypr-screen-mirror`)
- Mini Docker (`8bury/mini-docker`)
- AirPods (`DNF/Terra: librepods`; `harveywuk/airpods`)
- Home Assistant (`pozzoo/hassio`), blocked
  - Do not enable until the long-lived access token is proven to be stored
    through an acceptable secret provider rather than plaintext Noctalia
    state. Keep using the Home Assistant app or web UI until then.

## Webapps

Opened through `nimbus launch webapp`; Chezmoi owns the desktop entries.
The selected initial set is below. Fastmail uses the Flatpak above instead.

- [Google Maps](https://www.google.com/maps)
- [FotMob](https://www.fotmob.com/)
