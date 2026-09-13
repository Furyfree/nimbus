# Nimbus roadmap

Goal: a workstation that can be installed, maintained and repaired with a
small CLI and a Bubble Tea interface. [SPEC.md](SPEC.md) defines the behavior;
[TASKS.md](TASKS.md) holds current checkboxes and evidence.

This sequence replaces the old phase-by-phase implementation narrative.
Existing local code is a candidate, not proof that the new contract is shipped.

## Next steps

The prepared XDG defaults file remains unselected until explicit adoption of
the package-owned file is supported. Chezmoi already manages the working
per-user paths, so this does not block the engine release.

1. Finish the minimal dark GRUB theme and verify booting and theme removal.
2. Add FDE auto-unlock as an explicit post-install action, retaining passphrase
   unlock. Verify enrollment, booting and removal on real hardware.
3. Extract shared operations from the CLI, then build the Bubble Tea dashboard.

This is the current priority order. GRUB and FDE auto-unlock come before the
remaining CLI refactor and TUI work. Other installation checks below remain
open; this change does not mark them complete.

## Review draft: maintenance and setup workflows

Status: approved for local implementation on 2026-09-13. The candidate now
implements the maintenance/setup contract in both Nimbus and Chezmoi. The
proposal below is retained as the review record; executable usage is in
[README](../README.md#workflow) and the implemented contract is in
[SPEC](SPEC.md#everyday-commands). Release preparation targets Nimbus 0.5.0;
publication is tracked in TASKS.
The acceptance marks below record automated and disposable RPM validation;
full graphical installation and real-account checks remain explicitly separate.

### Approved completion and output follow-up

The follow-up to the 0.5.0 operator trial makes verify-only completion useful for
existing setup. This supersedes the manual-acknowledgment-only behavior described
in the original proposal below. Supported completion commands confirm manual
prerequisites, verify native configuration and record success without applying
files. The normal 1Password workflow also avoids applying matching files. MOK
permission failures offer explicit read-only privileged verification, while
routine inspection remains unprivileged and preserves uncertainty.
The subsequent operator trial established that CLI sign-in was needed despite
correct GUI settings, and that mokutil reports enrollment with exit 1. The
follow-up adds explicit native sign-in recovery and fixes enrollment parsing;
terminal handling is not a demonstrated cause of the original CLI failure.
A successful privileged MOK check now appears as "Previously verified" when
ordinary status cannot reread the certificate. The final report treats this as
a notice; observed native failures continue to take precedence.

The approved color pass shares terminal styling across text commands and help,
using the terminal palette and explicit status labels. Redirected text and JSON
stay plain; native progress and input retain their terminal connection. Nimbus
styling stays out of installer logs. The checklist shortens successful checks
and separates the sudo recheck command; help retains verification limits and
JSON retains native details. The presentation pass strengthens palette colors,
wraps terminal prose, cleans up Doctor, orders upgrade previews by execution
and aligns historical MOK preview wording with status. Automated checks use
fixtures and temporary
terminals; visual review remains with the owner.

Maintenance output groups unchanged ownership refreshes, shows only actual
replan changes and separates verification problems from setup work. Empty native
system upgrades skip transactions and snapshots after fresh checks. Usage is in
[README](../README.md#guided-setup); the implemented contract is in
[SPEC](SPEC.md#everyday-commands), with evidence in
[TASKS](TASKS.md#completion-and-output-follow-up-2026-09-13).

### Required work at a glance

Each item below is a required part of the proposal, with its own detailed
section and acceptance criteria. Local state and final reporting are explicit
deliverables, not incidental parts of another task.

1. [Improve init](#init-and-first-install-experience): visible installation,
   consolidated onboarding and a clear remaining-setup report.
2. [Simplify sync](#proposed-ordinary-sync): sudo, fresh engine check, stop for
   an available update before either Git fetch, otherwise sync/apply once.
3. [Simplify combined upgrade](#proposed-combined-upgrade): sudo, engine update
   and restart if needed, one sync, Topgrade, one final summary.
4. [Make updates reliable](#update-reliability): current repository metadata,
   explicit check failures and no recurring manual Nimbus upgrade workaround.
5. [Separate setup notes](#setup-notes): all notes through the dedicated
   command and init; only unseen relevant revisions during later syncs.
6. [Restructure postinstall](#postinstall-command-layout): help by default,
   real task subcommands, per-task help, plan mode and compact status.
7. [Guide task execution](#guided-task-execution-and-completion): prerequisite
   checks, manual confirmations, approved actions, verification and automatic
   completion. Include the full [1Password flow](#1password-walkthrough).
8. [Implement local state](#local-state-files-and-ownership): separate note
   display history and task completion, per-machine records, reset support
   and native rechecks that take precedence over old completion flags.
9. [Reduce noise and preserve progress](#output-and-progress): concise routine
   output with live downloads, prompts, errors and elapsed-time feedback.
10. [Improve final reporting](#final-reporting): changes, failures, remaining
    tasks, effective-config conflicts and reboot/logout notices in one place.

### Purpose and observed problems

Make maintenance predictable: check engine compatibility before fetching new
definitions, reconcile configuration once, show progress while work happens,
and leave a useful per-machine checklist afterward.

The desktop/laptop comparison and command review found:

- The laptop has Nimbus 0.4.5 while 0.4.6 is published. Its privileged DNF
  cache contains versions only through 0.4.5, and the upgrade log explicitly
  reuses that cache. Nimbus currently calls makecache and upgrade without
  forcing metadata refresh.
- The old engine reads newly fetched definitions before upgrading itself.
  A definition requiring a newer engine can block the upgrade path.
- A successful combined upgrade performs two full syncs, including two Git
  update passes and two Chezmoi applies with after-apply scripts.
- Onboarding notes, policy explanations, unchanged checks and intermediate
  summaries dominate routine output.
- Init's output logging changes how subprocesses connect to the terminal.
  Loss of terminal detection is a likely cause of missing progress; some
  preparation commands also capture output instead of showing activity.
  Reproduce the precise cases before selecting the implementation.
- Postinstall currently dumps all task details and treats `status` as a task
  ID. Several tasks remain generically unknown despite prior manual setup.
- Matching managed files does not prove matching effective configuration.
  The laptop's Noctalia GUI overrides disable the managed lockscreen widgets.
  The explicit lockscreen repair is now implemented; the installed laptop
  trial remains pending. Normal sync still preserves GUI overrides.

### Init and first-install experience

Init is explicitly part of this work, alongside both sync commands. Retain
its role of selecting or creating a machine, installing the required system
prerequisites and initializing Chezmoi. Improve the whole visible sequence:

1. Select or reuse the machine and show the installation plan. Preserve the
   existing trust checks, explicit 1Password SSH opt-in and approval before
   installation. Do not infer manual setup completion from `--yes`.
2. Install and configure system prerequisites with live native progress,
   including large downloads. Keep installation logging without losing the
   progress display or hiding prompts behind captured output.
3. Initialize Chezmoi and apply user configuration and declared tools with
   visible progress. Remove the repeated onboarding paragraphs from the Mise
   hook; keep actual tool installation and extension verification.
4. Present the applicable setup notes together through the same note service
   exposed by `nimbus setup-notes`, then record successfully displayed note
   revisions. Init remains an explicit place to see the applicable onboarding
   guidance; later syncs show only new or revised notes.
5. End with one installation result and a compact list of remaining tasks and
   session/reboot requirements. Point to the relevant guided postinstall
   commands instead of dumping every task's instructions and recovery text.

Do not defer a blocking prerequisite until the closing notes. For example,
if selected secret-backed dotfiles require an unlocked 1Password app before
they can be rendered, explain the required action at that point. Reuse the
guided task's prerequisite logic where appropriate; do not create competing
1Password instructions or silently disable the selected integration.

Init does not mark manual postinstalls complete just because packages were
installed or notes were displayed. On an existing machine, native inspection
recognizes completed automatic setup, while manual confirmations remain
specific to that machine. Retries preserve completed work and report partial
installation honestly. `init --plan` remains read-only.

The engine-first maintenance design must also be checked against the bootstrap
and direct-init entry points. Bootstrap must obtain a compatible engine before
loading newer definitions. Decide during review whether direct init should
stop or offer an engine update when outdated; that exact new behavior has not
been agreed, and should not be inferred from the sync rules.

### Proposed ordinary sync

For a normal mutating `nimbus sync`, use this order:

1. Validate invocation and obtain sudo credentials early. Run Nimbus as the
   normal user; elevate only native operations that need it. Authentication
   does not replace approval of the changes in the plan.
2. Refresh metadata for the configured, trusted Nimbus package repository.
   Check for a newer installable engine using RPM version semantics.
3. If a newer engine is available, stop before fetching either repository.
   Explain the installed and available versions and direct the user to
   `nimbus sync --upgrade`. An unsuccessful check must not be presented as
   "up to date" or permit fetching potentially incompatible definitions.
4. Inspect both Git repositories and fetch their approved tracking branches.
   Retain clean-worktree, origin and fast-forward checks. Update Nimbus and
   Chezmoi once, then load the new definitions.
5. Inspect the machine, show meaningful system changes and obtain approval.
   Reconcile sources, constraints, selected packages, system files and other
   managed resources in dependency order. Preserve native verification,
   ownership rules, required snapshots and failure reporting.
6. Apply Chezmoi once after the applicable approval, including declared tool
   installation and extension hooks. Preserve machine/profile selection and
   the explicit 1Password SSH choice.
7. Verify results and print one closing report with new setup notes and
   remaining postinstall, logout or reboot requirements.

Plain sync does not run Topgrade or generally upgrade existing software.
Installing required packages may still change dependencies. Chezmoi's native
Mise install hook remains responsible for satisfying its declared tools.

If no managed system changes are required, skip their mutation and snapshot
work. Do not claim that sudo was unnecessary overall: the agreed engine
preflight requests it at the start of normal sync.

### Proposed combined upgrade

For a normal mutating `nimbus sync --upgrade`, use this order:

1. Validate invocation and obtain sudo credentials early.
2. Refresh trusted Nimbus repository metadata and check the installed engine.
3. If needed, preview and approve the engine-only RPM transaction, execute it
   through DNF, and verify the installed result. Disclose any dependencies
   required by that transaction. Do not fetch either Git repository yet.
4. Restart into the updated executable when replacement occurred. Preserve
   machine, checkout and supported arguments. Prevent restart loops and fail
   clearly if the expected executable/version cannot be started.
5. Run the configuration sync workflow once: update repositories, reconcile
   system state, and apply Chezmoi and its hooks.
6. Run Topgrade once using the resulting Chezmoi-owned configuration.
   Its system callback remains `nimbus upgrade --system`, followed by the
   selected native user-tool and application updaters.
7. Perform final inspection and present one combined result. Do not fetch
   repositories or apply Chezmoi again as part of final verification.

The engine check must be possible without first resolving definitions that
the old engine cannot understand. Use the existing trusted installation and
selector information; do not depend on freshly downloaded manifests to find
the engine update. "Available" means available through the configured package
repository, not merely a tag or release on GitHub. Preserve signatures,
package-source selection and version constraints; do not enable testing or
switch to another provider automatically.

The early engine transaction is separate from Topgrade's general RPM upgrade.
Topgrade may inspect Nimbus again as an ordinary installed RPM; this does not
justify an additional sync or a second special engine-update phase.

Retain prerequisite ordering: a failed engine upgrade or configuration sync
stops before later phases. Independent Topgrade steps may retain their normal
retry behavior, but any unresolved failure remains visible in the combined
result and exit status. Report partial completion; do not promise rollback.

### Update reliability

Force fresh metadata for explicit system upgrades as well as engine checks.
The plan and privileged transaction must use consistent current metadata;
refreshing only an unprivileged cache does not solve the observed issue.
Handle refresh failure before claiming currency or authorizing a misleading
transaction. Keep offline previews separate from this execution behavior.

Normal maintenance must not require the recurring workaround
`sudo dnf upgrade --refresh nimbus`. The combined command owns that fresh
engine check and update. A newly published GitHub release is not an available
RPM until the configured trusted package repository provides it. Never change
repositories or weaken signature checks to make a newer engine appear.

### Help, approvals and read-only commands

Help, status, validation and plan-only commands must stay read-only. They must
not request sudo, refresh metadata, fetch repositories, launch applications,
or create completion-state files. Plan output must identify cached or unknown
update information instead of promising the latest remote transaction.

Keep approvals attached to concrete changes. Early sudo authentication is
not blanket consent to perform every operation. Consolidate repeated wording
and duplicate questions while retaining review of engine, system and user
configuration changes. Specify `--yes` consistently during implementation;
it must not silently confirm manual GUI work or bypass verification.

### Setup notes

Add `nimbus setup-notes` to show all guidance relevant to the selected
machine. Notes are instructions and reminders, not completion checks.

- Init shows the applicable onboarding notes together, without scattering
  them through tool-install output.
- Later syncs show only new or meaningfully revised applicable notes, once
  at the end. Routine upgrades stop replaying all initial setup advice.
- Give each note a stable ID, revision and applicability rules. Increment
  the revision when required user action changes; wording corrections alone
  should not repeatedly alert existing users.
- Track displayed revisions in
  `$XDG_STATE_HOME/nimbus/setup-notes.json`, defaulting to
  `~/.local/state/nimbus/setup-notes.json`.
- Record a note as shown only after it was successfully displayed. An
  interrupted or failed display must not silently consume new guidance.
- Explicitly viewing all notes is read-only and does not confirm tasks or
  change the record used for automatic reminders.
- On an existing machine with no record, show applicable unseen notes on the
  next suitable mutating run. Do not infer prior display from package age.

Owner clarification: setup notes are exclusively a Nimbus feature. Nimbus owns
the complete catalog, command, applicability and display tracking. Read the
catalog directly from its selected checkout; do not add a Chezmoi handoff or
standalone reader. Chezmoi retains ordinary configuration documentation.

Remove the repeated setup-note printing from the Mise after-apply hook once
the new delivery path exists. Keep the actual Mise installation behavior.

### Postinstall command layout

Both `nimbus postinstall` and `nimbus postinstall --help` show the same help
and list of task subcommands. They no longer inspect and print all tasks.
Help stays consistent across machines; task help explains applicability.

| Proposed invocation | Behavior |
| --- | --- |
| `nimbus postinstall` | Help and available subcommands |
| `nimbus postinstall --help` | The same help |
| `nimbus postinstall status` | Compact status of applicable tasks |
| `nimbus postinstall onepassword --help` | Purpose, prerequisites and options |
| `nimbus postinstall onepassword` | Guided setup and verified actions |
| `nimbus postinstall onepassword --plan` | Inspect and preview only |
| `nimbus postinstall onepassword --mark-done` | Acknowledge manual steps only |

Convert existing task IDs into proper subcommands without dropping their
native setup behavior. Keep JSON inspection available and define its output
contract alongside the command changes. Detailed recovery guidance belongs
in task documentation or relevant failure output, not every task-list row.
Logout and reboot requirements become notices in status and final reports,
rather than pretend executable setup tasks.

### Guided task execution and completion

For each task, distinguish manual prerequisites, automated actions and
verification. Inspect what can be checked natively. If a requirement cannot
be inspected, show specific instructions and request explicit confirmation
before proceeding. A missing requirement blocks dependent actions.

Show the proposed native action, obtain approval, execute it with visible
progress, then verify its actual result. A successful guided flow records
completion automatically; the user does not run `--mark-done` afterward.
Starting an application or receiving exit status zero is insufficient when
the task requires further setup or a native verification check.

`--mark-done` acknowledges the relevant manual portion of a task. It does not
run file changes implicitly, bypass automated prerequisites or mark failed
verification successful. A mixed task can therefore remain pending after
manual acknowledgment. Provide a way to reset acknowledgments; exact reset
syntax remains a review detail.

Use clear task statuses such as Pending, Verified, Confirmed by you,
Blocked, and Unable to check. Keep the reason concise and actionable.
For mixed tasks, preserve which steps were manually confirmed and which
were verified; a single "Done" row can summarize both without conflating
their evidence. Failure leaves remaining work incomplete and supports retry.

### 1Password walkthrough

The proposed task must do more than open the app and remember a checkbox:

1. Explain sign-in/unlock and enabling desktop CLI integration. If SSH
   integration is selected, also explain enabling the SSH agent.
2. Tell the user to skip onboarding instructions that manually edit SSH or
   Git files: Chezmoi owns those files in this setup. Enabling GUI features
   is still required. Preserve the existing keys and explicit SSH opt-in.
3. Ask when the user is ready to check prerequisites and continue. Confirm
   available CLI access and other supported checks without exposing secrets.
4. Preview the selected managed configuration and ask to apply it through
   Chezmoi. Coordinate the existing SSH configuration, public-key selectors,
   agent selection and Git signing settings. Do not write competing copies
   directly from Nimbus or apply unrelated user configuration unnecessarily.
5. Verify applied configuration and accessible integration behavior. Keep
   remote authentication or signing registration checks distinct from local
   configuration, and disclose any check that contacts an external service.
6. Record successful completion automatically, retaining the distinction
   between confirmed GUI steps and verified configuration. If a required
   check fails, explain what remains instead of marking the whole task done.

Illustrative wording before the manual steps:

~~~text
Complete the listed settings in 1Password.
Skip manual SSH/Git file edits; Nimbus will apply them through Chezmoi.

Ready to check prerequisites and continue? [Y/n]
~~~

### Local state files and ownership

Implement the local state layer as a dedicated step before wiring note and
task workflows into it. Keep setup-note display history separate from task
completion. Honor `$XDG_STATE_HOME`, defaulting to `~/.local/state`.

| Proposed file below the state directory | Stores |
| --- | --- |
| `nimbus/setup-notes.json` | Note IDs/revisions successfully displayed |
| `nimbus/postinstall.json` | Task/step confirmations and completion evidence |

The exact schema and filenames remain proposed implementation details; the
separation, per-machine behavior and verification semantics are required.

Records are local to the user and machine. Do not copy them through Chezmoi
or Git. Include a schema version, selected machine identity, stable task/step
IDs, relevant revisions, timestamps and the source of confirmation. Use
atomic writes and appropriate private permissions. Retain no credentials,
key material, account payloads or raw verification output in these records.

Native state remains authoritative for checkable tasks. Stored verification
is last-observed evidence, not a substitute for inspecting current state.
An unreadable check must not be hidden by an old "done" flag. Confirmations
for unrelated tasks survive a task revision; invalidate only the affected
requirements. Missing state starts manual tasks unconfirmed, while native
inspection can immediately recognize already-completed automatic tasks.
Unreadable or invalid state is an explicit problem, not silent data loss.

Required state transitions:

- Displaying a note changes only its shown revision, never task completion.
- Confirming manual prerequisites records only those confirmed steps.
- A guided task records overall completion automatically only after all
  required confirmations, actions and verification succeed.
- `--mark-done` cannot bypass automated setup or failed verification.
- Failure or cancellation preserves earlier valid step evidence and leaves
  the unfinished work pending. Do not mark the entire task done on exit alone.
- Resetting an acknowledgment clears that confirmation without uninstalling
  software, reversing configuration, or changing another machine's record.
- Status/help/plan commands inspect without writing state. Record refreshed
  observations only through appropriate mutating workflows.

On introduction, an existing desktop and laptop each start without manual
acknowledgments. Recognize existing automatic setup through native checks;
let the user confirm completed manual work without unnecessarily repeating
it. Confirming 1Password on one machine must not complete it on the other.

This adds Nimbus-owned runtime state below the user's home directory. Update
the ownership language in SPEC and applicable repository instructions when
implementing it; Chezmoi must continue to leave that runtime state unmanaged.
Keep existing privileged resource receipts under `/var/lib/nimbus` separate.

### Output and progress

Make normal output concise while keeping real work observable:

- Show the active phase before work starts. Condense unchanged checks.
- Stream native download, installation and interactive progress immediately.
- Preserve terminal behavior when installation logging is enabled. Investigate
  direct terminal attachment, supported native options or a terminal relay
  as needed; do not assume another buffered writer fixes terminal detection.
- Show activity and elapsed time for quiet operations. Do not invent progress
  percentages or label installation as complete before verification.
- Keep prompts, errors and changed-resource previews visible. Avoid duplicate
  summaries from nested Nimbus callbacks; retain useful native tool output.
- Preserve Ctrl-C, child-process exit status, terminal restoration and
  secret-safe logging. Test interactive and redirected output separately.
- Use the dedicated final-reporting behavior below instead of repeated
  intermediate summaries. Failed phases must not look complete.

### Final reporting

Implement final reporting as its own work item across init, sync and combined
upgrade. Aggregate phase results and print one closing Nimbus report, including
when a run fails or is interrupted. Do not hide a native error while trying
to simplify output, and preserve unsuccessful exit status.

The report must clearly answer:

1. What changed: engine version when replaced, updated repositories, relevant
   system/user configuration changes and verified software update results.
2. What failed: the failing phase, concise reason and the useful retry or
   diagnostic action. Identify subsequent phases that were not run.
3. What remains: applicable pending or blocked postinstall tasks, including a
   short reason and the proposed command that handles each one.
4. What needs session activation: logout/reboot notices with a reason, such
   as the updated greeter configuration. These are notices, not setup tasks.
5. What is overridden or unverified: known effective-configuration conflicts
   and visual/hardware checks that have not yet been performed.
6. Which setup notes are new: present unseen applicable revisions once and
   point to `nimbus setup-notes` for the complete guidance.

Use native task inspection plus local manual confirmations for the checklist.
Do not print "everything complete" merely because files match or native
commands returned zero. An unavailable check should have a specific reason.
An unchanged phase can occupy one short line. Omit empty detail sections.

Inspect known effective-configuration conflicts in that final reporting.
For example, identify the Noctalia GUI overrides that defeat managed
lockscreen settings. Explain a targeted repair; do not delete the whole
settings file or silently discard unrelated preferences. Distinguish a
verified file match from unverified visual behavior after the next login.

Illustrative successful execution with remaining setup:

~~~text
Maintenance completed; setup still needs attention.

Nimbus              Updated to <version>
Configuration       Applied
Software updates    Completed

Remaining setup:
  account-picture   Not registered
    nimbus postinstall account-picture

Configuration notice:
  Noctalia GUI settings override the managed lockscreen widgets.

Reboot required: activate the updated greeter configuration.
View setup guidance: nimbus setup-notes
~~~

The example is a proposed display, not a claim about the current machine.
The actual report must reflect observed results and avoid printing private
rendered configuration or credentials.

### Work across repositories

This is a coordinated Nimbus and Chezmoi change:

- Nimbus owns init/sync phase ordering, engine checks and restart, guided
  postinstall commands, the note catalog and presentation, local-state tracking,
  effective
  checks, command progress and combined reporting.
- Chezmoi removes repeated note printing from after-apply hooks and retains
  native tool/extension installation,
  and owns the actual 1Password SSH/Git configuration. Adjust its Topgrade
  configuration only where the accepted workflow requires it.
- Both repositories must agree on guided configuration application. Preserve
  standalone Chezmoi behavior and test a fresh install
  as well as maintenance on a machine with existing files and overrides.
- COPR packages and releases the new engine after validation and separate
  publication authorization. No new package provider is proposed here.

### Scope and implementation sequence

Local implementation and disposable validation are now authorized. Do not
apply to the workstation, edit the laptop, run live privileged setup, commit,
push or publish without separate authorization. Passwordless greeter sync
remains deferred until the compatible Fedora Noctalia update. This work does
not reopen the other boot/VM tasks.

Implementation sequence retained from the approved proposal:

1. Update the command and state contracts in SPEC, including read-only paths,
   approval boundaries, ownership and machine selection.
2. Implement fresh metadata and engine-first update/restart behavior, then
   remove the repeated full sync. Keep Topgrade configuration in Chezmoi.
3. Fix shared terminal progress and install logging, with a slow subprocess
   regression test before broader output suppression.
4. Implement the local state files as an independent deliverable: schemas,
   per-machine scope, atomic persistence, revisions, reset and failure rules.
   Test independence and native-state precedence before connecting workflows.
5. Add the postinstall command layout and guided execution;
   implement the complete 1Password workflow against that shared behavior.
6. Add the Nimbus note catalog, delivery and tracking to init and maintenance,
   then remove repetitive Chezmoi hook text.
7. Implement final reporting as a separate deliverable: aggregate results,
   pending postinstalls, configuration conflicts and session/reboot notices.
   Cover successful, unchanged, partially completed and failed runs.
8. Update shipped help and README usage, validate in disposable Fedora, and
   record remaining physical-machine checks. Publish only when authorized.

### Acceptance checklist

Covered by isolated workflow fixtures, real terminal subprocesses, and the
signed Fedora RPM container drill. These checks do not certify an actual desktop
login, live vault authorization, or physical-device behavior. See the remaining
operator trials in [TASKS](TASKS.md#maintenance-and-setup-candidate-2026-09-13).

- [x] Fresh init shows live installation progress, applies user configuration,
  presents the applicable notes and lists remaining setup in one closing
  report. Logging does not remove progress or hide prerequisite prompts.
- [x] Init retries preserve completed work; notes never mark tasks complete.
  Selected 1Password prerequisites are handled before dependent rendering.
- [x] Nimbus note delivery works without a Chezmoi reader or repeated hook text
  or breaking standalone dotfile installation.
- [x] Plain sync stops before either Git fetch when a newer engine exists.
- [x] Combined upgrade installs and verifies the engine before fetching
  definitions, restarts safely and preserves the selected machine/arguments.
- [x] Stale caches cannot hide an available engine/system update; unavailable
  metadata produces a clear failure rather than a false up-to-date result.
- [x] A successful combined upgrade fetches each repository once, applies
  Chezmoi once and launches Topgrade once, with no final mutating sync.
- [x] Failures preserve completed-work reporting and stop dependent phases.
- [x] No-op sync, dependency installs, constraints, source reconciliation and
  Snapper behavior remain correct; approval is not replaced by sudo login.
- [x] Help, status and previews stay read-only and usable without sudo.
- [x] New notes appear once per relevant revision; explicit note viewing does
  not mark tasks done. Existing machines handle missing state predictably.
- [x] Postinstall help lists tasks and each task has its own help. Status is
  compact, applies machine selection and explains blocked/unknown checks.
- [x] Guided success records completion; failure, cancellation or an ineffective
  native action does not. Manual acknowledgment cannot override failed checks.
- [x] Desktop/laptop task state remains independent. Corrupt state, concurrent
  writes, relevant revisions, reset and retry have defined tested behavior.
- [x] Note display and task completion have separate records. Existing machines
  start manual steps unconfirmed; native checks recognize completed automatic
  setup. Reset does not undo configuration or affect another machine.
- [x] Stored completion cannot hide missing native configuration or an
  unavailable verification check. Read-only commands do not create state.
- [x] 1Password GUI confirmation leads to reviewed Chezmoi configuration and
  verification, without manual duplicate file edits or exposing secrets.
- [x] Slow commands display output before exiting, with logging enabled.
  Terminal prompts, resizing where relevant, cancellation and logs work.
- [x] Final reporting distinguishes installed files, effective overrides and
  changes still requiring logout/reboot or manual setup.
- [x] One combined closing report identifies changes, failures, skipped phases,
  pending postinstalls and session notices without duplicate Nimbus summaries.
  Failed runs retain unsuccessful exit status and useful next actions.
- [x] Nimbus `just check`, affected Chezmoi checks and disposable installation
  trials pass. No laptop repair is claimed without later authorized testing.

## 1. Simplify the documentation

Keep SPEC, TASKS and ROADMAP as the product documents. INSTALLATION is the
Fedora operator guide, recovered from history and corrected. SPEC includes
installation, security, root-system-file ownership and a short architecture
and test policy.
Remove stale research narratives and completed task logs. Keep deferred work
explicit without making it a release requirement.

Done when commands, ownership, recovery scope and implementation gaps agree
across the docs, links work, and the local checks pass.

## 2. Align commands and ownership

Sync first authenticates and checks fresh engine metadata. It stops for a
newer engine, or updates clean repositories and syncs configuration once. The
combined upgrade replaces and restarts Nimbus first when needed, syncs once,
then runs Topgrade. This supersedes the second full sync from 0.4.2. Keep
previews local and read-only, and make dirty/divergent errors actionable.
Keep Topgrade configuration in Chezmoi and prevent duplicate system updates or
recursion. Its callback remains `nimbus upgrade --system` for RPMs and system
Flatpaks. This repository-update workflow is released in Nimbus 0.4.0;
see TASKS for COPR delivery and installation evidence.
The machine shell field requires engine 0.4.1 or newer; it chooses the default
login shell while common installs both Bash and Zsh and Chezmoi retains both
configurations.
Deploy the new engine before applying the matching dotfiles configuration.
Nimbus 0.3.1 asks for a machine on first use through the minimal
`curl ... | bash` installer and supports the earlier empty snapshot mount.
Its existing-layout VM installation and UWSM login passed. The remaining
refactors and dashboard can follow; the desktop trial still follows the later
gates below.

Add missing managed-state previews and explicit approval independent of JSON.
Keep direct Chezmoi commands available alongside sync's approved apply stage,
retaining initial setup and profile handoff.
Remove unused Cargo and installer follow-up declarations; Chezmoi and Mise
own those tools. Keep the required Mise binary bootstrap and prerequisites.

The development profile also bootstraps Zeron through its official installer
and selects its browser dependencies. Installer effect disclosures cover its
generated user service and lingering changes before approval. Engine 0.4.3 and
its Fedora 44 COPR RPM now support those definitions. Chezmoi owns the asset links
and native Topgrade updater; Zeron owns application and service state.

Keep only helper RPM declarations and small post-install calls in Nimbus.
COPR helpers own downloads, verification, installation, status and removal;
Topgrade calls their explicit updates. No custom application provider is needed.
Tailscale operator setup uses the same explicit post-install approval flow,
with native preference verification and a documented revocation command.
Validate this new action in disposable Fedora before claiming host coverage.
Account-picture registration uses AccountsService after the Chezmoi image is
available. The task requires engine 0.4.6 or newer; delivery and native
validation evidence are tracked in TASKS.
The matching greeter appearance is prepared in the Hyprland session component;
activation of its read-only systemd mount still needs a sync/reboot trial.
WoWUp requires a standalone install/update interface in COPR before integration
can finish. Uninstalling a helper alone does not remove its application.

Bootstrap logging and terminal supervision were reviewed and retained: they
must work before the engine is installed and preserve cancellation. Bootstrap
refreshes user metadata so init can present its cached installation preview.

Keep current state compatibility and native ownership protections. Remove
obsolete tests with removed behavior; consolidate repeated fixtures without
losing preview, failure, retry or removal coverage. Apply Ponytail and Modern
Go Guidelines for the project's Go version to each bounded code change.
Keep package names tied to their jobs and split large files by responsibility.
The README maps the code; repository-tool tests live outside the CLI package.

Native execution and read-only inspection now have separate owners. The
remaining extraction follows boot work, under the dashboard step below.

Done when CLI workflows and fake-native integration tests match SPEC, existing
state is handled safely, and both affected repositories pass their local gates.

## 3. Finish workstation integration

### GRUB first

Activate the dark GRUB theme through a small, reversible native integration.
Test Fedora-only and Windows-present menus, the five-second timeout, Fedora
default, older-kernel selection, and removal of the theme. Preserve BLS entries
and the EFI stub. Use a disposable VM snapshot as an independent test safeguard.

### FDE auto-unlock second

Add an optional post-install action for the existing LUKS2 installation.
First inspect Fedora's boot path, encryption, TPM and Secure Boot support;
choose and document the native enrollment method and boot-change policy before
implementation. Do not assume a UKI migration is required. Broad UKI generation
and boot-key management remain deferred.

The action must preview the target and changes, request approval, preserve a
working passphrase, and report unsupported setups without weakening security.
Provide status and instructions to remove the enrollment without losing disk
access. Sync and upgrades must not enroll a machine automatically.

Done when enrollment, unattended unlock, passphrase fallback and removal pass
on real hardware, including fallback after a boot change that invalidates the
chosen policy. VM checks can prepare this work but do not close the hardware
gate. This scoped boot test precedes the dashboard; the full workstation trial
still follows it. Until then, auto-unlock remains planned, not working.

### Remaining integration

Add an explicit greeter passwordless-sync post-install action after Fedora's
stable Noctalia package supports constrained sync. Keep the Fedora package
source; Chezmoi already enables native auto-sync. Nimbus should preview and
delegate one-time authorization to the greeter's native CLI for the invoking
local account, verify the result, and document native status and removal.
Require a compatible shell, helper and packaged Polkit action; never authorize
legacy sync. Complete this work when wallpaper changes sync without prompts
and the next login shows the updated wallpaper on both desktop and laptop.
See TASKS for package availability and the remaining validation.

The Noctalia plugin post-install repair first appeared in 0.4.4. The laptop trial
confirmed installation but exposed premature verification failure during the
background update. Version 0.4.5 adds retries; validate missing-plugin repair
from a fresh desktop session through the supported command. Native Noctalia
owns downloads; Chezmoi owns selection.
See README for usage and TASKS for validation evidence.

The Hyprland plugin post-install task requires engine 0.4.6 or newer.
Chezmoi selects ScrollOverview; Nimbus supplies matching development packages
and coordinates native HyprPM setup after approval. Validate a fresh laptop
installation through the task before closing its hardware gate. See README
for usage and TASKS for evidence.

NVIDIA selections include a system-wide mask for the X11 settings-loader
autostart. Verify its next-login behavior during the NVIDIA hardware trial.

Complete COPR application selection after verifying the actual published
helper interfaces and package sources. Retain the Hyprland and Noctalia version
families. Use native dependency solving rather than manually coordinating
library versions.

Recovery session installation and the old layout inspection are removed
locally. Test TTY repair with broken user configuration and retirement
of existing owned session files on installed Fedora. Keep ordinary services
and Fedora defaults. For a needed root-owned setting, compare Fedora/upstream
with Omarchy and CachyOS-Settings using the research rules in SPEC. Record the
source and reason for an accepted change; do not import user configuration.
Finish post-install task behavior and enable the waiting Chezmoi launchers only
when the installed Nimbus version supports them.

The `proton-cachyos` post-install action delegates native Steam runner setup to
ProtonPlus. Validate its download and retry on disposable Fedora; unit tests
cover selection, prerequisites and the absence of user-data or network reads.

Snapper remains selected by `hyprland-noctalia`. Native setup, bounded number
retention and sync/system-upgrade hooks are implemented locally. Test initial
setup with the documented subvolume layout and without pre-created snapshot
storage, failure handling, cleanup and a root restore on installed Fedora.
Use engine 0.3.1 or newer for the guide's pre-mounted empty `/.snapshots`.
The existing-mount setup and native config listing passed on the test VM;
fresh-layout setup, snapshot pairs, retention and restoration remain open.
Keep separate boot/EFI coverage explicit; do not claim automatic rollback.

Done when the affected installation, upgrade, removal and retry paths pass in
a disposable Fedora VM. GRUB requires an actual boot and restoration of its
previous configuration. FDE auto-unlock needs the scoped hardware test above;
other physical-device behavior remains the later hardware trial.

## 4. Build the dashboard

After GRUB and FDE auto-unlock, extract reconciliation from Cobra, then setup,
selection and file capture.
Preserve the lock held across a selection write and sync. Keep presentation in
CLI and reuse the same operations for the dashboard. Replace description-based
execution decisions with explicit data covered by the approved plan.

Bare `nimbus` opens the TUI in a terminal and otherwise prints help. Provide
status, sync, upgrade, software selection, post-install and diagnostics. Reuse
the CLI's operations and approvals; do not build another state model.

Use the existing Bubble Tea stack. Test keyboard navigation, small/resized
terminals, cancellation, native command handoff and CLI/TUI preview parity.
Done when these workflows work through either interface with the same effects.

## 5. Prepare the desktop trial

Run the local gate and a clean disposable-VM installation from the release
candidate. Check repeat sync, upgrades, failed-operation retry, owned removal,
TTY repair and normal login. Test the new integration, not every application's
entire feature set.

Commit, publication and COPR builds require their own authorization. Record
the exact release and packaging results before calling the candidate ready.
Run the full desktop trial after the dashboard and delivery are ready. The
scoped FDE auto-unlock hardware test happens earlier, as described above.

## Deferred beyond the desktop milestone

| Work | Reason to keep it separate |
| --- | --- |
| Boot archives and automatic whole-system restore | Beyond native Snapper |
| UKI generation and broader boot-key management | Separate boot design |
| Hibernation and disk-backed swap | Needs hardware and storage validation |
| Windows VM setup and lifecycle | Native Windows covers the immediate need |
| Home Assistant integration | Owner wants to understand it first |
| Additional desktop profiles | Add only for a maintained, demonstrated need |
| Performance changes | Measure an actual problem before adding machinery |
| Browser app launching through UWSM | Check session lifecycle separately |

Revisit [UWSM app launching](https://github.com/Vladimir-csp/uwsm) for browser
and webapp commands: terminal independence, session environment, logout cleanup
and behavior outside UWSM. This is separate from choosing the UWSM login session.

If Windows VM work is reopened, retain the owner's preferences: official
Microsoft media, capacity shown before setup, editable defaults of Windows 11
Pro with 4 vCPUs, 8 GiB RAM and 128 GiB disk, and separate data deletion.
Revalidate the backend then; the previous container/media research is not a
current implementation requirement.

## Hardware trial

The installed laptop and desktop are needed to check GPU/power/suspend,
audio/Bluetooth, fingerprint and NVIDIA MOK enrollment, portal file picking
and screen sharing, UWSM session cleanup, and appearance/keyring behavior.

Test the network Epson ET-5800 with native printing and scanning before adding
vendor drivers. Choose Voxtype models and CPU/GPU backend after trials on both
the laptop and RTX 3080 desktop and the owner's Omarchy comparison. Recheck
Fastmail's known email-link limitation on Fedora. These are explicit remaining
checks, not reasons to block independent local work.

## Not planned

Nimbus is not becoming a multi-distribution framework, fleet manager, general
AppImage manager, backup service or custom Fedora installer image. Package
discovery results are input for owner review, never automatically desired
state. Fedora major-release upgrades stay with native Fedora tools.

## Local agents in Copilot

Repeat postinstall now selects a concise refresh-only workflow when local setup
and managed configuration match. Provider skips and actual errors are distinct.

The postinstall and post-Topgrade model refresh are implemented locally; see the
[operator commands](../README.md#local-agents-in-copilot). Keep the upstream
source/compatibility pin reviewed when updating Nimbus. Automatic maintenance
refreshes catalogs, not the proxy application release. New-engine setup can
update that native release explicitly. Follow up when Antigravity gains an
external tool bridge or Copilot exposes a stable public registration API.
