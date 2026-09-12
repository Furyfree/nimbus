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

Sync updates clean Nimbus and Chezmoi repositories, reconciles the system,
then offers Chezmoi apply. Keep previews local and read-only, and make dirty
or divergent repository errors actionable. From 0.4.2, `sync --upgrade`
reconciles repositories, system setup and user configuration before Topgrade,
then starts the updated engine for a final sync. This prepares new sources
before the system-upgrade callback requires them.
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
