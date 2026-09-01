# Nimbus and dotfiles: final design

Written 2026-09-01. This file consolidates three analyses of the planning
foundation, recorded under `history/` as `2026-09-01-FABLE.md`,
`2026-09-01-GPT-5.6-Sol.md`, and `2026-09-01-GLM-5.3-Flash.md`, plus the
2026-08-31 record already there. Where the analyses disagreed, the owner
decided on 2026-09-01; section 10 lists every decision with its source.

This is the design that `SPEC.md`, `CLI.md`, `ROADMAP.md`, and the dotfiles
`AGENTS.md`, `PROFILES.md`, and `machines/README.md` must be reconciled to
before Phase 1 freezes TOML types. Until that reconciliation, the contract
files remain authoritative for implementation.

## 1. The system in one picture

One Git repository describes a workstation. Two engines apply it. Nimbus
applies the part that needs `sudo`. Chezmoi applies the part under `$HOME`.
Both plan before they apply, both keep the native tools visible, and either
works when the other is missing.

```text
        workstation repository (one checkout: ~/.local/share/chezmoi)
        +------------------------------------------------------------+
        |  machines/<id>.toml   which profiles this machine selects   |
        |  nimbus/              system definitions (data, not code)   |
        |  home/                Chezmoi source state (.chezmoiroot)   |
        +------------------------------------------------------------+
                 |                                     |
        nimbus (Go engine, RPM-signed)         chezmoi (unchanged)
        runs as you, sudo per native command   runs as you, never sudo
                 |                                     |
        validate -> facts -> plan -> apply     init -> diff -> apply
        receipts in /var/lib/nimbus            config in ~/.config/chezmoi
                 |                                     ^
                 +-- chezmoi init --promptMultichoice Profiles=...
                                  --promptString Machine=<id>
```

The engine is generic. The repository is personal. That is the same shape
Terraform and Chezmoi have, and it is the reason someone else can use Nimbus
by writing a repository rather than forking Go. `SPEC.md` still promises
only the owner's workstations for the first release; genericity is a
property of the design, not a support commitment.

## 2. Ownership

The ownership matrix compresses to one sentence with one refinement:

> If applying it needs `sudo`, Nimbus owns it. If it lives under `$HOME`,
> Chezmoi owns the file, and the tool that file configures owns the
> lifecycle it declares.

The refinement matters. Privilege decides who writes; a declarative file
decides who reconciles. `~/.config/mise/config.toml` is a Chezmoi-managed
file, and mise is the lifecycle owner of the tools it lists.

| Thing | Owner | Mechanism |
|---|---|---|
| repositories, RPMs, system Flatpaks | Nimbus | `dnf`, `flatpak --system` |
| `/etc` and `/usr` files, drop-ins, `modprobe.d`, sysctl | Nimbus | `file` provider, sources under `nimbus/root/` |
| services, timers, greeter, portals, seed session | Nimbus | `systemd`, `file` |
| boot, kernel arguments, LUKS inspection, version locks | Nimbus | `boot`, `dnf` |
| `chezmoi`, `git`, `mise`, `1password`, shells, editors | Nimbus | catalog entries in `common` |
| `chezmoi init` with the resolved selection | Nimbus, once at init | section 7 |
| files under `$HOME` | Chezmoi | source state |
| user-scope tools: runtimes, `cargo:`, `npm:`, `pipx:`, `go:` | mise | `~/.config/mise/config.toml`, Chezmoi-managed |
| SSH config, agent key selection | Chezmoi + 1Password | templates |
| credentials, caches, histories, sessions | nobody | never tracked |

Consequences:

- Nimbus has no user-scope provider family. Mise, Cargo, and uv leave the
  roadmap. Nimbus installs `mise` as a package and reports
  `mise ls --missing` in `status` as a hint.
- The dotfiles repository gains one documented exception to "scripts never
  install": `run_onchange_after_mise-install.sh.tmpl`, which hashes the mise
  config and runs `mise install`. User scope, no `sudo`, no DNF. The
  cargo-installed tools on the current workstation move under mise's
  `cargo:` backend or stay unmanaged; the design recommends the backend.
- Nimbus writes under `$HOME` only in `~/.config/nimbus`, `~/.cache/nimbus`,
  and, during `init`, the reviewed checkout itself: the clone and the new
  `machines/<id>.toml`. Chezmoi writes its own config when Nimbus invokes
  `chezmoi init`. Nimbus never manages a user target.

## 3. The workstation repository

The dotfiles repository is the workstation repository. Renaming it is
optional; the layout is not.

```text
dotfiles/
  .chezmoiroot                 home
  .chezmoiversion              2.72.0
  home/                        Chezmoi source state, unchanged
    .chezmoi.toml.tmpl         Profiles and Machine prompts
    .chezmoidata/machines.toml per-machine user data keyed by machine ID
    ...
  machines/                    already here, shape unchanged
    desktop.toml
    laptop.toml
  nimbus/                      moved from the Go repository
    nimbus.toml                schema = 1, min_nimbus = "0.1.0"
    profiles/*.toml
    components/*.toml
    catalog/*.toml
    triggers/*.toml
    root/                      sources for /etc and /usr files
  justfile                     validate, contract test, chezmoi checks
```

Why one repository:

- One clone at bootstrap; `nimbus init` already performs it.
- `chezmoi update --apply=false` pulls system and user intent together.
- The profile IDs are consumed within one repository, so a repository-local
  check can prove `.chezmoi.toml.tmpl` and `nimbus/profiles/` agree.
- macOS and Windows checkouts carry `nimbus/` harmlessly, as they already
  carry `machines/`. `.chezmoiroot` keeps Chezmoi out of it.

The Nimbus Go repository keeps `examples/` with the same layout. It is
embedded in the binary only as seed data for `nimbus init --new` and is
never resolved as live definitions. That is the one deliberate remaining
use of embedding.

### 3.1 Trust and compatibility

Definitions under `nimbus/` are privileged input: they name root-owned files
and trigger argv that Nimbus runs through `sudo`. The design accepts that
with these rules, all of which are tests before they are prose:

- **Trust decision at init.** `nimbus init` shows the locator and, after the
  clone, the commit and the `nimbus/` tree digest, and asks for confirmation
  before the first resolve. The locator is recorded in the manifest; the
  normalized origin is recorded in `~/.config/nimbus/config.toml`. A later
  origin change is reported by `status` and `plan` and blocks `apply` until
  re-confirmed with `nimbus init --trust`.
- **Schema range.** `nimbus/nimbus.toml` carries `schema` and
  `min_nimbus`. The engine publishes the schema range it reads in
  `nimbus version` and in `README.md`, Ignition-style. A definitions tree
  outside the range fails in `validate` with the exact mismatch. Never
  change a schema's meaning; add a schema and a migrator.
- **Canonical digest.** The `nimbus/` tree digest covers file content,
  mode, and path for every regular file, in sorted order. Symlinks under
  `nimbus/` are rejected. Any `source` or `target` that escapes its root
  through `..` or a symlinked parent is rejected in `validate`.
- **Dirty checkouts.** `plan` and `apply` work on the working tree, record
  the commit, a dirty flag, and the tree digest, and print a warning when
  dirty. `init` refuses a dirty or stale checkout it did not create.
- **Exact plan rendering.** Every privileged argv and every root-owned file
  diff is rendered in the plan before approval. No privileged operation
  exists that the plan did not show.
- **CI.** The workstation repository runs `nimbus validate` with a pinned
  engine release and the profile contract test on every change.

Reproducibility is per machine and per commit. A receipt records the Nimbus
version, the checkout commit, the dirty flag, and the tree digest. For a
clean checkout the commit identifies the input; for a dirty one only the
digest does, which is why both are recorded.

## 4. Definitions

Four layers, each answering one question.

| Layer | Question | Terraform analogue |
|---|---|---|
| catalog entry | how is this package installed, verified, updated, removed | provider resource |
| component | what does this capability need: packages, files, services, triggers, manual steps | module |
| profile | which components form a bundle | composition preset |
| machine | what does this box select on top of the bundles | tfvars, workspace |

**The catalog is the exception list, not a registry.** A bare package name
in a component or manifest means the DNF default lifecycle: install from
enabled repositories, verify with `rpm -q`, update with the system, remove
with `dnf remove`. A catalog file exists only for a non-default source,
repository, update group, verify hook, artifact, removal, or note. Golden
tests cover the bare-name path so the default stays deliberate.

```toml
# nimbus/catalog/akmod-nvidia.toml
schema = 1
id = "akmod-nvidia"
provider = "dnf"
repo = "rpmfusion-nonfree"          # component id that must be selected
update_group = "desktop-session"
verify = { modules = ["nvidia", "nvidia_drm"] }
note = "Open kernel modules need kmod-nvidia-open; decided by GPU generation."
```

```toml
# nimbus/catalog/zed.toml
schema = 1
id = "zed"
provider = "artifact"               # the Chezmoi external shape, system scope
url = "https://github.com/zed-industries/zed/releases/download/v0.200.0/zed-linux-x86_64.tar.gz"
sha256 = "<64 hex>"
install_to = "/opt/zed"
link = { "/usr/local/bin/zed" = "/opt/zed/bin/zed" }
```

```toml
# nimbus/components/nvidia.toml
schema = 1
id = "nvidia"
summary = "NVIDIA proprietary stack from RPM Fusion"
requires = ["rpmfusion-nonfree"]
packages = ["akmod-nvidia", "xorg-x11-drv-nvidia-cuda"]

[[files]]
source = "root/etc/modprobe.d/nvidia.conf"
target = "/etc/modprobe.d/nvidia.conf"
on_change = ["dracut"]

[[manual]]
id = "mok-enroll"
when = "facts.secure_boot"
summary = "Import the akmods key with mokutil and enroll it at the next boot."
check = ["mokutil", "--test-key", "/etc/pki/akmods/certs/public_key.der"]

[warnings]
missing_hardware = "No NVIDIA GPU detected; this component installs anyway."
```

```toml
# nimbus/profiles/hyprland-noctalia.toml
schema = 1
id = "hyprland-noctalia"
components = ["wayland-base", "hyprland", "noctalia", "noctalia-greeter",
              "xdg-portals-hyprland", "hyprland-seed-session"]
```

```toml
# machines/desktop.toml  (shape unchanged from SPEC.md)
schema = 1
profiles = ["common", "development", "hyprland-noctalia"]
components = ["nvidia", "windows-vm"]
packages = ["ripgrep", "flatpak:org.signal.Signal"]
package_exclusions = []

[package_constraints]
hyprland = ">=0.56, <0.57"

[dotfiles]
repo = "https://github.com/Furyfree/dotfiles.git"
```

Resolution rules:

- Profiles list components. Components may `requires` components. Nothing
  imports profiles. Cycles, unknown IDs, and duplicate IDs are errors.
- `when` and `warnings` read facts but never select. Membership comes only
  from the manifest. Detection produces warnings, never silent defaults.
- Provider variants such as open versus proprietary NVIDIA modules are
  chosen inside a selected component from facts and shown in the plan.
- Triggers are data: `nimbus/triggers/dracut.toml` holds argv, privilege,
  and whether it implies a reboot. A trigger runs at most once per apply.
- Resolution never reads the machine. `validate` and `config resolve` run
  anywhere the engine runs.

## 5. States and the plan

```text
machines/<id>.toml + nimbus/     ->  resolve   ->  desired
read-only inspection             ->  facts     ->  observed
/var/lib/nimbus/receipts         ->  applied   ->  last applied
desired x observed x applied     ->  plan      ->  ordered steps, warnings
```

- `nimbus validate` runs the resolver without facts. It is the CI gate.
- `nimbus facts` prints inspection as JSON. Its fixtures drive golden plans
  and make a wrong plan debuggable in isolation.
- `nimbus status` reports drift from full inspection, pending manual tasks,
  failed verification, reboot needs, and, as a fast first line, whether the
  definitions digest differs from the last receipt. The fast line never
  replaces inspection.
- `nimbus plan` is read-only. `--out FILE` writes the canonical plan and its
  digest. `nimbus apply FILE` re-inspects, recomputes, and refuses on any
  mismatch. Without a file, `apply` plans, shows, asks, and applies.
- Prune candidates stay informational unless `--prune`.
- `--target`, `--reinstall`, `forget`, and `renamed_from` are deferred until
  dependency closure, receipts, and recovery have been proven in the VM.
  Targeted plans are recovery tools, not the normal path.

## 6. Execution and privilege

The accepted rule stands: each privileged native command runs directly
through `sudo`, visible in the plan and in the sudo log as itself.

```text
nimbus apply (as you)
  1  sudo dnf install ...                          one prompt
  2  sudo nimbus internal write-files STEP DIGEST   root-owned files, atomic
  3  sudo systemctl enable ...                      sudo ticket reused
  4  sudo dracut --regenerate-all --force           trigger
  5  sudo nimbus internal record DIGEST             receipts, atomic
  chezmoi init ...                                  as you, no sudo
```

Exactly two internal privileged subcommands exist, both narrow:

- `internal write-files` writes the root-owned files of one step from a
  payload staged under `~/.cache/nimbus/plans/<digest>/`, verifying the
  payload digest against the plan before touching the filesystem. It exists
  because a multi-file change must be atomic and `install` per file is not.
- `internal record` copies the approved plan into
  `/var/lib/nimbus/plans/<digest>.json` and writes receipts atomically.

Everything else is `sudo <native argv>`. There is no general privileged
plan executor, no daemon, no helper, no keepalive. Steps are ordered so
privileged operations are adjacent and sudo's normal ticket does the rest.

Receipts are written per step, after that step's verification, so a failure
leaves accurate partial state and never a success record. A receipt holds
the Nimbus version, checkout commit, dirty flag, tree digest, plan digest,
resource ID, provider, exact argv, verification result, and the recovery
point ID when one was taken.

Apply re-resolves the DNF transaction immediately before running it and
aborts if it changed, as Niriland's migrations already did.

Fedora correctness rules carried from the 2026-08-31 analysis remain
required: report `.rpmnew` and `.rpmsave` next to Nimbus-owned files and
never merge them; verify RPM-owned files with `rpm -V`; resolve the invoking
user through UID and `SUDO_USER` for user-scoped work; isolate DNF5 output
parsing behind fixtures; own locale and console keymap as `common`
resources.

### 6.1 Recovery points

Before a transaction classified `disruptive` or `reboot`, Nimbus takes a
recovery point using the Btrfs layout in `FEDORA_TEST_INSTALLATION.md`:
read-only snapshots of `root` and, when Flatpaks change, `flatpak`, plus
`/boot` and `/boot/efi` archives when kernel or boot files change. The
classification is per transaction or resource group, not per step. The ID
goes into every receipt of that transaction.

Recovery points are enabled only after a restore drill in the test VM and a
retention policy exist. Rollback is documented manual work first;
`nimbus rollback ID` arrives after the drill.

## 7. The Chezmoi handoff

Terraform calls this an output. It stays as small as an output.

```sh
chezmoi init \
  --promptMultichoice 'Profiles=common/development/hyprland-noctalia' \
  --promptString 'Machine=desktop' \
  https://github.com/Furyfree/dotfiles.git
```

- `Profiles` is the resolved list, unchanged from the manifest, consumed by
  `promptMultichoiceOnce` under the `Profiles` key as today.
- `Machine` is the manifest ID, consumed by `promptStringOnce` under a
  `machine` key. Templates read `.chezmoidata/machines.toml` for user-side
  facts of that machine:

```toml
# home/.chezmoidata/machines.toml
[machines.desktop]
monitors = ["DP-1,3440x1440@144,0x0,1"]
[machines.laptop]
monitors = ["eDP-1,preferred,auto,1"]
```

- Direct `chezmoi init --prompt` answers the prompt or leaves it empty. An
  empty or unknown machine ID selects no per-machine data and templates
  must render valid defaults; that is a template test in the dotfiles
  repository.
- Hardware facts do not cross the handoff. Templates can use `lookPath`
  and `output` when a user file depends on hardware.
- Nimbus never runs `chezmoi apply`, never synchronizes Git, and never
  reads Chezmoi's config back.

**Profile contract test.** The profile IDs are canonical in
`nimbus/profiles/*.toml`. The choice list in `.chezmoi.toml.tmpl` is a
literal because the config template cannot read data files. A
repository-local check in the workstation `justfile` parses both and fails
on any difference. It is not a Nimbus CLI flag: the engine does not learn a
Chezmoi implementation detail.

## 8. Bootstrap and daily use

### 8.1 Bootstrap

Git is bootstrap infrastructure: nothing can be cloned without it. Chezmoi
is needed at the handoff, which happens in `init` before the first apply
can install it. Both are therefore installed with Nimbus, and both are
ordinary `common` catalog entries that the first apply adopts with
`dnf mark user` on exactly those packages.

```text
fresh Fedora 44 Everything, user in wheel
  |
  |  sudo dnf copr enable <owner>/nimbus && sudo dnf install nimbus chezmoi git
  |  (others: release binary with checksum, then dnf install chezmoi git)
  v
nimbus init [REPO] [--machine ID] [--new DIR]
  1. check Fedora 44, x86_64, not root, no existing Nimbus config
  2. obtain the checkout at ~/.local/share/chezmoi
       REPO      -> show locator; one git clone; working tree untouched;
                    show commit and nimbus/ digest; ask to trust
       --new DIR -> write the embedded example tree; run git init,
                    printed, never silent; origin is added later by hand
  3. choose the machine
       machines/<ID>.toml exists -> load, validate, never rewrite
       otherwise                 -> select profiles, components, packages;
                                    review the new manifest; write it
  4. write ~/.config/nimbus/config.toml
       checkout = "/home/pby/.local/share/chezmoi"
       machine  = "desktop"
       origin   = "https://github.com/Furyfree/dotfiles.git"
  5. chezmoi init with the handoff, no apply
  6. plan; review; apply
  7. print: chezmoi diff, chezmoi apply, git status
```

Fresh, reinstall, and no-repository are one state machine. The only branch
is step 2, and adopting a local repository later is `git remote add origin`
plus `git push`, which is plain Git work the user owns. The
`~/.config/nimbus/machine.toml` link and the plain local manifest are both
replaced by the config file above, which is printable, `--json`-able, and
overridable with `--checkout` and `--machine`.

Nimbus does not manage its own installation through definitions. The
owner's `common` profile lists the COPR repository and the `nimbus`
package so `dnf upgrade` keeps the engine current; another user's
definitions simply omit them.

### 8.2 The daily loop

```text
check    nimbus status                chezmoi status
change   edit nimbus/ or machines/    chezmoi edit
preview  nimbus plan                  chezmoi diff
apply    nimbus apply                 chezmoi apply
share    git commit && git push       (same repository)
sync     chezmoi update --apply=false     pulls both halves
         nimbus status                    "definitions changed since apply"
         nimbus plan && nimbus apply
         chezmoi diff && chezmoi apply
update   topgrade                          honors version locks
         nimbus plan --upgrade             grouped desktop updates
```

### 8.3 Updates and constraints

Fedora updates packages; Nimbus decides what may move.

- The invariant is accepted: a constraint must affect native updates or it
  does not exist. `package_constraints` become planned resources
  materialized in the DNF provider. Whether DNF5 `versionlock` can carry a
  range like `>=0.56, <0.57` losslessly is unverified; until the provider
  proves its representation in the VM, `validate` rejects constraints it
  cannot materialize. This is an open question in section 11.
- `nimbus plan --upgrade` adds available updates for managed packages,
  grouped by `update_group`. The `desktop-session` group is one `dnf`
  transaction preceded by a recovery point; it lifts and reapplies its own
  locks inside the transaction so the reviewed window can move.
- `topgrade` stays allowed and Chezmoi-configured.

## 9. Command surface and files

Engine verbs only. Everything personal moves to `home/`.

| Command | Reads | Writes |
|---|---|---|
| `nimbus init [REPO] [--machine ID] [--new DIR] [--trust]` | checkout | config, manifest, clone |
| `nimbus validate` | definitions | nothing |
| `nimbus config resolve` | definitions | nothing |
| `nimbus facts` | system | nothing |
| `nimbus status` | all three states | nothing |
| `nimbus plan [--out F] [--upgrade] [--prune]` | all three | nothing |
| `nimbus diff` | plan | nothing |
| `nimbus apply [F] [--prune]` | plan | system, receipts |
| `nimbus managed`, `nimbus unmanaged` | receipts, facts | nothing |
| `nimbus why RESOURCE` | desired graph, receipts | nothing |
| `nimbus doctor [--explain]` | system | nothing |
| `nimbus packages add`|remove|list | manifest | manifest, then "run plan" |
| `nimbus windows setup` | component state | starts the reviewed container |
| `nimbus version` | | prints engine and schema range |

Every read command has `--json` with the versioned envelope from `CLI.md`.
Exit codes stay 0, 1, 2.

Moved or deferred:

- `windows start|stop|connect|purge-data` become a Chezmoi-managed script;
  `purge-data` keeps its second confirmation naming the path. Nimbus keeps
  the `windows-vm` component and `windows setup`, which starts a privileged
  container and needs Nimbus' recovery model.
- `launch browser|webapp` become Chezmoi-managed scripts next to the key
  bindings that call them.
- `postinstall` is not a command yet. Components' `[[manual]]` entries with
  `check` commands are the single model; `status` lists the open ones. A
  dedicated view can come later without a second model.
- The dashboard and package browser wait until every verb has stable
  `--json`.

### 9.1 Files and the seed session

The `file` provider is Chezmoi's idea above the sudo line: sources mirror
the target path under `nimbus/root/`, mode and owner default to
`0644 root:root`, `nimbus diff` previews, and `on_change` names triggers.

The seed session replaces the `$HOME` seed file:

```text
/usr/share/wayland-sessions/hyprland-seed.desktop
    Exec=Hyprland --config /usr/share/nimbus/seed/hyprland.lua
/usr/share/nimbus/seed/hyprland.lua
    minimal keybinds, exec-once noctalia --daemon, a terminal
```

The greeter shows "Hyprland (Nimbus seed)" beside "Hyprland". The seed
works before `chezmoi apply` and stays as a recovery session afterwards. No
yield logic, no Chezmoi path check, no user-file write. VM proof before
acceptance into `SPEC.md`: the greeter lists the entry, the absolute config
path works, and removing the component removes both files.

## 10. Decision register

Source key: F = FABLE, G = GPT-5.6-Sol response, L = GLM-5.3-Flash response,
O = owner decision on 2026-09-01.

| Topic | Decision | Source |
|---|---|---|
| Where definitions live | Workstation repository under `nimbus/`; engine embeds only the `--new` example tree | F, L accept; G reject; O: repository |
| Trust of external definitions | Init-time confirmation, origin recorded, re-trust on change, canonical digest, schema range, path-escape rejection, CI | G conditions, adopted |
| Manifest selection | `~/.config/nimbus/config.toml` with `checkout`, `machine`, `origin` replaces the symlink and the local manifest | F, L accept; G keep symlink; follows from O |
| No-repository flow | `nimbus init --new DIR` creates a local repository; `git init` printed | F, L accept; G keep local flow; follows from O |
| Local Git reads | `rev-parse HEAD` and `status --porcelain` allowed; still no fetch, commit, pull, push | F, G, L accept |
| User-scope tools | mise via Chezmoi-managed config plus one `run_onchange_` install script; Mise, Cargo, uv providers removed | F, L accept; G defer; O: mise + script |
| Seed session | System-owned session entry with `Hyprland --config`; VM proof required | F, G, L accept |
| Handoff | Add `Machine=<id>`; `.chezmoidata/machines.toml`; empty-ID fallback tested | F, G, L accept |
| Profile contract test | Repository-local check in the workstation `justfile`, not a Nimbus flag | G correction, adopted |
| Version constraints | Invariant accepted; mechanism open until DNF5 versionlock is verified | G, L |
| Catalog | Exception list with a typed DNF default; golden bare-name test | F, G, L accept |
| Privilege model | Direct `sudo <native>` per command; two narrow internal privileged subcommands for atomic file and receipt writes; no general `__step` executor | G, adopted over F |
| Terraform-shaped flags | `plan --out` and `apply FILE` kept as the digest mechanism; `--target`, `--reinstall`, `forget`, `renamed_from` deferred | G, adopted |
| `status` | Full drift inspection plus a fast definitions-digest line | G correction, adopted |
| Recovery points | Per transaction or resource group, after a VM restore drill and retention policy | G, L |
| Command surface | Engine verbs; `windows setup` stays; runtime and launch helpers move to `home/`; dashboard deferred | G, L split, adopted |
| Post-install | `[[manual]]` entries as the one model, listed by `status` | F, G, L |
| Bootstrap | Nimbus, Git, and Chezmoi installed before `init`; adopted by the first apply; FABLE's step order was a cycle | G, L reject F; adopted |
| `$HOME` invariant | Narrowed to Nimbus XDG paths plus the reviewed checkout during init | G correction, adopted |
| Engine genericity | A design property; support stays owner-first per `SPEC.md` | G, L framing, adopted |

Unchanged from the accepted contracts: the ownership split, plan before
apply, receipts, the digest guard, prune semantics, no daemon, no keepalive,
no `chezmoi apply`, no silent Git, Fedora 44 first, Cobra and `go-toml/v2`,
read-only Phase 1, the JSON envelope and exit codes, and the 1Password
design in the dotfiles roadmap.

## 11. Open questions

These move to `OPEN_QUESTIONS.md` during reconciliation.

- Can DNF5 `versionlock` represent a range constraint, or only exact
  versions? Decide the manifest syntax after the VM test.
- Exact membership, sources, and verification of the `desktop-session`
  group. Already open.
- Which cargo-installed tools move under mise's `cargo:` backend, and which
  stay unmanaged.
- Whether the seed session's `Hyprland --config` path is honored by the
  Fedora Hyprland package and greeter combination. VM proof.
- Retention policy and restore procedure for recovery points.

## 12. Invariants

Each is a test or a CI job, not a sentence.

1. Nimbus writes under `$HOME` only in `~/.config/nimbus`,
   `~/.cache/nimbus`, and the checkout during `init`. Sandbox-home test.
2. Read-only commands never exec `sudo`, never open a network socket, and
   never write. Fake-runner assertion on every argv.
3. Every receipt contains the Nimbus version, commit, dirty flag, tree
   digest, plan digest, and verification result. Schema-validated on write
   and read.
4. `nimbus validate` and the profile contract test pass in the workstation
   repository CI against a pinned engine release.
5. Removing `nimbus` leaves Chezmoi fully working. Removing `chezmoi` leaves
   the system booting into the seed session. VM tests.
6. No Go source branches on a package name. `just check` greps for it.
7. A plan digest mismatch at apply is a hard refusal. Tested by editing the
   manifest between `plan --out` and `apply`.
8. Every mutating provider has a fixture proving a failed native command
   produces no applied receipt.
9. `validate` rejects symlinks under `nimbus/` and any path that escapes
   its root.

## 13. Sequencing

Each milestone is one Go learning goal, one deliverable, one test. The
first real install lands after milestone 5.

| # | Deliverable | Learning goal | Test |
|---|---|---|---|
| 0 | Reconcile `SPEC.md`, `CLI.md`, `ROADMAP.md`, `OPEN_QUESTIONS.md`; move definitions into the workstation repo; add `Machine` prompt, `.chezmoidata/machines.toml`, mise script exception, contract test | docs only | `chezmoi execute-template`, `git diff --check` |
| 1 | `nimbus validate` and `config resolve` over `--checkout`, schema range, tree digest | modules, structs, `go-toml/v2`, table tests, golden files | golden graphs, invalid fixtures, digest fixtures |
| 2 | `nimbus facts` for packages, repos, services, GPU, Secure Boot, Btrfs | `os/exec`, fake runners, JSON | fixtures from the test VM |
| 3 | `nimbus plan` for DNF packages, `--out`, digest | provider interface, canonical JSON, hashing | golden plans |
| 4 | `nimbus apply` for DNF, `internal record`, receipts, adoption | process control, atomic writes, error wrapping | disposable VM install and removal |
| 5 | `nimbus init`, trust flow, config file, Chezmoi handoff | Git via `git`, argv building, prompts | golden argv; fresh VM end to end |
| 6 | `copr`, `flatpak`, `systemd`, `file` with `internal write-files` and triggers, seed session | more providers on one interface | VM: full `hyprland-noctalia` |
| 7 | recovery points, version locks, `plan --upgrade`, `windows-vm` | Btrfs, transactions | VM rollback drill |
| 8 | `doctor`, `why`, quarantine, `--json` everywhere | polish | contract tests |
| later | `--target`, `forget`, boot provisioner, LUKS, ISO, TUI, other desktops | | |

Milestones 1 through 4 are the current roadmap's Phase 1 to 3 with
inspection split out as its own command. Milestone 0 is the reconciliation
GPT's response called for; it stops before Go code.
