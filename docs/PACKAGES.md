# Packages

What Nimbus installs on top of a minimal Fedora, by application name. What
Fedora's Standard install already provides is left out. Brackets mark the
install method only when it is not a Fedora package: Mise, a maker's script,
Cargo, or the Hyprland COPR.

## Session

- Hyprland - Wayland compositor [Hyprland COPR]
- hyprland-guiutils - Hyprland's dialogs; Hyprland warns at start without it
  [Hyprland COPR]
- xdg-desktop-portal-hyprland - screen sharing portal for Hyprland
  [Hyprland COPR]
- xdg-desktop-portal-gtk - file picker and settings portal
- Noctalia - shell: bar, launcher, control center, notifications, lock
  screen, idle, wallpaper, clipboard history, polkit agent, OSD
- Noctalia Greeter - greetd greeter matching Noctalia [Hyprland COPR]
- greetd - login manager
- Xwayland - X11 apps, Steam among them
- qt6-qtwayland and qt5-qtwayland - Qt apps on Wayland
- qt6ct - Qt theming
- xdg-utils - xdg-open and default applications
- xdg-user-dirs - standard home folders
- wl-clipboard - wl-copy and wl-paste
- gnome-keyring - secret service, recommended by Noctalia
- ddcutil - external monitor brightness, recommended by Noctalia
- gpu-screen-recorder - screen recording, recommended by Noctalia and used by
  its Screen Recorder plugin
- accountsservice - user accounts over D-Bus, read by Noctalia and the greeter
- udiskie and udisks2 - removable drives, also used by the Udiskie plugin

## Noctalia plugin dependencies

Declared by the plugins selected in the docs repository's plugin plan.
Plugins whose dependencies are already listed elsewhere: Keybind Cheatsheet
and Hypr-Screen-Mirror (hyprctl), Zed Provider (zed), GitHub Pull Requests
(gh), Tailnet (tailscale), Mini Docker (docker), File Search (fzf).

- grim and slurp - screenshot and region selection, for OCR
- Tesseract with English data - OCR engine
- qrencode - QR Code Encoder
- sqlite - the sqlite3 CLI, for Zed Provider
- jq and libnotify - Crashes
- socat - Hypr-Screen-Mirror
- ai-usagebar collector - AI Usage; source not decided
- librepods, patched fork - AirPods, phase 4, packaged by Nimbus later

## Desktop

- Ghostty - terminal
- Nautilus - file manager
- Loupe - image viewer
- Zathura - PDF viewer
- Brave Origin - browser
- Bibata - cursor theme
- Papirus - icon theme
- JetBrains Mono, JetBrains Mono Nerd, Inter, Terminus, Noto Emoji - fonts
- Noto Sans - fallback font

## Hardware

- PipeWire and WirePlumber - audio and screen capture
- BlueZ - Bluetooth
- power-profiles-daemon and upower - power profiles and battery, used by
  Noctalia
- fwupd - firmware updates
- NVIDIA driver - akmod-nvidia, CUDA libs, libva-nvidia-driver (desktop)
- akmods and mokutil - build and sign the NVIDIA module under Secure Boot
- CUPS and Epson escpr, escpr2 - printing
- Avahi - mDNS
- ffmpeg and GStreamer freeworld - media codecs

## Network

- NetworkManager-openconnect - OpenConnect VPN plugin
- WireGuard tools - WireGuard
- Tailscale - private network

## System

- Flatpak with the Flathub remote
- Flatseal - Flatpak permission GUI
- dnf5-plugins - copr and config-manager commands
- Snapper - Btrfs snapshots
- policycoreutils-python-utils - semanage

## Communication and media

- Signal - messaging
- Vesktop - Discord client
- Spotify - music streaming
- mpv - video player
- OBS Studio - screen recording and streaming
- Pinta - simple image editing

## Productivity

- 1Password and 1Password CLI - passwords and SSH agent
- Obsidian - notes
- Typst and Tinymist - typesetting and its language server

## Shell and CLI

- Zsh - shell
- Starship - prompt
- Sheldon - Zsh plugin manager
- zoxide - smarter cd
- fzf - fuzzy finder
- btop - system monitor
- fastfetch - system summary
- tldr - short command examples
- fd - find replacement
- ripgrep - grep replacement
- bat - cat with highlighting
- eza - ls replacement
- tokei - code statistics
- 7zip, zip, unzip, unrar - archives
- topgrade - update everything
- just - command runner [Cargo]
- watchexec - run commands on file change [Cargo]
- cargo-update - update Cargo tools [Cargo]

## Development

- Git, Git LFS, and GitHub CLI - version control and GitHub
- Neovim - terminal editor
- Zed - primary editor [zed.dev installer]
- VSCodium - secondary editor
- lazygit and lazydocker - Git and Docker TUIs
- Docker, Compose, and Buildx with container-selinux - containers
- Mise - runtime and tool manager [mise installer]
- Python via uv, .NET, Go, Java Corretto, Node.js, Rust - runtimes [Mise]
- C and C++ toolchain - gcc, clang, cmake, ninja
- Nix - package manager for dev shells
- markdownlint-cli - Markdown lint [Mise, npm]
- gopls - Go language server [Mise, go]
- Codex, Claude Code, Grok - CLI coding agents [Mise, npm]
- OpenCode - CLI coding agent [Mise, npm]
- Pi - CLI coding agent [Mise, npm]
- T3 Code - graphical control plane over the agents

## Gaming

- Steam - game store and launcher
- Lutris - launcher for other stores and Wine games
- Heroic - Epic and GOG launcher
- Prism Launcher - Minecraft
- Java runtime - for Prism Launcher
- WoWUp - WoW addon manager
- Wine, Winetricks, Protontricks - Windows games outside Steam
- umu-launcher - Proton for Lutris and Heroic
- ProtonUp-Qt - install Proton-GE and Wine-GE builds
- gamescope - micro-compositor for games
- MangoHud and GOverlay - performance overlay and its settings GUI
- gamemode - performance mode while a game runs
- 32-bit NVIDIA libraries - for Steam on the desktop

## Virtualization

- VM Curator - QEMU/KVM VM manager for Linux guests [Cargo]
- QEMU, OVMF, swtpm, virt-viewer - what VM Curator drives
- Windows guest - dockurr/windows container with FreeRDP

## Services Nimbus enables

- docker.service and docker.socket
- greetd.service
- power-profiles-daemon.service
- bluetooth.service
- cups.service
- avahi-daemon.service
- nix-daemon.service
- tailscaled.service
- snapper-cleanup.timer
- fstrim.timer
- sshd.service stays disabled

## Maybe

- fprintd - fingerprint unlock, supported by Noctalia (laptop)
- KDE Connect - phone integration, tried before the Phone Connect plugin
- thermald - Intel thermals (laptop)
- pipewire-codec-aptx - Bluetooth audio codecs
- simple-scan - scanning
- nm-connection-editor - connection editor GUI
- GNOME Disks - disk and USB tool
- a backup tool - Restic, Borg, or Pika Backup
