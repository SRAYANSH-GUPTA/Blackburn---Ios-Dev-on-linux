# Blackburn iOS Development Manual

**Master documentation and operational guide · Intel Linux · 6 November 2026**

Blackburn builds iOS Simulator applications on an Intel Linux laptop and runs them in Apple's CoreSimulator inside a locally hosted macOS VM. Its graphics stack translates guest Metal to host Vulkan. No physical iPhone is needed for this development loop.

This manual describes the checked-in Blackburn implementation and its recorded recovery evidence. Commands are marked by execution environment. Historical measurements, current source behavior, and future optimization targets are distinguished throughout. Writing this manual did not reboot the VM, delete devices, rebuild the graphics stack, or benchmark memory.

## Contents

1. [Architecture and validated baseline](#1-architecture-and-validated-baseline)
2. [Configuration and first-time preparation](#2-configuration-and-first-time-preparation)
3. [Daily development runbook](#3-daily-development-runbook)
4. [Technical failures and concrete recovery procedures](#4-technical-failures-and-concrete-recovery-procedures)
5. [Memory optimization and streaming decoupling](#5-memory-optimization-and-streaming-decoupling)
6. [Diagnostics, verification, and maintenance](#6-diagnostics-verification-and-maintenance)
7. [Evidence and supporting documentation](#7-evidence-and-supporting-documentation)

## 1. Architecture and validated baseline

### 1.1 Execution and graphics paths

```text
Intel Linux host: x86_64, Core Ultra 7 155H, Intel Arc 8086:7d55
 |
 +-- Flutter / Dart / xcross / Linux Swift + Clang + Mach-O linker
 |      +-- x86_64 iOS Simulator Runner.app
 |             +-- SSH transfer, guest signing, simctl install/launch
 |
 +-- Docker: macbox container
        +-- QEMU + KVM: x86_64 macOS Tahoe
        |      +-- Xcode 26.6 Universal host tools
        |      +-- iOS 26.5 Universal Simulator runtime, build 23F77
        |             +-- iPhone 17 Pro (macbox slim 1)
        |                    +-- Flutter application / Impeller / Metal
        |
        +-- reims-vgpu + metal2vulkan -> Mesa ANV Vulkan -> Intel Arc
        +-- display presentation
               +-- local: Reims SDL3 window on host Wayland
               +-- stream: private headless Sway -> capture/encode -> browser
```

KVM executes the Intel macOS guest on the Intel CPU. CoreSimulator executes an Intel simulator application against simulator frameworks; this is distinct from emulating an ARM iPhone. Docker packages the host tools and services; QEMU supplies the VM itself.

The graphics path is **guest Metal → Apple paravirtual graphics → reims-vgpu → metal2vulkan → Vulkan/SPIR-V → Mesa ANV → Intel GPU**. An exposed render node, normally `/dev/dri/renderD128`, supplies GPU access. Selecting that node and selecting the Vulkan ICD are related configuration tasks, but they are not the same setting.

### 1.2 Reference configuration

| Component | Blackburn baseline | Operational implication |
| --- | --- | --- |
| Host CPU | Intel Core Ultra 7 155H, x86_64, 22 logical CPUs | Native Intel KVM guest; avoid ARM-only host tools |
| Host GPU | Intel Arc, PCI vendor/device `8086:7d55` | Meteor Lake; use Mesa ANV Vulkan |
| VM | macOS Tahoe, x86_64, QEMU/KVM | Requires a functional guest session and SSH |
| Graphics | reims-vgpu with patched metal2vulkan | Rebuild host backend when changing AIR translation |
| Xcode | 26.6 Universal, with x86_64 Mach-O slices | Verify tools and their dynamically loaded frameworks |
| Simulator runtime | iOS 26.5 Universal, build `23F77` | Runtime must contain Intel simulator support |
| Daily simulator | `iPhone 17 Pro (macbox slim 1)` | One booted device minimizes working set |
| Simulator rendering | 1x, 402 × 874 pixels | Uses a custom device profile, not merely a smaller window |
| Slimming | Project baseline: 168 services disabled by simslim | Check actual count/version; disabled features affect tests |
| Profile defaults | 8 GiB RAM, 16 vCPUs, 160 GiB virtual disk | Current `macbox/.env.blackburn`; existing VM settings may differ |
| Desktop presentation | 1280 × 800, 60 Hz profile | Separate from simulator render size and benchmark FPS |
| Browser endpoint | `http://127.0.0.1:6081/` | Loopback published port; not a simulator-only stream |

The **approximately 95 FPS slim simulator** description comes from the reference GlassBench results: 402 × 874 at 95.8/92.5 FPS versus 1206 × 2622 at 38.5/39.5 FPS in the cited performance research. It is not a guarantee for every app or this laptop. The checked-in browser encoder is capped at 60 FPS. A simulator can render faster than the desktop or viewer presents frames. See the [performance report](../reports/Macbox%20Meteor%20Lake%20performance.md).

At 3x the simulator has nine times as many pixels as at 1x. Reducing scale can substantially reduce surface storage and rendering work; it does **not** imply ninefold lower total VM RAM or a fixed percentage reduction in application memory.

### 1.3 Verified application result and boundaries

The recorded BYTEPAD recovery shows the slim simulator booted and `Runner` alive as historical PID 906, with WindowServer alive as historical PID 199. A [lossless screenshot](../verification/bytepad-simulator-recovery/bytepad-success.png) records the rendered onboarding screen. The recovery report identifies Flutter Impeller's Metal backend. These PIDs and the historical UDID are evidence, not identifiers to assume on the next boot.

Historical simulator UDID: `6F8411E4-C8B3-4389-AA0C-F6464E24CD4B`. Resolve the current device with `mac-sim udid` before using it.

The local compilation path supports the patched xcross Flutter workflow. Native Swift/Objective-C projects may still need guest Xcode tools for asset catalogs, storyboards, Core Data models, and other project-specific steps. Simulator success does not establish physical-device signing, ARM device execution, or App Store release readiness. Hot reload is implemented in the launcher; do not infer a completed BYTEPAD hot-reload acceptance test from its screenshot alone.

## 2. Configuration and first-time preparation

### 2.1 Establish a consistent Linux shell

Run the following in **Bash on Linux**, including when your interactive login shell is zsh:

```bash
bash
export BLACKBURN=/home/srayansh/Documents/Golang/Blackburn
export MACBOX_CONFIG="${XDG_CONFIG_HOME:-$HOME/.config}/macbox/blackburn.env"
source "$BLACKBURN/macbox/.env.blackburn"
export MACBOX_WORKSPACE="$BLACKBURN"
export XCROSS_SIMULATOR_ARCH=x86_64
export XCROSS_SIMULATOR_HOST=macbox
export MAC_SIM_HOST=macbox
export PATH="$BLACKBURN/macbox:$BLACKBURN/macbox/sims/bin:$PATH"
```

The PATH entries select this checkout's `macbox` and `mac-sim`. They avoid relying on a stale global symlink. The SSH alias `macbox` supplies the guest username, key, and forwarded port; the recorded guest user is `maac`, but commands should use the alias rather than hard-code that account.

Configuration precedence matters: the macbox CLI loads its config file, then restores exported `MACBOX_*` values from the caller. The Blackburn profile supplies defaults and preserves already supplied values. Use the same `MACBOX_CONFIG` for setup, lifecycle commands, and simulator commands. Changing a default in an environment file does not resize an existing VM.

Do not run `macbox` under `sudo`: local rendering depends on the invoking user's Wayland environment and socket permissions. Run any separately required host administration command explicitly.

### 2.2 Host checks and initial setup

**Linux:**

```bash
cd "$BLACKBURN"
git submodule update --init --recursive
(cd xcross && dart pub get)
./scripts/blackburn-macbox.sh doctor
macbox doctor
```

Check `/dev/kvm`, the GPU render node, Docker, Vulkan, Dart, Flutter, Swift, Clang, `ld64.lld`, rsync, and SSH. Doctor validates the extracted xcross SDK as well as CLI startup. The current setup guard requires at least 100 GiB free on the VM storage filesystem; the graphics build separately checks for about 25 GB free. Sparse disk capacity is not preallocated free space. Plan extra capacity for Xcode archives, SDK extraction, runtime downloads, build trees, and backups.

For a **new installation**, follow the wrapper and guest setup prompts:

```bash
cd "$BLACKBURN"
./scripts/blackburn-macbox.sh setup-macbox
macbox next
# Complete macOS installation and the local account setup in the guest.
macbox ssh-bootstrap
ssh macbox true
macbox provision
macbox xcode /path/to/Xcode_26.6.xip
macbox pool create
macbox agents install
```

The first graphics build may take 20–60 minutes or longer. Current profile parallelism is 22 jobs; reduce `MACBOX_BUILD_JOBS` before building if host memory is constrained. Initial setup is not an unattended Apple-account workflow. Preserve the Intel-compatible Xcode/runtime baseline instead of assuming the newest downloadable artifact is compatible.

### 2.3 Validate guest architecture and tool selection

**Linux, executing checks in macOS:**

```bash
ssh macbox 'uname -m; sw_vers; xcode-select -p; xcodebuild -version'
ssh macbox 'file /Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild'
ssh macbox 'lipo -archs /Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild'
ssh macbox 'xcrun simctl list runtimes; xcrun simctl list devices available'
```

Expect guest `x86_64`, an Xcode 26.6 installation, and iOS 26.5 runtime build `23F77`. A universal host tool must include an Intel slice. An Intel-compatible Xcode executable alone is insufficient if `/Library/Developer/PrivateFrameworks` contains incompatible ARM-only dependencies.

Select the full Xcode installation when using CoreSimulator:

```bash
ssh macbox 'sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer'
ssh macbox 'sudo xcodebuild -runFirstLaunch'
```

If the runtime is missing, the checked-in guest helper requests the universal architecture variant:

```bash
ssh macbox '~/.macbox/macbox-guest runtime'
```

Verify the installed runtime afterward. Universal Xcode and a universal simulator runtime are separate requirements; extracting an SDK on Linux does not install the guest runtime.

### 2.4 Extract the Linux compilation SDK

**Linux:**

```bash
"$BLACKBURN/scripts/blackburn-macbox.sh" extract-sdk /path/to/Xcode_26.6.xip
# Alternative: an already extracted local Xcode.app directory.
# "$BLACKBURN/scripts/blackburn-macbox.sh" extract-sdk /path/to/Xcode.app
```

With no argument, `extract-sdk` copies `/Applications/Xcode.app` from the guest into a temporary directory before installing the SDK. Allow substantial temporary disk space. Re-extract older SDK installations if they predate the patched x86_64 simulator targets. Check Swift/SDK compatibility: earlier BYTEPAD work encountered an SDK expecting Swift 6.4 while the selected host compiler was 6.4.2; a numerically newer compiler does not prove module compatibility.

## 3. Daily development runbook

### 3.1 Start the VM and select a viewer

**Linux:**

```bash
macbox status
macbox up
ssh macbox true
bash "$BLACKBURN/scripts/blackburn-prewarm-metal.sh"
```

`macbox up` is suitable for starting an already configured VM. Wait until SSH responds; if it does not, inspect `macbox next` and QEMU logs. The prewarm helper reads and verifies the guest Metal plugin, then checks `simctl`.

Choose a view mode before starting a development session:

| Mode | Command and access | Lifetime behavior |
| --- | --- | --- |
| Browser stream | `macbox view stream`, then `http://127.0.0.1:6081/` | Closing a browser tab leaves the VM running |
| Native Linux window | `macbox view local` | Closing the Reims SDL3 window powers off the VM |
| Alternate browser transport | `macbox stream novnc` while streaming mode is active | noVNC through private Sway/wayvnc |

**Important:** `macbox view local` and `macbox view stream` call `cmd_down` and `cmd_up` when the container is running. They restart the VM even when selecting the same mode. Save app/debugger work before issuing them. These commands are not live viewer attachment/detachment.

For browser viewing:

```bash
macbox view stream
# Open http://127.0.0.1:6081/ in one browser tab.
macbox status
```

Earlier notes call this the **WebRTC stream**. In this checkout, `container/stream-up.sh` explicitly implements **Selkies H.264 over WebSocket**, captures the private Wayland desktop, and requests a 60 FPS encoder. Use “browser stream” when diagnosing the actual transport. The endpoint and viewer decoupling remain the same.

For the native host Wayland window:

```bash
macbox view local
```

Click inside to capture keyboard/mouse. Use `Ctrl+Alt+Super` to release input. Keep the window open while the VM is needed. After any mode-switch reboot, rerun the Metal prewarm and simulator boot steps.

### 3.2 Claim and boot the slim simulator

Run these commands from the **Flutter project's directory on Linux** so simulator claims remain associated with that checkout:

```bash
cd /path/to/flutter-app
mac-sim status
mac-sim up
SIM_UDID=$(mac-sim udid)
printf 'Simulator: %s\n' "$SIM_UDID"
ssh macbox "xcrun simctl bootstatus '$SIM_UDID' -b"
```

The Blackburn profile limits the pool to `slim-1`. `mac-sim` resolves names and claims a device by checkout. `mac-sim up` boots the selected device and `bootstatus -b` waits for readiness. A claim does not guarantee indefinite uptime: the installed idle timer can shut down idle devices, and a later command can boot them again.

The broader upstream pool supports `full`, `slim`, `sharp`, and `small`. A full device preserves services needed for push, widgets, Live Activities, and similar tests; a sharp device retains stock pixel density. These kinds must actually be created and included in `MACBOX_POOL` before selection. Do not assume they exist in Blackburn's single-device configuration.

Opening Simulator.app is optional for CLI installation, launch, screenshot, and recording. To see its guest window in the desktop stream:

```bash
ssh macbox 'open -a Simulator'
```

Keep the guest's logged-in graphical session available. Quitting the Simulator.app frontend is different from logging out macOS or stopping WindowServer.

### 3.3 Compile Flutter for the Intel iOS Simulator

The requested command spelling appears in earlier workflow notes:

```bash
xcross build iphonesimulator --simulator-arch x86_64
```

**Compatibility note:** that spelling is not the CLI exposed by Blackburn's vendored xcross source. Use it only with a separate xcross version whose `--help` explicitly supports it. This checkout uses the following actionable equivalent, from the Flutter project root:

```bash
flutter pub get
export XCROSS_SIMULATOR_ARCH=x86_64
"$BLACKBURN/scripts/blackburn-macbox.sh" build-sim
```

The wrapper invokes `xcross flutter build --target-platform simulator --debug`. Its patched Intel output is under `build/xcross-ios-simulator/x86_64/`; do not assume the older `build/ios/iphonesimulator/Runner.app` path. Locate and inspect the bundle produced by the successful build:

```bash
find build/xcross-ios-simulator/x86_64 -type d -name Runner.app -prune
# Set this to the exact bundle printed above:
APP=/absolute/path/to/Runner.app
file "$APP/Runner"
```

Expect an x86_64 Mach-O simulator executable. App frameworks, Flutter engine, extensions, native assets, and plugins must also contain compatible simulator slices. An ARM64 simulator bundle cannot execute on this Intel guest; an ARM64 device bundle targets a different platform as well as a different CPU.

### 3.4 Install and launch a prebuilt bundle

**Linux, from the same project directory:**

```bash
mac-sim install "$APP"
SIM_UDID=$(mac-sim udid)
BUNDLE_ID=com.example.bytepad
ssh macbox "xcrun simctl launch '$SIM_UDID' '$BUNDLE_ID'"
```

Replace the example bundle ID with `CFBundleIdentifier` from the actual app. `mac-sim install` copies a local `.app` into the guest, installs it, and attempts to launch it already. The explicit `simctl launch` command is useful for a repeat launch and for seeing launch errors; it is not a required second installation step.

For manual staging, signing, installation, and console attachment:

```bash
rsync -a -e ssh "$APP" macbox:/tmp/
ssh macbox "codesign --force --deep --sign - /tmp/Runner.app"
ssh macbox "xcrun simctl install '$SIM_UDID' /tmp/Runner.app"
ssh macbox "xcrun simctl launch --console-pty '$SIM_UDID' '$BUNDLE_ID'"
```

The final command stays attached to application console output. Ad-hoc signing here is for simulator execution; it does not create a device provisioning profile or distribution identity.

### 3.5 Integrated launch and hot reload

For the normal iterative Flutter loop, let the patched launcher handle building, staging, signing, installing, launching, and VM-service forwarding:

```bash
cd /path/to/flutter-app
"$BLACKBURN/scripts/blackburn-macbox.sh" run-sim
```

The wrapper claims the simulator, passes its UDID to xcross, and targets the `macbox` SSH alias. The launcher discovers the Dart VM-service URL and retains its authentication path while establishing a loopback-only SSH forward. Keep that process running:

- `r`: hot reload Dart changes.
- `R`: hot restart.
- `q`: end the run session.

Native plugin, Swift, Objective-C, entitlement, or framework changes require a rebuild/relaunch. If reload fails, inspect console output, guest app liveness, the SSH tunnel, and the VM-service URL. Do not assume a fixed service port or remove the authentication path. A manual `simctl launch` alone does not start Linux's incremental compiler or hot-reload controller.

The optional shared-workspace mode is documented in the [implementation guide](MACBOX_XCROSS_GUIDE.md). A Docker bind mount alone does not expose files to macOS; the guest must also mount the supplied SMB share.

### 3.6 Capture screenshots and recordings

**Linux, from the project checkout:**

```bash
mkdir -p artifacts
mac-sim shot "$PWD/artifacts/simulator.png"
mac-sim record "$PWD/artifacts/simulator.mp4" 10
# Omitting duration records for 5 seconds:
mac-sim record "$PWD/artifacts/short.mp4"
```

These commands capture the selected simulator and copy the media back to Linux. They avoid desktop/window chrome. A static simulator writes few video frames because recording follows screen changes; frame count is not a standalone FPS benchmark. Capturing an iOS screen also does not prove that a browser viewer is delivering frames at that rate.

For a whole-macOS diagnostic screenshot, use `macbox screenshot` and inspect the resulting format; it is a different capture path from `mac-sim shot`.

### 3.7 Stop cleanly

**Linux:**

```bash
# End the interactive Flutter run with q first, if active.
mac-sim down
mac-sim status
macbox down
macbox status
```

`mac-sim down` releases the checkout's claim and shuts down slim devices. Upstream deliberately leaves a full device running when released. `macbox down` requests guest shutdown and stops the container; the implementation falls back to forced stop if graceful termination fails. Save work first. Closing the browser merely disconnects the viewer and does not replace either shutdown command.

## 4. Technical failures and concrete recovery procedures

### 4.1 ARM64 PrivateFrameworks shadow Intel-compatible Xcode

**Symptoms:** `Bad CPU type in executable`, dyld architecture errors, or CoreSimulator failures despite an x86_64-capable `xcodebuild`. A newer installation can leave ARM-only frameworks under `/Library/Developer/PrivateFrameworks`, shadowing the dependencies expected by the Intel-compatible toolchain.

Affected dependencies include **DVTDownloads, CoreDevice, CoreDeviceUtilities, and ROCKit**. Inspect the binary itself, including nested frameworks, rather than relying on bundle names:

```bash
ssh macbox /bin/bash -s <<'GUEST'
for name in DVTDownloads CoreDevice CoreDeviceUtilities ROCKit; do
  root="/Library/Developer/PrivateFrameworks/$name.framework"
  if [ -d "$root" ]; then
    find "$root" -type f -name "$name" -exec file {} \;
  fi
done
find /Library/Developer/PrivateFrameworks -type f -name CoreDeviceUtilities -exec file {} \;
GUEST
```

**Repair source:** the matching Xcode 26.6 resources, particularly `XcodeSystemResources.pkg` under `/Applications/Xcode.app/Contents/Resources/Packages/`. Expand packages with `pkgutil --expand-full`, verify `lipo -archs` includes `x86_64` or `x86_64h`, back up installed frameworks, and use `ditto` to preserve bundle layout and symlinks when restoring matching copies. Do not mix arbitrary framework versions.

The repository provides an extensive repair helper:

```bash
bash "$BLACKBURN/scripts/repair-macbox-xcode.sh"
```

Read its scope before running: it expands all bundled packages, backs up private frameworks, runs installers, repairs DVTDownloads/CoreDevice/ROCKit if necessary, selects Xcode, runs first launch, installs a runtime/tools if needed, creates the pool, and boots a device. It writes its log/backups to a timestamped `~/macbox-xcode-repair-*` directory **inside the guest**. It requires passwordless guest sudo. It may stop at the Metal page-in fault described below; prewarm before retrying.

**CoreDeviceUtilities caveat:** the helper's explicit repair list does not include this framework. Copying an entire verified CoreDevice bundle can restore a nested dependency, but a separately shadowing CoreDeviceUtilities still requires inspection and targeted repair. To repair an additional affected framework, execute this pattern in a **guest Bash shell**, substituting the verified expanded payload path and actual installed destination:

```bash
name=CoreDeviceUtilities
source_framework=/absolute/path/to/expanded/Payload/CoreDeviceUtilities.framework
dest="/Library/Developer/PrivateFrameworks/$name.framework"
backup="$HOME/blackburn-framework-backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$backup"
file "$source_framework/Versions/A/$name"
lipo -archs "$source_framework/Versions/A/$name"
# Proceed only after confirming the matching Xcode payload and Intel slice.
if [ -e "$dest" ]; then sudo mv "$dest" "$backup/"; fi
sudo ditto "$source_framework" "$dest"
sudo chown -R root:wheel "$dest"
```

If the loaded dependency is nested elsewhere, use that destination rather than creating a duplicate at the example path. Use dyld's reported load path as the authority. Retain backups until `xcodebuild -version`, `xcrun simctl list`, and a simulator boot succeed. Rollback means restoring the corresponding backed-up bundle, with the guest tools stopped; avoid a half-old, half-new dependency set.

### 4.2 Guest DNS through passt

**Symptoms:** guest has a network route but cannot resolve download hosts or retrieve runtime/package metadata. The passt/gateway DNS path may fail even while Linux DNS works.

**Linux, running the fix in macOS:**

```bash
ssh macbox 'networksetup -listallnetworkservices; scutil --dns'
ssh macbox 'sudo networksetup -setdnsservers Ethernet 8.8.8.8 1.1.1.1'
ssh macbox 'dscacheutil -q host -a name developer.apple.com'
ssh macbox 'curl -I --connect-timeout 15 https://developer.apple.com/'
```

Use the exact network-service name printed by `-listallnetworkservices` if it is not `Ethernet`. This sets explicit resolvers to bypass the problematic gateway DNS path; it does not change passt's default route or fix a disconnected network. If an IP connection also fails, inspect routing/connectivity separately. Restore DHCP-provided DNS with:

```bash
ssh macbox 'sudo networksetup -setdnsservers Ethernet Empty'
```

### 4.3 Local SDL3 window-close shutdown trap

The native Reims window is part of VM presentation, rather than a detached remote viewer. Closing it queues the QEMU Apple graphics close event. In the inspected local QEMU source, `apple_gfx_input_close()` sets `SHUTDOWN_ACTION_POWEROFF` and calls `qemu_system_shutdown_request(SHUTDOWN_CAUSE_HOST_UI)`.

Earlier operational notes describe this as an **ACPI shutdown trap**. The concrete implementation is a host-UI QEMU poweroff request; it should not be assumed to wait for an orderly guest ACPI shutdown. Treat the close button as a VM power action.

**Fix:** select `macbox view stream` before the session and view `http://127.0.0.1:6081/`. Reims then presents to private headless Sway, while the browser is a separate client. Closing its tab leaves QEMU and CoreSimulator alive. Do not kill Sway or the Reims window to “detach” the stream. Switching from local mode to stream mode itself currently reboots the VM.

### 4.4 APFS compressed Metal-plugin page-in fault

**Symptoms:** `xcrun simctl` terminates with signal 10 / `SIGBUS` (often shell exit status 138). Recovery crash reports contain `EXC_BAD_ACCESS`, `KERN_MEMORY_ERROR`, and trap 14 with “no mapping for user instruction read.” The failing instruction address is in:

```text
/System/Library/Extensions/AppleParavirtGPUMetalIOGPUFamily.bundle/Contents/MacOS/AppleParavirtGPUMetalIOGPUFamily
```

The recorded failure occurred as CoreSimulator queried Metal devices. Recovery attributed it to compressed APFS demand-page loading; the crash signatures establish the instruction page-in failure, while the exact filesystem mechanism remains a diagnosis rather than a general proof for every SIGBUS.

**Concrete workaround, on Linux after SSH becomes available:**

```bash
bash "$BLACKBURN/scripts/blackburn-prewarm-metal.sh"
```

The helper executes these operations in the guest:

```bash
plugin=/System/Library/Extensions/AppleParavirtGPUMetalIOGPUFamily.bundle/Contents/MacOS/AppleParavirtGPUMetalIOGPUFamily
/usr/bin/shasum -a 256 "$plugin"
/usr/bin/codesign --verify --verbose=2 "$plugin"
/usr/bin/xcrun simctl list devices
```

A complete sequential hash read warms file pages before Metal executes them; signature verification checks the bundle binary. This preceded successful simulator calls in recovery. It is a **per-boot cache workaround**, not permanent APFS repair, on-disk decompression, or a reason to modify the sealed system volume. Cache eviction can also require another read. If hashing or verification fails, preserve the error and investigate storage/image integrity rather than repeatedly suppressing the failure.

Success criteria: `simctl list` returns, `bootstatus -b` completes, an app launches, and a nonblank screenshot is captured. See the [recovery crash reports](../verification/bytepad-simulator-recovery/) and [prewarm script](../scripts/blackburn-prewarm-metal.sh).

### 4.5 Flutter Impeller Metal AIR shader rejection

**Symptoms:** application launches but renders black; QEMU/reims logs contain `pipeline lookup failed: Abi(UnknownKey)` or `AbiError::UnknownKey` during shader/pipeline creation. This is separate from the pre-execution Metal-plugin SIGBUS.

**Cause:** Flutter Impeller's Metal AIR includes `llvm.module.flags` entries named `Dwarf Version` and `Debug Info Version`. The strict `metal2vulkan-air` ABI parser rejected these otherwise harmless debug metadata keys.

**Fix:** [patch 0006](../macbox/patches/metal2vulkan/0006-accept-Flutter-shader-debug-module-flags.patch) adds those two names to the existing ignored-module-flag branch, alongside `wchar_size`, `frame-pointer`, and `SDK Version`. The parser still consumes/validates the expected metadata structure. Resource-limit keys continue to update limits, and genuinely unknown flags continue to fail. This is a narrow metadata compatibility fix; it does not disable all ABI validation or change shader semantics.

The patch adds a regression case checking that debug flags do not change texture/sampler limits and updates the unknown-key test to use a genuinely unknown flag. The archived focused AIR test run reports eight passing tests; that is evidence for that selected run, not the entire backend suite.

**Rebuild procedure, Linux:** preserve any experimental changes in the host backend source trees first. `build-reims.sh` resets its managed checkouts, applies the pinned patch series, builds the Vulkan library, links QEMU, installs a timestamped build, and switches `qemu/current`. Editing a source file alone does not replace the library already linked into a running QEMU.

There is an important packaging prerequisite in this checkout: patch 0006 is currently a raw Git diff, while the builder applies patches using `git am`. Normalize it to a mail patch before invoking the builder. The following preserves the original alongside it with a non-`.patch` suffix and adds the required mail envelope only if absent:

```bash
python3 - "$BLACKBURN/macbox/patches/metal2vulkan/0006-accept-Flutter-shader-debug-module-flags.patch" <<'PY'
from pathlib import Path
import sys
p = Path(sys.argv[1])
data = p.read_bytes()
if data.startswith(b'diff --git '):
    backup = p.with_suffix(p.suffix + '.raw-backup')
    if backup.exists():
        raise SystemExit(f'Backup already exists: {backup}; inspect before proceeding')
    backup.write_bytes(data)
    header = (
        'From: Blackburn <blackburn@localhost>\n'
        'Date: Fri, 6 Nov 2026 12:00:00 +0000\n'
        'Subject: [PATCH] Accept Flutter shader debug module flags\n'
        '\n'
        'Accept known debug metadata without changing AIR resource limits.\n'
        '\n'
        '---\n'
    )
    p.write_bytes(header.encode() + data)
    print(f'Added git-am mail header; original: {backup}')
else:
    print('Patch is not a raw diff; inspect its existing mail format if needed')
PY
macbox build
macbox down
macbox up
bash "$BLACKBURN/scripts/blackburn-prewarm-metal.sh"
```

Do not use `macbox build --upstream` for this recovery; that deliberately skips Blackburn's patches. A build can run while the old VM remains alive, but the new backend takes effect only after restarting QEMU. If the build fails, preserve its output and existing known-good build; do not assume the active VM has changed.

After reboot, reinstall/relaunch the app and inspect:

```bash
macbox logs qemu 200
mac-sim up
mac-sim install "$APP"
mac-sim shot "$PWD/artifacts/after-impeller-fix.png"
```

Look for a rendered first frame, an Impeller Metal backend message in app output, and absence of the prior unknown-key failure. Keep the previous QEMU build ID and `current` target when planning rollback; use the backend's managed build selection rather than deleting old builds during recovery.

### 4.6 Remove unused stock simulator devices

The project records removing **11 default Apple stock devices**, recovering approximately **1.2 GB of guest disk space**, and reducing CoreSimulator device-indexing lag. Treat those numbers as a historical cleanup result; actual recovery depends on device data and APFS sharing. Removing a device does not remove its shared runtime and does not directly free 1.2 GB of RAM. Host qcow2 disk allocation may not shrink immediately after guest deletion.

**Controlled cleanup:** list devices, retain the slim pool device, and delete only explicitly selected unused UDIDs. The following is a template; replace the placeholder with each reviewed stock device:

```bash
ssh macbox 'xcrun simctl list devices available; du -sh ~/Library/Developer/CoreSimulator/Devices; df -h /'
UNUSED_UDID=REPLACE_WITH_ONE_REVIEWED_STOCK_DEVICE_UDID
ssh macbox "xcrun simctl shutdown '$UNUSED_UDID'"
# A shutdown device may report that it was already shut down; inspect the result.
ssh macbox "xcrun simctl delete '$UNUSED_UDID'"
ssh macbox 'xcrun simctl list devices available; du -sh ~/Library/Developer/CoreSimulator/Devices; df -h /'
```

Deletion removes that device's installed apps and data. `simctl delete unavailable` is a separate convenience operation for devices whose runtimes are unavailable; it does not select all unused stock devices. Avoid `delete all`, which also destroys the working pool. After cleanup, verify `mac-sim status`, the retained UDID, and an app launch. Do not rerun pool creation merely to clean stock devices.

## 5. Memory optimization and streaming decoupling

### 5.1 Can streaming the simulator out of macOS save RAM?

**Yes, avoiding guest frontend overhead and running a smaller VM can reduce the host working set. Streaming alone does not move CoreSimulator execution out of macOS.** The app, iOS services, Metal objects, and simulator surfaces remain in the guest. Video compression reduces transport bandwidth; it does not compress QEMU's configured guest RAM.

There are three separate budgets:

| Budget | Contributors | Effective controls |
| --- | --- | --- |
| Guest working set | macOS, WindowServer, CoreSimulator, app, Xcode/build tools | One slim device, Linux compilation, close guest IDE/frontend, tune VM RAM |
| Host graphics/display | Vulkan allocations, Reims presentation, Sway, encoder | 1x device, lower desktop resolution, avoid unnecessary presentation/capture |
| Host clients/tools | Browser decoding, IDE, compiler/build jobs | One viewer tab, moderate parallel builds, close unused clients |

The host QEMU RSS, guest memory pressure, Docker accounting, and GPU shared memory overlap in ways that prevent simply adding all reported values. Configured VM memory, resident memory, cache, and swap are distinct metrics. macOS memory compression can manage some pressure, but it costs CPU and is not a substitute for enough RAM. Host swap may prevent an out-of-memory kill while causing severe interactive stalls.

### 5.2 Reducing the VM from 8 GiB toward 4–5 GiB

A lean guest that runs one 1x slim simulator, compiles on Linux, and keeps Xcode/Simulator.app frontends closed is a reasonable **5 GiB trial**, followed by **4 GiB only if measurements pass**. This is a tuning target; there is no archived 4–5 GiB acceptance benchmark proving the entire BYTEPAD workflow at that size. Stream mode alone does not automatically make 4 GiB adequate.

Before resizing, capture a baseline at 8 GiB. Keep the guest user logged in, close unused guest applications, and stop extra simulators. To resize an **existing VM**, change its persisted reims-ctl configuration while it is stopped. Updating only `MACBOX_MEMORY` in an env file affects creation defaults, not the existing configuration.

The current CLI has no `macbox memory` command. One concrete way to update the persisted VM while the normal container is stopped is this **Linux Bash** sequence. It uses the container image and persistent HOME layout from `macbox`:

```bash
mac-sim down
macbox down
export MACBOX_MEMORY=5G
source "$MACBOX_CONFIG"
# Explicitly select the test value after loading configuration:
export MACBOX_MEMORY=5G
MACBOX_HOME=${MACBOX_HOME:-$HOME/.local/share/macbox}
MACBOX_VM=${MACBOX_VM:-mac}
MACBOX_IMAGE=${MACBOX_IMAGE:-macbox:run}
docker run --rm \
  -e "HOME=$MACBOX_HOME/home" \
  -v "$MACBOX_HOME:$MACBOX_HOME" \
  "$MACBOX_IMAGE" bash -c \
  '. "$HOME/.cargo/env"; exec reims-ctl set "$1" --memory "$2"' \
  blackburn-resize "$MACBOX_VM" "$MACBOX_MEMORY"
macbox up
bash "$BLACKBURN/scripts/blackburn-prewarm-metal.sh"
```

This assumes the image and VM have already been initialized. Record the new value in the chosen profile/config to keep operator expectations consistent; exported values override the file. Verify guest physical memory with `ssh macbox 'sysctl hw.memsize'`. Retain the former value, normally `8G`, for rollback using the same stopped-VM procedure. Never edit a running guest's memory by assuming its environment variable is a live balloon control.

Accept 5 GiB only after repeated cold boots, simulator readiness, app launch, rendering, scrolling/navigation, screenshot/recording, and a hot-reload/restart session. Monitor swap growth and guest memory pressure during longer sessions, not only idle usage. Restore 8 GiB if there are pressure-related crashes, persistent swap activity, or noticeable stalls. Guest Xcode builds or multiple simulators may require the larger allocation.

### 5.3 Avoiding Simulator.app GUI chrome

Simulator.app is a macOS frontend around CoreSimulator. Its window decoration, device bezel presentation, and optional visualizations add frontend/compositing work; detailed bezel/effect presentation can involve Metal. Do not treat a fixed “3D bezel” cost as established for every frontend/version. Rendering the actual iOS application remains necessary even when its frontend is closed.

For CLI development, boot via `mac-sim`/`simctl`, install/launch via CLI, and capture simulator media directly. If Simulator.app was opened, quit its frontend through the guest UI or application quit command and confirm the same device remains booted. Do not substitute `simctl shutdown`, logout, or killing WindowServer: those change essential execution state.

Existing browser mode streams the **macOS desktop**. It does not export a raw simulator IOSurface or eliminate Simulator.app's frontend by itself. Without a visible frontend, use `mac-sim shot`/`record` for inspection unless a separate simulator-specific viewer is installed and validated.

### 5.4 Four decoupling levels

| Design | Available now? | What it removes | What remains |
| --- | --- | --- | --- |
| Browser desktop stream | Yes: `macbox view stream` | Dependence on a visible host desktop window; browser closure no longer powers off VM | Private Sway, capture/encode, guest GUI session |
| CLI simulator without Simulator.app | Existing CLI path; verify frontend-free workload | Guest simulator window/chrome | CoreSimulator, app rendering, guest display services |
| Compositor-free host presentation | Source-supported proposal; no installed `view headless` mode | Host Sway/window/encoder if configured accordingly | Guest macOS session and simulator graphics |
| Direct simulator frame export | Engineering roadmap | Full desktop capture and potentially frontend chrome | Simulator surfaces, synchronization, encoder, input bridge |

The existing stream uses headless Sway on Linux. “Headless” here means no physical Linux display/session is required; it does **not** mean macOS has no WindowServer. True host-compositor-free Reims operation uses `REIMS_VGPU_WINDOW=0` in the VM's configured environment, with matching launcher changes to bypass display startup. Merely exporting that variable in the invoking shell is insufficient: reims-ctl filters inherited values and applies VM environment overrides.

The repository's [headless design notes](../research_notes/Headless%20macbox%20and%20IDE%20integration/headless.md) provide proposals. `macbox view private`, `macbox view headless`, and `macbox stream off` are **not implemented commands in the inspected CLI**. Do not copy them into daily operations as if available. Reims creates its presentation window during GPU realization; a later browser connection cannot conjure a window that was never created.

### 5.5 Raw CoreSimulator frames and IOSurfaces

A future simulator-only stream would capture CoreSimulator output without the entire guest desktop, export or encode frames, and supply an input/control channel. Candidate paths involve guest capture APIs/IOSurfaces or a frame-export interface in Reims. IOSurface handles are not automatically shareable across the VM boundary with a Linux encoder.

An implementation must establish frame ownership/lifetime, GPU fences, pixel formats, timestamps, readback/copy behavior, encoding, resolution changes, input mapping, and recovery after simulator restarts. A CPU readback prototype is simpler but may negate performance gains. Zero-copy requires an explicit cross-boundary resource-sharing design; Vulkan/Metal translation alone does not provide it.

Success would mean lower measured guest frontend/desktop overhead, preserved app rendering, and independent viewer lifetime. It would still require macOS and CoreSimulator. Prefer the existing browser stream for interactive work until a simulator-specific pipeline is implemented and measured.

### 5.6 Roadmap: direct QEMU iOS via vphone-linux

[vphone-linux](https://github.com/YahiaAngelo/vphone-linux) describes running a virtual iPhone on Linux using QEMU, Apple's `vresearch101` platform, reims-vgpu, and metal2vulkan. It is a separate execution architecture that could remove the macOS desktop/Xcode/CoreSimulator VM from the runtime path.

The project's **2–3 GiB RAM objective** for this route is a future target, not a verified minimum or total host-memory result. Validate the required iOS image, emulated device RAM, QEMU translation overhead, GPU allocations, installation/signing mechanism, and debugger behavior before relying on it. An ARM iOS device environment needs an ARM64 device build, not Blackburn's x86_64 simulator `Runner.app`. On an Intel host, ARM CPU execution also introduces emulation costs absent from the Intel KVM simulator path.

Evaluate it separately against the same app/navigation workload and measure total host RSS/PSS, GPU memory, CPU, first-frame latency, frame pacing, deployment, and reload/debugger support. It becomes a replacement only after these acceptance checks pass; a lower guest RAM setting alone is insufficient evidence.

## 6. Diagnostics, verification, and maintenance

### 6.1 Fast triage

| Failure | First checks | Recovery direction |
| --- | --- | --- |
| SSH unavailable | `macbox status`, `macbox next`, `macbox logs qemu` | Finish guest setup/Remote Login; check container and port |
| Tool architecture error | `file`, `lipo -archs`, dyld dependency path | Matching Universal Xcode and framework repair |
| Download name lookup fails | Guest `scutil --dns`, network service names | Explicit Ethernet DNS resolvers |
| `simctl` SIGBUS | Crash report signal/trap/module | Metal-plugin prewarm; investigate persistent I/O failure |
| Flutter launches black | QEMU unknown-key errors, app Impeller logs | Patch 0006 and restart into rebuilt backend |
| Closing local window kills session | Current `macbox view` | Use browser stream for detached viewer lifetime |
| Browser paused/black | One controller tab, Selkies logs | Close duplicate tabs; `macbox stream restart` |
| App cannot install | Bundle CPU/platform, guest signing, selected UDID | Rebuild x86_64 simulator bundle; inspect console |
| Hot reload fails | Run process, SSH forward, VM-service URL, app PID | Reattach integrated run; preserve auth path |
| High memory/lag | Booted device count, swap, 1x profile, open apps | One slim device; measured RAM/parallelism tuning |

Some older environments also need `METAL_ERROR_MODE=5` and reduced transparency to bypass unsupported desktop shaders. Use the existing guest base provisioning and the [guest tuning research](../research_notes/Macbox%20Meteor%20Lake%20performance/guest.md) when the logs match that failure. This is separate from accepting Flutter's AIR debug metadata.

### 6.2 Collect evidence before changing the system

**Linux:**

```bash
mkdir -p artifacts
macbox status > artifacts/macbox-status.txt
macbox logs qemu 300 > artifacts/qemu-tail.txt
macbox logs selkies 100 > artifacts/selkies-tail.txt
ssh macbox 'sw_vers; uname -m; xcodebuild -version; xcrun simctl list runtimes; xcrun simctl list devices' \
  > artifacts/guest-inventory.txt
mac-sim shot "$PWD/artifacts/current-simulator.png"
```

A screenshot command needs a usable selected device and may fail during early boot/recovery; keep that failure with the logs. Save relevant `.ips` reports from the guest's user or system `Library/Logs/DiagnosticReports` directories. Record the active QEMU build ID, patch revision, and previous known-good settings before backend changes.

For memory and CPU observations:

```bash
free -h
docker stats --no-stream "${MACBOX_CONTAINER:-macbox}"
ssh macbox 'sysctl hw.memsize; vm_stat; sysctl vm.swapusage; memory_pressure'
```

The [host performance audit script](../scripts/blackburn-performance-audit.sh) and [guest audit script](../research_notes/Macbox%20Meteor%20Lake%20performance/guest-audit.sh) provide broader inventory. Review their output alongside app behavior. Meteor Lake PCI `7d55` is not an Arrow Lake tuning target; do not force Arrow Lake-only driver facts based on the shared Intel branding.

### 6.3 Acceptance checklist after repair or upgrade

1. Confirm the expected VM architecture, Xcode version, Intel tool slices, and runtime build.
2. Confirm guest DNS resolves and SSH remains stable.
3. Run Metal prewarm; `xcrun simctl list devices` must complete without SIGBUS.
4. Boot exactly one slim simulator and wait for `bootstatus -b`.
5. Build/inspect an x86_64 simulator bundle; install and launch it on that UDID.
6. Capture a nonblank screenshot; exercise navigation, animation, and input.
7. Run the integrated Flutter loop and verify hot reload plus hot restart separately.
8. Capture a recording and confirm output is readable.
9. Close the browser viewer and confirm the VM/device/app remain alive; reconnect.
10. Check host/guest memory pressure and swap during sustained activity.
11. Stop the simulator and VM through their explicit lifecycle commands.

Archive actual commands, versions, outputs, and media. Report a rendering test as a rendering test, and a memory or FPS measurement only with the workload, duration, and capture method stated.

### 6.4 Upgrade policy

Keep Xcode 26.6 and iOS 26.5 Universal `23F77` as the reproducible baseline until an alternate version passes the checklist. Validate every executable dependency's architecture rather than relying on release numbering or a “Universal” filename. Retain a known-good VM backup and backend build before changing frameworks, runtime, graphics translation, or device profiles.

The operating procedures above concern local simulation. Device/distribution signing, keychain imports, Apple-team configuration, and generated Xcode project settings are covered separately in the [signing and Xcode configuration report](../reports/Apple%20signing%20keychain%20and%20xcode%20config%20without%20GUI.md).

## 7. Evidence and supporting documentation

The following sources anchor this manual to inspectable artifacts:

- [Blackburn profile](../macbox/.env.blackburn): current resource, GPU, pool, and parallel-build defaults.
- [Blackburn wrapper](../scripts/blackburn-macbox.sh): SDK extraction, doctor, simulator build/run, and workspace integration.
- [macbox CLI](../macbox/macbox): VM lifecycle, configuration precedence, viewer restart behavior, and Docker mappings.
- [mac-sim CLI](../macbox/sims/bin/mac-sim): pool claims, device lifecycle, install/launch, and media capture.
- [Browser stream implementation](../macbox/container/stream-up.sh): H.264/WebSocket, capture mode, encoder rate, and stream restart.
- [Graphics builder](../macbox/container/build-reims.sh): pinned checkouts, mail-patch application, Vulkan library build, and linked QEMU installation.
- [PrivateFrameworks repair helper](../scripts/repair-macbox-xcode.sh) and [Metal prewarm helper](../scripts/blackburn-prewarm-metal.sh).
- [Flutter metadata compatibility patch](../macbox/patches/metal2vulkan/0006-accept-Flutter-shader-debug-module-flags.patch).
- [Recovery final state](../verification/bytepad-simulator-recovery/final-state.txt), [AIR tests](../verification/bytepad-simulator-recovery/air-tests.log), [shader metadata](../verification/bytepad-simulator-recovery/flutter-shader-metadata.ll), and [rendered screenshot](../verification/bytepad-simulator-recovery/bytepad-success.png).
- [Implementation and hot-reload guide](MACBOX_XCROSS_GUIDE.md).
- [Architecture research](../reports/iOS%20compilation%20and%20simulator%20architectures.md), [headless/IDE report](../reports/Headless%20macbox%20and%20IDE%20integration.md), and [Meteor Lake performance report](../reports/Macbox%20Meteor%20Lake%20performance.md).
- [External BYTEPAD recovery report](/home/srayansh/.gemini/antigravity/brain/dc8edf1c-e0c0-4177-8dc0-1756acfebf93/bytepad_verification_report.md): local machine artifact; may be absent on other checkouts. The essential screenshot/log evidence is also stored in this repository.
- [Upstream macbox](https://github.com/TUNA-NOPE/macbox) and [vphone-linux](https://github.com/YahiaAngelo/vphone-linux): upstream project descriptions; Blackburn's checked-in sources govern the commands documented here.

Historical cleanup figures (11 devices, approximately 1.2 GB) and the 168-service slim baseline are project operational records, not new measurements performed for this document. The 95 FPS figure is a referenced simulator benchmark. The 4–5 GiB macbox and 2–3 GiB direct-iOS configurations remain optimization targets until recorded acceptance runs establish them.
