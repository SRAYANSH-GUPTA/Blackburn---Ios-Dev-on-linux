#!/usr/bin/env bash
# Read-only Linux audit. Optional: --guest SSH_ALIAS adds the macOS audit.
set -u
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
if (($#)) && [[ $# != 2 || $1 != --guest ]]; then
  printf 'Usage: %s [--guest SSH_ALIAS]\n' "$0" >&2
  exit 2
fi
printf '\nCPU topology\n'
lscpu -e=CPU,CORE,MAXMHZ,ONLINE
printf '\nMemory and active swapping (first vmstat row is since boot)\n'
free -h
vmstat 1 3
printf '\nGPU identity\n'
for node in /sys/class/drm/renderD*/device; do
  [[ -e "$node" ]] || continue
  printf '%s\n' "$node"
  cat "$node/vendor" "$node/device"
  readlink -f "$node/driver"
done
printf '\nSelected macbox settings\n'
config=${MACBOX_CONFIG:-${XDG_CONFIG_HOME:-$HOME/.config}/macbox/blackburn.env}
if [[ -r "$config" ]]; then
  # Inspect rather than source the config; do not expose unrelated values.
  rg '^(export )?MACBOX_(CPUS|MEMORY|CPUSET|RESOLUTION|REFRESH|GPU_TUNING|RENDER_NODE)=' "$config" || true
fi
printf '\nRunning QEMU threads\n'
ps -eLo pid,tid,psr,pcpu,comm | awk 'NR == 1 || /qemu|CPU [0-9]+\/KVM/'
if (($#)); then
  ssh -o BatchMode=yes -o ConnectTimeout=10 -- "$2" 'bash -s' \
    < "$ROOT/research_notes/Macbox Meteor Lake performance/guest-audit.sh"
fi
