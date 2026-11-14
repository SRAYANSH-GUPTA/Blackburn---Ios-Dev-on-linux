# Running Blackburn on AMD CPUs (`AuthenticAMD`) and AMD Radeon GPUs (Mesa `radv`)

> **Master Technical Report — CPU Virtualization (`kvm_amd` / SVM), Radeon GPU Acceleration (`udmabuf` + `radv`), Blackburn CLI Auto-Tuning, and `xcross` Compilation.**
>
> **Detailed Sub-Reports**:
> - [CPU Virtualization, XNU & CoreSimulator (`cpu.md`)](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/AMD%20CPU%20and%20Radeon%20GPU%20support/cpu.md)
> - [Radeon RADV, `reims-vgpu` & `metal2vulkan` (`gpu.md`)](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/AMD%20CPU%20and%20Radeon%20GPU%20support/gpu.md)
> - [Blackburn CLI, Hardware Prober & `xcross` (`cli-and-xcross.md`)](file:///home/srayansh/Documents/Golang/Blackburn/research_notes/AMD%20CPU%20and%20Radeon%20GPU%20support/cli-and-xcross.md)

---

## 1. Executive Summary

Can Blackburn run on **AMD Ryzen / Threadripper / EPYC CPUs** and **AMD Radeon iGPUs/dGPUs (RDNA2/RDNA3/RDNA3.5)**?

**Yes — ~85% of the low-level virtualization and Vulkan translation plumbing is already built into our pinned `reims-vgpu`, `metal2vulkan`, and `macbox` stack**, with upstream boot and rendering evidence already recorded on **AMD Ryzen 7 7840HS + Radeon 780M (`0x1002:0x15bf`)** and **Radeon RX 7900 XTX (`0x1002:0x744c`)**.

What currently blocks an AMD laptop from running Blackburn out of the box is **three explicit Intel-only admission gates in Blackburn's Go CLI (`internal/cli/appliance.go` and `internal/hardware/hardware.go`)**, plus a few AMD-specific host setup and CPU-model precautions:

| Layer | Current Status on AMD | What Needs to Happen |
| :--- | :---: | :--- |
| **1. `xcross` Linux Compilation** | **100% Ready** | Zero changes needed. `clang`, `swiftc`, `ld64.lld`, and `gen_snapshot` are pure `x86_64` (`amd64`) Linux binaries with no Intel vendor checks. |
| **2. QEMU / KVM & OpenCore CPU** | **Supported in `reims-ctl`** | `reims-ctl` already boots QEMU with `Skylake-Client-noTSX-IBRS,kvm=on,vendor=GenuineIntel,+invtsc` and spoofs OpenCore CPUID Leaf 1 (`0x00050654`). **Critical trap**: Never expose `AVX-512` on Zen 4/5 (AMD vs Intel `XSAVE` layout mismatch corrupts state in XNU). |
| **3. GPU (`reims-vgpu` + `radv`)** | **Supported via `/dev/udmabuf`** | Mesa `radv` refuses host-pointer imports of file-backed memory (`AMDGPU_GEM_USERPTR_ANONONLY`), so `reims-vgpu` uses chunked `sealed-memfd -> /dev/udmabuf -> VK_EXT_external_memory_dma_buf`. Host must load `udmabuf` and grant `/dev/udmabuf` access. |
| **4. Blackburn Go CLI & Prober** | **Blocked by 3 Hard Gates** | Remove `GenuineIntel`-only check in `appliance.go`, add PCI vendor `0x1002` in `hardware.go`, and accept Mesa `radv` alongside `anv`. |

---

## 2. Layer-by-Layer Technical Deep Dive

### Layer 1: CPU Virtualization (`kvm_amd` / SVM, XNU, and CoreSimulator)

1. **How `reims-ctl` Presents the CPU to macOS on AMD**:
   - In pinned `reims-vgpu` (`7198fe60`), `crates/reims-ctl/src/vm.rs` and `qemu.rs` default to:
     ```text
     -cpu Skylake-Client-noTSX-IBRS,kvm=on,vendor=GenuineIntel,+invtsc,vmware-cpuid-freq=on,+ssse3,+sse4.2,+popcnt,+avx,+aes,+xsave,+xsaveopt,check
     ```
   - Simultaneously, `reims-util` (`cbde8611`) configures OpenCore with a **CPUID Leaf 1 EAX override (`0x00050654` — Skylake-SP)**, `DummyPowerManagement = true`, and `ProvideCurrentCpuInfo = true`.
   - Because QEMU spoofs `vendor=GenuineIntel` with an Intel family/model and hides AMD-specific SVM leaves, **you do NOT need bare-metal "AMD Vanilla" OpenCore kernel patches** when using the named Intel CPU model.

2. **The Critical Zen 4 / Zen 5 AVX-512 Trap (Must Avoid)**:
   - Upstream Reims benchmarks on an **AMD Ryzen 7 7840HS (Zen 4)** proved that macOS 26.6.1 boots to the desktop using `Haswell-noTSX` or `Skylake-Client-noTSX-IBRS`.
   - However, if you use `Cascadelake-Server` or `-cpu host` on Zen 4/5, QEMU exposes **AVX-512**. AMD Zen 4/5 uses different `XSAVE` state area offsets for AVX-512 (`opmask` / `ZMM_Hi256` / `Hi16_ZMM`) than Intel CPUs, while macOS XNU hardcodes Intel's `XSAVE` offsets! When XNU restores thread/signal state, it corrupts registers and crashes processes.
   - **Rule for AMD**: Always keep the default `Skylake-Client-noTSX-IBRS` (or `Haswell-noTSX`) CPU model where **AVX2 is enabled and AVX-512 is disabled**.

3. **MSR Handling & Nested Virtualization (`CoreSimulator`)**:
   - On AMD `kvm_amd`, if XNU probes unimplemented Intel-specific Model-Specific Registers (MSRs), Linux KVM can inject a `#GP` fault unless `kvm.ignore_msrs=1` is set:
     ```bash
     echo "options kvm ignore_msrs=1" | sudo tee /etc/modprobe.d/blackburn-kvm.conf
     ```
   - **Does iOS Simulator (`launchd_sim`) need Nested Virtualization?**
     **No.** Unlike Docker Desktop or Android Emulator on macOS, Apple's iOS Simulator (`CoreSimulator` / `launchd_sim` / `dyld_sim`) runs iOS processes as native user-space Mach-O binaries directly on the guest XNU kernel. It does **not** use `Hypervisor.framework` (`vmx`). Therefore, the lack of Intel VMX on AMD SVM does not block iOS Simulator execution.

---

### Layer 2: GPU Acceleration (`reims-vgpu` + `metal2vulkan` on AMD Radeon `radv`)

1. **Why `/dev/udmabuf` is Mandatory on AMD GPUs**:
   - On Intel Arc/Iris Xe (`anv`), `metal2vulkan` imports QEMU's shared guest RAM (`memory-backend-memfd`) directly via `VK_EXT_external_memory_host`.
   - On AMD GPUs (`amdgpu` kernel driver + Mesa `radv`), the kernel enforces `AMDGPU_GEM_USERPTR_ANONONLY` — rejecting host-pointer imports of file-backed (`memfd`) memory!
   - To solve this without copying memory, `reims-vgpu` (`crates/reims-vgpu-vulkan/src/os/linux/dma_buf.rs`) converts the sealed guest `memfd` ranges into **DMA-BUFs** via `/dev/udmabuf` (`UDMABUF_CREATE_LIST`, chunked into 64 MiB / 1024-extent pieces) and imports them into RADV via `VK_EXT_external_memory_dma_buf`.
   - Upstream verified this exact path on **Radeon 780M (`0x15bf`)** and **Radeon RX 7900 XTX (`0x744c`)** with 5,119 zero-copy DMA-BUF imports and zero refusals.

2. **What `macbox` Already Has for AMD GPUs**:
   - [`macbox/macbox` line 60](file:///home/srayansh/Documents/Golang/Blackburn/macbox/macbox#L60):
     ```bash
     [ -e /dev/udmabuf ] && dev+=(--device /dev/udmabuf)   # AMD GPUs import guest memory through it
     ```
   - [`macbox/macbox` lines 262–263](file:///home/srayansh/Documents/Golang/Blackburn/macbox/macbox#L262-L263): `macbox doctor` already detects PCI vendor `0x1002` (AMD) and checks `/dev/udmabuf` write permissions.
   - [`macbox/docker/Dockerfile`](file:///home/srayansh/Documents/Golang/Blackburn/macbox/docker/Dockerfile): Already installs `mesa-vulkan-drivers` (which includes `libvulkan_radeon.so` / `radv`) and `mesa-va-drivers` (which includes Radeon VA-API hardware H.264/HEVC encoding for browser streaming).

3. **GPU Tuning & `host_rt_in_place` on RADV**:
   - In [`macbox/macbox` line 140](file:///home/srayansh/Documents/Golang/Blackburn/macbox/macbox#L140), `gpu_is_arl()` checks PCI device IDs without checking vendor `0x8086`. We should guard `gpu_is_arl()` to require vendor `0x8086`.
   - Patch `0004` (`host_rt_in_place=1`) was benchmarked on Intel ANV where `DmaBuf` was marked unchecked in microtests. On AMD RADV, start with `MACBOX_GPU_TUNING=off` first to verify baseline rendering, then test `MACBOX_GPU_TUNING=on`.

---

### Layer 3: Blackburn Go CLI & Hardware Prober Changes Required

There are **3 specific places** in our Go codebase that currently reject AMD hardware:

1. **CPU Vendor Gate in [`internal/cli/appliance.go` (lines 37–40)](file:///home/srayansh/Documents/Golang/Blackburn/internal/cli/appliance.go#L37-L40)**:
   - *Current code*:
     ```go
     cpuinfo, _ := os.ReadFile("/proc/cpuinfo")
     if !strings.Contains(string(cpuinfo), "GenuineIntel") {
         return r, fmt.Errorf("Intel CPU required")
     }
     ```
   - *Required change*: Accept both `GenuineIntel` and `AuthenticAMD` (and verify `avx2` flag in `/proc/cpuinfo`).

2. **GPU PCI Vendor Filter in [`internal/hardware/hardware.go` (lines 197–203)](file:///home/srayansh/Documents/Golang/Blackburn/internal/hardware/hardware.go#L197-L203)**:
   - *Current code*: Filters `/sys/bus/pci/devices` for `class == 0x03xxxx` and `vendor == "0x8086"`.
   - *Required change*: Also recognize `vendor == "0x1002"` (AMD Radeon), recording `Vendor: "0x1002"` and mapping known RDNA2/RDNA3/RDNA3.5 IDs (e.g. `0x15bf` Radeon 780M, `0x1681` Radeon 680M, `0x150e` Radeon 890M, `0x744c` RX 7900 XTX) as well as generic `"AMD Radeon"`.

3. **Vulkan Driver Gate in [`internal/cli/appliance.go` (lines 60–66)](file:///home/srayansh/Documents/Golang/Blackburn/internal/cli/appliance.go#L60-L66)**:
   - *Current code*:
     ```go
     if !regexp.MustCompile(`(?mi)^\s*driverName\s*=\s*(Intel open-source Mesa driver|anv)\s*$`).MatchString(v) {
         return r, fmt.Errorf("Intel Mesa ANV Vulkan device required")
     }
     ```
   - *Required change*: Accept Mesa RADV (`radv` / `AMD open-source driver` / `DRIVER_ID_MESA_RADV`) and, when an AMD GPU is selected, require `/dev/udmabuf` to be present and accessible (`os.OpenFile("/dev/udmabuf", os.O_RDWR, 0)`).

4. **CPU Topology / Affinity on AMD (`internal/hardware/hardware.go`)**:
   - On standard homogeneous AMD Ryzen CPUs (where all cores are full Zen cores), `cpu_core/cpus` does not exist, and `hardware.Probe` already falls back gracefully to leaving `CPUSet` empty (`""`) and sizing up to `min(8, onlineCPUs)` vCPUs.
   - On hybrid **Zen 4 + Zen 4c** or **Dual-CCD Ryzen 9 / X3D** laptops, we can optionally read `/sys/devices/system/cpu/cpu*/cache/index3/shared_cpu_list` to keep the 8 vCPUs inside a single L3 CCX/CCD to avoid cross-CCD infinity-fabric latency!

---

## 3. Step-by-Step Host Setup Checklist for an AMD Laptop

If you are setting up an AMD Ryzen + Radeon Linux laptop, here is the exact host preparation sequence:

### Step 1: Enable SVM in BIOS & Configure Linux Kernel Modules
1. Ensure **SVM Mode (AMD-V)** is enabled in BIOS/UEFI.
2. Load `kvm_amd`, `amdgpu`, and `udmabuf`, and configure `ignore_msrs=1`:
   ```bash
   sudo modprobe kvm_amd
   sudo modprobe amdgpu
   sudo modprobe udmabuf

   # Persist modules and MSR handling across reboots:
   echo "udmabuf" | sudo tee /etc/modules-load.d/blackburn-udmabuf.conf
   echo "options kvm ignore_msrs=1" | sudo tee /etc/modprobe.d/blackburn-kvm.conf
   ```

### Step 2: Grant `/dev/udmabuf` and `/dev/kvm` Permissions to Your User
Because AMD GPUs require `/dev/udmabuf` for zero-copy guest memory import, give the `kvm` group read/write access to `/dev/udmabuf`:
```bash
sudo tee /etc/udev/rules.d/99-blackburn-udmabuf.rules >/dev/null <<'EOF'
KERNEL=="udmabuf", GROUP="kvm", MODE="0660"
EOF
sudo udevadm control --reload-rules && sudo udevadm trigger /dev/udmabuf
sudo usermod -aG kvm,render,docker $USER
```
*(Log out and back in for group changes to apply, or verify with `test -r /dev/udmabuf -a -w /dev/udmabuf`)*

### Step 3: Install Mesa RADV & VA-API Packages
```bash
# Arch Linux:
sudo pacman -S vulkan-radeon libva-mesa-driver vulkan-tools libva-utils qemu-base docker zstd

# Ubuntu / Debian:
sudo apt install mesa-vulkan-drivers mesa-va-drivers vulkan-tools vainfo qemu-system-x86 docker.io zstd
```

### Step 4: Verify RADV & DMA-BUF Extensions
```bash
vulkaninfo | grep -E "deviceName|driverName|VK_EXT_external_memory_dma_buf"
```
Verify that `driverName = radv` and `VK_EXT_external_memory_dma_buf` are present.
