# Execution architectures and remaining compilation tools

Research date: 2026-11-04. Read-only research; no builds, firmware boots, Mac access, or graphics validation performed. External repository claims below are distinguished from source inspection and proposed engineering.

## 1. How can Linux compilation feed a remote Mac Simulator and retain hot reload?

### Takeaway
The highest-confidence architecture is Linux compilation plus the real MacBook Air's CoreSimulator, controlled over SSH and viewed through VNC. Flutter Dart reload can be driven from the Mac immediately; putting its incremental compiler and reload controller entirely on Linux requires a supported remote device adapter, rather than assuming a forwarded service URL makes stock Flutter's Linux iOS discovery work.

### Cited Findings
- Simulator is an Xcode developer tool that runs compiled apps on simulated devices. A simulator runtime is a separate OS package, rather than the SDK's headers and linker stubs. — [Apple Simulator](https://help.apple.com/xcode/mac/current/en.lproj/devfd9e28378.html); [Apple runtimes](https://developer.apple.com/documentation/safari-developer-tools/adding-additional-simulators).
- Apple documents `xcodebuild -downloadPlatform iOS [-buildVersion VERSION] [-architectureVariant universal|arm64]`. The default follows the Mac architecture and Rosetta destinations; its universal variant supports Intel and Apple silicon. `xcodebuild -runFirstLaunch` installs required components including simctl. This documentation is generic and does not prove universal variants exist for every latest runtime. — [Apple component management](https://developer.apple.com/documentation/xcode/downloading-and-installing-additional-xcode-components).
- Apple says macOS Tahoe 26 is the last release supporting Intel Macs. Therefore the user's MacBook Air model and OS compatibility must be inventoried before promising a newest-Xcode runtime. — [Apple 26.4 release notes](https://developer.apple.com/documentation/macos-release-notes/macos-26_4-release-notes); [Xcode supported OS versions](https://developer.apple.com/xcode/system-requirements).
- The inspected xcross CI helper executes `/usr/bin/xcrun simctl`, boots a runtime, waits for bootstatus and SpringBoard, installs the `.app`, launches with stdout/stderr files, and checks failures. This is an existing model for a Mac-side execution agent. — [xcross simulator_smoke.py](https://github.com/arxdeus/xcross/blob/main/.github/scripts/simulator_smoke.py).
- Apple Screen Sharing supports third-party VNC clients. Enabling “VNC viewers may control screen with password” allows conventional viewers to authenticate. — [Apple screen sharing](https://support.apple.com/en-ie/guide/mac-help/mh11848/27/mac/27).
- Xpra supports shadowing an existing macOS desktop with `xpra shadow ssh://USER@HOST/`. This is desktop capture/control; the display normally must be active and unlocked. Full macOS packages contain server support. It is not evidence that Simulator.app is an X11 program or can be run by ordinary `ssh -X`. — [Xpra shadow](https://github.com/Xpra-org/xpra/blob/master/docs/Usage/Shadow.md); [Xpra Mac packages](https://xpra-org.github.io/xpra/Download/MacOS.html).
- ScreenCaptureKit streams screen/audio content and CGEvent represents low-level input events. These provide components for a custom Mac capture/input bridge; Apple does not provide an automatic WebRTC Simulator bridge in these APIs. — [ScreenCaptureKit](https://developer.apple.com/documentation/screencapturekit); [CGEvent](https://developer.apple.com/documentation/coregraphics/cgevent).
- Flutter's attach command accepts `--debug-url` with the complete VM-service URI, including its authentication path. It creates a HotRunner for reload. Crucially `validateCommand()` and `runCommand()` still call `findTargetDevice()` even when the URL is supplied. A service tunnel does not itself create an iOS device in Linux Flutter's device manager. — [Flutter attach source](https://github.com/flutter/flutter/blob/master/packages/flutter_tools/lib/src/commands/attach.dart).
- Tailscale can carry ordinary SSH traffic, and SSH has forward/reverse TCP forwarding. Tailscale SSH is a separate optional authentication/server feature; macOS server availability differs by Tailscale distribution. Prefer macOS Remote Login/OpenSSH over the tailnet for the recipe below, rather than depending on Tailscale SSH-specific forwarding behavior. — [SSH over Tailscale](https://tailscale.com/docs/reference/ssh-over-tailscale); [Tailscale SSH prerequisites](https://tailscale.com/docs/features/tailscale-ssh).

### Inferences

**Recommended topology**: Linux editor, source, SDK preparation and native compiler -> `.app` transport -> Mac CoreSimulator. Independently carry (1) simctl control/logs over SSH, (2) screen/input over VNC or Xpra shadow, (3) Flutter VM service over a loopback SSH tunnel. This keeps UI latency separate from compiler speed.

**Architecture compatibility**: choose `arm64-apple-ios<minimum>-simulator` for Apple silicon Mac, `x86_64-apple-ios<minimum>-simulator` only for an Intel Mac with a matching supported runtime. The Linux host's x86_64 CPU does not force x86_64 output. Both ARM device and ARM Simulator are Mach-O but have different platform identities and framework slices; do not install a device IPA on CoreSimulator. Match plugin/XCFramework slices as well as the Runner executable. Match the runtime to SDK/deployment availability, and the Swift compiler version to interface syntax.

**Proposed inventory and deployment commands** (templates, not executed; replace placeholders; select an already available UDID to avoid unnecessary device creation):

```sh
# From Linux; mac-air is a configured SSH host or tailnet name.
ssh mac-air 'uname -m; sw_vers; xcodebuild -version; xcrun simctl list devices available; xcrun simctl list runtimes'
# On the Mac if its selected Xcode needs initial platform components:
ssh mac-air 'xcodebuild -runFirstLaunch'
# xcross build command/arch selection is documented in the companion repo notes.
# Preserve bundle structure and symlinks during transfer.
rsync -a /absolute/path/Runner.app/ mac-air:/Users/MACUSER/Blackburn/Runner.app/
ssh mac-air 'xcrun simctl boot SIMULATOR_UDID'
ssh mac-air 'xcrun simctl bootstatus SIMULATOR_UDID -b'
ssh mac-air 'xcrun simctl install SIMULATOR_UDID /Users/MACUSER/Blackburn/Runner.app'
ssh mac-air 'open -a Simulator'
ssh mac-air 'xcrun simctl launch --terminate-running-process SIMULATOR_UDID com.example.runner'
```

A boot already in progress/already booted can return a nonzero `boot` result; query status instead of erasing the simulator. Run the agent in the same logged-in Mac user session as the GUI. Screen Sharing and keep-awake configuration are separate deployment prerequisites; do not assume SSH creates an Aqua desktop session.

**GUI recipe**: enable Screen Sharing and Remote Login on Mac. On Linux, use an SSH local forward `ssh -N -L 127.0.0.1:15900:127.0.0.1:5900 mac-air`, then point a VNC viewer at localhost port 15900. Alternatively use `xpra shadow ssh://MACUSER@mac-air/` with a macOS package containing the server. A custom browser UI needs a Mac screen capture producer, encoder, WebRTC signaling/transport and input injection; merely adding WebRTC to Linux provides none of those Mac services. `simctl io screenshot` supports test artifacts, not a complete interactive touch/keyboard video protocol.

**Hot reload baseline**: synchronize sources and exact Flutter SDK revision to the Mac, launch the Linux-built Debug Simulator app, then run `flutter attach -d SIMULATOR_UDID --debug-url='http://127.0.0.1:MAC_VM_PORT/AUTH_TOKEN=/'` on the Mac through an interactive SSH terminal. Dart changes need source sync before reload; native plugin/Swift/ObjC changes require rebuilding/reinstalling. This baseline offloads incremental Dart compilation to the Mac, so it does not satisfy a strict “all compilation stays Linux” target.

**Strict Linux compiler/controller design**: add a RemoteSimulator target to xcross with device inventory, install/launch/log streaming over SSH, architecture metadata, fixed VM service endpoint discovery, and its frontend_server + VMService incremental reload path. Keep the same package URI/source mapping and engine/Dart revision. Existing xcross device launch is CoreDevice-oriented, so this is an adaptation, not a currently proven stock command.

**Correct tunnel direction and URI**: once a Debug app emits a reachable VM-service URI on the Mac, e.g. `http://127.0.0.1:54321/TOKEN=/`, from Linux run:

```sh
ssh -N -L 127.0.0.1:15432:127.0.0.1:54321 mac-air
# Linux debugger/adapter connects to:
# http://127.0.0.1:15432/TOKEN=/
```

The destination loopback is the **Mac** because it is evaluated by the SSH server. Keep the auth path, forward HTTP/WebSocket traffic transparently, and discover the actual listener port rather than assuming 12345. A listener bound only to `::1` needs that destination address instead. If only the Mac can initiate connections, run on Mac `ssh -N -R 127.0.0.1:15432:127.0.0.1:54321 linux-user@linux-host`; the remote forwarded port is then on Linux. A DDS endpoint may have its own port/auth URI: forward the chosen endpoint and avoid two conflicting DDS owners. No mDNS forwarding is needed when a known URI is used. Raw VM-service reload alone is insufficient: compile modified Dart to a compatible incremental kernel and synchronize it before `reloadSources`.

### Gaps
- MacBook Air CPU, macOS, RAM and exact compatible runtimes are unknown; this changes target and performance decisions. No independent proof of newest runtime x86_64 support was found. Apple's current generic universal-download page should not be read as a perpetual guarantee.
- No remote Mac was available to test screen sharing permissions, launch session, transport or hot reload.
- No measured VNC/WebRTC latency, compilation speed or laptop thermal/performance figures are available.

## 2. Can the Intel laptop execute iOS locally through macOS virtualization or runtime/emulation work?

### Takeaway
A local x86_64 macOS VM is a possible research machine, but its graphics and aging runtime compatibility make it a weaker first choice than the available physical Mac. A recent vphone-linux project now reports full graphical iOS on Linux x86_64: it is a significant experimental alternative, with only one tested high-end NVIDIA desktop and no evidence for the user's Intel Arc laptop or Flutter JIT/reload.

### Cited Findings
- OSX-KVM explicitly describes creating a virtual Hackintosh with QEMU/KVM/OpenCore. Its README states the basic setup lacks graphical acceleration and describes GPU passthrough separately. — [OSX-KVM limitations](https://github.com/kholia/OSX-KVM#setting-expectations-right).
- Docker-OSX wraps QEMU/KVM; its requirements include x86_64 KVM host and BIOS virtualization, and GPU acceleration remains a TODO in the inspected README. Containerization does not turn macOS or Simulator into Linux processes. — [Docker-OSX](https://github.com/sickcodes/Docker-OSX#capabilities).
- Apple's Tahoe SLA is explicitly for Apple-branded systems and limits virtualization permissions to qualifying Apple computers. Owning a donor Mac does not make a separate non-Apple laptop covered by that hardware condition. — [Apple macOS SLA](https://www.apple.com/legal/sla/docs/macOSTahoe.pdf).
- Darling currently describes a Mach-O loader, darlingserver implementing Mach IPC/POSIX/Darwin interfaces, reimplemented Apple frameworks, functional CLI tools, and GUI support still under development with an initial Vulkan-backed Metal layer. Its README says Xcode itself does not yet run. This is not a claim of working CoreSimulator or iOS runtime compatibility. Inspected upstream HEAD reported `c7f4d853ffd9faf70c49525d69b0a51fa46da226`, 2026-11-04. — [Darling README](https://github.com/darlinghq/darling/blob/c7f4d853ffd9faf70c49525d69b0a51fa46da226/README.md).
- Original qemu-t8030 was archived 2023-04-14. Its project checklist reports kernel/userspace/launchd/console shell and iOS 15 support, with framebuffer, multitouch, SEP and GPU unchecked. It is not a completed graphical Simulator implementation. The raw Markdown was inspected to preserve checked/unchecked state hidden by browser text extraction. — [qemu-t8030 status](https://github.com/TrungNguyen1909/qemu-t8030/wiki/Project-status); [raw checklist](https://raw.githubusercontent.com/wiki/TrungNguyen1909/qemu-t8030/Project-status.md).
- vphone-cli requires a physical Apple silicon Mac with macOS 15+ and explicitly does not work inside a macOS VM. It uses Virtualization.framework/PCC research VMs to boot iOS, not CoreSimulator. — [vphone-cli](https://github.com/Lakr233/vphone-cli).
- **Upstream claim, not reproduced**: vphone-linux commit `c2dc1996c8398ddb994288a29a719360f354d268` (2026-11-03) reports graphical iOS on Linux x86_64, based on a `vresearch101` QEMU machine, ARM64e TCG, reims-vgpu and metal2vulkan. The sole end-to-end tested PC is an i7-14700K/62 GB/RTX 4080 SUPER; Intel GPUs are untested. README specifies 16 GB+ RAM, about 100 GB free disk, Vulkan GPU, root setup steps; networking/audio remain untested. It quotes earlier 19–20 s cold/4–8 s warm app launches; these are project measurements, not a laptop benchmark. — [pinned vphone-linux README](https://github.com/YahiaAngelo/vphone-linux/blob/c2dc1996c8398ddb994288a29a719360f354d268/README.md).
- vphone-linux documents `xtool dev build --ipa` and `tools/install-ipa.py ... --launch ... --cid 3`; vphoned uploads and signs within the guest. Its documented edit loop replaces and relaunches the app. This is a **device-platform** app, not an iphonesimulator `.app`, and it does not demonstrate Flutter hot reload. — [vphone-linux development](https://github.com/YahiaAngelo/vphone-linux/blob/c2dc1996c8398ddb994288a29a719360f354d268/docs/APP-DEVELOPMENT.md).

### Inferences
- For the Intel Core Ultra laptop, x86_64 macOS KVM can execute same-ISA guest CPU instructions with virtualization; this is distinct from emulating ARM iOS. Twenty-two host threads do not solve missing accelerated graphics, Apple runtime compatibility, memory, disk or firmware issues. A GUI desktop rendering slowly does not prove modern CoreSimulator Metal workloads will work.
- Do not promise modern Intel Arc passthrough into macOS: this needs a functioning guest driver, IOMMU isolation and host display arrangements, not just VT-d. None were established for this laptop. Treat software rendering or basic virtual display as an experiment, with real Simulator boot/render as the acceptance test.
- Darling's engineering backlog for Simulator includes dyld/cache and platform behavior, Mach task/port/VM and exceptions, XPC and launchd_sim bootstrap namespaces, CoreSimulator services, UIKit/SpringBoard/backboardd, IOSurface/CoreAnimation compositor, graphics driver and Metal semantics, input, entitlement/code-sign checks, pthread/workqueue and shared-memory behavior. A Mach-O loader is only the first component. Some functions exist in Darling; the claim is missing end-to-end compatibility, not that every listed API is completely absent.
- QEMU user emulation only translating instruction sets cannot create Darwin kernel services or an iPhone SoC. Full-system emulation must model the boot chain, MMU/PAC/other CPU behavior, interrupt controller, storage, display/input, graphics and guest security expectations. qemu-t8030, vphone and Darling solve different layers.
- vphone-linux is concrete enough to warrant a separate prototype: reproduce pinned build on disposable guest disk; confirm Intel Vulkan capabilities and usable graphics; install a trivial arm64 device app; then test Flutter Release first. Debug requires explicit validation of code signing/JIT/mprotect/debug entitlement behavior and a usable VM-service transport. Networking being untested is material for debugger forwarding. Do not infer JIT support merely because a patched guest runs Swift apps.
- vphone-cli on an Apple silicon Air is another research backend, but real CoreSimulator is easier for ordinary Flutter testing. vphone can run device binaries with patched iOS behavior; Simulator exercises a different platform. Neither validates sensors, radio hardware, device timing or App Store service behavior completely.

### Gaps
- No iOS execution was attempted, and there is no evidence for vphone-linux on Core Ultra 7 155H/Intel Arc, working Flutter engine rendering, Debug JIT, KMP graphics, networking or debugger attach.
- No exact macOS guest version/runtime/graphics combination is validated on the laptop. The latest supported Intel macOS releases increasingly restrict the VM path.
- vphone-linux's README phrase “no Apple code at run time” must not be repeated literally as a claim about the whole system: its guest necessarily uses Apple firmware/OS downloaded from IPSWs; the reasonable interpretation is no host Virtualization.framework implementation dependency.

## 3. What actually remains for native resource compilation and tool replacement?

### Takeaway
Native Swift cross-compilation is demonstrably broader than Flutter/KMP, but source compilation does not imply arbitrary Xcode-project compatibility. Reduce reliance on proprietary resource compilers first; use the donor Mac as an output oracle while incrementally reimplementing formats and tool contracts. `gen_snapshot` is open-source Dart infrastructure and must not be classified as an Apple proprietary tool.

### Cited Findings
- xtool advertises SwiftPM package -> iOS app, signing/install and programmatic Apple Developer Services. Inspected clone HEAD was `2af0b67447d30063235e2adb7bdc77012f431d3a`, committed 2026-11-02. — [xtool README](https://github.com/xtool-org/xtool/blob/2af0b67447d30063235e2adb7bdc77012f431d3a/README.md).
- Its SDK builder configures arm64 device plus arm64/x86_64 Simulator and macOS triples; downloads a Linux LLVM Darwin toolset; uses an ld64.lld wrapper mapping unsupported `-r` partial links to a static archive; patches Swift resource paths, linker choice and build-task `CODESIGN=true`, with a comment stating signing occurs post-build. It notes SwiftBuild toolset path fixes require Swift 6.4+ rather than 6.3. — [xtool SDKBuilder.swift](https://github.com/xtool-org/xtool/blob/2af0b67447d30063235e2adb7bdc77012f431d3a/Sources/XToolSupport/SDKBuilder.swift).
- xtool's signer imports RCodesign and invokes signing with identity, entitlements and shallow/deep mode. This is concrete cross-platform signing, not reliance on `/usr/bin/codesign` completing SwiftBuild's tasks. — [RCodesignSigner.swift](https://github.com/xtool-org/xtool/blob/2af0b67447d30063235e2adb7bdc77012f431d3a/Sources/XKit/Signer/RCodesignSigner.swift); [swift-rcodesign](https://github.com/xtool-org/swift-rcodesign).
- Search of inspected xtool `Sources/`, docs and manifest found no actual actool/ibtool/metal compiler implementation; do not claim xtool reimplements those tools. Existing source is evidence for Swift build orchestration, SDK surgery and signing, not universal asset compilation. — [xtool Sources](https://github.com/xtool-org/xtool/tree/2af0b67447d30063235e2adb7bdc77012f431d3a/Sources).
- Apple publishes Windows-hosted Metal development tools for building iOS/macOS/tvOS shader programs/assets. Separately, Metal Shader Converter accepts DXIL/LLVM shader IR and produces metallib; it is available for macOS and Windows. That is not a native Linux MSL compiler and not identical to the metal frontend. — [Apple Metal tools](https://developer.apple.com/metal/tools/); [Metal Shader Converter](https://developer.apple.com/metal/shader-converter/).
- Dart gen_snapshot source is publicly available under the Dart SDK's license; xcross's resolver fetches engine-matched Linux-built iOS AOT compilers from xcross_gen_snapshot. Engine revision and snapshot compatibility, rather than proprietary Apple source access, are the constraints. — [Dart gen_snapshot](https://github.com/dart-lang/sdk/blob/main/runtime/bin/gen_snapshot.cc); [xcross resolver](https://github.com/arxdeus/xcross/blob/main/packages/xcross/lib/src/shared/flutter/gen_snapshot/ios_gen_snapshot_resolver.dart).

### Inferences

**Bounded replacement matrix / roadmap**:

| Step | Concrete native-Linux strategy | Remaining verification |
|---|---|---|
| Swift/ObjC/C/C++ | SDK patches + upstream compiler/Clang/LLVM ld64.lld; xtool for SwiftPM; xcross build model for Flutter/KMP | Apple Swift interface compatibility, plugins/macros, project graph/build scripts and tool version combinations |
| actool / Assets.car | Initially ship ordinary PNG/data/font bundle resources, generate app-icon plist/loose icons, constrain named asset usage. Reimplement asset catalog JSON ingestion, scale/idiom/appearance selection and compiled CAR representation in small supported increments. | Asset variant semantics, colors, vector/PDF rasterization, compression, app icons and platform validation. Inspect actual xcross asset implementation before deciding which subset already exists. |
| ibtool / storyboard/XIB | Build native UI programmatically or in SwiftUI; generate minimal launch configuration where supported. Treat arbitrary archived nib/storyboard objects as a separate compatibility goal. | Object archive schema, UIKit classes, constraints, connections, localization and OS evolution. Linux XML parsing alone does not compile an Interface Builder archive. |
| metal / metallib | Ship known-good precompiled libraries or compile source at runtime where API/runtime supports it. Use Mac shader-build oracle/cached artifacts initially. Evaluate Apple's Windows tools under Wine only as separate compatibility work. | Wine usage is not a native Linux tool replacement; MSL frontend/Metal AIR/container/target semantics are substantial work. metal2vulkan's guest Metal -> host Vulkan path is not a compiler for delivering Apple-device metallib. |
| codesign | Use existing cross-platform signing implementation, generate Mach-O signatures, bundle resource seals, entitlements and profile embedding correctly. | Nested bundle signing, executable changes invalidate signatures; real devices/App Store still require appropriate Apple-issued identities/profiles/services. Simulator or a patched guest does not remove those distribution requirements. |
| gen_snapshot | Rebuild open Dart source for Linux host / iOS AOT target at the exact engine revision; use xcross published matched binaries where available. | Snapshot version/hash, target ABI, release/profile mode and Flutter engine compatibility; no Apple reverse-engineering required. |
| Xcode project executor | Parse targets/configurations, build phases, xcconfig expansion, resources, dependencies, scripts and embed rules; SwiftPM route is a smaller starting scope. | Arbitrary projects can invoke macOS-only executables or build plugins. Availability of a compiler does not imply xcodebuild parity. |

**Oracle workflow**: on the donor Mac collect exact command lines, tool versions and output metadata for tiny fixtures. Build differential fixtures per target platform/deployment level, inspect bundle file trees/plists/Mach-O commands/resource structures, run them through Mac CoreSimulator and compare results. Grow a documented supported subset; keep unsupported resource features explicit. Cache Apple-produced resource outputs for hybrid workflows but label those builds hybrid rather than “100% Linux compilation.”

### Gaps
- No complete stand-alone open Linux actool/ibtool/metal replacement was established in this research. A specific xtool issue mentioning an actool wrapper is not proof of a compiler implementation.
- No legal interpretation of SDK redistribution/extraction is offered; a donor Mac does not by itself establish permission to redistribute Apple SDKs. The concise sourced virtualization hardware condition above is the only licensing conclusion required for architecture comparison.
