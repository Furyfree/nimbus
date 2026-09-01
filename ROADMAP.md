# Nimbus roadmap

This file owns implementation order. Current checkboxes and evidence live in
[TASKS.md](TASKS.md).

Current phase: **1. Configuration resolver**.

## Principles

- One profile graph and one owner for each resource.
- Inspect and plan before apply.
- Native tools remain visible and usable without Nimbus.
- Recovery is part of a mutating feature, not follow-up work.
- Chezmoi owns user files.

## 1. Configuration resolver

### Outcome

Build a read-only Go CLI that loads and resolves versioned TOML configuration
without inspecting the workstation.

### Scope

- machine, profile, component, and catalog schemas
- build-time embedding of versioned `profiles/`, `components/`, and `catalog/`
  TOML
- initial `common`, `development`, `gaming`, and `hyprland-noctalia` profiles
- deterministic imports and ordered profile resolution
- validation for cycles, conflicts, duplicates, and missing references
- stable human and JSON output with actionable errors
- a standalone read-only `nimbus validate` reusing the same loader and
  validation code, usable as a local or CI check
- `nimbus why` provenance over the resolved desired graph

Use `~/.config/nimbus/machine.toml` by default, including when it links to a
tracked machine manifest, and support `--config FILE` as an explicit
alternative. Do not add another config file or local profile/catalog override
rules in this phase. The resolver must not write
`/var/lib/nimbus/state.json`.

Use Cobra for commands and `go-toml/v2` for TOML. Do not add Bubble Tea until
an interactive TUI is a scheduled outcome.

### Risk and recovery

The phase is read-only. Reject ambiguous input instead of guessing. No system
recovery is needed.

### Validation

Use unit tests, table-driven invalid cases, golden output fixtures, `gofmt`,
`go vet ./...`, and `go test ./...`.

### Exit criteria

`nimbus config resolve` meets the first-release acceptance criteria in
[SPEC.md](SPEC.md). It performs no system inspection, Chezmoi invocation, or
mutation.

## 2. Inspection and planning

Inspect Fedora 44 Everything on x86_64 and produce a deterministic
desired-versus-actual plan. Include packages, repositories, services, files,
boot/security facts, and Chezmoi bootstrap compatibility. Stop before writes.

Validate manually in the disposable Fedora 44 test VM before enabling mutating
providers on the real workstation. Automated golden-plan regression tests in
CI are deferred until a CI environment exists and are not a Phase 2 acceptance
requirement.

## 3. Providers and apply

Add typed providers, then a small reviewed set of sources, one family at a
time: DNF first (with RPM Fusion and COPR as enabled repositories), then
Flatpak, Mise (runtimes plus npm-, go-, and pipx-backed tools), Cargo, uv, and
verified upstream artifacts with pinned digests. Each provider owns install,
verify, update, and removal for its sources; one provider owns an executable.
Apply immutable plans with scoped privilege, verification,
failure reporting, and source-specific removal. Providers define health and
quarantine with the provider model: a failed provider stays visible, blocks
only its own resources and dependants, and never hides unrelated resources.
`doctor --explain` uses the same reconcilers to report the observation, impact,
and remediation for each failed check.

Apply hashes the canonical plan at approval, recomputes it immediately before
execution, and refuses to run when the digest changed; the approved digest is
recorded in the receipt. After successful verification, Nimbus writes
versioned state and receipts under `/var/lib/nimbus` through an
operation-scoped privileged step, recording the Nimbus version,
embedded-definition digest, exact lifecycle, and verification result. Do not
record failed operations as applied.

Run each privileged native command directly through `sudo`; do not add a root
daemon or custom sudo keepalive. Select the first low-risk official package
fixtures during this phase using `tools/package-query/` and record them in the
catalog, not in this roadmap.

Use the software installed on a reference workstation as migration input, not
as automatic desired state. For each selected component, compare the native
Fedora package, Mise, and Cargo where applicable, then record one supported
provider with explicit install, verify, update, and removal ownership. Chezmoi
may manage a tool's user configuration but must not declare a competing
installation or version.

When adopting bootstrap packages, preserve DNF install reasons and use
`dnf mark user` only for an exact package when required; never broadly mark
dependencies or packages belonging to the Anaconda-installed base.

Destructive tests run only in disposable Fedora virtual machines. No resource
may mutate state before its recovery path is defined.

## 4. Bootstrap and Chezmoi handoff

Implement the minimal bootstrap of Nimbus, Chezmoi, and the required Git
transport. Fetch the configured dotfiles source without applying targets: the
bootstrap clones the repository once, directly into the Chezmoi source
directory, and `chezmoi init` then detects that existing repository without
fetching again. Support both creating a new `machines/<id>.toml` and restoring
an existing one.
Machine manifests are plain, versioned TOML, not templates. On a reinstallation,
`nimbus init` lists the tracked manifests and the user selects one, or passes
an explicit `--machine ID`. Nimbus links its default config path to the
selected manifest.

`nimbus init` also completes without a repository locator: it creates a
reviewed plain local manifest at `~/.config/nimbus/machine.toml`, runs the
same plan and apply path, and documents adoption into a tracked
`machines/<id>.toml` later.

The repository locator is an explicit bootstrap input. A new manifest records
it; an existing manifest must match it. Nimbus runs `chezmoi init <repo>`
without `--apply` and supplies the resolved selection through Chezmoi's native
`--promptMultichoice` flag. Conceptually:

```sh
chezmoi init \
  --promptMultichoice 'Profiles=common/development/hyprland-noctalia' \
  <repo>
```

The flag value is the selected profile list, slash-separated, and its key must
match the template's prompt text exactly. The dotfiles `.chezmoi.toml.tmpl`
consumes the selection with `promptMultichoiceOnce` under the stable `Profiles`
key and writes it into Chezmoi's generated config, so a later init without
Nimbus restores the same selection. Direct `chezmoi init` prompts for the same
profiles interactively. Never run `chezmoi apply` or automatic Git
synchronization.

On a fresh installation, review and create the machine manifest before the
full system plan. On a reinstallation, load the existing manifest unchanged.
In both cases, run the full Nimbus plan and apply before the user reviews and
applies dotfiles. A later Nimbus apply adopts the bootstrap packages into its
normal ownership records.

## 5. Workstation resources

Add services, system files, desktop integration, hardware policy, firewall,
mount, swap, system locale and console keymap, and other resource groups
incrementally. Each group gains apply only after inspection, planning,
validation, and recovery are complete.

Hibernation keeps zram enabled for memory pressure and hibernates to the disk
swap. Inspection verifies that zram is active and that a disk-backed swap with
a working resume target exists; hibernation support reports a manual step when
either is missing.

The coordinated `desktop-session` update group applies the Hyprland, Noctalia,
greeter, portal, and session integration stack as one reviewed transaction
under the catalog update policies in `SPEC.md`. Its exact membership, package
sources, and verification checks require a separate
analysis before catalog entries are implemented; until then, catalog files
must not encode the group.

The `hyprland-noctalia` profile includes a Nimbus-owned seed `hyprland.lua`
with minimal keybinds and `exec-once noctalia --daemon`, rendered from the
machine's desktop selection. Plan writes it only when Chezmoi does not manage
the path; once `chezmoi apply` takes the path over, Nimbus reports the path as
Chezmoi-managed and stops restoring the seed.

## Later

- LUKS2 inspection and optional reviewed auto-unlock
- Fedora systemd-boot, UKI, and encrypted-hibernation provisioning
- a separate Fedora installer ISO using the same Nimbus packages and profiles
- managed Secure Boot support; until then the boot provisioner refuses
  unsupported Secure Boot mutations and MOK enrollment stays manual
- mature Arch and Debian/Ubuntu backends
- reviewed Package Index candidate imports
- `hyprland-dms`, `niri-noctalia`, and `niri-dms` profiles

## Done

- Repository scope, ownership, working rules, roadmap, task tracking, and open
  questions are documented.
