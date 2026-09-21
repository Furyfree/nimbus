# Nimbus

Personal Fedora workstation installer and system manager: Go engine, TOML
system definitions, Chezmoi user configuration. Supported platforms are in
`nimbus.toml`; this is not a general-purpose configuration framework.

## What must not break

- Preserve preview, approval, locking, ownership and partial-failure rules
  in [SPEC](docs/SPEC.md). Unknown state never proves absence or success.
- Keep user configuration in Chezmoi. Native tools own their application
  state, credentials and removal; Nimbus records verified operations.
- Status and previews must not authenticate, activate services or mutate.

## Map

- `cmd/nimbus/`, `internal/cli/`: command entry points and orchestration.
- `internal/definitions/`: parse, validate and resolve TOML selections.
- `nimbus.toml`, `machines/`, `profiles/`, `components/`: compatibility,
  package sources, machine choices and resource declarations.
- `system/`: managed system files, repository keys and boot payload.
- `internal/inspect/`, `internal/native/`: observe state and invoke tools.
- `internal/plan/`, `internal/apply/`, `internal/state/`: plan changes,
  execute them and record verified ownership.
- `internal/postinstall/`, `internal/agentproxy/`, `internal/launch/`:
  guided setup and desktop helpers.
- `internal/bootmenu/`, `internal/snapper/`: boot-menu and snapshot operations.
- `install.sh`, `install-develop.sh`, `bootstrap`, `tools/install/`:
  bootstrap, signature verification, logging and terminal handoff.
- `internal/native/nativetest/`, `tests/integration/`: isolated native
  substitutes and cross-tool tests; Go tests also live beside their code.
- `tools/`: tool-local READMEs cover VM trials, native DNF checks,
  package queries, boot previews and release preparation.
- `docs/SPEC.md`: behavior contracts. `docs/TASKS.md`: unfinished work.
  README is the entry point; the GitHub wiki holds detailed usage.
- `licenses/`: third-party notices included in releases; retain them.

## Ways to hurt yourself

This checkout may be the installed definitions checkout. Never run install,
apply, package changes or privileged operations on the workstation without
explicit authorization. Use disposable Fedora VMs for system trials.

Tests must isolate HOME and XDG state and select a temporary checkout.
Never read or write the live selector, receipts, credentials or run records.
Preserve signature checks, Secure Boot, SELinux and service ownership checks.

## Verify

Use Go from `go.mod`, Python 3, Ruff, Just, Markdownlint and ShellCheck.
Run `just check` and `just validate`. `justfile` defines the gate; it includes
Go tests, Python lint/format and behavior checks, assets, Markdown and shell
linting. Native DNF tests are opt-in; see their `tools/` READMEs. A passing
unit test does not establish boot, hardware or snapshot recovery behavior.

Use controlled examples for engine tests; validate the real TOML inventory
without asserting personal package choices. Keep Markdown wrapped at 80
columns with ASCII punctuation and `~~~` fences.
