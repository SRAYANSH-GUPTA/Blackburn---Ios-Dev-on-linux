# Meteor Lake GPU, Vulkan and Reims tuning

## Which tuning is applicable to PCI 8086:7d55?

### Takeaway
Use the complete patched macbox build, including synchronization patch 0005, with `MACBOX_GPU_TUNING=auto` and `host_rt_in_place=1`. This is the strongest optimization candidate on Meteor Lake, but the published measurements are from an Arrow Lake desktop, not the 155H laptop.

### Cited Findings
- `host_rt_in_place` avoids copies around render passes by rendering guest framebuffer/simulator surfaces directly into guest RAM. It probes correctness per memory class and falls back on failure. The check covers padding and multiple image shapes; surfaces must be recreated after changing the switch. See [local performance.md:60](/home/srayansh/Documents/Golang/Blackburn/macbox/docs/performance.md:60), [patch 0004:7](/home/srayansh/Documents/Golang/Blackburn/macbox/patches/metal2vulkan/0004-live-switches-host_rt_in_place-and-shado.patch:7), and [upstream repository performance page](https://github.com/TUNA-NOPE/macbox/blob/main/docs/performance.md).
- The local patch suite reports shadow copies taking 51% of simulator GPU time, with 3x GlassBench improving from about 22.5 to 39 FPS. Its reference is Core Ultra 7 265, Mesa 26.2.4, 8 vCPUs/12 GB. These numbers cannot be advertised as measurements on 155H. Patch 0005 fixes ordering between two imports of the same guest pages and must accompany 0004. [Local patch inventory](/home/srayansh/Documents/Golang/Blackburn/macbox/patches/README.md:13); [published patch inventory](https://github.com/TUNA-NOPE/macbox/blob/main/patches/README.md).
- `M2V_ARL_FACTS` is not a general Xe knob. The launcher excludes 7d55; more strictly, patch 0001 only honors it when driver is ANV and identity is 8086:7d67. Setting it on 7d55 has no effect. There is even a mismatch between the launcher's broader list of GPU IDs and the patch's one-ID gate. [Launcher:103](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:103), [patch gate:64](/home/srayansh/Documents/Golang/Blackburn/macbox/patches/metal2vulkan/0001-env-gated-Arrow-Lake-S-facts-and-deviati.patch:64).
- Patch 0003 offers `facts_on=` / `facts_off=` in `M2V_LIVE`, independent of that PCI gate. These bypass measured driver-fact selection; they can enable incorrect behavior and are appropriate only for one-fact-at-a-time validation using metal2vulkan GPU lanes. No validated 7d55 fact list was found. [Live facts implementation](/home/srayansh/Documents/Golang/Blackburn/macbox/patches/metal2vulkan/0003-live-experiment-switches-M2V_LIVE-driver.patch:325).
- Avoid `heap_compressed=1`, `ANV_QUEUE_OVERRIDE=gc=6`, and `REIMS_VGPU_NON_APPLE_GPU` as defaults: no gain or regressions in the source benchmark; `fbread` breaks WindowServer. [Local negative results](/home/srayansh/Documents/Golang/Blackburn/macbox/docs/performance.md:76).

### Inferences
- Meteor Lake and Arrow Lake sharing Xe lineage is a reason to test the in-place path, not to spoof device IDs or transplant facts. For a clean A/B, preserve live.conf, change only `host_rt_in_place`, and restart VM for each run. `MACBOX_GPU_TUNING=off` is less controlled since it also disables other automatic tuning.
- A practical profile is 1920x1080 at 60 Hz, one 1x simulator, Simulator.app closed, local Wayland display when interacting. These settings follow the measured pixel-work bottleneck; they must still be benchmarked on this laptop. [Settings source](/home/srayansh/Documents/Golang/Blackburn/macbox/docs/performance.md:43).

### Gaps
- No Meteor Lake-specific validated driver facts or measured in-place benefit was available. Pinned upstream driver_facts.rs could not be fetched through web (cache miss), and shell network DNS was unavailable; only supplied local patch context was inspected.

## Can Mesa cache/environment settings eliminate hitches?

### Takeaway
Retain persistent caches and warm the representative scene. Treat cache-size changes as optional experiments; there is no source-backed ANV magic switch that removes all compilation hitches.

### Cited Findings
- Mesa disk shader caching defaults on; `MESA_SHADER_CACHE_DISABLE=false` enables it, `MESA_SHADER_CACHE_MAX_SIZE` defaults to 1 GB, and `MESA_SHADER_CACHE_SHOW_STATS=true` reports hits/misses at exit. Increasing to `2G` is sensible only if cache eviction is observed. `MESA_DISK_CACHE_DATABASE=1` selects another storage implementation; speed improvement here is unmeasured. [Mesa environment documentation](https://docs.mesa3d.org/envvars.html#shader-cache-environment-variables).
- Reims separately configures a shared shader cache at `~/.cache/reims-vgpu-vulkan`. Its directory is relative to the container HOME, which macbox sets to `$MACBOX_HOME/home` and persists by bind-mounting MACBOX_HOME. The Mesa cache also naturally lives under this persisted HOME. Do not routinely delete either cache. [Reims cache documentation in patch](/home/srayansh/Documents/Golang/Blackburn/macbox/patches/reims-vgpu/0003-port-the-Vulkan-platform-to-metal2vulkan.patch:1743), [launcher HOME and mount](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:21).
- An application Vulkan pipeline cache (`VkPipelineCache`) requires application creation/loading/saving. Mesa environment variables do not add a missing application-level cache. [Khronos pipeline-cache implementation](https://github.com/KhronosGroup/Vulkan-Samples/blob/main/samples/performance/pipeline_cache/pipeline_cache.cpp).
- Intel ANV calls `INTEL_DEBUG=stall`, `sync`, `noccs` and queue overrides debugging tools. Do not leave them on for performance. ANV also requires suitable GuC firmware on recent Intel generations. [Official ANV documentation](https://docs.mesa3d.org/drivers/anv.html#runtime-dependencies).
- Exporting Mesa vars in the host shell does not automatically forward them through macbox: `cenv` explicitly forwards a small allowlist. Configure QEMU's environment via `reims-ctl set --env` instead. [cenv:33](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:33).

### Inferences
Optional, reviewable commands for an already-created container/VM (run after sourcing your actual macbox config; defaults shown). This records the variables in the VM's configuration, survives VM restarts, and avoids changing global host environment or needing a launcher patch:

```bash
MACBOX_HOME=${MACBOX_HOME:-$HOME/.local/share/macbox}
MACBOX_CONTAINER=${MACBOX_CONTAINER:-macbox}
MACBOX_VM=${MACBOX_VM:-mac}
docker exec -e "HOME=$MACBOX_HOME/home" "$MACBOX_CONTAINER" \
  bash -c '. "$HOME/.cargo/env"; exec reims-ctl set "$1" \
    --env MESA_SHADER_CACHE_DISABLE=false \
    --env MESA_SHADER_CACHE_MAX_SIZE=2G' _ "$MACBOX_VM"
# Apply at next VM restart. For an experiment only, add:
# --env MESA_SHADER_CACHE_SHOW_STATS=true
# Remove optional override later with --env MESA_SHADER_CACHE_MAX_SIZE=
```

For integration into macbox itself, add explicit cache defaults to `apply_tuning`'s `ctl set --env` arguments. Adding them merely to host config or cenv is insufficient if reims-ctl filters inherited process environment. Keep diagnostic stats optional.

```bash
# Inspect cache occupancy without clearing it:
du -sh "$MACBOX_HOME/home/.cache/"* 2>/dev/null
# Confirm actual device first:
cat /sys/class/drm/renderD128/device/{vendor,device}
# Verify driver loaded inside the container, not just the host:
docker exec "$MACBOX_CONTAINER" vulkaninfo --summary
```

### Gaps
- Local patch context proves Reims shader-cache path but does not prove which artifacts it stores or persistence of a separate VkPipelineCache blob. Do not label the Reims cache a verified Vulkan pipeline cache. Exact pipeline-cache behavior needs the pinned full source/build tree.
- No validated Meteor Lake latency improvement was found for `MESA_VK_ENABLE_SUBMIT_THREAD`, WSI present-mode overrides, alternative Mesa cache storage, or Intel queue overrides. They should not enter defaults without frame-time A/B results.

## What causes residual input/animation latency?

### Takeaway
An upstream guest-display timing defect can add roughly 100–150 ms before the first command buffer when idle. It needs a guest kext fix; reducing encoder delay or shader compilation does not address that gap.

### Cited Findings
- Reims issue 141 documents the idle gap with no host command buffers during the delay; keeping a tiny continuously rendering window drops it to about 8 ms, but wastes resources. A display-link-only workaround spins about 3.5 CPU cores; host present-complete interrupts did not fix it. Maintainer confirms a kext patch is required. [Reims issue 141](https://github.com/steelbrain/reims-vgpu/issues/141).

### Inferences
- Measure steady scrolling and first interaction after idle separately. Avoid a permanent animation workaround when minimal idle resources is a goal; track/upstream the timer fix instead.

### Gaps
- No merged fix was visible in the inspected issue. No measured 155H input-to-photon numbers were available.
