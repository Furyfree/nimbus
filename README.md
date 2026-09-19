# Nimbus

Nimbus is a personal, opinionated Fedora workstation installer and system
manager. It turns a supported Fedora base into the system its owner wants,
keeps that system inspectable, and makes every change through a plan it shows
first. The target is Fedora 44 on x86_64 with Hyprland and Noctalia.

Chezmoi owns user configuration. COPR owns packaging. Topgrade runs the
update workflow. Nimbus connects these tools where needed.

## Install

On Fedora 44 x86_64, run as your normal user:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash
~~~

For the develop channel:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/develop/install-develop.sh | bash
~~~

The first run asks for the machine name (`desktop`, `laptop` or `vm`), shows
the installation plan and asks before applying it. Reruns reuse the saved
machine. See [Installation](https://github.com/Furyfree/nimbus/wiki/Installation)
for the Fedora choices, and
[Commands](https://github.com/Furyfree/nimbus/wiki/Commands) to change
channels or inspect drift.

## Everyday commands

| Task | Command |
| --- | --- |
| See what needs attention | `nimbus status` |
| Preview system changes | `nimbus sync --plan` |
| Apply system changes | `nimbus sync` |
| Update Nimbus, sync once, then upgrade | `nimbus sync --upgrade` |
| List and run setup tasks | `nimbus postinstall` |
| Check remaining setup | `nimbus postinstall status` |
| Review setup guidance | `nimbus setup-notes` |
| Diagnose a problem | `nimbus doctor` |
| Show the engine channel | `nimbus channel` |

## Documentation

- [Wiki](https://github.com/Furyfree/nimbus/wiki): installation, commands,
  postinstall tasks, hardware and troubleshooting
- [SPEC.md](docs/SPEC.md): behavior, commands, ownership, security
- [ROADMAP.md](docs/ROADMAP.md): implementation order and deferred scope
- [TASKS.md](docs/TASKS.md): open work and decisions

[Paper Dark boot-theme sources and previews](tools/boot-theme/README.md) are
available for review.

## Development

Use the Go version in `go.mod` and run:

~~~sh
just check
just validate
~~~

`just check` runs formatting, vet, tests, asset checks, diff checks and the
available Markdown and shell linters. `just validate` checks the definitions.
Tests never modify the workstation; native system trials use a disposable VM
described in [tools/vm/README.md](tools/vm/README.md). Releases and COPR
publication are manual actions described in
[tools/release/README.md](tools/release/README.md).
