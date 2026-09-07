#!/usr/bin/env bash
# Nimbus installer. Run with:
#
#   curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash
#
# This script stays small on purpose: it checks the platform and the user,
# obtains Git through DNF when it is missing, clones or validates the Nimbus
# checkout at ~/.local/share/nimbus, and hands over to that checkout's own
# bootstrap script with its input on the terminal. It never updates, resets,
# or replaces an existing checkout.
set -euo pipefail

ORIGIN="https://github.com/Furyfree/nimbus.git"
ORIGIN_ID="github.com/furyfree/nimbus"
CHECKOUT="${HOME}/.local/share/nimbus"
SUPPORTED_FEDORA="44"

say() { printf '%s\n' "$*"; }
fail() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

if ! ( : <> /dev/tty ) 2>/dev/null; then
  fail "a controlling terminal is required; run this from an interactive shell"
fi
[ "$(id -u)" -ne 0 ] || fail "run as the normal user, not root; Nimbus uses sudo per command"

# shellcheck disable=SC1091
. /etc/os-release 2>/dev/null || fail "/etc/os-release is missing; this is not Fedora"
[ "${ID:-}" = "fedora" ] || fail "unsupported operating system ${ID:-unknown}; Nimbus supports Fedora ${SUPPORTED_FEDORA}"
[ "${VERSION_ID:-}" = "${SUPPORTED_FEDORA}" ] || fail "unsupported Fedora ${VERSION_ID:-unknown}; Nimbus supports Fedora ${SUPPORTED_FEDORA}"
[ "$(uname -m)" = "x86_64" ] || fail "unsupported architecture $(uname -m); Nimbus supports x86_64"

say "Nimbus origin: ${ORIGIN}"
say "checkout:      ${CHECKOUT}"

if ! command -v git >/dev/null 2>&1; then
  say "git is missing; it is installed through DNF:"
  say "  sudo dnf5 -y install git"
  read -r -p "Install Git? [y/N] " answer </dev/tty || fail "Git installation declined"
  case "${answer}" in
    y|Y|yes|YES) ;;
    *) fail "Git installation declined" ;;
  esac
  sudo dnf5 -y install git
fi

# Normalize a Git locator to its repository identity the way Nimbus does.
normalize() {
  local url="$1"
  url="${url#*://}"
  url="${url#*@}"
  url="${url/:/\/}"
  url="${url%/}"
  url="${url%.git}"
  printf '%s' "$(printf '%s' "${url%%/*}" | tr '[:upper:]' '[:lower:]')/${url#*/}"
}

if [ -e "${CHECKOUT}" ]; then
  real="$(realpath -e "${CHECKOUT}")" || fail "${CHECKOUT} exists but cannot be resolved"
  top="$(git -C "${real}" rev-parse --show-toplevel 2>/dev/null)" || fail "${CHECKOUT} exists and is not a Git worktree"
  [ "$(realpath -e "${top}")" = "${real}" ] || fail "${CHECKOUT} is not the Git worktree root"
  remote="$(git -C "${real}" config --get remote.origin.url || true)"
  [ -n "${remote}" ] || fail "${CHECKOUT} has no origin remote"
  got="$(normalize "${remote}")"
  [ "$(printf '%s' "${got}" | tr '[:upper:]' '[:lower:]')" = "${ORIGIN_ID}" ] || fail "${CHECKOUT} points at ${remote}, not the Nimbus origin; it is left untouched"
  say "reusing the existing checkout at ${real}"
else
  say "cloning ${ORIGIN}"
  mkdir -p "$(dirname "${CHECKOUT}")"
  git clone "${ORIGIN}" "${CHECKOUT}"
fi

BOOTSTRAP="${CHECKOUT}/bootstrap"
[ -x "${BOOTSTRAP}" ] || fail "${BOOTSTRAP} is missing or not executable"
say "handing over to ${BOOTSTRAP}"
exec bash "${BOOTSTRAP}" </dev/tty
