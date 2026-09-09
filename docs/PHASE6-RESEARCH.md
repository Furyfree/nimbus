# Phase 6 package and defaults evidence

Research date: 2026-09-08. Package inspection used the disposable Fedora 44
package-search container. Its packages and presets are evidence of shipped
behavior, not proof of effective configuration on the installed VM. The VM's
SSH port refused the read-only connection; the reported boot failure remains
undiagnosed. Runtime acceptance belongs in TASKS.md.

## Greeter and recovery

Fedora `greetd-0.10.3-6.fc44` supplies account `greetd`, a unit whose
`ExecStart` is `greetd`, and the `display-manager.service` alias. Its default
configuration runs `agreety --cmd /bin/sh`. The package does not select
Noctalia. Terra `noctalia-greeter-1.3.1-1.fc44` supplies the greeter client,
compositor, and `/usr/bin/noctalia-greeter-session`, but has no activation
scriptlets and explicitly removes upstream's tmpfiles fallback. These were
checked in the downloaded RPM file lists, scriptlets, and extracted contents.
The [Terra spec] documents the same packaging decisions.

Nimbus therefore owns a separate `/etc/greetd/nimbus.toml` and a service
drop-in selecting it. It preserves Fedora's `/etc/greetd/config.toml` and PAM
files. The configuration uses Fedora's `greetd` account and the packaged
session wrapper, as required by [Noctalia's installation guide]. An owned
tmpfiles rule prepares `/var/lib/noctalia-greeter` with that account and mode
0750. The [upstream packaging guide] documents the state directory; Nimbus
does not run the upstream setup script, which also modifies PAM.

Greetd is enabled for the next boot, without starting or restarting it during
sync. Selecting `graphical.target` changes the next boot only. A foreign
display-manager alias must block activation. File changes still require a
planned daemon reload, and the closing report must tell the user to reboot.
The source-profile option is disabled so a broken user `.profile` cannot
prevent recovery login. The normal desktop continues to use Chezmoi's
Hyprland configuration; interactive shells load their own configuration.

Recovery uses the [Hyprland 0.55 Lua API], matching the existing dotfiles.
The desktop entry calls the [native start-hyprland launcher] with an explicit
system-owned Lua file. Noctalia's [session discovery] reads the selected
system session directory. Recovery reuses Ghostty instead of installing Foot.
It disables default terminal configuration, single-instance forwarding and
shell integration, then runs Bash with `--noprofile --norc`. Its XDG config
root points at the system recovery directory and GTK override variables are
unset, avoiding user GTK configuration. The [Ghostty option reference] owns
these command-line settings; full recovery login remains a VM gate.
Super+Return opens another terminal, Super+Shift+Q closes its window, and
Super+Shift+E exits. Native compositor startup, user-service interference,
keyboard behavior, and independence from broken dotfiles still need the VM
drill; syntax checks alone cannot establish recovery.

[Ghostty option reference]: https://ghostty.org/docs/config/reference

The generic file provider remains limited to `/etc`. The separate recovery
resource owns exactly the Lua configuration under `/usr/local/lib/nimbus`
and the desktop entry under `/usr/share/wayland-sessions`.
Removing those owned files leaves empty parent directories and the greeter's
mutable state directory intact; Nimbus does not claim ownership of or delete
greeter preferences and appearance data.

The [portal configuration contract] permits a system policy below
`/etc/xdg/xdg-desktop-portal`. The Hyprland-specific policy chooses Hyprland
with GTK fallback and GTK for file choosing. User configuration retains
precedence. No new user-unit enablement is declared for D-Bus-activated
portals; screen sharing and file picking require a live session test.

## Workstation-default decisions

- **Docker: use its `local` logging driver.** Docker's default `json-file`
  driver does not rotate logs by default. Its [logging documentation]
  recommends `local` for ordinary installations because rotation is built in.
  Keep the existing SELinux setting. Use Docker's own rotation defaults,
  without inventing numeric limits. A planned Docker restart activates the
  daemon default; only newly created containers inherit it. Verify with
  `docker info --format '{{.LoggingDriver}}'` and a new disposable container.
  Removal restores the recorded prior file; existing containers retain their
  own logging configuration and are never recreated implicitly.
- **ZRAM: retain Fedora policy.** The extracted
  `zram-generator-defaults-1.2.1-5.fc44` configuration uses
  `zram-size = min(ram, 8192)`. [Fedora's ZRAM design] explains the package
  ownership. No Nimbus size/compression override is justified. Verify package
  presence and `zramctl`/`swapon --show` on the target before claiming it is
  active; the container is not a RAM-policy test.
- **OOM handling: retain Fedora policy.** Fedora's presets enable
  `systemd-oomd.*`, and `systemd-oomd-defaults` supplies system and user slice
  drop-ins. Inspect target `oomctl` and effective unit/drop-in settings. Do
  not introduce a second OOM policy or change thresholds without workload
  evidence.
- **Memory maps: retain Fedora policy.** [Fedora raised vm.max_map_count]
  for game compatibility. Check the installed sysctl sources and effective
  value before considering any Nimbus override; do not copy an old Linux
  default or a tuning guide's number into desired state.
- **Other sysctls, inotify, and file-descriptor limits: retain defaults.** No
  failed workload or measured limit exhaustion has been supplied. Capture
  effective sysctl values and service/user limits on the target when a
  concrete application demonstrates a need. No broad limits.conf or sysctl
  bundle is introduced.
- **Journald: retain native bounded storage.** [systemd's journal policy]
  already limits storage and preserves free space. Inspect the target's
  merged configuration and `journalctl --disk-usage`; no owner retention or
  disk budget justifies a second numerical policy yet.
- **SSD trim: retain Fedora scheduling.** [Fedora enables fstrim.timer].
  Check its target state and `lsblk --discard`; do not force immediate trim
  or override device support based on VM behavior.
- **Laptop power/suspend: retain the selected component's native daemon.**
  `laptop-power` already selects `power-profiles-daemon`; Phase 6 declares its
  service. Do not add custom suspend, governor, or battery thresholds. A
  Fedora installation using `tuned-ppd` needs an explicit conflict decision
  before changing providers. Suspend/resume and battery behavior require the
  real laptop.
- **Hardware udev/storage rules and gaming tweaks: defer overrides.** There
  is no identified device defect or measured application need. Keep native
  driver rules and existing package selections. Add only a scoped setting
  whose matching-hardware verification and removal can be demonstrated.

Fedora's extracted service presets enable Bluetooth and Avahi and select
`cups.socket` plus `cups.path`, explicitly preferring socket activation over
enabling `cups.service`. Nimbus follows those native activation choices.
Bluetooth enablement is declared without requiring an active unit, because
the native unit condition can skip machines without a Bluetooth adapter.
Existing memberships and service enablement need preservation on component
removal; declarations are not permission to erase pre-existing state.
Fedora-only repository queries also confirmed Tailscale 1.94.2 in the release
repository and 1.98.8 in updates; its existing Fedora package source remains.

[Terra spec]: https://github.com/terrapkg/packages/blob/frawhide/anda/desktops/noctalia/greeter/noctalia-greeter.spec
[Noctalia's installation guide]: https://docs.noctalia.dev/greeter/installation/
[upstream packaging guide]: https://github.com/noctalia-dev/noctalia-greeter/blob/v1.3.1/PACKAGING.md
[Hyprland 0.55 Lua API]: https://github.com/hyprwm/Hyprland/blob/v0.55.0/example/hyprland.lua
[native start-hyprland launcher]: https://github.com/hyprwm/Hyprland/blob/v0.55.0/example/hyprland.desktop.in
[session discovery]: https://github.com/noctalia-dev/noctalia-greeter/blob/v1.3.1/src/greeter/greeter_sessions.cpp
[portal configuration contract]: https://flatpak.github.io/xdg-desktop-portal/docs/portals.conf.html
[logging documentation]: https://docs.docker.com/engine/logging/configure/
[Fedora's ZRAM design]: https://fedoraproject.org/wiki/Changes/SwapOnZRAM
[Fedora raised vm.max_map_count]: https://fedoraproject.org/wiki/Changes/IncreaseVmMaxMapCount
[systemd's journal policy]: https://www.freedesktop.org/software/systemd/man/latest/journald.conf.html
[Fedora enables fstrim.timer]: https://fedoraproject.org/wiki/Changes/EnableFSTrimTimer
