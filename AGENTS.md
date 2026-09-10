# Nimbus

Nimbus is a personal, opinionated Fedora workstation installer and system
manager. It turns a supported Fedora base into the system its owner wants,
keeps that system inspectable, and makes changes through plans it shows first
rather than hidden automation.

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
- **plan** is the complete set of operations sync shows before it asks. A
  mutation that neither the plan nor the closing report shows is a bug.
- **receipt** records one verified operation. A failed operation never gets a
  successful receipt.

## What matters

### 1. Simple, explicit systems

Prefer the smallest model that makes correct behavior obvious. Do not preserve
complexity merely because it already exists, and do not add layers, options, or
extension points for imagined future needs.

Configuration and code should say what the machine is meant to be. Avoid
special cases hidden in control flow when the behavior belongs in typed data.

### 2. Show before mutation

A user must be able to see what Nimbus observed and what it intends to
install, upgrade, and remove before it runs, and to learn afterwards what
differed from that. Every mutation path must follow the preview and approval
contract in `docs/SPEC.md`. A mutation that neither the plan nor the report
shows is a bug.

Read-only work stays read-only. Do not add incidental writes, privilege
escalation, network access, or native-tool mutation to validation, inspection,
status, or planning paths.

### 3. Native tools stay visible

Use Fedora and established specialist tools through their supported interfaces.
Nimbus coordinates and verifies them; it should not obscure their transactions
or grow weak replacements for tools that already own a lifecycle.

### 4. Failure and repair

Stateful changes need defined ownership, verification, removal and failure
behavior. Unknown ownership blocks deletion. Test the supported repair path;
do not require a general snapshot or rollback subsystem for ordinary changes.
Never claim restoration works without testing it.

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

- `docs/SPEC.md` owns product behavior, commands, installation, system-file
  ownership, security and the short architecture/test policy.
- `docs/ROADMAP.md` owns implementation order, deferred scope and exit criteria.
- `docs/TASKS.md` owns the current checklist, open decisions, evidence and risk.
- TOML definitions own the software inventory. `system/` holds the root-owned
  configuration and assets we change; user configuration belongs to Chezmoi.
- `README.md` is the entry point; tool-local READMEs explain their own usage.

Keep shared contract changes consistent across SPEC, ROADMAP and TASKS without
copying their contents into this file. Do not add separate product documents.
Older designs are history, not active requirements; revalidate before reuse.

## Neighbouring repositories

- `~/git/dotfiles` is the Chezmoi source state. It owns everything below
  `$HOME` except the selector, works without Nimbus on Linux, macOS, and
  Windows, and gates only Nimbus-calling targets on `managed_by_nimbus`. Its
  `PROFILES.md` owns the handoff prompt keys. Do not add a machine manifest,
  system package list, or component graph there. Chezmoi owns the native Mise
  configuration, including its Cargo tool list, and invokes `mise install`
  after applying it. Dotfiles scripts never install system packages or escalate
  privileges.
- `~/git/docs` is history and earlier Nimbus designs, not a source of truth.
- `~/git/niriland` is a reference configuration. Never import it wholesale.
- For root-owned settings, research Fedora/upstream first and compare Omarchy,
  CachyOS-Settings or other relevant repositories as described in SPEC. Adapt
  only justified system changes; user files remain in the dotfiles repository.

## Research tools

`just package-search QUERY` runs `dnf5 search` in a throwaway container with
Fedora 44, RPM Fusion, and Terra enabled; `docker run --rm
nimbus-fedora-packages repoquery ...` answers anything else. It changes no host
package or repository configuration; it leaves a Docker image and build cache
that `docker image rm nimbus-fedora-packages` removes. Results are evidence
for a decision, never desired state; accepted packages go into profiles,
components, or a manifest, prefixed with their declared repository when it is
not Fedora.

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
- The complete local gate is `just check`: `gofmt`, `go vet ./...`,
  `go test ./...`, `git diff --check`, and markdownlint.
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
