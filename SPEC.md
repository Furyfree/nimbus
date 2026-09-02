# Nimbus specification

## Purpose

Nimbus is a personal, opinionated Fedora workstation installer and system
manager. It defines the system its owner wants to run, turns a supported
installed Fedora base into that system, detects drift in resources it owns, and
performs reviewed installation, repair, upgrade, and removal.

Nimbus owns the system layer. Chezmoi owns the user configuration layer.

Nimbus is not a general configuration-management engine or a support promise
for arbitrary third-party definitions. Other users may inspect or fork the
complete repository and change its definitions and implementation.

## System boundary

Nimbus starts from an installed system with:

- a supported Fedora release
- an x86_64 machine
- a normal user with wheel access
- working networking
- a mounted and bootable root filesystem

Nimbus does not require Hyprland, Noctalia, Chezmoi, development tools, or the
owner's dotfiles to be present.

Nimbus does not normally install Fedora, partition disks, create filesystems,
or configure full-disk encryption. A later installer image may reuse the same
definitions and provisioning operations while leaving destructive storage
creation to Anaconda.

## Repository and distribution model

The Nimbus repository contains one complete personal system:

~~~text
nimbus/
  cmd/
  internal/
  machines/
  profiles/
  components/
  catalog/
  system/
    root/
    migrations/
    triggers/
  nimbus.toml
~~~

- Go code implements the engine.
- Machine manifests select intended workstation compositions.
- Profiles are user-facing system bundles.
- Components describe reusable capabilities and own resources.
- Catalog entries describe packages with non-default lifecycle.
- system/root mirrors Nimbus-owned system-file targets.
- Migrations represent controlled transitions that cannot be expressed as
  steady-state resources.

An installed engine reads live definitions from an explicitly selected Nimbus
checkout. nimbus.toml declares the definition schema and minimum compatible
engine version. The engine rejects unsupported schemas.

The canonical definition digest covers the path, mode, and content of every
regular definition and system-source file in sorted order. Symlinks inside the
definition tree are rejected. Plans and receipts identify the engine version,
checkout origin, commit, dirty state, and definition digest.

The engine may embed an example definition tree only to create a new local
checkout. Embedded data is never the live personal desired state.

Engine packaging and the checkout may be delivered separately. Their
compatibility is explicit and verified; Nimbus never updates either itself.

## Configuration model

The local selector is:

~~~text
~/.config/nimbus/config.toml
~~~

Example:

~~~toml
schema = 1
checkout = "/home/pby/.local/share/nimbus"
machine = "desktop"
~~~

The selector points to one checkout and one tracked manifest. It does not copy
profiles, components, packages, constraints, or dotfiles configuration.
Commands may accept explicit checkout and machine overrides.

A machine manifest has a stable ID:

~~~toml
schema = 1
id = "desktop"

profiles = [
  "common",
  "development",
  "gaming",
  "hyprland-noctalia",
]

components = [
  "nvidia",
  "windows-vm",
]

packages = [
  "ripgrep",
  "flatpak:org.signal.Signal",
]

package_exclusions = []

[package_constraints]
hyprland = "=0.56.0"

[dotfiles]
repo = "https://github.com/Furyfree/dotfiles.git"
~~~

The manifest ID must match its filename. Hardware inspection never edits or
silently augments the manifest.

The initial profile vocabulary is:

- common
- development
- gaming
- hyprland-noctalia

Profiles select components and never import other profiles. Components may
require other components and contribute packages, repositories, services,
system files, groups, triggers, warnings, manual tasks, runtime commands,
verification, removal policy, and recovery classification.

Machine manifests may select optional components and add ad hoc packages.
Profile and component definitions remain the source of reusable intent;
machine-specific differences stay sparse.

## Resolution

Resolution is deterministic:

~~~text
machine
  -> profiles
  -> profile components
  -> explicit components
  -> component dependencies
  -> resources
  -> manual tasks
  -> runtime command groups
~~~

Resolution reads configuration only. It never inspects hardware, the installed
system, native package databases, or Nimbus applied state.

nimbus validate checks every definition and machine manifest in the selected
checkout. nimbus config resolve resolves one selected machine. Selecting no
optional component is valid; later inspection may warn that the machine lacks a
desktop session or that detected hardware has no selected supporting component,
but it never changes the selection.

Validation rejects:

- unsupported schemas or engine compatibility
- duplicate machine, profile, component, resource, or catalog IDs
- unknown references and dependency cycles
- conflicting components or desired resource states
- duplicate lifecycle ownership
- invalid package exclusions or constraints
- exclusions of protected or technical dependencies
- invalid provider configuration
- arbitrary shell operations
- path traversal, symlink escape, and system sources outside the checkout

The same checkout content and machine selection always resolve to the same
desired graph and canonical digest.

## Package model

Nimbus is Fedora-first. A bare package name uses the typed default DNF
lifecycle:

- inspect through RPM and DNF
- install from an enabled reviewed repository
- update through DNF
- preserve the native install reason
- verify through the native package database
- remove through DNF only when Nimbus owns the package and removal is safe

A catalog entry exists only for exceptional behavior such as:

- a required Fedora repository, RPM Fusion source, or COPR
- a provider other than DNF
- a coordinated update group
- special verification or removal
- a pinned external artifact
- a package-specific warning

The catalog is an exception list, not a registry of every Fedora package.
Package-specific behavior belongs in catalog data, never package-name
conditionals in Go.

One provider owns an installed executable lifecycle. Nimbus may manage
system-scoped Flatpaks. User-scoped runtimes and tools declared in
~/.config/mise/config.toml remain owned by Mise; Chezmoi owns that file.
Nimbus may install Mise and report missing runtimes but does not provide Mise,
Cargo, uv, npm, pipx, or Go user-scope installation providers.

A package constraint is accepted only when the native provider can enforce it
during normal native updates. Unsupported comparison syntax is rejected rather
than represented as an advisory-only constraint.

Compatibility-sensitive packages may form a coordinated update group. The
desktop-session group covers the selected Hyprland, Noctalia, greeter, portal,
and session integration resources. Its exact membership and verification must
be proven against the selected Fedora sources before it is encoded.

## Resource model

A resource is one stable unit of desired system state. Resource families
include:

- DNF and COPR repositories
- RPM packages
- system Flatpaks
- systemd system units
- system files
- system groups and user group memberships
- kernel arguments and boot resources
- external artifacts with approved integrity evidence
- fixed-argv triggers
- migrations

Each resource has:

- a stable ID and one technical lifecycle definition
- contributing selection paths for provenance
- desired state and inspection
- planned apply and verification
- exact privilege requirements
- removal behavior or an explicit unsupported-removal state
- recovery classification
- logout or reboot requirements

Profiles do not define technical lifecycle. Components or explicit machine
entries select resources; catalog data and the typed provider define how each
resource is inspected, applied, verified, updated, and removed. Provenance
records every profile, component, or explicit machine path that selected it.

## Ownership

Nimbus owns:

- Fedora and approved third-party repositories
- Nimbus-selected RPMs and system Flatpaks
- system services, timers, groups, files, and drop-ins
- graphical-session, greeter, portal, and recovery-session integration
- hardware, kernel, boot, update, and recovery policy it declares
- inspection, plans, apply, verification, removal, state, and receipts
- installation of system tools including Git, Chezmoi, Mise, and 1Password
- the explicit first Chezmoi initialization
- typed manual workflows and Nimbus runtime commands

Chezmoi owns:

- files and templates below the user's home directory
- Hyprland, Noctalia, shell, terminal, editor, browser, and application user
  configuration
- systemd user unit files, user scripts, and desktop entries
- secret-backed templates
- its source checkout and normal diff, apply, edit, and update lifecycle

Native tools retain their own lifecycle. Nimbus invokes and verifies DNF,
systemd, Flatpak, Mise, Git, Chezmoi, and other specialist tools rather than
reimplementing them.

Credentials, tokens, private keys, application databases, histories, caches,
documents, unrelated containers and virtual machines, and undeclared local
system configuration remain unmanaged.

## Desired, observed, and applied state

Nimbus keeps three states separate:

~~~text
checkout definitions and machine manifest -> desired
live native-system inspection             -> observed
/var/lib/nimbus receipts                  -> last applied
~~~

Desired configuration never comes from state or receipts. Observed state never
becomes desired merely because it exists. Applied state records only verified
Nimbus operations and never replaces current inspection.

State under /var/lib/nimbus is versioned, contains no secrets, and is written
atomically through a narrow operation-scoped privileged action. A receipt
records at least:

- state and engine versions
- definition origin, commit, dirty state, and digest
- machine and resolved selections
- resource and provider
- previous observation and intended state
- exact lifecycle operation
- approved plan digest
- verification result
- recovery-point ID when applicable
- timestamp and reboot or logout requirement

A failed operation never produces a successful receipt. Accurate receipts for
previously completed independent operations remain after partial failure.

## Inspection, status, plan, and apply

Inspection uses native read-only interfaces and produces structured facts.
Status compares desired, observed, and applied state without mutation.

A plan contains:

- resolved intent and relevant observed facts
- install, change, adopt, repair, and owned-removal operations
- unchanged, blocked, manual, and unmanaged resources
- exact privileged commands and system-file diffs
- verification, triggers, recovery, warnings, and reboot requirements
- optional prune candidates kept separate from normal apply

The canonical plan excludes volatile display data. Nimbus hashes it when shown
for approval and refuses apply if configuration, definitions, facts, native
transactions, or the digest changed before execution.

Planning never mutates the system. A plan command may write only an explicitly
named output file containing the canonical plan and digest.

Plain apply installs and repairs desired resources, adopts compatible existing
resources, and removes resources previously owned by Nimbus that are no longer
desired. It leaves unrelated unmanaged resources unchanged.

Pruning is a separate expanded plan and approval. An unmanaged resource is
eligible only when the native provider proves explicit installation,
non-protected status, dependency safety, and an exact removal and verification
path.

## Privilege and concurrency

Nimbus runs as the normal user and refuses to run the whole CLI as root.
Read-only system operations never invoke sudo.

Every privileged operation is rendered in the reviewed plan. Native operations
run directly through sudo. Nimbus may expose only two classes of narrow
internal privileged action:

- atomically install the approved system-file payload for one plan step
- atomically record the approved root-owned state and receipts

They accept only staged data bound to the approved plan digest. Nimbus has no
general privileged executor, root daemon, helper service, or sudo keepalive.

Only one mutating Nimbus operation may run at a time. Locks identify the
operation and process and are never removed solely because they are old.
Read-only commands may run concurrently when they can obtain consistent input.

## System files, triggers, and migrations

System file sources mirror their absolute targets under system/root. File
resources declare source, target, ownership, mode, verification, change
triggers, removal, and recovery. Nimbus shows complete diffs before apply.

Triggers use reviewed stable IDs and fixed argument vectors. A trigger runs at
most once per apply even when several resources request it. Arbitrary shell is
not a trigger or resource type.

A migration handles a versioned transition that steady-state reconciliation
cannot safely express. It has applicability checks, preflight, an exact plan,
verification, recovery, and a completion receipt. Normal installation must not
become a growing sequence of migrations.

## Bootstrap and initialization

Bootstrap does only enough to obtain a compatible Nimbus engine, Git transport,
and trusted Nimbus checkout. It verifies the supported platform and shows every
repository or artifact it will trust before changing the system. The first
successful apply adopts bootstrap packages that belong to desired state.

nimbus init:

1. validates the engine and checkout compatibility
2. lists tracked machine manifests and selects one
3. writes ~/.config/nimbus/config.toml
4. resolves and inspects the selected system
5. shows the complete system plan
6. applies only after approval
7. initializes Chezmoi when selected and available
8. reports direct Chezmoi and remaining manual steps

Fresh installation and reinstallation use the same tracked machine manifest.
Creating a new machine writes a reviewed manifest to the Nimbus checkout and
leaves the resulting Git change for the user.

Nimbus works without a dotfiles repository. In that case the system remains
usable and the Chezmoi initialization task stays pending.

Nimbus may cause an explicitly selected dotfiles repository to be cloned only
through the first supported Chezmoi initialization. Outside that explicit
operation it performs no Git network mutation. It never silently replaces a
checkout, changes its remote, commits, pulls, pushes, resets, stashes, or
resolves conflicts.

## Chezmoi handoff

Nimbus passes only the context needed by user-file templates:

- machine ID
- ordered selected profile IDs

The transport uses supported Chezmoi initialization arguments and never edits
Chezmoi internal state directly. Direct Chezmoi initialization prompts for the
same information when Nimbus is absent. Hardware facts and secrets do not cross
the handoff.

Nimbus never runs chezmoi apply or chezmoi update. After initialization it
prints the direct commands needed to inspect and apply user configuration.
Removing Nimbus leaves the dotfiles checkout and Chezmoi lifecycle usable.

## Desktop and recovery session

The hyprland-noctalia profile owns the system requirements for Hyprland,
Noctalia, the greeter, portals, session entries, and compatibility-sensitive
updates. Chezmoi owns normal user configuration for Hyprland and Noctalia.

Nimbus installs a separate minimal system-owned recovery session under /usr.
It uses an explicit system configuration, opens a terminal, provides minimal
recovery bindings, and does not depend on user dotfiles. It works before and
after Chezmoi apply and is removed with its owning component.

Nimbus never writes a seed compositor configuration into the user's home
directory.

## Manual tasks and runtime commands

Components may contribute typed manual tasks for work requiring human
interaction, such as signing in to 1Password, enrolling an NVIDIA MOK,
enrolling a fingerprint, completing a Windows guest, or rebooting.

A task has a stable owner, prerequisites, status check, instructions or a typed
action, verification, completion state, recovery, and reboot or logout
requirements. Status checks remain authoritative after a receipt. Tasks are not
arbitrary shell scripts.

Runtime command groups operate only on already selected and applied components.
They never install missing components implicitly. A later Windows component may
provide setup, status, start, connect, stop, and explicit data-purge operations.
Guest data survives normal component removal.

A future interactive interface is a presentation layer over the same resolver,
facts, plans, tasks, and commands. It has no independent business logic.

## Updates and recovery

Normal Fedora updates remain visible through DNF. Nimbus defines the expected
managed packages, repository state, enforceable constraints, coordinated update
groups, recovery requirements, and post-update verification. Nimbus does not
silently update itself, its checkout, dotfiles, or system packages.

Before disruptive, reboot-required, or stateful transactions, Nimbus creates a
recovery point only after the applicable restore procedure and retention policy
have been tested. Recovery may include Btrfs snapshots, boot and EFI archives,
the approved plan, and related receipts.

Automatic rollback is unsupported until a complete restore drill proves it.
Manual recovery remains documented independently of Nimbus state.

## Security

- Configuration, plans, state, receipts, and logs contain no secrets.
- External artifacts require an approved source and cryptographic digest or
  supported signature.
- Remote shell scripts are not a resource type.
- Native package signatures remain enabled.
- Paths are validated against traversal, symlink escape, and unsafe ownership.
- Destructive operations name their exact target and require explicit approval.
- Unknown ownership prevents automatic removal.
- Guest management interfaces bind to loopback unless explicitly configured.
- Nimbus never disables Secure Boot.
- Secure Boot key enrollment is explicit manual work with verification.
- Nimbus never mutates live disk partitioning or root encryption.
- Failed verification prevents a success receipt.

## Command contract

The intended non-interactive engine includes:

~~~text
nimbus validate
nimbus config resolve
nimbus facts
nimbus status
nimbus plan
nimbus apply
nimbus managed
nimbus unmanaged
nimbus why RESOURCE
nimbus doctor
nimbus packages
nimbus init
nimbus version
~~~

Commands return conventional exit codes:

- 0 for success, including an empty result
- 1 for validation, inspection, planning, verification, or operation failure
- 2 for invalid usage

Structured output uses a versioned JSON envelope containing the engine version,
output schema, data, and structured errors. Human and JSON rendering use the
same underlying data.

## Compatibility

- Nimbus supports Linux only.
- The first supported target is Fedora 44 on x86_64.
- The first desktop target is Hyprland with Noctalia.
- Other operating systems and desktop compositions require evidence from real
  systems before entering the supported matrix.
- Dotfiles may remain cross-platform; Nimbus manages only Linux system state.

## Non-goals

Nimbus is not:

- a dotfiles deployment or template engine
- a general Ansible, Terraform, or configuration-management replacement
- a generic third-party provider or plugin platform
- a package manager, package store, or dependency solver
- a user-scope development-runtime manager
- a background reconciliation daemon
- a secret manager
- a generic virtual-machine or remote-host manager
- a tool for silently removing manually installed software
- a live disk partitioning or encryption tool
- a universal Linux installer
- a promise to support unknown forks or definition repositories

## System acceptance

Nimbus is complete when a supported clean Fedora installation can:

- select a tracked machine and resolve the same desired graph deterministically
- inspect the real system and show a complete non-mutating plan
- apply only the approved unchanged plan with scoped privilege
- verify every successful mutation and write complete receipts
- report and repair owned drift without claiming unrelated state
- remove owned resources safely while preserving unmanaged data
- initialize the selected Chezmoi repository without taking over its lifecycle
- boot both the normal user session and the system-owned recovery session
- recover from each supported disruptive operation through a tested path
- expose equivalent stable human and structured command behavior

The implementation order and decision gates for reaching this contract live in
[ROADMAP.md](ROADMAP.md).
