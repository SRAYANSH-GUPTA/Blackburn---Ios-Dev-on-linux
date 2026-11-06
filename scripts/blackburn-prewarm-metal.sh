#!/usr/bin/env bash
# Run on Linux after the macOS guest becomes reachable over SSH.
# Work around compressed Metal-plugin page-in failures without changing the SSV.
set -euo pipefail
ssh -o BatchMode=yes -o ConnectTimeout=10 "${BLACKBURN_SSH_ALIAS:-${MACBOX_SSH_HOST:-macbox}}" /bin/bash -s <<'GUEST'
set -euo pipefail
plugin=/System/Library/Extensions/AppleParavirtGPUMetalIOGPUFamily.bundle/Contents/MacOS/AppleParavirtGPUMetalIOGPUFamily
# A complete sequential read preceded successful simctl calls during recovery.
# This is a per-boot cache workaround, not an on-disk filesystem repair.
/usr/bin/shasum -a 256 "$plugin"
# Driver is patched by reims-vgpu so codesign check is diagnostic only
/usr/bin/codesign --verify --verbose=2 "$plugin" 2>&1 || true
/usr/bin/xcrun simctl list devices >/dev/null
GUEST

