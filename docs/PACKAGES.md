# Packages

Applications and explicit support packages for the workstation. The install
source or command is shown in parentheses.

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
  - `xdg-utils`, `xdg-user-dirs`, `gnome-keyring` (`DNF/Fedora`)
  - `accountsservice`, `udiskie`, `udisks2` (`DNF/Fedora`)
  - `playerctl`, `pipewire-pulse`, `pipewire-alsa` (`DNF/Fedora`)

## GUI Apps

- Nautilus and Sushi (`DNF/Fedora: nautilus sushi`)
  - Phone and network shares (`DNF/Fedora: gvfs gvfs-mtp gvfs-smb`)
- Zed (`curl -f https://zed.dev/install.sh | sh`)
- Spotify (`Flatpak/Flathub: com.spotify.Client`)
- T3 Code (`DNF/Terra: t3code`)
- ChatGPT Desktop (official OpenAI RPM)

  ~~~sh
  curl -fL -o ~/Downloads/chatgpt.x86_64.rpm \
    https://persistent.oaistatic.com/codex-app-prod/linux/rpm/latest/chatgpt.x86_64.rpm
  sudo dnf install -y ~/Downloads/chatgpt.x86_64.rpm
  ~~~

- GitHub Desktop (own COPR, pending)
  - Terra alternative: `desktop-plus-bin`, which is the Desktop Plus fork
  - `github-copilot-installer` installs GitHub Copilot, not GitHub Desktop
- Vesktop (`DNF/Terra: vesktop`)
- VSCodium (`DNF/Terra: codium`)
- 1Password and 1Password CLI (`DNF/1Password RPM repository`)
- Obsidian (`Flatpak/Flathub: md.obsidian.Obsidian`)
- Signal (`DNF/Terra: signal-desktop`)
- Brave Origin (`DNF/Brave RPM repository: brave-browser`)
- Zathura (`DNF/Fedora: zathura`)
- Ghostty (`DNF/Terra: ghostty`)
- OBS Studio (`DNF/Fedora: obs-studio`)
- Pinta (`DNF/Fedora: pinta`)
- GIMP, optional component (`DNF/Fedora: gimp`)
- Loupe (`DNF/Fedora: loupe`)
- Celluloid and mpv (`DNF/Fedora: celluloid mpv`)
- LibreOffice (`DNF/Fedora: libreoffice`)
- Flatpak (`DNF/Fedora: flatpak`)
- Flathub system remote

  ~~~sh
  sudo flatpak remote-add --if-not-exists --system flathub \
    https://dl.flathub.org/repo/flathub.flatpakrepo
  ~~~

- GNOME Disks (`DNF/Fedora: gnome-disk-utility`)
- Flatseal (`DNF/Fedora: flatseal`)

## Gaming

- Proton-GE, managed through ProtonPlus (`DNF/Terra: protonplus`)
- Steam (`DNF/RPM Fusion Nonfree: steam`)
- Lutris (`DNF/Fedora: lutris`)
- Heroic Games Launcher (`DNF/Terra: heroic-games-launcher`)
- Wine (`DNF/Fedora: wine`)
- Prism Launcher (`DNF/Terra: prismlauncher`)
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
- Hardware support (automatic component selection)
  - Intel and AMD graphics and firmware (`DNF/Fedora`)
  - NVIDIA driver (`DNF/RPM Fusion Nonfree: akmod-nvidia`)
  - NVIDIA CUDA and 64-bit libraries
    (`DNF/RPM Fusion Nonfree: xorg-x11-drv-nvidia-cuda`)
  - NVIDIA 32-bit libraries
    (`DNF/RPM Fusion Nonfree: xorg-x11-drv-nvidia-libs.i686`)
  - NVIDIA VA-API (`DNF/Fedora: libva-nvidia-driver`)
  - AMD video decoding (`DNF/RPM Fusion Free: mesa-va-drivers-freeworld`)
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
  (`DNF/RPM Fusion Free: ffmpeg gstreamer1-plugins-bad-freeworld`)
- Snapper (`DNF/Fedora: snapper`)
- NetworkManager OpenConnect (`DNF/Fedora: NetworkManager-openconnect`)
- WireGuard (`DNF/Fedora: wireguard-tools`)
- Tailscale (`DNF/Fedora: tailscale`)
- Compression
  - `zip`, `unzip`, `7zip` (`DNF/Fedora`)
  - `unrar` (`DNF/RPM Fusion Nonfree`; Fedora's `unrar` is `unrar-free`)
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

- Mise (`curl https://mise.run | sh`)
  - Auto-update (`mise settings set auto_update true`)
  - Agentic and AI tools
    - Codex (`Mise/npm`)
    - Claude Code (`Mise/npm`)
    - OpenCode (`Mise/npm`)
    - Pi (`Mise/npm`)
    - Grok (`Mise/npm`)
  - Programming languages and tools
    - Go (`Mise`)
    - .NET (`Mise`)
    - uv (`Mise`)
    - Python (`Mise`)
    - Java Corretto (`Mise`)
    - Node.js (`Mise`)
    - Rust (`Mise`, using rustup underneath)
    - markdownlint-cli (`Mise/npm`)
    - gopls (`Mise/Go`)
    - golangci-lint (`Mise/Go`)
- Cargo tools
  - Caligula (`cargo install caligula`)
  - Typst (`cargo install typst-cli`)
  - Tinymist (`cargo install tinymist`)
  - cargo-update (`cargo install cargo-update`)
- Just (`DNF/Fedora: just`)
- Topgrade (`cargo install topgrade`)
- Herdr (`curl -fsSL https://herdr.dev/install.sh | sh`; `herdr update` runs
  from the Topgrade configuration in Chezmoi)
- ShellCheck and gitleaks (`DNF/Fedora: ShellCheck gitleaks`)
- Neovim (`DNF/Fedora: neovim`)
- Chezmoi (`DNF/Fedora: chezmoi`)
- Bash completion (`DNF/Fedora: bash-completion`)
- C and C++ toolchain
  (`DNF/Fedora: gcc gcc-c++ clang cmake ninja-build`)
- Docker and Compose (`DNF/Docker RPM repository: docker-ce docker-ce-cli
  containerd.io docker-buildx-plugin docker-compose-plugin`)
- Git (`DNF/Fedora: git`)
- Git LFS (`DNF/Fedora: git-lfs`)
- GitHub CLI (`DNF/Fedora: gh`)
- lazydocker
  (`Mise/Go: go install github.com/jesseduffield/lazydocker@latest`)
- lazygit (`DNF/Fedora: lazygit`)
- Yazi (`DNF/Terra: yazi`)
  - Required: `file` (`DNF/Fedora`)
  - Preview support: `ffmpeg`, `7zip`, `jq`, `poppler-utils`,
    `ImageMagick` (`DNF/Fedora` or RPM Fusion as listed elsewhere)
  - Search and navigation: `fd-find`, `ripgrep`, `fzf`, `zoxide`
    (`DNF/Fedora`)
  - Clipboard: `wl-clipboard` (`DNF/Fedora`)
  - SVG preview (`cargo install resvg`)
- Nix (`DNF/Fedora: nix nix-daemon`; enables `nix-daemon.service`)

## Shell

- Zsh (`DNF/Fedora: zsh`)
- Starship (`DNF/Terra: starship`)
- Sheldon (`cargo install sheldon`)
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

- VM Curator (`cargo install vm-curator`)
- QEMU and TPM support
  (`DNF/Fedora: qemu-system-x86 qemu-img swtpm virt-viewer`)
- Windows container (`Docker: dockurr/windows`, pinned by digest)
- FreeRDP (`DNF/Fedora: freerdp`)

## Noctalia plugins

Enable with `noctalia msg plugins enable <id>` from the built-in `official`
and `community` sources. Phases follow `~/git/docs/plugins.md`.

### Official Noctalia Plugins

- Notes (`noctalia msg plugins enable noctalia/notes`), phase 1
- Timer (`noctalia msg plugins enable noctalia/timer`), phase 1
- Translator (`noctalia msg plugins enable noctalia/translator`), phase 1
- Wallhaven (`noctalia msg plugins enable noctalia/wallhaven`), phase 1
- Screen Recorder (`DNF/Terra: gpu-screen-recorder`;
  `noctalia msg plugins enable noctalia/screen_recorder`), phase 3

### Community Plugins

- Keybind Cheatsheet
  (`noctalia msg plugins enable kenn/keybind-cheatsheet`), phase 1
- File Search (`noctalia msg plugins enable nightwatch75/file-search`),
  phase 1
- OCR (`DNF/Fedora: grim slurp tesseract tesseract-langpack-eng`;
  `noctalia msg plugins enable fel/ocr`), phase 1
- QR Code (`DNF/Fedora: qrencode`;
  `noctalia msg plugins enable yocraft/qrcode`), phase 1
- Zed Provider (`DNF/Fedora: sqlite`;
  `noctalia msg plugins enable cleboost/zed-provider`), phase 1
- AI Usage (collector source pending;
  `noctalia msg plugins enable felipeartur/ai-usagebar`), phase 2
- GitHub Pull Requests
  (`noctalia msg plugins enable raycursive/github-prs`), phase 2
- SSH Launcher (`noctalia msg plugins enable cleboost/ssh-launcher`), phase 2
- Portctl (`noctalia msg plugins enable rxtsel/portctl`), phase 2
- Crashes (`DNF/Fedora: jq libnotify`;
  `noctalia msg plugins enable umedbazarov/crashes`), phase 2
- Tailnet (`noctalia msg plugins enable rylos/tailnet`), phase 3
- Udiskie Manager (`noctalia msg plugins enable aristides/udiskie`), phase 3
- Lid Guard (`noctalia msg plugins enable 8bury/lid-guard`), phase 3
- Hypr-Screen-Mirror (`DNF/Fedora: socat`;
  `noctalia msg plugins enable profidev/hypr-screen-mirror`), phase 3
- Mini Docker (`noctalia msg plugins enable 8bury/mini-docker`), phase 3
- AirPods (`librepods`, future own package;
  `noctalia msg plugins enable harveywuk/airpods`), phase 4
- Home Assistant (`noctalia msg plugins enable pozzoo/hassio`), blocked
  - Do not enable until the long-lived access token is proven to be stored
    through an acceptable secret provider rather than plaintext Noctalia
    state. Keep using the Home Assistant app or web UI until then.

## Webapps

- [Claude](https://claude.ai/) (`browser webapp`)
- [Messenger](https://www.facebook.com/messages) (`browser webapp`)
- [Grok](https://grok.com/) (`browser webapp`)
- [Fastmail](https://app.fastmail.com/) (`browser webapp`)
- [Google Maps](https://www.google.com/maps) (`browser webapp`)
- [Outlook](https://outlook.cloud.microsoft/mail/) (`browser webapp`)
- [Teams](https://teams.cloud.microsoft/) (`browser webapp`)
