# Nimbus specification

## Purpose

Nimbus is a personal, opinionated Fedora workstation installer and system
manager. It defines the system its owner wants to run, turns a supported
installed Fedora base into that system, detects drift in resources it owns, and
performs reviewed installation, repair, upgrade, and removal.

Nimbus owns the system layer. Chezmoi owns user configuration and invokes
native tools to install the user tools it declares.

Nimbus is not a general configuration-management engine or a support promise
for arbitrary third-party definitions. Other users may inspect or fork the
complete repository and change its definitions and implementation.

## System boundary

Nimbus starts from an installed system with:

- a supported Fedora release
- an x86_64 machine
- a normal user with wheel access
- working networking
- a mounted and bootable root filesystem

Nimbus does not require Hyprland, Noctalia, Chezmoi, development tools, or the
owner's dotfiles to be present.

Nimbus does not normally install Fedora, partition disks, create filesystems,
or configure full-disk encryption. A later installer image may reuse the same
definitions and provisioning operations while leaving destructive storage
creation to Anaconda.

## Repository and distribution model

The Nimbus repository contains one complete personal system:

~~~text
nimbus/
  install.sh
  bootstrap
  cmd/
  internal/
  machines/
  profiles/
  components/
  system/
    root/
    migrations/
    triggers/
  nimbus.toml
~~~

- Go code implements the engine.
- install.sh is the minimal remote entry point and bootstrap is the versioned
  checkout-owned installation handoff.
- Machine manifests select intended workstation compositions.
- Profiles are user-facing system bundles.
- Components describe reusable capabilities and own resources.
- nimbus.toml declares the schema, supported Fedora releases, minimum engine
  version, and every repository other than Fedora with its pinned key.
- system/root/etc mirrors Nimbus-owned system-file targets below /etc.
- Migrations represent controlled transitions that cannot be expressed as
  steady-state resources.

An installed engine reads live definitions from an explicitly selected Nimbus
checkout. nimbus.toml declares the definition schema, supported Fedora
releases, minimum compatible engine version, and the repositories. The engine
rejects unsupported schemas and invalid compatibility metadata.

The canonical definition boundary is `nimbus.toml` plus every regular file
recursively below `machines/`, `profiles/`, `components/`, and `system/`. The
complete boundary participates even when one machine selects only part of it.
Engine code, Git metadata, documentation, history, development tooling,
directories themselves, and every other checkout path are excluded.

The definition digest is SHA-256 over an unambiguously framed sequence of
entries sorted bytewise by slash-separated checkout-relative path. The input
starts with `nimbus-definitions-v1` followed by a zero byte. Each entry encodes
the path length as an unsigned 64-bit big-endian integer, the path bytes, mode
as
an unsigned 32-bit big-endian integer holding octal `0100644` or `0100755`,
content length as an unsigned 64-bit big-endian integer, and the exact content
bytes. The digest renders as `sha256:` followed by lowercase hexadecimal.

Regular-file mode is normalized to `100755` when the owner execute bit is set
and to `100644` otherwise, which is Git's rule. Other permission bits and
directory modes are ignored. A system file's desired target ownership and
mode remain explicit resource data and participate through the content of
the declaring definition.

The configured checkout root may be a symlink. Nimbus resolves it once at the
start of an operation and uses the resulting canonical directory for origin
checking, containment validation, loading, and hashing. `nimbus.toml` and every
entry within the definition directories must be a regular file or directory;
symlinks and special files within that boundary are errors. Plans and receipts
identify the engine version, checkout origin, commit, dirty state, and
definition digest. Apply re-resolves and re-hashes the checkout, so a changed
root target cannot reuse an earlier plan when its trust identity, commit, or
effective definition input differs.

The engine may embed an example definition tree only to create a new local
checkout. Embedded data is never the live personal desired state.

Engine packaging and the checkout may be delivered separately. Their
compatibility is explicit and verified; Nimbus never updates either itself.

## Configuration model

The local selector is:

~~~text
~/.config/nimbus/config.toml
~~~

Example:

~~~toml
schema = 1
checkout = "/home/user/.local/share/nimbus"
machine = "desktop"
origin = "github.com/Furyfree/nimbus"
~~~

The selector is a regular Nimbus-owned local file written only through an
explicit reviewed selector operation. It is not a symlink, is not managed by
Chezmoi, and does not contain desired system configuration. The checkout is the
root of the selected Nimbus repository, and the machine value selects
`machines/<machine>.toml` within it. The selector does not copy profiles,
components, packages, constraints, or dotfiles configuration. Commands may
accept explicit checkout and machine overrides.

The required origin is the repository identity approved for the checkout. It
does not pin a commit or prevent the checkout from being updated. Nimbus
normalizes supported Git locators by removing transport and user information,
lowercasing the host, removing trailing slashes and then one trailing `.git`
suffix from the repository path, and preserving the remaining path. For example,
`git@github.com:Furyfree/nimbus.git` and
`https://github.com/Furyfree/nimbus` both normalize to
`github.com/Furyfree/nimbus`.

For selector-based loading, Nimbus reads the checkout's local Git configuration
without invoking Git or accessing the network and compares its normalized
origin with the selector. It reads `.git/config` directly, following the
`gitdir` pointer when `.git` is a worktree file; an `include` or `includeIf`
directive is an error that names the file and tells the user to set
`remote.origin.url` directly. A missing, invalid, or different origin is an
error.
A legitimate origin change requires a separate explicit reviewed trust action
that rewrites the selector; Nimbus never changes the remote. Commit,
working-tree, and definition changes do not alter this repository trust and
remain identified separately in plans and receipts.

The checkout root file declares compatibility and repositories:

~~~toml
schema = 1

[compatibility]
fedora = ["44"]
min_engine = "0.1.1"

[dnf]
max_parallel_downloads = 20
fastestmirror = true
defaultyes = true

[repositories.terra]
kind = "dnf"
baseurl = "https://repos.fyralabs.com/terra44"
key_url = "https://repos.fyralabs.com/terra44/key.asc"
key = "AE09 157A 4DE8 8B49 7EA1 D5D3 00CD AB43 DE22 6D6F"
priority = 110

[repositories.docker]
kind = "dnf"
baseurl = "https://download.docker.com/linux/fedora/$releasever/$basearch/stable"
key_url = "https://download.docker.com/linux/fedora/gpg"
key = "060A 61C5 1B55 8A7F 742B 77AA C52F EB6B 621E 9F35"
priority = 100

[repositories.hyprland-copr]
kind = "copr"
project = "lionheartp/Hyprland"
key = "<fingerprint>"
priority = 130

[repositories.flathub]
kind = "flatpak"
url = "https://dl.flathub.org/repo/flathub.flatpakrepo"
key = "<fingerprint>"
~~~

A repository ID is the prefix that package references use. `dnf`, `cargo`, and
`flatpak` are reserved: `dnf` is Fedora and `flatpak` is the single declared
`flatpak` repository, while `cargo` selects user-scope Cargo tools. On the
host, a `dnf` repository Nimbus enables from a `baseurl` lives in
`/etc/yum.repos.d/nimbus-<id>.repo` with the DNF repository ID `nimbus-<id>`,
so ownership is readable from the directory; a
release package and a COPR keep the IDs their own tooling creates. A file
that already provides a declared repository under the maker's own ID is
foreign: Nimbus neither duplicates it nor takes it over silently. `key_url` is
where DNF fetches the signing key and `key` is the fingerprint that key must
have; a key with another fingerprint fails the operation. A maker that
publishes no key URL, such as OpenAI, or only a bundle with additional primary
keys, such as Brave, has its single pinned key stored in the checkout instead
as `key_file`, a path below `system/`. A COPR derives its key URL from
the project, and a Flatpak remote carries its key inside the `.flatpakrepo`
file. A `dnf` repository may instead name a `release_package` URL with its
`sha256` when the maker distributes a release RPM, as RPM Fusion does.
`priority` is the DNF repository priority and is required on every `dnf` and
`copr` repository: a number above Fedora's default of 99, so no later source in
the [SECURITY.md](SECURITY.md) order can shadow Fedora, and distinct for every
repository in that order, since DNF breaks a tie by version and a later
source could then shadow an earlier one. The stored key file is
checked for its armored public-key form at validation; its fingerprint is
verified when sync imports the key. Existing sources are checked against the
active local key and effective signature settings too. DNF and COPR sources
use the verified local key through their native configuration. Changing a pin
requires a deliberate definition edit and verified replacement; Nimbus never
accepts a newly advertised key automatically. An existing Flatpak remote with
unknown or different trust blocks for explicit native repair, because importing
a key adds trust rather than replacing its existing keyring.

The `[dnf]` table holds libdnf5 `[main]` options as numbers, booleans, or
strings. String values must be single-line and contain no NUL bytes.
Nimbus renders it, keys sorted, into
`/etc/dnf/libdnf5.conf.d/20-nimbus.conf`, the drop-in directory libdnf5 reads
before `dnf.conf`, and plans that file as the first operation so every
transaction downloads with the declared settings. The file is compared whole
with the rendering: an absent file is written, a differing one rewritten, an
identical one adopted, and a file Nimbus wrote is removed when the table is
removed. A drop-in Nimbus never wrote is left alone.

Machine, profile, and component IDs use lowercase letters, digits, and
dashes, starting with a letter or digit.

A machine manifest has a stable ID:

~~~toml
schema = 1
id = "desktop"

profiles = [
  "common",
  "development",
  "gaming",
  "hyprland-noctalia",
  "windows-vm",
]

components = [
  "nvidia",
]

packages = [
  "ripgrep",
  "flatpak:com.spotify.Client",
]

package_exclusions = []

[dotfiles]
repo = "https://github.com/Furyfree/dotfiles.git"
~~~

The manifest ID must match its filename. Every manifest lists `common`
itself; the resolver never injects it, and validation reports a manifest that
omits it. The `profiles` list keeps the author's order because the Chezmoi
handoff receives it. Every other resolved list is sorted by canonical ID so
output is stable, and a duplicate entry in any list is an error.

Static validation and resolution never inspect hardware or silently augment
the manifest. During creation of a new machine, `nimbus init` may inspect DMI
identity and PCI devices, propose the matching hardware components, and
include them in the reviewed manifest diff. The accepted component IDs are
then ordinary explicit desired state; later resolution never re-detects or
silently changes them.

A profile file and a component file look like this:

~~~toml
# profiles/common.toml
schema = 1
id = "common"

packages = [
  "git",
  "chezmoi",
  "flatpak",
  "flatpak:com.spotify.Client",
]

components = ["snapper"]
~~~

~~~toml
# components/docker.toml
schema = 1
id = "docker"

requires = []
conflicts = []
packages = [
  "docker:docker-ce",
  "docker:docker-ce-cli",
  "docker:containerd.io",
  "docker:docker-buildx-plugin",
  "docker:docker-compose-plugin",
]
removes = ["moby-engine", "podman-docker"]

[[files]]
source = "etc/docker/daemon.json"
owner = "root"
group = "root"
mode = "0644"
~~~

`schema` and `id` are required and `id` must match the filename. Every other
field is optional and defaults to empty. A profile carries `packages` and
`components`. A component carries `requires` and `conflicts`, both lists of
component IDs, `packages`, `removes`, and `files`. A `[[files]]` entry names
its `source` relative to `system/root/`, which must start with `etc/`; the
target is the same path below `/`. `owner`, `group`, and `mode` are required
on every entry. An unknown field anywhere is an error.

The initial profile vocabulary is:

- `common`: the base every machine needs
- `development`: developer tooling, Docker, Nix, and the system dependencies
  Mise needs
- `virtualization`: QEMU/KVM host packages for Linux and other guests the
  user manages directly through VM Curator, which the user installs with
  Cargo; guest disks live in the user's home and are not Nimbus resources
- `gaming`: the complete gaming stack for the desktop
- `laptop-gaming`: light gaming for the laptop, currently PrismLauncher from
  the Terra repository as `terra:prismlauncher` with Fedora's
  `java-25-openjdk` and small helpers such as `gamemode`; a machine selects
  `gaming` or `laptop-gaming`, and the two share components rather than
  repeating packages
- `hyprland-noctalia`: the Hyprland and Noctalia session
- `windows-vm`: the Windows guest

The `windows-vm` profile selects the `windows-vm` component; it is a profile
rather than a bare component so the Chezmoi handoff can see it. It is not
named `windows` because the dotfiles repository derives a `windows` platform
profile from the operating system.

A profile lists packages directly and selects components. It never imports
another profile. A component exists only for a bundle that two profiles share,
such as Docker, or for a capability that owns more than packages, such as the
NVIDIA driver or the Hyprland session. Components may require other
components and contribute packages, package removals, services, system files,
groups, triggers, warnings, manual tasks, runtime commands, verification,
removal policy, and recovery classification.

Profile and component files carry only the fields the current definitions
use: `id`, `packages`, `components`, `requires`, `conflicts`, `removes`, and
system-file sources. Services, groups, triggers, warnings, manual tasks,
runtime command groups, and recovery metadata are added to the schema when a
real definition and fixture exercise them.

Machine manifests may select optional components and add ad hoc packages.
Profile and component definitions remain the source of reusable intent;
machine-specific differences stay sparse.

Hardware is represented by components, not profiles. The first two recognized
targets are the MSI Z690 desktop with Intel integrated graphics and NVIDIA RTX
3080, and the HP EliteBook X G1a with AMD integrated graphics. Detection uses
the machine's DMI product or board identity plus relevant PCI vendor and device
IDs. The desktop display component selects `ddcutil`; the laptop display and
power component selects `brightnessctl`. An unknown or ambiguous device yields
a warning and a component picker, never a guessed selection. The `vm`
manifest is the disposable Fedora virtual machine used for apply drills; it
selects no hardware component and lists Mesa directly for virtio graphics.

## Resolution

Resolution is deterministic:

~~~text
machine
  -> profiles
  -> profile packages and components
  -> explicit components
  -> component dependencies
  -> resources
  -> manual tasks
  -> runtime command groups
~~~

Resolution reads configuration only. It never inspects hardware, the installed
system, native package databases, or Nimbus applied state.

`nimbus validate` checks every definition in the selected checkout and resolves
every tracked machine manifest so schema and graph errors are found before a
change is committed or applied. Resolution remains an internal engine
capability reused by validation, status, planning, package workflows, and
tests; it is not a separate public command. Selecting no optional component is
valid. Later inspection may warn that the machine lacks a desktop session or
that detected hardware has no selected supporting component, but it never
changes the selection.

Validation rejects:

- unsupported schemas or engine compatibility
- a manifest that omits `common`
- duplicate machine, profile, component, resource, or repository IDs
- a package prefix that names no declared repository
- unknown references and dependency cycles
- conflicting components or desired resource states
- duplicate lifecycle ownership
- invalid package exclusions or constraints
- invalid provider configuration
- arbitrary shell operations
- path traversal, symlink escape, and system sources outside the checkout
- system-file sources outside system/root/etc or mappings outside /etc

The same checkout content and machine selection always resolve to the same
desired graph and canonical digest.

## Package model

Nimbus is Fedora-first. A bare package name uses the typed default DNF
lifecycle:

- inspect through RPM and DNF
- install from an enabled reviewed repository
- update through DNF
- preserve the native install reason
- verify through the native package database
- remove through DNF only when Nimbus owns the package and removal is safe

Package references use a closed, versioned grammar. Values have no leading or
trailing whitespace. A reference without a colon is a Fedora package name and
canonicalizes to `dnf:<name>`. The qualified form is `<repository>:<name>`,
split at the first colon:

- `dnf:<name>` is the same default Fedora lifecycle
- `flatpak:<app-id>` is a system-scoped Flatpak from the declared Flathub
  remote
- any other prefix is a repository ID declared in `nimbus.toml`, such as
  `terra:ghostty`, `rpmfusion-nonfree:steam`, `docker:docker-ce`, or
  `hyprland-copr:hyprland`, and uses the DNF lifecycle from that repository

An empty value, empty suffix, undeclared prefix, or invalid provider-native
identifier is rejected. Prefixes are lowercase and reserved by the schema.

The prefix is the package's declared source. Canonical identity is the
provider-qualified native package target; references that resolve to the same
canonical identity merge provenance when their prefix agrees, and two prefixes
for one name are duplicate ownership and fail validation. A repository becomes
a desired resource when any selected package names it, is owned once however
many packages use it, and is removed only when no selected package names it
and inspection shows no installed package that still comes from it.
Nimbus never enables a repository or installs its release package with
signature checking disabled; the plan shows the pinned key or digest.

The plan notes a package that DNF would install from a repository other
than the one its prefix names. A component may list `removes`, packages that
must leave when it is applied; the plan renders that as a native swap, such
as RPM Fusion's `ffmpeg` replacing Fedora's `ffmpeg-free`. Package-specific
behavior belongs in typed definition data, never package-name conditionals in
Go.

A `package_exclusions` entry in a manifest may name only a package that a
selected profile or component installs on that machine, and never a package
a selected component requires. An entry that matches nothing is a validation
error. Whether an excluded package is protected or still needed by an
installed package is a planning check against the observed system, not a
validation check.

One provider owns an installed executable lifecycle. Nimbus manages
system-scoped Flatpaks from Flathub. User-scope tools follow the
[SECURITY.md](SECURITY.md) source order: Nimbus runs a maker's installer
script, `cargo install`, or `mise install` as the normal user, without sudo,
as a plan step. A component declares a maker's installer in an `[installer]`
table: the `url` of the script, the `binary` it leaves relative to the home
directory, and optionally the `config` file the Chezmoi handoff writes and the
`install` command to run once it exists, with `<home>` standing for the home
directory. A crate is a package reference with the reserved `cargo:` prefix;
it waits for `~/.cargo/bin/cargo`, which the Rust runtime provides, and is
verified through `cargo install --list`. The tracked profiles instead keep
their Cargo tools in Chezmoi's `~/.config/mise/conf.d/cargo.toml`, alongside
the runtimes in `~/.config/mise/config.toml`. Mise installs both after Chezmoi
has written the files, through Chezmoi's after-apply script. Chezmoi owns
these declarations and the invocation; Nimbus does not repeat their list or
installation command. Mise owns installation, versions, and updates. Nimbus
reports the handoff as a dotfiles and tools stage with native output.
Nimbus-owned user-scope steps are safe to repeat and write no receipt;
verification uses native command results and presence checks. Their
removal is explicit, shown in the plan, and never triggered by removing a
profile: `cargo uninstall <crate>`, `mise implode`, and `zed --uninstall`.

A package constraint is accepted only when the native provider can enforce it
during normal native updates. Unsupported comparison syntax is rejected rather
than represented as an advisory-only constraint. Constraint keys use the
canonical provider-qualified package identity, such as `dnf:hyprland`, rather
than a bare name. A constraint attaches after all selection paths have been
resolved and therefore applies to that canonical package regardless of which
profile, component, or machine entry selected it.

Compatibility-sensitive packages may form a coordinated update group. The
desktop-session group covers the selected Hyprland, Noctalia, greeter, portal,
and session integration resources. Its exact membership and verification must
be proven against the selected Fedora sources before it is encoded.

## Resource model

A resource is one stable unit of desired system state. Resource families
include:

- DNF and COPR repositories and the Flathub remote
- RPM packages
- system Flatpaks
- user-scope tools installed through a maker's script, Cargo, or Mise
- systemd system units
- system files
- system groups and user group memberships
- kernel arguments and boot resources
- external artifacts with approved integrity evidence
- fixed-argv triggers
- migrations

Each resource has:

- a stable ID and one technical lifecycle definition
- contributing selection paths for provenance
- desired state and inspection
- planned apply and verification
- exact privilege requirements
- removal behavior or an explicit unsupported-removal state
- recovery classification
- logout or reboot requirements

Profiles do not define technical lifecycle. Profiles, components, or explicit
machine entries select resources; the typed provider and the repository
declaration define how each resource is inspected, applied, verified,
updated, and removed. Provenance
records every profile, component, or explicit machine path that selected it.

## Ownership

Nimbus owns:

- the local selector at ~/.config/nimbus/config.toml
- Fedora and approved third-party repositories
- Nimbus-selected RPMs and system Flatpaks
- system services, timers, groups, files, and drop-ins
- graphical-session, greeter, portal, and recovery-session integration
- hardware, kernel, boot, update, and recovery policy it declares
- inspection, plans, apply, verification, removal, state, and receipts
- installation of system tools including Git, Chezmoi, Docker, Nix, and
  1Password
- installation of explicitly Nimbus-selected user-scope tools, including the
  Mise binary, through native maker channels as the user
- the explicit first Chezmoi initialization and apply
- the Windows guest data root and its protected credentials file
- typed manual workflows and Nimbus runtime commands

Chezmoi owns:

- files and templates below the user's home directory except the Nimbus local
  selector and the binaries that Nimbus-run maker installers, Cargo, and Mise
  place below `~/.local` and `~/.cargo`
- Hyprland, Noctalia, shell, terminal, editor, browser, and application user
  configuration
- systemd user unit files, user scripts, and desktop entries
- secret-backed templates
- native Mise tool declarations and invoking `mise install` after applying
  their configuration, without privileged or system package installation
- its source checkout and normal diff, apply, edit, and update lifecycle

Native tools retain their own lifecycle. Nimbus invokes and verifies DNF,
systemd, Flatpak, Mise, Git, Chezmoi, and other specialist tools rather than
reimplementing them.

Credentials, tokens, private keys, application databases, histories, caches,
documents, containers and virtual machines other than the Windows guest, and
undeclared local system configuration remain unmanaged. A user-scope tool such
as Zed or Mise is installed by Nimbus as the user, updates itself on the
maker's schedule, and has its configuration owned by Chezmoi.

## Desired, observed, and applied state

Nimbus keeps three states separate:

~~~text
checkout definitions and machine manifest -> desired
live native-system inspection             -> observed
/var/lib/nimbus receipts                  -> last applied
~~~

Desired configuration never comes from state or receipts. Observed state never
becomes desired merely because it exists. Applied state records only verified
Nimbus operations and never replaces current inspection.

Nimbus state and receipts below /var/lib/nimbus are versioned, contain no
secrets, are readable by the normal user, and are written atomically through a
narrow operation-scoped privileged action that accepts only a stage bound to
the plan digest. New receipts and baselines use schema 2 to retain native RPM
name and
architecture. Schema 1 remains readable; ambiguous legacy ownership blocks
removal. Older engines must refuse version 2 rather than discard its identity.
The first sync also records the baseline: the
packages installed before Nimbus took over, which are never prune candidates
and are listed by `unmanaged --all` as pre-existing. Selecting a baseline
package explicitly adopts it; later deselection removes that owned package
through the reviewed plan. Baseline protection applies to unmanaged pruning.
The Windows guest data
root mounted at `/var/lib/nimbus/windows` is a separate subvolume holding guest
data, not Nimbus state. A receipt records at least:

- state and engine versions
- definition origin, commit, dirty state, and digest
- machine and resolved selections
- resource and provider
- previous observation and intended state
- exact lifecycle operation
- plan digest
- verification result
- recovery-point ID when applicable
- timestamp and reboot or logout requirement

A failed operation never produces a successful receipt. Accurate receipts for
previously completed independent operations remain after partial failure.

## Inspection, status, plan, and apply

Internal inspection uses native read-only interfaces and produces structured
facts for status, planning, doctor, ownership views, and tests. Raw fact
collection is not a public command.
Status compares desired, observed, and applied state without mutation.

A plan contains:

- resolved intent and relevant observed facts
- install, change, adopt, repair, and owned-removal operations
- unchanged, blocked, pending, manual, and unmanaged resources
- exact privileged commands and system-file diffs
- verification, triggers, recovery, warnings, and reboot requirements
- known normal-update candidates kept separate from normal apply

`nimbus sync --plan` shows exactly what `nimbus sync` would do and changes
nothing. With `--prune` the plan adds a visibly separate section with the
unmanaged packages `sync --prune` would remove; without the flag that section
is absent. A pending operation waits for an
earlier operation in the same plan, such as a package whose repository the
plan enables; apply runs the earlier operation and re-plans so the exact
transaction is reviewed before it runs. A blocked operation is a problem
the owner must resolve, and it makes the plan incomplete.

A desired package that is already installed is adopted whatever its source,
and the receipt records that source; a Flatpak from another remote is
adopted with a note. A declared repository counts as present only
when the host provides it from the file Nimbus owns, with signature checking
on and the declared location and priority. The owned file is compared whole
with what Nimbus would write, so a changed value, a missing key, or a key
Nimbus does not write is a repair operation that rewrites the file; a
foreign file blocks. Native overrides that redirect a repository location or
weaken its TLS verification block for explicit native correction before sync;
rewriting the repository file cannot override those settings.
A host repository under another ID that serves a
declared baseurl, such as the file a maker's package writes when it is
installed, is a duplicate provider: the repository operation disables it
through a DNF override and never edits the maker's file. A release package
Nimbus installed to enable a repository belongs to that repository and is
managed, never unmanaged or pruned.

The canonical plan excludes volatile display data. Its digest covers the
machine, the definition digest, and the operations with their exact steps
and native transactions; the checkout origin, commit, and dirty state are
reported beside it. Sync and selection commands separately recheck origin and
commit under the operation lock and reject an identity change during the run.
Dirty state is refreshed for receipts; Nimbus's own manifest edits can change
it. The plan carries a digest that receipts record, so the
state says which plan produced it.

Planning never invokes sudo, writes files, accesses the network, or changes
Nimbus state; `sync --plan` reads the DNF metadata cache as it is and says so. A
run refreshes that cache first, as the user, so the plan it shows and the
transactions read current package lists; a refresh that fails is reported and
the plan reads the cache as it is. DNF transactions are previewed from that
cache. Update information uses the same metadata and reports when its freshness
or availability is insufficient. A plan with a problem, such as a package no
repository provides, is incomplete and says so; sync runs only a complete plan.

Sync installs and repairs desired resources, adopts existing ones whatever
their source and records that source, and removes resources previously owned
by Nimbus that are no longer desired. It leaves unrelated unmanaged resources
unchanged. If an owned RPM or Flatpak has already been removed, sync retires
only its receipt after confirming absence; unknown state blocks retirement.
It then upgrades the system, unless `--no-upgrade` is given, so one
command keeps the machine both as declared and current.

`nimbus sync` works the way an installer does: show, ask once, run, report.
It shows the plan as it is known at that moment and asks `Proceed? [Y/n]`
once; `-y` answers yes and `--json` asks nothing. After the answer it takes
the lock and plans again; a plan that changed while the question was open
stops the run. On a host whose sources
exist the plan is exact, with versions, dependencies, and download size. On a
fresh host the sources do not exist yet, so the plan names the packages and
DNF prints the exact transaction as it starts. Sudo is primed once after the
answer and its credential is renewed while sync runs, so the password is
asked once; a run that holds only user-scope steps, or whose every operation
waits for the Chezmoi handoff, asks for nothing and says what it waits for.
Execution then prepares the declared sources, each declared in
`nimbus.toml` with its pinned key: the DNF drop-in, the repositories and their
duplicates, the Flatpak remote when `flatpak` is present, and a metadata
refresh when a DNF repository changed. It runs the native commands with their
own output on the terminal: one `dnf5 install` for the packages, then Flatpak
and removals; whatever waited on that run, such as a Flatpak behind the
`flatpak` package, runs in a further pass without asking again. Then it
upgrades the system with `dnf5 upgrade` and `flatpak update`. DNF resolves at
install time, so the result may differ from the preview; sync does not stop
for that but verifies the result afterwards, writes receipts from what is
actually installed, and reports the differences by name, or "none". A
requested package that is not installed after its transaction is a failure.
A failed command stops the run; earlier receipts stay. `nimbus sync --prune`
uses the same process with prune candidates promoted into a visibly separate
expanded plan; before the first sync has recorded the baseline there are no
candidates, and the plan says why. An unmanaged resource is eligible only when
the native provider proves explicit installation, non-protected status,
dependency safety, and an exact removal and verification path.

## Privilege and concurrency

Nimbus runs as the normal user and refuses to run the whole CLI as root.
Read-only system operations never invoke sudo.

Privileged native commands run with their own output on the terminal, so
DNF's and Flatpak's download and transaction progress stays visible; in JSON
mode that output goes to standard error. Every privileged operation is
rendered in the plan. Native operations
run directly through sudo. Nimbus may expose only two classes of narrow
internal privileged action:

- atomically install the approved system-file payload for one plan step
- atomically record the approved root-owned state and receipts

They accept only staged data bound to the plan digest. Nimbus has no
general privileged executor, root daemon, or helper service. Sync primes the
sudo credential once after the answer and renews it only while that run
lasts; nothing keeps it alive afterwards.

Only one mutating Nimbus operation may run at a time. Locks identify the
operation and process and are never removed solely because they are old. The
normal user holds a kernel advisory lock at
`$XDG_RUNTIME_DIR/nimbus/operation.lock`. Nimbus creates the containing
directory with mode `0700` and the lock file with mode `0600`; both are owned by
that user. A missing, foreign, symlinked, or otherwise invalid runtime directory
blocks mutation.

The lock is acquired after the answer but before any manifest write, sudo, or
other mutation, and is held through verification and receipt recording. Its
content identifies the command, operation ID, PID, and start time for
diagnostics. Kernel lock state is authoritative: stale content is replaced only
after the file is successfully locked, never deleted merely because it is old.
Read-only commands and plan review may run concurrently when they can obtain
consistent input; `sync --plan` is one.

The operation lock rejects symlinked directories and symlinked, non-regular,
foreign-owned, or multiply linked lock files before writing diagnostic data.

## System files, triggers, and migrations

The generic system-file provider manages regular files below `/etc` only. Its
sources live below `system/root/etc/`. The checkout-relative suffix determines
the target directly: for example,
`system/root/etc/modprobe.d/nvidia.conf` maps to
`/etc/modprobe.d/nvidia.conf`. A file resource cannot declare a different
target. It declares the source, desired ownership and mode, verification,
change triggers, removal, and recovery. Nimbus shows the complete diff before
apply.

Static validation rejects an empty suffix, path traversal, symlinks or special
files in the definition tree, and any mapping that does not remain below
`/etc`. Observed-system validation checks every existing target-path component
without following symlinks. The provider refuses a target whose path contains
a symlink, whose type is not a regular file, or whose ownership belongs to
another provider or cannot be established. Taking over such a target requires
an explicit typed migration with its own preflight and recovery.

For an intentional live edit to an already managed generic system file, the
user may run `nimbus files accept /etc/PATH` to propose the reverse flow from
the observed target into its existing source below `system/root/etc`. The
command accepts exactly one absolute `/etc` target that is already selected as
a generic system-file resource and whose current successful receipt proves
Nimbus ownership. The target and every path component must pass the normal
non-symlink and regular-file checks, and the normal user must be able to read
the file without sudo. Foreign, unknown, unselected, unreadable, or non-`/etc`
targets are refused.

The command shows the reverse content diff and exact checkout source, reminds
the user that system-file sources cannot contain secrets, and requires explicit
approval. It then takes the normal operation lock, rechecks the checkout,
target, ownership receipt, and proposed definition digest, and atomically
updates only the source content. It preserves the source file's executable
state and never changes the resource's declared target owner, group, mode,
triggers, removal, or recovery metadata. It invokes no sudo, changes no live
system file, writes no receipt, and performs no Git operation. The resulting
checkout change remains uncommitted. The user runs validation and the normal
plan and apply flow afterward; apply verifies and adopts the now-matching live
file under the new definition identity before recording a receipt.

The generic provider never targets `/usr`, `/boot`, `/var`, `/run`, `/tmp`,
`/home`, `/root`, `/proc`, `/sys`, `/dev`, or any other root. Files below
`/usr` are delivered by a native package or by a separately specified typed
integration with exact targets. Boot resources, Nimbus state, user files, and
runtime paths likewise retain their own providers and safety contracts rather
than widening the system-file provider. All system-file content remains subject
to the prohibition on secrets.

Triggers use reviewed stable IDs and fixed argument vectors. A trigger runs at
most once per apply even when several resources request it. Arbitrary shell is
not a trigger or resource type.

A migration handles a versioned transition that steady-state reconciliation
cannot safely express. It has applicability checks, preflight, an exact plan,
verification, recovery, and a completion receipt. Normal installation must not
become a growing sequence of migrations.

## Bootstrap and initialization

The supported installation entry point is:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash
~~~

Nimbus and the dotfiles repository are intended to be public. Fresh clones use
HTTPS without GitHub authentication; access to secrets needed by templates is
separate and remains with the owner's secret provider. Changing GitHub
visibility is an explicit publication action outside installation. A private
fork needs authenticated Git before cloning; its private raw installer cannot
be fetched anonymously. Bootstrap never stores a token or configures login.

Engine source releases are manual. The owner supplies an existing stable
`vMAJOR.MINOR.PATCH` tag reachable from main to `Prepare release`, dispatched
on main. Push and tag events never release. The workflow checks the tagged
commit, exports its tracked source with generated Go dependencies and notices,
and verifies an offline build before creating a GitHub draft release. Assets
include the source archive, its exact commit and toolchain/module inventory,
and SHA-256 checksums. Build and test jobs have read-only repository access;
only the separate draft-creation job receives release write permission.

Publication and COPR build submission remain separate manual actions. The
workflow never moves a tag or overwrites an existing release. Failed source
checks create no release; an incomplete draft after an upload failure requires
inspection before retry. The source archive matches the version selected in
the COPR spec; COPR compiles, tests, and signs the installable engine RPM.

Running it explicitly trusts the current `install.sh` on the approved Nimbus
`main` branch. This remote entry point is deliberately small. It:

1. requires a controlling terminal and refuses root
2. verifies the supported Fedora release and architecture
3. starts private installation logs and obtains sudo once for prerequisites
4. obtains the required Git transport through DNF when absent
5. clones the approved origin to `~/.local/share/nimbus`, or validates and
   reuses an existing checkout there without changing it
6. invokes that checkout's versioned `bootstrap` script with validated arguments

An existing target is accepted only when its canonical checkout has the
approved normalized origin and passes containment checks. A symlinked checkout
root is accepted under the normal checkout rules. The installer never fetches,
pulls, resets, stashes, changes a remote, replaces a directory, or resolves a
conflict. Every other existing target is an error.

The outer Bash process reads `install.sh` from a pipe, so it must not replace
its own standard input. It runs the checked-out bootstrap script with that
child's standard input redirected from `/dev/tty`; absence of a controlling
terminal is an error before mutation. The checked-out script revalidates the
checkout and uses the DNF-owned `/usr/bin/nimbus`. A different engine ahead
of that path is an error. Before a fresh installation, it requires the reviewed
COPR public key and fingerprint from `system/keys/nimbus.asc` and
`system/keys/nimbus.fingerprint`. These files contain the verified project
key; replacing them requires review of the new key and its signed RPM.
No fingerprint is accepted from an environment variable.

Bootstrap checks that the key contains exactly the pinned, valid primary key,
then downloads the x86_64 engine from `furyfree/nimbus` using an isolated DNF
repository configuration. Native RPM verification requires both signature and
digests with an isolated keyring containing only that key. A failed download,
wrong signer, unsigned package, or wrong package identity stops installation.
Only after verification does bootstrap install its public key and repository
file, import the key, and install the verified local RPM and its dependencies
through DNF with `-y`. Git, GPG, and the engine are bootstrap prerequisites;
their displayed commands need no separate yes prompt. The normal-user shell
supervises a bounded sudo keepalive through init and stops it on exit. The
engine asks for machine/profile selection as soon as it is available, then
shows the workstation plan and asks once before installing its resources.
Existing bootstrap files must match exactly; foreign
files and symlinks are left untouched. The source is restricted to `nimbus`.

The bootstrap key and repository remain for native DNF updates. Temporary
downloads and the isolated verification keyring are removed on success or
failure. A failed DNF transaction leaves any completed source preparation in
place for inspection and retry. With the engine present, bootstrap runs:

~~~text
nimbus init --checkout ~/.local/share/nimbus
~~~

Neither installation script installs Chezmoi, applies workstation resources,
or duplicates Nimbus planning. The reviewed first Nimbus sync installs
Chezmoi when desired, after which init initializes it if needed and applies
its local source. Git and repository resources installed during bootstrap may be
adopted when they belong to desired state. The running Nimbus engine remains a
DNF-owned prerequisite outside Nimbus resource ownership.

The approved plan also covers disabling duplicate providers created by
package transactions. Sync checks repositories after system installation and
upgrade, shows exact native DNF overrides, and verifies convergence before
continuing. Vendor repository files and signing keys remain intact. Canonical
source or key changes beyond these duplicate overrides require a new run.
The policy is included in the plan JSON and approval digest.

The installation is rerunnable. It reuses only already-valid pieces and stops
on ambiguity; partial DNF state remains recoverable through DNF, and a cloned
checkout remains ordinary user-owned Git state. DNF invoked directly by the
user or normal Fedora tooling owns later engine upgrades and removal. The user
owns checkout updates through normal Git. Nimbus never updates its engine or
checkout.

nimbus init:

1. validates the engine and checkout compatibility and reads the checkout's
   Git origin, which becomes the selector's approved origin
2. reads the hardware: the DMI product and board names, the chassis kind,
   and the display adapters by PCI vendor and device
3. asks which machine this is, listing the tracked manifests with the one
   whose declared `hardware` appears in the DMI names pre-selected, plus
   "new"; `--machine ID` answers without the menu, and an existing selector
   for the same checkout is reused
4. for a new machine, given as `--new ID`, asks for its profiles, its
   components with the ones the hardware detection rules propose
   pre-selected, and its dotfiles repository, defaulting to the one every
   tracked manifest shares; `--dotfiles URL` and `--no-dotfiles` answer the
   last question and are rejected without `--new`; it then writes
   `machines/<id>.toml` with the DMI product as `hardware` and leaves the Git
   change to the user
5. writes ~/.config/nimbus/config.toml; replacing an existing checkout or
   approved origin requires a separate trust confirmation with an explicit
   yes, even with `-y`
6. runs `nimbus sync` for the machine, which shows the plan and asks once;
   `-y` answers yes
7. initializes Chezmoi when the manifest names a dotfiles repository and
   no source is initialized yet, then runs `chezmoi apply`; an existing
   source is applied locally without fetching or forcing overwrites, after
   checking its origin and stored selection match the chosen machine
8. runs a second user-scope pass only when explicit Nimbus-owned Cargo
   references or installer commands require it, under the approval already
   given; a failed apply skips these dependencies. The tracked manifests
   need no second pass: Chezmoi apply includes their Mise tool installation
9. prints succeeded, failed, and skipped stages, with reasons; required work
   that fails or remains pending makes init exit unsuccessfully

The new manifest is validated before writing. Init holds the operation lock
from the manifest and selector writes through its final stage. Platform checks
precede every mutation. Definitions are reloaded after approval and between
execution passes; changed selection or definitions require a new run. Selection
edits carry the reviewed plan digest into sync. EOF without an answer is never
approval.

A machine manifest may carry `hardware`, a substring of the DMI product or
board name, and a component may carry a `[detect]` table with `chassis`,
"laptop" or "desktop", or `display_vendor`, the PCI vendor ID of a display
adapter. Detection proposes; it never decides, and resolution never inspects
hardware.

Fresh installation and reinstallation use the same tracked machine manifest.
Creating a new machine writes a reviewed manifest to the Nimbus checkout and
leaves the resulting Git change for the user.

Nimbus works without a dotfiles repository. In that case the system remains
usable and dotfiles are reported as skipped. Chezmoi-owned tools require a
separate full apply; explicit Nimbus-owned user tools still require their
declared configuration and runtime prerequisites to complete.

Nimbus may cause an explicitly selected dotfiles repository to be cloned only
through the first supported Chezmoi initialization. The explicit
`nimbus dotfiles update` command delegates remote updates to Chezmoi, including
its native Git behavior. Outside these operations Nimbus performs no Git
network mutation. It never changes the Nimbus checkout's remote, commits,
pulls, pushes, resets, stashes, or resolves conflicts.

## Chezmoi handoff

Nimbus passes only the context needed by user-file templates:

- `machine`: the selected machine ID
- `managed_by_nimbus`: `true`
- `profiles`: the ordered selected profile IDs

The transport uses Chezmoi's prompt flags, whose keys are the template's prompt
texts:

~~~sh
chezmoi init \
  --promptString Machine=<machine id> \
  --promptBool ManagedByNimbus=true \
  --promptBool 'Enable 1Password SSH integration=false' \
  --promptMultichoice 'Profiles=<id>/<id>/...' \
  <dotfiles repository>
~~~

Fresh Nimbus initialization answers the optional 1Password SSH prompt with
false. `--onepassword-ssh`, forwarded by both shell entry points, opts in. It
excludes `--no-dotfiles`. Existing initialized sources retain their stored
answer; an explicitly requested opt-in against a disabled existing source
prints the native refresh command instead of silently rewriting it. The
1Password applications and closing setup instructions remain available.
Standalone Chezmoi retains its normal interactive prompt.

When reading `chezmoi data`, Nimbus uses the exact `Machine`, `ManagedByNimbus`,
and `Profiles` keys. The lowercase `profiles` key is dotfiles' derived platform
list, not the machine selection; JSON key casing must remain significant.

Nimbus never edits Chezmoi internal state directly. The dotfiles repository is
cross-platform and standalone: direct Chezmoi initialization prompts for the
machine name and profiles, sets `managed_by_nimbus` to `false`, and works on
Linux, macOS, and Windows without Nimbus. Templates gate on
`managed_by_nimbus` only for targets that call Nimbus, such as the desktop
entry for `nimbus windows connect`. Hyprland and Noctalia configuration is
selected by profile and never implies Nimbus. Hardware facts and secrets do not
cross the handoff. The handoff keys are documented in the dotfiles repository's
`PROFILES.md`.

The common profile selects Mise and its build prerequisites. Sync installs
Mise before the handoff: it downloads the maker's installer from
`https://mise.run` to a file, shows the digest, runs that file as the normal
user, and verifies
`~/.local/bin/mise`. The component declares the installer, binary, and system
build prerequisites, without a tool installation command. Source builds need
Make, pkg-config, and the OpenSSL, curl, zlib, and libudev development packages
as well as the compiler toolchain; Nimbus installs them before the handoff.
Chezmoi writes `~/.config/mise/config.toml` and the Linux tool fragment at
`~/.config/mise/conf.d/cargo.toml`; Nimbus never edits those files.
The common profile supplies Typst through `terra:typst`; Chezmoi does not
declare a second Typst installation through Cargo.

On Linux and macOS, every full Chezmoi apply runs an after script that invokes
Mise from the home directory after writing configuration:

~~~sh
MISE_SYSTEM_DEPS=warn MISE_AUTO_UPDATE=false mise -C "$HOME" install
~~~

The script requires an existing Mise installation and runs as the normal user.
Missing Mise or a failed installation fails apply visibly. It is not gated by
`managed_by_nimbus`, so standalone Chezmoi use installs the same declared
user tools. It renders empty on Windows, where the Mise configuration is not
deployed. Read-only diff and preview do not run installations.

`MISE_SYSTEM_DEPS=warn` leaves system dependencies to Nimbus or the standalone
operator. `MISE_AUTO_UPDATE=false` prevents an incidental Mise binary update
during apply; upgrades remain explicit through native Mise or the separately
approved user update lifecycle. The script never bootstraps system packages or
escalates privileges. Configuration for an application does not imply its
installation: only the native Mise declarations select user tools.

Mise installs Tinymist, Sheldon, and resvg through its native registry's
Aqua release backends. Caligula and VM Curator use native GitHub release
backends with the appropriate upstream binary assets. All select `latest`
stable; native `mise upgrade`, including the existing Topgrade Mise step,
updates them without editing pinned tags. Normal Mise checksum verification
remains enabled. Cargo binaries are no longer globally disabled, and
`cargo-update` is removed because Mise already owns these updates.
After installing and checking the replacement executables, the after script
prunes only the six obsolete Cargo tool identities through native Mise.
Versions still required by other tracked configurations and unrelated tools
remain installed. A replacement failure prevents this cleanup.
The script runs on every full apply, rather
than only when configuration changes, so reapplying restores missing tools.
Nimbus reports the handoff as one dotfiles and tools stage; individual tool
results remain visible in Mise's output. Existing direct installations in
`~/.cargo/bin` are neither adopted nor removed; cleanup is an explicit action.

Init repeats explicit `Setup note:` lines from the Chezmoi handoff after its
closing stage summary, even when apply or tool installation fails. Native
output remains visible as it arrives. Dotfiles emits applicable shell,
1Password, and desktop setup instructions before starting Mise; Nimbus also
marks the Chezmoi answer-refresh command. Only these marked instructions are
retained in memory, deduplicated, bounded to 32 lines of at most 4096 bytes,
and never executed as commands. Control characters are excluded
from the closing notes.

Installation logs live in private per-run directories below
`${XDG_STATE_HOME:-~/.local/state}/nimbus/install`. The path is printed at
startup and completion. Bootstrap supervises the complete run without an
external terminal recorder or capturing keyboard input. Direct init creates
the same layout. Keep the newest 20 completed runs; active runs and directories
with unknown files or unsafe ownership are preserved. Symlinks and shared or
hardlinked log files are rejected. Logging failures make the run fail visibly.

`bootstrap.log` holds bootstrap commands, output, key checks, exit status,
and elapsed time. `engine.log` adds selection, source identity, plans, native
package output, command timing/status, repository reconciliation, and stage
and total timings. The Mise hook writes its own `mise.log`, including full
native install and migration output. Successful inspection output from safe
package commands is logged even when the terminal only needs a summary.
Chezmoi output, arbitrary user scripts, secret-capable command results,
authentication input, environment dumps, and template renderings are excluded;
their lifecycle status is recorded while native diagnostics remain visible.
Read-only commands never create these logs. Logs are local diagnostic state,
never receipts or repository content.

Ordinary sync neither invokes Chezmoi nor installs tools from these dotfiles
configurations. The convenience commands delegate directly:

~~~sh
nimbus dotfiles diff    # chezmoi diff, local inspection
nimbus dotfiles apply   # chezmoi apply, existing local source
nimbus dotfiles update  # chezmoi update, native Git pull and apply
~~~

Apply and update use the operation lock and preserve Chezmoi's conflict and
secret handling, including its user-tool scripts and their native errors.
No force flag is added. Native Chezmoi commands remain usable,
and removing Nimbus leaves its checkout and lifecycle intact. Failed or
unverifiable work is reported separately from completed operations. Independent
user-tool steps can continue after another user tool fails; dependent steps
stay skipped. Native package transaction failures stop dependent system work.
DNF changes are compared before and after install, removal, and upgrade,
including architecture, version differences, and collateral changes. A failed
native command retains observable partial changes in the report; incomplete
verification never becomes a claim that no differences occurred.

Initial source initialization runs once; later explicit init runs apply again.
When the selected profiles change later, the
`profiles add` and `profiles remove` commands end by printing the direct
refresh command, and `nimbus doctor` reports a mismatch between the manifest
profiles and Chezmoi's stored selection using Chezmoi's read-only data
output. Nimbus never reruns the initialization itself. The template uses
`prompt*Once` functions, which keep a stored answer, so the refresh forces
them to prompt again and supplies every value, including the current
1Password answer read beforehand through `chezmoi data`:

~~~sh
chezmoi init --prompt \
  --promptString Machine=<machine id> \
  --promptBool ManagedByNimbus=true \
  --promptBool 'Enable 1Password SSH integration=<current>' \
  --promptMultichoice 'Profiles=<id>/<id>/...'
~~~

The dotfiles template declares the three Nimbus keys and stores the profile
list as sent, without checking it against a fixed set, so a new Nimbus profile
never breaks the handoff. On Linux it deploys every user configuration
unconditionally except the Hyprland and Noctalia files, gated on
`hyprland-noctalia`, and the entries that call Nimbus, gated on
`managed_by_nimbus`. macOS and Windows use their platform profiles and deploy
only what applies to them.

## Desktop and recovery session

The hyprland-noctalia profile owns the system requirements for Hyprland,
Noctalia, the greeter, portals, session entries, and compatibility-sensitive
updates. Chezmoi owns normal user configuration for Hyprland and Noctalia.

Nimbus installs a separate minimal system-owned recovery session under /usr.
It uses an explicit system configuration, opens a terminal, provides minimal
recovery bindings, and does not depend on user dotfiles. It works before and
after Chezmoi apply and is removed with its owning component.

Nimbus never writes a seed compositor configuration into the user's home
directory.

## Manual tasks and runtime commands

Components may contribute typed manual tasks for work requiring human
interaction, such as signing in to 1Password, enrolling an NVIDIA MOK,
enrolling a fingerprint, completing a Windows guest, or rebooting.

A task has a stable owner, prerequisites, status check, instructions or a typed
action, verification, completion state, recovery, and reboot or logout
requirements. Status checks remain authoritative after a receipt. Tasks are not
arbitrary shell scripts.

Runtime command groups operate only on already selected and applied components.
A group's `setup` command is the one explicit entry point that may stage the
owning profile and take it through the normal plan and approval; every other
runtime command refuses when the component is not applied and names `setup`.

`nimbus postinstall` is the terminal presentation of typed pending work from
the selected components and observed state. It can present 1Password readiness,
fingerprint or MOK enrollment, Windows setup, logout, and reboot. Selecting an
action invokes that task's same typed status, instructions, approval,
verification, and recovery contract; it never discovers or executes arbitrary
scripts. In a non-terminal context it prints the pending tasks and their direct
commands without selecting one. Certificates, gaming packages, virtualization
host packages, services, groups, and networks remain normal plan and apply
resources rather than post-install actions.

### Windows guest

The `windows-vm` component, selected by the `windows-vm` profile, runs one
Windows guest through the `dockurr/windows` container on Docker with KVM. It
requires the `docker` component, which the `development` profile also selects
and which owns the Docker packages, `docker.service`, the daemon settings, and
the owner's `docker` group membership as [SECURITY.md](SECURITY.md) specifies.
Apply installs FreeRDP and KVM support, the container image pinned by digest,
and a root-owned Compose definition. Nimbus does not wrap libvirt or Quickemu
and does not manage arbitrary VMs or containers.

Persistent guest data is contained below:

~~~text
/var/lib/nimbus/windows/
  compose.yaml       root-owned, rendered from desired state
  credentials.env    owned by the normal user, mode 0600
  storage/           bind-mounted into the container as /storage
~~~

The directory is the mountpoint of the separate `windows` Btrfs subvolume, so
guest data never enters a recovery point. The Compose definition declares the
`/dev/kvm` and `/dev/net/tun` devices, the `NET_ADMIN` capability, the
`storage/` bind mount, `env_file: credentials.env`, and ports 8006 (web
installer and recovery console) and 3389 (RDP) bound to `127.0.0.1` only.
Windows version, RAM, CPU, and disk size are typed desired data in the
definitions, not prompts. Every path operation rejects traversal, symlinks,
unexpected file types, and foreign content.

`credentials.env` holds only the guest `USERNAME` and `PASSWORD`. It is
created by `windows setup`, is never tracked, rendered into the checkout, shown
in a plan or diff, logged, or recorded in state or receipts, and is deleted
only by `purge-data`. `status` reports its presence, owner, and mode and never
its content.

The public lifecycle is:

~~~text
nimbus windows setup
nimbus windows status
nimbus windows start
nimbus windows connect [--keep-alive]
nimbus windows stop
nimbus windows remove
nimbus windows purge-data
~~~

`setup` is the single entry point and is rerunnable. When the `windows-vm`
profile is not selected it stages the same manifest change as
`profiles add windows-vm`, shows the manifest diff and complete system plan,
and applies after approval. When the component is selected but drifted it
shows and applies that plan. It then prompts for the guest credentials when
`credentials.env` is missing, starts the container for the unattended Windows
installation when `storage/` is empty, and directs the user to the web console
for anything the unattended path cannot finish. On an installed guest it
reports the state and points to `connect`. `status` is read-only and reports
component, host, container, storage, credentials-file, and RDP and web-port
state. `start` and `stop` run Compose on the root-owned definition as the
user through the `docker` group membership the `docker` component declares.
`connect`
starts the guest when needed, waits for the container to report Windows as
started, opens FreeRDP against `127.0.0.1:3389` with the stored credentials,
and stops the guest when the session closes unless `--keep-alive` is set.
`status`, `start`, `stop`, and `connect` never install a missing component;
they name `setup`.

`remove` is `profiles remove windows-vm` with the same diff, plan, and
approval. The resulting owned removals stop and remove the container and the
safe owned host integration while preserving `/var/lib/nimbus/windows/`, and
the command ends by naming `purge-data`. `purge-data` requires the component to
be removed or an otherwise explicit purge context, a stopped guest, an exact
resolved path below the fixed root, proof that Nimbus owns the data, and a
second confirmation. It deletes only that data, including
the credentials file. The container and Compose definition can be recreated
from desired state, but the guest disk is excluded from Nimbus recovery points
and may be lost and recreated from external sources. Purge has no Nimbus
rollback.

### Desktop launch helpers

The narrow desktop helpers are:

~~~text
nimbus launch browser [URL] [--private]
nimbus launch webapp URL
~~~

The browser helper resolves the XDG default browser and translates private-mode
arguments for supported browser families. The webapp helper uses that browser
when it supports application mode or a configured Chromium-family fallback.
Both accept only `http` and `https` URLs, parse desktop entries without shell
evaluation, and launch an exact argv vector. Chezmoi may call them from its
owned keybindings and desktop entries. They do not install browsers, write user
configuration, or become generic process launchers. How desktop entries for
terminal applications open a terminal is Q-019 in DECISIONS.md.

### Interactive dashboard

Bare `nimbus` in a terminal opens the dashboard once it is delivered. In a
non-terminal context, or with `--json`, bare `nimbus` prints grouped help. The
dashboard is a presentation layer over the same resolver, facts, plans, tasks,
and commands. It owns no business logic, state, configuration, or approval
rule of its own, and every action it offers exists as a command whose name it
shows.

Its screens are:

- Overview: selector, machine, checkout identity, doctor summary, status
  counts, and pending manual tasks.
- Profiles and Components: every ID defined in the checkout with its selected
  state and selection path. Toggling an entry stages a manifest change.
- Packages: the `packages installed` view and the `packages install` and
  `packages remove` pickers.
- Review: the staged manifest diff and the complete system plan, the approval
  step, apply progress, verification, and the receipt summary.
- Tasks: the `postinstall` list and its typed actions.

Staged edits exist only in the running session. Approval writes the manifest
atomically and applies through the normal path with the same lock, digest
refusal, and privilege boundary; leaving the dashboard discards unapproved
edits. Read-only screens never invoke sudo or mutate. `nimbus init` reuses the
machine, profile, component, and package screens when creating a new machine,
so a machine can add individual packages beyond its profiles before the first
apply. Profiles and components themselves are authored by editing their files
in the checkout and running `nimbus validate`; the dashboard edits only the
machine manifest. The
dashboard is keyboard-driven, works without a mouse, and shares its widget
library with the command-line pickers.

## Updates and recovery

Normal Fedora updates remain visible through DNF. Nimbus defines the expected
managed packages, repository state, enforceable constraints, coordinated update
groups, recovery requirements, and post-update verification. Nimbus does not
silently update itself, its checkout, dotfiles, or system packages.

`nimbus sync` is the primary entry point for normal workstation updates as
well: its last step upgrades the system with `dnf5 upgrade` and `flatpak
update`, after the definition changes of the same run, so drift and updates
are one decision. `--no-upgrade` leaves that step out. Recovery points and
reboot or logout requirements join the plan when Phase 7 delivers them. The
upgrade operates only within the currently installed Fedora release and has
no target-release flag.

After the verified system phase, the command offers a separate Topgrade phase
for the user-scope update managers. Topgrade reads the user's own
Chezmoi-owned configuration; Nimbus installs the Topgrade package and passes
`--only` with the declared allowlist of user-scope steps plus
`--no-self-update`, so system, Flatpak, firmware, Nix, Chezmoi, and
Git-repository steps, and any step Topgrade adds later, never run from this
phase. The reviewed plan identifies every allowed Topgrade step and its
command, and Topgrade runs as the normal user without sudo. The phase is
command-level review: Topgrade dry-run does not resolve the exact downstream
versions selected by Mise, Cargo, npm, uv, and similar managers, so Nimbus
does not describe those mutations as exact, managed, or recoverable
transactions.

The recovery point's pre snapshot is created before the system mutation and
its post snapshot as soon as the system phase is verified, before the
Topgrade phase starts. It covers the root and system Flatpak subvolumes
only. User tools below the home subvolume are
outside that recovery boundary; a failed Topgrade step is repaired through its
own manager or by reconstructing the declared user environment.

Fedora release upgrades are permanently owned by Fedora's native DNF5
system-upgrade workflow and the user. Nimbus never invokes or wraps that
workflow, prepares or approves its offline transaction, requests its reboot,
records it as a Nimbus operation, or claims recovery for it. Fedora's native
documentation, transaction log, and recovery procedures remain authoritative.

The selected checkout declares its supported Fedora releases. `nimbus doctor`
reports the installed release and that compatibility without claiming to
preflight a future native transaction. `nimbus version` and static
`nimbus validate` remain available on an unsupported installed release, and
doctor can report the incompatibility, but Nimbus refuses system mutation until
the installed release is supported by the compatible engine and checkout.
There is no special Nimbus release-upgrade version beyond the normal declared
minimum-engine contract.

Before starting the native Fedora workflow, the user ensures that the installed
engine and selected checkout declare support for the target release. After the
native upgrade succeeds, the user runs `nimbus validate`, `nimbus status`, and
then `nimbus sync` as needed. These commands inspect and repair
Nimbus-owned drift after the event; they do not retroactively make the Fedora
release upgrade a Nimbus operation.

The running Nimbus engine itself is excluded because its lifecycle remains a
direct DNF operation rather than a Nimbus self-update.

The first recovery-point implementation supports only this installed layout:

~~~text
UEFI/GPT
|- 1 GiB FAT32  /boot/efi
|- 2 GiB ext4   /boot
`- remaining    LUKS2
   `- Btrfs "fedora"
      |- root         /
      |- home         /home
      |- snapshots    /.snapshots
      |- log          /var/log
      |- cache        /var/cache
      |- swapfile     /var/swap
      |- flatpak      /var/lib/flatpak
      |- windows      /var/lib/nimbus/windows
      |- docker       /var/lib/docker
      `- containerd   /var/lib/containerd
~~~

There is no separate `/var` subvolume. Nimbus validates this topology and never
creates, converts, repartitions, or encrypts it on a mounted live system. A
different layout remains usable for operations that need no recovery point, but
blocks every operation whose provider requires one.

Snapshots are created and deleted through Snapper, never through direct
`btrfs subvolume` commands. Nimbus declares the Snapper configurations for the
`root` and `flatpak` subvolumes as system resources with timeline snapshots and
background cleanup disabled, so only Nimbus creates and retires recovery
snapshots. Immediately before such a mutation, Nimbus creates a recovery point
consisting of:

- a read-only Snapper pre snapshot of `root`, paired with a post snapshot after
  the operation
- the same pair for `flatpak` only when system Flatpaks will change
- `/boot` and `/boot/efi` archives only for a kernel, initramfs, bootloader,
  NVIDIA boot-integration, or EFI change
- a manifest with the Snapper snapshot numbers, source subvolume and
  filesystem UUIDs, archive checksums, operation and plan digests, and
  completion state
- a standalone restore guide containing the discovered device-independent
  mount and restore inputs

Archives, manifest, and guide live in `/.snapshots/nimbus/<id>/` as
`root:root` state with mode `0700`; files within it use mode `0600`. Snapshot
descriptions and userdata carry the Nimbus operation ID so Snapper's own
listing identifies them.

Nimbus verifies every requested item through Snapper metadata and its own
checksums before marking the point complete; a partial point never authorizes
the mutation. A partial point created by the current attempt is removed through
Snapper as one proven-owned unit before any mutation; failed cleanup blocks the
operation and preserves it for inspection. The `root` snapshot includes Nimbus
state below `/var/lib/nimbus`, while `home`, `log`, `cache`, `swapfile`,
`windows`, `docker`, and `containerd` are separate subvolumes and are excluded.
Nimbus provides no backup lifecycle. The workstation holds no canonical-only
data: user files are synchronized or stored externally, and local guest and
container data may be lost and recreated. A recovery point is local same-disk
state and is never described as a backup.

Nimbus retains the newest three complete recovery points. A point tied to an
unresolved failed operation is marked important in Snapper userdata, is
protected, and does not count as an eligible old point. Before creating
another, Nimbus may delete only older complete eligible points through Snapper,
wait for Btrfs deletion to finish, and remeasure using Btrfs-aware usable space
rather than `df` alone. If less than 20 GiB remains, Nimbus blocks before
mutation. It never deletes home, VM, container, unknown, partial, protected, or
non-Nimbus snapshots to make room.

The initial restore procedure is manual and independent of a working Nimbus
binary:

1. Boot a supported Fedora live or rescue environment.
2. Unlock the recorded LUKS2 container and mount the Btrfs top level plus the
   `snapshots` subvolume.
3. Verify the selected recovery manifest, snapshot identity, and boot-archive
   checksums.
4. Preserve the failed `root`, then create a writable `root` snapshot from the
   selected read-only Snapper snapshot below `/.snapshots`.
5. Restore the matching `/boot` and `/boot/efi` archives when the manifest
   contains them; leave every excluded subvolume, including `home`, untouched.
6. Reboot, run doctor and status, and retain the failed root until the restored
   system is explicitly accepted.

`snapper rollback` assumes a different default-subvolume layout and is not the
supported restore path. The generated guide records the exact discovered UUIDs,
snapshot numbers, and paths needed for these steps. Recovery-point creation
remains disabled until the Snapper fit check in [ROADMAP.md](ROADMAP.md) has
passed and this complete path, including boot archives, succeeds in a
disposable Fedora VM. Nimbus provides no automatic rollback.

## Security

[SECURITY.md](SECURITY.md) owns the workstation policy: accepted software
sources and their trust pins, disk encryption, Secure Boot, SELinux, firewall,
privilege, secrets, and the doctor checks. The engine invariants are:

- Configuration, plans, state, receipts, and logs contain no secrets.
- External artifacts require an approved source and cryptographic digest or
  supported signature.
- Remote shell scripts are not a resource type. A maker's installer script is
  downloaded to a file, shown with its digest, and run only as the normal
  user; Nimbus never pipes a download into a shell and never runs one as
  root.
- Native package signatures remain enabled, and a repository is enabled only
  with a pinned release package or key.
- Paths are validated against traversal, symlink escape, and unsafe ownership.
- Destructive operations name their exact target and require explicit approval.
- Unknown ownership prevents automatic removal.
- Guest management interfaces bind to loopback unless explicitly configured.
- Guest credentials exist only in the protected credentials file.
- Nimbus never disables Secure Boot.
- Secure Boot key enrollment is explicit manual work with verification.
- Nimbus never mutates live disk partitioning or root encryption.
- Failed verification prevents a success receipt.

## Command contract

The public CLI is:

~~~text
nimbus
nimbus init
nimbus dotfiles diff
nimbus dotfiles apply
nimbus dotfiles update
nimbus validate
nimbus status
nimbus sync [-p|--plan] [-y|--yes] [-n|--no-upgrade] [-r|--prune]
nimbus postinstall
nimbus packages install [QUERY]
nimbus packages remove [QUERY]
nimbus packages installed [QUERY]
nimbus profiles list
nimbus profiles add [ID...]
nimbus profiles remove [ID...]
nimbus components list
nimbus components add [ID...]
nimbus components remove [ID...]
nimbus files accept /etc/PATH
nimbus managed
nimbus unmanaged [--all]
nimbus why RESOURCE
nimbus windows setup
nimbus windows status
nimbus windows start
nimbus windows connect [--keep-alive]
nimbus windows stop
nimbus windows remove
nimbus windows purge-data
nimbus launch browser [URL] [--private]
nimbus launch webapp URL
nimbus doctor
nimbus version
~~~

Bare `nimbus` opens the interactive dashboard described above when one is
delivered and a terminal is available; otherwise it prints grouped help. A
release lists only commands it implements completely. Future mutating commands
never appear as stubs.

`nimbus validate` is the configuration authoring check. It validates the full
selected checkout and resolves every tracked machine, reports every discovered
error with its source location, and performs no system inspection. It accepts a
checkout override but no machine override because validation is checkout-wide.

`nimbus status` is the concise desired, observed, and last-applied overview.
`nimbus sync --plan` is its complete non-mutating explanation. `nimbus managed`
lists resources Nimbus owns or has explicitly adopted; `nimbus unmanaged` lists
only
supported resources Nimbus can identify but does not own, not arbitrary user
data. `nimbus why RESOURCE` reports every desired, dependency, adoption, and
ownership path for one canonical resource.

The package commands are focused conveniences over the same desired-state and
apply lifecycle:

- `packages install [QUERY]` opens a multi-select package picker, prefilled by
  the optional query, and adds approved canonical references to the selected
  machine manifest.
- `packages remove [QUERY]` selects from desired or Nimbus-managed packages. It
  removes an explicit machine reference or adds a valid profile exclusion, and
  refuses technical dependencies, protected packages, foreign ownership, and
  unknown removal lifecycles.
- `packages installed [QUERY]` is read-only and browses explicitly installed
  supported packages. It labels each package as managed (a receipt exists),
  adopt (desired and installed from an acceptable source), blocked (desired
  but installed from a source the plan refuses), pre-existing (in the
  baseline), unmanaged (installed by hand since), or dependency, and shows
  its selection provenance.

Install and remove show the proposed manifest diff and complete system plan,
then require approval before writing the manifest atomically and applying it.
They leave the Git change for the user and never commit, pull, or push. A failed
apply leaves the reviewed desired configuration present and reports the
remaining drift. The remove picker does not offer unmanaged packages; eligible
unmanaged removal remains part of `sync --prune`. In a non-interactive context,
a package command that still requires selection or approval fails rather than
guessing.

The selection commands edit the two manifest lists a user would otherwise
change by hand:

- `profiles list` and `components list` are read-only. They show every profile
  or component defined in the checkout, whether the selected machine selects
  it, and through which path.
- `profiles add [ID...]`, `profiles remove [ID...]`, `components add [ID...]`,
  and `components remove [ID...]` change the selected machine manifest. Without
  IDs they open a multi-select picker over the valid choices. Remove offers
  only entries the manifest names explicitly; a component selected through a
  profile is removed by removing the profile.

They follow the package workflow: show the manifest diff and the complete
system plan, require approval, write the manifest atomically, apply through
the normal path, and leave the Git change for the user. Removing a profile or
component turns its no-longer-desired resources into owned removals in that
plan. A profile change on a machine with an initialized Chezmoi checkout ends
by printing the direct `chezmoi init` command with the new `Profiles` value.

`nimbus files accept /etc/PATH` is the narrow reverse workflow for an
intentional edit to an already Nimbus-owned generic system file. It shows the
live-to-checkout diff and exact `system/root/etc` destination, requires
approval, writes only the source content, and leaves the Git change for the
user. It is not a general drift sync, does not accept multiple files, does not
capture ownership or mode, and never changes or adopts foreign system state.
Accidental drift continues to be repaired in the forward direction through
`nimbus sync`.

`nimbus doctor` performs read-only health checks for the capabilities available
in the installed engine. Each failure already includes its observation, impact,
and remediation; there is no separate explain mode. Doctor never repairs,
invokes sudo, or accesses the network. `nimbus version` and the equivalent
`nimbus --version` report engine build identity and the supported definition,
state, receipt, and structured-output schema ranges without loading a checkout
or inspecting the system.

Human output is the default. Every delivered command that returns Nimbus data
accepts `--json` and renders the same result in a versioned envelope containing
the engine version, output schema, data, and structured errors. JSON output
does not change lifecycle behavior. For sync and selection edits, `--json`
explicitly skips the approval question, as does `--yes`; it never supplies
missing machine or profile choices. Commands that hand control to a graphical
browser, console, or RDP
client reject `--json`; `postinstall --json` lists tasks without selecting one.

Commands that load desired configuration accept invocation-local `--checkout`
and `--machine` overrides where applicable. An omitted value comes from the
selector. Overrides never rewrite the selector. `validate` accepts only
`--checkout`; `version` accepts neither. Plans, approvals, and receipts identify
the effective checkout, origin, machine, commit, dirty state, and definition
digest.

Commands return conventional exit codes:

- 0 for success, including an empty result
- 1 for validation, inspection, planning, verification, or operation failure
- 2 for invalid usage

## Compatibility

- Nimbus supports Linux only.
- The first supported target is Fedora 44 on x86_64.
- The first desktop target is Hyprland with Noctalia.
- Other operating systems and desktop compositions require evidence from real
  systems before entering the supported matrix.
- Dotfiles may remain cross-platform; Nimbus manages only Linux system state.

## Non-goals

Nimbus is not:

- a dotfiles deployment or template engine
- a general Ansible, Terraform, or configuration-management replacement
- a generic third-party provider or plugin platform
- a package manager, package store, or dependency solver
- a replacement for Mise, Cargo, or Chezmoi; Nimbus runs them, it does not
  reimplement them
- a background reconciliation daemon
- a secret manager
- a generic virtual-machine or remote-host manager
- a tool for silently removing manually installed software
- a live disk partitioning or encryption tool
- a universal Linux installer
- a promise to support unknown forks or definition repositories

## System acceptance

Nimbus is complete when a supported clean Fedora installation can:

- select a tracked machine and resolve the same desired graph deterministically
- inspect the real system and show a complete non-mutating plan
- run only what the plan showed, with scoped privilege, and report what
  differed
- verify every successful mutation and write complete receipts
- report and repair owned drift without claiming unrelated state
- remove owned resources safely while preserving unmanaged data
- initialize the selected Chezmoi repository without taking over its lifecycle
- boot both the normal user session and the system-owned recovery session
- recover from each supported disruptive operation through a tested path
- expose equivalent stable human and structured command behavior

The implementation order and decision gates for reaching this contract live in
[ROADMAP.md](ROADMAP.md).
