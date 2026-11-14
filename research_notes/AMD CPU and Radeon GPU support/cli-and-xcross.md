# Blackburn CLI, hardware inventory and xcross on AMD

## What currently assumes Intel, and what must change?

### Takeaway
Blackburn onboarding/import has three independent Intel-only gates; upstream-facing macbox itself already recognizes AMD SVM and Radeon. Linux x86_64 is the correct architecture on both vendors. Removing the gates alone does not qualify a particular QEMU CPU model, OpenCore configuration, RADV device or the local GPU patches.

### Cited Findings
Local source references below are relative to the repository root and line numbers refer to the inspected checkout, 2026-11-15. Findings are source inspection, not hardware execution.

| Source | Classification | Finding / reach |
|---|---|---|
| [internal/cli/appliance.go:27](../../internal/cli/appliance.go#L27) | Hard vendor gate | `prerequisites()` tests linux/amd64, then searches real `/proc/cpuinfo` for `GenuineIntel`; AMD rejected at lines 38–39 before KVM open/ioctl. `amd64` is an architecture name, not a vendor restriction. |
| [internal/cli/appliance.go:54](../../internal/cli/appliance.go#L54) | Hard GPU/driver gate | Requires nonempty Intel-only GPU probe; globally matches `driverName = Intel open-source Mesa driver` or `anv` in `vulkaninfo --summary`. RADV fails. No minimum Mesa version is enforced. |
| [internal/hardware/hardware.go:128](../../internal/hardware/hardware.go#L128) | Intel topology assumption | Uses PMU `cpu_core/cpus` / `cpu_atom/cpus`, then unqualified numeric `topology/core_type` 2→P, 1→E. Lower-frequency E cores labeled LP-E (inferred). |
| [internal/hardware/hardware.go:174](../../internal/hardware/hardware.go#L174) | Existing safe fallback | With no P identification, CPUSet empty, min(8, allowed online logical CPUs), warning; scheduler controls placement. It already respects `/proc/self/status` cpuset and online IDs. |
| [internal/hardware/hardware.go:197](../../internal/hardware/hardware.go#L197) | Hard PCI filter | Class 0x03 devices only, vendor must equal 0x8086. Six Intel PCI IDs labeled; unknown Intel remains recognized. AMD 0x1002 never appears in Report.GPUs. Driver field is kernel symlink basename, e.g. amdgpu, not RADV Vulkan driver. |
| [internal/hardware/hardware.go:13](../../internal/hardware/hardware.go#L13) | Missing topology metadata | CPU fields ID, Kind, MaxKHz, Package, Core only; no vendor, SMT mask, die, LLC, NUMA, CPPC ranking. GPU fields PCI, Device, Family, Driver only, no vendor/render node/Vulkan identity. Both constructors in Probe are positional. |
| [internal/cli/appliance.go:70](../../internal/cli/appliance.go#L70) | Shared tuner | `optimized()` replaces CPUSet and local macbox RAM/vCPU counts; sets 2G shader cache. Used by init and appliance import. |
| [internal/cli/appliance.go:99](../../internal/cli/appliance.go#L99) | Caller reach | init invokes prerequisites; appliance import at line 215 invokes it; export does not. Launch uses `probeHardware`, not prerequisites (cli.go:514), so there is no equivalent CPU vendor gate on every launch. |
| [internal/cli/cli.go:520](../../internal/cli/cli.go#L520) | Vendor-neutral reuse | Validates configured affinity CPUs against allowed online probe CPUs before applyProfile/start; passes `MACBOX_CPUSET`. Keep this validation. |
| [internal/cli/cli.go:605](../../internal/cli/cli.go#L605) | Vendor-neutral reuse | `applyProfile` preserves unrelated extra_env/provider flags, sets MESA_SHADER_CACHE_MAX_SIZE and viewer state. Existing shader cache knob works for RADV without a new dependency. |
| [internal/cli/appliance.go:260](../../internal/cli/appliance.go#L260) | Import portability gap | Rewrites name/cpus/memory and resets extra_env to cache + window/close policy. Imported QEMU CPU model and q35/OpenCore files remain donor ones. AMD kernel-patch core counts would need updating if that alternative route is chosen. |
| [internal/appliance/archive.go:32](../../internal/appliance/archive.go#L32) | Architecture-only | Manifest architecture x86_64, not Intel; no format change necessary for identical guest ISA. q35 OpenCore/code/vars files exported as self-contained references. No CPU compatibility metadata exists. |
| [internal/config/config.go:35](../../internal/config/config.go#L35) | Config reach | CPUSet plus ShaderCache, no selected GPU PCI/Vulkan record. Normalize/Validate supports empty or 2G cache; no need change cache syntax to support AMD. |
| [internal/cli/appliance_test.go:16](../../internal/cli/appliance_test.go#L16) | Test gap | `portableApp` injects ProbeHardware, which skips real CPU/KVM gate; fake Vulkan is Intel Mesa25.3. Passing fixture tests cannot establish AMD boot/RADV support. |
| [internal/hardware/hardware_test.go:39](../../internal/hardware/hardware_test.go#L39) | Fixture reach | Meteor Lake mask/PCI/battery and generic numeric core_type fallback only; no AMD cpuset/Radeon/multi-GPU cases. |
| [macbox/macbox:58](../../macbox/macbox#L58) | Partial AMD support | Passes KVM and selected MACBOX_RENDER_NODE to container, conditionally adds `/dev/udmabuf` only if it exists. Missing udmabuf not a startup error here. |
| [macbox/macbox:140](../../macbox/macbox#L140) | Guard defect / tuning | gpu_is_arl compares device ID without vendor; runtime metal2vulkan patch additionally guards Intel vendor AND ANV driver. Fix shell vendor check too. |
| [macbox/macbox:146](../../macbox/macbox#L146) | Qualification risk | Auto tuning enabled by macbox build identity, irrespective of AMD GPU. Creates host_rt_in_place=1 live.conf and M2V_LIVE; M2V_ARL_FACTS empty for other devices. Qualify AMD portable path separately; initial baseline use tuning off. |
| [macbox/macbox:251](../../macbox/macbox#L251) | Already vendor-neutral CPU | Doctor accepts vmx OR svm, requires AVX2, advises kvm_intel or kvm_amd; AMD GPU recognized with udmabuf permission warning, NVIDIA recognized but no driver in image. |
| [macbox/docker/Dockerfile:3](../../macbox/docker/Dockerfile#L3) | Already Mesa multi-driver | Ubuntu26.10 image installs mesa-vulkan-drivers, vulkan-tools, mesa-va-drivers alongside Intel media driver. Presence of Intel media package is not an AMD gate. |
| [macbox/.env.blackburn:2](../../macbox/.env.blackburn#L2) | Laptop-specific sizing | 16 vCPU /8G defaults; MACBOX_BUILD_JOBS22, MAKEFLAGS-j22, CMAKE/CARGO/NINJA22. Override for Ryzen laptop, do not infer CCD or host capacity from these constants. |
| [scripts/blackburn-macbox.sh:7](../../scripts/blackburn-macbox.sh#L7) | Correct architecture default | Explicit x86_64 simulator and remote Macbox SSH. Doctor checks tools, Vulkan startup, disk and xcross flutter doctor; no vendor check. |
| [scripts/repair-macbox-xcode.sh](../../scripts/repair-macbox-xcode.sh) | Architecture-only prose | Intel payload refers to x86_64 Mach-O slices; still needed on AMD-backed Intel macOS guest. |
| [macbox/guest/macbox-guest](../../macbox/guest/macbox-guest) | Architecture-only prose | darwin-amd64 Go/Homebrew Ruby/universal Xcode and hub_is_intel are correct guest x86_64 artifacts, not vendor restrictions. |
| [internal/cli/sim.go:192](../../internal/cli/sim.go#L192) | Architecture-only | macho.CpuAmd64 validates x86_64 simulator payload; no change for AMD. |

Inventory method: recursive case-insensitive `rg` across internal/, scripts/, macbox/macbox, macbox/container, macbox/patches, xcross/packages and then all text source excluding lockfiles/binary artifacts for GenuineIntel, AuthenticAMD, Intel/AMD, 8086/1002, cpu_core/cpu_atom/core_type, ANV/RADV, udmabuf, M2V_ARL, kvm_intel/kvm_amd. Verification logs/docs contain Intel measurements, not additional executable gates. Read source around hits and shared callers. Paths under vendor/build artifacts were not exhaustively line-read.

### Inferences

Minimal complete first implementation (proposed, not applied):

1. `hardware.go`: read vendor once from proc/cpuinfo; recognize Radeon class0x03 vendor0x1002 with `Family: "AMD Radeon (unclassified)"` instead of adding an unverified RDNA PCI table. Add GPU Vendor and render-node identity (and Report CPUVendor only if consumed by prerequisite validation). Convert local positional constructors to keyed before adding fields. Keep existing Intel map unchanged, especially 0x7d45 Meteor Lake.
2. Keep AMD initial CPU affinity empty and capped at 8 allowed logical vCPUs. Gate Intel PMU/raw-core-type heuristics to Intel until tested normalized kernel interfaces are used. Change warning to vendor-neutral `preferred CPU placement unavailable; affinity left to the scheduler`. Do not label all unknown Zen cores P, then accidentally pin a whole Threadripper/EPYC package.
3. `appliance.go`: rename arch error to `x86_64 Linux required`; accept exact cpu vendor `GenuineIntel` or `AuthenticAMD`; keep KVM API12 ioctl and memory validation. Add explicit AVX2 requirement matching macbox. Checking for SVM/VMX is diagnostic; accessible working KVM is more decisive than a module-name string.

Suggested vendor check, with testable report/proc parsing rather than a hidden real-host-only branch:

```go
switch r.CPUVendor {
case "GenuineIntel", "AuthenticAMD":
    // Supported host ISA vendors; guest CPU model qualification is separate.
default:
    return r, fmt.Errorf("unsupported x86_64 CPU vendor %q", r.CPUVendor)
}
```

4. Replace global ANV regex with selected-GPU validation. Obtain render-node's `/sys/class/drm/renderD*/device` PCI/vendor and map it to structured Vulkan physical-device PCI identity/DRM render-node metadata. Require selected AMD device RADV (driverID MESA_RADV, driverName radv), selected Intel ANV; reject llvmpipe/software fallback. Run equivalent check inside container because actual Vulkan userspace is container Mesa, not host Mesa. Global regex accepting `(Intel...|anv|radv)` is a useful small prototype but is NOT a complete fix on hybrid laptops: it may approve unrelated GPU while actual selected node fails.
5. For selected Radeon, require usable `/dev/udmabuf` before launching and ensure Docker device pass-through; kernel driver should normally be amdgpu. Do not confuse amdgpu (kernel) with radv (Vulkan). Checking node perms alone is necessary but import extension/probe remains required.
6. `macbox`: include PCI vendor0x8086 in gpu_is_arl; initialize Radeon with tuning off for baseline, empty M2V_ARL_FACTS; retain 2G common Mesa cache. Never carry Intel measured fact names into RADV. No need add RADV_PERFTEST for base support.
7. Choose/qualify QEMU AMD-safe CPU baseline in provider and retain CPU settings during appliance import/profile changes; do not add arbitrary `-cpu` extra_args when archive References only allows a narrow safe argument set. Current CPU/OpenCore fields may suffice for spoof route; manifest schema need not change for base AMD enablement, but document imported image requirements. If persisted ABI metadata is added later it must validate before replacing recipient files.

Second implementation, only after baseline hardware proof: collect topology from `thread_siblings_list`, package/die IDs, highest-level cache `level`/`type`/`shared_cpu_list` (do not hardcode cache/index3), NUMA node links, normalized type/capacity/CPPC ranking if exposed. Group by LLC/NUMA and choose complete physical cores with SMT awareness; intersect with online/allowed first. A core ID alone is not globally unique; package+die+core matters. Treat LLC as locality group, not universal literal CCD label. Dual-CCD X3D has cache/clock tradeoffs; do not automatically prefer largest L3 for simulator latency. EPYC NUMA CPU pinning does not itself enforce guest memory NUMA locality. Zen4c is compact CPU implementation, not Intel Atom; no frequency-only identity heuristic.

Latest upstream Linux documents AMD CPUID0x80000026 power/efficiency/core type. Its code separately maps vendor-specific types to generic performance/efficiency/low_power. Therefore raw AMD CPUID0→performance is NOT the same thing as blindly assigning a particular sysfs core_type integer. Kernel version/distribution ABI must be checked. Use the actual exposed normalized interface with fixture evidence, otherwise leave scheduling unpinned. — [Linux topology](https://www.kernel.org/doc/html/latest/arch/x86/topology.html), [Linux vendor mapping](https://raw.githubusercontent.com/torvalds/linux/master/arch/x86/kernel/cpu/topology_common.c), [AMD raw type enum](https://raw.githubusercontent.com/torvalds/linux/master/arch/x86/include/asm/topology.h)

Required reach/tests: hardware AMD single CCD+SMT, dual LLC, no topology, Zen4/Zen4c, sparse/cpuset/offline, Radeon+NVIDIA and Intel+Radeon; unchanged Intel Meteor/Arrow mapping; GPU constructor/report JSON consumers; prerequisites AMD vendor/SVM+AVX2, udmabuf absent/permission denied, RADV vs AMDVLK/lavapipe, multi-GPU mismatch, actual container probe failures; init/import and hardware.json; applyProfile keeps selected CPU model/env; appliance import preserves firmware and safe backup/rollback, reject incompatible ABI before replacement; docs/BLACKBURN_LAPTOP_OPTIMIZATION_AND_PACKAGING.md, docs/BLACKBURN_CLI.md, macbox/.env.blackburn comments and doctor diagnostics. Existing `TestTunedLaunchAffinityAndCache` protects stale affinity and cache, retain it.

### Gaps
No AMD machine was available to this researcher and no host/kernel/VM changes were made. No performance or simulator success claim follows from fixtures. Precise selected-device structured Vulkan collection should be designed around pinned vulkaninfo capabilities. A new GPU config key is only needed if existing MACBOX_RENDER_NODE cannot remain the single source of truth.

## Does xcross Linux-side compilation depend on Intel CPUs?

### Takeaway
No Intel CPU-vendor gate was found in xcross source. Ryzen/Threadripper/EPYC execute the same Linux-x64 host tools, while simulator targets must remain x86_64 for this macOS guest. Architecture/OS, SDK/Swift version, release artifacts and dependencies matter; host brand does not.

### Cited Findings
- `install.sh:148` maps x86_64 or amd64 to `xcross-linux-x64.tar.gz`. Native host uses Dart ABI rather than CPUID vendor — [installer](../../xcross/install.sh#L148), [native host](../../xcross/packages/cli_kit/lib/composition/native_host.dart#L37).
- Linux gen_snapshot host accepts x64 or arm64 and chooses `linux-${host.architecture}`; resolver names assets `gen_snapshot-${mode.name}-${host}.zip`, validates manifest engine/version/SHA256. It is not an Intel-only binary selection — [host](../../xcross/packages/xcross/lib/src/host/linux/flutter/ios_gen_snapshot_host.dart#L15), [manifest](../../xcross/packages/xcross/lib/src/shared/flutter/gen_snapshot/ios_gen_snapshot_manifest.dart#L79), [resolver](../../xcross/packages/xcross/lib/src/shared/flutter/gen_snapshot/ios_gen_snapshot_resolver.dart#L56).
- Swift Linux host requires >=6.4 in this checkout, directs to official Swift Linux install; official distributions expose x86_64 and aarch64 host downloads, not vendor editions — [source](../../xcross/packages/xcross/lib/src/host/linux/sdk/linux_swift_toolchain_host.dart#L10), [official install](https://www.swift.org/install/linux/).
- ObjC Flutter runner is compiled with resolved clang and linked with ld64.lld with explicit target/SDK arguments. No KVM/host GPU dependency in compilation — [runner shim](../../xcross/packages/xcross/lib/src/shared/flutter/build/runner_shim.dart#L46).
- Flutter AOT uses gen_snapshot `--snapshot_kind=app-aot-macho-dylib`; Debug JIT does not need device AOT toolchain. Engine-version artifact matching remains relevant independently of vendor — [snapshotter](../../xcross/packages/xcross/lib/src/shared/flutter/build/flutter_aot_snapshotter.dart#L24).
- Compose Linux explicitly supports x64 and rejects ARM64 host due upstream Kotlin/Native compiler dependency availability; classifier linux-x86_64, konanTarget linux_x64; JDK must match x64. It downloads macos-x86_64 toolchain artifact as well. Thus AMD64 Linux is the supported architecture for Compose, not a gap — [Linux Compose](../../xcross/packages/xcross/lib/src/host/linux/compose/linux_compose_host.dart), [architecture aliases](../../xcross/packages/xcross/lib/src/host/shared/compose/posix_compose_host.dart#L86).
- Blackburn helper exports XCROSS_SIMULATOR_ARCH=x86_64 and builds/runs simulator through actual current `xcross flutter build/run --target-platform simulator` commands — [wrapper](../../scripts/blackburn-macbox.sh#L7).

### Inferences
No xcross production source change required specifically for AuthenticAMD. A host-built third-party plugin using `-march=native`, proprietary tool download or missing x86 simulator slice could still fail independently; test the actual Flutter project. AVX2 requirement here is macOS Tahoe VM requirement, not evidence that every host compiler requires AVX2. The same x86_64 Mach-O plugin/engine selection applies to Intel and AMD host.

Commands after host provisioning and CPU/GPU baseline qualification (the setup command creates/boots a VM, so review CPU choice before running it):

```sh
# Run at Blackburn root. Override laptop-specific build defaults before wrapper sources them.
export MACBOX_CPUS=8 MACBOX_MEMORY=8G
export MACBOX_BUILD_JOBS=8 MAKEFLAGS=-j8 CMAKE_BUILD_PARALLEL_LEVEL=8 CARGO_BUILD_JOBS=8 NINJAFLAGS=-j8
export MACBOX_GPU_TUNING=off
export MACBOX_RENDER_NODE=/dev/dri/renderD128 # verify PCI vendor/driver first
scripts/blackburn-macbox.sh doctor
# For a new install; inspect/configure AMD-safe CPU model before first boot.
# scripts/blackburn-macbox.sh setup-macbox
scripts/blackburn-macbox.sh extract-sdk /path/to/Xcode.xip
# Wrapper preserves cwd; invoke from Flutter project directory with absolute wrapper path.
/home/srayansh/Documents/Golang/Blackburn/scripts/blackburn-macbox.sh build-sim
/home/srayansh/Documents/Golang/Blackburn/scripts/blackburn-macbox.sh run-sim
```

Current `blackburn init --auto` is intentionally not presented as working unchanged on AMD: it fails GenuineIntel/RADV gates. After implementation it should retune recipient allocations, then `blackburn appliance import FILE` and normal `blackburn up` can be qualified. Export remains `blackburn appliance export --output=PATH --compress=zstd|none`.

### Gaps
No native AMD compiler benchmark, new compile or actual app launch was run. No source grep can guarantee third-party closed binaries never dispatch differently by vendor. Existing Intel-named test titles/comments reflect architecture terminology and should be renamed only when touched; they need no functional port.
