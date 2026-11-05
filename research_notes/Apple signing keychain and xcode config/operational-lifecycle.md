# Blackburn setup-once, on-demand GUI, daily-headless lifecycle

Research date: 2026-11-05. Read-only research: no VM, host services, signing material, or lifecycle implementation was changed. The commands below are an engineering runbook, not a record of execution. Source review included root `AGENTS.md`, `macbox/AGENTS.md`, and `.agents/skills/macbox-ios-development/SKILL.md`. The skill's Command Line Tools advice is incorrect for a normal iOS/Xcode toolchain: full Xcode must remain installed and selected. ([Apple command-line reference](https://developer.apple.com/documentation/xcode/xcode-command-line-tool-reference), [local selection checks](/home/srayansh/Documents/Golang/Blackburn/macbox/guest/macbox-guest:325))

## 1. What “headless” actually means and which lifecycle commands exist

### Takeaway

Xcode.app need not run as a GUI process for builds or signing, but its installed developer tools remain required. Removing the Linux viewer/compositor is different from stopping macOS WindowServer or logging out the user; keep the existing guest graphical session for the established CoreSimulator/Metal workflow. The current wrapper has only `local` and `stream` view modes; `private`, `headless`, and `stream off` in earlier Blackburn research are proposals, not shipped commands. ([Apple](https://developer.apple.com/documentation/xcode/xcode-command-line-tool-reference), [current parser](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:605), [previous proposal status](/home/srayansh/Documents/Golang/Blackburn/research_notes/Headless%20macbox%20and%20IDE%20integration/headless.md:99))

### Cited Findings

- `macbox view local` mounts the invoking user's Wayland socket and renders the Reims SDL window on the Linux desktop. `macbox view stream` uses a private GPU-backed Sway virtual output and starts wayvnc plus Selkies/noVNC. Both calls stop and restart the VM when its container is running—even if requesting the same mode. They are not live attach/detach commands. ([local mounting](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:54), [restart dispatcher](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:608), [private display startup](/home/srayansh/Documents/Golang/Blackburn/macbox/container/display-up.sh:13), [upstream architecture](https://github.com/TUNA-NOPE/macbox/blob/main/docs/architecture.md))
- **Closing the native local Reims window powers off the VM.** The installed SDL shell sends the close request to the host; QEMU's `apple_gfx_input_close()` sets `SHUTDOWN_ACTION_POWEROFF` and requests `SHUTDOWN_CAUSE_HOST_UI`. Thus “configure, close local window, continue over SSH” is unsafe/inaccurate for this checkout. Minimize it temporarily or use a planned shutdown/restart. ([SDL close handler](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-vgpu-window/src/sdl/shell.rs:380), [QEMU close callback](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:438))
- Closing a browser client disconnects its viewer; it does not call `macbox down`. The container starts Selkies detached under `setsid`, independently of the tab. This leaves private Sway/stream servers running and is not proof that all capture/encoding overhead disappears. ([stream launcher](/home/srayansh/Documents/Golang/Blackburn/macbox/container/stream-up.sh:39), [explicit VM shutdown](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:324))
- Existing `macbox up` always invokes `display-up.sh` unless the mounted host Wayland socket exists. `REIMS_VGPU_WINDOW=0` alone therefore removes the Reims window, but does not prevent Sway, wayvnc and Selkies from being launched by the wrapper. No supported `stream off` branch exists. ([cmd_up](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:298), [stream options](/home/srayansh/Documents/Golang/Blackburn/macbox/container/stream-up.sh:28))
- Reims/QEMU supports GPU execution without a Linux presentation window: for this graphics backend `reims-ctl` uses `-display none`; `REIMS_VGPU_WINDOW=0` suppresses Reims' otherwise independent window, while GPU/display objects are still created with a null presentation-window configuration. ([QEMU display decision](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/qemu.rs:424), [window gate](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:869), [device creation](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:1090), [upstream overrides](https://github.com/steelbrain/reims-vgpu#environment-overrides))
- Persist the flag with `reims-ctl set <vm> --env REIMS_VGPU_WINDOW=0`. A shell export around `macbox up` or `reims-ctl start` is insufficient: ctl deliberately removes inherited `REIMS_VGPU_WINDOW`, then applies the VM's explicit `extra_env`. The chosen window mode takes effect on next QEMU start, not when restarting an encoder. ([environment sanitization](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/qemu.rs:119), [configured overrides](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/qemu.rs:147), [GPU realization](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:1090))
- Local guest provisioning supports automatic login, disables sleep and can reduce transparency. Autologin is refused when FileVault is enabled; it is a convenience choice, not a prerequisite for command-line signing. ([provisioner](/home/srayansh/Documents/Golang/Blackburn/macbox/guest/macbox-guest:187), [guest documentation](https://github.com/TUNA-NOPE/macbox/blob/main/guest/README.md))

### Inferences

- Recommended default for occasional interactive troubleshooting: keep the stock stream-mode VM and close the browser tab when finished. This already frees the user from a permanent visible desktop and running Xcode GUI. For lower idle overhead, the earlier private-Sway/encoder-off proposal is a small lifecycle integration task, not a reason to build a project-settings GUI.
- True no-host-compositor operation is source-supported and feasible with a manual lower-level transition (below), but the wrapper should eventually implement it explicitly so its status/restore/start behavior is reliable.
- Do not kill WindowServer, securityd, trustd, CoreSimulator, SpringBoard or backboardd as a substitute for suppressing the Linux viewer. Keep the same guest user for SSH signing and simulator ownership. The supplied workflow does not establish that an unlogged-in guest can render the same apps.

### Gaps

- No runtime test here proves nonblank simulator rendering, hot reload, power savings, or frame rate in no-window mode. Do not label it benchmarked or claim a percentage saving.
- Apple's general simulator documents do not promise that every UI test or privacy-sensitive app works without a logged-in graphical session. Treat logout-free operation as the supported Blackburn operational baseline; validate any further daemon/session reduction separately.

## 2. Setup once and use the GUI only when useful

### Takeaway

Initial macOS account/SSH setup can use an on-demand desktop; Xcode installation, runtime installation, native builds and signing can subsequently use SSH. Apple ID sign-in to Xcode is optional if using API-key/manual signing; initial Keychain inspection is optional too. Full Xcode installation is distinct from keeping the Xcode app open. ([macbox setup](https://github.com/TUNA-NOPE/macbox/blob/main/docs/setup.md), [Apple tools](https://developer.apple.com/documentation/xcode/xcode-command-line-tool-reference), [Apple component installation](https://developer.apple.com/documentation/xcode/downloading-and-installing-additional-xcode-components))

### Cited Findings

- Apple identifies `simctl`, `xcodebuild`, and `devicectl` as tools that ship with Xcode rather than the standalone Command Line Tools package. Select full Xcode before invoking them. ([Apple reference](https://developer.apple.com/documentation/xcode/xcode-command-line-tool-reference))
- Apple documents selecting Xcode, running `xcodebuild -runFirstLaunch`, and downloading/importing runtimes from Terminal. The `universal` simulator-runtime architecture variant includes Intel support when available. ([Apple components](https://developer.apple.com/documentation/xcode/downloading-and-installing-additional-xcode-components))
- Apple explicitly says to use Xcode 26 or earlier when running Xcode on Intel; macbox targets Intel-compatible Xcode 26 and iOS 26 simulator runtimes. An arbitrary current App Store Xcode/download is therefore not an interchangeable upgrade. ([Apple Intel statement](https://developer.apple.com/documentation/apple-silicon/porting-your-macos-apps-to-apple-silicon), [Apple version table](https://developer.apple.com/xcode/system-requirements), [upstream ceiling](https://github.com/TUNA-NOPE/macbox#what-you-need))
- Local `macbox xcode <xip>` copies and expands the archive, selects `/Applications/Xcode.app/Contents/Developer`, accepts the license if necessary, runs first-launch installation and enables developer debugging access. Runtime provisioning requests `-architectureVariant universal`. No Xcode GUI launch occurs in these functions. ([Xcode installation](/home/srayansh/Documents/Golang/Blackburn/macbox/guest/macbox-guest:369), [runtime](/home/srayansh/Documents/Golang/Blackburn/macbox/guest/macbox-guest:396), [host wrapper](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:430))

### Inferences

Concrete staged procedure, with placeholders replaced by the existing installation's values:

1. From Linux, use the configured Blackburn macbox instance; check `macbox status`. Do not start a second/default instance accidentally. `scripts/blackburn-macbox.sh` selects `~/.config/macbox/blackburn.env`, whereas bare upstream `macbox` defaults to `~/.config/macbox/macbox.env`. Set `MACBOX_CONFIG` explicitly if necessary. ([wrapper config](/home/srayansh/Documents/Golang/Blackburn/scripts/blackburn-macbox.sh:4), [default config](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:7))
2. If GUI interaction is needed, save work because a running instance restarts on view selection. Run `macbox view stream; macbox up` and visit `http://127.0.0.1:6081/`, or `macbox view local; macbox up` from the user's Wayland session. The second `up` is idempotent if already running. Choose stream for an easily dismissible browser viewer. ([parser](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:608), [startup](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:298))
3. For an already provisioned VM, skip macOS Setup Assistant. Otherwise create the local macOS user and enable Remote Login using the GUI/bootstrap workflow; verify `ssh macbox true`. Apple Account sign-in to macOS/iCloud is not necessary for this stage. If choosing Xcode-account signing, open Xcode Settings > Accounts only now and complete the user's sign-in/2FA interactively. If choosing API-key/manual signing, skip Xcode sign-in. ([setup instructions](https://github.com/TUNA-NOPE/macbox/blob/main/AGENTS.md), [SSH bootstrap](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:402))
4. Install a compatible **universal Intel-capable Xcode 26** archive and matching universal simulator runtime. Verify the actual binaries rather than inferring architecture from filename alone. Run these checks/setup commands in the guest over an interactive SSH terminal (license acceptance follows the user's acceptance of Apple's terms):

```bash
ssh -t macbox
file /Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
sudo xcodebuild -license accept
sudo xcodebuild -runFirstLaunch
xcodebuild -version
xcodebuild -showsdks
xcrun --find simctl
xcrun simctl list runtimes
# If a compatible runtime is missing, with Xcode 26 selected:
xcodebuild -downloadPlatform iOS -architectureVariant universal
exit
```

The built-in `macbox xcode` and guest runtime provisioner already implement equivalent setup. Do not select `/Library/Developer/CommandLineTools` to fix a missing/ARM-only `xcodebuild`; install/select the correct full Xcode. ([local setup](/home/srayansh/Documents/Golang/Blackburn/macbox/guest/macbox-guest:381), [Apple tools](https://developer.apple.com/documentation/xcode/xcode-command-line-tool-reference), [Apple runtimes](https://developer.apple.com/documentation/xcode/downloading-and-installing-additional-xcode-components))

5. Configure signing through the separate CLI/keychain runbook. Optionally open Keychain Access once to inspect a certificate's private-key disclosure and access controls when diagnosing import/prompt problems; it is not required for ordinary CLI automation. Quit Xcode and Keychain Access normally after inspection, leaving the guest user logged in.
6. Close a **browser viewer tab** to return to command-line-only daily work. For a native viewer, do not press its close button: that shuts the VM down. Either minimize it, switch deliberately to stream mode (planned restart), or perform the no-window transition in the next section.

### Gaps

- These instructions deliberately do not enroll an Apple Developer account, accept new contracts, expose secrets, or create real signing identities. Those are account-specific setup tasks outside this read-only research.
- GUI sign-in is a convenience route and may require periodic reauthentication. “Setup once” does not promise certificates/profiles never expire or contracts never need acceptance.

## 3. Exact no-window transition, daily operations, and recovery

### Takeaway

For existing code, lower-level `reims-ctl` commands can persist no-window mode, and the already-running container can retain QEMU/network/storage while its private display services are stopped. The clean future interface is an explicit wrapper headless mode; do not imply that interface exists today. The procedure below is **source-derived, not VM-tested**. ([current startup](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:298), [persisted environment](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/qemu.rs:147))

### Cited Findings

- `macbox down` gracefully asks the guest to shut down where SSH/sudo permit, falls back to ctl stop/force, and removes the container. Thus it is appropriate for final shutdown but cannot retain a stripped-down container across starts. ([shutdown](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:324))
- Reims' presentation-window decision occurs when its GPU is realized. Setting `REIMS_VGPU_WINDOW=1` on a running no-window VM does not dynamically create a viewer. A QEMU restart is needed before returning to local/Sway viewing. ([realization](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:1090))
- The installed simulator bridge boots, waits for readiness, installs and launches using simctl; it does not require `open -a Simulator`. ([local launcher](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart:136))
- A simulator app is separate from a device-distribution app. Apple's older TN2415 explicitly distinguishes simulator builds from device signing; current tooling may apply ad hoc signatures, so do not generalize that note to “no simulator signatures ever.” A distribution certificate/provisioning profile is not needed for ordinary Intel simulator development. ([Apple TN2415](https://developer.apple.com/library/archive/technotes/tn2415/))

### Inferences

**Manual existing-source path without editing macbox:** use Bash on Linux; stop active builds/simulators first. Set `MACBOX_CONFIG` to the actual existing configuration before running. The function below calls ctl inside the current container with the same HOME used by macbox. The `start` invocation reuses persisted networking, graphics, tuning and disk settings.

```bash
# Linux Bash shell. This file is existing trusted local configuration.
export MACBOX_CONFIG="$HOME/.config/macbox/blackburn.env"
source "$MACBOX_CONFIG"
: "${MACBOX_HOME:=$HOME/.local/share/macbox}"
: "${MACBOX_CONTAINER:=macbox}" "${MACBOX_VM:=mac}"
: "${MACBOX_DOCKER:=docker}" "${MACBOX_SSH_HOST:=macbox}"
ctl_existing() {
  "$MACBOX_DOCKER" exec -e "HOME=$MACBOX_HOME/home" \
    "$MACBOX_CONTAINER" bash -c \
    '. "$HOME/.cargo/env"; exec reims-ctl "$@"' reims-ctl "$@"
}

# Keep container up; do NOT use macbox down for this transition.
macbox up
ctl_existing set "$MACBOX_VM" --env REIMS_VGPU_WINDOW=0 --env WAYLAND_DISPLAY=
ssh "$MACBOX_SSH_HOST" 'sudo -n shutdown -h now'
# SSH may exit 255 during shutdown. Wait, then check:
ctl_existing list
# Proceed ONLY once the VM is stopped; repeat status checks as needed.

# Now no VM is attached to Sway: stop the host-side viewers/capture servers.
"$MACBOX_DOCKER" exec "$MACBOX_CONTAINER" pkill -TERM -f '[s]elkies-venv/bin/selkies' || true
"$MACBOX_DOCKER" exec "$MACBOX_CONTAINER" pkill -TERM -f '/[w]ebsockify' || true
"$MACBOX_DOCKER" exec "$MACBOX_CONTAINER" pkill -TERM -x wayvnc || true
"$MACBOX_DOCKER" exec "$MACBOX_CONTAINER" pkill -TERM -x sway || true
ctl_existing start "$MACBOX_VM"
"$MACBOX_DOCKER" exec "$MACBOX_CONTAINER" ps -eo pid,args
```

This sequence intentionally tolerates a display process already being absent. Inspect full process arguments afterward for leftover encoder/capture helpers; do not claim success merely because the top-level names vanished. Never stop Sway while QEMU still owns an active presentation window. This manual path does not change the stock `macbox up` logic: **after a subsequent `macbox down`/`up`, display services will be launched again** until the earlier wrapper proposal is integrated. A container originally created in local mode may still have its Wayland mount even though no process uses it; recreating it in stream mode before this transition avoids that mount. ([socket mount](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:61), [display services](/home/srayansh/Documents/Golang/Blackburn/macbox/container/display-up.sh:13), [stream process patterns](/home/srayansh/Documents/Golang/Blackburn/macbox/container/stream-up.sh:18))

**Return to GUI:** while the container still exists, `ctl_existing set "$MACBOX_VM" --env REIMS_VGPU_WINDOW=1`, then deliberately `macbox view stream` (or `local`). The wrapper restarts the guest and reconnects the display. If the container was already removed, `macbox up` recreates it; set the override to 1 through ctl and then request the chosen view, causing a planned restart. Simply changing view while the override remains 0 produces no Reims presentation window. ([parser](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:608), [persistent gate](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:873))

**Preferred eventual integration:** add explicit `headless` selection that skips `display-up.sh`, sets the persisted flag to 0, prints accurate status, and starts QEMU; set the flag to 1 when returning to visible modes. A separate private-display mode can keep Sway/Reims alive and toggle only the encoder for restart-free viewing. Earlier source-reviewed proposal files exist; implementing them is a separate requested engineering change. ([proposal](/home/srayansh/Documents/Golang/Blackburn/research_notes/Headless%20macbox%20and%20IDE%20integration/headless.md:57))

**Daily native build / simulator example:** sync the project to the same macOS user's directory (or use the existing mounted workspace), then run the following on the guest. Replace scheme, bundle ID, paths and a stable simulator UUID. Select a runtime/device that exists; do not hard-code a future iPhone model.

```bash
ssh macbox
launchctl print "gui/$(id -u)" >/dev/null
xcrun simctl list devices available
cd ~/work/MyApp
xcodebuild -project MyApp.xcodeproj -scheme MyApp -configuration Debug \
  -sdk iphonesimulator -destination 'platform=iOS Simulator,id=SIMULATOR_UUID' \
  -derivedDataPath "$PWD/DerivedData" ARCHS=x86_64 ONLY_ACTIVE_ARCH=YES build
# Use -workspace MyApp.xcworkspace instead of -project when appropriate.
xcrun simctl boot SIMULATOR_UUID  # only if presently shut down
xcrun simctl bootstatus SIMULATOR_UUID -b
xcrun simctl install SIMULATOR_UUID DerivedData/Build/Products/Debug-iphonesimulator/MyApp.app
xcrun simctl launch SIMULATOR_UUID com.example.MyApp
xcrun simctl io SIMULATOR_UUID screenshot /tmp/myapp.png
exit
scp macbox:/tmp/myapp.png /tmp/myapp.png
```

Run the native build through the full guest Xcode toolchain; this research does not establish that generic native Swift/ObjC projects can run through Linux xcross unchanged. Flutter's separate Linux `xcross` path targets `x86_64-apple-ios-simulator`, then deploys/attaches over SSH; keep its existing launcher and VM-service tunneling. Do not transfer an arm64 device IPA or an arm64-only simulator plugin into an Intel simulator and expect signing to solve an architecture mismatch. ([Blackburn wrapper](/home/srayansh/Documents/Golang/Blackburn/scripts/blackburn-macbox.sh:7), [Apple build tool](https://developer.apple.com/documentation/xcode/xcode-command-line-tool-reference))

Acceptance: wait for SSH, verify the same-user `gui/<uid>` domain, complete simctl bootstatus, install/launch the real app, and inspect a nonblank screenshot. Inspect full container command lines to prove no Sway/wayvnc/Selkies helpers remain. Keep screenshots/logs as evidence; an SSH prompt alone is not proof of functioning simulator rendering. If idle for long periods, shut down individual simulators first and use `macbox down` when no guest execution is needed. ([readiness implementation](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart:136), [shutdown](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:324))

### Gaps

The manual display-service shutdown/start procedure was reasoned from the installed code, not executed. Existing wrapper status will misleadingly report stream mode even when its servers were manually stopped; an explicit lifecycle patch is preferable for repeated unattended use. Simulator/device signing behavior and API-key setup are covered in the companion signing notes rather than assumed from this runbook.
