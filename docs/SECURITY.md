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

Every executable on the system has exactly one provider. Prefer the maker's own
channel over a repackage, and a signed repository over a script. In order:

| Tier | Source | Scope | How it enters |
| --- | --- | --- | --- |
| 1 | Fedora repositories | system | bare package name |
| 2 | Maker repository, COPR, or Flatpak | system | catalog entry; key pinned |
| 3 | Maker's installer script | user | manual task; never a resource |
| 4 | RPM Fusion, Terra, community builds | system | catalog entry; key pinned |
| 5 | Pinned external artifact | system | catalog entry; URL and SHA-256 |
| 6 | Container image | system | digest pinned in the component |

Tier 4 covers community Flathub builds and third-party COPRs as well.

Rules:

- Pick the highest tier the maker offers. Mise has the maker-owned COPR
  `jdxcode/mise`, so Nimbus installs it from there and DNF updates it. 1Password
  comes from AgileBits' repository, not the Terra repackage.
- Tier 3 exists because some makers ship only a script. Zed installs to
  `~/.local/zed.app` and updates itself. Such tools are user scope: the user
  runs the script, the tool owns its updates, Chezmoi owns its configuration,
  and Nimbus lists it as a typed manual task with a presence check. Nimbus and
  root never run a maker's script, and the script never touches the system
  layer. Nimbus's own `install.sh` is the one script run as bootstrap, trusted
  once from the approved `main` branch and never used for updates.
- A tier 3 tool that gains a maker repository or Flatpak moves up. A tier 4
  package that the maker adopts moves up the same way.
- A repository is enabled only through a catalog entry whose release package
  or signing key is pinned. `--nogpgcheck` never appears in a plan.
- Flatpaks are system scope and come from Flathub only. Per-application
  permission overrides are user configuration and belong to Chezmoi.
- Runtimes and tools installed through Mise, Cargo, npm, pipx, or Go run as the
  user and are the user's responsibility. Nimbus reports missing runtimes and
  never elevates for them.
- AppImages and manually downloaded binaries are not managed and not trusted.
- Container images are pinned by digest and never run privileged. The Windows
  guest needs `/dev/kvm`, `/dev/net/tun`, and `NET_ADMIN`, nothing more.
- Docker on Fedora is `moby-engine` from Fedora. Docker's own repository is a
  vendor repository under the rule above if it is ever needed.

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
  `/home`, guest disks, and container data need an off-disk backup.

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
- Nimbus itself makes no network calls except through DNF, Flatpak, and the
  one explicit Chezmoi initialization.

## Privilege

- The root account is disabled. One user in `wheel` runs `sudo` with a
  password. No `NOPASSWD` rules, no sudo keepalive, no root daemon.
- Nimbus runs as the user and refuses to run as root. Each privileged command
  is rendered in the plan and executed through `sudo` directly.
- The owner's account is in the `docker` group, declared as a group-membership
  resource. That membership is root-equivalent and is treated as such: it is
  the one standing privilege besides `sudo`, doctor reports any other member,
  and the guest's Compose definition stays root-owned so the user cannot alter
  what the daemon runs.
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

- System-layer updates from every tier go through `nimbus upgrade` or direct
  DNF and Flatpak; nothing in the system layer updates unattended.
- Tier 3 user-scope tools update themselves on the maker's schedule. That is
  accepted because they cannot reach the system layer.
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
