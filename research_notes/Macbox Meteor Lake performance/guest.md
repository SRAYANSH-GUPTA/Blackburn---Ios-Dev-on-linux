# macOS Tahoe guest tuning

## Which guest changes materially improve smoothness?

### Takeaway
Use supported Reduce Transparency / Reduce Motion, a modest desktop resolution, and avoid duplicate simulator rendering. No verified extreme WindowServer/CoreGraphics/Quartz defaults flag fixes the current paravirtual display's idle-animation delay; that is a guest-driver issue.

### Cited Findings
- macbox reports GPU limits, reduced desktop blur from `macbox-guest base --reduce-transparency`, lower desktop resolution/refresh, Simulator.app closed, and one animating simulator. These are Arrow Lake reference measurements, not Meteor Lake measurements. Local `macbox/docs/performance.md:43–58`. — [upstream performance](https://github.com/TUNA-NOPE/macbox/blob/main/docs/performance.md)
- Apple's supported settings: Accessibility → Display → Reduce Transparency; Accessibility → Motion → Reduce Motion (reduces interface/Liquid Glass morphing). The Apple settings pages explain appearance, not FPS uplift. — [Apple transparency](https://support.apple.com/guide/mac-help/mchl11ddd4b3/mac), [Apple motion](https://support.apple.com/en-au/guide/mac-help/mchlc03f57a1/mac)
- `METAL_ERROR_MODE=5` is already configured by `base_metal`, to avoid icon-service crash / broken desktop under Reims; it is compatibility, not demonstrated performance tuning. Local `macbox/guest/macbox-guest:98–105`. — [guest source](https://github.com/TUNA-NOPE/macbox/blob/main/guest/macbox-guest)
- The open idle-display bug shows ~100–150 ms animation onset delay. Upstream owner confirms a kext patch is needed. Host fake present-complete interrupts did not help; continuous animation reduced delay but consumes work, while CADisplayLink alone used ~3.5 vCPU cores without producing frames. Avoid a permanent animation/keepalive as a minimal-resource optimization. — [Reims issue 141](https://github.com/steelbrain/reims-vgpu/issues/141)

### Inferences
- Reduce Motion reduces the visible cost of transitions; it does not repair APVDisplayPort timing. Reduce Transparency is independent in macOS and iOS: a macOS preference does not change simulator glass effects.
- Best deeper engineering target: patch the guest APVDisplayPort VBL timer to arm while the display pipe is bound / WindowServer requests notifications. This is an upstream-proposed fix, not implemented or benchmarked here.

### Gaps
- No primary current source was found validating `Compositor`, `CompositorTiming`, `CGDisableBeamSync`, `QuartzGLEnabled`, `DisplayCycle`, or legacy beam-sync defaults as Tahoe/Reims optimizations. Do not include them as proven commands.
- No benchmark of Reduce Motion/Transparency specifically on this 155H was run. SSH read-only inspection failed: default SSH config permissions, then sandbox networking `socket: Operation not permitted` with explicit user config.

## What background work can be reduced and how?

### Takeaway
macbox already has a useful baseline. Disable features in settings first; selectively experiment with a confirmed launchd label only if sampling shows it consuming resources. Installed or resident processes alone do not establish CPU load or memory pressure.

### Cited Findings
- `macbox-guest base` sets no sleep/screensaver, Spotlight indexing off (`sudo mdutil -a -i off`), and automatic macOS/App Store update downloads off. It also configures passwordless sudo and screen-lock behavior, so use its whole base command only when those setup changes are desired. Local `macbox/guest/macbox-guest:85–165`; setup recommends skipping Apple Account and declining Location Services, analytics, Screen Time and Siri. — [base source](https://github.com/TUNA-NOPE/macbox/blob/main/guest/macbox-guest), [setup](https://github.com/TUNA-NOPE/macbox/blob/main/docs/setup.md)
- Apple documents Analytics & Improvements switches, Siri off, Spotlight search privacy exclusions, and Safari Search → Preload Top Hit in the background. Safari's Top Hit loading happens while typing; it is distinct from a possible Safari launchd prewarming agent. — [analytics](https://support.apple.com/guide/mac-help/mh27990/mac), [Siri](https://support.apple.com/guide/mac-help/siri-settings-on-mac-mchl3fd7fc15/mac), [Spotlight](https://support.apple.com/en-gb/102321), [Safari](https://support.apple.com/en-ae/guide/safari/ibrwe75c2a3c/mac)

### Inferences
- Candidates to inspect, not universally validated Tahoe labels: `com.apple.SafariLaunchAgent` (prewarming), `com.apple.assistantd` / `com.apple.Siri.agent` (Siri), `com.apple.SubmitDiagInfo` / `com.apple.diagnosticd` (diagnostics), `com.apple.biometrickitd` (biometrics), `com.apple.bird` / `com.apple.cloudd` (sync). Find actual plists and `Label` values on this guest; many may be absent, protected, idle, or required by a feature being tested.
- Prefer leaving diagnostics available during development; use Analytics sharing settings first. Avoid sync daemon removal if testing CloudKit/iCloud, and biometric removal if testing authentication. Keep `logd`, `notifyd`, `securityd`, `trustd`, `tccd`, `runningboardd`, WindowServer, and CoreSimulator services.
- `disable` is persistent; `bootout` removes a currently loaded definition. Use `man launchctl` on the exact Tahoe guest as authoritative syntax. Some Apple jobs are protected and commands may fail: record failures and stop that experiment rather than change SIP or the sealed system volume.

Concrete commands, in guest:

```sh
# High-value narrow changes; record current settings first.
mdutil -as
sudo mdutil -a -i off         # restore only previously enabled volumes with -i on
# Full provisioner, if its other setup changes are wanted:
macbox-guest base --reduce-transparency
# Or only appearance (SSH writes may be denied; then use System Settings):
defaults write com.apple.universalaccess reduceTransparency -bool true
# Reduce Motion: use Accessibility > Motion; verify behavior after relogin.

# Read-only service inspection from the supplied script:
# ssh macbox 'bash -s' < guest-audit.sh

# Example one-service experiment ONLY after plist and original state verified:
plist=/System/Library/LaunchAgents/com.apple.SafariLaunchAgent.plist
label=$(/usr/libexec/PlistBuddy -c 'Print :Label' "$plist") || exit 1
domain="gui/$(id -u)"
launchctl print-disabled "$domain" > /tmp/blackburn-before-disabled.txt
launchctl print "$domain/$label" > /tmp/blackburn-before-service.txt
# Proceed only if it was enabled, and the feature is unused:
launchctl disable "$domain/$label"
launchctl bootout "$domain/$label"
# Roll back to the originally enabled and loaded state:
launchctl enable "$domain/$label"
launchctl bootstrap "$domain" "$plist"
# System daemon equivalent: domain=system; sudo each launchctl command.
```

### Gaps
- Candidate labels, protected-job behavior, idle wakeups and resource savings remain unverified on the running guest. No permanent blanket disable script is justified by the available evidence. `guest-audit.sh` only reads settings/processes/plist labels and passes `bash -n`.
- Apple online support may now default to macOS 27; use the version selector for Tahoe. Several full Apple page fetches were rate limited; search returned the cited documented controls. Modern launchctl man-page mirrors were found, but are not treated as an Apple current-version primary source.
