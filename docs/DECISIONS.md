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

Resolved 2026-09-02. The dotfiles repository removed its machine manifests and
symlink selector, adopted the handoff in Q-011, and now cites `docs/SPEC.md`.
Phase 5 verifies the prompt flags against its real template before exit.

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
`catalog:prismlauncher` with a Terra repository entry whose release package or
signing key is pinned. Rejected: the Flathub Flatpak, kept as the fallback if
Terra trust cannot be represented, because the owner prefers native RPMs with
system Java; and a PrismLauncher COPR, because Terra already carries the
package with a maintained signing key.

#### D-010: SECURITY.md owns the workstation security policy

Added 2026-09-02. Software-source trust tiers, encryption, Secure Boot,
SELinux, firewall, privilege, secrets, and doctor checks moved into one policy
file so SPEC.md keeps only the engine invariants. Terra and RPM Fusion are
accepted sources with pinned keys; `curl | sh` installers are refused because
Terra and Fedora ship the tools that use them; firewalld is the firewall on
Fedora rather than ufw; the `docker` group is not granted because it is
root-equivalent. Rejected: keeping the policy as bullets inside SPEC.md,
because it was already too thin to guide the Terra and Windows decisions.

Amended 2026-09-02 after the owner's ranking. Sources are ranked by who
publishes them, not by format: Fedora, then the maker's own repository, COPR,
or Flatpak, then the maker's installer script as a user-scope manual task, then
community repackages such as RPM Fusion, Terra, and third-party COPR, then
pinned artifacts. The owner wanted maker channels for their prompt updates;
verifying the installers showed they write below `~/.local`, so they are user
scope by construction and Nimbus never runs them. Mise therefore comes from the
maker-owned COPR `jdxcode/mise` and Zed from its installer. Rejected: refusing
maker scripts outright, because Zed offers nothing else and the script cannot
reach the system layer; and letting Nimbus run them as root, because they are
unsigned and would be a mutation outside the plan.

Amended 2026-09-02 after the owner's privilege and encryption calls. The owner
joins the `docker` group as a declared group-membership resource; the policy
names it root-equivalent and doctor reports any undeclared member. Rejected:
`sudo docker` for every call, because daily Docker use without sudo is the
point of the group. TPM2 auto-unlock is enrolled without a PIN, bound to PCR 7,
alongside the permanent passphrase slot. Rejected: requiring a PIN, because the
owner wants hands-free unlock and accepts that theft, not `/boot` tampering, is
the protected case until unified kernel images add PCR 11.

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

Chezmoi owns a development-profile-gated
`run_onchange_after_install-mise-runtimes.sh.tmpl` action. It runs as the normal
user after `~/.config/mise/config.toml` is applied and includes the rendered
configuration's checksum in its own rendered content. Chezmoi therefore shows
the action during review and reruns it only when the effective Mise
configuration changes. The action invokes:

~~~sh
MISE_SYSTEM_DEPS=warn mise -C "$HOME" install
~~~

The warning policy prevents Mise from taking over privileged system-package
installation. Nimbus installs Mise and the selected system dependencies;
Chezmoi manages the configuration and action; Mise installs the declared
user-scope runtimes.

Users who want a separate runtime preview can apply files without scripts, run
the same Mise operation with `--dry-run`, and then apply scripts. If a runtime
is deleted without a configuration change, the onchange action does not rerun.
Nimbus reports the missing runtime and the direct Mise command but never runs
the repair itself. SPEC.md owns the normative handoff.

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

### Open questions

None.

### Consequences for the next documentation pass

Q-001 through Q-012 are resolved and D-007 is closed. The active documents are
consolidated below `docs/`, and legacy history is retained only in the named
Git snapshot. One cleanup remains: `files accept` is described in SPEC.md
twice and repeated here, in ROADMAP.md, and in TASKS.md, and the selector and
digest paragraphs repeat in ROADMAP.md Phase 1. When SPEC.md is next
restructured, keep one full description per behavior and reduce the others to
a sentence and a link. The CI workflow arrives with go.mod per TASKS.md.
