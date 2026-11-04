# Blackburn: Intel Linux → macbox iOS Simulator

Implemented on `feat/macbox-xcross-simulator`. Linux builds the Flutter Debug app;
macOS runs CoreSimulator. SSH carries commands, console logs and a loopback-only
Dart VM-service tunnel. Dart edits use xcross's existing incremental compiler and
`HotReloadController` (`r` reload, `R` restart, `q` quit).

## What is implemented

- `XCROSS_SIMULATOR_ARCH=x86_64|arm64` controls compiler triples, runner and extension
  linkage, SwiftPM framework selection, native hooks/manifest selection and framework
  thinning. x64 hosts default to x86_64; ARM64 remains available explicitly.
- SDK installation writes **both** Simulator targets in `swift-sdk.json`, using the
  iPhoneSimulator SDK and shared `toolset.json`. Reinstall previously extracted SDKs.
- Intel Simulator output is under `build/xcross-ios-simulator/x86_64/`; ARM64 keeps
  its previous output path. Architecture-specific SwiftPM caches prevent reuse of
  incompatible plugin builds.
- `xcross flutter run --target-platform simulator` builds with
  `SimulatorFlutterTarget`, stages, ad-hoc signs on the Mac, installs, launches with
  `simctl --console-pty`, discovers the VM service, forwards its port, and attaches
  hot reload. The authentication path survives both WebSocket and devFS HTTP requests.
- `SimctlSimulatorLauncher` lives in
  `xcross/packages/xcross/lib/src/composition/simulator/`. This checkout's actual
  runner/compiler implementations are `shared/flutter/build/runner_shim.dart`,
  `flutter_debug_bundler.dart`, `ios_native_assets.dart`,
  `build/internal/native_asset_frameworks.dart` and `swiftpm/binary_layout.dart`;
  several filenames in the original task belong to a different xcross revision.
- `macbox/.env.blackburn`: 8 GiB guest RAM, 8 vCPUs, 1280×800 at 60 Hz, one simulator,
  four build jobs, `/dev/dri/renderD128`. The existing pinned reims-vgpu and
  metal2vulkan patches supply Vulkan graphics. Arrow Lake-only driver facts remain
  restricted to upstream's detected PCI IDs; Meteor Lake is not assumed equivalent.
- Source snapshots of xcross and macbox are vendored in Blackburn. Their licenses
  remain in those directories. The required OpenAppleMacros dependency is a root
  Git submodule pinned to `b29af51df1f1d6d4168c73955ad247ca1d1866ad`.

## First checkout and host checks

```bash
export BLACKBURN=/home/srayansh/Documents/Golang/Blackburn
cd "$BLACKBURN"
git submodule update --init --recursive
(cd xcross && dart pub get)
./scripts/blackburn-macbox.sh doctor
```

Doctor checks KVM read/write access, Docker daemon, GPU render-node access,
Vulkan inventory, disk space, Dart/Flutter/Swift/Clang/LLD, SSH/rsync and the patched
xcross CLI. It does not install host packages, change groups or modify sysctls.
`macbox doctor` provides additional VM-specific checks. This upstream xcross
checkout requires a compatible Swift toolchain (its Linux policy requires 6.4+)
and a matching extracted Apple SDK; merely finding `swift` does not prove SDK
compatibility.

The upstream macbox license disclaimer applies: Apple's macOS license restricts
installation to Apple hardware. An Intel Linux VM configuration is provided here
as engineering work; no claim of license authorization or measured performance
is made.

## Build and provision macbox

```bash
cd "$BLACKBURN"
./scripts/blackburn-macbox.sh setup-macbox
```

This builds the Docker image and pinned graphics/QEMU stack, creates the guest
only if missing, starts it, and prints `macbox next`. The first build can take
20–60 minutes. Use a separate terminal for progress. On a 16 GiB host, avoid a
Flutter compilation while building the graphics stack; 8 GiB is reserved for the
guest once started. Override exported `MACBOX_MEMORY=6G` if needed before creation.
Existing VM CPU/RAM settings are not silently rewritten.

A new VM still needs macOS installation, its user account, SSH bootstrap, Xcode
and a compatible **Intel** iOS Simulator runtime. Follow `macbox/docs/setup.md`
and the next-step output. Set the same config for direct upstream CLI commands:

```bash
export MACBOX_CONFIG="${XDG_CONFIG_HOME:-$HOME/.config}/macbox/blackburn.env"
source "$BLACKBURN/macbox/.env.blackburn"
export MACBOX_WORKSPACE="$BLACKBURN"
"$BLACKBURN/macbox/macbox" next
# After macOS installation and account setup:
"$BLACKBURN/macbox/macbox" ssh-bootstrap
ssh macbox true
"$BLACKBURN/macbox/macbox" provision
# Download Xcode yourself using your Apple account; substitute the actual path:
"$BLACKBURN/macbox/macbox" xcode /path/to/Xcode.xip
"$BLACKBURN/macbox/macbox" pool
ssh macbox 'xcrun simctl list runtimes; xcrun simctl list devices available'
```

macbox's GUI/user-password steps are documented upstream and are not automated by
this wrapper. `setup-macbox` is not an unattended Apple account or Xcode installer.
Do not expect an ARM-only runtime to execute an x86_64 app.

## Install the compilation SDK on Linux

```bash
"$BLACKBURN/scripts/blackburn-macbox.sh" extract-sdk /path/to/Xcode.xip
# An existing local Xcode.app directory is supported too:
"$BLACKBURN/scripts/blackburn-macbox.sh" extract-sdk /path/to/Xcode.app
# Alternatively, copy /Applications/Xcode.app from the configured SSH guest:
"$BLACKBURN/scripts/blackburn-macbox.sh" extract-sdk
```

The last command needs room for a temporary Xcode copy as well as the extracted
SDK. It uses `xcross sdk install` on Linux and deletes the temporary donor tree.
Simulator runtime installation remains a separate macOS operation.

## Build, run and hot reload

From a Flutter project's root (with `ios/` configured):

```bash
cd /path/to/flutter-app
"$BLACKBURN/scripts/blackburn-macbox.sh" build-sim
"$BLACKBURN/scripts/blackburn-macbox.sh" run-sim
# Pass ordinary Flutter/xcross flags through:
"$BLACKBURN/scripts/blackburn-macbox.sh" run-sim --dart-define=API_URL=https://example.test
```

`run-sim` claims/boots the upstream mac-sim pool device, passes its UDID to xcross,
and uses SSH alias `macbox`. It copies the app with rsync unless shared-workspace
mode is explicitly enabled. Keep the mac-sim pool claim while using the app; release
it afterward with `macbox/sims/bin/mac-sim down`. Native/plugin changes require a
rebuild and relaunch; Dart changes use `r` or `R`. No physical iPhone, Apple ID or
provisioning profile is needed for this Simulator launch path.

The raw patched CLI can also be used directly:

```bash
cd /path/to/flutter-app
export XCROSS_SIMULATOR_ARCH=x86_64
export XCROSS_SIMULATOR_HOST=macbox
dart run "$BLACKBURN/xcross/packages/xcross/bin/xcross.dart" flutter run \
  --target-platform simulator --udid YOUR_SIMULATOR_UDID
```

If `--udid` is omitted, one device must already be booted. A supplied UDID is
booted and awaited. The launcher uses SSH configuration for usernames, ports,
keys and host verification; it does not accept a password. The SSH process lives
only for the run session, and its local forward binds to 127.0.0.1.

## Shared bundles without copying

A Docker bind mount is not a macOS filesystem share. This implementation adds
both layers: the Linux `MACBOX_WORKSPACE` is bound at container `/workspace`, and
an SMB2+ share exposes it only inside the container/guest network. No host SMB
port is published. The share is writable so guest `codesign` can sign Linux-built
bundles before installation. Processes in the container and the guest can write
the selected workspace; choose its scope accordingly.

For a guest created with the new profile/network settings:

```bash
cd "$BLACKBURN"
./scripts/blackburn-macbox.sh mount-workspace
ssh macbox 'ls /Users/Shared/BlackburnWorkspace'
cd "$BLACKBURN/workspace/my-flutter-app"
BLACKBURN_SHARED=1 "$BLACKBURN/scripts/blackburn-macbox.sh" run-sim
```

The share is mounted by macOS's native `mount_smbfs` at
`/Users/Shared/BlackburnWorkspace`; the guest's sealed root is not modified.
The pinned QEMU passt backend maps 10.0.2.2 to container loopback, where Samba runs.
Mount again after guest reboot. A pre-existing container must be stopped/recreated
through `macbox down`/`up` to acquire the new mount/server; a pre-existing VM's
network `extra-args` must include the new passt `map-host-loopback` setting.
For an older installation, rsync mode works without any sharing changes.
Only projects within `MACBOX_WORKSPACE` use the mapping; other apps fall back to
rsync automatically. Build the `.app` completely before launching it.

## A remote Mac or ARM64 Simulator

```bash
# Configure an SSH alias my-mac in ~/.ssh/config first.
export XCROSS_SIMULATOR_HOST=my-mac
export XCROSS_SIMULATOR_ARCH=arm64  # Apple Silicon; use x86_64 for an Intel Mac
cd /path/to/flutter-app
"$BLACKBURN/scripts/blackburn-macbox.sh" run-sim --udid YOUR_REMOTE_UDID
```

The remote host needs Xcode, rsync, a compatible runtime and working noninteractive
SSH. mac-sim pool management is bypassed for other SSH hosts. On macOS itself,
unset `XCROSS_SIMULATOR_HOST` and invoke the raw xcross CLI to run local simctl.
Simulator Profile/Release builds remain rejected by the existing build policy.

## Validation and limits

Validation commands:

```bash
cd "$BLACKBURN/xcross"
dart analyze packages/darwin_sdk_kit packages/xcross
(cd packages/darwin_sdk_kit && dart test)
(cd packages/xcross && dart test test/simulator test/flutter \
  test/cli/flutter_command_args_test.dart test/cli/sdk_metadata_test.dart)
cd "$BLACKBURN"
bash -n scripts/blackburn-macbox.sh macbox/macbox macbox/container/build-reims.sh
./scripts/blackburn-macbox.sh doctor
```

The checked-in generated CLI and development version sources allow `dart run`
after `dart pub get`. If regenerating CLI annotations, run `dart run build_runner
build` from `xcross/packages/xcross`, then restore the checked-in
`lib/src/shared/runtime/version.g.dart` (build_runner removes that separately
managed release-identity file).

Automated tests cover architecture selection, existing ARM64/device behavior,
SDK metadata, native assets/SwiftPM selection, hot reload logic, VM-service URL
parsing and token preservation, and a mocked SSH transport exercising shared
staging, signing, installation, failed launch and termination cleanup. These are
not a guest hardware test.

Host doctor passed here with KVM, Docker, Intel Arc Meteor Lake (PCI 0x7d55),
Mesa ANV 26.2.6 Vulkan and approximately 128 GiB free. No macbox guest, Xcode SDK
extraction, real app compilation, Intel CoreSimulator boot, SMB mount, graphics
benchmark or live Dart reload was performed in this session. Those remain the
hardware acceptance steps after provisioning. In particular, no FPS or complete
SDK/runtime compatibility result is claimed.
