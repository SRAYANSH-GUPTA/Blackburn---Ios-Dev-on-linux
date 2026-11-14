# Radeon RADV, reims-vgpu and metal2vulkan

## What works at Blackburn’s pinned revisions?

### Takeaway
AMD support is already substantive upstream functionality at the exact pinned Reims revision, not merely a future master feature. Blackburn’s patched combination and its CoreSimulator/Flutter workload still require Radeon hardware qualification.

### Cited Findings
- Inspected exact git objects from existing local source repositories without changing their checkout: Reims `7198fe60c9df14593e86d090cb4640c3e45ac65b`, metal2vulkan `0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd`; these match [macbox/versions.env](../../macbox/versions.env). Local checkout HEADs contain later/local changes, so findings below use `git show <pin>:<path>`.
- **Pinned evidence:** Reims’s qemu documentation already reports Radeon 780M cold boot to macOS desktop and Safari Wikipedia load/quit, with zero refusals among 5,119 dma-buf requests (5,095 single-buffer, remainder 2–16 chunks), at stock udmabuf limits. This is an upstream maintainer report, not a Blackburn measurement. [Pinned qemu.md lines 642–659](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/docs/qemu.md#L642-L659).
- Pinned documentation also contains RX 7900 XTX measurements and guest video evidence; pinned runtime driver facts explicitly contain Radeon780M PCI0x15bf and RX7900XTX PCI0x744c. These justify RDNA3 candidates, not universal Radeon certification. [Pinned qemu.md](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/docs/qemu.md#L990-L1020), [pinned driver facts](https://github.com/steelbrain/metal2vulkan/blob/0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd/crates/metal2vulkan/src/device/driver_facts.rs#L645-L654).
- Pinned 780M display results warn against promising smooth 4K: 3840x2160 WindowServer buffers about25ms/frame and roughly quarter of 30fps video frames dropped versus about12ms at1920x1080 and roughly1% drops. These measurements use earlier Reims/M2V combinations named in the document, not Blackburn’s patched binaries. [Pinned qemu.md lines661–690](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/docs/qemu.md#L661-L690).
- RADV’s own documentation supports Linux-supported graphics-capable GCN/RDNA GPUs and reports Vulkan1.4 on GFX8 and newer. This establishes driver coverage, not complete Metal translation or Simulator correctness. [Mesa RADV](https://docs.mesa3d.org/drivers/radv.html#supported-hardware).

### Inferences
- Radeon680M (RDNA2), 780M (RDNA3), 890M (RDNA3.5) and discrete RDNA2/3 should use the same generic Vulkan/import path when all necessary features are present; 780M has direct pinned evidence, 680M/890M remain candidates requiring tests. Never equate RADV conformance with product support.
- AMD CPU and AMD GPU support are independent axes; Intel CPU+Radeon and AMD CPU+Intel GPU should not be excluded by vendor-pair assumptions.

### Gaps
- No inspected source establishes Blackburn+its local patches+CoreSimulator+xcross end-to-end success on any Radeon, nor explicit680M/890M runs. No new boot/build/benchmark was performed.

## What Vulkan and Linux memory facilities are required?

### Takeaway
RADV cannot import QEMU’s file-backed shared guest pages through its ordinary host-pointer path; Reims already implements sealed-memfd→udmabuf→Vulkan dma-buf import with chunking. Check capabilities and actual import behavior rather than demanding an invented blanket “Vulkan1.3 GPU” gate.

### Cited Findings
- RADV uses `AMDGPU_GEM_USERPTR_ANONONLY`; shared memfd pages are refused (`vkGetMemoryHostPointerPropertiesEXT` returns `VK_ERROR_UNKNOWN` in780M measurement). Reims obtains dma-bufs of the exact same pages through `UDMABUF_CREATE_LIST`, imports through `VK_EXT_external_memory_dma_buf`, retaining CPU/GPU sharing without copying the payload. Pinned code contains chunking for current module `list_limit`/`size_limit_mb` (defaults1024 runs/64MiB), sealed against shrinking memfds, and page-aligned file offsets. **No need to raise limits merely to obtain support; no need to upgrade master for this chunking.** [Pinned dma_buf.rs lines1–60](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/crates/reims-vgpu-vulkan/src/os/linux/dma_buf.rs#L1-L60).
- The same source records pinning overhead: about9ms/64MiB for one piece; more live imports increase submission cost (1024 ~642μs,4096 ~2.5ms in measured configuration). “Zero copy” does not mean free import, no pinning, or no later texture shadow copies. [Pinned dma_buf.rs](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/crates/reims-vgpu-vulkan/src/os/linux/dma_buf.rs#L17-L52).
- **Hard creation floor:** Vulkan1.2 loader/physical device; graphics+compute queue; runtime unconditionally enables core1.2 `timelineSemaphore` and `hostQueryReset`, checks conformant timeline-value difference. [Pinned creation](https://github.com/steelbrain/metal2vulkan/blob/0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd/crates/metal2vulkan-runtime/src/device/create.rs), [pinned enables.rs lines1166–1170](https://github.com/steelbrain/metal2vulkan/blob/0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd/crates/metal2vulkan-runtime/src/enables.rs#L1166-L1170).
- **Hard for Radeon guest-memory path:** usable `/dev/udmabuf`, `VK_KHR_external_memory_fd` AND `VK_EXT_external_memory_dma_buf`, and a compatible imported memory type; library docs explicitly say missing access kills AMD boot at first no-copy buffer. `VK_EXT_external_memory_host` is enabled where listed, but successful file-backed pointer import cannot be assumed on RADV. [Pinned creation lines887–934](https://github.com/steelbrain/metal2vulkan/blob/0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd/crates/metal2vulkan-runtime/src/device/create.rs#L887-L934), [pinned qemu.md](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/docs/qemu.md#L642-L651).
- **Optional paths, not universal startup requirements:** `VK_EXT_image_drm_format_modifier`+`VK_EXT_queue_family_foreign` enable dma-buf texture images; `VK_NV_linear_color_attachment`/linearColorAttachment, host-image-copy, graphics-pipeline-library and generated-commands are opportunistically selected. Exact texture formats, tiling and compatible memory are checked independently; lack can yield shadow/fallback rather than whole-device rejection. [Pinned creation](https://github.com/steelbrain/metal2vulkan/blob/0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd/crates/metal2vulkan-runtime/src/device/create.rs#L924-L934).
- `bufferDeviceAddress`+`shaderInt64` select Addressed profile; otherwise translator selects Floor. `scalarBlockLayout` is only required where emitted types need it. Features such as fragment interlock, float16/int64 atomics, descriptor indexing and sparse resources influence particular operations/policies; do not label all universal mandatory extensions without workload proof. [Pinned derive.rs lines172–205](https://github.com/steelbrain/metal2vulkan/blob/0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd/crates/metal2vulkan/src/device/derive.rs#L172-L205).
- Native window display also needs platform surface+swapchain support; Reims adds window extensions, while device creation refuses explicitly required missing extension features. Headless rendering and window presentation have different capability sets. [Pinned platform.rs](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/crates/reims-vgpu-vulkan/src/platform.rs#L355-L383).

### Inferences
- Doctor should separate “can create Vulkan device”, “can import guest memfd”, “can render native window”, “can encode browser video”, and “passes Simulator rendering”; `vulkaninfo --summary` alone proves only inventory.
- Shared huge pages can reduce some import-memory workload costs, but keep host hugepage policy untouched by default; pinned docs describe device-dependent results and advising shared memory, not a universal hugepages requirement.

### Gaps
- Static extension presence does not establish all import-format combinations work. A targeted sealed-memfd import smoke test and rendering tests are needed on the actual laptop, kernel and container Mesa.

## What does Blackburn need to change, and how should an AMD host be diagnosed?

### Takeaway
macbox already exposes Radeon render nodes, udmabuf and Mesa drivers. Remaining gaps include CLI Intel gates, strict permission preflight, explicit Vulkan GPU selection, and Radeon validation of local rendering optimizations and streaming.

### Cited Findings
- `macbox/macbox` line60 passes `/dev/udmabuf` into Docker only when it exists; lines262–263 identify AMD0x1002 and warn on permissions. This is support plumbing already present, but a warning does not ensure successful boot. [Local macbox](../../macbox/macbox).
- Container includes `mesa-vulkan-drivers`, `mesa-va-drivers`, Vulkan tools and VA-API tools; Intel’s iHD package is additional, not the only GPU implementation. Ubuntu26.10 image controls userspace Mesa while host kernel/firmware control amdgpu. No NVIDIA proprietary driver mounts or NVIDIA Container Toolkit `--gpus` wiring is configured. Upstream NVIDIA measurements therefore do not imply stock macbox NVIDIA works. [Dockerfile](../../macbox/docker/Dockerfile).
- `MACBOX_RENDER_NODE` drives Docker device exposure, Sway `WLR_RENDER_DRM_DEVICE`, and Selkies `--encode-dri`. cenv does not pass DRI_PRIME/VK_DRIVER_FILES selectors; reims device options leave kind selection empty; runtime ranks **discrete,integrated,virtual,CPU,other**, then enumeration order. There is no direct mapping from `MACBOX_RENDER_NODE` to runtime physical device. Device-node exposure often constrains real hardware availability, but llvmpipe or multiple ICD enumeration still warrants checking. [Local shell](../../macbox/macbox), [display-up](../../macbox/container/display-up.sh), [stream-up](../../macbox/container/stream-up.sh), [pinned device ranking](https://github.com/steelbrain/metal2vulkan/blob/0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd/crates/metal2vulkan-runtime/src/device/create.rs#L728-L747), [preference](https://github.com/steelbrain/metal2vulkan/blob/0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd/crates/metal2vulkan-runtime/src/device/properties.rs#L45-L58).
- `M2V_ARL_FACTS` is currently gated by Intel ArrowLake device IDs7d67,7dd1,7d41,7d51,b640; keep those facts empty on Radeon. `gpu_is_arl()` presently reads device ID without vendor, so harden against vendor-ID collisions. `host_rt_in_place=1` is auto-enabled on any patched macbox build, including Radeon, not restricted to Intel. Local0004 tests each page class and falls back if checks fail, but its description explicitly says **DmaBuf unchecked** in its measured ANV run. Its microtests are not Radeon validation. [Local tuning](../../macbox/macbox), [patch0004](../../macbox/patches/metal2vulkan/0004-live-switches-host_rt_in_place-and-shado.patch), [patch overview](../../macbox/patches/README.md).
- `MESA_SHADER_CACHE_MAX_SIZE=2G` is a valid optional cache capacity choice for ANV and RADV, not a correctness requirement. Mesa documents default1GiB and cache location overrides; RADV_PERFTEST is a collection of particular version-dependent flags rather than a recommended blanket tuning preset. Leave RADV_PERFTEST unset initially (do not blindly set legacy `aco`, `gpl`, `ngg` folklore). Reims also has its own translated-shader cache at `$HOME/.cache/reims-vgpu-vulkan`, default25GiB, separate from Mesa’s. [Mesa cache/env docs](https://docs.mesa3d.org/envvars.html#core-mesa-environment-variables), [RADV_PERFTEST](https://docs.mesa3d.org/envvars.html#radv-perftest), [pinned Reims cache](https://github.com/steelbrain/reims-vgpu/blob/7198fe60c9df14593e86d090cb4640c3e45ac65b/crates/reims-vgpu-vulkan/src/platform.rs#L521-L538).

### Inferences
Recommended smallest implementation reaches `internal/hardware` GPU model/probe and tests, `internal/cli/appliance.go` init validation and tests, macbox doctor/container preflight, cenv/QEMU process env or upstream physical-device selection API, docs and relevant fixtures. Accept Radeon0x1002+RADV independently from CPU vendor; validate selected actual physical device against chosen DRM major/minor or PCI BDF; keep Intel facts vendor+device gated. Start AMD qualification with MACBOX_GPU_TUNING=off, then test host_rt_in_place on/off per page class and compare screenshot correctness/GPU latency. Do not add global RADV_PERFTEST defaults.

Proposed Ubuntu-family AMD setup/diagnostics (not executed; adapt distribution/package release and actual render node):

```sh
sudo apt install mesa-vulkan-drivers mesa-va-drivers vulkan-tools vainfo linux-firmware
sudo modprobe amdgpu
sudo modprobe udmabuf
ls -l /dev/kvm /dev/udmabuf /dev/dri/renderD*
lspci -nnk
vulkaninfo --summary
vulkaninfo | rg 'driverName|deviceName|vendorID|external_memory_(fd|dma_buf|host)|image_drm_format_modifier|queue_family_foreign|timelineSemaphore|hostQueryReset'
vainfo --display drm --device /dev/dri/renderD128
cat /sys/module/udmabuf/parameters/{list_limit,size_limit_mb}
```

Grant only the groups actually owning the nodes (commonly render,kvm; inspect first), log out/in after group membership changes. For systems without a packaged udmabuf rule, an administrator can create a narrow `KERNEL=="udmabuf", GROUP="kvm", MODE="0660"` udev rule and persist module loading. Do not use chmod666. Docker daemon membership is independent of render access. Recreate existing macbox container after adding udmabuf or changing render-node mapping because Docker device exposure is set at container creation.

Run `macbox doctor` and repeat vulkaninfo/vainfo **inside the actual existing container**, using its recorded name rather than guessing. Verify GPU name/driver, not merely Vulkan loader availability. Shell exports such as MESA_SHADER_CACHE_MAX_SIZE or DRI_PRIME currently do not automatically reach QEMU; explicitly persist selected variables using supported `reims-ctl set VM --env KEY=VALUE` and inspect launched environment, or implement cenv forwarding. `MACBOX_RENDER_NODE` must be saved in macbox config before starting a fresh container.

Qualification sequence: 1080p boot; native and browser viewers; GPU/VA-API errors; Simulator boot; Metal screenshot comparison; Flutter Impeller and hot reload; repeated teardown/reopen; import counts/memory residency; host_rt on/off. Radeon iGPU shares system RAM, discrete Radeon uses imported system memory across PCIe; measure each, avoid predicting equal throughput.

### Gaps
No host module, group, configuration, VM, build or container change was made. No AMD hardware performance or correctness was measured; all commands above are proposed procedures.
