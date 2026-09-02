# Nimbus CLI

This file defines the intended user-facing CLI. `SPEC.md` remains the product
contract.

## Command groups

```text
Dashboard:
  nimbus

Setup commands:
  init
  postinstall
  dotfiles

Daily commands:
  status
  plan
  apply

Configuration commands:
  packages
  validate

Ownership commands:
  managed
  unmanaged
  why

Runtime helpers:
  windows
  launch

Diagnostics:
  doctor
  version
```

The root help groups commands by user task so advanced functionality does not
obscure the normal workflow. Running `nimbus` in a terminal opens the Bubble
Tea dashboard. Without a terminal it prints help and exits.

Profiles, components, packages, inspection, and state are part of Nimbus'
internal model. The TOML files remain directly editable for users who do not
want the interface.

## Distribution and bootstrap

Nimbus is one Go binary distributed as a signed Fedora RPM through the personal
COPR repository. The RPM includes the binary, built-in profiles, catalog,
completions, and manual pages. DNF owns installation, upgrades, and removal;
Nimbus does not contain a self-updater.

The repository contains a small `bootstrap` script that:

1. Verifies Fedora 44 and x86_64.
2. Shows the COPR project and package that will be trusted.
3. Verifies DNF5 COPR capability, enables the COPR, and installs Nimbus,
   Chezmoi, and Git through DNF. The exact package that provides COPR support
   on Fedora 44 is verified in the test environment rather than assumed to be
   part of the Anaconda base.
4. Drops privileges and runs `nimbus init`.

These are the only packages installed before a Nimbus plan. A later apply
adopts them into normal Nimbus ownership. The bootstrap does not clone Nimbus,
install the rest of the workstation, or duplicate setup logic. The separate
COPR packaging repository owns the RPM spec and publishing.

Built-in definitions live as versioned TOML in `profiles/`, `components/`, and
`catalog/`. They are embedded in the Go binary at build time, so an installed
Nimbus version always resolves the same built-in data. Changing these files
requires a new Nimbus release and COPR package build; `dnf upgrade` delivers the
new definitions with the new binary. Nimbus never updates them directly from
Git or the network.

## Configuration

The dotfiles repository stores machine manifests outside its Chezmoi source
root:

```text
machines/
  desktop.toml
  laptop.toml
home/
  # Chezmoi-managed source state
```

`machines/<id>.toml` is the canonical desired configuration. Manifests are
plain, versioned TOML files, not templates. Nimbus creates
`~/.config/nimbus/machine.toml` as a link to the selected manifest, so normal
commands keep one stable default path. Nimbus owns the manifest and link;
Chezmoi owns the surrounding checkout and its Git lifecycle. Nimbus shows file
changes but never commits, pulls, or pushes them. Authoritative state under
`/var/lib/nimbus` stays on the machine and is never tracked.

```toml
schema = 1
profiles = ["common", "development", "hyprland-noctalia"]
components = []
packages = ["ripgrep", "btop"]
package_exclusions = []

[package_constraints]
hyprland = ">=0.56, <0.57"

[dotfiles]
repo = "https://github.com/Furyfree/dotfiles.git"
```

- `profiles` selects reusable bundles.
- `components` enables optional capabilities such as `windows-vm`, `libvirt`,
  or fingerprint support.
- `packages` adds catalog IDs or provider-qualified native packages.
- `package_exclusions` removes selected packages contributed by a profile on
  this machine.
- `package_constraints` sets a package's version on this machine with
  comparison syntax (`>=0.56, <0.57`, `=1.2.3`). The catalog carries no
  constraints; this is the only place versions are set. It applies however the
  package is selected and never defines sources or lifecycles.

Nimbus validates and resolves configuration before inspecting or changing the
system. Invalid references, cycles, conflicts, exclusions, or schema versions
stop before planning.

## Init and dashboard

```text
nimbus init
nimbus init REPO
nimbus init REPO --machine ID
nimbus init --repo REPO --machine ID
nimbus
```

`init` is the explicit first-run command. It refuses to replace an existing
configuration silently. If compatible Nimbus configuration already exists, it
opens the normal dashboard instead.

The repository locator may be passed as a positional argument, mirroring
`chezmoi init`, or through `--repo`. `--machine` selects the machine ID
explicitly. Both may be omitted in a terminal and entered in the setup
interface; declining the repository selects the no-repository flow. The
repository locator is fetched only after it is shown for
review, and it must match the repository recorded by an existing manifest.
Nimbus makes the source available without applying any home-directory targets.

`init` performs exactly one Git read: a clone of the provided locator directly
into the Chezmoi source directory. It never commits, pulls, or pushes.
`chezmoi init` then detects that existing repository and does not fetch again.
An existing checkout that is dirty or stale is reported and refused, never
reconciled, and a later origin divergence from the manifest record is a
visible warning, not a mutation.

### Fresh install

Use this flow when the selected machine ID does not exist in the dotfiles
repository:

1. Run `bootstrap`; it installs Nimbus, Chezmoi, and the required Git transport.
2. `nimbus init` asks for the dotfiles repository and a new machine ID.
3. Select profiles, components, and extra packages.
4. Review and create `machines/<id>.toml` in the dotfiles checkout.
5. Review and apply the complete Nimbus system plan.
6. Review the new manifest as a normal Git diff and commit it manually.
7. Run `chezmoi diff`, then `chezmoi apply` when the dotfiles are ready.

### Reinstall

Use this flow when `machines/<id>.toml` is already tracked:

1. Run the same `bootstrap` on the clean Fedora installation.
2. `nimbus init` asks for the dotfiles repository, lists the tracked manifests
   in `machines/`, and the user selects one; `--machine ID` selects it
   explicitly.
3. Nimbus loads and validates the tracked manifest without rewriting it.
4. Review and apply the complete Nimbus system plan.
5. Run `chezmoi diff`, then `chezmoi apply` when the dotfiles are ready.

### Without a dotfiles repository

Use this flow when there is no dotfiles repository yet:

1. Run the same `bootstrap` on the clean Fedora installation.
2. Run `nimbus init` without a repository locator.
3. Select profiles, components, and extra packages.
4. Review and write the plain local manifest to
   `~/.config/nimbus/machine.toml`.
5. Review and apply the complete Nimbus system plan.
6. Initialize Chezmoi's local config with the resolved profiles so a later
   repository adoption restores the same selection.

When a dotfiles repository is adopted later, add `machines/<id>.toml`, run
`nimbus init REPO`, and select the tracked manifest. Nimbus then links the
tracked manifest over the local file and shows the diff for review; the local
file is superseded, never silently merged.

Nimbus never applies dotfiles before the system plan. User configuration may
depend on shells, applications, and desktop resources installed by Nimbus.

The dashboard is the main interface after initialization. It shows current
status and provides focused screens for profiles, optional components,
packages, plan, apply, and pending post-install work.

The initial Bubble Tea flow is:

1. Check Fedora, architecture, existing Nimbus state, and bootstrap tools.
2. Select the dotfiles repository and machine ID, then fetch its source without
   applying targets.
3. Load an existing machine manifest, or select profiles, components, and extra
   packages for a new one.
4. Initialize Chezmoi's local config with the resolved profiles without
   applying dotfiles.
5. Review the machine manifest diff and complete system plan.
6. Choose whether eligible unmanaged packages should be pruned.
7. Approve apply or exit without changing the system.

Configuration is saved atomically after its review. Before approval, Nimbus
writes no machine manifest and changes no system state. Exiting from the plan
screen keeps the reviewed configuration but performs no system changes.

Deselecting every optional component is always a valid outcome. `common` is
default-selected, and every selection is reversible until approval. The plan
carries explicit warnings when the selection leaves the machine without a
desktop session, and when inspection detects NVIDIA hardware without a
matching selected component. Warnings never change selections silently.

The package screen distinguishes:

- selected through one or more profiles
- selected explicitly for this machine
- installed and managed by Nimbus
- installed but unmanaged
- excluded on this machine
- required as a technical dependency

Deselecting an explicit package removes it from `packages`. Deselecting a
package contributed by a profile adds it to `package_exclusions`; the profile
and its other packages remain selected. A technical dependency or a package
required by another selected source cannot be excluded.

Deselecting a profile removes resources contributed only by that profile.
Resources shared with another profile, component, or explicit package remain
selected.

## Packages

```text
nimbus packages
nimbus packages search QUERY
nimbus packages list
nimbus packages list --json
nimbus packages add PACKAGE
nimbus packages remove PACKAGE
```

`packages` without a subcommand opens the Bubble Tea package browser. It offers
selected, available, installed, unmanaged, and excluded views with fuzzy
search, multi-select, and a package preview. `search` opens the same browser
with `QUERY` prefilled and queries Fedora and configured RPM Fusion repositories
without installing anything.

`list` remains non-interactive for scripts and pipes. It shows whether each
package comes from a profile, an explicit machine selection, or an exclusion,
and whether it is installed, managed, unmanaged, or required as a dependency.
`--json` emits the versioned envelope described under
[Scripting contract](#scripting-contract).

`add` and `remove` change desired configuration only. Removing a package
contributed by a profile creates an exclusion and leaves the rest of the
profile selected. Commands print the resulting config change and direct the
user to `nimbus plan`; they never install or remove packages directly.

## Scripting contract

Every read command accepts `--json` and emits a versioned envelope:

```json
{
  "nimbus_version": "0.1.0",
  "schema": 1,
  "data": {}
}
```

Failures carry structured error codes inside the envelope, such as
`ambiguous_reference` or `unsupported_provider`, instead of additional process
exit codes.

Process exit codes stay conventional:

- `0`: successful execution, including an empty result
- `1`: validation, configuration, inspection, or operation failure
- `2`: invalid command-line usage

## Validate

```text
nimbus validate
nimbus validate --config FILE
```

`validate` loads machine, profile, component, and catalog data and reports
every schema, reference, cycle, conflict, and exclusion error with its source
location. It reuses the same loader and validation code as
`nimbus config resolve`, creates no second validation path, performs no system
inspection or mutation, and exits nonzero on any error, which makes it
suitable for local checks and CI.

## Status

```text
nimbus status
```

Status is the quick daily view. It summarizes desired, observed, and
last-applied state without producing the full execution plan. It reports drift,
pending installs and removals, prune candidates, failed verification, required
reboots, and unfinished post-install tasks.

Status performs no writes. When detail is needed, it points to `nimbus plan`,
`nimbus managed`, `nimbus unmanaged`, or `nimbus doctor`.

## Plan

```text
nimbus plan
```

Plan is read-only. It validates and resolves configuration, inspects the
machine, and shows:

- packages and resources that will be installed or changed
- Nimbus-managed resources that are no longer desired and will be removed
- unchanged and adopted resources
- blocked or manual operations
- eligible unmanaged packages as a separate prune-candidate list
- privileges, warnings, verification, recovery, and reboot requirements

Prune candidates are informational during `plan`. They are not normal removal
actions and are never included by a later plain `nimbus apply`.

## Apply

```text
nimbus apply
nimbus apply --prune
```

Apply recalculates and prints the complete current plan before asking for
approval. It hashes the canonical plan when it is shown, recomputes the plan
immediately before execution, and refuses to run when the digest changed.
Canonical plan data excludes volatile values such as timestamps. The approved
digest is stored in the resulting receipt so applied operations trace to the
exact reviewed plan. The digest guard supplements, never replaces,
provider-specific transaction and precondition checks.

Plain `apply` installs desired resources and removes resources previously owned
by Nimbus that are no longer desired. It leaves unmanaged resources unchanged.

`apply --prune` promotes eligible prune candidates to removal actions, shows
the exact expanded plan, and requires explicit approval. The Bubble Tea review
screen exposes the same behavior as a prune toggle.

An unmanaged package is eligible only when the native provider proves it was
explicitly installed, it is not protected or required as a dependency, and
Nimbus has an exact removal and verification path. Unknown ownership blocks
pruning.

Nimbus runs each privileged native command directly through `sudo`, verifies
the result, and atomically records only successful operations in versioned
state and receipts under `/var/lib/nimbus` through an operation-scoped
privileged step. Each receipt records the Nimbus version, embedded-definition
digest, exact lifecycle, and verification result. Partial failures stop
dependent operations and retain accurate recovery information.

When adopting bootstrap packages, Nimbus preserves DNF install reasons and uses
`dnf mark user` only for an exact package when required. It never broadly marks
dependencies or packages belonging to the Anaconda-installed base.

## Post-install work

```text
nimbus postinstall
```

Post-install work is interactive or manual setup that cannot be completed as a
normal declarative resource. The command opens a Bubble Tea list derived from
the selected components and applied state. Completed tasks disappear unless
their verification later fails.

Typical tasks are:

```text
[ ] Review and apply Chezmoi dotfiles
[ ] Set up the Windows guest
[ ] Enroll a fingerprint
[ ] Log out or reboot
```

Each task has a stable owner, prerequisites, status check, instructions,
completion receipt, and recovery path. `postinstall` does not scan for or run
arbitrary scripts. Direct component commands remain available, such as
`nimbus dotfiles bootstrap`, `nimbus windows setup`, and a future fingerprint
enrollment command.

Cache refreshes, service changes, database rebuilds, and other non-interactive
work are normal planned apply operations, not post-install tasks.

### 1Password readiness

Dotfiles templates that render from 1Password fail until 1Password is signed in
with CLI integration enabled. Recording the integration choice during init
does not require `op` to be present. When post-install verification detects
the not-ready state, Nimbus shows:

```text
1Password is not ready.

Sign in to 1Password and enable CLI integration, then run:
  chezmoi diff
  chezmoi apply

Or continue without secret-backed templates:
  chezmoi --skip-secrets diff
  chezmoi --skip-secrets apply
```

Nimbus does not wrap Chezmoi's secret handling. A normal SSH-agent
configuration still applies because it contains no secret and becomes usable
once 1Password and its agent are ready.

## Doctor

`nimbus doctor` is read-only. It checks Fedora and architecture support,
configuration and state compatibility, required native commands, repository
availability, privilege readiness, Chezmoi compatibility, locks, and broken
Nimbus-owned resources. It prints remediation without applying it.

`--explain` reports, for each failed check, the observation, impact, and
remediation using the same reconcilers as provider health reporting.

For Nimbus-owned RPM configuration, doctor reports adjacent `.rpmnew` and
`.rpmsave` files. Nimbus never merges or deletes them automatically.

## Ownership

```text
nimbus managed
nimbus unmanaged
```

`managed` lists resources recorded as owned or explicitly adopted by Nimbus,
including their profile or machine source and last verification result.

`unmanaged` lists supported resources that Nimbus can identify but does not
own. It does not attempt to inventory arbitrary user data. Eligible packages
are marked as prune candidates, while protected packages, dependencies, and
unknown ownership are clearly excluded from pruning.

Both commands are read-only. Stopping management without removing a real
resource is an advanced recovery operation and is not part of the normal
command surface yet.

## Why

```text
nimbus why RESOURCE
```

`why` is a targeted read-only provenance lookup. It explains whether a resource
was selected explicitly, contributed by a profile or a component, required as a
technical dependency, or adopted from existing state, and shows every
contributing path when multiple selections require the same resource.

Phase 1 explains desired configuration from the resolved graph. Phase 2 adds
observed installation and Nimbus ownership information once system inspection
exists. The command never mutates configuration or system state. `--json`
emits the versioned envelope.

## Chezmoi

The minimal bootstrap installs Chezmoi so Nimbus can restore a tracked machine
manifest before the full system plan. The explicit initialization or recovery
command is:

```text
nimbus dotfiles bootstrap
```

`nimbus init` may invoke the same operation. It verifies the configured
repository and may run:

```text
chezmoi init --promptMultichoice 'Profiles=<resolved profiles>' <repo>
```

The `--promptMultichoice` value is the selected profile list, slash-separated.
Its key must match the template's prompt text exactly. The dotfiles config
template consumes the selection with `promptMultichoiceOnce`, so Nimbus
supplies it without prompting and direct `chezmoi init` prompts for the same
profiles interactively. The template also records the 1Password SSH intent
without requiring the `op` binary.

Nimbus never adds `--apply`, synchronizes the repository, or hides normal
Chezmoi commands.

The `hyprland-noctalia` profile also ships one Nimbus-owned seed session
file: a single `hyprland.lua` rendered from the machine's desktop selection,
with minimal keybinds and `exec-once noctalia --daemon`. Plan checks whether
Chezmoi manages that path and includes the seed write only when it does not.
When `chezmoi apply` later takes the path over, the seed yields: Nimbus
reports the path as Chezmoi-managed and stops restoring its own version. This
is a bounded first-login fallback, not a dotfiles deployment feature.

## Windows

`windows-vm` is an optional component selected in the setup interface. Apply
installs its reviewed packages, KVM integration, protected Dockur configuration,
and FreeRDP client.

```text
nimbus windows status
nimbus windows setup
nimbus windows start
nimbus windows connect
nimbus windows stop
nimbus windows purge-data
```

The Dockur image is pinned by release and digest. RDP and the recovery web
interface bind to loopback. Removal preserves guest data by default.
`purge-data` names the resolved data path and requires a second confirmation.

Host installation belongs to plan and apply. `windows setup` is the interactive
post-install workflow that starts the protected container, opens the loopback
installation interface, and verifies that the guest becomes reachable. Daily
runtime commands never install a missing Windows component implicitly.

## Desktop helpers

```text
nimbus launch browser [URL] [--private]
nimbus launch webapp URL
```

The browser helper resolves the XDG default browser and translates private-mode
arguments for supported browser families. The webapp helper uses the default
browser when compatible or an explicitly configured Chromium-family fallback.

Helpers parse desktop entries without shell evaluation, pass arguments as an
argv array, and accept only `http` and `https` URLs by default. Chezmoi may call
them from key bindings and desktop entries while retaining ownership of those
files.

## Internal model and safety

- Desired configuration, observed state, and last-applied state remain
  separate.
- Nimbus runs as the normal user and never runs the whole CLI as root.
- Profiles resolve into components; components resolve into typed resources.
- Declarative changes use plan and apply. Interactive follow-up work uses typed
  post-install tasks. Daily operations use runtime helpers.
- Nimbus does not expose a generic command for arbitrary scripts or actions.
- External repositories, images, and artifacts are pinned catalog sources, not
  Git submodules.
- Nimbus never guesses removal behavior or removes an unknown dependency.
- Nimbus never stores secrets, partitions a live system, runs `chezmoi apply`,
  or performs silent Git operations.
