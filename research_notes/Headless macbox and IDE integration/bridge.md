# Linux compilation, simulator launch, and debugger bridge

## What already works in this checkout, and what command should Blackburn use?

### Takeaway
The repository already contains most of the requested bridge. Prefer its simulator launcher and hot reload controller over writing a second Flutter runner. The local runbook's `xcross build iphonesimulator --simulator-arch x86_64` and `build/ios/iphonesimulator/Runner.app` do not describe the checked-in CLI.

### Cited Findings
- `FlutterRunArgs.targetPlatform` accepts `iphone` and `simulator`; `--udid` overrides `--device-id`; the simulator branch invokes `SimctlSimulatorLauncher.run`. The command is `xcross flutter run --target-platform simulator --udid UUID --debug`. — [local source](../../xcross/packages/xcross/lib/src/composition/cli/flutter_run_command.dart)
- Architecture comes from `XCROSS_SIMULATOR_ARCH`, with x64 host default x86_64. Current bundle output is `build/xcross-ios-simulator/x86_64/<appName>.app`, where app name follows project metadata. Simulator target supports debug/JIT and explicitly rejects precompiled modes. — [architecture](../../xcross/packages/darwin_sdk_kit/lib/target/simulator/simulator_target.dart), [output policy](../../xcross/packages/xcross/lib/src/target/simulator/flutter/simulator_flutter_target.dart), [bundle naming](../../xcross/packages/xcross/lib/src/shared/flutter/flutter_bundle_assembler.dart)
- `XCROSS_SIMULATOR_HOST=macbox` selects SSH; without it Linux launcher fails explicitly. SSH alias owns ports, keys and host verification. Staging uses unique `/tmp/xcross-simulator-*`, rsync preserving modes/symlinks; optional shared root mapping avoids the copy. Guest runs `codesign --force --deep --sign -`, `simctl install`, `simctl launch --terminate-running-process --console-pty`, dynamic VM service port discovery, SSH `-L`, local VM connection retry, initial sync, then serialized r/R reload/restart. Cleanup terminates selected bundle and removes only invocation's staging. — [launcher](../../xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart)
- `mac-sim up` claims a simulator per git checkout; `mac-sim udid` returns its UUID. `mac-sim install` auto-launches with no Flutter debugger options and swallows launch errors; `mac-sim run` is Expo/React Native specific and starts Metro, not Flutter. — [mac-sim](../../macbox/sims/bin/mac-sim), [remote helper](../../macbox/sims/bin/mac-sim-remote)

### Inferences
- Working CLI recipe for this checkout:

```bash
cd /absolute/path/to/flutter_app
mac-sim up
UDID=$(mac-sim udid)
XCROSS_SIMULATOR_HOST=macbox XCROSS_SIMULATOR_ARCH=x86_64 \
  xcross flutter run --target-platform simulator --udid "$UDID" --debug
# Keep this session alive. r = reload; R = restart; q = stop.
```

- For build-only or separate installation: `XCROSS_SIMULATOR_ARCH=x86_64 xcross flutter build --target-platform simulator --debug`. Read its printed bundle path. Validate that app, Flutter.framework, plugins and runtime all include x86_64 simulator slices. An arm64-only dependency cannot run in Intel macbox.
- Keep Xcode or a proven compatible simctl plus CoreSimulator components available in the guest. `xcrun simctl help` must work for the same SSH user as the simulator owner. A Swift toolchain/SDK alone does not supply a runnable simulator.
- Select a UUID once and pass it throughout; never use `booted` for concurrent sessions. Existing launcher accepts `booted` but that is an ambiguous default.

### Gaps
- No VM lifecycle, app installation, signing, simulator, or hot reload command was executed during this research. Findings above are source analysis, not new end-to-end proof.
- `mac-sim` idle timer sees mac-sim calls/viewers/app foreground switches, not a persistent xcross/SSH VM-service connection. For sessions exceeding idle thresholds, design a lease/heartbeat that refreshes the claimed simulator while runner is alive, or use a dedicated simulator outside the managed pool. Do not silently disable a user's global idle timer. See [idle policy](../../macbox/sims/bin/mac-sim).
- `mac-sim up` can quit an already-open Simulator.app globally via its remote boot helper. Do not invoke automatically in a shared Mac session without considering that behavior. `simctl bootstatus UUID -b` alone avoids changing viewer windows. — [remote boot helper](../../macbox/sims/bin/mac-sim-remote)

## How should VM discovery, forwarding and IDE hot reload be designed?

### Takeaway
Preserve the complete authenticated URI and tunnel to macOS loopback over guest SSH. A Dart VM attach is useful for breakpoints; Flutter hot reload additionally needs the resident compiler, DevFS upload and Flutter reassemble orchestration already implemented by xcross.

### Cited Findings
- Flutter supports attach to an externally started debug engine and hot reload; only debug builds support hot reload. — [Flutter attach documentation](https://docs.flutter.dev/add-to-app/debugging), [hot reload](https://docs.flutter.dev/tools/hot-reload)
- Current Flutter attach source calls `findTargetDevice()` even if a debug URL was provided; `--debug-url`/historical `--debug-uri` preserves authentication path, while `--debug-port` is deprecated and requires auth disabled. Its IOSSimulators discovery only supports a macOS host. — [attach source](https://raw.githubusercontent.com/flutter/flutter/master/packages/flutter_tools/lib/src/commands/attach.dart), [simulator discovery](https://raw.githubusercontent.com/flutter/flutter/master/packages/flutter_tools/lib/src/ios/simulators.dart)
- xcross DAP looks for the exact stdout marker `vm-service: `, then calls `connectDebugger`. The physical device launcher emits it; simulator launcher currently emits only engine console messages and never the marker after rewriting the tunnel URI. Therefore simulator launch can have working CLI hot reload and still leave VS Code debugger waiting. — [DAP](../../xcross/packages/xcross/lib/src/shared/dap/xcross_dap.dart), [marker](../../xcross/packages/xcross/lib/src/shared/runtime/constants.dart), [simulator launcher](../../xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart)
- xcross hot reload controller performs initial DevFS sync and source compilation. VM `reloadSources` is just one RPC in this larger process. — [launcher wiring](../../xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart), [reload RPC API](https://api.flutter.dev/flutter/vm_service/VmService/reloadSources.html)

### Inferences
- Recommended IDE integration is a small simulator-path DAP repair plus xcross's existing custom Flutter DAP setup. Emit the existing marker using the *local forwarded* URI after tunnel and initial sync readiness; preserve auth path. Keep xcross as the only reload/compiler owner. IDE agent supplies concrete patches/configurations.
- Do not prescribe stock `flutter attach --debug-url URI` on Linux as a guaranteed solution: Linux cannot discover an iOS simulator device, and falsely selecting `-d linux` makes the frontend compiler target the Linux platform despite connecting to an iOS engine.
- Forward direction is Linux `ssh -L 127.0.0.1:LOCAL:127.0.0.1:GUEST_VM_PORT macbox`. The right-side loopback belongs to macOS SSH server. Forwarding through a Docker-container SSH endpoint points at the wrong network namespace. Never expose the VM service to 0.0.0.0 merely to make discovery easier. macOS simulator processes share Mac networking in the launcher model.
- Example engine URI `http://127.0.0.1:54321/token=/` becomes `http://127.0.0.1:LOCAL/token=/`; websocket becomes `ws://127.0.0.1:LOCAL/token=/ws`. Replacing it with `http://localhost:8181/` discards authentication. Guest ephemeral port (`--vm-service-port=0`) plus ephemeral host port supports multiple checkouts. A fixed host port is a convenience with single-owner semantics, not a universal requirement.
- Standard bridge lifecycle: boot chosen UUID; wait bootstatus; stage unique directory; sign/install; launch with console; parse VM URI; establish local forward and verify getVM through it; publish URI atomically; maintain leases; on cancellation stop own tunnel/console and terminate only owned bundle/UUID; remove own staged files. Never kill arbitrary port listeners or unrelated SSH sessions.
- Guest still needs a functioning macOS graphical user environment for the established simulator/Metal workflow. Removing the *Linux* GNOME desktop does not mean removing guest WindowServer/login. Provision login/autologin separately; test `ssh macbox 'launchctl print gui/$(id -u) >/dev/null; xcrun simctl list devices available'`. Existing guest provisioning explicitly supports autologin. — [guest provisioning](../../macbox/guest/README.md)

### Gaps
- Exact startup-breakpoint behavior and source mapping need testing with matching Flutter SDK/engine, xcross frontend configuration, and IDE DAP version. Do not promise a generic Dart attach gives Flutter toolbar reload or inspector integration.
- CI without guest login or WindowServer was not validated; do not infer it from the absence of a visible Simulator.app window.

## What standalone code is provided, and how is it validated?

### Takeaway
The supplied bridge is an explicit fallback for app launch and a stable debugger URI file. It deliberately does not implement a second compiler/reload pipeline.

### Cited Findings
- [examples/blackburn-sim-bridge.py](examples/blackburn-sim-bridge.py) accepts SSH alias, explicit UUID, actual .app path; checks x86_64 Mac and app binary; bootstatus; tar transfer; codesign; install; launch; parses dynamic authenticated URI; validates tunneled getVM; writes mode-0600 `.dart_tool/blackburn-vm-service-uri`; foreground lifetime owns cleanup. Every SSH argv is shell-quoted with `shlex.join`; guest shell fragments use `shlex.quote`. It never uses broad pkill, global simulator shutdown, or kills a port listener. — [provided code](examples/blackburn-sim-bridge.py)

### Inferences
Manual fallback example (substitute the actual printed app path):

```bash
mac-sim up
UDID=$(mac-sim udid)
XCROSS_SIMULATOR_ARCH=x86_64 xcross flutter build --target-platform simulator --debug
python3 /absolute/path/to/blackburn-sim-bridge.py \
  --host macbox --udid "$UDID" \
  --app build/xcross-ios-simulator/x86_64/YourApp.app
# Another terminal: cat .dart_tool/blackburn-vm-service-uri
# Paste complete URI into a Dart attach configuration for breakpoint debugging.
```

- A VS Code background task can wait for `^BLACKBURN_READY ` and attach to the published URI (prompt/manual paste or a small command-variable extension reading the file). Standard VS Code `${file:...}` is not an arbitrary file-content variable; do not invent it. Prefer repaired xcross DAP for one-click workflow.

### Gaps
- Validation performed: `python3 -m py_compile .../blackburn-sim-bridge.py` and `python3 .../blackburn-sim-bridge.py --help`, both successful. No host/guest state was changed. Tar transport, code signing, getVM HTTP readiness and actual target engine output remain end-to-end untested.
- The bridge owns the selected app, not the simulator or VM. Reserve a dedicated simulator before using it concurrently, and integrate the pool lease policy above for long sessions.
