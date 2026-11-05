#!/usr/bin/env bash
# Run on the Linux host. Remote log and backups remain on the macOS guest.
set -euo pipefail
ssh -F "$HOME/.ssh/config" macbox /bin/bash -s <<'GUEST'
set -euo pipefail
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
work="$HOME/macbox-xcode-repair-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$work"
exec > >(tee "$work/repair.log") 2>&1
echo "Repair log and backups: $work"
test "$(uname -m)" = x86_64
sudo -n true
packages=/Applications/Xcode.app/Contents/Resources/Packages
ls -lh "$packages"
file "$DEVELOPER_DIR/usr/bin/xcodebuild"
shopt -s nullglob
pkgs=("$packages/"*.pkg)
test "${#pkgs[@]}" -gt 0

# Expand before installing: never replace a framework with another ARM-only copy.
for pkg in "${pkgs[@]}"; do
  name="$(basename "$pkg" .pkg)"
  pkgutil --payload-files "$pkg" > "$work/$name.payload.txt"
  pkgutil --expand-full "$pkg" "$work/$name"
done
for name in DVTDownloads CoreDevice; do
  found=0
  while IFS= read -r binary; do
    file "$binary"
    if /usr/bin/lipo -archs "$binary" | /usr/bin/grep -Eq '(^| )x86_64(h)?( |$)'; then
      found=1
    fi
  done < <(find "$work" -type f -path "*/$name.framework/Versions/*/$name")
  if [ "$found" != 1 ]; then
    echo "STOP: no Intel $name payload found. See $work; nothing installed."
    exit 1
  fi
done

# Preserve all installed private frameworks before running Apple's installers.
if [ -d /Library/Developer/PrivateFrameworks ]; then
  sudo ditto /Library/Developer/PrivateFrameworks "$work/PrivateFrameworks.before"
fi
for pkg in "${pkgs[@]}"; do
  sudo installer -pkg "$pkg" -target /
done

# If newer receipts caused older payloads to be skipped, restore just the
# affected frameworks from the verified Xcode packages. Keep the backup above.
for name in DVTDownloads CoreDevice ROCKit; do
  dest="/Library/Developer/PrivateFrameworks/$name.framework"
  if /usr/bin/lipo -archs "$dest/Versions/A/$name" 2>/dev/null | /usr/bin/grep -Eq '(^| )x86_64(h)?( |$)'; then
    continue
  fi
  source_framework=''
  while IFS= read -r binary; do
    if /usr/bin/lipo -archs "$binary" | /usr/bin/grep -Eq '(^| )x86_64(h)?( |$)'; then
      source_framework="${binary%%.framework/*}.framework"
      break
    fi
  done < <(find "$work" -type f -path "*/$name.framework/Versions/*/$name" ! -path "$work/PrivateFrameworks.before/*")
  test -n "$source_framework" || { echo "No Intel payload for $name"; exit 1; }
  if [ -e "$dest" ]; then
    sudo mv "$dest" "$work/$name.framework.after-installer"
  fi
  sudo ditto "$source_framework" "$dest"
  sudo chown -R root:wheel "$dest"
done
sudo xcode-select --switch "$DEVELOPER_DIR"
sudo xcodebuild -runFirstLaunch
for name in DVTDownloads CoreDevice ROCKit; do
  file "/Library/Developer/PrivateFrameworks/$name.framework/Versions/A/$name"
done
xcodebuild -version
xcrun simctl list

test -x "$HOME/.macbox/macbox-guest"
# The helper explicitly requests -architectureVariant universal.
"$HOME/.macbox/macbox-guest" runtime
test -x "$HOME/go/bin/simslim" || "$HOME/.macbox/macbox-guest" tools
test -x "$HOME/go/bin/simslim"
"$HOME/.macbox/macbox-guest" pool create
xcrun simctl list devices available -j > "$work/devices.json"
udid="$(python3 - "$work/devices.json" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    devices = json.load(f)['devices']
matches = [d for group in devices.values() for d in group
           if d.get('isAvailable') and '(macbox slim 1)' in d['name']]
if len(matches) != 1:
    raise SystemExit('Expected one macbox slim 1 simulator; inspect pool list')
print(matches[0]['udid'])
PY
)"
state="$(xcrun simctl list devices available | grep "$udid")"
case "$state" in *Booted*) ;; *) xcrun simctl boot "$udid" ;; esac
xcrun simctl bootstatus "$udid" -b
xcrun simctl io "$udid" screenshot "$work/simulator.png"
"$HOME/.macbox/macbox-guest" pool list
echo "Verified booted simulator UDID: $udid"
echo "Screenshot and full log: $work"
GUEST
