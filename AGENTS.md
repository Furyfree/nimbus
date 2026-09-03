# Nimbus

Nimbus is a personal, opinionated Fedora workstation installer and system
manager. It turns a supported Fedora base into the system its owner wants,
keeps that system inspectable, and makes changes through reviewed plans rather
than hidden automation.

The repository contains the Go engine and the versioned definitions it uses.
The first target is post-install Fedora 44 on x86_64. A separate dotfiles
repository handles user configuration below the home directory.

Nimbus is deliberately not a general-purpose configuration framework. Build
the owner's workstation well before considering abstractions for hypothetical
users, distributions, or providers.

## A small glossary

- **checkout** is the selected clone of this repository that the installed
  engine reads definitions from, normally `~/.local/share/nimbus`.
- **selector** is `~/.config/nimbus/config.toml`: checkout path, machine ID,
  and approved origin. It holds no desired state.
- **machine manifest** is `machines/<id>.toml`: the profiles, components, and
  extra packages one workstation selects.
- **profile** is a user-facing system bundle that lists packages and selects
  components. The dotfiles repository reuses the same profile IDs for user
  configuration.
- **component** is a reusable capability that owns resources, is shared by
  profiles, and may require other components.
- **repository** is an entry in `nimbus.toml`: a package source other than
  Fedora with its pinned key. Its ID is the prefix a package reference uses;
  bare names are Fedora packages.
- **desired, observed, applied** are the three states: checkout definitions,
  live system inspection, and receipts under `/var/lib/nimbus`.
- **plan** is the complete reviewed set of operations. A mutation absent from
  it is a bug.
- **receipt** records one verified operation. A failed operation never gets a
  successful receipt.
- **recovery point** is a Snapper snapshot pair plus Nimbus boot archives and
  manifest. It is same-disk state, not a backup.

## What matters

### 1. Simple, explicit systems

Prefer the smallest model that makes correct behavior obvious. Do not preserve
complexity merely because it already exists, and do not add layers, options, or
extension points for imagined future needs.

Configuration and code should say what the machine is meant to be. Avoid
special cases hidden in control flow when the behavior belongs in typed data.

### 2. Review before mutation

A user must be able to understand what Nimbus observed, what it intends to
change, why the change is needed, and what requires privilege before approving
it. A mutation absent from the reviewed plan is a bug.

Read-only work stays read-only. Do not add incidental writes, privilege
escalation, network access, or native-tool mutation to validation, inspection,
status, or planning paths.

### 3. Native tools stay visible

Use Fedora and established specialist tools through their supported interfaces.
Nimbus coordinates and verifies them; it should not obscure their transactions
or grow weak replacements for tools that already own a lifecycle.

### 4. Recovery is part of the feature

A stateful or destructive capability is incomplete until ownership,
verification, removal, failure behavior, and recovery are defined and tested.
Unknown ownership blocks deletion. Recovery claims require a real restore
drill, not only a successful backup or snapshot command.

## Start with context

- Read this file and the relevant active documents before planning or editing.
- Inspect the current branch, base, upstream state, worktree, relevant files,
  and existing validation.
- Treat existing user changes as intentional and preserve unrelated work.
- Trace affected flows and callers before changing behavior. Fix the root cause
  at the narrowest correct layer.
- Resolve uncertainty from repository evidence. Ask only when plausible choices
  materially change the result.
- Keep one bounded outcome per change. Broad rewrites are a signal to recheck
  scope before continuing.

## Sources of truth

- `docs/SPEC.md` owns accepted product behavior, safety boundaries, and
  invariants.
- `docs/DECISIONS.md` records rationale and unresolved questions.
- `docs/SECURITY.md` owns the workstation security policy: software sources,
  encryption, Secure Boot, SELinux, firewall, privilege, and secrets.
- `docs/ROADMAP.md` owns implementation order, phase gates, risks, recovery,
  and exit criteria.
- `docs/TASKS.md` owns the current checklist, evidence, blockers, and residual
  risk.
- `docs/INSTALLATION.md` is the Fedora base-install operator guide.
- `docs/PACKAGES.md` lists what Nimbus installs, by application; it feeds the
  definitions and shrinks as they land.
- `README.md` is the short user-facing entry point.
- The removed `history/` tree contains superseded evidence, not active
  requirements. Its last complete snapshot is commit
  `c0bb8a4660732e6e9556297da2c15ba0f286ee98`.

Do not duplicate the product contract in this file. When shared behavior
changes, update `docs/SPEC.md`, `docs/ROADMAP.md`, and `docs/TASKS.md` together
while keeping their responsibilities distinct. Inspect removed historical
material from the named commit without restoring it to the active tree, and
revalidate it against the active documents before reuse.

## Neighbouring repositories

- `~/git/dotfiles` is the Chezmoi source state. It owns everything below
  `$HOME` except the selector, works without Nimbus on Linux, macOS, and
  Windows, and gates only Nimbus-calling targets on `managed_by_nimbus`. Its
  `PROFILES.md` owns the handoff prompt keys. Do not add a machine manifest,
  package list, or component graph there.
- `~/git/docs` is history and earlier Nimbus designs, not a source of truth.
- `~/git/niriland` is a reference configuration. Never import it wholesale.

## Research tools

`just package-search QUERY` runs `dnf5 search` in a throwaway container with
Fedora 44, RPM Fusion, and Terra enabled; `docker run --rm
nimbus-fedora-packages repoquery ...` answers anything else. Nothing touches the
host. Results are evidence for a decision, never desired state; accepted
packages go into profiles, components, or a manifest, prefixed with their
declared repository when it is not Fedora.

## Protect the workstation and user

- Do not run bootstrap, installation, apply, package mutation, privileged
  commands, or destructive integration tests on the live workstation unless
  the user explicitly authorizes that exact action.
- Never expose or commit credentials, tokens, keys, sessions, logs, caches,
  private runtime state, or personal data.
- Resolve destructive targets exactly. Reject ambiguous paths, foreign
  ownership, traversal, symlink escape, and unproven removal behavior.
- Never weaken trust, approval, signature, Secure Boot, sandbox, or permission
  controls to make a task pass.
- Do not edit live agent configuration, agent homes, authentication, or runtime
  state unless the user explicitly asks for that exact system-level change.
- This checkout lives on the owner's workstation. Tests, validation, and
  fixtures never read or create `~/.config/nimbus` or `/var/lib/nimbus`; they
  use temporary directories and explicit checkout and machine inputs.
- Run destructive system tests only in disposable Fedora virtual machines.

## Make controlled changes

- Follow existing code patterns, naming, schemas, and tests before introducing
  a new pattern.
- Prefer the standard library, platform capabilities, and dependencies already
  declared by the project.
- Keep schemas strict and versioned. Reject unknown or ambiguous input rather
  than guessing intent.
- Keep desired input, observed facts, and recorded results distinct in code and
  tests.
- Add context as errors propagate and render them once at the command boundary.
- Test meaningful behavior and failure paths, not implementation-shaped
  assertions that merely mirror the code.
- Remove only orphans created by the current change. Leave unrelated cleanup
  for its own bounded task.
- Keep code, comments, and Markdown plain, direct, and proportional. Markdown
  wraps at 80 columns, uses ASCII punctuation and `~~~` fences, and passes the
  markdownlint run inside `just check`.

## Verify honestly

- Run focused checks while iterating and `just check` before handoff.
- Once Go code exists, the complete local gate includes `gofmt`, `go vet ./...`,
  `go test ./...`, and `git diff --check`.
- Read complete errors and relevant logs before fixing their cause.
- Inspect the final diff, staged diff when applicable, and untracked files.
- Report the exact checks run, failures, skipped checks, and unavailable tools.
- Do not claim that CI, review, deployment, recovery, or external state passed
  without current evidence.

## Git and delivery

- Local edits require a user request. Commit, push, pull-request creation or
  update, merge, installation, deployment, privileged operations, and
  infrastructure apply each require separate explicit authorization.
- Use a task-named branch based on the current default branch unless the user
  has authorized work on the existing branch.
- Never discard user changes or use destructive Git operations to clear the
  worktree.
- Use conventional commit titles in plain language and keep one concern per
  commit or pull request.
- Never create or update a pull request, trigger hosted review, or merge unless
  the user explicitly asks.

## Taste

- Favor obvious behavior over clever machinery.
- Fight scope creep and speculative generality.
- Keep native commands, ownership, privilege, and recovery visible.
- Make the safe path the easiest path to understand.
- If these defaults conflict with the task or active contract, surface the
  conflict and get the user's direction instead of quietly choosing a side.
