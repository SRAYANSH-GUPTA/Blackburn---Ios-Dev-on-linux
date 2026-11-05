# Run Blackburn headlessly and debug seamlessly

**Blackburn can remove GNOME from its VM execution path today at the source level; seamless Linux IDE debugging needs a small xcross repair plus explicit editor routing.** Existing macbox streaming already uses a private headless Sway rather than GNOME. For CI, the installed Reims/QEMU implementation supports GPU rendering with no host compositor or Reims window, using `-display none` and persisted `REIMS_VGPU_WINDOW=0`. For daily development, keep private Sway alive and start its capture/encoder only when requested: this permits late viewing without restarting macOS. The local xcross checkout already implements Linux compilation, remote simulator installation, authenticated VM-service forwarding and compiler-driven hot reload. Two missing DAP connections and Dart-Code's device-discovery gate prevent presenting its generated configuration as a finished Linux iOS target picker. The designs and copyable configurations below separate source-established behavior from changes that still require a real VM/IDE acceptance run.

Research date: **2026-11-05**. Evidence concerns Blackburn's local `xcross` package 2.0.9 at checkout commit `0635c405e8a0b9114535f83b4a85fd58cd0100de`, installed Dart-Code **3.146.0**, and installed locally patched Reims sources. Reims pins are base `7198fe60c9df14593e86d090cb4640c3e45ac65b`, QEMU `bd95509aa307f48b13fe8497d8a1b64267cb4c3e`, and metal2vulkan `0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd`; the installed patched Reims checkout HEAD inspected was `4743cc8497605e6b51c7cfcf662c1822ec90251d`. These are version-specific findings, not a promise that every downloaded xcross/Reims release has the same interfaces. No VM was started, simulator installed, host service changed, or IDE reconfigured in this research. Existing unrelated changes were preserved. ([Pins](/home/srayansh/Documents/Golang/Blackburn/macbox/versions.env:1), [research notes](</home/srayansh/Documents/Golang/Blackburn/research_notes/Headless macbox and IDE integration/headless.md>))

## 1. GPU execution does not require GNOME, KMS or a host window

### A render node supplies GPU access without owning a display

`/dev/dri/renderD128` grants rendering access through the DRM driver; it does not grant physical modesetting or require DRM master. Physical connector ownership uses a primary `/dev/dri/cardN` node and a different permissions/seat lifecycle. The useful compositor-free pipeline is **guest Metal → Reims → metal2vulkan → Mesa Vulkan driver → DRM render-node ioctls → Linux GPU driver**. QEMU still runs in userspace and uses KVM for the macOS CPU; “direct kernel” does not mean bypassing Mesa or replacing Reims with a kernel driver. No GNOME, Xorg, host Wayland server, physical monitor or KMS master is required for this offscreen path. ([Linux DRM render nodes](https://docs.kernel.org/gpu/drm-uapi.html#render-nodes), [Reims](https://github.com/steelbrain/reims-vgpu), [metal2vulkan](https://github.com/steelbrain/metal2vulkan))

The current container exposes `/dev/kvm` and the selected render node, and mounts the host Wayland socket only for `view local`. The private display stack creates its own Sway using `WLR_BACKENDS=headless`, `WLR_HEADLESS_OUTPUTS=1`, `WLR_LIBINPUT_NO_DEVICES=1` and `WLR_RENDER_DRM_DEVICE=$RENDER`. A virtual `HEADLESS-1` output hosts the Reims window; the compositor does not scan out to a physical display. Thus **existing `view stream` already decouples macbox from GNOME**, although it still pays private composition, capture and encoding costs. ([Container setup](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:54), [display startup](/home/srayansh/Documents/Golang/Blackburn/macbox/container/display-up.sh:12), [Sway configuration](/home/srayansh/Documents/Golang/Blackburn/macbox/docker/sway.conf:1))

| Mode | Host desktop dependency | Private compositor | Capture/encode | Suitable use |
|---|---|---|---|---|
| Existing `local` | Host Wayland session | None additional | None | Interactive local window |
| Existing `stream` | None | Headless Sway | Eagerly started | Browser-driven use |
| Proposed `private` | None | Headless Sway | Off until requested | Daily CLI/IDE, occasional viewing |
| Proposed `headless` | None | None | None | CI, background simulator, screenshots |
| KMS kiosk | None | DRM compositor or new direct scanout | Optional | Dedicated physical monitor |

`MACBOX_RENDER_NODE` selects Docker exposure, Sway's rendering device and Selkies' encoding device. It is **not itself a Reims Vulkan-device-selection setting**. Reims control configures Vulkan ICD/device selection through variables including `VK_DRIVER_FILES`, `VK_ICD_FILENAMES` and `VK_LOADER_DEVICE_ID_FILTER`. On a multi-GPU host, inspect container visibility and the selected ICD rather than assuming the render-node variable picks every stage. Device numbering can change; identify the intended GPU before setting `MACBOX_RENDER_NODE`. ([Reims QEMU environment setup](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/qemu.rs:119))

### True headless is a Reims setting, not an egl-headless recipe

The installed `apple_gfx_window_wanted()` creates Reims' own presentation window only when QEMU display type is `NONE` and `REIMS_VGPU_WINDOW` is not `0`. The GPU device still exists when the window is absent; its display configuration receives `window=NULL`. The source-supported configuration is therefore **QEMU `-display none` plus `REIMS_VGPU_WINDOW=0`**, skipping Sway startup entirely. This preserves guest GPU execution. ([Window decision](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:869), [GPU realization](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:1090), [upstream environment overrides](https://github.com/steelbrain/reims-vgpu#environment-overrides))

Persist the override in the VM configuration: **exporting it around `reims-ctl start` is insufficient** because control removes inherited presentation variables before applying configured `extra_env`. At the control-tool level, the operation is:

```bash
# Run where reims-ctl and this VM's configuration are available.
reims-ctl set mac --env REIMS_VGPU_WINDOW=0
reims-ctl start mac
```

Here `mac` is an example VM name; macbox internally uses `ctl set "$MACBOX_VM" ...`. This setting takes effect on the next VM start and must accompany bypassing `display-up.sh`; calling the current `macbox up` unchanged still starts its display services. Do not edit or start the same VM from a second control environment. ([Environment filtering](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/qemu.rs:119), [set command](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/cli.rs:1084), [current up](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:298))

QEMU `-display egl-headless,rendernode=/dev/dri/renderD128` initializes its EGL/OpenGL display backend, normally combined with a VNC/SPICE viewer. It does not replace Reims' Metal-to-Vulkan acceleration or establish a faster route for `apple-gfx-pci`. It can select a no-own-window branch in this fork, but adds an unproven display backend dependency; prefer the explicit Reims offscreen switch. Similarly, `SDL_VIDEO_DRIVER=kmsdrm` is **not a verified drop-in** here: Reims' Linux presentation creates an SDL Vulkan surface, and the build's SDL backend support, Vulkan WSI, seat permissions and connector ownership need separate validation. The current correct SDL3 variable is `SDL_VIDEO_DRIVER=wayland`. A dedicated Sway/Weston DRM session is a more conservative physical-monitor kiosk design, but remains a compositor and requires primary-node/seat access. Neither KMS nor stopping GDM is needed for the requested CI/IDE workflows. ([QEMU display options](https://www.qemu.org/docs/master/system/qemu-manpage.html#display-options), [Reims Linux rendering](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-vgpu-window/src/native/linux/render.rs:66), [SDL3 video driver hint](https://wiki.libsdl.org/SDL3/SDL_HINT_VIDEO_DRIVER))

## 2. Separate VM lifetime, private display and streaming

### The current scripts couple three independent services

Current `cmd_up` calls `display-up.sh` whenever it cannot use the host Wayland socket. That script starts private Sway, wayvnc and streaming together. `stream-up.sh` supports Selkies, noVNC, restart and status, but no off operation. Existing `view local|stream` stops and restarts the VM, so it is a boot-mode switch, not late viewer attachment. The current Selkies command uses H.264 over a WebSocket, `PIXELFLUX_HOST_CAPTURE=zwlr`, the private Wayland display and the selected encoder render node; it is not the conventional Selkies WebRTC setup. ([Current display startup](/home/srayansh/Documents/Golang/Blackburn/macbox/container/display-up.sh:29), [stream startup](/home/srayansh/Documents/Golang/Blackburn/macbox/container/stream-up.sh:1), [view dispatch](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:605))

The supplied [display-up-proposal.sh](</home/srayansh/Documents/Golang/Blackburn/research_notes/Headless macbox and IDE integration/examples/display-up-proposal.sh>) and [stream-up-proposal.sh](</home/srayansh/Documents/Golang/Blackburn/research_notes/Headless macbox and IDE integration/examples/stream-up-proposal.sh>) are reviewable replacement designs, **not installed commands**. Display startup defaults to stream off, starts wayvnc only for noVNC, and delegates streaming via `BLACKBURN_STREAM_SCRIPT`. Stream off stops capture/encoder servers while leaving Sway alive. Integrate them into macbox rather than assuming running an example file changes the CLI. Their process ownership currently assumes a dedicated macbox container; a shared multi-VM container should use explicit PID ownership instead of the container-wide wayvnc stop.

Extend macbox's mode parser/dispatcher to accept `local|stream|private|headless`, forward `MACBOX_STREAM_MODE` through its container environment helper, and replace `cmd_up`'s display selection with the following proposed block. Retain the existing `apply_tuning`, affinity and lifecycle handling; update its final status output for modes with no stream. Socket mounting must remain conditional on local mode, and existing container instances may require recreation when their mounts change.

```bash
local wd="" window=0
case "$(view)" in
  headless) ;;
  local)
    window=1
    if in_c test -S /tmp/host-wayland/wayland-0; then
      wd=/tmp/host-wayland/wayland-0
      in_c mkdir -p -m 700 /tmp/xdg
    else
      wd=$(in_c env MACBOX_STREAM_MODE=off "$REPO/container/display-up.sh" |
        sed -n 's/^WAYLAND_DISPLAY=//p')
    fi
    ;;
  private|stream)
    window=1
    local stream_mode=off
    if [ "$(view)" = stream ]; then
      stream_mode=$(cat "$RUN/stream" 2>/dev/null || echo selkies)
    fi
    wd=$(in_c env "MACBOX_STREAM_MODE=$stream_mode" "$REPO/container/display-up.sh" |
      sed -n 's/^WAYLAND_DISPLAY=//p')
    ;;
  *) die "unsupported view mode: $(view)" ;;
esac
if ! vm_running; then
  ctl set "$MACBOX_VM" --env "REIMS_VGPU_WINDOW=$window" \
    --env "WAYLAND_DISPLAY=$wd" >/dev/null
  apply_tuning
  ctl start "$MACBOX_VM"
fi
```

In integrated versions, replace the example display script's `BLACKBURN_STREAM_SCRIPT` indirection with the real companion path or set it explicitly. A late `macbox stream selkies|novnc` command must invoke display startup in the requested stream mode so noVNC's wayvnc exists; `macbox stream off` invokes the stream shutdown operation. `private` and `headless` are proposed new modes, and are not accepted by the current CLI. After integration the intended interaction is:

```bash
macbox view private        # one restart to establish private Sway
macbox up
# CLI/simulator/debugging run without any viewer.
macbox stream selkies      # attach browser capture to existing private output
macbox stream off          # retain VM, Sway and simulator
macbox view headless       # restart into compositor-free mode
```

### Late streaming depends on how the VM was started

Private-display mode permits streaming on demand because a Reims window already exists on Sway. Do not kill Sway when stopping the encoder; Reims remains connected to its display socket. **True-headless cannot acquire a Reims SDL window later merely by changing `WAYLAND_DISPLAY`**: the window pointer is chosen during GPU realization. Starting Selkies later cannot capture a window that was never created. ([GPU realization](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:1090))

For a compositor-free VM, use QMP screendump (`macbox screenshot`) or `xcrun simctl io UUID screenshot PATH` over SSH for occasional inspection. For live viewing without restart, a proposed fallback is to configure QEMU VNC at **initial VM startup** with `-vnc unix:/tmp/macbox-vnc.sock`, retain `REIMS_VGPU_WINDOW=0`, and start only the browser proxy when wanted:

```bash
# Inside the container, after configuring QEMU VNC at VM startup:
websockify --web /usr/share/novnc --unix-target=/tmp/macbox-vnc.sock 6080
```

The Unix-target option is defined by [websockify's option parser](https://github.com/novnc/websockify/blob/master/websockify/websocketproxy.py). This is a different path from wayvnc: QEMU supplies framebuffer data directly to VNC. The installed offscreen GPU code marks frames dirty and performs display/readback when a console listener or screendump requests it. It therefore offers a source-grounded path for inspection, with **GPU-to-CPU readback costs**, not the same capture/VA-API path as Sway/Selkies. Verify VNC was compiled into the installed QEMU and exercise actual Reims framebuffer output before adopting it. Keep its Unix socket container-local; macbox's existing loopback host mapping can expose the browser proxy. Preserve all existing disk/network extra arguments when adding VNC because `--extra-args` replaces the full list. No direct GPU frame-export encoder integration was implemented here. ([Offscreen console behavior](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:105), [QEMU display options](https://www.qemu.org/docs/master/system/qemu-manpage.html#display-options))

### Headless CI removes the Linux desktop, not macOS graphical services

The established workflow still needs a provisioned macOS guest, a functioning user graphical session, compatible CoreSimulator runtime, signing tools and guest Metal support. Do not infer that WindowServer, SpringBoard or backboardd can be stopped because there is no Linux window. `Simulator.app` itself is optional for CLI simulator operations. Test provisioning as the same macOS SSH user that owns the simulator:

```bash
ssh blackburn-sim 'launchctl print gui/$(id -u) >/dev/null; xcrun simctl list devices available'
ssh blackburn-sim 'xcrun simctl bootstatus REPLACE_WITH_SIMULATOR_UUID -b'
# After installing/launching the test app:
ssh blackburn-sim 'xcrun simctl io REPLACE_WITH_SIMULATOR_UUID screenshot /tmp/blackburn-ci.png'
scp blackburn-sim:/tmp/blackburn-ci.png ./blackburn-ci.png
```

Boot the chosen UUID before `bootstatus` when it is shut down; bound SSH and boot readiness waits in a CI wrapper and archive app logs/screenshots. Existing guest provisioning supports autologin; it was not changed here. A nonblank screenshot and completed app test are required acceptance evidence for actual background simulator operation. ([Guest provisioning](/home/srayansh/Documents/Golang/Blackburn/macbox/guest/README.md:1), [current simulator boot path](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart:136))

## 3. Reuse xcross's simulator runner and repair its DAP boundary

### Current CLI behavior supersedes the older runbook

The local checkout supports `xcross flutter run --target-platform simulator --udid UUID --debug`; on Linux it requires `XCROSS_SIMULATOR_HOST` identifying a **macOS SSH endpoint**, not a Docker-container shell. Architecture comes from `XCROSS_SIMULATOR_ARCH`, defaulting to x86_64 for an x64 Linux host. This report deliberately uses the environment variable: **the inspected run CLI does not define `--arch`**, so the configurations below use the architecture environment variable. The older `xcross build iphonesimulator --simulator-arch x86_64` and `build/ios/iphonesimulator/Runner.app` recipes do not describe this checkout. Intel simulator output is **`build/xcross-ios-simulator/x86_64/<appName>.app`**; read the build's printed path instead of assuming the app is named Runner. This target supports debug/JIT and rejects precompiled release/profile modes. ([Run CLI](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/composition/cli/flutter_run_command.dart:42), [generated CLI parser](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/composition/cli/flutter_run_command.g.dart:1), [architecture selection](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/darwin_sdk_kit/lib/target/simulator/simulator_target.dart:10), [output/mode policy](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/target/simulator/flutter/simulator_flutter_target.dart:14), [bundle naming](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/shared/flutter/flutter_bundle_assembler.dart:76))

For a dedicated macbox workflow, use existing simulator allocation then let xcross own install and debug launch:

```bash
cd /absolute/path/to/flutter_app
macbox up
mac-sim up
UDID=$(mac-sim udid)
XCROSS_SIMULATOR_HOST=macbox XCROSS_SIMULATOR_ARCH=x86_64 \
  xcross flutter run --target-platform simulator --udid "$UDID" --debug
# Keep this process alive: r = reload, R = restart, q = stop.
```

`mac-sim up` claims a simulator per checkout, and `mac-sim udid` returns its UUID. Its current remote boot helper can quit an already-open `Simulator.app` globally, so do not automatically run it on a shared Mac without accommodating that behavior. With an existing assigned simulator, use its UUID and xcross's boot/status path directly. `mac-sim install` installs and auto-launches without the Flutter VM-service flags, and swallows launch errors; `mac-sim run` is an Expo/React Native flow that starts Metro. Consequently **`xcross run` is the right owner of Flutter install/launch**, even when mac-sim owns allocation or viewing. A literal separate install chain is useful for inspection, but should not replace the debug launcher. ([mac-sim](/home/srayansh/Documents/Golang/Blackburn/macbox/sims/bin/mac-sim:298), [remote boot helper](/home/srayansh/Documents/Golang/Blackburn/macbox/sims/bin/mac-sim-remote:63), [dispatch](/home/srayansh/Documents/Golang/Blackburn/macbox/sims/bin/mac-sim:575))

The existing launcher boots and waits for the explicit UUID, stages the app using rsync into a unique guest temporary directory unless shared-root mapping applies, ad-hoc signs on macOS, installs, and launches through `simctl launch --terminate-running-process --console-pty` with a dynamically selected VM-service port. It parses the complete engine URI, opens a loopback SSH forward, verifies VM connectivity, starts a persistent `HotReloadController`, and owns its session cleanup. Guest prerequisites include Xcode/simctl/CoreSimulator, an installed matching runtime, codesign, rsync and noninteractive SSH. A Swift toolchain/SDK alone does not provide a runnable simulator. Optional `XCROSS_SIMULATOR_SHARED_ROOT` and `XCROSS_SIMULATOR_GUEST_ROOT` must describe a real shared mount; they are unnecessary for rsync staging. ([Existing launcher](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart:23))

### A VM service tunnel must preserve authentication and compiler ownership

An engine URL `http://127.0.0.1:54321/token=/` becomes `http://127.0.0.1:LOCAL_PORT/token=/`; WebSocket access is `ws://127.0.0.1:LOCAL_PORT/token=/ws`. Forward from Linux to macOS loopback:

```bash
ssh -N -o ExitOnForwardFailure=yes \
  -L 127.0.0.1:LOCAL_PORT:127.0.0.1:GUEST_VM_PORT blackburn-sim
```

The right-side `127.0.0.1` is the macOS SSH server's namespace. Using a container-only SSH endpoint forwards to the wrong loopback. Preserve the authentication path, use loopback-only forwarding, and let dynamic ports avoid cross-checkout collisions. Explicit URI forwarding removes the need for mDNS discovery or disabling VM-service authentication. ([Existing forwarding](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart:196))

**A debugger connection alone does not implement edited-source hot reload.** xcross's controller runs persistent frontend-server compilation on Linux, uploads incremental kernel data through devFS, invokes `reloadSources` with the new root library URI, then `ext.flutter.reassemble`. Restart performs a new full kernel/run-in-view sequence. Calling reloadSources or reassemble without compilation/upload cannot pick up changed Linux source. Maintain exactly one compiler/reload owner; a stock Dart debugger can inspect the same process while xcross CLI owns `r/R`. Stock `flutter attach` on Linux is not a guaranteed substitute: current Flutter attach still resolves a target device, while iOS simulator discovery is macOS-only. Selecting Linux falsely introduces a different platform/build backend. ([xcross reload controller](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/shared/flutter/hot_reload/hot_reload_controller.dart:1), [Flutter attach source](https://github.com/flutter/flutter/blob/master/packages/flutter_tools/lib/src/commands/attach.dart), [simulator discovery](https://github.com/flutter/flutter/blob/master/packages/flutter_tools/lib/src/ios/simulators.dart), [Flutter hot reload](https://docs.flutter.dev/tools/hot-reload))

mac-sim's pool idle timer uses mac-sim activity, viewers and app foreground changes; a persistent xcross VM-service connection is not automatically a pool lease. For long debug sessions, implement a session-owned heartbeat/lease integration or reserve a dedicated simulator outside the pool. Do not silently disable the global idle policy. Avoid `booted` as a selector in concurrent sessions, and terminate only the bundle/session that the runner owns. ([Idle policy](/home/srayansh/Documents/Golang/Blackburn/macbox/sims/bin/mac-sim:455))

### Two small DAP repairs connect the existing pieces

`xcross ide vscode` generates `.vscode/xcross_dap.dart`, a custom Flutter adapter setting and an `env.XCROSS=true` launch configuration. Its generated shim embeds the installed executable path and routes marked launches to `xcross flutter dap`; unmarked requests fall back to stock Flutter DAP. The adapter starts `xcross flutter run --target <program> ...args`, waits for exact stdout `vm-service: <uri>`, and sends `r/R` for hot reload/restart. The simulator runner currently never emits that marker, and DAP startup forwards only `XCROSS_DAP=1`, dropping launch-specific environment variables. Both gaps are covered by the reviewable [xcross-simulator-dap.patch](</home/srayansh/Documents/Golang/Blackburn/research_notes/Headless macbox and IDE integration/examples/xcross-simulator-dap.patch>):

```diff
-      environment: const {'XCROSS_DAP': '1'},
+      environment: {...?launchArgs.env, 'XCROSS_DAP': '1'},
```

```dart
// After establishing the local forwarded VM connection:
runtime.output.writeln('vm-service: ${websocketUri(uri)}');
vm.onConnectionLost = stop;
```

The supplied marker patch publishes a **reachable authenticated local WebSocket URI after VM connection, before initial devFS sync**. It does not mean hot reload is ready; wait for `Hot reload ready`. For a stronger readiness contract, place marker publication after successful `initialSync` and before accepting debugger/reload requests, while ensuring failure cleanup remains intact. This is a proposed refinement, not part of the provided minimal patch. Apply/rebuild in an isolated checkout and regenerate IDE files from the resulting installed executable named `xcross`; no source patch was applied here. The patch does not solve device discovery or startup-breakpoint timing. ([Generator](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/shared/cli/ide/subcommands/vscode_command.dart:1), [adapter spawning](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/shared/dap/xcross_dap.dart:127), [launch readiness](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart:223), [official IDE integration](https://github.com/arxdeus/xcross#ide-integration))

## 4. Configure F5 explicitly; treat the native target picker as additional work

### Establish the macOS connection before generating editor files

Create a host alias using the actual VM SSH address, port and user. The following endpoint is illustrative; verify it against macbox's configured SSH access. Keep host-key verification enabled.

```sshconfig
Host blackburn-sim
    HostName 127.0.0.1
    Port 2222
    User user
    IdentityFile ~/.ssh/id_ed25519
    BatchMode yes
    ServerAliveInterval 15
    ServerAliveCountMax 3
```

```bash
ssh blackburn-sim 'xcrun simctl list devices available'
cd /absolute/path/to/flutter_app
# Use the patched, rebuilt, installed executable; its path is embedded in the shim.
xcross ide vscode
```

All templates below use an explicit simulator UUID. Substitute a device with an Intel-compatible runtime and ensure app binary, Flutter.framework and native plugins contain x86_64 simulator slices. For an Apple Silicon Mac Air use `XCROSS_SIMULATOR_ARCH=arm64` and compatible arm64 runtime/engine inputs, rather than reusing the Intel setting.

### Source-designed no-device F5 avoids Dart-Code's Flutter device gate

The installed Dart-Code 3.146.0 validates the selected Flutter device before creating a custom Flutter DAP. A remote UUID that Flutter daemon does not know about fails that gate; simply putting it in `deviceId` does not register a simulator. Its custom Dart adapter route accepts the generated shim invocation and avoids Flutter device validation when `debuggerType` is `dart`. This supports a concrete **proposed F5 launch/debug route**, not proof of complete Flutter toolbar integration. ([Dart-Code adapter settings](https://dartcode.org/docs/settings/#dartcustomdartdappath), [debug configuration provider](https://github.com/Dart-Code/Dart-Code/blob/master/src/extension/providers/debug_config_provider.ts), [inspected installed extension](/home/srayansh/.vscode/extensions/dart-code.dart-code-3.146.0/out/dist/extension.js:1))

In a dedicated Flutter app workspace, merge the following settings, replacing `/opt/flutter` with the real matching SDK. Do not install the custom Dart adapter globally: xcross's unmarked fallback uses Flutter DAP and is unsuitable as a universal Dart CLI debugger replacement. Downloadable template: [settings proposal](</home/srayansh/Documents/Golang/Blackburn/research_notes/Headless macbox and IDE integration/examples/vscode-settings-proposal.json>).

```json
{
  "dart.flutterSdkPath": "/opt/flutter",
  "dart.customFlutterDapPath": ".vscode/xcross_dap.dart",
  "dart.customDartDapPath": ".vscode/xcross_dap.dart",
  "dart.hotReloadOnSave": "never"
}
```

Use this `.vscode/launch.json` after the two repairs and shim generation. Downloadable template: [launch proposal](</home/srayansh/Documents/Golang/Blackburn/research_notes/Headless macbox and IDE integration/examples/vscode-launch-proposal.json>).

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Blackburn: iOS Simulator (xcross DAP)",
      "type": "dart",
      "request": "launch",
      "debuggerType": "dart",
      "program": "lib/main.dart",
      "cwd": "${workspaceFolder}",
      "env": {
        "XCROSS": "true",
        "XCROSS_SIMULATOR_HOST": "blackburn-sim",
        "XCROSS_SIMULATOR_ARCH": "x86_64"
      },
      "args": [
        "--target-platform",
        "simulator",
        "--udid",
        "REPLACE_WITH_SIMULATOR_UUID",
        "--debug"
      ]
    }
  ]
}
```

These `args` are **xcross CLI arguments because this custom adapter explicitly treats them that way**. This differs from stock Flutter adapter semantics, where `toolArgs` carries Flutter-tool flags and `args` carries app arguments. The inspected xcross DAP does not forward `toolArgs`, `deviceId` or `flutterMode` to its runner. It implements launch, not marked attach. Do not apply this configuration unchanged to stock Flutter DAP. ([xcross spawn implementation](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/shared/dap/xcross_dap.dart:127), [Dart-Code launch configuration](https://dartcode.org/docs/launch-configuration/))

Select “Blackburn: iOS Simulator (xcross DAP)” in Run and Debug and press F5. The configuration chooses the remote simulator directly, so no stock Flutter target selection is required. Breakpoints/variables use xcross's DDS-based adapter and local package URI mapping. **Flutter toolbar reload/restart buttons, Inspector, DevTools, Run Without Debugging and startup breakpoints still require acceptance testing**, because editor UI can depend on `debuggerType`. The adapter implements `hotReload`/`hotRestart` requests, but that does not prove this editor route exposes the relevant controls. Explicit debug/JIT remains required; `noDebug` does not automatically choose a release simulator build. On editor versions allowing a custom Flutter adapter without the gate, `debuggerType: flutter` can use the generated custom Flutter adapter path, but that is version-dependent. ([Custom requests](/home/srayansh/Documents/Golang/Blackburn/xcross/packages/xcross/lib/src/shared/dap/xcross_dap.dart:182))

A working Linux/Android device can satisfy the observed stock Flutter gate even though xcross subsequently uses its explicit simulator UUID; that is an awkward routing hack and not the recommended product design. The simulator is **not** added to Flutter's status-bar picker by these repairs.

### Provide a terminal task for the CLI reload owner

The following `.vscode/tasks.json` launches the source-supported command independently of DAP changes. Keep its terminal open and focused for `r/R/q`. Do not run this concurrently with the DAP launch against the same app. It is long-lived and should not be a finite `preLaunchTask`, which would wait for process exit. Downloadable template: [tasks proposal](</home/srayansh/Documents/Golang/Blackburn/research_notes/Headless macbox and IDE integration/examples/vscode-tasks-proposal.json>).

```json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "Blackburn: run simulator CLI",
      "type": "process",
      "command": "xcross",
      "args": [
        "flutter",
        "run",
        "--target-platform",
        "simulator",
        "--udid",
        "REPLACE_WITH_SIMULATOR_UUID",
        "--debug"
      ],
      "options": {
        "cwd": "${workspaceFolder}",
        "env": {
          "XCROSS_SIMULATOR_HOST": "blackburn-sim",
          "XCROSS_SIMULATOR_ARCH": "x86_64"
        }
      },
      "problemMatcher": [],
      "presentation": {
        "reveal": "always",
        "focus": true,
        "panel": "dedicated"
      }
    }
  ]
}
```

### Stock Dart attach is a conservative breakpoint fallback

For stock attach, remove `dart.customDartDapPath` from workspace settings; retain the Flutter custom setting only if desired. Do not set `env.XCROSS`, because xcross DAP rejects attach. Run the CLI controller and use its host-reachable VM URI when available; with the proposed marker repair, use the local forwarded `vm-service:` value, not the original engine console URI pointing at guest loopback. This keeps xcross as the hot-reload owner while stock Dart attach supplies breakpoints and variable inspection.

For independent launch/debug inspection, a standalone [blackburn-sim-bridge.py](</home/srayansh/Documents/Golang/Blackburn/research_notes/Headless macbox and IDE integration/examples/blackburn-sim-bridge.py>) is included. It accepts an explicit SSH alias, UUID and built `.app`, validates the Intel target, boots/waits, transfers through tar, signs, installs, launches with VM-service flags, establishes loopback forwarding, verifies `getVM`, atomically publishes a mode-0600 URI file, and owns its tunnel/app/staging cleanup. It does **not** implement a compiler or hot reload, so this bridge route is debugging-only unless a compiler owner is deliberately added. Reserve a dedicated simulator or integrate the idle lease before long sessions.

```bash
cd /absolute/path/to/flutter_app
XCROSS_SIMULATOR_ARCH=x86_64 \
  xcross flutter build --target-platform simulator --debug
# Substitute the actual printed output bundle path and previously assigned UUID.
python3 '/home/srayansh/Documents/Golang/Blackburn/research_notes/Headless macbox and IDE integration/examples/blackburn-sim-bridge.py' \
  --host blackburn-sim --udid REPLACE_WITH_SIMULATOR_UUID \
  --app build/xcross-ios-simulator/x86_64/YourApp.app
# Keep bridge alive; another terminal:
cat .dart_tool/blackburn-vm-service-uri
```

Use the following independent stock attach configuration and paste the complete published URI when prompted. Stop debugging detaches the debugger; stop the bridge terminal separately to clean up the owned app/tunnel. Downloadable template: [stock attach proposal](</home/srayansh/Documents/Golang/Blackburn/research_notes/Headless macbox and IDE integration/examples/vscode-stock-attach-proposal.json>).

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Blackburn: attach existing VM (stock Dart)",
      "type": "dart",
      "request": "attach",
      "debuggerType": "dart",
      "cwd": "${workspaceFolder}",
      "vmServiceUri": "${input:blackburnVmUri}"
    }
  ],
  "inputs": [
    {
      "id": "blackburnVmUri",
      "type": "promptString",
      "description": "Paste the complete tunneled VM URI, including its authentication path"
    }
  ]
}
```

A plain URI text file is **not** valid input to Dart-Code `vmServiceInfoFile`, which expects JSON such as `{"uri":"http://127.0.0.1:PORT/token=/"}`. If a file-driven attach is preferred, convert the bridge's text file after readiness and replace `vmServiceUri` with `vmServiceInfoFile: "${workspaceFolder}/.dart_tool/blackburn-vm-service.json"`. This finite conversion does not own process readiness or cleanup:

```bash
python3 - <<'PYCONVERT'
import json, os, pathlib
p = pathlib.Path('.dart_tool/blackburn-vm-service.json')
p.write_text(json.dumps({'uri': pathlib.Path('.dart_tool/blackburn-vm-service-uri').read_text().strip()}))
os.chmod(p, 0o600)
PYCONVERT
```

A future background task can wait for bridge stdout `^BLACKBURN_READY `, then perform the conversion atomically and start attach, with session-owned cancellation. Do not invent VS Code `${file:...}` file-content interpolation: standard variables do not read arbitrary file contents. The explicit prompt route above avoids an extra extension/task-readiness dependency. Source paths must still match the local project and package configuration; do not add an unsupported `sourceFileMap` property to Dart launch JSON. ([Dart-Code schema](https://github.com/Dart-Code/Dart-Code/blob/master/package.json), [launch configuration](https://dartcode.org/docs/launch-configuration/), [VS Code variables](https://code.visualstudio.com/docs/reference/variables-reference))

### Flutter custom devices cannot be configured as iOS simulators

The inspected Flutter custom-device configuration validates platform as `linux-x64` or `linux-arm64`; its callbacks implement an embedded-Linux build/deploy device, not an iOS backend. Consequently **there is no correct `custom_devices.json` entry with `platform: ios` for this requirement**. `flutter config --enable-custom-devices` and `flutter custom-devices list` can locate the managed file, whose path depends on configuration, but adding an iOS platform is rejected. A Linux proxy entry would be a selector hack and would still require independent iOS compilation/debug routing. ([Flutter custom-device config](https://github.com/flutter/flutter/blob/master/packages/flutter_tools/lib/src/custom_devices/custom_device_config.dart), [device implementation](https://github.com/flutter/flutter/blob/master/packages/flutter_tools/lib/src/custom_devices/custom_device.dart), [config storage](https://github.com/flutter/flutter/blob/master/packages/flutter_tools/lib/src/custom_devices/custom_devices_config.dart))

For a genuinely seamless **Blackburn-owned simulator picker**, build a small VS Code extension with a `DebugConfigurationProvider`, `DebugAdapterDescriptorFactory` and a distinct debug type such as `blackburn-ios`. Query `ssh blackburn-sim xcrun simctl list devices available --json`, display eligible runtimes/devices, persist the explicit UUID per workspace, ensure VM/simulator readiness and lease, and hand the configuration to the repaired xcross adapter. Supply separate Show/Hide Stream commands so debugging does not force a viewer. Such an extension owns its own picker; it does not automatically populate Dart-Code's native Flutter picker. That latter objective needs explicit Dart-Code/Flutter device-provider integration or full Flutter daemon/machine protocol compatibility, including device events, debug-port events, restart and stop. A shell `customTool` wrapping human-readable xcross output is insufficient. ([VS Code debugger extension APIs](https://code.visualstudio.com/api/extension-guides/debugger-extension), [Dart-Code custom tool protocol requirements](https://dartcode.org/docs/launch-configuration/))

For Antigravity, open the same dedicated app workspace and install compatible Dart and Flutter extensions, then verify custom adapter settings, process spawning and task behavior in its extension host. xcross describes VS Code forks with Dart-Code as an integration target, but **no Antigravity instance/version was inspected or tested here**. Treat F5/no-device routing and toolbar behavior as the same explicit acceptance requirements, rather than promising compatibility from branding alone. ([xcross IDE integration](https://github.com/arxdeus/xcross#ide-integration))

## 5. Validate behavior before claiming performance or seamless operation

| Scope | Established in this work | Required next acceptance evidence |
|---|---|---|
| Reims compositor-free mode | Window decision and persistent environment source inspected | Boot with no host display sockets; no Sway/capture processes; nonblank app screenshot |
| Lazy private streaming | Standalone script syntax checked | Start/stop stream without changing QEMU PID or simulator state |
| QEMU VNC late viewing | Offscreen console path source inspected; design supplied | Installed VNC capability and usable live output/performance |
| xcross pipeline | Existing build/launch/forward/reload implementation inspected | SDK-backed Intel app build, install, launch, state-preserving reload |
| DAP repairs | Reviewable minimal patch supplied | Compile/analyze patch; local forwarded marker; per-launch env survives |
| VS Code | Exact JSON templates supplied; installed gating inspected | F5 without stock target, breakpoints, variables, reload/restart and cleanup |
| Stock bridge/attach | Python syntax/help checked; URI design inspected | Actual transport/signing/engine output/getVM/attach |
| Antigravity | Portable configuration design supplied | Installed extension compatibility and complete debug session |

Compare existing local Wayland, private Sway with stream off, private Sway with one viewer, and true headless under the **same guest, runtime, resolution, vCPU/RAM, app workload and warmup**. Record QEMU/Sway/encoder/GNOME CPU and RSS, Intel render/video engine utilization and power, app/test wall time, delivered frame rate and latency, and thermal state. Reims supports `REIMS_VGPU_STATS=1`, already enabled at macbox creation. Record actual Vulkan device selection. No FPS uplift or power saving was measured here; previous 60–96 FPS claims are not measurements of these proposals. Do not force `M2V_ARL_FACTS` tuning on the Core Ultra 7 155H: this laptop is Meteor Lake, not Arrow Lake. ([Creation settings](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:294), [Reims environment overrides](https://github.com/steelbrain/reims-vgpu#environment-overrides))

For true headless acceptance, start with Linux `DISPLAY`/`WAYLAND_DISPLAY` unset, verify no host Wayland mount, confirm QEMU's persisted `REIMS_VGPU_WINDOW=0`, inspect complete container process command lines for Sway/wayvnc/Selkies, wait for SSH and simulator readiness, launch the app and archive a nonblank simulator screenshot. Do not accept an SSH prompt alone as proof of GPU-backed simulator rendering. For IDE acceptance, check local/package entrypoint breakpoints, startup timing, expression evaluation, edited-source state-preserving reload, hot restart, Stop cleanup, simulator idle lease, reconnect and concurrent-workspace isolation. A native plugin missing an Intel simulator slice is a build/input problem, not a DAP problem.

## Conclusion

The practical split is to make **display availability an optional service of a persistent simulator host**. Private Sway with lazy encoding offers the simplest daily interaction model, while explicit Reims offscreen mode offers the cleanest CI execution path; their different late-viewing constraints should be visible in macbox's CLI. On the IDE side, the existing resident compiler and simulator launcher are the valuable components to preserve. Repair their debugger boundary first, prove one no-device F5 route, then implement the target-picker contract as a deliberate extension rather than disguising the simulator as an embedded Linux device.
