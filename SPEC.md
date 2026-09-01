# Nimbus specification

## Problem

A workstation needs reproducible system setup without mixing privileged system
management with personal dotfiles. Existing shell installers make ownership,
drift, removal, and partial failure difficult to inspect.

## Outcome

Nimbus resolves declarative workstation intent, inspects Linux system state,
shows a complete plan, and applies only reviewed operations with verification
and recovery information.

## Users

Nimbus is built first for the owner's Linux workstations. Others may inspect or
adapt it, but the project does not promise a generic installer for every setup.

## Ownership

Nimbus owns:

- the machine profile graph and canonical component catalog
- packages, repositories, services, system files, hardware, and boot resources
- inspection, planning, apply, verification, removal, applied state, and logs
- installation and explicit first bootstrap of Chezmoi

Chezmoi owns:

- user files and templates below the user's home directory
- its source checkout and normal diff, apply, and update lifecycle
- platform and profile conditions that affect only user files

The dotfiles repository may also contain Nimbus-owned machine manifests under
`machines/`, outside its Chezmoi source root. Chezmoi owns the surrounding
checkout and Git lifecycle but does not deploy or edit those manifests. Nimbus
may edit a selected manifest after showing its diff, but never commits, pulls,
or pushes the repository; the one Git read Nimbus performs is the init-time
clone or fetch defined under Configuration and state. Manifests are plain,
versioned TOML files, not templates.

Nimbus passes the ordered resolved profile IDs to Chezmoi unchanged. It does
not own a second dotfiles profile list, file database, template language, or
backup system.

## Profiles and packages

The initial profiles are:

- `common`
- `development`
- `gaming`
- `hyprland-noctalia`

Later desktop profiles may add `hyprland-dms`, `niri-noctalia`, and `niri-dms`.
Profiles group canonical components. The catalog owns Fedora package names,
sources, versions, install behavior, and other package-specific details.

GPU hardware is not a profile. Inspection reports detected hardware; a
selected graphics component declares the desired capability and resolves the
compatible provider variant. Detection must not silently enable optional
components.

Built-in definitions are versioned TOML files under `profiles/`, `components/`,
and `catalog/`. These files are the source of truth and are embedded in the Go
binary at build time, so an installed Nimbus always resolves exactly the
definitions of its release. There are no user catalog overrides. During
development, Nimbus runs from the working tree and uses the edited files
directly; delivering changed definitions to installed machines requires a new
Nimbus release and package build. Nimbus does not fetch profile or catalog
updates independently from Git or the network.

The catalog holds curated, reusable definitions. A machine manifest can add
ad-hoc packages directly through `packages` with provider-qualified native
entries, so personal one-off packages never require a catalog entry or a new
release.

Catalog entries carry update policies. Coordinated update groups apply related
packages as one reviewed transaction; the initial `desktop-session` group
covers the Hyprland, Noctalia, greeter, portal, and session integration stack.
`plan` shows the complete transaction; `apply` requires the defined snapshot,
verification, and logout or reboot reporting. The group's exact membership,
package sources, and verification checks are tracked in
[OPEN_QUESTIONS.md](OPEN_QUESTIONS.md).

The `hyprland-noctalia` profile ships a Nimbus-owned seed session file: a
single `hyprland.lua` rendered from the machine's desktop selection, with
minimal keybinds and `exec-once noctalia --daemon`. It is a first-login
fallback, not a dotfiles engine. Nimbus writes it only when Chezmoi does not
manage that path; once `chezmoi apply` takes the path over, the seed yields
and Nimbus reports the path as Chezmoi-managed instead of restoring its own
version. This is the only home-directory file Nimbus writes on its own
behalf.

Version constraints live only in machine manifests. `package_constraints`
entries use comparison syntax such as `>=0.56, <0.57` or an exact `=1.2.3`,
evaluated by the provider that owns the source with the provider's native
version ordering. The catalog carries no constraints. Profiles never pin
versions; Go code contains no package-specific constraints.

## Configuration and state

- `<dotfiles checkout>/machines/<id>.toml` is the canonical, Git-trackable
  machine configuration.
- `~/.config/nimbus/machine.toml` is a Nimbus-owned link to the selected
  manifest and remains the default CLI path. In the no-repository workflow it
  is a plain Nimbus-owned manifest instead of a link.
- `nimbus config resolve --config FILE` selects an explicit alternative.
- Built-in profiles and catalog entries ship with Nimbus.
- `/var/lib/nimbus/` records verified applied state and receipts, including the
  exact lifecycle needed to inspect or remove it later. It lives inside the
  root filesystem so root snapshots restore the system and Nimbus' record of it
  together. Read-only commands read non-secret state without elevation;
  mutating commands write state atomically through an operation-scoped
  privileged step. User-specific cache stays under the user's XDG paths.

Machine manifest shape:

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

Desired configuration never comes from `state.json`. Phase 1 is read-only and
must not create or update the state file.

On a fresh installation, Nimbus fetches an explicitly selected dotfiles source
without applying it, creates a reviewed `machines/<id>.toml`, and selects it.
On a reinstallation, Nimbus lists the tracked manifests, the user selects one,
and Nimbus verifies that its recorded repository matches the bootstrap input.
Nimbus accepts SSH and HTTPS locators for the same repository and normalizes
them before matching. The dotfiles repository is currently public, so init
fetches it anonymously over HTTPS; a private repository instead requires
whatever credential the transport needs, entered interactively and never
stored. The bootstrap installs only Nimbus, Chezmoi, and Git. Init performs
exactly one Git read - a clone of the provided locator directly into the
Chezmoi source directory - and leaves the working tree untouched; a checkout
that is dirty or stale is reported and refused, never reconciled.
`chezmoi init` then detects that existing repository and does not fetch again,
so the bootstrap performs one fetch in total. Afterward the user and Chezmoi
own all Git operations, and a checkout origin that later diverges from the
manifest record is a visible warning, not a mutation. The full Nimbus plan and
apply run before the user reviews and runs `chezmoi apply`.

`nimbus init` also completes without a dotfiles repository. In that workflow
it creates a reviewed, plain local manifest at `~/.config/nimbus/machine.toml`
instead of a link, still records the resolved profile selection in Chezmoi's
local config for a later repository adoption, and runs the same review, plan,
and apply path. Adoption into a tracked `machines/<id>.toml` is documented
manual work: once a repository is selected, init links the tracked manifest,
shows the diff, and the local file is superseded.

## Required behavior

- Load versioned TOML machine, profile, component, and catalog data.
- Resolve imports deterministically and reject cycles, missing references,
  duplicate IDs, and incompatible intent. Profiles import components only;
  profiles never import other profiles.
- Keep configuration resolution independent of the current machine.
- Inspect actual state without mutation before producing a plan.
- Show privileges, risk, warnings, manual steps, and reboot requirements before
  apply.
- Use typed providers and native system tools instead of package-specific Go
  branches.
- Record the exact source and lifecycle used for successful mutations.
- Refuse unsafe removal when ownership or the recorded removal path is unknown.
- Keep Chezmoi directly usable when Nimbus is absent.
- Provide a standalone read-only `nimbus validate` that reuses the resolver's
  loader and validation and performs no system inspection or mutation.
- Explain resource provenance with a read-only `nimbus why RESOURCE` lookup.
- Emit machine-readable output through a versioned JSON envelope containing the
  Nimbus version, output schema, data, and structured error codes. Process exit
  codes stay conventional: 0 success including an empty result, 1 failure, 2
  invalid usage.
- Hash the canonical plan when it is shown for approval, refuse apply if the
  digest changed, and record the approved digest in the resulting receipt.
  Canonical plan data excludes volatile values such as timestamps.
- From the first mutating implementation, version state and receipts and record
  the Nimbus version, embedded-definition digest, exact resource lifecycle, and
  verification result.
- Own system locale and the console keymap as explicit `common` resources.
- Treat selecting no optional component as valid, and surface selection gaps
  such as a missing desktop session or NVIDIA hardware without a matching
  component as explicit plan warnings rather than silent defaults.

The Phase 1 CLI uses Cobra. TOML decoding uses `go-toml/v2`. Bubble Tea is
reserved for a later interactive TUI and is not a Phase 1 dependency.

During dotfiles bootstrap, Nimbus passes the resolved profiles to `chezmoi
init` through Chezmoi's native `--promptMultichoice` argument. The flag value
is the selected profile list, slash-separated, and its key must match the
template's prompt text exactly. The dotfiles config template consumes the
selection with `promptMultichoiceOnce` and persists it in Chezmoi's own config;
direct `chezmoi init` prompts for the same profiles interactively. The template
also records whether 1Password SSH integration is desired, without requiring
the `op` binary to be present. This initialization may happen before the full
system plan so an existing machine manifest can be restored. Nimbus does not
run `chezmoi apply`.

## Security and recovery

- Plans and configuration must not contain secrets.
- Remote scripts or artifacts require pre-approved integrity evidence.
- Privilege elevation is operation-scoped.
- Nimbus runs as the normal user and refuses to run the whole CLI as root. It
  retains the invoking user's identity and home explicitly for user-scoped
  work.
- Read-only commands run without `sudo`. Each privileged native command is run
  directly through `sudo`; Nimbus does not install a root daemon or maintain
  its own sudo session.
- A failed operation must not be recorded as successfully applied.
- Stateful resources require a tested recovery or explicit manual recovery
  path before apply support is added.
- Nimbus never repartitions or encrypts a mounted live system.
- The boot provisioner refuses unsupported Secure Boot mutations and never
  disables Secure Boot automatically. On a Secure-Boot-enabled machine, NVIDIA
  MOK enrollment is explicit manual post-install work with verification; a
  fully managed Secure Boot path is deferred.

## Package research

The Fedora 44 query container under `tools/package-query/` is a development
tool for searching Fedora and RPM Fusion repository metadata. It does not
define desired state and is not part of the Nimbus runtime.

## Compatibility

- Nimbus supports Linux only.
- The first system target is Fedora 44 Everything on x86_64.
- The first desktop integration target is Hyprland with Noctalia.
- Other Linux backends are added only after the Fedora resource model is stable.
- Dotfiles may remain cross-platform; Nimbus itself manages Linux systems.

## Non-goals

- a dotfiles deployment or template engine
- a package format, package store, or background reconciliation daemon
- silent Git operations in the dotfiles checkout
- live disk repartitioning or root encryption
- automatic internet discovery during apply
- universal Linux or hardware support in the first release

## First release acceptance

`nimbus config resolve` loads versioned TOML, resolves the selected profile
graph deterministically, emits stable human and JSON output, includes the
Chezmoi handoff data, reports actionable source locations, and performs no
system inspection or mutation. The standalone `nimbus validate` reuses the
same loader and validation, is usable as a local or CI check, and performs no
system inspection or mutation.

Unresolved decisions are tracked in [OPEN_QUESTIONS.md](OPEN_QUESTIONS.md).
