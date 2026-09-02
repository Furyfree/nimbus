# Repository instructions

## Purpose

Nimbus is a personal, opinionated Fedora workstation installer and system
manager. This repository owns both its Go engine and the versioned definitions
for the systems it manages.

The first target is post-install Fedora 44 on x86_64. The separate dotfiles
repository owns Chezmoi source state and user configuration.

## Start with context

- Read SPEC.md, DECISIONS.md, ROADMAP.md, TASKS.md, and this file before changing
  architecture, scope, or implementation.
- Inspect the current branch, base, upstream state, worktree, relevant files,
  and existing validation before editing.
- Treat existing user changes as intentional and preserve unrelated work.
- Keep one bounded outcome per change.
- Resolve uncertainty from repository evidence. Ask only when plausible choices
  materially change the result.

## Architecture

- Keep machines, profiles, components, catalog entries, system files, migrations,
  and Go code in this repository.
- Read live definitions from the selected Nimbus checkout. Do not embed live
  personal definitions in the engine; embedding is allowed only for a future
  example tree used to create a new checkout.
- Keep the regular local selector at ~/.config/nimbus/config.toml. Nimbus owns
  it; it is not a symlink or Chezmoi-managed file. It identifies the checkout,
  reviewed origin, and machine without duplicating the machine manifest.
- Allow the selected checkout root to be a symlink, resolve it to one canonical
  directory before use, and reject symlinks or special files inside the
  definition boundary.
- Profiles select components. Components may require other components. Profiles
  never import profiles.
- Keep package-specific behavior in catalog data, never package-name branches in
  Go. A bare Fedora package name uses the typed default DNF lifecycle; catalog
  entries describe exceptions.
- Restrict the generic system-file provider to regular files below /etc. Derive
  each target from its source below system/root/etc; use native packages or a
  separately specified typed resource for every other target root.
- Resolve desired configuration without inspecting the current machine.
- Keep desired, observed, and last-applied state separate.
- Implement inspection and planning before mutation.
- Keep resolution and fact collection as internal engine capabilities. The
  public CLI exposes validate, status, plan, doctor, and focused ownership or
  package workflows rather than raw engine debug commands.
- Make package install and remove convenience commands update reviewed desired
  configuration and reuse the same planner and apply path; never add a second
  package lifecycle.
- Keep install.sh as the minimal curl entry point. It may obtain Git and the
  trusted checkout, then must hand control to the checkout's versioned
  bootstrap script; neither script owns Chezmoi or normal system provisioning.
- Delegate Fedora release upgrades permanently to native DNF5 tooling. Keep
  `nimbus upgrade` within the installed release, report release compatibility,
  and refuse system mutation on unsupported releases.
- Keep post-install work typed and component-owned. Browser and webapp launchers
  are narrow runtime helpers; they never become a generic command runner.
- Support only the reviewed Btrfs recovery topology and system-libvirt Windows
  guest. Preserve home and guest data outside Nimbus system recovery.
- Prefer native system tools and established specialist tools over custom
  replacements.

## Ownership

- Nimbus owns packages, repositories, system services, system files, hardware
  integration, boot policy, profiles, planning, applied state, receipts, and
  recovery.
- Chezmoi owns files and templates below the user's home directory except the
  Nimbus local selector, plus its normal diff, apply, edit, and update
  lifecycle.
- Nimbus may install Chezmoi and perform one explicit first initialization. It
  must not run normal Chezmoi apply or update operations or reimplement Chezmoi.
- Mise owns runtimes declared in its Chezmoi-managed configuration. Nimbus may
  install Mise and report missing runtimes, but it has no user-scope runtime
  provider.
- Chezmoi owns the development-profile-gated onchange action that invokes Mise
  as the normal user with `MISE_SYSTEM_DEPS=warn`; Nimbus never invokes that
  action or repairs missing user runtimes itself.
- Nimbus owns machine manifests under machines/. The dotfiles repository must
  not contain a second machine manifest, component graph, or package catalog.
- Nimbus may read local Git metadata. Outside the explicit bootstrap and first
  Chezmoi initialization, it never causes a clone or fetch. It never commits,
  pulls, pushes, resets, stashes, or resolves Git conflicts.

## Safety

- Run Nimbus as the normal user; refuse the whole CLI when invoked as root.
- Read-only system commands never invoke sudo or mutate the system.
- Read-only plan commands never write files or state.
- Show every privileged operation and system-file diff in the reviewed plan.
- Use direct sudo for native commands. Narrow internal subcommands may only
  install an approved atomic system-file payload or record approved root-owned
  state; never add a general privileged executor, daemon, or sudo keepalive.
- Validate system paths against traversal, symlink escape, and unsupported
  targets.
- Never store credentials, keys, tokens, sessions, private runtime state, or
  secret values in configuration, plans, logs, state, or receipts.
- Never guess removal commands or delete paths Nimbus cannot prove it owns.
- Define verification and recovery before enabling a mutating resource.
- Use the normal user's lock at `$XDG_RUNTIME_DIR/nimbus/operation.lock` for
  every Nimbus mutation and hold it through verification and receipt recording.
- Keep Windows guest data below `/var/lib/libvirt/images/nimbus/windows/`;
  normal removal preserves it and only explicit double-confirmed purge may
  delete proven owned data.
- Never repartition or encrypt a mounted live system.
- Never disable Secure Boot automatically.

## Repository map

- machines/ contains versioned machine manifests.
- profiles/ contains user-facing composition bundles.
- components/ contains reusable capabilities and their resources.
- catalog/ contains package definitions with non-default behavior.
- system/ contains Nimbus-owned system file sources, migrations, and triggers.
- install.sh is the minimal remote bootstrap entry point; bootstrap is the
  checkout-owned installer handoff.
- cmd/ and internal/ contain the Go engine.
- history/ contains superseded designs and dated research, never active
  requirements or task status.
- tools/ contains development-only helpers.

## Validation

Run focused tests while iterating and just check before handoff. Once Go code
exists, the complete gate must run gofmt, go vet ./..., go test ./..., and
git diff --check.

Run destructive integration tests only in disposable Fedora virtual machines.
Test recovery before enabling the corresponding real mutation.

Inspect the final diff and untracked files. Report exact commands, failures,
skipped checks, and unavailable tools.

## Documentation

- SPEC.md owns the complete accepted system requirements and boundaries. It is
  phase-independent.
- DECISIONS.md records architectural rationale and unresolved questions without
  replacing accepted contracts, phase order, or current work.
- ROADMAP.md owns phase order, phase-specific decisions, risks, recovery,
  validation, exit criteria, and the placement of decision gates.
- TASKS.md owns current checkboxes, evidence, blockers, and residual risk.
- README.md is the short user-facing entry point.
- Supporting material and history must not become an alternative owner for
  requirements, phase order, or current status.

When a shared contract changes, update SPEC.md, ROADMAP.md, and TASKS.md
together without mixing their responsibilities.

## Authorization

Local implementation and documentation edits require a user request. Commit,
push, pull-request creation or update, merge, installation, deployment,
privileged operations, and infrastructure apply each require separate explicit
authorization.

## Code review rules

- Treat commands, paths, permissions, ownership, recovery, and documentation as
  behavior that must match the implementation.
- Reject a mutation absent from the reviewed plan or lacking ownership,
  verification, and recovery.
- Errors gain context as they propagate and render once at the command boundary.
- Every receipt records the engine version, definition commit and tree digest,
  plan digest, exact lifecycle, and verification result.
- Report .rpmnew and .rpmsave files next to Nimbus-owned configuration; never
  merge or delete them automatically.
