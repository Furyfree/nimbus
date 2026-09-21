# Open work

## Installed-system checks

- [ ] Laptop and desktop: upgrade through `nimbus sync --plan` and
  `nimbus sync --upgrade`; verify engine restart and schema-1 selector
  migration with checkout, machine and origin preserved (#40, #72).
- [ ] Disposable VM: develop-to-stable channel switch, state compatibility
  and refusal of checkout/repository drift (#72).
- [ ] Fresh Fedora: install the released RPM, repeat sync, upgrade, retry
  failed operations and repair through a TTY with broken dotfiles (#40).
- [ ] Installation terminal: one sudo authentication, live DNF progress,
  Ctrl+C, resize and readable private logs; check the pre-reboot MOK notice.
- [ ] Managed resources: acceptance/removal, service/group restoration,
  failed activation, receipt retirement and legacy-state compatibility.
- [ ] Repository updates: dirty-tree refusal and upgraded-engine handoff.
- [ ] Shell selection: switching and repair after installing a compatible
  engine, followed by logout and login.

## Boot, login and recovery

- [ ] Desktop: boot Windows and an older kernel from Previous kernels;
  inspect the redrawn lock icon. Verify default boot after the countdown.
- [ ] Laptop: type the passphrase at the themed prompt and boot an older
  kernel; inspect the menu on the 2880x1800 panel (#74).
- [ ] GRUB: collect `videoinfo` on both machines before changing text size.
- [ ] Disposable VM: kernel add/remove and marker removal, mkconfig-only
  hook, submenu guard and Fedora flat-menu fallback after a failed rebuild.
- [ ] Auto-login: Hyprland starts, the default keyring needs no prompt,
  logout returns to Noctalia, and manual login preserves that keyring.
- [ ] NVIDIA MOK: native signing/enrollment and `nvidia-smi` on hardware;
  skipping enrollment still permits boot and offers enrollment again.
- [ ] Snapper: fresh storage, snapshot pairs, retention, cleanup and a root
  restore in a disposable VM. Check boot/EFI exclusions (#38).
- [ ] Investigate intermittent Plymouth script-plugin crashes at quit
  observed on the desktop; keep a reproducer before changing the theme.

## Postinstall trials

- [ ] Compare service activity and startup state with systemd, including
  stopped/removed services and a deselected agent proxy.
- [ ] Fingerprint: approve activation while fprintd is idle, verify or
  enroll, then confirm passive status leaves the daemon idle (#75).
- [ ] Laptop: approved proxy uninstall and reboot preserve Copilot, its
  credentials, Chezmoi proxy configuration and Mise Herdr; units are gone.
- [ ] Agent proxy: registration and refresh on laptop; Antigravity discovery
  after native sign-in.
- [ ] Zeron: fresh install, update, opt-in and opt-out in a disposable VM;
  verify service and linger effects, then an owner desktop trial.
- [ ] Hostname: browsers reopen the same profile after the change/reboot.
- [ ] Voxtype: managed model download and CPU/GPU backend on both machines;
  compare with the owner's Omarchy setup and check input permissions.
- [ ] DTU eduroam: campus Wi-Fi, DTU-only services, password renewal and
  SELinux-enforcing operation. Review the CA bundle before 2027-12-02.
- [ ] Tailscale: native browser sign-in on laptop and operator setup in a VM.
- [ ] Greeter appearance: wallpaper sync after authorization and reboot on
  both machines, including the promoted systemd mount.
- [ ] Lockscreen: visual check in the laptop session.
- [ ] 1Password: desktop guided setup, SSH/Git authorization and sign-in
  recovery; review terminal colors.
- [ ] ScrollOverview: fresh download/build and activation on laptop.
- [ ] Proton-CachyOS: initial ProtonPlus download and retry on Fedora.
- [ ] Installed helpers: ble.sh, LibrePods and Copilot; RPM source correction.
- [ ] NVIDIA settings-loader mask: next-login behavior on NVIDIA hardware.
- [ ] Owner trial of interactive maintenance commands.

## Hardware and session checks

- [ ] GPU, suspend/resume, power, audio and Bluetooth on both machines.
- [ ] Portal file picking and screen sharing; UWSM logout, relogin and
  cleanup, including regular/private browser and webapp launches.
- [ ] Appearance, keyring and application integration; Fastmail email links
  and notifications.
- [ ] Epson ET-5800: native printing, trays, duplex and feeder scanning.

## Deferred

- Terminal dashboard (#36): reuse existing operations and approval/locking
  rules. Extract from Cobra only what the dashboard needs; replace
  description-based execution decisions with approved structured data.
- Windows VM lifecycle (#33): official Microsoft media, capacity preview,
  editable Windows 11 Pro defaults (4 vCPUs, 8 GiB RAM, 128 GiB disk),
  separate data deletion. Reassess the backend when work starts.
- Hibernation and disk-backed swap; boot archives and whole-system restore.
- Home Assistant, after the owner has explored it.
- Additional profiles for a demonstrated need; performance work after
  measuring a problem.
