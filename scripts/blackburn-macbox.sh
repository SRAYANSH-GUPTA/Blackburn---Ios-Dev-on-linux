#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
source "$ROOT/macbox/.env.blackburn"
export MACBOX_CONFIG="${MACBOX_CONFIG:-${XDG_CONFIG_HOME:-$HOME/.config}/macbox/blackburn.env}"
export MACBOX_WORKSPACE="${MACBOX_WORKSPACE:-$ROOT}"
export XCROSS_SIMULATOR_ARCH="${XCROSS_SIMULATOR_ARCH:-x86_64}"
export XCROSS_SIMULATOR_HOST="${XCROSS_SIMULATOR_HOST:-$MACBOX_SSH_HOST}"
MACBOX="$ROOT/macbox/macbox"
fail() { printf '%s\n' "$*" >&2; exit 1; }
check_setup_space() {
  local storage free_kb
  storage=${MACBOX_HOME:-}
  if [[ -z "$storage" ]]; then
    storage=$(
      if [[ -f "$MACBOX_CONFIG" ]]; then source "$MACBOX_CONFIG"; fi
      printf '%s' "${MACBOX_HOME:-$HOME/.local/share/macbox}"
    )
  fi
  # Check the filesystem that will actually hold the VM.
  while [[ ! -d "$storage" ]]; do storage=$(dirname -- "$storage"); done
  free_kb=$(df -Pk "$storage" | awk 'NR==2 {print $4}')
  if ((free_kb < BLACKBURN_MIN_FREE_GIB*1024*1024)); then
    printf 'MISSING: at least %s GiB free for macbox setup; %s KiB available on %s\n' "$BLACKBURN_MIN_FREE_GIB" "$free_kb" "$storage" >&2
    return 1
  fi
}
export PATH="$ROOT/bin:$PATH"
xcross() { "$ROOT/bin/xcross" "$@"; }
usage() {
  cat <<'EOF'
Usage: scripts/blackburn-macbox.sh COMMAND [arguments]
  doctor                       Read-only prerequisite checks
  setup-macbox                 Build/create/start VM; resume guest setup via macbox next
  extract-sdk [Xcode.xip|Xcode.app]  Install local SDK, or copy Xcode.app from guest
  build-sim [Flutter build options]  Run from Flutter project directory
  run-sim [Flutter run options]      Boot mac-sim pool device and attach hot reload
  mount-workspace              Mount container /workspace in guest over SMB
Environment: XCROSS_SIMULATOR_ARCH, XCROSS_SIMULATOR_HOST, MACBOX_WORKSPACE,
  BLACKBURN_SHARED=1 (use mounted guest workspace), MACBOX_CONFIG.
EOF
}
cmd=${1:-help}; if (($#)); then shift; fi
case "$cmd" in
  doctor)
    bad=0
    check() { if "$@"; then printf 'OK: %s\n' "$*"; else printf 'MISSING: %s\n' "$*"; bad=1; fi; }
    check test -r /dev/kvm; check test -w /dev/kvm
    check test -r "$MACBOX_RENDER_NODE"; check test -w "$MACBOX_RENDER_NODE"
    for tool in docker dart flutter swift clang ld64.lld rsync ssh; do check command -v "$tool"; done
    check docker info
    if command -v vulkaninfo >/dev/null; then check vulkaninfo --summary; else printf 'MISSING: vulkaninfo\n'; bad=1; fi
    free_kb=$(df -Pk "$ROOT" | awk 'NR==2 {print $4}')
    check check_setup_space
    check xcross --help
    # CLI startup alone does not validate the installed SDK or Swift identity.
    check xcross flutter doctor
    printf 'Guest RAM: %s; GPU: %s; free disk: %s KiB\n' "$MACBOX_MEMORY" "$MACBOX_RENDER_NODE" "$free_kb"
    exit "$bad" ;;
  setup-macbox)
    # This command changes only macbox's user files/containers, never host packages/groups/sysctl.
    # Refuse before building images or fetching recovery media on a full disk.
    check_setup_space || fail 'Free disk space before running setup-macbox.'
    "$MACBOX" init
    "$MACBOX" doctor
    "$MACBOX" build
    vm_dir="${MACBOX_HOME:-$HOME/.local/share/macbox}/home/.local/share/reims-ctl/vms/${MACBOX_VM:-mac}"
    if [[ ! -f "$vm_dir/vm.toml" ]]; then "$MACBOX" create; fi
    "$MACBOX" up
    "$MACBOX" next ;;
  extract-sdk)
    if (($#)); then
      xcross sdk install "$1"
    else
      donor=$(mktemp -d "${TMPDIR:-/tmp}/blackburn-xcode.XXXXXX")
      trap 'rm -rf -- "$donor"' EXIT
      printf 'Copying installed Xcode from %s (requires substantial disk space)\n' "$XCROSS_SIMULATOR_HOST"
      rsync -a --protect-args -e 'ssh -o BatchMode=yes' -- "$XCROSS_SIMULATOR_HOST:/Applications/Xcode.app" "$donor/"
      xcross sdk install "$donor/Xcode.app"
    fi ;;
  mount-workspace)
    # Native macOS SMB client; no guest third-party filesystem driver required.
    ssh -o BatchMode=yes -- "$XCROSS_SIMULATOR_HOST" \
      'mkdir -p /Users/Shared/BlackburnWorkspace && /sbin/mount_smbfs -N //guest@10.0.2.2/workspace /Users/Shared/BlackburnWorkspace'
    printf 'Mounted. Set BLACKBURN_SHARED=1 for runs whose project is inside MACBOX_WORKSPACE.\n' ;;
  build-sim) xcross flutter build --target-platform simulator --debug "$@" ;;
  run-sim)
    device=()
    if [[ "$XCROSS_SIMULATOR_HOST" == "$MACBOX_SSH_HOST" ]]; then
      export MAC_SIM_HOST="$MACBOX_SSH_HOST"
      "$ROOT/macbox/sims/bin/mac-sim" up
      device=(--udid "$("$ROOT/macbox/sims/bin/mac-sim" udid)")
    fi
    if [[ "${BLACKBURN_SHARED:-0}" == 1 ]]; then
      export XCROSS_SIMULATOR_SHARED_ROOT="$MACBOX_WORKSPACE"
      export XCROSS_SIMULATOR_GUEST_ROOT=/Users/Shared/BlackburnWorkspace
    fi
    xcross flutter run --target-platform simulator "${device[@]}" "$@" ;;
  help|-h|--help) usage ;;
  *) usage >&2; exit 2 ;;
esac
