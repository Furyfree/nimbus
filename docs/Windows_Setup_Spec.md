# Windows 11 workstation with Fedora WSL

<!-- markdownlint-configure-file {"MD013": false} -->
<!-- Preserve the imported report's paragraph and table formatting. -->

Imported Windows/WSL planning report, retained for reference. Native Linux
remains the primary system; [DESKTOP-STORAGE.md](DESKTOP-STORAGE.md) records
its drive allocation and the owner's NAS backup choice. The verification
claims below belong to the original Windows session and were not rerun here.

The referenced PowerShell profile, source inventories and CTT screenshots
were not supplied with this import. Their filenames are retained below.

Date: 2026-09-11  
Status: implementation specification; no workstation setup has been executed.  
Companion: `Microsoft.PowerShell_profile.ps1`.

This document defines the desired state for a future Windows PowerShell bootstrap and Fedora provisioning script. It carries forward the recommendations from the planning discussion, incorporates the user's decisions, and separates selected components from optional additions.

## 1. Confirmed decisions

| Decision | Selection |
| --- | --- |
| Purpose | Full daily workstation for development and gaming |
| Installation media | Existing CTT Win11 Creator ISO, Windows 11 Pro |
| Development location | Use WSL as much as practical |
| WSL distribution | Fedora |
| Containers | Docker Engine inside Fedora WSL |
| Editors | Zed, Neovim, and VSCodium |
| Browser | Brave Origin **stable**; user already owns a license |
| Desktop | GlazeWM, PowerToys, Zebar, and Windhawk |
| Office suite | Microsoft 365 only; exclude LibreOffice |
| Gaming baseline | Steam, Battle.net, Prism Launcher, and WowUp-CF |
| AI baseline | ChatGPT/Codex desktop capabilities, Codex CLI, Claude desktop, and Claude Code |
| Configuration | Existing Chezmoi repository; repository work is outside this task |
| PowerShell | A useful companion shell with a .ps1 profile; WSL remains primary |
| Existing installations | Inventory and preserve; no automatic cleanup or uninstall |

Implementation defaults selected here: Fedora 44; WSL-managed default storage location; Linux username `pby`; Linux repositories in `~/git`; Zed on Windows with WSL remoting; Neovim in WSL; VSCodium in WSL through WSLg. These are editable configuration values, not additional questions required before implementation.

Microsoft 365 covers the office-suite role: documents, spreadsheets, presentations, and notes, according to the user's license. SumatraPDF, NAPS2, Paint.NET, and OBS have separate reading, scanning, image-editing, and recording roles.

## 2. Evidence and rationale

Inputs:

- Original Windows candidate list (`Current_Windows_App_List.md`).
- Declared Linux inventory (`Linux_install.md`). This describes repository selections, not proof of installation.
- The three CTT screenshots in this folder. They show available choices, not an instruction to install the whole catalog.
- Read-only inspection of Windows applications, startup registrations, command locations, hardware, and WSL distributions.

Observed Windows installation: Windows 11 Pro 25H2, i9-12900K, RTX 3080 plus Intel UHD 770, and 32 GiB RAM. Existing WSL distributions were Ubuntu and Debian, both version 2. Neither distribution's contents were inspected.

The uninstall registry contained 198 distinct display-name entries, including runtimes, drivers, games, and installer components. That is not a count of 198 independent applications or a performance measurement.

The current app collection includes multiple general-purpose editors and JetBrains IDEs, several browsers, VMware and VirtualBox, multiple sync clients, and several hardware-control utilities. The new baseline should have deliberate roles and controlled startup behavior. Existing software is not automatically classified as removable.

Relevant demonstrated setups:

- [Nick Janetakis's Windows/WSL workflow](https://nickjanetakis.com/blog/linux-dev-and-productivity-environment-on-windows-with-wsl-2-and-docker): Linux terminal development alongside native Windows applications. His [tools page](https://nickjanetakis.com/blog/the-tools-i-use) says he moved to native Linux in December 2025; the Windows setup is historical inspiration.
- [Scott Hanselman's 2021 tool list](https://www.hanselman.com/blog/scott-hanselmans-2021-ultimate-developer-and-power-users-tool-list-for-windows): useful examples of Terminal, PowerToys, package management, and diagnostic utilities. It is an older list, not a current package manifest.
- [Scott Spence's WSL Git setup](https://scottspence.com/posts/git-ssh-and-commit-signing-setup-in-wsl-ubuntu): an example of treating WSL configuration and Git identity as maintained development infrastructure.

The architecture below is a recommendation tailored to this user's choices, not a claim that those people use this exact combination.

## 3. Configuration contract

The future script should read structured configuration. This YAML is the starting configuration schema, not an executable installer.

~~~yaml
schema_version: 1
target: windows11-pro-workstation
mode: fresh-install
architecture: x64

wsl:
  enabled: true
  distribution: FedoraLinux-44
  version: 2
  username: pby
  install_location: null          # Omit --location; use WSL's default
  repositories: /home/pby/git
  default_shell: zsh
  memory_gb: 16
  swap_gb: 4
  processors: null                # Keep WSL's processor default
  networking: default             # Do not force mirrored networking
  systemd: true
  gui_apps: true
  start_at_windows_login: false

containers:
  provider: docker-engine-wsl
  repository: docker-official-fedora
  start_when_distribution_starts: true
  add_user_to_docker_group: true
  install_docker_desktop: false
  expose_tcp_daemon: false

editors:
  zed: windows-with-wsl-remoting
  neovim: wsl
  vscodium: wslg
  native_vscodium: false
  vscode: false
  extension_owner: chezmoi

desktop:
  browser: brave-origin-stable
  browser_license_present: true
  office: microsoft365
  window_manager: glazewm
  powertoys: true
  zebar: true
  windhawk: true
  windhawk_mods: []                # No mods were selected yet
  terminal_font: JetBrainsMono-Nerd-Font
  office_ui_language: en-us       # Implementation default; editable
  keyboard_layout: preserve
  timezone: preserve

configuration:
  owner: chezmoi
  repository: null                # Existing repository supplied at implementation
  bootstrap_repository_automatically: false
  powershell_profile: Microsoft.PowerShell_profile.ps1
  git_identity: from-chezmoi

profiles:
  desktop_core: true
  communication: true
  documents_media: true
  development_wsl: true
  ai: true
  gaming_core: true
  scanning: true
  local_transfer: true
  native_development: false
  extra_game_launchers: false
  diagnostics: false
  usb_tools: false
  advanced_media: false
  experimental_ai: false

policy:
  remove_existing_apps: false
  remove_existing_distributions: false
  install_prereleases: false
  change_default_apps_without_supported_api: false
  automatic_reboot: false
  automatic_system_tweaks_bundle: false
  manage_hardware_utilities: false
  backup_provider: null
~~~

Changing `wsl.username` must update derived Linux paths. Do not assume the Windows username and Linux username always match.

Runtime inputs may include an existing Microsoft 365 installer, a selected Office deployment configuration, Git identity, and an optional WSL install location. Authentication, license activation, and default-app selection may remain interactive completion tasks.

The script must distinguish:

- **Selected**: required for the requested baseline. An unresolved installation is reported as incomplete.
- **Optional**: install only when its profile or individual flag is enabled.
- **Excluded**: do not add it to a fresh installation. This does not authorize removing it from an existing installation.
- **Manual completion**: a known user interaction such as sign-in, activation, or an unsupported-to-automate preference.

## 4. Windows and WSL responsibilities

| Responsibility | Windows | Fedora WSL |
| --- | --- | --- |
| Desktop apps, games, screen capture, audio devices | Primary | Only selected developer GUI apps |
| Main development shell | PowerShell companion | Zsh primary, Bash available |
| Git | Small native installation for host config and GUI integrations | Primary Git, GitHub CLI, Lazygit, Git LFS |
| Editors | Zed front end | Zed remote services, Neovim, VSCodium through WSLg |
| Language runtimes and compilers | Optional native-development profile | Primary installation |
| Containers and development databases | Windows browser accesses local services | Docker Engine, Buildx, Compose, volumes |
| Files | Documents, media, downloads, game libraries | Git working trees, build output, environments, caches |
| Network access | Tailscale client and Windows firewall | Uses WSL networking; no second Tailscale daemon by default |
| Configuration | Chezmoi for Windows settings | Chezmoi for Linux settings |

Use `/home/pby/git/<repository>` for Linux projects. Do not put Linux `node_modules`, virtual environments, build directories, or container bind-mounted source trees under `/mnt/c` by default. Windows-native projects belong on Windows storage. This follows [Microsoft's filesystem guidance](https://learn.microsoft.com/en-us/windows/wsl/filesystems).

Keeping Windows Git, GitHub CLI, 1Password CLI, Starship, and zoxide is intentional: they support the Windows shell or native applications. These copies must not become the primary tools used for Linux repositories.

GUI integration should use each editor's supported remote mechanism. Opening a Linux directory via a UNC path alone does not make a Windows editor's tools run in Linux.

## 5. Package and configuration ownership

| Manager | Owns |
| --- | --- |
| WinGet | Windows apps with verified WinGet identities |
| Microsoft Store | Store product registrations and their updates |
| Official vendor installers | Stable Brave Origin, licensed Microsoft 365 installation, and documented exceptions |
| UniGetUI | Graphical package review; start with WinGet integration |
| DNF | Fedora system packages, build libraries, Docker packages, VSCodium RPM |
| Mise | User development runtimes, CLI tools, linters, Codex CLI, Claude Code |
| uv | User Python installations, Python environments and tools |
| rustup | Rust toolchain and components |
| Chezmoi | Personal configuration and editor extension selections |
| WinUtil | Selected Windows setup preferences, used deliberately |

No Scoop, Chocolatey, NVM, fnm, or separate global Python installer is required by this baseline.

Fedora's system Python can remain as an OS dependency. It is distinct from the uv-managed Python used by projects.

For every command, record one intended owner in the generated manifest. Remove duplicate *declarations* when adapting the existing Linux inventory: Rust belongs to rustup here; Docker belongs to DNF; ShellCheck belongs to Mise. This document does not request edits to the user's existing Chezmoi repository.

Mise's [registry](https://mise.jdx.dev/registry.html) maps tool names to backends. Resolve and record the backend and exact version when producing an installation lock. Prefer release binaries where available. A project-local version declaration takes precedence over personal defaults.

## 6. Selected Windows packages

The following identifiers were checked against local WinGet/Store metadata on 2026-09-11. Revalidate exact identity, publisher, architecture, and release channel when the implementation resolves its installation plan. Listed versions are intentionally not frozen here.

| Key | Application | Source and identifier | Profile |
| --- | --- | --- | --- |
| terminal | Windows Terminal | winget: `Microsoft.WindowsTerminal` | desktop_core |
| powershell | PowerShell 7 | winget: `Microsoft.PowerShell` | desktop_core |
| powertoys | PowerToys | winget: `Microsoft.PowerToys` | desktop_core |
| glazewm | GlazeWM | winget: `glzr-io.glazewm` | desktop_core |
| zebar | Zebar | winget: `glzr-io.zebar` | desktop_core |
| windhawk | Windhawk | winget: `RamenSoftware.Windhawk` | desktop_core |
| everything | Everything stable | winget: `voidtools.Everything` | desktop_core |
| unigetui | UniGetUI | winget: `Devolutions.UniGetUI` | desktop_core |
| starship_windows | Starship | winget: `Starship.Starship` | desktop_core |
| zoxide_windows | zoxide | winget: `ajeetdsouza.zoxide` | desktop_core |
| git_windows | Git for Windows | winget: `Git.Git` | desktop_core |
| gh_windows | GitHub CLI | winget: `GitHub.cli` | desktop_core |
| chezmoi_windows | Chezmoi | winget: `twpayne.chezmoi` | desktop_core |
| password_manager | 1Password | winget: `AgileBits.1Password` | desktop_core |
| password_cli_windows | 1Password CLI | winget: `AgileBits.1Password.CLI` | desktop_core |
| tailscale | Tailscale | winget: `Tailscale.Tailscale` | desktop_core |
| archives | 7-Zip | winget: `7zip.7zip` | desktop_core |
| signal | Signal | winget: `OpenWhisperSystems.Signal` | communication |
| discord_client | Vesktop | winget: `Vencord.Vesktop` | communication |
| notes | Obsidian | winget: `Obsidian.Obsidian` | documents_media |
| music | Spotify | winget: `Spotify.Spotify` | documents_media |
| pdf_reader | SumatraPDF | winget: `SumatraPDF.SumatraPDF` | documents_media |
| video_player | mpv, shinchiro build | winget: `shinchiro.mpv` | documents_media |
| image_editor | Paint.NET | winget: `dotPDN.PaintDotNet` | documents_media |
| recorder | OBS Studio | winget: `OBSProject.OBSStudio` | documents_media |
| audio_mixer | EarTrumpet | winget: `File-New-Project.EarTrumpet` | documents_media |
| file_transfer | LocalSend | winget: `LocalSend.LocalSend` | local_transfer |
| scanner | NAPS2 | winget: `Cyanfish.NAPS2` | scanning |
| zed | Zed stable | winget: `ZedIndustries.Zed` | development_wsl |
| openai_desktop | ChatGPT with Codex desktop capabilities | msstore: `9PLM9XGG6VKS` | ai |
| claude_desktop | Claude | winget: `Anthropic.Claude` | ai |
| steam | Steam | winget: `Valve.Steam` | gaming_core |
| battlenet | Battle.net | winget: `Blizzard.BattleNet` | gaming_core |
| minecraft | Prism Launcher | winget: `PrismLauncher.PrismLauncher` | gaming_core |
| wow_addons | WowUp with CurseForge | winget: `WowUp.CF` | gaming_core |
| vc_runtime_x64 | Visual C++ v14 redistributable x64 | winget: `Microsoft.VCRedist.2015+.x64` | gaming_core |
| vc_runtime_x86 | Visual C++ v14 redistributable x86 | winget: `Microsoft.VCRedist.2015+.x86` | gaming_core |

The mpv package is a third-party Windows build linked by the [mpv project](https://mpv.io/installation/). It is not a reason to install a second player or a general codec pack.

For existing apps, detect package identity as well as uninstall/Store registrations. Some applications expose multiple component entries; do not infer duplicate installations from display names alone. Do not install a second PowerShell distribution merely because the first came from the Store.

### 6.1 Selected applications with separate installation handling

| Key | Application | Required handling |
| --- | --- | --- |
| browser | Brave Origin stable | Obtain the stable Windows installer from [Brave](https://brave.com/origin/); validate publisher and channel; activation uses the user's existing license |
| office | Microsoft 365 | Use the installer available to the user's Microsoft account, or a separately supplied valid Office deployment configuration |
| fonts | JetBrains Mono Nerd Font | Install the chosen official font release for the Windows user; reuse Chezmoi font preferences |
| fastmail | Fastmail | Use the browser; an installed web-app shortcut is optional |

WinGet currently returns `Brave.BraveOrigin.Nightly` but no stable Origin package in this environment. Do not silently select Nightly or regular Brave. Resolve a stable vendor installer, verify it, and determine its supported unattended options before execution. If unattended setup cannot be established, provide the official installer as a manual completion step.

Do not guess a Microsoft 365 product ID, license type, or deployment channel. Existing compatible Microsoft 365 installations satisfy the application requirement. A launcher or hub app does not count as installed Word/Excel/PowerPoint. Never uninstall an existing Office installation to resolve this automatically.

### 6.2 OpenAI package identity

The checked Store product `9PLM9XGG6VKS` is published by OpenAI and describes ChatGPT, Work, and Codex desktop capabilities. It satisfies the desktop requirements using the current product packaging. `OpenAI.Codex` in WinGet is **Codex CLI**, not the desktop application.

Configure the desktop agent to use WSL and choose WSL separately for its integrated terminal. Restart the app after changing the agent environment. Keep native Git for desktop Git features. These are separate settings in the [official Windows app documentation](https://learn.chatgpt.com/docs/windows/windows-app).

Install Codex CLI inside Fedora through Mise. Keep authentication and machine-specific session data local; sign in interactively. Do not automatically share credential/session directories between Windows and Linux.

## 7. Fedora WSL setup

### 7.1 Installation and resume behavior

1. Detect WSL and virtualization availability.
2. Install or update the supported WSL package using documented Windows mechanisms.
3. When installing WSL itself, use `--no-distribution` so Ubuntu is not added accidentally.
4. Record a required reboot and exit cleanly. Resume with the same input configuration after reboot.
5. Validate `FedoraLinux-44` against `wsl --list --online`. This identifier was available during inspection.
6. Install Fedora as WSL 2. Omit `--location` for the selected default; use it only when a configured location has been validated.
7. Complete initial user creation as `pby`, with normal sudo access. Do not embed a password.
8. Make this Fedora distribution the WSL default and create `/home/pby/git`.
9. Keep Ubuntu, Debian, and any other existing distributions intact.

Sources: [WSL commands](https://learn.microsoft.com/en-us/windows/wsl/basic-commands), [official Fedora WSL images](https://fedoraproject.org/misc/).

A distribution with a matching name must be inspected and reused only when it matches the expected OS and user. A conflict produces an actionable status; it never triggers `wsl --unregister`.

### 7.2 Storage and resource defaults

Let WSL choose its normal installation location. Record the actual location in the installation report. A different drive can be selected later through supported WSL migration commands; no disk is reformatted by setup.

The WSL disk contains repositories, uncommitted work, ignored files, toolchains, caches, container images, and Docker volumes. GitHub and Chezmoi help rebuild the environment but do not automatically preserve all of that state.

For the inspected 32 GiB machine, the following is an initial upper memory limit, not a reservation:

~~~ini
[wsl2]
memory=16GB
swap=4GB
~~~

Keep processor allocation, networking, DNS integration, and memory-reclaim behavior at current WSL defaults unless a tested need justifies an override. Do not enable experimental disk settings or force mirrored networking in the baseline.

Merge, rather than replace, existing `.wslconfig`. These settings apply to all WSL 2 distributions, so report the scope and defer any necessary restart until work has been saved. Source: [WSL configuration](https://learn.microsoft.com/en-us/windows/wsl/wsl-config).

Inside Fedora, enable systemd and the configured default user by merging into `/etc/wsl.conf`:

~~~ini
[boot]
systemd=true

[user]
default=pby
~~~

Preserve working Windows interop and WSLg. Put user Linux executables before inherited Windows paths, and verify tool resolution. Do not copy a full Linux desktop configuration over WSL's display, DNS, mount, or GPU integration.

### 7.3 Fedora system packages

DNF owns the following baseline packages. Package names are implementation candidates that must be resolved against the selected Fedora release before the script applies changes; they have not been tested inside a newly installed Fedora distribution here.

| Group | DNF package names |
| --- | --- |
| Base and administration | `sudo`, `shadow-utils`, `util-linux`, `which`, `findutils`, `procps-ng`, `less`, `diffutils`, `file` |
| Fetching and certificates | `curl`, `wget`, `ca-certificates`, `gnupg2`, `dnf-plugins-core` |
| Git and SSH | `git`, `git-lfs`, `openssh-clients` |
| Shell | `zsh`, `bash-completion` |
| Archives | `tar`, `gzip`, `xz`, `zip`, `unzip`, `7zip` |
| Native Linux builds | `gcc`, `gcc-c++`, `clang`, `cmake`, `ninja-build`, `make`, `pkgconf-pkg-config` |
| Development libraries | `openssl-devel`, `libcurl-devel`; resolve Fedora's appropriate zlib development provider |
| Small database CLI | `sqlite` |
| GUI editor | `codium` from the VSCodium-related RPM repository described below |

Use narrow package lists; do not install a Workstation desktop group. If a package was renamed or has a different provider, record the verified replacement explicitly. Do not substitute a similarly named tool with a different command interface.

## 8. WSL runtimes and CLI tools

Bootstrap Linux Mise, Chezmoi, and rustup using their official distribution methods. Use per-user locations and record versions and checksums where the upstream provides them. Run user tooling as `pby`, not root.

### 8.1 Runtime policy

| Tool | Owner | Version selector | Rule |
| --- | --- | --- | --- |
| Node.js | Mise | `lts` | Resolve to an exact supported LTS version |
| pnpm | Mise | `latest` stable | Project package-manager declaration may select another version |
| Go | Mise | `latest` stable | Project declarations take precedence |
| Java development JDK | Mise | `corretto-25` | Vendor-qualified; independent of Minecraft |
| .NET SDK | Mise | `10` | Linux SDK, not Windows Desktop Runtime |
| uv | Mise | `latest` stable | uv then owns user Python |
| Python | uv | Project-selected; default 3.13 | Respect existing `.python-version` and project constraints |
| Rust | rustup | `stable` | Install rustfmt and clippy; respect `rust-toolchain.toml` |
| Lua | Optional Mise entry | Project-selected | Do not install standalone Lua solely for Neovim configuration |

Treat selectors such as `latest` and `lts` as resolution policies. Record resolved versions before installation, and reuse that resolution when resuming an interrupted run. Rerunning setup is not an implicit upgrade.

Do not add Rust to Mise as well as rustup. Do not replace Fedora's system Python or use global sudo pip.

References: [Mise Java vendor selection](https://mise.jdx.dev/lang/java.html), [uv Python management](https://docs.astral.sh/uv/guides/install-python/), [.NET support policy](https://dotnet.microsoft.com/en-us/platform/support/policy).

### 8.2 User tools

Mise owns these Linux tools. Resolve each shorthand/backend against the current registry and existing Chezmoi definitions during implementation.

| Purpose | Selected tools |
| --- | --- |
| Shell prompt and plugins | Starship, Sheldon |
| Navigation and search | eza, fzf, fd, bat, zoxide, ripgrep |
| System and file inspection | tokei, btop, tealdeer/tldr, duf, Yazi |
| Structured data | jq, Mike Farah's yq |
| Git | GitHub CLI (`github-cli` registry key), Lazygit |
| Tasks and secrets tooling | just, Gitleaks, 1Password CLI |
| Editor | Neovim |
| Go tooling | gopls, Staticcheck, golangci-lint |
| Formatting and checks | Ruff, StyLua, ShellCheck, shfmt, Taplo, actionlint, markdownlint-cli |
| Document development | Typst, Tinymist |
| AI coding | Codex CLI, Claude Code |

Git and Git LFS themselves remain DNF-owned. Do not also install Mise Docker CLI or Compose; Docker's Fedora packages own both.

Current registry entries include `codex` and `claude` with binary-download backends. For this specification, Mise owns both CLIs. Do not additionally execute standalone installers or npm global installs for them. Disable Claude Code's self-updater for the managed copy using its documented setting, and update through Mise. See the [Mise registry](https://mise.jdx.dev/registry.html) and [Claude Code installation/update documentation](https://code.claude.com/docs/en/setup).

Language servers may be editor-managed when an extension requires that model. Avoid a separate duplicate tool installation unless its purpose and configured executable path are explicit.

### 8.3 Linux shell configuration

Use Zsh as the primary interactive shell, with Bash available for scripts.

Chezmoi should supply the existing Starship theme, Sheldon configuration, completions, autosuggestions, syntax highlighting, fzf-tab, zoxide integration, and key bindings that work under WSL.

Use `EDITOR=nvim` and `VISUAL=nvim` inside WSL. Make `~/.local/bin` and Mise shims available to non-interactive editor processes as well as interactive shells. Initialize interactive Mise shell activation in the appropriate shell startup file.

Keep Linux programs ahead of similarly named Windows executables. Validate `git`, `node`, `python`, `pnpm`, `go`, `cargo`, `dotnet`, `codex`, and `claude` in the contexts where they are used.

Do not install or activate Hyprland, Noctalia, greetd, Linux display-manager services, GRUB themes, desktop portals, Linux GPU kernel drivers, or a PipeWire desktop session inside WSL.

## 9. Docker Engine inside Fedora

Use the official Docker Fedora repository with signature verification. The selected packages are:

~~~text
docker-ce
docker-ce-cli
containerd.io
docker-buildx-plugin
docker-compose-plugin
~~~

Use systemd to start Docker when Fedora starts. Add `pby` to the Docker group and refresh the login session. Docker group membership grants root-equivalent control within the distribution; it does not grant Windows administrator membership.

Keep the Unix socket as the local interface. Do not expose an unauthenticated TCP daemon. Run `docker` and `docker compose` in Fedora, including when called through `wrun`.

Docker data stays on the Linux filesystem. Starting PowerShell must not start Fedora; Docker begins when the development distribution is used. Do not add container restart policies or project services that the user's projects did not specify.

Source and package procedure: [Docker Engine on Fedora](https://docs.docker.com/engine/install/fedora/).

When inspecting an existing environment, report conflicting Docker providers rather than automatically uninstalling them. Docker Desktop is excluded from a fresh installation.

Required checks:

- `docker version` includes a reachable server.
- `docker compose version` and `docker buildx version` work.
- A temporary hello-world container succeeds.
- A temporary development service is reachable from the Windows browser through the intended localhost mapping.
- Temporary verification containers are removed without pruning unrelated images, containers, or volumes.

## 10. Editors and extensions

### 10.1 Zed

Install Zed on Windows and use its native WSL project support. Use `projects: open wsl` to select Fedora and open a directory under `~/git`. Zed's remote tools and terminals must resolve Linux executables.

Zed supplies the front end while the source, language tooling, and build processes stay in WSL. Reuse the relevant Chezmoi settings and selected themes. Reference: [Zed remote development](https://zed.dev/docs/remote-development).

### 10.2 Neovim

Install Neovim only in WSL for the baseline. Reuse the existing lazy.nvim, Gitsigns, nvim-surround, nvim-treesitter, Snacks, and which-key configuration.

Make it usable from Windows Terminal and from the editors' Linux terminals. Verify clipboard behavior under WSL before importing platform-specific clipboard settings.

### 10.3 VSCodium

Default to the Linux RPM installation inside Fedora and present its window through WSLg. This places the extension host, terminal, and language tools alongside the Linux projects without requiring Microsoft's proprietary remote extension.

Use the RPM repository referenced by [VSCodium's installation instructions](https://vscodium.com/), keep repository/package signature checks enabled, and install `codium`. Manage one repository file idempotently rather than appending duplicate definitions.

Create a clearly labeled Windows shortcut such as **VSCodium (Fedora)**, or use the provided `codiumw` PowerShell helper. WSLg supports integrated Linux application windows; no separate desktop environment or X server is required. [Microsoft WSL GUI documentation](https://learn.microsoft.com/en-us/windows/wsl/tutorials/gui-apps)

If WSLg does not meet the user's display or input needs, the optional native VSCodium package is `VSCodium.VSCodium`. Its Linux remote-development path must be separately verified; do not silently install VS Code or assume that Microsoft's Remote WSL extension is supported.

### 10.4 Extension policy

Chezmoi remains the extension owner. Start with the tools needed by active projects and preserve an optional extended set from the Linux inventory.

Suggested groups:

- Base: Git support, TOML/YAML/JSON, EditorConfig or equivalent settings, formatting, selected theme and icons.
- Languages: Go, Python, Rust, Java, .NET, web, and other groups enabled by actual project use.
- Documents: Typst/Tinymist and selected document viewers.
- AI: supported extensions for the selected tools.
- Optional: Java/Jupyter bundles, Flutter/Dart, Nix, LaTeX, extra themes, and specialized viewers.

Validate Open VSX availability and actual activation. Do not copy all 52 extensions automatically or modify product identities to make incompatible extensions appear supported. The existing manual-extension list needs per-extension decisions. [VSCodium compatibility documentation](https://github.com/VSCodium/vscodium/blob/master/docs/extensions.md)

## 11. PowerShell companion profile

The companion `Microsoft.PowerShell_profile.ps1` is a ready-to-review profile implementation. It is not installed in the user's home directory by this task.

The future setup should hand it to Chezmoi or deploy it to the path reported by `$PROFILE.CurrentUserAllHosts` **inside PowerShell 7**. Resolve this value instead of hardcoding Documents, which may be redirected.

| Feature | Behavior |
| --- | --- |
| Prompt | Starship, with the shared theme where compatible |
| Navigation | zoxide `z`; `ll`; `..`; `...`; `up`; `mkcd` |
| Inspection | `which`, `path`, `ports` |
| Windows Git | `gs`, `gd`, `glog` |
| Package review | `apps` and `updates`; `updates` alone lists available updates |
| WSL entry | `w` opens Fedora home; `wg` opens the Linux Git directory |
| WSL files | `wfiles` opens the Linux Git directory in Explorer |
| WSL status | `wstatus` lists distributions |
| Linux command execution | `wrun '<Linux directory>' <command> <arguments>` |
| Linux GUI editor | `codiumw '<Linux directory>'` |
| Discovery | `pshelp` lists the helpers |
| Editing | PSReadLine history prediction/search and menu completion |
| Completion | WinGet completion loaded on demand |

Examples:

~~~powershell
w
wg
wrun '~/git/nimbus' git status --short
wrun '~/git/my-app' docker compose ps
codiumw '~/git/my-app'
gs
updates
~~~

`wrun` forwards an argument array to `wsl --exec`. It does not build a shell command string or automatically reinterpret Windows paths. Shell pipelines and aliases should be used inside the interactive Fedora shell.

The profile must not install modules, run upgrades, start WSL, launch a browser, or contact account services at startup. Missing optional prompt tools must not break the shell. It must not replace standard `cd`, `rm`, `cp`, or `mv` semantics.

Set `WORKSTATION_WSL_DISTRO`, `WORKSTATION_WSL_USER`, and `WORKSTATION_WSL_GIT_DIR` through the eventual configuration when values differ from the defaults. `WORKSTATION_PROFILE_MINIMAL=1` skips prompt/completion initialization for diagnosis.

References: [Starship](https://starship.rs/guide/), [zoxide](https://github.com/ajeetdsouza/zoxide), [WinGet completion](https://learn.microsoft.com/en-us/windows/package-manager/winget/tab-completion).

## 12. Desktop configuration

### 12.1 Window management

GlazeWM owns automatic tiling and workspace key bindings. Zebar provides the selected bar. Keep PowerToys FancyZones disabled while GlazeWM owns window layouts.

Use a simple workspace scheme initially: 1 browser, 2 code, 3 terminal, 4 communication, 5 documents/media. Preserve the user's established Hyprland-style shortcuts where they do not conflict with Windows or game bindings.

Add tested exceptions for fullscreen games, launchers, dialogs, and transient utility windows. Exact matching rules belong in versioned configuration. [GlazeWM documentation](https://github.com/glzr-io/glazewm)

Install Windhawk but leave its mod list empty until specific mods are chosen. It is not a requirement to add StartAllBack, ExplorerPatcher, Nilesoft Shell, or another shell customization framework.

### 12.2 PowerToys and files

Initial enabled modules: Command Palette, Peek, PowerRename, Keyboard Manager, Color Picker, Image Resizer, File Locksmith, and Always On Top. Keep unneeded modules disabled. Use Command Palette as the primary launcher; do not also configure another launcher hotkey by default.

Use Explorer as the Windows file manager and Everything for filename search. Keep Photos, Paint, Notepad, and Snipping Tool when present. SumatraPDF is the preferred PDF reader; mpv is the preferred video player.

Reference: [PowerToys modules](https://learn.microsoft.com/en-us/windows/powertoys/).

Make default-app choices through supported Windows mechanisms or a manual completion task. Do not write protected UserChoice hashes.

### 12.3 Startup policy

Start the utilities that provide continuous desktop functionality: GlazeWM, Zebar, PowerToys, Everything as configured, EarTrumpet, and the password manager if needed for integration. Tailscale uses its normal service behavior.

Signal and Vesktop startup are user preferences; default both to off until configured. Keep Steam, Battle.net, extra launchers, OBS, Spotify, UniGetUI, and AI desktop apps off automatic login startup by default.

Preserve Windows security notification components. Do not disable vendor services globally. NZXT CAM, Logitech G HUB, Razer software, and Wooting utilities require assessment against the attached hardware and desired features.

Do not treat every installed application as a background process. Use actual startup settings and service purposes when reviewing an existing installation.

### 12.4 Documents, scanning, and transfer

Use Microsoft 365 as the only office suite. OneDrive is optional according to the user's sync preference; do not automatically relocate Desktop/Documents or place WSL disks in a sync folder.

Use NAPS2 for scanning. Test the Epson ET-5800 over the network; install only the required official driver if the selected scanning path needs it. Do not assume that its Linux driverless path proves Windows compatibility.

Use LocalSend for device transfers and Tailscale for the existing tailnet. Authentication, pairing, and firewall prompts are completion tasks; no credentials are stored in the manifest.

## 13. Gaming

Install only the selected launchers, not all games or every store client. Expose library locations as inputs or let the user choose them in each launcher. Do not infer that an apparently empty drive may be reformatted.

Prism should use automatic Java detection/download for each Minecraft version. Do not point it at the WSL JDK or force Java 25 for all instances. [Prism Java documentation](https://prismlauncher.org/wiki/getting-started/installing-java/)

Configure WowUp-CF after the relevant WoW installation directories exist. Wago and other addon managers are not part of the baseline.

Retain required game services and redistributables. Install additional .NET Desktop Runtime versions only for applications that require them. A newer runtime does not automatically satisfy every older application. The Linux .NET SDK does not satisfy Windows desktop runtime requirements.

Use Windows GPU drivers from the hardware vendor. NVIDIA App and overlay features are optional according to use. OBS is the selected general recording tool; additional recording overlays are optional.

## 14. Optional applications and profiles

These options remain available for a future manifest but are disabled by default.

| Profile / option | Application | Verified identifier or resolution rule |
| --- | --- | --- |
| extra_browser | Firefox | `Mozilla.Firefox` |
| extra_game_launchers | EA app | `ElectronicArts.EADesktop` |
| extra_game_launchers | Epic Games Launcher | `EpicGames.EpicGamesLauncher` |
| extra_game_launchers | Ubisoft Connect | `Ubisoft.Connect` |
| native_development | Visual Studio Build Tools 2026 | `Microsoft.VisualStudio.BuildTools` |
| native_development | Full Visual Studio Community 2026 | `Microsoft.VisualStudio.Community`; choose this instead of redundant Build Tools installation when workloads overlap |
| native_development | Windows .NET SDK 10 | `Microsoft.DotNet.SDK.10` |
| native_development | Windows Rust toolchain | `Rustlang.Rustup` |
| native_development | CMake and Ninja | `Kitware.CMake`, `Ninja-build.Ninja`; avoid duplicates if Visual Studio owns these |
| native_vscodium | VSCodium for Windows | `VSCodium.VSCodium` |
| optional_vscode | VS Code | `Microsoft.VisualStudioCode`; explicit opt-in only |
| diagnostics | Wireshark | `WiresharkFoundation.Wireshark`; capture-driver choice must be explicit |
| diagnostics | Autoruns | `Microsoft.Sysinternals.Autoruns` |
| diagnostics | WizTree | `AntibodySoftware.WizTree` |
| diagnostics | HWiNFO | `REALiX.HWiNFO` |
| usb_tools | Rufus | `Rufus.Rufus` |
| usb_tools | Ventoy | `Ventoy.Ventoy` |
| advanced_media | GIMP | `GIMP.GIMP.3` |
| advanced_media | ShareX | Resolve official package if advanced screenshot workflows are desired |
| advanced_media | Blender, DaVinci Resolve, Audacity | Resolve vendor packages when selected; preserve license/edition choices |
| experimental_ai | GitHub Copilot app, T3 Code, OpenCode, other agents | Resolve exact official app/CLI identity; do not confuse Copilot with GitHub Desktop |
| linux_extras | fastfetch, Topgrade, Nix | Only when their purpose is explicit; Topgrade must respect package ownership |
| linux_document_tools | FFmpeg, ImageMagick, Poppler, resvg, Tesseract, qrencode, Ghostscript | Linux CLI tools under DNF/Mise as appropriate; resolve each name/provider first |
| host_utilities | Twinkle Tray, AutoHotkey | Only for a monitor-control or automation need not already covered |
| remote_media | Moonlight/Sunshine, Jellyfin, other servers | Separate opt-in networking/service profile |

Optional profiles that lack resolved identifiers or dependencies cannot execute until their package adapter is complete. They must not be installed by fuzzy name matching.

The WinGet identifiers `Microsoft.VisualStudio.2026.BuildTools` and `Microsoft.VisualStudio.2026.Community` did **not** resolve during inspection. Use the verified identifiers in the table and still recheck their selected channel at implementation.

For native development, install only required Visual Studio workloads and SDK components. Record their component IDs in an explicit workload configuration. The full IDE is not a requirement for Linux compilation.

Rufus and Ventoy installation does not authorize writing or formatting a USB drive. Actual media creation is a separate operation with a selected device.

## 15. Excluded from the fresh baseline

- Brave Origin Nightly, beta channels, and additional browsers without a selected purpose.
- LibreOffice, because Microsoft 365 was explicitly selected.
- Docker Desktop and separate Windows container providers.
- Standalone Windows Node, Python, Go, Java, Lua, Rust, and .NET SDK installations outside the native-development profile.
- Windows Neovim and the full native Linux CLI collection.
- Caligula for Windows/WSL disk writing; [Windows support remains planned](https://github.com/ifd3f/caligula).
- WinRAR unless creating RAR archives is required; [7-Zip extracts RAR](https://www.7-zip.org/).
- DISMTools as an everyday application; keep image servicing a separate task.
- VMware, VirtualBox, QEMU/virt-manager, and Linux Windows-VM infrastructure without a specific VM requirement.
- Linux gaming compatibility infrastructure: Wine, Winetricks, Protontricks, ProtonPlus, UMU, Gamescope, GameMode, MangoHud, GOverlay.
- Linux desktop/session infrastructure, hardware services, Snapper/Btrfs integration, display-manager setup, and GRUB configuration inside WSL.
- Additional Linux copies of browser, communication apps, office suite, screen recorder, and password-manager GUI.
- General codec bundles, registry cleaners, driver updater collections, and blanket performance-tweak packages.
- Extra browser shells, launchers, image viewers, or sync clients merely because they appear in the screenshots.

Exclusion does not imply that an application is bad or that an existing installation should be removed.

## 16. CTT ISO and Windows preferences

Treat the user's existing CTT Win11 Creator ISO as the installation input. Its contents and build options were not inspected. This specification begins after Windows installation.

CTT describes Creator as customizing an official Windows image while aiming to retain normal application compatibility. Use this as context, not as a guarantee about this particular ISO. [CTT's Creator explanation](https://christitus.com/winutil-in-2026/)

Preflight must check that Windows Update, Defender, Store/App Installer, WebView2, WSL support, and required game components work. Repair a detected missing prerequisite through a documented route; do not apply another blanket debloat layer.

Suggested preferences: show filename extensions, reduce unsolicited suggestions, and configure the user's chosen defaults. Leave security services, firewall, UAC, virtualization, and update functionality operational.

Do not import the entire current WinUtil tweak preset or disable everything in Advanced Tweaks. The final implementation should have a small, named set of reversible preferences with state detection.

## 17. Chezmoi integration boundary

The user already has a Chezmoi repository and did not request repository changes or discovery.

The future script needs a narrow integration point: detect Chezmoi, accept an already-configured checkout or supplied repository input, apply the appropriate Windows/WSL configuration subset, and report the applied revision.

Reuse shared themes, fonts, Git preferences, shell behavior, editor settings, and extensions where appropriate. Keep platform paths and machine-specific settings conditional.

The Linux inventory currently declares Windows Mise configuration as excluded. This WSL-first plan does not require native Mise unless the optional native-development design chooses it. WSL uses Linux Mise.

The script must not run Linux desktop provisioning hooks from Chezmoi in WSL or launch Nimbus's complete desktop profile. If Nimbus later supplies a dedicated WSL profile, it may replace the Fedora package adapter while preserving the ownership rules.

Do not store passwords, tokens, machine authentication caches, or private key material in the generated app manifest. Git identity and authentication are separate tasks; never infer the user's email from an OS account name.

## 18. Installer design and execution order

Suggested implementation files, to be created later:

~~~text
setup.ps1                   Windows orchestration and state reporting
config/workstation.yaml     User choices
manifests/windows.json      Resolved Windows packages and installer adapters
manifests/fedora.json       DNF packages and repositories
manifests/mise.toml         WSL user tools and version selectors
scripts/provision-fedora.sh Fedora-specific setup
profiles/                   Optional additions and workload definitions
state/                      Resolved versions, checkpoints, logs, completion report
~~~

The proposed script interface should support `-Plan`, `-Apply`, `-Resume`, `-Verify`, and a separately selected `-Update` mode. Applying a selected configuration is authorized setup work; do not add repeated confirmations for every package.

Execution phases:

1. **Inspect:** OS, hardware, current applications, WSL distributions, pending reboot, configuration inputs.
2. **Resolve:** exact package identities, sources, versions, required installer arguments, and profile dependencies.
3. **Windows foundation:** Terminal, PowerShell, WinGet prerequisites, native integration tools, WSL prerequisites.
4. **Reboot boundary:** save state and provide a resume command when necessary.
5. **Fedora initialization:** install/reuse distribution, user, folders, systemd, resource configuration.
6. **Linux system provisioning:** DNF repositories and base/build packages.
7. **Linux user provisioning:** Mise, uv, rustup, CLI tooling, and shell setup.
8. **Containers and editors:** Docker, VSCodium/WSLg, Zed WSL connection.
9. **Desktop and gaming apps:** selected Windows profiles, vendor/Store adapters.
10. **Configuration:** Chezmoi integration, PowerShell profile, shortcuts, startup preferences.
11. **Verification:** component checks and cross-environment workflows.
12. **Completion report:** installed/unchanged/failed/manual items, resolved versions, reboot needs.

Script requirements:

- Reruns detect satisfied state and do not duplicate profile content, shortcuts, repositories, PATH entries, or services.
- A phase is complete only after verifying its result. A logged command without a successful outcome is not completion.
- Use argument arrays across PowerShell/WSL; avoid dynamically constructing shell source from paths or user input.
- Use per-user installation where supported; elevate only machine-level phases.
- Maintain existing configuration keys when updating structured files; save changed-file backups.
- Check native process exit codes, including reboot-required installer results.
- Keep unrelated phases progressing after an isolated failure where dependencies allow it.
- Unresolved selected vendor installers appear as manual/incomplete, never silently omitted.
- Package-manager metadata can change: revalidate when resolving a new plan, then keep the resolved plan stable during resume.
- Do not execute raw destructive snippets copied from vendor uninstall documentation.
- Do not prune disks, Docker volumes, WSL distributions, or existing applications.

## 19. Verification and completion

| Area | Required evidence |
| --- | --- |
| Windows prerequisites | Correct edition/architecture; Store/App Installer, Windows Update, and WSL prerequisites usable |
| Fedora | WSL 2; Fedora 44; configured non-root user; systemd available |
| Shell | Zsh opens cleanly; Mise activation and shims work; shared prompt renders |
| PowerShell | Profile parses; normal and minimal loading work; helpers do not start WSL at shell startup |
| Tool ownership | Linux tools resolve to Linux paths and the intended managers; Windows integration tools resolve independently |
| Runtimes | Version checks; a small real project/build relevant to each enabled language profile |
| Docker | Reachable daemon, Compose, Buildx, temporary test container and Windows localhost access |
| Zed | Fedora project opens; terminal/compiler/language tooling run inside Fedora |
| VSCodium | WSLg window opens; Linux terminal and selected extensions function |
| Neovim | Starts with Chezmoi config; required plugins and clipboard behavior function |
| AI tools | CLIs start inside Fedora; desktop agent uses selected WSL environment; authentication reported separately |
| Desktop | GlazeWM shortcuts, Zebar, selected PowerToys modules, and fullscreen game exceptions work |
| Documents | Microsoft 365 activated; PDF/video/image apps open representative files |
| Gaming | Launchers sign in; Prism chooses appropriate Java; one representative game launches |
| Scanning/transfer | Epson test scan and LocalSend transfer where those devices are available |
| Rerun | A second normal apply reports already-satisfied work without duplicating or resetting configuration |

For runtimes, a binary version check alone is insufficient when a build depends on system libraries, an SDK workload, or a remote editor environment. Keep checks small and relevant; do not run a large unrelated test suite.

Manual completion items may include Microsoft/Brave/AI/password-manager/launcher sign-in, browser/default-app selection, license activation, scanner discovery, hardware-specific exceptions, and selected Windhawk mods. The report must clearly distinguish installed software from fully configured accounts.

## 20. Updates, reconstruction, and local data

Use the same owners for updates: WinGet/Store/vendor updater for Windows apps, DNF for Fedora and Docker, Mise for its user tools, uv for user Python, rustup for Rust, and Chezmoi for configuration. Avoid multiple scheduled updaters managing the same tools.

Windows applications and games should continue receiving their supported security and compatibility updates. Do not freeze the OS indefinitely for the sake of a static installer.

The user's repositories and configuration are on GitHub, so WSL should be easy to reconstruct. Still preserve uncommitted files, ignored configuration, and Docker volumes when modifying an existing distribution. No automatic distribution reset or `docker system prune --volumes` belongs in the setup script.

An optional backup provider and destination remain unset. A future backup profile can cover Windows documents, Obsidian, game saves, and any valuable WSL state. Until selected, report backup as unconfigured rather than implying that GitHub is a complete backup.

## 21. What has and has not been verified for this document

- Reviewed both local inventories and all three CTT screenshots.
- Inspected Windows application/startup metadata and hardware without installing or removing software.
- Checked the listed Windows package identities against WinGet/Store metadata.
- Confirmed FedoraLinux-44 is offered by WSL in this environment.
- Consulted current primary documentation for WSL, Docker, editors, package managers, and the relevant app behavior.
- Supplied a PowerShell profile as a workspace file for review. Checked syntax, isolated loading, preserved navigation aliases, explicit WSL argument forwarding, and Explorer path construction without launching WSL.
- Did not install Fedora, Docker, applications, or the profile; did not modify Chezmoi or the existing Windows configuration.
- Fedora package resolution, complete installer integration, hardware workflows, and the full end-to-end setup remain implementation validation work.
