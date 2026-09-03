# Nimbus decisions

This file records architectural decision rationale and unresolved questions.
It does not replace the accepted contract in [SPEC.md](SPEC.md), phase order in
[ROADMAP.md](ROADMAP.md), or current work in [TASKS.md](TASKS.md).

When a question is resolved, move the accepted behavior into its owning
contract, record the answer here, and mark the question resolved. ROADMAP.md
references question IDs when they gate a phase.

## 2026-09-02 foundation passthrough

Three independent read-only passes used Claude Fable 5.1 High, Grok 4.6 High,
and GLM 5.3 Flash Max. The final adjudication also inspected the active files,
repository support files, archived drafts, and the relevant dotfiles contracts.
The agents made no repository changes.

The overall architecture is accepted: Nimbus owns system state, Chezmoi owns
user files, Mise owns user runtimes, live definitions come from the selected
Nimbus checkout, and desired, observed, and last-applied state remain separate.
The following decisions tighten the foundation before implementation.

### Accepted decisions

#### D-001: SPEC remains complete and phase-independent

SPEC.md must contain the accepted schema semantics, command behavior, ownership,
safety, and system invariants. It must not contain phase status, delivery order,
or temporary implementation shortcuts.

ROADMAP.md decides when the contract is delivered. TASKS.md records only the
current work and evidence.

#### D-002: Schema contracts precede the resolver implementation

Before the first Go types are frozen, SPEC.md must define the required and
optional semantics for:

- nimbus.toml compatibility metadata
- the local selector
- machines, profiles, components, and exceptional catalog entries
- package references, exclusions, and enforceable constraints
- resources and system-file declarations
- warnings, manual tasks, and runtime command groups
- source locations and selection provenance

The schema stays strict and versioned. Unknown fields remain errors. Fields are
added only when the accepted definitions or validation fixtures exercise them.

#### D-003: Static validation and observed-system validation are separate

`nimbus validate` and configuration resolution inspect only the selected
checkout. They may reject structural errors, but they cannot claim to validate
facts that require DNF, RPM, hardware, filesystem, or operating-system
inspection.

Provider availability, installed dependency safety, protected-package state,
and native transaction behavior belong to later facts and planning validation.

#### D-004: Nimbus never disables Secure Boot

The rule is absolute for Nimbus operations, whether automatic, prompted, or
otherwise explicit. Secure Boot key and MOK enrollment may be represented only
as reviewed manual work with verification and recovery guidance.

Amended 2026-09-03 after the passthrough. The target is a locally built
unified kernel image signed with the same machine owner key, with the TPM2
slot rebound to a signed PCR 11 policy, so the kernel and initramfs are
measured and boot stays hands-free with one login password. Fedora 44 ships
a signed unified kernel image only for virtual machines, so bare metal builds
its own with `systemd-ukify`. Until that is delivered and drilled, PCR 7
unlock stays, described as protection against offline reading and disk
removal rather than against an attacker who rewrites `/boot`. Rejected: a
TPM PIN, because the owner wants hands-free unlock.

Amended 2026-09-02. The owner reconsidered running with Secure Boot off and
kept it on. The deciding fact: the hands-free TPM2 unlock in SECURITY.md binds
to PCR 7, which is a machine constant when Secure Boot is disabled, so any live
USB on the same hardware would receive the LUKS key. Rejected: Secure Boot off
with PCR 7, because it leaves the disk effectively unencrypted against a thief
holding the laptop; Secure Boot off with PCR 4, because every shim or GRUB
update forces a passphrase boot and re-enrollment; TPM2 with PIN or passphrase
only, because the owner wants hands-free unlock. The one accepted cost is the
single MOK enrollment for the NVIDIA akmods on the desktop.

#### D-005: The catalog remains an exception list

Bare Fedora package names use the default typed DNF lifecycle. Catalog entries
exist only for non-default providers, repositories, coordinated update groups,
special verification or removal, pinned external artifacts, and other explicit
exceptions.

The package-query container produces research evidence. Its results do not
automatically become catalog entries or desired state.

Superseded 2026-09-03 by D-016. There is no catalog. A package reference
names its repository as a prefix, and the repository is declared once in
`nimbus.toml`.

#### D-006: Superseded evidence remains available through Git history

The legacy `history/` tree was removed from the active checkout after the
foundation documents were consolidated. Its last complete snapshot is commit
`c0bb8a4660732e6e9556297da2c15ba0f286ee98`.

Archived wording may contradict the current design. Inspect it from that commit
without restoring it to the active tree, and never copy it without revalidation
against the active documents. New accepted rationale belongs in this file;
supporting research enters the active tree only when a current task needs it.

#### D-007: The dotfiles reconciliation remains a bootstrap gate (resolved)

The dotfiles repository still contains obsolete Nimbus machine manifests, the
old symlink-based selector contract, and references to the archived Nimbus
CLI draft. This does not block the isolated read-only resolver, but bootstrap and
Chezmoi handoff cannot be accepted until the dotfiles repository uses the same
ownership and selector model as Nimbus.

Resolved 2026-09-02 for the profile handoff, which the dotfiles repository
adopted and documented with a citation of `docs/SPEC.md`.

Corrected 2026-09-03 after the passthrough: the dotfiles repository still
holds `machines/desktop.toml`, `machines/laptop.toml`, and a README that
describes the old symlink selector at `~/.config/nimbus/machine.toml`, and
its template lacks the `Machine` and `ManagedByNimbus` keys. Those removals
and additions are the dotfiles task in TASKS.md, and Phase 5 verifies the
prompt flags against the real template before exit.

#### D-008: Low-value cleanup is not part of the correction pass

The foundation pass does not need to remove the existing Dependabot entry,
rewrite the stock Go gitignore, add a README license link, delete additional
history, or make the local and raw package-query commands identical. Those
items do not affect the product contract or the current phase gate.

#### D-009: The profile vocabulary follows the owner's machines

`laptop-gaming` joins the vocabulary on 2026-09-02 as the light gaming profile
for the laptop, starting with PrismLauncher and small helpers, while `gaming`
stays the complete desktop stack. A subset profile keeps the laptop manifest
sparse instead of carrying an ad hoc package list.

PrismLauncher is not in the Fedora or RPM Fusion repositories. The
package-query container, now including Terra, shows `prismlauncher-11.0.3` in
Terra 44 recommending Fedora's `java-25-openjdk`. It enters as
`terra:prismlauncher`, written under D-016 against the Terra repository
declared in `nimbus.toml` with its signing key pinned. Rejected: the Flathub
Flatpak, kept as the fallback if Terra trust cannot be represented, because
the owner prefers native RPMs with system Java; and a PrismLauncher COPR,
because Terra already carries the package with a maintained signing key.

Amended 2026-09-02 after the coherence pass. `virtualization` joins the
vocabulary for the owner's Linux and other guests: Fedora's QEMU/KVM host
packages plus VM Curator, a QEMU front end that needs no libvirt and keeps
disks below `~/vm-space`, so no subvolume returns and guests stay outside
Nimbus. VM Curator is installed by the user with `cargo install`, user scope,
because neither Fedora nor Terra packages it and the owner already uses Cargo
for tools such as `just`; the maker's release RPM as a pinned artifact was
rejected as a manual pin for a fast-moving tool. Nix remains Fedora's `nix`
package, tier 1. The requested upstream multi-user installer is incompatible
with the accepted security policy while its own Linux instructions require
SELinux to be disabled. The Windows guest stays the one VM Nimbus owns.

#### D-010: SECURITY.md owns the workstation security policy

Added 2026-09-02. Software-source trust tiers, encryption, Secure Boot,
SELinux, firewall, privilege, secrets, and doctor checks moved into one policy
file so SPEC.md keeps only the engine invariants. Terra and RPM Fusion are
accepted sources with pinned keys; `curl | sh` installers are refused because
Terra and Fedora ship the tools that use them; firewalld is the firewall on
Fedora rather than ufw; the `docker` group is not granted because it is
root-equivalent. Rejected: keeping the policy as bullets inside SPEC.md,
because it was already too thin to guide the Terra and Windows decisions.

Amended 2026-09-03 after checking the makers' current instructions. Sources are
ranked by who publishes them and by mutation scope: Fedora, then the maker's own
repository, COPR, or Flatpak, then a maker installer whose complete mutation
boundary is user scope, then community repackages such as RPM Fusion, Terra,
and third-party COPR, then pinned artifacts. Mise joins Zed as a user-owned
maker install because Mise recommends its optimized `mise.run` binary on Linux
and it writes to `~/.local/bin`; its self-update cannot mutate the system layer.
Herdr joins them on 2026-09-03: its script installs only to `~/.local/bin`
without sudo, and its `herdr update` command runs from the Topgrade
configuration in Chezmoi. Rejected: letting Nimbus or root run any of the
scripts.

Tailscale is not the same exception. Its official script invokes sudo, adds the
maker repository, installs an RPM, and enables a system service. Nimbus models
those documented Fedora operations directly instead of executing a mutable
root script. The requested upstream Nix multi-user installer is also rejected
for now because its own Linux prerequisites say SELinux must be disabled;
Fedora's `nix` package remains compatible with the accepted enforcing policy.

Superseded 2026-09-03 by D-015 and D-017. The six tiers are replaced by one
fixed order, Tailscale comes from Fedora 44, which carries it, and Nimbus
itself runs the user-scope maker scripts as the user.

Amended 2026-09-02 after the owner's privilege and encryption calls. The owner
joins the `docker` group as a declared group-membership resource; the policy
names it root-equivalent and doctor reports any undeclared member. Rejected:
`sudo docker` for every call, because daily Docker use without sudo is the
point of the group. TPM2 auto-unlock is enrolled without a PIN, bound to PCR 7,
alongside the permanent passphrase slot. Rejected: requiring a PIN, because the
owner wants hands-free unlock and accepts that theft, not `/boot` tampering, is
the protected case until unified kernel images add PCR 11.

#### D-011: Docker comes from Docker's Fedora repository

Decided 2026-09-02. The `docker` component uses Docker's own repository for
Fedora with its signing key pinned, the five `docker-ce` packages, and an
explicit `"selinux-enabled": true` in `daemon.json`. This is the procedure in
Docker's Fedora documentation and in Chris Titus's linutil, verified to be the
same repository, key, and packages. Rejected: Fedora's `moby-engine`,
`docker-cli`, `docker-compose`, and `docker-buildx`, although they are tier 1,
current at 29.7.2, and pull `container-selinux` as a dependency, because the
owner prefers the channel that every Docker document and tutorial assumes and
has lagged Fedora less often than the reverse. Both `moby-engine` and
`podman-docker` are conflicts of the component. The Fedora packages remain the
documented fallback if Docker's repository is ever late for a Fedora release.

#### D-012: Hardware is detected once and recorded as components

Decided 2026-09-03. Hardware is not a user-facing profile. During new-machine
initialization, Nimbus detects DMI identity and PCI devices, proposes the known
hardware components, and writes the accepted IDs into the reviewed machine
manifest. All later resolution remains configuration-only. The first exact
targets are the MSI Z690 desktop with Intel integrated graphics and NVIDIA RTX
3080, and the HP EliteBook X G1a with AMD integrated graphics. The desktop
component selects `ddcutil`; the laptop component selects `brightnessctl`.
Unknown or ambiguous facts open the component picker instead of guessing.

Rejected: runtime package selection from hardware on every apply, because it
would make desired state depend on observations and could silently change after
a device or firmware update; and hardware profiles, because hardware is a
technical component rather than user intent shared with Chezmoi.

#### D-013: Topgrade aggregates only the user-owned upgrade phase

Decided 2026-09-03. `nimbus upgrade` is the primary workstation update entry
point. Nimbus retains exact DNF and Flatpak planning and surrounds the workflow
with its recovery point. After system verification it offers a separately
approved Topgrade run restricted to declared user managers such as Mise and
Cargo. System, Flatpak, firmware, Nix, Chezmoi, Git repository, and Topgrade
self-update steps are disabled in the Nimbus-owned Topgrade configuration.

Amended 2026-09-03 by D-018. Chezmoi owns the Topgrade configuration; Nimbus
installs Topgrade and disables those steps on the command line instead.

Topgrade dry-run prints the commands it would invoke but does not resolve their
downstream versions. The user phase is therefore explicitly command-level and
not represented as an exact Nimbus transaction. Root and Flatpak snapshots do
not cover tools below home. Rejected: making unrestricted Topgrade the system
updater, because that would bypass the reviewed system transaction and allow
mutations absent from Nimbus's plan.

#### D-014: Package-source refinements follow the selected lifecycles

Decided 2026-09-03 from Fedora 44 package-query evidence. Hyprland,
`hyprland-guiutils`, and `xdg-desktop-portal-hyprland` come from the
`lionheartp/Hyprland` COPR, the fork Hyprland's own documentation recommends
now that `solopasha/hyprland` is unmaintained; Fedora 44 and Terra carry no
Hyprland package. Noctalia Greeter uses Terra's stable
`noctalia-greeter` package rather than the COPR's continuously rebuilt
`noctalia-greeter-git`; this keeps the session's greeter on a release lifecycle
while retaining the selected Hyprland source.

Terra supplies `yazi`, `umu-launcher`, and `topgrade`. Fedora supplies
`gamescope`, `mangohud`, `goverlay`, `gamemode`, `protontricks`, `winetricks`,
`wireguard-tools`, and `freerdp`. Yazi's required `file` and its selected
preview helpers are Fedora packages. Typst uses the `typst-cli` crate and
Tinymist the `tinymist` crate under the existing user-owned Cargo lifecycle.
`gopls` and `golangci-lint` remain Mise-owned.

Amended 2026-09-03 after the Q-017 inventory review. Tools the owner updates
through `cargo-update` stay on Cargo even when Terra packages them: Topgrade,
Sheldon, and Yazi's `resvg` preview helper. Fedora still wins over Cargo, so
Just moves to Fedora's `just`. Tailscale and Nix with `nix-daemon` come from
Fedora rather than their maker scripts, which D-010 rejects; Starship comes
from Terra because its script installs to `/usr/local/bin` with sudo and has
no self-update; `unrar` comes from RPM Fusion because Fedora's package is the
`unrar-free` wrapper. Where Fedora and Terra both carry a package, Noctalia
and Chezmoi, Fedora is the provider and Terra must not shadow it.

WoWUp remains desired but unavailable from the accepted Fedora 44 sources. It
may enter through a separately reviewed Nimbus-owned COPR package when that
package exists; its upstream AppImage remains rejected by the standing policy.

Amended 2026-09-03 by D-015. Topgrade moves to Terra as the one exception to
the Cargo rule, `librepods` moves to Terra, which now carries it, and Fedora's
lazygit package is `golang-github-jesseduffield-lazygit`; nothing provides
the bare name.

#### D-015: One fixed source order replaces the tiers

Decided 2026-09-03. The owner found the tier table inconsistent from package
to package. SECURITY.md now states one order applied to every package: Fedora;
the maker's own channel, whether repository, COPR, installer script, Cargo
crate, or Mise runtime; Flathub for GUI applications without host
integration; Terra, RPM Fusion, or a community COPR when nothing earlier
offers the package; and a COPR the owner builds when nothing offers it at all,
which is GitHub Desktop, WoWUp, and the Nimbus engine. Where two sources
carry a name, the earlier one wins and a DNF priority stops the later one from
shadowing it.

Package decisions taken under that order the same day: Tailscale from Fedora
44, which carries 1.98; ChatGPT Desktop from OpenAI's DNF repository, which
the downloaded RPM's post-install script registers with key
`3BFA 0E4A E8B8 CC16 A2D9 BA68 4A3B 4A56 6C46 60E4`; Brave Origin as
`brave-origin` from Brave's repository, verified present; VSCodium from its
maker's RPM repository; Signal from Terra, because neither the Flathub nor the
Terra build is the maker's and the owner prefers native RPMs where an
accepted repository has one, Flathub being permitted rather than preferred;
`librepods` from Terra; ProtonPlus, Heroic, Vesktop, gpu-screen-recorder, and
Prism Launcher as Terra RPMs rather than their makers' Flatpaks, because they
integrate with Steam, Wine, Noctalia, or system Java; Rust through Mise;
`virt-viewer` always, because VM Curator offers SPICE clipboard sharing
through it. Rejected: applying the tier table as written, which would have
moved five applications to Flathub.

#### D-016: Repositories are declared once and named by prefix

Decided 2026-09-03. `nimbus.toml` declares every repository other than Fedora
with its kind, location, pinned key, and DNF priority. A package reference
uses the repository ID as its prefix: `terra:ghostty`,
`rpmfusion-nonfree:steam`, `docker:docker-ce`, `flatpak:com.spotify.Client`.
Bare names stay Fedora. A repository is desired while any selected package
names it and is removed when none does. A component may list `removes` so a
swap such as RPM Fusion's `ffmpeg` over `ffmpeg-free` is typed data. The
`catalog/` directory and the `catalog:` form are removed.

Profiles list packages directly; a component exists only for a bundle two
profiles share or a capability that owns services or files. Rejected: one
catalog file per non-Fedora package, because it repeated every package name
in a second place and made Docker's one repository and five packages
ambiguous.

#### D-017: Nimbus runs the user-scope installs as the user

Decided 2026-09-03. Nimbus runs the Mise, Zed, and Herdr installer scripts,
`cargo install`, and `mise install` as the normal user, without sudo, as
reviewed plan steps. Mise is installed before the Chezmoi handoff; runtimes
and Cargo tools follow it because they depend on the Chezmoi-written Mise
configuration. Chezmoi keeps the configuration files and the makers keep
their own updates. This replaces the rule that Nimbus never runs a maker's
script and the Q-007 Chezmoi onchange action.

The cost is accepted: a maker's script cannot be pinned, so the plan shows its
URL and the digest of the fetched script rather than a version. Nothing below
home has a recovery point, which the no-backup decision already accepts.
Rejected: typed manual tasks the user runs by hand, because the owner wants
one installer that finishes the machine.

Amended 2026-09-03 after the passthrough. A pipeline cannot be hashed before
it runs, so Nimbus downloads the script to a file, shows the digest, and runs
that file. `mise settings set auto_update true` writes the Chezmoi-owned Mise
config, so the setting lives in the Chezmoi template and Nimbus never runs
that command. User-scope steps write no receipt and verify by presence;
removal is explicit through `cargo uninstall`, `mise implode`, and
`zed --uninstall`, shown in the plan and never triggered by profile removal.

#### D-018: Topgrade is Nimbus-installed with a Chezmoi-owned configuration

Decided 2026-09-03. Nimbus installs Terra's `topgrade`. The configuration is
a user file and belongs to Chezmoi, where the `herdr update` custom command
also lives. `nimbus upgrade` keeps its Topgrade phase and may refresh
metadata around it, but enforces the exclusion on the command line with
`--disable` for the system, Flatpak, firmware, Nix, Chezmoi, Git-repository,
and self-update steps. Rejected: a second Nimbus-owned configuration, because
two files would drift; dropping the phase, because the owner wants one update
command.

Amended 2026-09-03 after the passthrough. Topgrade 17.9 exposes more than
150 steps, so a `--disable` blocklist would enable any step a later release
adds. Nimbus passes `--only` with the declared allowlist plus
`--no-self-update` instead. The root post snapshot closes as soon as the
system phase is verified, before this phase starts.

#### D-019: Passthrough corrections before code

Decided 2026-09-03 after the three-pass repository review recorded in
TASKS.md. Beyond the amendments above, the review settled four gaps in the
Phase 1 contract. Profile and component files now have a written shape with
examples in SPEC.md, and a `[[files]]` entry carries source, owner, group,
and mode. A `package_exclusions` entry may name only a package a selected
profile or component installs on that machine, never a required package, and
one that matches nothing is an error. `package_constraints` leaves the Phase
1 schema until Phase 7 proves DNF5 version locking. Repository declarations
carry `key_url` as well as the fingerprint, and a repository is removed only
when no selected package names it and no installed package still comes from
it.

Rejected: dropping `launch browser` and `launch webapp` as scope that owns no
system state, because the owner wants the browser detection and argument
building typed and tested in one binary rather than kept in sync across
Chezmoi scripts; Q-005 stands.

#### D-020: Planning reads the cache, apply replays the plan

Decided 2026-09-03 before Phase 3. `plan` previews every DNF transaction from
the local metadata cache with `dnf5 --assumeno --cacheonly`, so plan and apply
read the same package lists and apply can refuse a changed transaction; `nimbus
refresh` runs `dnf5 makecache` as the one opt-in network step, its own command
so planning paths stay free of network access, and `upgrade` refreshes by
design. DNF5's `--store` downloads packages while recording the transaction, so
it is the Phase 4 apply mechanism, download then `dnf5 replay`, not a preview.
Rejected: planning online by default, because two consecutive resolutions could
then disagree through no fault of the owner.

Repositories Nimbus enables from a `baseurl` live in
`/etc/yum.repos.d/nimbus-<id>.repo`, so ownership is visible by name; a
maker's release package keeps the maker's file and IDs, and RPM Fusion's
key, which ships inside a release RPM signed by that same key, is extracted
from the SHA-256-verified RPM with `rpm2archive` and imported before the
package is installed. Terra stays a `baseurl` repository because its own
installer uses that URL. A foreign file that already provides a declared
repository blocks the operation instead of being duplicated or taken over.
Rejected: writing every repository file ourselves, because a maker-updated
release package tracks mirror moves that a hand-written file would miss;
and `--nogpgcheck` for the release RPM, because the policy forbids it and
extraction costs two read-only steps.

### Resolved questions

#### Q-001: Package reference grammar and catalog precedence (resolved)

Resolved 2026-09-02.

Bare names always mean Fedora packages and canonicalize to `dnf:<name>`.
Explicit `dnf:`, `flatpak:`, and `catalog:` forms are accepted. Catalog entries
are selected only through `catalog:<id>` and never shadow bare names. Unknown or
empty prefixes and invalid provider-native identifiers are errors.

Canonical identity is the provider-qualified native package target. Duplicate
selection paths merge provenance only when their technical lifecycle agrees;
otherwise validation fails. Constraint keys use canonical provider-qualified
identities and attach after selection resolution. SPEC.md owns the normative
grammar.

Amended 2026-09-03 by D-016. The `catalog:` form is gone. Any prefix other
than `dnf` and `flatpak` is a repository ID declared in `nimbus.toml`, and an
undeclared prefix is an error.

#### Q-002: Selector trust and checkout origin (resolved)

Resolved 2026-09-02.

The local selector records the normalized Git origin that the user approved for
the selected checkout. The origin identifies the repository, not a commit or
definition version. Equivalent supported SSH and HTTPS locators normalize to
the same repository identity.

Nimbus compares the recorded identity with the checkout's local Git
configuration without command execution or network access. A normal commit or
working-tree change does not change repository trust. A missing or different
origin fails selector-based loading until an explicit reviewed trust action
updates the selector. Nimbus never changes the checkout remote itself.

The selector is a regular Nimbus-owned local file, not a symlink or a
Chezmoi-managed file. Its machine ID selects the tracked manifest under
`machines/` in the checkout. SPEC.md owns the normative selector contract.

Amended 2026-09-02 after the foundation review. The origin check is kept.
Rejected: dropping `origin` and trusting the checkout path alone, because the
selector then could not detect a checkout swapped for a different repository;
and running `git config` to read the origin, because selector-based loading
must not execute commands. Reading `remote.origin.url` from `.git/config`
requires handling worktree `.git` files and `include` directives; `insteadOf`
rewrites are ignored because they change transport, not identity.

Amended 2026-09-03 after the passthrough. Nimbus reads `.git/config` and
follows a worktree `.git` file's `gitdir` pointer, nothing more; an `include`
or `includeIf` directive is an error that tells the user to set
`remote.origin.url` directly. Rejected: reimplementing Git's include
handling for one value, and running `git config`, which Q-002 already
excluded.

#### Q-003: Definition digest boundary and mode normalization (resolved)

Resolved 2026-09-02.

The canonical definition digest covers `nimbus.toml` and every regular file
recursively below `machines/`, `profiles/`, `components/`, `catalog/`, and
`system/`. It excludes engine code, repository metadata, documentation,
history, and development tooling. The complete definition tree participates,
not only files selected by one machine.

The versioned SHA-256 input uses each byte-sorted checkout-relative path,
normalized regular-file mode, and exact content with unambiguous framing. Mode
is normalized to Git's meaningful executable state, `100644` or `100755`;
other permission bits and directory modes do not participate. Desired target
permissions remain explicit resource data and therefore participate through
the declaring file's content.

The configured checkout root may be a symlink. Nimbus resolves it to one
canonical directory before origin checking, containment validation, loading,
and hashing. Symlinks or special files within the definition boundary are
rejected. Re-resolution and trust, commit, and digest checks prevent changed
effective input from reusing an earlier plan. SPEC.md owns the normative digest
contract.

Amended 2026-09-03 by D-016: the `catalog/` directory is gone, so the digest
covers `nimbus.toml` plus `machines/`, `profiles/`, `components/`, and
`system/`. The executable rule is Git's: the owner execute bit alone decides
`100755`.

#### Q-004: Supported system-file target roots (resolved)

Resolved 2026-09-02.

The generic system-file provider manages regular files below `/etc` only. A
source below `system/root/etc/` maps directly to the corresponding absolute
target below `/etc`; definitions cannot choose an independent target. This
keeps path containment structural and avoids a growing allowlist of individual
configuration directories.

Files below `/usr` belong in a native package or a separately specified typed
integration with exact targets. The same separation applies to boot resources,
Nimbus state, user files, runtime paths, and other target roots. In particular,
the planned recovery-session files below `/usr` do not widen the generic
system-file provider.

Static validation rejects invalid source mappings, traversal, symlinks, and
special files in the definition tree. Later system inspection and planning
must also reject target-path symlinks and files owned by another provider or
with unknown ownership. Taking over such a file requires an explicit typed
migration. Every accepted change retains its full diff, atomic install,
verification, receipt, removal behavior, and recovery contract. SPEC.md owns
the normative system-file contract.

Amended 2026-09-02. Intentional content drift in an already Nimbus-owned
generic `/etc` file may be accepted back into its existing
`system/root/etc` source through the narrow `files accept` workflow. It never
captures foreign files, metadata, multiple targets, unreadable content, or
other target roots. The reverse operation changes only the checkout; normal
validation, plan, apply, verification, receipt, and user-owned Git steps remain
separate.

#### Q-005: Complete command semantics and phase ownership (resolved)

Resolved 2026-09-02.

The public CLI stays task-oriented. Configuration resolution and fact
collection are internal engine capabilities, not public `config resolve` or
`facts` commands. `nimbus validate` is the single configuration check: it
validates the complete checkout and resolves every tracked machine without
system inspection. `nimbus doctor` always explains detected health problems
and has no separate `--explain` mode.

The normal lifecycle is `status`, `plan`, and `apply`. `plan` is read-only,
writes no plan file, and accepts no prune or upgrade mode. It shows normal
apply actions, prune candidates, and known upgrade information as separate
sections. Plain `apply` never prunes or upgrades. `apply --prune` shows and
requires approval for an expanded plan. Normal managed-resource updates use a
separate `upgrade` command; Fedora release upgrades remain outside it and are
delegated by Q-008.

Amended 2026-09-03 after Phase 3. `plan` gains `--prune` and mirrors apply
exactly: plain `plan` shows what `apply` would run, `plan --prune` adds the
unmanaged packages `apply --prune` would remove. Rejected: always showing
prune candidates in `plan`, because a plan then listed removals no apply
would perform.

The package workflow intentionally matches the owner's existing `npi`, `npr`,
and `npl` habits through `packages install [QUERY]`, `packages remove [QUERY]`,
and read-only `packages installed [QUERY]`. Install and remove update the
selected machine manifest, show its diff and the system plan, and apply only
after approval. They reuse normal resolution, ownership, planning, and apply;
Nimbus never commits the resulting Git change. Unmanaged packages are not
removed through this shortcut. Top-level `managed`, `unmanaged`, and `why`
remain provider-independent views for every resource type.

Amended 2026-09-02 after the system-file drift review. `files accept
/etc/PATH` provides one explicit live-to-checkout content operation for a
selected, receipt-owned generic system file. The name states the direction and
avoids a general `sync` command. Accidental drift still uses `apply`; accepted
drift leaves an uncommitted checkout change and returns to validate, plan, and
apply.

Amended 2026-09-02 after Q-009 and Q-010. The later command surface also
includes `postinstall` for the selected machine's typed pending work,
`windows` for the one supported guest lifecycle, and `launch browser` and
`launch webapp` as narrow desktop helpers. Post-install presentation delegates
to the same typed component actions and never becomes an arbitrary script
runner. Certificates, gaming packages, and virtualization host resources
remain normal plan and apply resources; external application-data repositories
such as the existing WoW setup remain outside Nimbus.

`version` is a configuration-independent Phase 1 command. `doctor` begins with
the read-only system inspector, package ownership views arrive with planning,
package mutation arrives with controlled DNF apply, and `upgrade` arrives with
the update and recovery phase. `--json` is a machine-facing renderer, while
checkout and machine overrides are invocation-local inputs for commands that
load desired configuration; none rewrites the selector. Commands appear in
help only in the phase that implements them. Before the later dashboard is
delivered, bare `nimbus` prints grouped help rather than exposing stubs.

SPEC.md owns the normative command behavior and ROADMAP.md owns delivery phase.

Amended 2026-09-02 after the foundation review. `launch browser` and
`launch webapp` stay in Nimbus. Rejected: moving them to Chezmoi-owned scripts,
because browser discovery, private-mode translation, URL validation, and exact
argv construction are the part that must be tested and typed; Chezmoi owns only
the keybindings and desktop entries that call them. `windows connect` gains
`--keep-alive` per Q-010.

Amended 2026-09-02 after the Windows walkthrough. Manifest selection gets the
same treatment as packages: `profiles list|add|remove` and
`components list|add|remove` edit the selected manifest, show the diff and
plan, apply after approval, and leave the Git change. Rejected: hand-editing
the manifest as the only path, because every other desired-state change has a
guided command; and one generic manifest editor, because typed lists give
validation, pickers, and owned removals. Because the Chezmoi handoff runs once,
a profile change prints the direct `chezmoi init` command and doctor reports a
stale Chezmoi selection.

#### Q-006: Bootstrap distribution and actor (resolved)

Resolved 2026-09-02.

The supported entry point is:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash
~~~

This explicitly trusts the current `install.sh` on the approved Nimbus `main`
branch. The remote script stays minimal: it verifies the supported platform and
normal-user context, obtains Git through DNF when necessary, clones or validates
the approved Nimbus checkout at `~/.local/share/nimbus`, and invokes the
checkout's versioned `bootstrap` script. It never replaces or updates an
existing checkout. A correct existing checkout, including an accepted symlinked
root, is reused without fetch, pull, or reset; every other existing target is an
error.

Because the outer Bash process reads `install.sh` from a pipe, it invokes the
checked-out script with its input attached specifically to `/dev/tty`. The
installation requires a controlling terminal and fails before mutation when
one is unavailable. The checked-out bootstrap shows and enables the approved
COPR, installs the compatible Nimbus RPM, and runs `nimbus init` with the
checkout path. It does not install or initialize Chezmoi directly; the reviewed
first Nimbus apply installs Chezmoi before init performs the one permitted
handoff.

The scripts may be rerun but never reconcile Git state or duplicate normal
provisioning. DNF owns later Nimbus-engine updates and removal, the user owns
checkout updates through normal Git, and Nimbus has no engine or checkout
self-update lifecycle. SPEC.md owns the normative bootstrap contract.

Amended 2026-09-02 after the foundation review. COPR delivery is kept and the
RPM packaging moves to a separate repository; a GitHub release may provide the
artifact COPR consumes. Rejected: building the engine from the definitions
checkout during bootstrap, which would remove the compatibility metadata but
execute code from a mutable checkout and give the engine no DNF-owned
lifecycle. The engine and checkout change independently, so `nimbus.toml`
compatibility metadata stays and is verified before any operation.

#### Q-007: Mise installation handoff (resolved)

Resolved 2026-09-02.

Amended 2026-09-03. Before Chezmoi initialization, the development profile
presents the official `curl https://mise.run | sh` installer and
`mise settings set auto_update true` as a user-run manual task. Nimbus verifies
the resulting user-owned binary but never runs the installer or installs Mise
as a system resource.

Chezmoi then owns a development-profile-gated
`run_onchange_after_install-mise-runtimes.sh.tmpl` action. It runs as the normal
user after `~/.config/mise/config.toml` is applied and includes the rendered
configuration's checksum in its own rendered content. Chezmoi therefore shows
the action during review and reruns it only when the effective Mise
configuration changes. The action invokes:

~~~sh
MISE_SYSTEM_DEPS=warn mise -C "$HOME" install
~~~

The warning policy prevents Mise from taking over privileged system-package
installation. The user owns the Mise binary, Nimbus installs the selected
system dependencies, Chezmoi manages the configuration and action, and Mise
installs the declared user-scope runtimes.

Users who want a separate runtime preview can apply files without scripts, run
the same Mise operation with `--dry-run`, and then apply scripts. If a runtime
is deleted without a configuration change, the onchange action does not rerun.
Nimbus reports the missing runtime and the direct Mise command but never runs
the repair itself. SPEC.md owns the normative handoff.

Amended 2026-09-03 by D-017. Nimbus runs the Mise installer as the user
before the handoff and `mise install` after it; the Chezmoi onchange action
is dropped and a missing runtime is reinstalled by the next apply.

#### Q-008: Fedora release upgrades (resolved)

Resolved 2026-09-02.

Fedora release upgrades are permanently delegated to Fedora's native DNF5
system-upgrade workflow. Nimbus does not expose a release-upgrade command,
accept a target release, invoke `dnf5 system-upgrade`, prepare its offline
transaction, reboot into it, record it as a Nimbus operation, or promise its
recovery. `nimbus upgrade` remains limited to normal updates within the
currently installed Fedora release.

The checkout compatibility metadata declares the Fedora releases it supports.
Nimbus doctor reports the installed and supported releases without claiming to
preflight a native release-upgrade transaction, and Nimbus refuses mutation on
an unsupported installed release. The normal checkout minimum-engine contract
still applies, but there is no special Nimbus version that makes release
upgrades Nimbus-owned.

Before using Fedora's tooling, the user ensures that the installed engine and
selected checkout can manage the target release. After Fedora completes the
upgrade, the user runs Nimbus validation and inspection to expose definition or
system drift before approving any Nimbus repair. Fedora's documentation,
transaction log, and recovery procedures remain authoritative for the release
upgrade itself. SPEC.md owns the normative boundary.

#### Q-009: Recovery layout, retention, and operation lock (resolved)

Resolved 2026-09-02.

The first recovery implementation supports the reviewed Fedora layout only:
UEFI/GPT with separate FAT32 `/boot/efi` and ext4 `/boot`, then LUKS2 containing
the `fedora` Btrfs filesystem. Its subvolumes are `root`, `home`, `snapshots`,
`log`, `cache`, `swapfile`, `flatpak`, `libvirt`, `docker`, and `containerd` at
the mountpoints specified by SPEC.md. Nimbus never converts a live layout.

A recovery point under `/.snapshots/nimbus/<id>/` contains a read-only `root`
snapshot, a system-Flatpak snapshot only when that provider changes, boot and
EFI archives only when affected, a checksummed manifest, and a standalone
restore guide. Home, logs, caches, swap, VM disks, and container data are not
snapshotted. This intentionally narrows the archived installation draft:
Nimbus does not own user data, and the machine manifest lives in the Nimbus
checkout rather than the Chezmoi repository.

Nimbus retains the newest three complete recovery points. A point associated
with an unresolved failed operation remains protected. Before creation Nimbus
removes only older eligible points, waits for Btrfs deletion, and rechecks
Btrfs-aware usable space. Less than 20 GiB then blocks the mutation. A partial
point never authorizes mutation, and recovery stays disabled until a complete
manual restore drill succeeds in a disposable Fedora VM.

Restore boots a Fedora live or rescue environment, unlocks LUKS2, mounts the
Btrfs top level and snapshots subvolume, verifies the selected manifest and
checksums, preserves the failed root, creates a writable `root` from the
read-only snapshot, and restores matching boot archives when present. It never
restores home automatically. The generated guide records discovered UUIDs and
paths so recovery does not depend on a working Nimbus installation.

Mutating commands use the normal user's kernel-held lock at
`$XDG_RUNTIME_DIR/nimbus/operation.lock`. The containing directory is mode
`0700`, the file is mode `0600`, and its diagnostic content identifies the
operation and process. A missing valid runtime directory blocks mutation.
Read-only commands remain concurrent, and stale text is never treated as a
held lock or removed merely because of age. SPEC.md owns the normative
recovery and concurrency contract.

Amended 2026-09-02 after the foundation review. The snapshot mechanism changes;
every requirement above stands. Snapper creates and deletes the `root` and
`flatpak` snapshots as pre and post pairs, while Nimbus owns the Snapper
configurations, boot and EFI archives, manifest, restore guide, retention, and
the 20 GiB preflight. Rejected: a Nimbus-implemented `btrfs subvolume`
manager, because Snapper already owns that lifecycle on Fedora and principle 3
forbids weak replacements. `snapper rollback` is not the restore path because
it assumes a different default-subvolume layout. The `libvirt` subvolume is
renamed `windows` and mounted at `/var/lib/nimbus/windows` per Q-010. Recovery
stays disabled until the Snapper fit check in ROADMAP.md Phase 7 and the manual
restore drill both pass.

Amended 2026-09-03. Nimbus provides no backup facility. The workstation holds
no canonical-only data: user files are synchronized or stored externally, and
local guest and container data may be lost. Complete disk loss is an accepted
rebuild and resynchronization event. Recovery points remain same-disk rollback
for reviewed system mutations and are never described as backups.

#### Q-010: Windows backend and guest-data location (resolved)

Resolved 2026-09-02.

The supported backend is QEMU/KVM through the system libvirt connection
`qemu:///system`. Nimbus manages the selected host packages, libvirt resources,
one stable Windows domain definition, and its lifecycle. It does not wrap
Quickemu or Dockur and is not a generic VM manager.

All persistent guest data is contained below
`/var/lib/libvirt/images/nimbus/windows/` on the excluded `libvirt` subvolume.
Nimbus records provider ownership without guessing or recursively replacing
native libvirt ownership. Normal component removal stops and undefines the
Nimbus-owned domain and removes safe host integration while preserving that
directory. `windows purge-data` requires a stopped and unreferenced guest,
names the resolved path, rejects symlinks or foreign files, and requires a
second confirmation before deleting only proven Nimbus-owned guest data.

The public group is `windows status`, `setup`, `start`, `connect`, `stop`, and
`purge-data`. Setup is the interactive guest-install workflow after the
component has been applied; runtime commands never install a missing
component. The domain definition is reproducible from desired state, but the
guest disk is excluded from Nimbus recovery points and needs a separate
VM-aware backup. SPEC.md owns the normative lifecycle.

Amended 2026-09-02 after the foundation review. The backend changes to the
`dockurr/windows` container on Docker with KVM, following Omarchy's
`omarchy-windows-vm`. Rejected: system libvirt, the earlier choice, because the
owner's workstation already runs Docker and the container image bundles the
unattended Windows installation, RDP, and web console that a libvirt domain
would need Nimbus to assemble; and a Docker named volume for guest data,
because it would hide the disk below `/var/lib/docker` instead of a visible,
purgeable path. Guest data lives below `/var/lib/nimbus/windows/` on the
renamed `windows` subvolume with a root-owned Compose definition and a
user-owned `0600` credentials file that `status` reports on but never reads
aloud. `connect` starts the guest when needed, waits for Windows, opens
FreeRDP, and stops the guest afterwards unless `--keep-alive` is set. Ports
bind to loopback only. Removal preserves the data root; `purge-data` deletes
it, including the credentials file. The `windows-vm` component is selected by
the `windows-vm` profile per Q-011.

Amended 2026-09-02 after the flow walkthrough. `windows setup` is the single
entry point: when the profile is missing it stages `profiles add windows-vm`
and runs the normal diff, plan, approval, and apply before the guest
installation, and `windows remove` is the matching profile removal. Rejected:
requiring `profiles add` first and making `setup` appear only afterwards,
because a user asking for Windows should not need to know the profile name.
The rule that runtime commands never install a component now exempts `setup`
alone, and only through the reviewed plan.

Amended 2026-09-03. Nimbus does not back up the excluded Windows guest disk.
Its loss is accepted because canonical files live outside the workstation; the
guest may be recreated from desired state and those external sources. This
supersedes the earlier requirement for a separate VM-aware backup.

#### Q-011: Chezmoi handoff values and Nimbus-aware gating (resolved)

Resolved 2026-09-02.

Nimbus passes `machine`, `managed_by_nimbus = true`, and the ordered profile
IDs through Chezmoi's `--promptString`, `--promptBool`, and
`--promptMultichoice` flags, whose keys are the template's prompt texts. The
dotfiles repository stays cross-platform and standalone: direct initialization
prompts for the machine name and profiles and sets `managed_by_nimbus` to
`false`. Only targets that call Nimbus gate on that value; Hyprland and
Noctalia are profile-gated and never imply Nimbus.

`windows-vm` becomes a profile that selects the `windows-vm` component so the
handoff can see it. Rejected: passing the selected component graph to Chezmoi,
which would make the dotfiles repository depend on Nimbus internals; and naming
the profile `windows`, which collides with the platform profile the dotfiles
template derives from the operating system. SPEC.md owns the normative
handoff; the dotfiles `PROFILES.md` owns the prompt keys.

#### Q-012: Interactive dashboard scope and widget library (resolved)

Resolved 2026-09-02.

Bare `nimbus` in a terminal opens a dashboard that shows and edits the selected
manifest (profiles, components, packages), reviews and applies the resulting
plan, and presents post-install tasks. It is a presentation layer only: no
resolver, planner, state, or approval logic of its own, staged edits discarded
on exit, and every action mapped to a named command. Rejected: a manifest
editor that writes TOML without going through plan and apply, because that
would create a mutation absent from a reviewed plan; and a web interface,
because the tool is terminal-first and single-user.

The widget library is Bubble Tea with Bubbles and Lip Gloss from Charm. It is
the one interactive dependency, shared by the Phase 4 package pickers and the
Phase 9 dashboard. Rejected: a second library for pickers, or hand-rolled
terminal handling, because both duplicate work the chosen library owns.
SPEC.md owns the dashboard contract and ROADMAP.md Phase 9 its delivery.

Amended 2026-09-02. The init flow also offers the package picker, so a new
machine can add individual packages beyond its profiles. Authoring profiles and
components stays file-based: edit the TOML in the checkout, run
`nimbus validate`. Rejected for now: `profiles create` and per-profile package
add and remove commands, because they would rebuild a text editor for six
small files and double the command surface; the machine `packages` list plus
`packages install` already covers one-off additions, and a package wanted on
several machines is moved into a profile by editing that file. Revisit if the
number of profiles grows.

#### Q-017: Package inventory drift from the accepted sources (resolved)

A review of PACKAGES.md on 2026-09-03 against SECURITY.md, D-009, D-010,
D-014, and Fedora 44 package-query evidence found six entries that
contradicted an accepted source, four entries available from two accepted
repositories, and packages the Standard base does not provide. Resolved the
same day:

- Tailscale, Nix with `nix-daemon`, and Just move to Fedora; Starship moves to
  Terra; Topgrade and `resvg` stay on Cargo under the amended D-014 rule.
- Noctalia and Chezmoi come from Fedora, Steam from RPM Fusion, and `unrar`
  from RPM Fusion because Fedora's `unrar` is the `unrar-free` wrapper.
- Accepted additions: `mesa-va-drivers-freeworld`, `intel-media-driver`,
  `playerctl`, `pipewire-pulse`, `pipewire-alsa`, `gvfs` with MTP and SMB,
  `ShellCheck`, `gitleaks`, `yq`, `duf`, `sane-airscan`, `simple-scan`,
  Herdr under D-010, and Sheldon on Cargo.
- Rejected: `pavucontrol` and `wtype`, because Noctalia covers them; `tmux`,
  because Herdr is the agent runtime; `google-noto-fonts-all`, because it
  installs every Noto script rather than the extra weights the owner meant.

The Noctalia plugin manifests confirm the listed dependencies. Crashes also
needs `coredumpctl` from systemd, AI Usage needs the still-unsourced
`ai-usagebar` binary, and Screen Recorder needs `gpu-screen-recorder`, now
listed with the plugin. Q-016 keeps the inventory's purpose and its remaining
service-activation entries.

Amended 2026-09-03 by D-015: Topgrade leaves Cargo for Terra.

Amended 2026-09-03 after the passthrough: Fedora's lazygit package is
`golang-github-jesseduffield-lazygit`, 1Password is `1password` and
`1password-cli`, and the graphics entry names `mesa-dri-drivers`,
`mesa-vulkan-drivers`, `intel-gpu-firmware`, and `amd-gpu-firmware`.

#### Q-013: Phase 1 schema boundary and canonical ordering (resolved)

D-002 requires the public definition contract to precede frozen Go types, but
SPEC.md currently gives concrete TOML only for the selector and machine
manifest. Before implementing the resolver, define the required fields,
optional fields, nesting, and enum values for `nimbus.toml`, profiles,
components, and every resource declaration retained in Phase 1.

The same decision must settle:

- whether every machine must explicitly select `common` or the resolver injects
  it
- which declared lists preserve author order and which resolved identities are
  canonicalized
- how component requirements, conflicts, source locations, and selection
  provenance are represented
- whether warnings, manual tasks, runtime command groups, recovery metadata,
  and other later-phase declarations are omitted until real definitions and
  fixtures exercise them

The narrow candidate is to require `common` explicitly, preserve declared
profile order for the Chezmoi handoff, canonicalize technical identities and
their deterministic output, and defer unused later-phase fields.

Resolved 2026-09-03 as the narrow candidate. Every manifest lists `common`
and validation reports its absence. The manifest `profiles` list keeps author
order; every other list is sorted by canonical ID and a duplicate is an error.
Files carry only `id`, `packages`, `components`, `requires`, `conflicts`,
`removes`, and system-file sources; later-phase fields wait for a real
definition and fixture. Rejected: injecting `common`, because the manifest
should say what the machine is.

#### Q-014: Catalog entries and repository trust ownership (resolved)

The package contract says that one catalog entry resolves to one
provider-native package identity, while a third-party repository also enters
through a catalog entry. That is straightforward for one package such as
PrismLauncher, but ambiguous for Docker's one repository and five packages.

Decide whether repositories are stable first-class resources referenced by
catalog package entries, how a release-package digest or signing-key
fingerprint is encoded, and how one component selects and owns the repository
exactly once without duplicating lifecycle ownership. The answer must retain
the exception-only catalog, explicit trust pin, deterministic resolution, and
safe repository removal rules.

Resolved 2026-09-03 by D-016: repositories are declared once in `nimbus.toml`
and package references name them by prefix. Docker is one declaration and
five `docker:` references. Flathub is the declared `flatpak` repository.

#### Q-015: Exact Chezmoi profile-refresh handoff (resolved)

The initial three-value handoff is valid, but the later printed refresh command
is incomplete. The dotfiles template uses `prompt*Once`, so a supplied
`--promptMultichoice` value does not necessarily replace an existing stored
selection unless initialization is forced to prompt or another supported
update path is used.

Define one exact user-run refresh command or mechanism that updates `machine`,
`managed_by_nimbus`, and the ordered profile IDs without editing Chezmoi
internal state. It must preserve the independent 1Password choice and be
verified in isolated homes for fresh initialization, adoption of an existing
standalone checkout, profile addition, and profile removal.

Resolved 2026-09-03. The refresh is `chezmoi init --prompt`, which forces the
`prompt*Once` functions to prompt while the flags supply `Machine`,
`ManagedByNimbus`, `Profiles`, and the current 1Password answer read through
`chezmoi data`. The dotfiles template today declares only `onePasswordSsh`
and `Profiles` and fails on any profile outside its four choices. It gains
the two keys and stops validating the list: only `hyprland-noctalia` and the
Nimbus-calling entries are gated, every other Linux config deploys
unconditionally, and macOS and Windows keep their platform profiles. TASKS.md
carries that task and Phase 5 verifies both flows in isolated homes.
Rejected: keeping a fixed choice list in the template, because every new
Nimbus profile would break the handoff; and shrinking the handoff to
`Profiles`, because the `managed_by_nimbus` gate exists for real targets.

#### Q-016: Package inventory purpose and ownership notation (resolved)

PACKAGES.md says it lists what Nimbus installs, but it also contains user-owned
maker-script and Cargo tools plus Mise-owned runtimes. Its source markers no
longer agree with SECURITY.md in places: Mise still names its installer rather
than the accepted maker COPR, Docker service activation includes
`docker.socket` beyond the accepted `docker.service`, and
`snapper-cleanup.timer` conflicts with Nimbus-only snapshot retirement.
Fedora's `nix-daemon` package adds `nix-daemon.service` as a third service
activation to reconcile.

Decide whether the file becomes a complete workstation software inventory with
an explicit owner, provider, source tier, and lifecycle for each entry, or is
restricted to Nimbus-owned installation targets. The complete-inventory form
is the preferred candidate because it preserves useful owner intent while
making the boundary machine-readable by a human.

Resolved 2026-09-03. PACKAGES.md lists what Nimbus installs, and under D-017
that includes the user-scope steps Nimbus runs as the user, so every entry is
Nimbus-owned and no owner marker is needed. The Noctalia plugin phases and the
reference to the docs repository are dropped; enabling a plugin is user
state. The specific conflicts the question named had already been removed by
Q-017.

### Open questions

#### Q-018: Should the Fedora base be extracted live instead of listed?

Opened 2026-09-03 after Phase 3. `components/fedora-base.toml` lists the 132
packages the installer leaves behind so that none of them is a prune
candidate. The list is generated from Fedora's comps groups plus a
hand-verified Anaconda set, must be regenerated per Fedora release, and can
only be completed from the real prune output of the first VM run. The owner
asks whether Nimbus should instead read the base set from the live system
and always keep it out of the prune check, so the repository does not carry
a list of every base package.

Candidates, to be judged against real data from the Phase 4 VM run:

- Read DNF history: the installer's transaction is the first one, and its
  package set is the base by definition. Cheapest if DNF5 exposes it
  reliably and Anaconda's transaction is identifiable.
- Read the installed comps groups: `dnf5 group list --installed` plus
  `group info` gives the same set the current list is generated from, but
  live and per release, and misses the Anaconda-only packages the same way.
- Keep the generated list, accepting the per-release regeneration.

What a live rule changes: the base stops being desired state, so `why` no
longer explains `bash`, apply neither adopts nor removes base packages, and
prune eligibility becomes a policy on observed facts rather than a
comparison with definitions. That is different from the D-012 hardware
case, where observation was rejected as desired state, because a prune
exclusion never installs anything.

Direction chosen 2026-09-03, implemented and resolved in Phase 4. Every
installed package falls into one of three buckets: managed, with a receipt
because Nimbus installed or adopted it; pre-existing, recorded as a baseline
at the first `nimbus init` and never a prune candidate, which on a fresh
machine is exactly the Fedora base; and unmanaged-added-later, installed by
hand after Nimbus took over, which are the prune candidates. `unmanaged`
lists the third bucket by default and `--all` adds the baseline with a
`pre-existing` marker. After a Fedora release upgrade, renamed base packages
appear once in the third bucket and an explicit accept command folds them
into the baseline after review. `fedora-base` is deleted when the baseline
exists. Rejected: reading DNF history or comps live, because both depend on
what the installer recorded and the comps route needs metadata at plan
time.

### Consequences for the next documentation pass

Every question through Q-017 is resolved and D-007 is closed. Q-018 stays
open until the Phase 4 VM run supplies the data to decide it. The only work
outside this repository is the dotfiles template change from Q-015, which
gates the Phase 5 Chezmoi handoff. The active documents are consolidated below
`docs/`, and legacy history is retained only in the named Git snapshot. One
cleanup remains: `files accept` is described in SPEC.md twice and repeated
here, in ROADMAP.md, and in TASKS.md, and the selector and digest paragraphs
repeat in ROADMAP.md Phase 1. When SPEC.md is next restructured, keep one full
description per behavior and reduce the others to a sentence and a link. The
CI workflow arrives with go.mod per TASKS.md.
