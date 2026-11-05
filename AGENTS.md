# Blackburn — iOS Development on Non-Mac (Intel Linux) Research & Engineering

## 1. Mission & Problem Statement
Our goal is to make full **iOS app development (compilation, linking, signing, running/simulating, and hot-reload/debugging)** possible on a **non-Mac Intel Linux laptop** (`x86_64`, Intel Core Ultra 7 155H), specifically addressing the constraint:
- **No physical iPhone / iOS device available** ("I do not have an Apple device").
- **Primary machine**: Intel (`x86_64`) Linux laptop.
- **Secondary resource available**: A MacBook Air ("mac air desktop") that can be used as a donor for SDKs/simulator runtimes, a remote execution/simulator node, or a reference system for reverse-engineering Apple compilation & runtime pipelines.
- **Reference Project**: [`arxdeus/xcross`](https://github.com/arxdeus/xcross) — an open-source Dart tool that reimplements the Flutter/Compose iOS build pipeline, Darwin SDK extraction (`Xcode.xip`), and CoreDevice deployment natively on Linux and Windows.

---

## 2. Core Research Questions
1. **What does `arxdeus/xcross` already solve on an Intel (`x86_64`) Linux laptop?**
   - How does `darwin_sdk_kit` extract `Xcode.xip` (`xar` / `pbzx` / `cpio`) and patch the Darwin SDK for Linux `swiftc` + `clang` + `ld64.lld`?
   - How does `xcross` build Debug (JIT) and Release/Profile (AOT via `xcross_gen_snapshot`) iOS binaries on `x86_64` Linux without macOS?
   - Can `xcross` build pure Swift / UIKit / SwiftUI apps, or only Flutter & Compose Multiplatform (KMP), and how hard is it to generalize its compilation pipeline to native Swift/ObjC Xcode projects?
2. **How does `xcross` handle iOS Simulator builds, and how can we run/test iOS apps without a physical iPhone?**
   - `xcross` claims: *"xcross builds debug (JIT) device and simulator apps"*. How does `xcross` build simulator `.app` bundles (`x86_64-apple-ios-simulator` vs `arm64-apple-ios-simulator`), and where does it run them?
   - Since we **do not have a physical iPhone**, how can we run and interact with the compiled iOS `.app`?
     - **Path A (Hybrid Local Compile + Remote Mac Air Simulator / Stream)**: Compile 100% locally on the Intel Linux laptop using `xcross` (or custom LLVM/Swift Darwin toolchain), and offload/stream the iOS Simulator from the MacBook Air (or vice versa), forwarding the Dart VM Service port over SSH for Linux-driven hot reload.
     - **Path B (Local GPU-Accelerated macOS / Simulator VM on Intel Linux — `macbox`)**: Run a container/KVM macOS 26 Tahoe (`x86_64`) guest on the 22-thread Intel Core Ultra 7 155H laptop using [`TUNA-NOPE/macbox`](https://github.com/TUNA-NOPE/macbox) with [`reims-vgpu`](https://github.com/steelbrain/reims-vgpu) + [`metal2vulkan`](https://github.com/steelbrain/metal2vulkan) on the Intel iGPU (achieving 60–96 FPS in Apple's real iOS Simulator), driving `simctl` (`mac-sim`) and compilation from Linux.
     - **Path C (Direct QEMU iPhone Emulation on Linux — `Inferno` vs `vphone-linux`)**:
       - **Gen 1 (`t8030` / iOS 14 — CPU software rendering)**: [`ChefKissInc/Inferno`](https://github.com/ChefKissInc/Inferno), [`raafatabualazm/ios-linux-emulator`](https://github.com/raafatabualazm/ios-linux-emulator), and [`JacobFV/behavioral-iphone-inferno`](https://github.com/JacobFV/behavioral-iphone-inferno) (iPhone 11 / iOS 14.0 beta 5 on QEMU TCG; slow 15–67x CPU rendering, no Metal GPU, iOS 14 vs iOS 17+ CoreDevice gap).
       - **Gen 2 (`vresearch101` / iOS 26.6–27.0 — Vulkan Metal GPU)**: [`YahiaAngelo/vphone-linux`](https://github.com/YahiaAngelo/vphone-linux) (fork of `ChefKissInc/QEMUAppleSilicon` booting iPhone 16 Pro / iOS 26.6–27.0 on `x86_64` Linux with `reims-vgpu` + `metal2vulkan` at 60+ FPS, and `vphoned` over `virtio-vsock` CID 3:1339 for 3-second `.ipa` install/signing/launch without Apple ID or CoreDevice).
       - **Barebones Darwin / Syscall Translation**: [`jprx/darwin-vm`](https://github.com/jprx/darwin-vm) (CLI-only Darwin root shell) and [`darlinghq/darling`](https://github.com/darlinghq/darling) (lacks `launchd_sim`, `backboardd`, `SpringBoard`, and `Metal`/`IOSurface` required for UIKit/Flutter).
3. **Reverse Engineering Apple Compilation for Intel Linux**:
   - Which parts of Apple's compilation toolchain are already open-source or cross-compilable to `x86_64-unknown-linux-gnu` (`clang`, `swiftc`, `ld64.lld`, `llvm-lipo`, `xcrun` shims, `.tbd` stub linking)?
   - Which parts are proprietary Apple binaries inside `Xcode.xip` (`actool` for Asset Catalogs, `ibtool` for Storyboards/XIBs, `momc` for CoreData, `metal` compiler for `.metallib`), and how does `xcross` / `xtool` / `behavioral-iphone-inferno` replace or bypass them on Linux?

---

## 3. Agent Roles & Workflow
- **Lead Architect (Antigravity)**:
  - Coordinates research, synthesizes findings, maintains `AGENTS.md`, designs architecture options, and brainstorms directly with the User.
- **Research & Reverse-Engineering Worker (`codex` CLI — `gpt-6.1-sol`)**:
  - Invoked via `codex exec --dangerously-bypass-hook-trust --skip-git-repo-check ...`
  - Used to deep-dive into repository source code (`arxdeus/xcross`, `xtool-org/xtool`, `darlinghq/darling`, `vphone-linux`, `macbox`, `Inferno`, etc.), inspect compiler flags, Mach-O linker behavior, simulator runtime internals, and produce structured technical reports.

---

## 4. Deliverables & Research Reports
1. **Master Technical Report (Architecture & Compilation)**: [`reports/iOS compilation and simulator architectures.md`](file:///home/srayansh/Documents/Golang/Blackburn/reports/iOS%20compilation%20and%20simulator%20architectures.md) (and [`codex_research_report.md`](file:///home/srayansh/Documents/Golang/Blackburn/codex_research_report.md)).
2. **Master Technical Report (Headless Decoupling & IDE Integration)**: [`reports/Headless macbox and IDE integration.md`](file:///home/srayansh/Documents/Golang/Blackburn/reports/Headless%20macbox%20and%20IDE%20integration.md).
3. **Detailed Sub-Reports**:
   - **SDK Extraction & `.tbd` / Toolchain Patching**: [`research_notes/iOS compilation and simulator architectures/sdk.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/iOS%20compilation%20and%20simulator%20architectures/sdk.md)
   - **Compilation Pipeline & Proprietary Tool Gaps (`xcross`, `gen_snapshot`, `actool`, `ibtool`, `codesign`)**: [`research_notes/iOS compilation and simulator architectures/builds.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/iOS%20compilation%20and%20simulator%20architectures/builds.md)
   - **Execution Without an iPhone (`macbox`, `vphone-linux`, `Inferno`, Remote Mac Air)**: [`research_notes/iOS compilation and simulator architectures/execution.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/iOS%20compilation%20and%20simulator%20architectures/execution.md)
   - **Compositor Decoupling (Direct DRM/KMS & Headless `REIMS_VGPU_WINDOW=0`)**: [`research_notes/Headless macbox and IDE integration/headless.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/Headless%20macbox%20and%20IDE%20integration/headless.md)
   - **IDE & DAP Debugger Integration (F5, Hot Reload, Bridge)**: [`research_notes/Headless macbox and IDE integration/bridge.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/Headless%20macbox%20and%20IDE%20integration/bridge.md) and [`examples/xcross-simulator-dap.patch`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/Headless%20macbox%20and%20IDE%20integration/examples/xcross-simulator-dap.patch)
3. **Master Technical Report (Meteor Lake & Simulator Smoothness Optimization)**: [`reports/Macbox Meteor Lake performance.md`](file:///home/srayansh/Documents/Golang/Blackburn/reports/Macbox%20Meteor%20Lake%20performance.md).
   - **GPU & Vulkan In-Place Rendering (`host_rt_in_place=1`, Mesa 2G cache, PCI 7d55)**: [`research_notes/Macbox Meteor Lake performance/gpu.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/Macbox%20Meteor%20Lake%20performance/gpu.md)
   - **macOS Guest Tuning & Idle Animation Latency (Reims Issue 141, Reduce Motion/Transparency)**: [`research_notes/Macbox Meteor Lake performance/guest.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/Macbox%20Meteor%20Lake%20performance/guest.md)
   - **iOS Simulator 1x vs 3x Scale & Daemon Slimming (`simslim`, agent-device, QMP sleeps)**: [`research_notes/Macbox Meteor Lake performance/simulator.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/Macbox%20Meteor%20Lake%20performance/simulator.md)
   - **Host & Guest Performance Audit Scripts**: [`scripts/blackburn-performance-audit.sh`](file:///home/srayansh/Documents/Golang/Blackburn/scripts/blackburn-performance-audit.sh) and [`research_notes/Macbox Meteor Lake performance/guest-audit.sh`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/Macbox%20Meteor%20Lake%20performance/guest-audit.sh)
4. **Master Technical Report (Apple Signing, Keychain & Xcode Config without GUI)**: [`reports/Apple signing keychain and xcode config without GUI.md`](file:///home/srayansh/Documents/Golang/Blackburn/reports/Apple%20signing%20keychain%20and%20xcode%20config%20without%20GUI.md).
   - **Signing & Keychain CLI (`security`, `.p12`, fastlane match/cert/sigh, API keys)**: [`research_notes/Apple signing keychain and xcode config/signing-and-keychain.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/Apple%20signing%20keychain%20and%20xcode%20config/signing-and-keychain.md)
   - **Xcode Project Configuration (XcodeGen, Tuist, `.xcconfig`, PlistBuddy, SweetPad)**: [`research_notes/Apple signing keychain and xcode config/project-configuration.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/Apple%20signing%20keychain%20and%20xcode%20config/project-configuration.md)
   - **Operational Lifecycle (On-Demand GUI vs Headless, Window close traps)**: [`research_notes/Apple signing keychain and xcode config/operational-lifecycle.md`](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/Apple%20signing%20keychain%20and%20xcode%20config/operational-lifecycle.md)
5. **BYTEPAD Flutter Execution & Metal2Vulkan Patch**: [`bytepad_verification_report.md`](file:///home/srayansh/.gemini/antigravity/brain/dc8edf1c-e0c0-4177-8dc0-1756acfebf93/bytepad_verification_report.md).
   - **Metal2Vulkan Flutter Impeller Patch**: [`macbox/patches/metal2vulkan/0006-accept-Flutter-shader-debug-module-flags.patch`](file:///home/srayansh/Documents/Golang/Blackburn/macbox/patches/metal2vulkan/0006-accept-Flutter-shader-debug-module-flags.patch)
   - **APFS Metal Driver Prewarm Script**: [`scripts/blackburn-prewarm-metal.sh`](file:///home/srayansh/Documents/Golang/Blackburn/scripts/blackburn-prewarm-metal.sh)
   - **Verified Simulator Screenshot**: [`bytepad_success.png`](file:///home/srayansh/.gemini/antigravity/brain/dc8edf1c-e0c0-4177-8dc0-1756acfebf93/bytepad_success.png)
6. **Master Operational Manual**: [`docs/BLACKBURN_IOS_DEVELOPMENT_MANUAL.md`](file:///home/srayansh/Documents/Golang/Blackburn/docs/BLACKBURN_IOS_DEVELOPMENT_MANUAL.md) — 688 lines, comprehensive end-to-end runbook, issue root causes, architecture, commands, and RAM optimization.
7. **Multi-Mode Developer Kit Architecture & Implementation Plan**: [`docs/BLACKBURN_MULTI_MODE_KIT_PLAN.md`](file:///home/srayansh/Documents/Golang/Blackburn/docs/BLACKBURN_MULTI_MODE_KIT_PLAN.md) — 646 lines, product architecture, 5 operational presets, unified `blackburn` CLI, decoupled viewers, RAM quotas, and phased roadmap.




