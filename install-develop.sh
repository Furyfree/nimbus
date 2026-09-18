#!/usr/bin/env bash
# Nimbus develop-channel entry point.
#
# Fetched from the develop branch, so the URL selects the channel:
#   curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/develop/install-develop.sh | bash
# It re-fetches install.sh from the same branch with the channel exported.
# main's install.sh stays the stable entry point.
set -euo pipefail

if ! ( : <> /dev/tty ) 2>/dev/null; then
  printf '%s\n' 'installer: a controlling terminal is required; run this from an interactive shell' >&2
  exit 1
fi

export NIMBUS_CHANNEL=develop
printf '%s\n' 'Nimbus develop channel: fetching install.sh from the develop branch.'
curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/develop/install.sh | bash
