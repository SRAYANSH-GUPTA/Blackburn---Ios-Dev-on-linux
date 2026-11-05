# iOS Simulator: scale, service slimming and interaction latency

## What makes 1x fast, and what does simslim change?

### Takeaway
The 95 versus 38 FPS comparison is **pixel density (1x versus 3x), not one versus three concurrent simulators**. Use a 1x slim device for ordinary development, a 1x full device for system-feature tests, and 3x only for visual fidelity checks.

### Cited Findings
- The reference benchmark used Core Ultra 7 **265 Arrow Lake**, 128 GB, Mesa 26.2.4, an 8-vCPU/12-GB guest, macOS 26.7.1/iOS 26.5, one simulator at a time, no encoding and Simulator.app closed. GlassBench measured 95.8/92.5 FPS at 402×874 versus 38.5/39.5 at 1206×2622. Those are nine times as many pixels, not three simulators. This is not a measured result for Meteor Lake 155H. [Upstream performance](https://github.com/TUNA-NOPE/macbox/blob/main/docs/performance.md); local `macbox/docs/performance.md:17–41`.
- `make_type` copies a stock device-type bundle and changes `mainScreenScale`, width, height and DPI while retaining layout in points. Scaling Simulator.app's window alone does not implement this optimization. `full` and `slim-N` use this custom 1x type; `sharp` uses the original device type; every kind except `full` is slimmed. [Guest implementation](https://github.com/TUNA-NOPE/macbox/blob/main/guest/macbox-guest); local `macbox/guest/macbox-guest:490–525,573–612`.
- `pool create` invokes guest `~/go/bin/simslim on UDID --preserve-boot-state`; it checks per-device `/private/var/tmp/com.apple.CoreSimulator.SimDevice.UDID/disabled.plist` and leaves existing devices with unexpected types unchanged. Introducing a custom type requires restarting CoreSimulatorService; the script refuses while simulators are booted unless FORCE=1. [Guest implementation](https://github.com/TUNA-NOPE/macbox/blob/main/guest/macbox-guest); local lines 528–539,573–612.
- Simslim disables per-simulator launchd jobs and preserves overrides on supported runtimes. It does **not** strip the signed shared runtime. Service categories include Siri, search, iCloud, widgets, sync and diagnostics. `off` restores services; `--except` and `--keep` retain needed functionality. Use the full device for push/widgets/Live Activities or explicitly retain their services. [Simslim README](https://github.com/MobAI-App/simslim#usage).
- Macbox pins simslim v0.12.2 and installs via Go into guest `~/go/bin`, not necessarily `~/.macbox/bin`. On this Linux filesystem `/home/srayansh/.macbox/bin/simslim` does not exist; guest placement was not checked by SSH. [Version pin](https://github.com/TUNA-NOPE/macbox/blob/main/guest/share/versions.env); local `macbox/guest/share/versions.env`, `macbox/guest/macbox-guest:289–295`.

### Inferences
- Lower pixel density reduces GPU bandwidth/fill and translation work; daemon slimming principally reduces memory/background pressure. Treat them as separate optimizations. Benchmark `full` versus `slim-1` at the same 1x resolution to isolate slimming.
- Keep the stock runtime intact. A fresh disposable pool device avoids requiring a runtime backup; for valuable simulator state, clone it before changing service overrides. `simslim off` reverses services, whereas erase/delete or disk-clean affect data.

### Gaps
- No Meteor Lake FPS, input-to-photon or per-daemon memory benchmark was run; upstream results are directional evidence only. Installed guest simslim version/path and runtime are unverified. Confirm via commands below before using newer CLI options.

## Which interaction path minimizes latency?

### Takeaway
For humans, native Wayland display removes the stream capture/encode/decode stages. For simulator automation, agent-device targets the simulator directly and allows Simulator.app to stay closed. QMP is useful for macOS GUI setup, but the supplied wrapper deliberately inserts sleeps.

### Cited Findings
- `macbox view local` displays Reims SDL3 through the host Wayland socket without capture or encoding. The current documented Selkies implementation uses H.264 over a WebSocket with acknowledgement-driven rate control; calling every installation “WebRTC” is imprecise. noVNC uses wayvnc; QEMU's own VNC is reported at only 2–3 FPS because of GPU readback. [Architecture](https://github.com/TUNA-NOPE/macbox/blob/main/docs/architecture.md); local `macbox/docs/architecture.md:68–88`.
- `mac-sim ad` forwards agent-device commands to the guest, with persistent SSH ControlMaster. The CLI supports accessibility snapshots and direct simulator presses. Simulator.app is unnecessary and its extra presentation window increases GPU work. [Agent workflow](https://github.com/TUNA-NOPE/macbox/blob/main/docs/agents.md); local `macbox/sims/bin/mac-sim:86–97`, `macbox/docs/performance.md:48–49`.
- `vmctl.py` sends QMP `input-send-event`, but sleeps 50 ms after pointer movement and 60 ms around press/release; a click helper runs at least 170 ms of deliberate sleeps. Key press/release adds 30+30 ms, and `key` adds another 100 ms; type has 60 ms per character. These are wrapper pacing, not intrinsic QMP latency. [Implementation](https://github.com/TUNA-NOPE/macbox/blob/main/lib/vmctl.py); local `macbox/lib/vmctl.py:69–100`.
- Open Reims issue #141 reports 114–143 ms from an idle-display animation request to the guest's first submitted command buffer, falling to 8 ms with continuous rendering. Maintainer confirms a kext patch is required. A display-link-only workaround burned ~3.5 CPU cores with zero presents. This latency precedes host GPU work and cannot be cured by browser bitrate settings. [Reims #141](https://github.com/steelbrain/reims-vgpu/issues/141).

### Inferences
- Native display is the lowest-overhead desktop path, but not zero-latency; neither QMP nor agent-device has a published comparable end-to-end benchmark here. Do not claim a precise winner between automation backends without measurement.
- For app-only agent operation, keep Simulator.app closed. For human interaction requiring its window, display only one device and accept the additional composition cost, or use an existing direct simulator viewer (T3 device panel).
- A persistent QMP automation connection with measured, shorter event pacing could reduce scripted interaction overhead; this is an engineering experiment requiring tests for missed gestures/keys, not a general “disable all sleeps” prescription.

### Gaps
- No hardware-camera input-to-photon measurements; issue #141 is desktop guest-driver evidence and does not quantify the separate direct simulator framebuffer path. A reported 95 FPS framebuffer throughput does not mean a 60 Hz monitor displays 95 unique frames/s.

## Concrete Blackburn commands and validation

### Takeaway
The host already has 1080p60, GPU auto tuning, 8 GB guest RAM and P-core pinning. The immediate simulator optimization is correct device selection and avoiding duplicate presentation/recording, followed by a controlled resource experiment.

### Cited Findings
- Read-only inspection on 2026-11-05: `~/.config/macbox/blackburn.env` specifies 12 vCPUs, 8G RAM, 1920×1080, 60 Hz and CPUSET 0–11. `lscpu -e=CPU,CORE,MAXMHZ,ONLINE` maps 0–11 to six SMT P cores (4.5/4.8 GHz), 12–19 to eight E cores (3.8 GHz) and 20–21 to two LP-E cores (2.5 GHz). `free -h` showed 15 GiB RAM, ~3.1 GiB available and 6.8 GiB swap used, a transient snapshot rather than proof of current active swapping. [Local configuration](/home/srayansh/.config/macbox/blackburn.env:12). No host/guest settings were changed.
- `simfps` directly polls framebuffer IOSurface seeds without encoding; `sim-fps` analyzes recordings. Upstream documents software `simctl recordVideo` limits near 50 FPS with one animated simulator and 18 FPS each with three; recordings are not a reliable ceiling for renderer throughput. [Measurement implementation](https://github.com/TUNA-NOPE/macbox/blob/main/bench/simfps.swift); [Performance](https://github.com/TUNA-NOPE/macbox/blob/main/docs/performance.md).

```bash
# Linux: verify path and exact installed version, without modifying guest state.
ssh macbox 'command -v simslim; ls -l ~/.macbox/bin/simslim ~/go/bin/simslim 2>/dev/null; ~/go/bin/simslim --help'
ssh macbox '~/.macbox/macbox-guest pool list; xcrun simctl list devices booted'

# One-time guest provisioning: creates ONE requested pool device, retains existing others.
# Beforeward shut down only devices you own if new custom types need loading.
ssh macbox 'MACBOX_POOL=slim-1 ~/.macbox/macbox-guest pool create'

# Use the chosen UUID from pool list; these execute inside the macOS guest.
# Do not use "booted" when several simulators might be running.
ssh macbox
sim_udid='<UUID from pool list>'
~/go/bin/simslim status "$sim_udid"
~/go/bin/simslim on "$sim_udid" --preserve-boot-state
xcrun simctl boot "$sim_udid"   # skip if already booted
xcrun simctl bootstatus "$sim_udid" -b
~/go/bin/simslim measure "$sim_udid"
# Rollback services if needed:
# ~/go/bin/simslim off "$sim_udid" --preserve-boot-state
exit

# From a supported app checkout, choose the slim-only pool for the mac-sim session:
MACBOX_CONFIG="$HOME/.config/macbox/blackburn.env" MACBOX_POOL=slim-1 mac-sim up
MACBOX_CONFIG="$HOME/.config/macbox/blackburn.env" MACBOX_POOL=slim-1 mac-sim snapshot
MACBOX_CONFIG="$HOME/.config/macbox/blackburn.env" MACBOX_POOL=slim-1 mac-sim ad press @e12

# Human desktop display (may switch/restart the container; run when ready):
macbox view local
```

### Inferences
- Keep RAM at 8G initially; compare 6G only while watching both host `vmstat 1` (swap-in/out, not merely swap occupancy) and guest `memory_pressure`/`vm_stat`. Slashing guest RAM can increase stalls. Current 12 guest vCPUs consume all 12 P-core logical CPUs; compare 8 vCPUs against 12 rather than assuming all 22 host threads are better. Keep the same GPU, app, device, display and warm-up for each run.
- Measure a deterministic scrolling scene for at least 30 s after warm-up with Simulator.app closed and no recording. Use `simfps UUID 30 label` only if built/provisioned; source is at `macbox/bench/simfps.swift`, so do not assume the docs' historical guest/bench path exists. Separate first-interaction latency from sustained FPS; collect frame intervals (p95/p99), GPU busy time, CPU use and host/guest swap behavior.

### Gaps
- Example commands were source-checked, not executed against the running guest; UUID placeholders must be replaced. The host missing-path result does not disprove the user's existing guest build. Pool boot-state restore is intentional, but booting/slimming may temporarily consume resources. No performance gain is claimed until measured on this laptop.
