# Nimbus

Nimbus installs and maintains a personal Fedora 44 x86_64 workstation with
Hyprland and Noctalia. It previews system changes and asks before applying
them. Chezmoi handles user configuration; Topgrade coordinates updates.

## Install

Run as your normal user on Fedora:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash
~~~

For the develop channel:

~~~sh
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/develop/install-develop.sh | bash
~~~

Choose `desktop`, `laptop` or `vm` when asked. Reruns reuse your selection.
Follow the [installation guide](https://github.com/Furyfree/nimbus/wiki/Installation)
for disk layout, first boot and channel switching.

## Use

| Task | Command |
| --- | --- |
| See what needs attention | `nimbus status` |
| Preview changes | `nimbus sync --plan` |
| Apply changes | `nimbus sync` |
| Update Nimbus, sync, then upgrade software | `nimbus sync --upgrade` |
| Check remaining setup | `nimbus postinstall status` |
| Review setup guidance | `nimbus setup-notes` |
| Diagnose a problem | `nimbus doctor` |

See the [wiki](https://github.com/Furyfree/nimbus/wiki) for commands,
postinstall tasks, hardware and troubleshooting.
