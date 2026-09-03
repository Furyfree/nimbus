# Nimbus security policy

This file owns the workstation's security policy: where software may come
from, how disks, boot, mandatory access control, network, privilege, and
secrets are handled, and what `nimbus doctor` checks. [SPEC.md](SPEC.md) owns
the engine's own safety invariants and cites this file. When the two disagree,
fix them together.

## Threat model

One owner, one user account, no hostile local users. The threats that matter:

- a lost or stolen laptop
- a malicious or compromised package, installer, repository, or mirror
- a privilege or policy weakened "to make something work" and never restored
- a mutation nobody reviewed

Out of scope: targeted firmware attacks, a compromised Fedora signing key, and
attackers with root on the running system.

## Software sources

Every executable on the system has exactly one provider. The order of
preference is fixed and the same for every package:

1. Fedora repositories, written as a bare package name.
2. The maker's own channel: a signed repository or COPR at system scope, or
   an installer script, Cargo crate, or Mise runtime at user scope.
3. Flathub, for a GUI application that needs no host integration.
4. Terra, RPM Fusion, or a community COPR, when nothing above offers the
   package.
5. A COPR the owner builds, when nothing offers the package at all: GitHub
   Desktop, WoWUp, and the Nimbus engine itself.

Container images pinned by digest are the one remaining case; the Windows
guest is the only one. Every repository, including the owner's COPRs and the
Flathub remote, is declared in `nimbus.toml` with its signing key pinned, and
every package reference names its repository as a prefix. `--nogpgcheck`
never appears in a plan.

Rules:

- The earlier source wins where two carry the same name. Fedora provides
  Chezmoi, Noctalia, Just, Tailscale, and Nix; RPM Fusion provides Steam. A
  later repository is declared with a lower DNF priority so it cannot shadow
  an earlier one, and planning refuses a package that DNF would take from a
  repository other than the one its prefix names.
- Maker repositories in use: Docker, 1Password, Brave for `brave-origin`,
  VSCodium, and OpenAI's ChatGPT repository. The ChatGPT RPM's own
  post-install script would add that repository; Nimbus declares it directly
  and installs `chatgpt` from it so DNF owns the updates.
- User-scope maker channels are the Mise, Zed, and Herdr installer scripts,
  `cargo install`, and `mise install`. Nimbus runs them as the normal user,
  never as root, as steps of the reviewed plan. A maker's script is fetched
  from its documented URL and cannot be pinned to a version, so the plan
  shows the URL and the digest of the fetched script. Mise installs Rust, so
  Cargo steps follow the Mise runtime step. The maker owns later updates:
  Mise's `auto_update = true`, Zed's own updater, and Topgrade for Herdr,
  Cargo, and Mise runtimes.
- Cargo counts as the maker's channel, so Sheldon, VM Curator, Typst,
  Tinymist, Caligula, `cargo-update`, and Yazi's `resvg` helper stay on
  Cargo although Terra packages some of them. Topgrade is the one deliberate
  exception: Nimbus installs Terra's `topgrade` so the tool that drives the
  user-scope update phase is not replaced by that phase.
- Flatpaks are system scope and come from Flathub only: Spotify and Obsidian.
  ProtonPlus, Heroic, Vesktop, gpu-screen-recorder, and Prism Launcher stay
  native RPMs from Terra because they integrate with Steam, Wine, Noctalia,
  or system Java. Per-application permission overrides are user
  configuration and belong to Chezmoi.
- Nix comes from Fedora's `nix` and `nix-daemon` packages with
  `nix-daemon.service` as a system resource. The upstream multi-user
  installer is not accepted while its documented Linux prerequisite is
  disabled SELinux. Chezmoi owns `~/.config/nix`.
- Tailscale comes from Fedora, which carries the current release. Its
  official `install.sh` reaches the system layer through sudo and is not
  used.
- Starship comes from Terra because its maker script installs to
  `/usr/local/bin` with sudo and has no self-update.
- VM Curator is a Cargo crate that drives QEMU directly without libvirt.
  Nimbus owns `qemu-system-x86`, `qemu-img`, `swtpm`, and `virt-viewer` as
  Fedora packages; `qemu-system-x86` supplies the display backends, OVMF, and
  `passt`, and `virt-viewer` gives SPICE clipboard sharing with a guest.
  Guest disks live below `~/vm-space` on the `home` subvolume, outside
  Nimbus.
- AppImages and manually downloaded binaries are not managed and not trusted.
- Container images are pinned by digest and never run privileged. The Windows
  guest needs `/dev/kvm`, `/dev/net/tun`, and `NET_ADMIN`, nothing more.
- Docker comes from Docker's own Fedora repository, declared in `nimbus.toml`
  with the signing key `060A 61C5 1B55 8A7F 742B 77AA C52F EB6B 621E 9F35`.
  The `docker` component, selected by `development` and required by
  `windows-vm`, installs `docker-ce`, `docker-ce-cli`, `containerd.io`,
  `docker-buildx-plugin`, and `docker-compose-plugin`, enables
  `docker.service`, sets `"selinux-enabled": true` in `/etc/docker/daemon.json`
  because Docker's packages do not, and conflicts with Fedora's `moby-engine`
  and with `podman-docker`, whose fake `docker` binary would shadow the real
  one.

## Disk encryption

- The root filesystem sits in LUKS2. `/boot` and `/boot/efi` are unencrypted,
  so kernels and the bootloader are readable and replaceable by anyone with the
  disk. Secure Boot limits what can be booted; it does not hide the files.
- A passphrase key slot always exists and is never removed by automation. The
  passphrase is stored in 1Password.
- TPM2 auto-unlock without a PIN is the accepted convenience, enrolled through
  `systemd-cryptenroll --tpm2-device=auto --tpm2-pcrs=7` as a later reviewed
  operation. It adds a slot and never replaces the passphrase slot. PCR 7 binds
  the key to the Secure Boot state, so a stolen disk in another machine or a
  boot with Secure Boot disabled falls back to the passphrase.
- The known gap: with GRUB and an unencrypted `/boot`, PCR 7 does not measure
  the kernel or initramfs, so a tampered initramfs signed by the same shim
  chain could still receive the key. Closing that needs unified kernel images
  with PCR 11 binding, which is later work; until then the auto-unlock protects
  against theft, not against an attacker who can rewrite `/boot` first.
- Firmware, Secure Boot, or bootloader changes invalidate the binding. Doctor
  reports a slot that no longer unlocks, and re-enrollment is a reviewed
  operation that removes the old TPM2 slot only after the new one is verified.
- Nimbus inspects LUKS2 and never creates, converts, resizes, or re-encrypts a
  live volume.
- Snapper recovery points live on the same encrypted disk and are not backups.
  Nimbus provides no backup facility. The workstation holds no canonical-only
  data: user files are synchronized or stored externally, and local guest and
  container data may be lost and recreated. Complete disk loss is therefore an
  accepted rebuild and resynchronization event, not a Nimbus restore path.

## Secure Boot

- Secure Boot stays enabled. Nimbus never disables it, prompts to disable it,
  or treats a disabled state as acceptable on the owner's hardware.
- Out-of-tree kernel modules such as NVIDIA use akmods signed with a local
  Machine Owner Key. The private key is root-only on the machine and never
  enters the repository, state, or receipts. Enrollment through `mokutil` is a
  typed manual task with a reboot, verification, and recovery guidance.
- systemd-boot and unified kernel images are later work and do not weaken the
  rules above.

## SELinux

- SELinux stays enforcing. Nimbus never sets permissive or disabled, and
  doctor reports either state as a failure.
- A denial is fixed with a typed resource shown in the plan: a boolean, a file
  context, or a port label. `setenforce 0`, `audit2allow` modules, and
  relabelling the world are not fixes.
- Bind mounts into containers carry the correct label. Guest and container
  data stay under their own labelled roots.

## Firewall and network

- Fedora ships firewalld; it is the firewall. `ufw` exists in Fedora but is not
  installed, so there is one ruleset and one owner.
- The default zone drops inbound traffic. A component opens a service or port
  as a typed resource, and removal closes it.
- Windows guest ports 8006 and 3389 bind to 127.0.0.1 only.
- `sshd` is not enabled unless a component declares it. When enabled, password
  authentication is off and keys come from the 1Password agent.
- Nimbus itself makes no network calls except through DNF, Flatpak, digest-
  pinned container image pulls, the user-scope maker channels named above,
  and the one explicit Chezmoi initialization.

## Privilege

- The root account is disabled. One user in `wheel` runs `sudo` with a
  password. No `NOPASSWD` rules, no sudo keepalive, no root daemon.
- Nimbus runs as the user and refuses to run as root. Each privileged command
  is rendered in the plan and executed through `sudo` directly.
- The owner's account is in the `docker` group, declared as a group-membership
  resource that the plan renders as `sudo usermod -aG docker <user>` and that
  takes effect at the next login. That membership is root-equivalent and is
  treated as such: it is the one standing privilege besides `sudo`, doctor
  reports any other member, removal of the `docker` component removes it, and the
  guest's Compose definition stays root-owned so the user cannot alter what
  the daemon runs.
- Polkit rules, sudoers drop-ins, and group memberships are typed resources
  with owned removal.

## Secrets

- Secrets live in 1Password. The SSH agent, `~/.ssh/config`, and
  secret-backed templates are Chezmoi's domain.
- The repository, plans, state, receipts, and logs contain no secrets. Doctor
  and validation never print a secret while checking for one.
- The single exception is the Windows guest `credentials.env`, owned by the
  user with mode `0600`, reported by presence only, and deleted by
  `windows purge-data`.

## Updates

- System-layer updates from every source go through `nimbus upgrade` or
  direct DNF and Flatpak; nothing in the system layer updates unattended.
- `nimbus upgrade` is the primary entry point. Nimbus plans and applies exact
  DNF and Flatpak transactions itself, surrounded by its recovery point. It
  then offers a separately approved Topgrade phase for the user-scope
  managers. Topgrade reads the user's own Chezmoi-owned configuration, and
  Nimbus passes `--disable` for the system, Flatpak, firmware, Nix, Chezmoi,
  Git-repository, and self-update steps on the command line so that phase
  cannot bypass the system plan or update source checkouts. Direct `topgrade`
  runs are the user's own, like direct DNF.
- Topgrade dry-run shows commands, not the downstream package versions selected
  by each manager. The plan therefore labels the user phase as command-level
  review, never as an exact or recoverable transaction. The root and Flatpak
  snapshots do not cover home-directory tools.
- User-scope tools may update themselves on the maker's schedule. Mise's
  accepted global setting is `auto_update = true`; it replaces only the
  user-owned Mise binary. Zed updates itself the same way. This is accepted
  because neither can reach the system layer.
- Firmware updates go through `fwupd` as a reviewed manual task.
- Fedora release upgrades are Fedora's native workflow, not Nimbus's.
- The Nimbus engine updates through DNF like any other package.

## What doctor checks

- Secure Boot enabled, SELinux enforcing, firewalld active with the expected
  default zone
- root LUKS2 present, passphrase slot present, TPM2 binding current when
  enrolled
- root account locked, `wheel` membership, no `NOPASSWD` rule, `docker` group
  containing only the declared user
- only declared repositories enabled, each with its pinned key
- no `sshd` unless declared, and no password authentication when declared
- `credentials.env` mode and owner when the Windows guest exists

Doctor reports; it never repairs.
