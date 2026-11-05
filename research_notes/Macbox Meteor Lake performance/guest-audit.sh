#!/bin/bash
# Read-only; run in macOS guest: ssh macbox 'bash -s' < guest-audit.sh
set -u
[ "$(uname -s)" = Darwin ] || { echo 'Run in the macOS guest.' >&2; exit 1; }
printf '\nOS and memory pressure\n'
sw_vers
memory_pressure | tail -5
printf '\nCPU and resident memory (KiB)\n'
ps -axo pid,pcpu,rss,comm -r | head -25
printf '\nBase settings\n'
mdutil -as
pmset -g
defaults read /Library/Preferences/.GlobalPreferences METAL_ERROR_MODE 2>/dev/null || true
defaults read com.apple.universalaccess reduceTransparency 2>/dev/null || true
defaults read com.apple.universalaccess reduceMotion 2>/dev/null || true
printf '\nPersistent launchd overrides\n'
launchctl print-disabled "gui/$(id -u)"
launchctl print-disabled system
printf '\nCandidate plists: names are discovery hints, not a disable list\n'
for dir in /System/Library/LaunchAgents /System/Library/LaunchDaemons; do
  for plist in "$dir"/*.plist; do
    case "$plist" in
      *[Ss]iri*|*[Ss]afari*|*[Bb]iometr*|*[Dd]iagno*|*SubmitDiag*|*[Aa]ssistant*|*[Cc]loud*|*bird*|*[Ss]ync*)
        label=$(/usr/libexec/PlistBuddy -c 'Print :Label' "$plist" 2>/dev/null) || continue
        printf '%s\t%s\n' "$label" "$plist"
        ;;
    esac
  done
done
