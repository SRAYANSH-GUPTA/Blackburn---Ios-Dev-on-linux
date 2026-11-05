# Blackburn: IDE integration research and exact proposed configuration

## 1. Can existing xcross own the whole simulator debugging loop?

### Takeaway
Yes at the CLI layer in this local checkout; the IDE layer needs a small proposed patch and an editor device-discovery workaround. Do not present `xcross ide vscode` alone as tested seamless simulator support. Research date 2026-11-05; xcross package 2.0.9 at workspace commit `0635c405e8a0b9114535f83b4a85fd58cd0100de`, local files inspected, with unrelated existing modifications left untouched.

### Cited Findings

- `XCROSS_SIMULATOR_ARCH=x86_64 xcross flutter run --target-platform simulator --udid <UUID>` selects `SimctlSimulatorLauncher`. On Linux it requires `XCROSS_SIMULATOR_HOST`, an SSH config alias. The launcher boots the specified simulator, waits with `bootstatus -b`, stages the app with rsync unless shared roots map it directly, runs guest `codesign --force --deep --sign -`, installs, launches with console PTY and dynamic VM port, discovers the full auth URI, forwards through SSH, and runs a persistent `HotReloadController`. [Local CLI](../../xcross/packages/xcross/lib/src/composition/cli/flutter_run_command.dart); [local launcher](../../xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart).
- `XCROSS_SIMULATOR_SHARED_ROOT` and `XCROSS_SIMULATOR_GUEST_ROOT` optionally map host app paths into a shared guest filesystem; neither is required with rsync. The remote target must have rsync, simctl/Xcode tools, codesign, an installed simulator runtime, and noninteractive SSH access. No Apple device/CoreDevice tunnel is involved in this simulator launcher. [Local launcher](../../xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart).
- `xcross ide vscode` already generates `.vscode/xcross_dap.dart`, settings `dart.customFlutterDapPath`, and a Dart launch config marked `env.XCROSS=true`. The generated shim launches the installed executable as `xcross flutter dap`, and the router sends unmarked configurations to stock Flutter's adapter. Run the installed binary to generate it, since its absolute path is embedded. [Official README](https://github.com/arxdeus/xcross#ide-integration); [local generator](../../xcross/packages/xcross/lib/src/shared/cli/ide/subcommands/vscode_command.dart); [local merge](../../xcross/packages/xcross/lib/src/shared/cli/ide/subcommands/vscode_json_merge.dart).
- The local xcross DAP spawns `xcross flutter run --target <program> ...launchArgs.args`, waits for stdout `vm-service: <uri>`, uses DDS DartDebugAdapter for breakpoints/stepping/variables, maps local files to `package:` URIs, and sends `r`/`R` to its child for hot reload/restart. It supports launch only; XCROSS-marked `request:attach` fails by design. `toolArgs`, `deviceId` and `flutterMode` are not forwarded by this adapter; xcross CLI flags belong in `args` here. [Local DAP](../../xcross/packages/xcross/lib/src/shared/dap/xcross_dap.dart).
- Two actual gaps: simulator launcher never emits that `vm-service:` marker; DAP child startup does not forward `launchArgs.env`, only `XCROSS_DAP=1`. Thus putting `XCROSS_SIMULATOR_HOST` in launch.json does not itself work without a patch or starting the editor with inherited environment. [Local launcher](../../xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart); [local DAP](../../xcross/packages/xcross/lib/src/shared/dap/xcross_dap.dart).
- Dart-Code documents custom tool replacement but requires protocol compatibility. A shell wrapper that invokes plain human-readable `xcross flutter run` is not a replacement for `flutter run --machine`; use xcross's DAP or implement the full Flutter machine protocol. [Dart-Code customTool requirements](https://dartcode.org/docs/launch-configuration/).
- Dart-Code exposes both custom adapter paths. Installed Dart-Code 3.146.0 invokes either custom adapter as `dart <path> debug_adapter`, with `--test` for tests, so the xcross-generated shim accepts either entrypoint. Its Flutter launch provider validates the selected device before DAP creation. A remote simulator unknown to Flutter daemon cannot pass that check simply by supplying its UUID as `deviceId`. [Official adapter settings](https://dartcode.org/docs/settings/#dartcustomflutterdappath); [official source](https://github.com/Dart-Code/Dart-Code/blob/master/src/extension/providers/debug_config_provider.ts); local inspection: `/home/srayansh/.vscode/extensions/dart-code.dart-code-3.146.0/out/dist/extension.js`, `prepareLaunchDevice`, `requiresDevice`, `descriptorForType`.

### Inferences / actionable design

Recommended development investment: fix xcross's two wiring gaps, then expose a dedicated Blackburn launch configuration. Use the Dart adapter route to avoid the Flutter daemon device gate while an actual Blackburn device-provider extension is built. Standard Flutter toolbar integration remains a separate acceptance test.

The reviewable patch is [examples/xcross-simulator-dap.patch](examples/xcross-simulator-dap.patch). It forwards non-null environment maps supported by the checkout's DDS API and publishes the **host-reachable authenticated WebSocket URI** after the tunnel connects. It changes no build/install/hot reload implementation. Patch contents:

```diff
--- a/packages/xcross/lib/src/shared/dap/xcross_dap.dart
+++ b/packages/xcross/lib/src/shared/dap/xcross_dap.dart
@@
-      environment: const {'XCROSS_DAP': '1'},
+      environment: {...?launchArgs.env, 'XCROSS_DAP': '1'},
--- a/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart
+++ b/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart
@@
-      vm.onConnectionLost = stop;
+      runtime.output.writeln('vm-service: ${websocketUri(uri)}');
+      vm.onConnectionLost = stop;
```

Apply in an isolated xcross checkout, rebuild its installed bundle using its source build workflow, and rerun `xcross ide vscode` from the resulting **installed executable named xcross** in the Flutter app folder. The patch file is relative to `xcross/`, not Blackburn root. No patch was applied during this research.

SSH example (`~/.ssh/config`; substitute actual VM SSH endpoint, do not assume address):

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

Validate without IDE first:

```bash
ssh blackburn-sim 'xcrun simctl list devices available'
# Replace SIMULATOR_UUID with an available Intel runtime device UUID.
XCROSS_SIMULATOR_HOST=blackburn-sim XCROSS_SIMULATOR_ARCH=x86_64 xcross flutter run \
  --target-platform simulator --udid SIMULATOR_UUID
# Wait for Hot reload ready, edit Dart source, type r, then R, then q.
```

Optional shared root environment, only if mount is actually configured:

```bash
export XCROSS_SIMULATOR_SHARED_ROOT=/absolute/host/shared
export XCROSS_SIMULATOR_GUEST_ROOT=/Volumes/shared
```

An Apple Silicon Mac Air requires an arm64 simulator target/runtime; set `XCROSS_SIMULATOR_ARCH=arm64` and matching engine/SDK artifacts instead of copying the Intel VM configuration unchanged.

### Gaps

The xcross simulator launcher exists locally but its raw GitHub source URL was not available through this session's web fetch. Local implementation is authoritative for this Blackburn proposal; do not assume every released xcross binary contains it. Proposed wiring changes are source-reviewed, not compiled or exercised in a VM. The existing adapter emits an irrelevant iOS 17+ tunnel warning on simulator sessions; suppress `_warnIfTunnelUnreachable()` for the simulator CLI option as a later cosmetic change. A noDebug flag does not select release mode in this adapter; explicit simulator runs remain debug/JIT.

## 2. What exact VS Code / Antigravity configuration should be used?

### Takeaway
For the fully integrated direction, configure xcross's DAP and explicitly distinguish no-device Dart routing from normal Flutter routing. For immediate conservative operation use CLI hot reload plus a stock Dart attach configuration as described in the separate bridge notes. Antigravity compatibility depends on its installed Dart-Code extension and extension-host behavior; it was not tested here.

### Cited Findings

- Stock Dart-Code launch schema has `debuggerType` overrides including `dart` and `flutter`, `vmServiceUri`, `vmServiceInfoFile` and `deleteServiceInfoFile`. Its custom Dart adapter executes the same source shim invocation accepted by xcross. [Official source/schema](https://github.com/Dart-Code/Dart-Code/blob/master/package.json); [settings](https://dartcode.org/docs/settings/#dartcustomdartdappath).
- Stock Flutter semantics use `toolArgs` for flutter-tool arguments and `args` for app arguments. xcross's custom adapter has a different implementation and consumes `args` as xcross-tool arguments. Do not translate the xcross launch block to stock Flutter without changing it. [Dart-Code launch documentation](https://dartcode.org/docs/launch-configuration/); [local xcross DAP](../../xcross/packages/xcross/lib/src/shared/dap/xcross_dap.dart).
- xcross officially describes VS Code forks with Dart-Code installed as supported IDEs; this does not establish Antigravity-specific extension version or hot reload behavior. [xcross README](https://github.com/arxdeus/xcross#ide-integration).

### Inferences / exact proposed files

After generating the shim and applying the two proposed source fixes, use this `.vscode/settings.json` **in a dedicated Flutter app workspace**:

```json
{
  "dart.flutterSdkPath": "/opt/flutter",
  "dart.customFlutterDapPath": ".vscode/xcross_dap.dart",
  "dart.customDartDapPath": ".vscode/xcross_dap.dart",
  "dart.hotReloadOnSave": "never"
}
```

The Dart override is intentionally limited to the dedicated workspace: xcross's fallback router sends unmarked requests to Flutter's DAP, so it is unsuitable as a global replacement for normal Dart CLI debugging. Keep ordinary Dart projects in another workspace or remove the customDart setting when using the stock attach fallback.

`.vscode/launch.json` **proposed no-device F5 configuration**:

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
        "--target-platform", "simulator",
        "--udid", "REPLACE_WITH_SIMULATOR_UUID"
      ]
    }
  ]
}
```

`debuggerType:dart` avoids the installed extension's Flutter device validation. This is a proposed DAP launch/debug workaround, not a claim that the stock Flutter device picker now lists the simulator. Breakpoints and Restart follow xcross DAP's implementation. Flutter-specific toolbar/Inspector/DevTools and Run Without Debugging remain acceptance tests because Dart-Code may condition their UI on the debugger type. The adapter itself accepts `hotReload`/`hotRestart` requests, but this does not prove those controls are visible in this route. Keep the terminal task below as a reliable command path for r/R, and do not run both sessions concurrently against one bundle.

For an editor version that permits custom Flutter adapters without daemon device validation, change `debuggerType` to `flutter` and use the generated customFlutter path; then xcross's normal Flutter toolbar design applies. On the observed Dart-Code 3.146.0 a real valid Linux/Android device can satisfy device validation, after which xcross ignores that selected device and uses the explicit `--udid`; this is an awkward routing workaround and is **not recommended as seamless simulator discovery**. Do not put remote simulator UUID into `deviceId` expecting it to be discovered.

Complete terminal task, independent of proposed DAP fixes, in `.vscode/tasks.json`:

```json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "Blackburn: run simulator CLI",
      "type": "process",
      "command": "xcross",
      "args": [
        "flutter", "run",
        "--target-platform", "simulator",
        "--udid", "REPLACE_WITH_SIMULATOR_UUID"
      ],
      "options": {
        "cwd": "${workspaceFolder}",
        "env": { "XCROSS_SIMULATOR_HOST": "blackburn-sim", "XCROSS_SIMULATOR_ARCH": "x86_64" }
      },
      "problemMatcher": [],
      "presentation": { "reveal": "always", "focus": true, "panel": "dedicated" }
    }
  ]
}
```

This is interactive and long-lived. Do not make it a finite `preLaunchTask` without a background-ready matcher; it would prevent debugger launch until termination. The bridge notes supply the separate finite readiness/task interface for stock attach.

For an actual seamless target picker, build a Blackburn VS Code extension with its own `DebugConfigurationProvider` and `DebugAdapterDescriptorFactory`, use a distinct type such as `blackburn-ios`, list guest devices using `ssh alias xcrun simctl list devices available --json`, and hand a selected UUID to the existing xcross DAP. That avoids intercepting Dart-Code's private device registry and avoids pretending a Linux custom device is iOS. Provide Start/Stop Simulator and viewer commands separately; IDE launch should ensure VM/runtime readiness, not require a visible stream. An alternative is implement Flutter daemon and machine-run compatibility completely, including device events, app.restart/app.stop and app.debugPort events; a tiny shell wrapper is insufficient. This extension is a design, not delivered/tested implementation.

### Gaps / acceptance tests

No Antigravity instance was inspected or changed. Install compatible Dart-Code Dart and Flutter extensions there, open the same app workspace and check settings recognition, DAP startup and tasks. Verify exact editor/extension versions, F5 without a stock Flutter device, breakpoints in `package:` and entrypoint files, expression evaluation, edited-source reload, state-preserving reload, hot restart, Stop cleanup, reconnect, authentication paths, and no orphaned SSH tunnel. Unit tests should assert env forwarding to child, marker includes forwarded port and token, and DAP launches/connects when simulator is selected. No VM or host state was changed for these notes.

## 3. Can Flutter custom-devices or plain attach provide the missing simulator backend and hot reload?

### Takeaway
Stock Flutter custom-devices cannot describe an iOS simulator. Plain VM attach can debug an existing Flutter process but does not by itself implement the compilation and upload step required for edited-source hot reload.

### Cited Findings

- Flutter's CustomDeviceConfig explicitly validates platform as `linux-x64` or `linux-arm64`; CustomDevice defaults to `linux-arm64`. Its postBuild/install/run callbacks are an embedded Linux mechanism, not an iOS build backend. Adding `"platform":"ios"` to `~/.config/flutter/custom_devices.json` is rejected. [Official CustomDeviceConfig source](https://github.com/flutter/flutter/blob/master/packages/flutter_tools/lib/src/custom_devices/custom_device_config.dart); [official CustomDevice implementation](https://raw.githubusercontent.com/flutter/flutter/master/packages/flutter_tools/lib/src/custom_devices/custom_device.dart).
- Custom devices are stored under the managed Flutter configuration's `custom_devices.json` filename, and `flutter config --enable-custom-devices` creates the file; consult `flutter custom-devices list` for the actual configured path because XDG configuration may vary. [Official configuration source](https://raw.githubusercontent.com/flutter/flutter/master/packages/flutter_tools/lib/src/custom_devices/custom_devices_config.dart).
- The local hot reload controller runs persistent frontend_server compilation, uploads compressed incremental dill into VM devFS, calls `reloadSources` with the uploaded `rootLibUri`, then `ext.flutter.reassemble`. Restart resets frontend compilation and supplies a complete new kernel to `_flutter.runInView`. Simply invoking reloadSources or reassemble cannot compile changed Linux files. [Local HotReloadController](../../xcross/packages/xcross/lib/src/shared/flutter/hot_reload/hot_reload_controller.dart).
- Source debugging in xcross DAP translates local file paths using package_config; native xcross compilation from Linux keeps local source ownership. Remote Mac builds instead embed remote file locations; source matching must be addressed and verified rather than inventing an unsupported `sourceFileMap` launch property. [Local DAP](../../xcross/packages/xcross/lib/src/shared/dap/xcross_dap.dart); [Dart-Code launch schema](https://github.com/Dart-Code/Dart-Code/blob/master/package.json).

### Inferences / actionable constraints

Do not supply a fake custom_devices.json as a working solution. If using a custom-device entry purely as a selector proxy, explicitly label it a Linux proxy and still use the independent xcross DAP for all compilation/execution; that is a UI hack, not backend integration and not the recommended design.

For stock attach use `debuggerType:dart`, `request:attach`, `cwd` the same local Flutter project, `vmServiceInfoFile` containing JSON `{"uri":"http://127.0.0.1:LOCAL_PORT/AUTH_TOKEN=/"}`. Keep the complete auth path, bind SSH forwarding to loopback only, and preserve both HTTP/devFS and WebSocket access. Do not set env.XCROSS on an attach configuration because xcross DAP rejects attach. Do not leave customDartDapPath active for stock Dart attach. The bridge notes supply full prelaunch task and cleanup code.

Use exactly one compilation/reload owner: xcross's CLI controller, or a fully functioning Flutter attach tool with a compatible device/build backend. A raw Dart debugger can attach alongside xcross CLI to inspect variables while CLI handles r/R, after publishing or capturing the tunneled URI; do not let two independent reload controllers manage the same devFS session. No service-auth disabling is needed; no mDNS discovery across VM NAT is needed when the authenticated URI is explicitly supplied.

### Gaps

A fully compatible Flutter daemon/device-provider backend for this Linux iOS target does not exist in the inspected integration code. No claim of seamless Flutter status-bar device selection is justified without that additional implementation. Standard Flutter attach on Linux may accept an explicit debugger URI in some versions, but compilation/platform assumptions must be validated with the pinned Flutter SDK; the plain Dart attach fallback is conservatively debug-only.

Operational integration note: existing mac-sim idle shutdown logic must be checked before leaving a long IDE session attached. The direct xcross simulator launcher does not necessarily establish a mac-sim activity lease; use a dedicated simulator or add lease/heartbeat integration so the idle timer cannot stop a live debug session. See the independently inspected bridge notes for current mac-sim behavior. A plain URI text file is **not** interchangeable with `vmServiceInfoFile`: that debugger property expects JSON containing `uri`; ensure the bridge format matches the selected launch configuration.
