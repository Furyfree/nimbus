#!/usr/bin/env bash
# curl -fsSL https://raw.githubusercontent.com/Furyfree/nimbus/main/install.sh | bash -s -- --machine vm
# The entry point also supplies the checked-out bootstrap shell helpers.
set -euo pipefail
installer_log_ready=false
work=""

# These functions are also sourced by the checked-out bootstrap.
installer_event() {
  [ "${installer_log_ready:-false}" = true ] || return 0
  printf '%s %s\n' "$(date -u +%FT%TZ)" "$*" >> "${NIMBUS_INSTALL_LOG_DIR}/bootstrap.log"
}
say() { printf '%s\n' "$*"; installer_event "$*"; }
fail() { printf 'installer: %s\n' "$*" >&2; installer_event "error: $*"; exit 1; }

installer_arguments() {
  local machine='' new='' dotfiles='' no_dotfiles=false onepassword=false arg
  while [ "$#" -gt 0 ]; do
    arg="$1"; shift
    case "$arg" in
      --machine|--new|--dotfiles)
        if [ "$#" -eq 0 ] || [[ "$1" == -* ]] || [ -z "$1" ]; then
          fail "${arg} requires a value"
        fi
        case "$arg" in --machine) machine="$1";; --new) new="$1";; --dotfiles) dotfiles="$1";; esac
        shift ;;
      --machine=*) machine="${arg#*=}"; [ -n "$machine" ] || fail '--machine requires a value';;
      --new=*) new="${arg#*=}"; [ -n "$new" ] || fail '--new requires a value';;
      --dotfiles=*) dotfiles="${arg#*=}"; [ -n "$dotfiles" ] || fail '--dotfiles requires a value';;
      --no-dotfiles) no_dotfiles=true;;
      --onepassword-ssh) onepassword=true;;
      -y|--yes) ;;
      -h|--help)
        printf '%s\n' 'Usage: install.sh [--machine ID | --new ID] [--dotfiles URL | --no-dotfiles] [--onepassword-ssh] [-y]'
        printf '%s\n' 'First installation requires --machine ID or --new ID; reruns reuse the trusted selector.' 'Init shows and applies its plan without confirmation; --new opens a dialogue.' 'Sudo and Chezmoi may ask for input. --yes is accepted for compatibility.'
        exit 0;;
      *) fail "unsupported installer argument: ${arg}";;
    esac
  done
  [ -z "$machine" ] || [ -z "$new" ] || fail '--machine and --new exclude each other'
  [ -z "$dotfiles" ] || [ "$no_dotfiles" = false ] || fail '--dotfiles and --no-dotfiles exclude each other'
  [ "$onepassword" = false ] || [ "$no_dotfiles" = false ] || fail '--onepassword-ssh and --no-dotfiles exclude each other'
  if [ -n "$dotfiles" ] || [ "$no_dotfiles" = true ]; then
    [ -n "$new" ] || fail '--dotfiles and --no-dotfiles require --new'
  fi
}

# Refuse symlinks in the path before creating or opening private log files.
installer_private_dir() {
  local path="$1" part owner mode current=''
  local -a parts=()
  [[ "$path" == /* && "$path" != *'/../'* && "$path" != */.. && "$path" != *'/./'* && "$path" != */. ]] || fail 'installation log path must be absolute and normalized'
  IFS=/ read -r -a parts <<< "$path"
  for part in "${parts[@]}"; do
    [ -n "$part" ] || continue
    current="${current}/${part}"
    [ ! -L "$current" ] || fail "refusing symlink in log path: ${current}"
    if [ ! -e "$current" ]; then mkdir -m 700 -- "$current"; fi
    [ ! -L "$current" ] || fail "refusing symlink in log path: ${current}"
    [ -d "$current" ] || fail "not a log directory: ${current}"
    owner="$(stat -c %u "$current")"
    mode="$(stat -c %a "$current")"
    [ "$owner" = "$UID" ] || [ "$owner" = 0 ] || fail "foreign owner in log path: ${current}"
    if (( (8#$mode & 0022) != 0 )); then
      if [ "$owner" != 0 ] || (( (8#$mode & 01000) == 0 )); then fail "writable ancestor in log path: ${current}"; fi
    fi
  done
  if [ ! -O "$path" ] || [ "$(stat -c %a "$path")" != 700 ]; then
    fail "log directory must be owned by this user with mode 700: ${path}"
  fi
}

installer_prune() {
  local root="$1" dir file count=0 safe
  local -a runs=()
  # Names created by mktemp contain no newlines. Sort only immediate children.
  mapfile -d '' -t runs < <(find "$root" -mindepth 1 -maxdepth 1 -type d -name 'run-*' -print0 | sort -zr)
  for dir in "${runs[@]}"; do
    if [ -L "$dir" ] || [ ! -O "$dir" ] || [ "$(stat -c %a "$dir")" != 700 ]; then
      continue
    fi
    if [ ! -f "$dir/.nimbus-install" ] || [ -L "$dir/.nimbus-install" ] ||
      [ "$(cat "$dir/.nimbus-install")" != 1 ]; then
      continue
    fi
    if [ -e "$dir/.active" ] || [ -L "$dir/.active" ] ||
      [ ! -f "$dir/.finished" ]; then
      continue
    fi
    safe=true
    while IFS= read -r -d '' file; do
      case "${file##*/}" in .nimbus-install|.finished|bootstrap.log|engine.log|mise.log) ;; *) safe=false;; esac
      [ -f "$file" ] && [ ! -L "$file" ] && [ -O "$file" ] && [ "$(stat -c %a "$file")" = 600 ] && [ "$(stat -c %h "$file")" = 1 ] || safe=false
    done < <(find "$dir" -mindepth 1 -maxdepth 1 -print0)
    [ "$safe" = true ] || continue
    count=$((count + 1))
    [ "$count" -gt 20 ] || continue
    rm -- "$dir/.nimbus-install" "$dir/.finished"
    for file in "$dir/bootstrap.log" "$dir/engine.log" "$dir/mise.log"; do [ ! -e "$file" ] || rm -- "$file"; done
    rmdir -- "$dir"
  done
}

installer_session() {
  installer_log_owner=false
  installer_sudo_pid=''
  installer_child_pid=''
  installer_started=$SECONDS
  if [ -n "${NIMBUS_INSTALL_LOG_DIR:-}" ]; then
    installer_private_dir "$NIMBUS_INSTALL_LOG_DIR"
    if [ ! -f "$NIMBUS_INSTALL_LOG_DIR/.nimbus-install" ] ||
      [ -L "$NIMBUS_INSTALL_LOG_DIR/.nimbus-install" ] ||
      [ "$(cat "$NIMBUS_INSTALL_LOG_DIR/.nimbus-install")" != 1 ]; then
      fail 'invalid installation log marker'
    fi
    local marker
    for marker in .nimbus-install .active bootstrap.log; do
      if [ ! -f "$NIMBUS_INSTALL_LOG_DIR/$marker" ] ||
        [ -L "$NIMBUS_INSTALL_LOG_DIR/$marker" ] ||
        [ ! -O "$NIMBUS_INSTALL_LOG_DIR/$marker" ] ||
        [ "$(stat -c %a "$NIMBUS_INSTALL_LOG_DIR/$marker")" != 600 ] ||
        [ "$(stat -c %h "$NIMBUS_INSTALL_LOG_DIR/$marker")" != 1 ]; then
        fail 'unsafe inherited installation log'
      fi
    done
    if [ -e "$NIMBUS_INSTALL_LOG_DIR/.finished" ] || [ -L "$NIMBUS_INSTALL_LOG_DIR/.finished" ]; then
      fail 'installation log is already finished'
    fi
  else
    local root="${XDG_STATE_HOME:-${HOME}/.local/state}/nimbus/install"
    while [[ "$root" == *'//'* ]]; do root="${root//\/\//\/}"; done
    installer_private_dir "$root"
    NIMBUS_INSTALL_LOG_DIR="$(mktemp -d "${root}/run-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXXXX")"
    export NIMBUS_INSTALL_LOG_DIR
    (umask 077; printf '1\n' > "$NIMBUS_INSTALL_LOG_DIR/.nimbus-install"; printf '%s\n' "$$" > "$NIMBUS_INSTALL_LOG_DIR/.active"; : > "$NIMBUS_INSTALL_LOG_DIR/bootstrap.log")
    installer_log_owner=true
    installer_prune "$root"
  fi
  installer_log_ready=true
  trap installer_cleanup EXIT
  trap 'installer_interrupt INT 130' INT
  trap 'installer_interrupt TERM 143' TERM
  say "Installation logs: ${NIMBUS_INSTALL_LOG_DIR}"
}

installer_interrupt() {
  if [ -n "${installer_child_pid:-}" ]; then
    kill -"$1" "$installer_child_pid" 2>/dev/null || true
    wait "$installer_child_pid" 2>/dev/null || true
    installer_child_pid=''
  fi
  exit "$2"
}

installer_handoff() {
  local status=0
  [ -f "$CHECKOUT/tools/install/handoff.py" ] || fail 'checkout lacks the installation supervisor; update and review the checkout before retrying'
  (trap - INT TERM; exec /usr/bin/python3 -I -B "$CHECKOUT/tools/install/handoff.py" "$@") <&0 &
  installer_child_pid=$!
  wait "$installer_child_pid" || status=$?
  installer_child_pid=''
  return "$status"
}

installer_cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if [ -n "${installer_sudo_pid:-}" ]; then
    kill "$installer_sudo_pid" 2>/dev/null || true
    wait "$installer_sudo_pid" 2>/dev/null || true
  fi
  [ -z "${work:-}" ] || rm -rf -- "$work"
  installer_event "shell finished status=${status} elapsed_seconds=$((SECONDS - installer_started))" || { [ "$status" -ne 0 ] || status=1; }
  if [ "${installer_log_owner:-false}" = true ]; then
    if (umask 077; set -C; printf 'status=%s elapsed_seconds=%s\n' "$status" "$((SECONDS - installer_started))" > "$NIMBUS_INSTALL_LOG_DIR/.finished"); then
      rm -- "$NIMBUS_INSTALL_LOG_DIR/.active" || { [ "$status" -ne 0 ] || status=1; }
    else
      printf 'installer: cannot create installation completion marker; preserving active run\n' >&2
      [ "$status" -ne 0 ] || status=1
    fi
    installer_prune "${NIMBUS_INSTALL_LOG_DIR%/*}" || { [ "$status" -ne 0 ] || status=1; }
    printf 'Installation finished in %ss (status %s). Logs: %s\n' "$((SECONDS - installer_started))" "$status" "$NIMBUS_INSTALL_LOG_DIR"
  fi
  exit "$status"
}

installer_sudo() {
  installer_event 'sudo authentication start (input is not recorded)'
  if ! sudo -n -v 2>/dev/null; then sudo -v; fi
  installer_event 'sudo authentication succeeded'
  if [ "${NIMBUS_BOOTSTRAP_SUDO:-}" = 1 ]; then return; fi
  export NIMBUS_BOOTSTRAP_SUDO=1
  (
    sleeper=''
    trap '[ -z "$sleeper" ] || kill "$sleeper" 2>/dev/null; exit 0' TERM INT
    while :; do
      sleep 45 & sleeper=$!
      wait "$sleeper" || exit
      sudo -n -v || { installer_event 'sudo keepalive failed'; exit 1; }
    done
  ) &
  installer_sudo_pid=$!
}

# Only call this for fixed, non-secret bootstrap commands, never sudo auth or init.
installer_run() {
  local start=$SECONDS status command
  local -a results=()
  printf -v command '%q ' "$@"
  installer_event "command start: ${command}" || return 1
  if "$@" 2>&1 | tee -a "$NIMBUS_INSTALL_LOG_DIR/bootstrap.log"; then
    results=("${PIPESTATUS[@]}")
  else
    results=("${PIPESTATUS[@]}")
  fi
  status="${results[0]}"
  if [ "${results[1]}" -ne 0 ]; then
    printf 'installer: installation log write failed (tee status=%s)\n' "${results[1]}" >&2
    [ "$status" -ne 0 ] || status=1
  fi
  if ! installer_event "command end: status=${results[0]} log_status=${results[1]} elapsed_seconds=$((SECONDS - start)) ${command}"; then
    printf 'installer: cannot record command completion in installation log\n' >&2
    [ "$status" -ne 0 ] || status=1
  fi
  return "$status"
}

installer_main() {
ORIGIN="https://github.com/Furyfree/nimbus.git"
ORIGIN_ID="github.com/furyfree/nimbus"
CHECKOUT="${HOME}/.local/share/nimbus"
SUPPORTED_FEDORA="44"

installer_arguments "$@"

if ! ( : <> /dev/tty ) 2>/dev/null; then
  fail "a controlling terminal is required; run this from an interactive shell"
fi
[ "$(id -u)" -ne 0 ] || fail "run as the normal user, not root; Nimbus uses sudo per command"

# shellcheck disable=SC1091
. /etc/os-release 2>/dev/null || fail "/etc/os-release is missing; this is not Fedora"
[ "${ID:-}" = "fedora" ] || fail "unsupported operating system ${ID:-unknown}; Nimbus supports Fedora ${SUPPORTED_FEDORA}"
[ "${VERSION_ID:-}" = "${SUPPORTED_FEDORA}" ] || fail "unsupported Fedora ${VERSION_ID:-unknown}; Nimbus supports Fedora ${SUPPORTED_FEDORA}"
[ "$(uname -m)" = "x86_64" ] || fail "unsupported architecture $(uname -m); Nimbus supports x86_64"

installer_session
installer_sudo

say "Nimbus origin: ${ORIGIN}"
say "checkout:      ${CHECKOUT}"

prerequisites=()
command -v git >/dev/null 2>&1 || prerequisites+=(git-core)
[ -x /usr/bin/python3 ] || prerequisites+=(python3)
if [ "${#prerequisites[@]}" -gt 0 ]; then
  say "Installing missing bootstrap prerequisites: ${prerequisites[*]}"
  installer_run sudo dnf5 -y install "${prerequisites[@]}"
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
  [ "$(printf '%s' "${got}" | tr '[:upper:]' '[:lower:]')" = "${ORIGIN_ID}" ] || fail "${CHECKOUT} does not match the Nimbus origin; it is left untouched"
  say "reusing the existing checkout at ${real}"
else
  say "cloning ${ORIGIN}"
  mkdir -p "$(dirname "${CHECKOUT}")"
  installer_run git clone "${ORIGIN}" "${CHECKOUT}"
fi

BOOTSTRAP="${CHECKOUT}/bootstrap"
[ -x "${BOOTSTRAP}" ] || fail "${BOOTSTRAP} is missing or not executable"
say "handing over to ${BOOTSTRAP}"
installer_run git -C "$CHECKOUT" rev-parse HEAD
installer_handoff bash "${BOOTSTRAP}" "$@" </dev/tty
}

if [[ "${BASH_SOURCE[0]:-$0}" == "$0" ]]; then
  installer_main "$@"
fi
