# Nimbus architecture

How the code is organized and how a command flows through it. The product
contract lives in [SPEC.md](SPEC.md); this file explains the shape of the
implementation that delivers it. It grows one section per phase as
[ROADMAP.md](ROADMAP.md) delivers them.

## Layout

~~~text
cmd/nimbus/            main: calls cli.Execute and exits with its code
internal/cli/          Cobra command tree, output rendering, exit codes
internal/definitions/  desired configuration: load, validate, resolve, hash
internal/facts/        observed system: read-only inspection behind a Source
internal/doctor/       health checks over facts, each with its remediation
internal/plan/         desired versus observed: operations, digest, previews
internal/state/        applied state: receipts, baseline, journal, record action
internal/apply/        executes the plan: lock, native steps, receipts
internal/selector/     the local selector and the checkout origin check
internal/version/      engine build identity and supported schema numbers

nimbus.toml            schema, supported Fedora releases, repositories
machines/              one manifest per workstation
profiles/              user-facing bundles: packages and components
components/            shared capabilities: packages, removals, files
system/root/etc/       sources of Nimbus-owned files below /etc
system/keys/           repository signing keys with no public URL

docs/                  contracts, policy, roadmap, tasks, decisions
tools/package-query/   throwaway Fedora container for package research
~~~

Everything under `internal/` is private to this module. Dependencies point
one way: `cli` uses `definitions`, `facts`, `doctor`, `plan`, `apply`,
`state`, `selector`, and `version`; `doctor` uses `facts`; `plan` uses
`definitions`, `facts`, and `state`; `apply` uses `plan`, `facts`, and
`state`;
`facts` uses `selector` for the origin read; `definitions` uses `version`
for the engine check; nothing imports `cli`, and `facts` never imports
`definitions`, so observed state cannot leak into desired state.

## The flow of `nimbus validate`

~~~text
selector (or --checkout)
  -> canonical checkout root      one EvalSymlinks, reused everywhere
  -> origin check                 .git/config read directly, no git command
  -> Load                         strict TOML, boundary walk, entries
  -> Validate                     schema, references, graph, repositories
  -> Resolve, per machine         ordered profiles, closure, provenance
  -> Digest                       SHA-256 over the sorted entries
  -> render                       human text or the JSON envelope
~~~

1. `cli` decides where the checkout is. With `--checkout` it uses that path;
   otherwise `selector.Load` reads `~/.config/nimbus/config.toml` and
   `selector.Verify` compares the checkout's `remote.origin.url` with the
   approved origin. The path is canonicalized once and that root is passed
   through every later step.
2. `definitions.Load` reads `nimbus.toml` and walks `machines/`, `profiles/`,
   `components/`, and `system/`. Every regular file becomes an `Entry` with
   its content and Git-style mode; symlinks and special files are errors.
   Definition files are decoded with unknown fields rejected, and each file's
   `id` must match its name.
3. `definitions.Validate` checks the root file, every repository, every
   profile, component, and machine, and the `requires` graph, then resolves
   every machine so graph-level problems surface in the same run.
4. `definitions.Resolve` turns one machine into its desired graph: components
   from profiles, explicit selections, and the `requires` closure; packages
   with every path that selected them; exclusions, removals, files, and the
   repositories the packages need. Output lists are sorted so the same input
   always produces the same result.
5. `definitions.Digest` hashes the entries in path order with the framing
   SPEC.md specifies.
6. `cli` renders the result and maps the outcome to exit 0, 1, or 2.

## The flow of `nimbus doctor`

~~~text
selector (or --checkout)  -> canonical root, definitions loaded for the
                             supported releases; failures become checks
facts.Inspect(Source)     -> one Section per fact family, unknown on error
doctor.Run(facts, config) -> nine checks with observation, impact, fix
render                    -> human lines or the JSON envelope; exit 1 on fail
~~~

`facts.Source` is the only door to the host: `Run` executes a command
without a shell, `ReadFile`, `ReadDir`, and `LookPath` read the filesystem.
`ExecSource` is the real one; `FakeSource` replays recorded Fedora output
and fails on anything not recorded, so a test can never reach the host by
accident. Each fact family is a `Section` holding a value or the reason it
is unknown. Doctor treats unknown as a separate state from failure.

## The flow of `nimbus sync`

~~~text
loadSelected            -> canonical root, validated checkout, one machine
dnf5 makecache          -> current metadata, as the user; failure is reported
facts.Inspect(Source)   -> installed packages, repository files, Flatpak
plan.Build(inputs)      -> sources to prepare, packages to adopt or install,
                           declared removals, Flatpaks, prune candidates,
                           update information, digest
render, Proceed? [Y/n]  -> --plan stops here; -y or --json skips the question
apply.Run(sources)      -> DNF drop-in, repositories, Flatpak remote; refresh
apply.Run(plan)         -> per operation: native steps through Source with
                           their output on the terminal, verification by
                           re-inspection, receipts through `internal record`
apply.Upgrade           -> dnf5 upgrade, flatpak update (unless -n)
report                  -> operations applied, differences from the plan
~~~

The planner asks DNF for its own view of each transaction with
`dnf5 --assumeno --cacheonly install ...`, which prints the resolved table
and aborts. It parses that table and notes what goes beyond the definitions:
an undeclared install or removal, a package from another repository, an
upgrade the requested packages need. A package whose repository does not
exist yet waits for the source preparation of the same run. Only the
operations are hashed into the digest; prune candidates and update
information are informational and volatile.

`status`, `managed`, `unmanaged`, `why`, `profiles list`,
`components list`, and `packages installed` are views over the same
resolver and facts. Until receipts exist, "managed" means desired and
installed, which sync would adopt.

The executor never runs a shell. Privileged commands are the exact argv
the plan holds, prefixed with `sudo`, streamed to the terminal. Keys are
verified with `gpg` before any privileged command touches them. A DNF
install is one `dnf5 install`; afterwards the installed set is compared
with the preview and the differences are reported, not refused. Receipts go
through the hidden `nimbus internal record` action, which validates the
stage against the plan digest and writes atomically below `/var/lib/nimbus`;
it is the one privileged action of this phase. The selection commands edit a
manifest in memory, validate and plan it, show the diff and plan, ask once,
and then write the file and sync without system updates.

## Rules the code keeps

- **Read-only.** Nothing writes a file or invokes sudo except `sync`
  without `--plan`; the only network step of the read-only path is the
  metadata refresh at the start of sync. `definitions` and `selector` run
  no command at all;
  `facts` and `plan` run native read-only commands only through `Source`,
  so a test can see every one of them. Tests run the loader against a
  read-only tree to prove the first part.
- **Errors are collected, not thrown.** `definitions.ErrorList` carries every
  problem with its checkout-relative path so `validate` reports all of them
  at once. Structural load errors are reported first; semantic checks run on
  a tree that decoded cleanly.
- **Desired, observed, and applied stay apart.** `definitions` knows only the
  checkout. Observed facts and receipts arrive in later packages and never
  feed back into it.
- **Strict data, small types.** Files carry only the fields current
  definitions use; a new field enters the schema together with a definition
  and a fixture that exercise it.
- **Thin command layer.** `cli` holds no logic beyond argument handling and
  rendering, so the same resolver serves tests, later commands, and the
  dashboard.

## Data model

`Ref` is a parsed package reference: a prefix and a name. Bare names get the
`dnf` prefix; every other prefix must be a repository declared in
`nimbus.toml`, except `flatpak`, which maps to the single Flatpak remote.
`Resolved` is the per-machine result: ordered profiles, sorted components and
packages with their selection paths, removals, files with derived `/etc`
targets, and the repositories in use. The JSON envelope wraps any result with
the engine version and output schema number.

## Where later phases attach

- Phase 5 adds bootstrap, `init`, the hardware detector, and the Chezmoi
  handoff.

Each arrives as its own package with its own tests, and `cli` stays a thin
layer over them.

## Testing

Tests build small checkouts in temporary directories from an in-memory base
tree and mutate one thing per case, so every rejection has a fixture. One
test validates and resolves the real definitions in this repository. Nothing
reads or creates `~/.config/nimbus` or `/var/lib/nimbus`.
