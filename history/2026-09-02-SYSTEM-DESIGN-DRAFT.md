# Nimbus system design

Nimbus is a personal, opinionated Fedora workstation installer and system
manager.

It defines the system Patrick wants to run, installs that system on supported
machines, detects drift, repairs managed resources, performs controlled
upgrades, and coordinates interactive setup that cannot be completed
declaratively.

Nimbus owns the system layer. Chezmoi owns the user configuration layer.

Nimbus is not a general Linux configuration-management engine. It does not
attempt to support arbitrary third-party definitions, unknown distributions,
or every possible Linux setup. Anyone who wants a different system can fork
Nimbus and change its definitions, implementation, and defaults.

This document describes the intended complete system. It does not define
implementation phases or delivery order.

## 1. System boundary

Nimbus starts from an installed Fedora system with:

- a supported Fedora version
- an `x86_64` machine
- a normal user with `wheel` access
- working networking
- a mounted root filesystem
- no requirement that Hyprland, Noctalia, Chezmoi, or development tools are
  already installed

Nimbus does not initially install Fedora itself, repartition disks, create
filesystems, configure full-disk encryption, or replace Anaconda.

Nimbus turns the installed Fedora base into the intended workstation and keeps
the Nimbus-owned part of that workstation consistent afterward.

```text
Fedora installation
        |
        v
Nimbus bootstrap
        |
        v
Select machine
        |
        v
Resolve profiles and components
        |
        v
Inspect system
        |
        v
Review plan
        |
        v
Apply system resources
        |
        v
Initialize and apply Chezmoi
        |
        v
Complete post-install tasks
```

## 2. Ownership

Ownership is determined by resource lifecycle, not only by filesystem
location or privilege level.

### Nimbus owns

- Fedora repositories and COPR repositories
- RPM packages installed as part of the workstation
- system-scoped Flatpaks, if system ownership is selected
- systemd system units
- system files under `/etc`, `/usr`, and other root-owned locations
- the graphical session and recovery session
- greeter and portal integration
- hardware-related system configuration
- kernel arguments and boot-related configuration explicitly declared by
  Nimbus
- system groups and required group memberships
- package update constraints
- inspection, planning, application, verification, removal, and repair
- recovery points for disruptive Nimbus operations
- Nimbus state and receipts
- installation of Git, Chezmoi, Mise, 1Password, and other workstation tools
  when they are part of the selected system
- the initial handoff to Chezmoi
- typed post-install workflows contributed by Nimbus components
- Nimbus runtime helpers such as the Windows VM commands

### Chezmoi owns

- files and templates below the user's home directory
- Hyprland user configuration
- Noctalia user configuration
- shell, terminal, editor, browser, and application configuration
- systemd user unit files
- user scripts and desktop entries
- user-specific monitor, keyboard, and application preferences
- its source repository and normal file-management lifecycle
- rendering, diffing, applying, and removing managed user files
- secret-backed templates and integration with password managers

### Other tools retain their native lifecycle

Nimbus or Chezmoi may install and configure a tool without replacing the
tool's own lifecycle.

Examples:

- DNF owns RPM installation and dependency resolution.
- systemd owns service state.
- Flatpak owns Flatpak applications.
- Mise owns development runtimes declared in its configuration.
- 1Password owns credentials and SSH-agent keys.
- Dockur or the selected virtualization backend owns the Windows guest
  runtime.
- Chezmoi owns rendering and applying user files.

Nimbus invokes native tools and verifies their results. It does not reimplement
their package stores, databases, template engines, dependency solvers, or
runtime management.

### Unmanaged state

Nimbus does not attempt to own the complete machine.

The following remain unmanaged unless explicitly declared:

- manually installed experimental packages
- user documents and downloads
- application databases
- browser profiles
- credentials and tokens
- caches and histories
- container data unrelated to a selected Nimbus component
- virtual machines unrelated to Nimbus
- locally created system configuration outside Nimbus's resource set

Nimbus reports drift only for resources it owns or has explicitly adopted.

## 3. Repository model

Nimbus code and Nimbus system definitions live in the same repository.

```text
nimbus/
  cmd/
    nimbus/

  internal/
    config/
    resolve/
    inspect/
    plan/
    apply/
    state/
    providers/
    postinstall/
    dashboard/

  machines/
    desktop.toml
    laptop.toml

  profiles/
    common.toml
    development.toml
    gaming.toml
    hyprland-noctalia.toml

  components/
    fedora-base.toml
    workstation-base.toml
    development.toml
    gaming.toml
    wayland.toml
    hyprland.toml
    noctalia.toml
    greeter.toml
    portals.toml
    nvidia.toml
    laptop.toml
    libvirt.toml
    windows-vm.toml
    fingerprint.toml

  packages/
    # Definitions only for packages with non-default behavior.

  system/
    root/
      etc/
      usr/

    migrations/
    triggers/

  bootstrap
  CLI.md
  DESIGN.md
  SPEC.md
```

The repository represents one complete system:

- Go code implements Nimbus.
- Profiles describe the intended workstation variants.
- Components describe installable capabilities.
- Machine manifests select the intended composition for each machine.
- System files contain the root-owned files deployed by Nimbus.
- Migrations describe controlled transitions that cannot be represented as
  normal steady-state resources.

Other users are expected to fork this repository. Nimbus does not promise that
an upstream binary will safely execute an unrelated third-party definition
repository.

The Dotfiles repository remains separate and contains only Chezmoi source
state and Dotfiles-specific data.

## 4. Configuration model

Nimbus has six primary configuration concepts:

1. machine
2. profile
3. component
4. resource
5. post-install task
6. runtime command group

### 4.1 Machine

A machine manifest selects the intended system for a specific machine.

```toml
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
  "libvirt",
  "windows-vm",
]

packages = [
  "btop",
  "ripgrep",
]

package_exclusions = []

[package_constraints]
hyprland = ">=0.56, <0.57"

[dotfiles]
repo = "https://github.com/Furyfree/dotfiles.git"
```

A laptop may select a different composition:

```toml
schema = 1
id = "laptop"

profiles = [
  "common",
  "development",
  "hyprland-noctalia",
]

components = [
  "laptop",
  "fingerprint",
]

packages = []
package_exclusions = []

[dotfiles]
repo = "https://github.com/Furyfree/dotfiles.git"
```

Machine manifests describe intentional differences. They do not duplicate the
complete package and resource list.

Hardware detection never silently modifies the selected machine manifest.
Nimbus may warn that an NVIDIA GPU has no NVIDIA component, but it does not
select the component automatically.

### 4.2 Profile

A profile is an opinionated system bundle with user-visible meaning.

Initial profiles are:

- `common`
- `development`
- `gaming`
- `hyprland-noctalia`

Profiles select components. Profiles do not contain machine detection and do
not import other profiles.

```toml
schema = 1
id = "hyprland-noctalia"
summary = "Hyprland desktop with Noctalia"

components = [
  "wayland",
  "hyprland",
  "noctalia",
  "greeter",
  "portals",
]
```

A profile exists when the bundle is meaningful as a user selection. Profiles
are not used merely to represent operating-system facts such as `linux`,
`fedora`, or `x86_64`.

### 4.3 Component

A component is an independently selectable capability.

A component may contribute:

- required components
- packages
- repositories
- services
- system files
- group memberships
- triggers
- warnings
- post-install tasks
- runtime command groups
- verification rules
- removal policy
- recovery classification

Example:

```toml
schema = 1
id = "windows-vm"
summary = "Managed Windows virtual machine"

requires = [
  "container-runtime",
]

packages = [
  "freerdp",
]

postinstall = [
  "windows-guest-setup",
]

commands = [
  "windows",
]

recovery = "stateful"
```

Components may require other components. Dependency cycles, unknown component
IDs, conflicting components, and duplicate resource ownership are validation
errors.

### 4.4 Resource

A resource is one unit of desired system state with a stable identity.

Supported resource families are:

- DNF repository
- COPR repository
- RPM package
- system Flatpak
- systemd system unit
- system file
- system group
- user group membership
- kernel argument
- external artifact
- trigger
- migration

Every resource has:

- a stable ID
- an owner profile or component
- desired state
- inspection logic
- apply logic
- verification logic
- removal logic, when safe
- privilege requirements
- recovery classification
- reboot or logout requirements

Resource IDs remain stable even if package names or implementation details
change.

### 4.5 Post-install task

A post-install task represents interactive or manual work that cannot be
completed as a normal declarative resource.

Examples:

- sign in to 1Password
- enable 1Password CLI integration
- apply secret-backed Dotfiles templates
- complete the Windows guest installation
- enroll a fingerprint
- enroll an NVIDIA Machine Owner Key
- log out or reboot
- verify a hardware-specific result

A post-install task has:

- a stable ID
- the component that contributes it
- prerequisites
- a status check
- instructions or a typed action
- verification
- completion state
- recovery instructions
- optional reboot or logout requirement

Post-install tasks are not arbitrary shell scripts.

A completion receipt records that the task was completed, but the status check
remains authoritative. If later verification fails, the task becomes pending
again.

### 4.6 Runtime command group

A runtime command group provides operations for an installed component.

For example, the `windows-vm` component contributes:

```text
nimbus windows status
nimbus windows setup
nimbus windows start
nimbus windows connect
nimbus windows stop
nimbus windows purge-data
```

Runtime commands do not install missing components implicitly. If
`windows-vm` is not selected and applied, the command explains that the
component must first be added to the machine configuration.

## 5. Resolution

Nimbus resolves configuration deterministically.

```text
machine
  -> selected profiles
  -> profile components
  -> explicitly selected components
  -> component dependencies
  -> resources
  -> post-install tasks
  -> runtime command groups
```

Resolution does not inspect the current machine. The same source configuration
always resolves to the same desired graph.

System inspection happens after resolution and produces observed state.

Resolution rejects:

- unknown profiles
- unknown components
- dependency cycles
- duplicate IDs
- incompatible components
- contradictory desired states
- invalid package exclusions
- exclusions of technical dependencies
- invalid provider configuration
- unsupported schema versions
- invalid package constraints
- path traversal
- system file sources that escape the Nimbus tree
- unsafe or ambiguous removal definitions

Profiles passed to Chezmoi preserve their resolved IDs and ordering.

## 6. Package model

Nimbus is Fedora-specific. A bare package name therefore uses the normal DNF
lifecycle by default.

```toml
packages = [
  "git",
  "chezmoi",
  "mise",
  "ripgrep",
]
```

The default lifecycle is:

- inspect with RPM and DNF
- install from enabled repositories
- update through DNF
- remove with DNF when Nimbus has recorded ownership and removal is safe
- preserve the native DNF install reason
- verify the installed package after mutation

A separate package definition is only required when a package has exceptional
behavior:

- a required COPR or RPM Fusion repository
- an external artifact source
- a pinned digest
- a coordinated update group
- special verification
- special removal
- a package-specific warning
- a non-DNF lifecycle

```toml
schema = 1
id = "akmod-nvidia"
provider = "dnf"
package = "akmod-nvidia"
repository = "rpmfusion-nonfree"
update_group = "desktop-session"

[verify]
kernel_modules = [
  "nvidia",
  "nvidia_drm",
]
```

Nimbus does not maintain a generic multi-distribution package-name catalog.
If another distribution is added later, its resource model must be based on a
real implementation rather than speculative mappings.

### 6.1 Explicit packages and exclusions

Machine manifests may add packages outside the selected profiles and
components.

Removing an explicitly selected package removes it from the machine's
`packages` list.

Removing a package contributed by a profile or component adds it to
`package_exclusions`.

An exclusion is valid only when the package is not:

- required by another selected component
- required as a technical dependency
- protected by the Fedora base
- required for Nimbus itself
- required for the selected graphical session

Nimbus shows the configuration change before writing the machine manifest.
It never commits or pushes the change.

### 6.2 Constraints

Package constraints are explicit and visible in the machine configuration.

A constraint is accepted only if Nimbus can enforce it through the package
provider. A constraint that affects planning but not normal native updates is
invalid.

Nimbus must not claim that a package is constrained while allowing ordinary
DNF upgrades to violate the constraint.

### 6.3 Coordinated updates

Packages that form one compatibility-sensitive system may belong to an update
group.

The initial coordinated group is `desktop-session`, covering the parts that
must remain compatible across a Hyprland and Noctalia update.

A coordinated update:

1. resolves all candidate package versions
2. shows the complete transaction
3. creates the required recovery point
4. applies the packages in one native transaction where possible
5. runs group verification
6. reports logout or reboot requirements
7. records one group-level result with individual resource receipts

## 7. Desired, observed, and applied state

Nimbus keeps three states separate.

```text
Nimbus repository and machine manifest
                    |
                    v
                 desired

live Fedora inspection
                    |
                    v
                 observed

/var/lib/nimbus receipts
                    |
                    v
              last applied
```

### Desired state

Desired state comes only from the selected machine, profiles, components, and
resource definitions.

State files never become a source of desired configuration.

### Observed state

Observed state is inspected from native system tools.

Examples:

- RPM package database
- DNF repository configuration
- Flatpak state
- systemd unit state
- file contents, ownership, mode, and digest
- group membership
- kernel command line
- hardware facts
- Secure Boot state
- Btrfs layout
- component-specific verification

### Applied state

Applied state records successful Nimbus operations.

It allows Nimbus to distinguish:

- a desired resource already present before Nimbus adopted it
- a resource installed by Nimbus
- a Nimbus-owned resource that is no longer desired
- an unmanaged resource
- an interrupted or partially completed operation
- an operation requiring recovery or reboot

Applied state does not replace live inspection.

## 8. State and receipts

Authoritative state is stored under:

```text
/var/lib/nimbus/
  state.json
  receipts/
  plans/
  postinstall/
  recovery/
```

The initial state format is versioned JSON. SQLite is not required unless a
future concrete requirement cannot be satisfied safely with atomic JSON files.

A resource receipt records:

- state schema version
- Nimbus version
- Nimbus definition version or commit
- definition digest
- machine ID
- resolved profiles
- resource ID
- contributing profile or component
- provider
- previous observed state
- intended state
- exact operation
- verification result
- plan digest
- recovery-point ID, when relevant
- timestamp
- reboot or logout requirement

Receipts are written only after successful verification.

A failed operation never produces a successful receipt. Previously completed
independent operations retain their accurate receipts after a partial failure.

State contains no secrets.

## 9. Status

```text
nimbus status
```

`status` is the normal read-only system overview.

It reports:

- selected machine
- selected profiles and components
- definition version or commit
- whether the Nimbus definition checkout is dirty
- system drift
- missing desired resources
- changed Nimbus-owned files
- Nimbus-owned resources no longer desired
- unmanaged resources that Nimbus can identify
- failed verification
- pending post-install tasks
- required reboot or logout
- Dotfiles initialization and drift
- missing Mise runtimes as an informational user-scope warning
- incomplete previous operations
- available recovery information

Status does not mutate the system, update repositories, acquire `sudo`, or
synchronize Git.

## 10. Plan

```text
nimbus plan
nimbus plan --upgrade
nimbus plan --prune
nimbus plan --out FILE
```

A plan is always read-only.

It contains:

- resolved machine configuration
- relevant observed facts
- resources to install
- resources to change
- resources to adopt
- Nimbus-owned resources to remove
- unchanged resources
- blocked resources
- manual work
- post-install tasks
- exact privileged commands
- root-owned file diffs
- verification steps
- triggers
- recovery requirements
- reboot and logout requirements
- warnings
- unmanaged prune candidates

The canonical plan excludes volatile values such as display timestamps.

Nimbus hashes the canonical plan. The digest identifies the exact reviewed
operation.

`--out FILE` writes the canonical plan and its digest for later application.

## 11. Apply

```text
nimbus apply
nimbus apply PLAN
nimbus apply --prune
```

Without a saved plan, `apply`:

1. resolves configuration
2. inspects the system
3. calculates the plan
4. prints the complete plan
5. asks for approval
6. recalculates immediately before execution
7. refuses if the plan changed
8. executes the approved operations
9. verifies every completed operation
10. writes receipts
11. reports post-install tasks and reboot requirements

Applying a saved plan repeats inspection and refuses if current state,
configuration, definitions, or the plan digest no longer match.

Plain `apply`:

- installs and repairs desired resources
- adopts already-present desired resources
- removes resources previously owned by Nimbus that are no longer desired
- leaves unrelated unmanaged resources unchanged

`apply --prune` additionally proposes eligible unmanaged resources for
removal.

Pruning requires a separate expanded plan and explicit approval.

An unmanaged package is eligible for pruning only when the native provider can
prove that it was explicitly installed, is not protected, is not required as a
dependency, and has an exact removal and verification path.

## 12. Privilege model

Nimbus runs as the normal user.

Read-only commands never require `sudo`.

Privileged native operations are shown in the plan and executed directly
through `sudo`.

Examples:

```text
sudo dnf install ...
sudo dnf remove ...
sudo systemctl enable ...
sudo systemctl disable ...
sudo grubby ...
```

Nimbus does not run the complete CLI as root, install a root daemon, or
maintain a permanent privileged service.

Nimbus may provide narrowly scoped internal privileged operations for actions
that cannot safely be expressed as one native command:

- atomically installing a reviewed set of root-owned files
- atomically recording root-owned receipts

These internal operations accept only a staged payload tied to the approved
plan digest. They are not general privileged command executors.

## 13. System files and triggers

Nimbus-managed system files are stored below `system/root/` using paths that
mirror their targets.

```text
system/root/etc/modprobe.d/nvidia.conf
system/root/etc/systemd/system/example.service.d/override.conf
system/root/usr/share/wayland-sessions/nimbus-hyprland.desktop
```

A file resource declares:

- source
- target
- owner
- group
- mode
- verification
- change triggers
- removal policy

Nimbus validates that:

- sources remain inside the system tree
- targets are absolute supported system paths
- symlinks cannot escape validation
- the complete diff is shown before apply

Triggers represent bounded native follow-up actions such as:

- `systemctl daemon-reload`
- `dracut`
- `sysctl --system`
- rebuilding a desktop database
- refreshing a package cache

A trigger runs at most once per apply, even when several changed resources
request it.

Triggers are declared by stable ID and fixed argument vectors. Nimbus does not
provide a generic arbitrary-shell trigger.

## 14. Migrations

A migration handles a system transition that cannot be represented safely as
steady-state reconciliation.

Examples:

- replacing one package family with another
- moving data between system-owned locations
- changing a Windows VM storage layout
- replacing an obsolete service
- converting an old Nimbus state schema

A migration has:

- a stable ID
- applicability conditions
- preflight checks
- a reviewed plan
- exact operations
- verification
- recovery instructions
- a completion receipt

Migrations run once for each applicable version transition. A receipt never
overrides verification when the migration's result can be checked directly.

General installation behavior must not be implemented as a growing sequence of
migrations.

## 15. Recovery

Nimbus classifies operations as:

- normal
- disruptive
- reboot-required
- stateful

Before disruptive, reboot-required, or stateful transactions, Nimbus may
create a recovery point when the machine's storage layout supports it.

For the intended Btrfs layout, a recovery point may include:

- a read-only root subvolume snapshot
- a Flatpak subvolume snapshot when system Flatpaks change
- `/boot` archive when kernel or initramfs state changes
- EFI system partition archive when bootloader files change
- a copy of the approved plan
- the related Nimbus receipts

Nimbus records the recovery-point ID in every affected receipt.

Nimbus does not claim automatic rollback support until the restore procedure
has been tested. Manual recovery instructions remain available even when
automatic rollback is unavailable.

## 16. Bootstrap and initialization

The bootstrap has one purpose: make Nimbus runnable on a supported Fedora
installation.

It may install only the tools required to obtain and run Nimbus. All normal
workstation resources are subsequently adopted into Nimbus ownership.

The exact distribution mechanism is unresolved, but the bootstrap must:

1. verify the supported Fedora version and architecture
2. verify that it is running as a normal user with `wheel` access
3. show every repository or artifact it will trust
4. obtain a matched Nimbus executable and system definition
5. verify package signatures or artifact checksums
6. create or select the canonical Nimbus checkout
7. run `nimbus init`

### `nimbus init`

```text
nimbus init
nimbus init --machine desktop
```

Initialization:

1. validates the Nimbus installation and definition compatibility
2. lists the tracked machine manifests
3. selects a machine
4. writes the local Nimbus selection
5. resolves profiles, components, resources, tasks, and commands
6. inspects the system
7. shows the complete installation plan
8. applies only after approval
9. initializes Chezmoi when its package and Git are available
10. offers a Chezmoi diff and apply
11. opens or prints pending post-install work

Local selection is stored under:

```text
~/.config/nimbus/config.toml
```

Example:

```toml
schema = 1
machine = "desktop"
checkout = "/home/pby/.local/share/nimbus"
```

The file selects a tracked machine manifest. It does not duplicate the
manifest's profiles, components, packages, or constraints.

Fresh installation and reinstallation use the same flow. A reinstallation
selects the existing tracked machine definition and converges the new Fedora
installation toward it.

Nimbus can operate without a Dotfiles repository. In that case, the Dotfiles
post-install task remains pending while the system layer remains fully usable.

## 17. Chezmoi integration

Nimbus installs Chezmoi as part of the normal system definition.

The Dotfiles repository does not contain Nimbus machine manifests, Nimbus
profiles, Nimbus components, or system package definitions.

Nimbus passes only the resolved user-facing context Chezmoi needs:

- machine ID
- ordered profile IDs

Conceptually:

```text
Machine=desktop
Profiles=common/development/gaming/hyprland-noctalia
```

The exact transport must use a supported Chezmoi initialization mechanism and
must not depend on Nimbus editing Chezmoi's internal state directly.

The Dotfiles repository may use the machine ID and profiles in templates, but
it does not resolve the Nimbus component graph again.

Machine-specific Dotfiles values remain Dotfiles-owned. They may be expressed
as normal Chezmoi data or template conditions. They are not a second copy of
the Nimbus machine manifest.

### Dotfiles commands

```text
nimbus dotfiles init
nimbus dotfiles status
nimbus dotfiles diff
nimbus dotfiles apply
nimbus dotfiles update
```

These are orchestration commands, not a second Dotfiles engine.

They call Chezmoi as the normal user and preserve Chezmoi's output. Direct
Chezmoi commands remain fully supported.

`nimbus dotfiles apply`:

1. verifies that Chezmoi is initialized
2. checks required integrations such as 1Password readiness
3. shows `chezmoi diff`
4. asks for confirmation
5. runs `chezmoi apply`
6. verifies the expected result
7. updates the related post-install task

Nimbus never applies Chezmoi files with `sudo`.

### 1Password readiness

Secret-backed templates may require:

- 1Password installed
- the user signed in
- CLI integration enabled
- the 1Password SSH agent configured

Nimbus reports these as post-install readiness checks. It does not store,
proxy, or inspect the user's secrets.

Non-secret Dotfiles configuration should remain applicable independently where
the Dotfiles design supports it.

## 18. Hyprland and Noctalia

The `hyprland-noctalia` profile owns the system requirements for the graphical
environment:

- Hyprland package source and package
- Noctalia package source and package
- Wayland utilities
- portals
- greeter
- session entries
- required services
- system integration files
- compatibility-sensitive update grouping
- verification
- logout or reboot reporting

Chezmoi owns the normal user Hyprland and Noctalia configuration.

### Recovery session

Nimbus installs a minimal system-owned recovery session:

```text
/usr/share/wayland-sessions/nimbus-hyprland.desktop
/usr/share/nimbus/seed/hyprland.conf
```

The recovery session:

- launches Hyprland with an explicit system configuration
- opens a terminal
- provides minimal recovery key bindings
- starts Noctalia only when safe and available
- does not depend on the user's Dotfiles
- remains available after Chezmoi is applied
- is removed when the related component is removed

Nimbus never writes a temporary Hyprland configuration into the user's home
directory.

## 19. Post-install system

```text
nimbus postinstall
```

The command opens an interactive list of pending tasks contributed by the
selected components.

Example:

```text
[ ] Sign in to 1Password
[ ] Enable 1Password CLI integration
[ ] Review and apply Chezmoi
[ ] Complete Windows guest setup
[ ] Enroll fingerprint
[ ] Enroll NVIDIA MOK
[ ] Reboot
```

The dashboard and `nimbus status` use the same task model. There is no separate
dashboard-only task database.

Post-install work is reserved for actions that require human interaction or
cannot be safely expressed as normal resources.

The following are not post-install tasks:

- installing packages
- enabling services
- writing system files
- refreshing caches
- rebuilding initramfs
- applying sysctl settings
- registering normal system resources

Those remain planned apply operations.

## 20. Windows VM component

`windows-vm` is an optional Nimbus component.

It owns the host-side requirements for the managed Windows guest:

- virtualization or container backend
- required packages
- system services
- protected configuration
- storage location
- loopback-only management interface
- FreeRDP client
- pinned image release and digest
- host-side verification
- removal policy
- recovery classification

It contributes the `windows-guest-setup` post-install task and the
`nimbus windows` runtime command group.

```text
nimbus windows status
nimbus windows setup
nimbus windows start
nimbus windows connect
nimbus windows stop
nimbus windows purge-data
```

### Setup

`nimbus windows setup`:

1. verifies that the `windows-vm` component has been applied
2. verifies storage and backend readiness
3. starts the protected Windows installation environment
4. opens or prints the loopback installation interface
5. guides the manual Windows installation
6. waits for the guest to become reachable
7. verifies RDP connectivity
8. marks the post-install task complete

### Runtime

`start`, `connect`, and `stop` operate only on the existing managed guest.

They never install missing host resources or recreate missing guest data
without an explicit setup operation.

### Removal

Removing the `windows-vm` component removes managed host integration but
preserves guest data by default.

```text
nimbus windows purge-data
```

`purge-data`:

- prints the exact resolved data path
- verifies that it belongs to the managed Windows component
- stops the guest
- requires a second explicit confirmation
- removes only the named Windows guest data
- records the destructive operation

## 21. Package browser

```text
nimbus packages
nimbus packages search QUERY
nimbus packages list
nimbus packages list --json
nimbus packages add PACKAGE
nimbus packages remove PACKAGE
```

`nimbus packages` opens the interactive package browser.

It distinguishes:

- selected through a profile
- selected through a component
- explicitly selected by the machine
- required as a technical dependency
- excluded by the machine
- installed and managed
- installed but unmanaged
- available from configured repositories
- protected from removal

Search queries configured Fedora and approved third-party repositories without
installing anything.

`add` and `remove` edit desired machine configuration only. They show the TOML
diff and direct the user to `nimbus plan`.

They never install or remove a package directly.

## 22. Dashboard and CLI

Running `nimbus` in an interactive terminal opens the dashboard.

Without an interactive terminal, it prints help and exits.

The dashboard is a user interface over the same commands, resolver, inspection,
plans, and task model used by the non-interactive CLI.

```text
Dashboard:
  nimbus

Setup:
  nimbus init
  nimbus postinstall
  nimbus dotfiles

Daily:
  nimbus status
  nimbus plan
  nimbus apply

Configuration:
  nimbus packages
  nimbus validate

Ownership:
  nimbus managed
  nimbus unmanaged
  nimbus why

Runtime:
  nimbus windows
  nimbus launch

Diagnostics:
  nimbus doctor
  nimbus version
```

The dashboard provides screens for:

- current system status
- selected machine
- profiles
- optional components
- packages
- system plan
- apply
- post-install tasks
- Dotfiles
- Windows VM
- reboot requirements
- recovery information

The dashboard does not implement independent business logic.

## 23. Ownership inspection

```text
nimbus managed
nimbus unmanaged
nimbus why RESOURCE
```

### Managed

`managed` lists resources Nimbus owns or has explicitly adopted.

It shows:

- resource ID
- resource type
- contributing machine, profile, or component
- provider
- desired state
- observed state
- last verification
- last applied plan
- removal support

### Unmanaged

`unmanaged` lists supported system resources Nimbus can identify but does not
own.

It does not inventory arbitrary user data.

Packages are classified as:

- eligible prune candidate
- protected
- dependency
- unknown ownership
- intentionally unmanaged

### Why

`why` explains every path that selected a resource.

Example:

```text
hyprland
  machine desktop
    -> profile hyprland-noctalia
      -> component hyprland
        -> package hyprland
```

When several profiles or components require the same resource, `why` displays
every contributing path.

## 24. Doctor

```text
nimbus doctor
nimbus doctor --explain
```

Doctor is read-only.

It checks:

- Fedora version and architecture
- Nimbus executable and definition compatibility
- selected machine validity
- repository state
- required native commands
- DNF and repository health
- sudo readiness
- state and receipt compatibility
- operation locks
- Btrfs recovery capability
- graphical-session integration
- Chezmoi installation and compatibility
- 1Password readiness
- Windows VM backend health
- broken Nimbus-owned resources
- `.rpmnew` and `.rpmsave` files adjacent to Nimbus-owned configuration

`--explain` shows:

- what Nimbus observed
- why it matters
- which resource owns the requirement
- how to repair it

Doctor never repairs automatically.

## 25. Runtime launch helpers

```text
nimbus launch browser [URL] [--private]
nimbus launch webapp URL
```

These helpers are part of the opinionated Nimbus system and remain available
for Dotfiles key bindings and desktop entries.

They:

- resolve the XDG default browser
- translate private-mode arguments for supported browser families
- support an explicitly configured Chromium fallback for web applications
- parse desktop entries without shell evaluation
- pass arguments as an argv array
- accept only supported URL schemes
- never install a browser implicitly

Chezmoi may reference these commands while retaining ownership of the files
that invoke them.

## 26. Update behaviour

Normal Fedora updates continue to use DNF.

Nimbus controls:

- which managed packages are expected
- which repositories are enabled
- which constraints apply
- which packages must update together
- which updates require recovery points
- which verification runs after an update
- whether a logout or reboot is required

```text
nimbus plan --upgrade
```

shows available updates for Nimbus-managed resources without applying them.

The plan groups compatibility-sensitive resources and distinguishes:

- normal package updates
- constrained packages
- coordinated desktop-session updates
- external artifact updates
- blocked updates
- updates requiring recovery
- updates requiring reboot or logout

Nimbus does not silently update itself, its definitions, the Dotfiles
repository, or system packages.

## 27. Git behaviour

Nimbus system definitions and machine manifests are version-controlled.

Nimbus may read:

- current commit
- working-tree status
- configured origin
- file diffs required for configuration editing

Nimbus does not silently:

- commit
- push
- discard local changes
- reset the checkout
- overwrite a dirty working tree

Any command that updates the Nimbus checkout must show:

- current commit
- target commit
- changed system definitions
- schema compatibility
- whether the working tree is clean

Machine changes made through the dashboard or package commands remain normal
Git changes for the user to review and commit.

## 28. Scripting contract

Read-only commands support `--json` where structured output is useful.

```json
{
  "nimbus_version": "0.1.0",
  "schema": 1,
  "data": {}
}
```

Structured failures contain stable error codes.

Process exit codes remain conventional:

- `0`: success, including an empty result
- `1`: validation, inspection, planning, verification, or operation failure
- `2`: invalid command-line usage

Human output and JSON output use the same underlying data structures.

## 29. Concurrency and locking

Only one mutating Nimbus operation may run at a time.

Nimbus uses an operation lock covering:

- apply
- machine configuration changes
- post-install actions that mutate system state
- Windows setup and destructive Windows operations
- Nimbus checkout updates
- state migrations

Read-only commands may run concurrently when they can obtain a consistent
snapshot of configuration and state.

Interrupted locks include enough metadata to identify the operation and process.
Nimbus never deletes a lock merely because it is old without checking whether
the owning process still exists.

## 30. Security rules

- Nimbus runs as a normal user.
- The whole CLI never runs as root.
- Read-only commands never invoke `sudo`.
- Plans and receipts contain no secrets.
- Every privileged command appears in the reviewed plan.
- External artifacts require an approved source and cryptographic digest.
- Remote shell scripts are not a resource type.
- System file paths are validated against traversal and symlink escape.
- Destructive operations name their exact targets.
- Guest management interfaces bind to loopback unless explicitly configured
  otherwise.
- Nimbus never disables Secure Boot automatically.
- Secret values remain in their owning secret manager.
- Failed verification prevents a success receipt.
- Unknown resource ownership prevents automatic removal.
- Dirty repositories are reported and never overwritten silently.
- Native package signatures remain enabled and enforced.
- Nimbus records the exact definition digest used for every apply.

## 31. Invariants

The following are system invariants and must be testable:

1. The same machine definition and Nimbus definition version resolve to the
   same desired graph.
2. Resolution does not inspect or mutate the current machine.
3. Read-only commands perform no writes and invoke no privilege escalation.
4. Every mutation belongs to a reviewed plan.
5. Applying a plan whose inputs changed is refused.
6. Every successful mutation is verified before its receipt is written.
7. A failed mutation is never recorded as successful.
8. Nimbus removes only resources it owns or resources explicitly approved
   through pruning.
9. Direct Chezmoi usage works without Nimbus.
10. Removing Chezmoi leaves the system bootable through the Nimbus recovery
    session.
11. Removing Nimbus does not delete Dotfiles or user data.
12. Machine hardware detection produces warnings and variants, not silent
    component selection.
13. Post-install tasks come from selected components and use the same status
    model everywhere.
14. Runtime commands never install missing components implicitly.
15. Windows guest data survives normal component removal.
16. No arbitrary privileged script can be introduced through a profile or
    machine manifest.
17. Nimbus-owned file changes are visible as diffs before application.
18. State files never become desired configuration.
19. A package constraint is rejected unless it affects native package updates.
20. Machine manifests and system definitions remain normal reviewable Git
    files.

## 32. Non-goals

Nimbus is not:

- a replacement for Chezmoi
- a general Dotfiles engine
- a Terraform-compatible system manager
- a general Ansible replacement
- a universal Linux installer
- a generic provider platform
- a plugin marketplace
- a background reconciliation daemon
- a package manager
- a package store
- a secret manager
- a container orchestrator
- a generic virtual-machine manager
- a tool for arbitrary remote hosts
- a system for silently removing manually installed software
- a live disk partitioning or encryption tool
- a promise of support for unknown forks

Nimbus may gain additional personal components, machine definitions, desktop
profiles, or Linux backends when they are required by the owner's real
systems.

## 33. Open questions

### Nimbus distribution

How should the executable and its matching system definitions be distributed?

Options include:

- a signed RPM from a personal COPR
- a GitHub release containing the binary and definitions
- a canonical Git checkout with a locally installed binary
- a combination where COPR installs the binary and the matching Nimbus
  checkout supplies personal definitions

The selected mechanism must keep the executable and definition schema
compatible and must not make ordinary personal configuration changes
unnecessarily difficult.

### Nimbus repository updates

Should Nimbus manage updates to its own canonical checkout, or should Git
remain entirely manual?

If Nimbus provides an update command, it must refuse dirty checkouts and show
the exact current and target commits before changing anything.

### Machine manifest editing

Should interactive commands edit the tracked machine manifest directly, or
write a reviewed local override that is later promoted into Git?

Direct editing is simpler and keeps one source of truth. A local override
allows experimentation without immediately changing the tracked manifest but
introduces another configuration layer.

### COPR ownership

If Nimbus is distributed through COPR, should the COPR contain:

- Nimbus only
- Nimbus and selected workstation packages
- custom packages required by the Hyprland and Noctalia stack

Nimbus must not become responsible for rebuilding upstream packages without a
clear maintenance reason.

### Package definitions

Should exceptional package metadata use separate files under `packages/`, or
remain inline inside components?

Separate definitions improve reuse and provenance. Inline definitions reduce
indirection for a personal Fedora-only system.

### DNF5 constraints

Can DNF5 version locking represent the desired comparison ranges, or should
Nimbus support only exact native locks?

The accepted machine-manifest syntax must match what DNF can actually enforce
during ordinary updates.

### Desktop-session update group

Which exact packages, repositories, system files, and verification checks
belong to the `desktop-session` update group?

The group must cover Hyprland, Noctalia, greeter, portal, and session
compatibility without unnecessarily blocking unrelated updates.

### Flatpak scope

Should Nimbus manage Flatpaks system-wide, or should user Flatpaks belong to
Chezmoi or remain manually managed?

The ownership rule must avoid two tools controlling the same Flatpak
installation.

### Mise installation

Should Chezmoi run `mise install` automatically when the Mise configuration
changes, should Nimbus expose a post-install task, or should runtime
installation remain a direct manual Mise operation?

Chezmoi should own the configuration file. Mise must remain the lifecycle owner
of the runtimes.

### Chezmoi handoff

Which supported Chezmoi mechanism should carry `Machine` and `Profiles` during
initialization?

The mechanism should:

- work non-interactively through Nimbus
- still support direct interactive `chezmoi init`
- avoid Nimbus editing Chezmoi's internal state
- avoid duplicating Nimbus machine manifests in the Dotfiles repository

### Chezmoi application during init

Should `nimbus init` offer to run `nimbus dotfiles apply` immediately after the
system apply, or should Dotfiles application always remain a separate
post-install action?

The system must handle 1Password not being ready without leaving the user
without a usable session.

### Recovery session

Does the Fedora Hyprland package and selected greeter reliably honor an
explicit system configuration path for the Nimbus recovery session?

The session must be tested from a clean installation without any user
Dotfiles.

### Recovery retention

How many recovery points should Nimbus keep, and which operation should remove
old recovery data?

Retention must account for available disk space and avoid deleting the only
known-good recovery point.

### Recovery restoration

Should Nimbus initially provide only documented manual restoration, or expose
a guarded `nimbus rollback` command?

Automatic rollback must not be added before the complete restore path has been
tested.

### Fedora upgrades

How should Nimbus handle upgrades between Fedora releases?

The design must define:

- compatibility checks
- repositories that must be disabled or changed
- package constraints
- COPR compatibility
- recovery requirements
- when a Nimbus release is required before the Fedora upgrade

### Secure Boot and NVIDIA

What is the supported NVIDIA Secure Boot workflow?

Nimbus must not disable Secure Boot. Key generation, MOK enrollment, module
verification, and recovery must be explicit.

### Windows backend

Is Dockur the permanent Windows guest backend, or should the component use
libvirt directly?

The selected backend must support:

- reproducible host setup
- loopback-only management
- persistent guest storage
- reliable RDP access
- safe host-component removal
- explicit guest-data destruction

### Windows data location

Which exact filesystem path and Btrfs subvolume should hold the managed Windows
guest data?

The path must support snapshots, explicit preservation, and safe
`purge-data`.

### Full operating-system installation

Should Nimbus remain a post-Fedora installer, or eventually include a
Kickstart, Anaconda profile, or installation image for the base operating
system?

Any full installer must remain separate from normal live-system reconciliation
and must not introduce disk mutation into `nimbus apply`.
