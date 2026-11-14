# AMD CPU virtualization, XNU and CoreSimulator

## What does Blackburn's pinned virtualization stack actually expose?

### Takeaway
The pinned stack already has an AMD-compatible CPU presentation design and upstream documents a Ryzen 7 7840HS boot. Blackburn should qualify that existing path before adding AMD Vanilla patches or inventing a second CPU model. This is upstream evidence, not an AMD Blackburn execution test performed in this investigation.

### Cited Findings
- Blackburn pins Reims `7198fe60c9df14593e86d090cb4640c3e45ac65b`, whose ctl explicitly pins reims-util `cbde8611f5d28a4835099d0d01279549e1b7709a` in `crates/reims-ctl/src/reims_util.rs:37–39`. The local Reims working tree is at a different commit (`4743cc8497605e6b51c7cfcf662c1822ec90251d`); all pinned findings below were read with `git show`, not assumed from HEAD. [Pinned util dependency](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/crates/reims-ctl/src/reims_util.rs#L37-L39)
- Default `[q35].cpu_model` is `Skylake-Client-noTSX-IBRS`; default `cpu_flags` is `+ssse3,+sse4.2,+popcnt,+avx,+aes,+xsave,+xsaveopt,check`. [vm.rs:438–447](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/crates/reims-ctl/src/vm.rs#L438-L447)
- The generated CPU argument starts `Skylake-Client-noTSX-IBRS,kvm=on,vendor=GenuineIntel,+invtsc,vmware-cpuid-freq=on`, appending those default flags. The q35 machine uses KVM and `-smp N,sockets=1,cores=N,threads=1`, independent of host CCDs and SMT layout. Linux RAM uses `memory-backend-memfd,...,share=on`. [qemu.rs:165–205](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/crates/reims-ctl/src/qemu.rs#L165-L205)
- Profile eligibility checks Linux + x86_64, not GenuineIntel. Nested VMX is added only when `/sys/module/kvm_intel/parameters/nested` reads Y or 1. AMD's SVM is deliberately not substituted for VMX because Apple's Hypervisor.framework does not drive it. [host.rs:61–66,105–110](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/crates/reims-ctl/src/host.rs#L61-L66)
- OpenCore generation separately overrides CPUID leaf 1 EAX to `0x00050654` (Skylake-SP family 6/model 85/stepping 4), with mask bytes `ff ff ff 00`; this is **not** QEMU's CPU model and does not turn on AVX-512. It enables `DummyPowerManagement` and `ProvideCurrentCpuInfo`. Kernel patches contain only optional VM-hiding string replacements; there is no AMD Vanilla patch injection. [Pinned config.rs:165–225](https://github.com/steelbrain/reims-util/blob/cbde8611f5d28a4835099d0d01279549e1b7709a/src/opencore/config.rs#L165-L225)
- ctl creates/refreshes OpenCore without disabling that default CPUID spoof. The macOS-compatible SMBIOS default is separate from CPU vendor/model. [reims_util.rs:249–295](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/crates/reims-ctl/src/reims_util.rs#L249-L295)
- Pinned docs report macOS 26.6.1 reaching desktop on Ryzen 7 7840HS with Haswell-noTSX and Cascadelake-Server under the leaf-1 override. They measured missing PCID on Ryzen and missing HLE/RTM for Cascade. Crucially, Cascade exposed AVX-512 with AMD XSAVE offsets, while XNU used Intel offsets: signal/thread-state restore corrupted opmask/ZMM state. The document explicitly leaves a boot of the default Skylake model itself unmeasured. [CPU measurements](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/docs/qemu.md#L1092-L1134)

### Inferences
- First AMD implementation should keep the current Skylake client default and current OpenCore spoof, then test it on AMD hardware; do not expose AVX-512 through Cascade/Skylake-Server or `-cpu host` on Zen 4/5. If comparing models experimentally, Haswell-noTSX has upstream boot evidence but depends on the retained CPUID override because its default stepping is rejected by XNU. Named models do not emulate missing CPU instructions under KVM.
- QEMU `check` warns and filters unsupported features; a successful QEMU start is not proof of correct guest behavior. Capture emitted CPU properties and guest state. Do not advertise PCID, TSX, Intel VMX, or AVX-512 merely because an Intel named model includes them.
- No change to q35 CPU vendor spoof appears necessary just to admit AMD; the Blackburn host-side GenuineIntel gate is a different issue, handled in the main report.

### Gaps
- No AMD machine was available for execution. Skylake-client boot, suspend/resume timing, long-session stability, AMD EPYC/Threadripper, Zen4c/Zen5c, and CoreSimulator were not measured here.
- The Reims measurements do not establish cross-host migration compatibility or correct AVX-512 support; they establish a concrete reason to avoid AVX-512.

## Are AMD Vanilla patches, MSR overrides or nested virtualization required?

### Takeaway
Treat kernel compatibility and nested virtualization separately. The known Reims KVM path presents an Intel-style virtual CPU and does not inject AMD Vanilla; bare-metal AMD patch instructions should not be copied wholesale into this q35 guest.

### Cited Findings
- QEMU's named-model infrastructure allows adjusting exposed CPU features; host passthrough exposes host model/features and is unsafe for heterogeneous live migration. [QEMU CPU model documentation](https://www.qemu.org/docs/master/system/qemu-cpu-models.html)
- AMD Vanilla is a binary kernel patch set for AMD CPUs, requires `ProvideCurrentCpuInfo`, and includes core-count and platform-specific patches. Its bare-metal Zen4 IOPCIFamily and PAT choices are not automatically applicable to virtual q35 hardware. [AMD Vanilla README](https://github.com/AMD-OSX/AMD_Vanilla/blob/master/README.md)
- Linux `kvm.ignore_msrs=1` causes unhandled guest MSR accesses to be ignored; default 0 injects #GP. [Linux kernel parameter reference](https://www.kernel.org/doc/html/latest/admin-guide/kernel-parameters.html)
- OSX-KVM supplies AMD configuration `options kvm_amd nested=1` and `options kvm ignore_msrs=1 report_ignored_msrs=0`, and its Tahoe boot command uses a Skylake client with HLE/RTM disabled, GenuineIntel, invtsc and VMware frequency CPUID. This is a separate project's setup, not proof that all those settings are mandatory for pinned Reims. [AMD modprobe config](https://github.com/kholia/OSX-KVM/blob/master/kvm_amd.conf), [boot script](https://github.com/kholia/OSX-KVM/blob/master/OpenCore-Boot.sh)
- Reims only offers nested VMX when Intel KVM provides it; its docs explicitly distinguish AMD SVM from Hypervisor.framework VMX. [VMX design](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/docs/qemu.md#L1136-L1161)

### Inferences
- Begin with existing OpenCore and named-model path. If an AMD boot fails, collect verbose guest serial/panic output, KVM unhandled-MSR logs and actual `-cpu` command before selecting any workaround.
- Keep `report_ignored_msrs` diagnostics enabled while qualifying; only suppress them deliberately after the failure is understood. `ignore_msrs` changes host-wide KVM behavior, so report that scope. It is a compatibility workaround, not CPU emulation or a fix for wrong XSAVE layout.
- CoreSimulator for iOS is not itself a nested macOS VM. Lack of Apple's nested Hypervisor.framework support on SVM should not be assumed to block `launchd_sim`/`dyld_sim`. Nested Docker/UTM/Apple Virtualization workloads are a separate unsupported goal on this configuration.
- `invtsc` should remain paired with VMware CPUID frequency presentation, but stable timekeeping still needs actual AMD tests across host idle states, suspend/resume and load; do not promise that adding the flag solves all drift.

### Gaps
- No evidence that `ignore_msrs=1` is strictly required for every Reims/AMD combination; present it as a diagnostic boot-compatibility fallback with known semantics.
- No credible evidence from inspected sources that blanket AMD Vanilla is required by this exact pinned VM. If a separate host-passthrough experimental path is needed, design and pin it separately, mask AVX-512, and qualify its exact guest-visible CPU and version-specific patches.

## How should an AMD host be prepared and qualified?

### Takeaway
Validate hardware SVM/KVM first, keep the versioned Tahoe + x86_64 Simulator baseline, then separately qualify graphics and hot reload. CPU brand is not a new compilation target architecture.

### Cited Findings
- Apple distinguishes CPU architecture from platform; `TARGET_OS_SIMULATOR` is the proper simulator conditional, while `TARGET_CPU_X86_64` names x86_64 code. [Apple WWDC architecture guidance](https://developer.apple.com/videos/play/wwdc2020/10214/)
- Current Apple Xcode 27 notes say it only installs/runs on Apple silicon; therefore future Xcode releases are not a drop-in upgrade to a Tahoe x86_64 VM. Preserve qualified Xcode/runtime versions. [Apple Xcode 27 release notes](https://developer.apple.com/documentation/xcode-release-notes/xcode-27-release-notes)

### Inferences
Proposed commands (not executed; no system or running-VM changes made by this investigation):

```sh
# Firmware: enable SVM/AMD-V. GPU passthrough/IOMMU is not needed for Reims.
lscpu
rg -m1 'vendor_id|flags' /proc/cpuinfo
sudo modprobe kvm_amd
ls -l /dev/kvm
cat /sys/module/kvm_amd/parameters/npt
cat /sys/module/kvm/parameters/ignore_msrs
cat /sys/module/kvm/parameters/report_ignored_msrs
# Do not unload/reload KVM while existing VMs run.

# Only if boot diagnostics show relevant unhandled-MSR faults:
printf 'options kvm ignore_msrs=1\n' | sudo tee /etc/modprobe.d/blackburn-kvm.conf
# Applies on next module load/reboot; no reboot command is required in a research report.
# nested=1 does not make Intel VMX available on AMD and is not required for iOS Simulator.
```

Qualification checks inside the newly booted guest:

```sh
sysctl machdep.cpu.vendor machdep.cpu.family machdep.cpu.model machdep.cpu.stepping
sysctl hw.optional.avx2_0 hw.optional.avx512f
sysctl hw.ncpu hw.physicalcpu hw.logicalcpu
xcodebuild -version
xcrun simctl list runtimes
xcrun simctl list devices available
# Boot an already installed x86_64-capable runtime/device, then:
xcrun simctl bootstatus booted -b
xcrun simctl spawn booted launchctl print system
```

Record the full CPU invocation, guest build, OpenCore/util commit, runtime architecture, cold boot, repeated simulator launch/shutdown, app launch, Flutter Impeller rendering, Dart VM service attach/hot reload, signal-heavy and vector workloads, host suspend/resume, and time drift. Verify that AVX-512 remains disabled. Distinguish cold shader compilation from steady-state frame pacing.

### Gaps
- `launchd_sim`/`dyld_sim` AMD compatibility is an execution qualification question, not established by x86_64 ABI reasoning or a desktop screenshot. Do not claim successful Blackburn simulator/hot-reload execution on AMD without this matrix.
- Existing user guest/runtime may differ from upstream's measured 26.6.1; the exact installed versions must be recorded during future qualification.
