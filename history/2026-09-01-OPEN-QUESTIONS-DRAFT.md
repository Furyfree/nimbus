# Open questions

This file tracks decisions that affect implementation. Once accepted, move the
answer into `SPEC.md` or `ROADMAP.md` and remove the question here.

## Desktop-session update group

The accepted update policy applies the Hyprland, Noctalia, greeter, portal,
and session integration stack as one reviewed group transaction. Version
constraints live only in machine manifests through `package_constraints`
(`SPEC.md`); the catalog carries none. Before catalog entries are implemented,
a separate analysis must define the group's exact membership, package sources,
and verification checks. Until then, catalog files must not encode the
`desktop-session` group.

## Publication actions

The private-data audit is resolved: no redaction is needed. The local username
and the public GitHub account name are accepted content; anyone finding the
code already knows the account. Durable secret rules stay in `SPEC.md` and
`AGENTS.md`.

Two repository-owner actions remain, both user actions:

- Make both repositories public.
- Install the Codex GitHub App so `@codex review` works on pull requests;
  hosted review runs only when you trigger it.