# Open questions

This file tracks decisions that affect implementation. Once accepted, move the
answer into `SPEC.md` or `ROADMAP.md` and remove the question here.

## Bootstrap repository transport and identity

The machine manifest records `repo = "git@github.com:Furyfree/dotfiles.git"`
(SSH), while the dotfiles README documents `chezmoi init
https://github.com/Furyfree/dotfiles.git` (HTTPS), and Nimbus requires an
exact match between the bootstrap input and a tracked manifest
(`SPEC.md`, "Configuration and state"). On a fresh installation the private
repository must be fetched before 1Password, its SSH agent, and its SSH keys
exist, and the bootstrap installs only Nimbus, Chezmoi, and the Git transport.
Repository identity and transport should be separated or normalized before
Phase 4.

- A (recommended): manifests record one canonical identity, for example
  `github.com/Furyfree/dotfiles`; Nimbus normalizes any SSH or HTTPS URL to it
  before matching and chooses the transport per machine state. The initial
  fetch on a clean machine uses HTTPS with an interactively entered,
  short-lived credential that is never stored or logged; SSH becomes the daily
  transport once 1Password and its agent are ready.
- B: require SSH everywhere. The bootstrap then needs a manual key-provisioning
  step before the fetch and cannot rely on 1Password.
- C: make the dotfiles repository public and keep the fetch anonymous over
  HTTPS.

Affects the `SPEC.md` bootstrap paragraph, `CLI.md` init and dotfiles
bootstrap, and the dotfiles `README.md` and `machines/*.toml`.

## Desktop-session update group

The accepted update policy keeps Hyprland within its reviewed `0.56.x` line,
pins Noctalia Shell and Noctalia Greeter to exact reviewed versions while they
are unstable, and applies the session stack as one reviewed group transaction
(`SPEC.md`, "Profiles and packages"). Before catalog entries are implemented,
a separate analysis must define the group's exact membership, package sources,
version constraints, and verification checks. Until then, catalog files must
not encode the `desktop-session` group.

## Cross-repo state

The 2026-08-31 review's handoff commits (created 2026-09-01) live in two
repositories:

- dotfiles, committed directly to `main` as
  `2c14046 feat: adopt Nimbus profile handoff and machine manifests`: new
  `home/.chezmoi.toml.tmpl` (`promptMultichoiceOnce` under `Profiles`, global
  1Password prompt, `windows` platform profile), rewritten `PROFILES.md`, and
  new `machines/` manifests (`README.md`, `desktop.toml`, `laptop.toml`).
- nimbus (this pull request): the foundation contracts, the resolved
  2026-08-31 decisions, and `history/GLM-5.3-Flash.md` moved out of the
  foundation set.

Chezmoi analysis for the dotfiles change (chezmoi 2.72.0, all read-only):

- `chezmoi managed` lists 0 files; `chezmoi status`, `chezmoi diff`, and
  `chezmoi verify` are clean; `git diff --check` passes.
- `chezmoi init --config-path` in `/tmp` verified the template end to end: the
  Nimbus-supplied selection `--promptMultichoice
  'Profiles=common/development/hyprland-noctalia'` resolves without prompting,
  direct init with `--promptDefaults` selects the default (`common`), and an
  invalid profile fails with a source-aware error and exit code 1.
- Flag semantics verified against chezmoi 2.72.0 source: the
  `--promptMultichoice` pair key must match the template's prompt text exactly
  (`Profiles`), values are slash-separated, and `promptMultichoiceOnce`
  returns persisted `[data] Profiles` on later inits without prompting.

## Resolved alongside this change

Historical record only; durable requirements live in `SPEC.md` and
`ROADMAP.md`.

- Fixed the `windows` platform profile regression in the dotfiles template.
- Moved the 1Password SSH prompt out of the Linux-only branch so the choice is
  recorded on every platform.
- Nimbus `README.md` now says Chezmoi owns selected user configuration, not
  every file in the home directory.
- `GLM-5.3-Flash.md` moved to `history/`; it is analysis history, not active
  foundation.
