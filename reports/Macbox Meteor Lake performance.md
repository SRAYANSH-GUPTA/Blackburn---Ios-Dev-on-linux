# Reduce pixel work before tuning daemons

**The highest-impact profile is one 1x slim simulator, the complete patched GPU stack, persistent caches, and no duplicate Simulator.app presentation or recording.** The often-quoted 95 versus 38 FPS compares **1x versus 3x pixel density**, not one versus three simulators; it comes from Arrow Lake, not this Meteor Lake laptop. Blackburn already uses 1080p60, P-core pinning, 12 vCPUs and 8 GB guest RAM. Its roughly 15 GiB host RAM makes memory pressure an immediate concern. GPU facts copied from Arrow Lake and undocumented Quartz defaults are not validated shortcuts. The commands below are source-checked proposals; no running host/VM settings were changed and no local performance gains were benchmarked. ([Performance source](https://github.com/TUNA-NOPE/macbox/blob/main/docs/performance.md), [research evidence](</home/srayansh/Documents/Golang/Blackburn/research_notes/Macbox Meteor Lake performance/simulator.md>))

## In-place rendering is applicable; Arrow Lake facts are not

Keep `MACBOX_GPU_TUNING=auto` with the **complete macbox build**. Patch 0004's `host_rt_in_place=1` renders eligible guest framebuffer/simulator surfaces directly in guest RAM, eliminating shadow copies that consumed 51% of simulator GPU time in the reference workload. **Patch 0005 must accompany it**: it fixes ordering between imports of the same guest pages. The path probes each memory class and falls back on failure, making it a reasonable Meteor Lake experiment, although measured benefits are from Core Ultra 7 265/Mesa 26.2.4. Change `host_rt_in_place` in `$MACBOX_HOME/run/live.conf`, then recreate surfaces or restart the VM; preserve the file and compare only this switch. ([Patch inventory](https://github.com/TUNA-NOPE/macbox/blob/main/patches/README.md), [performance](https://github.com/TUNA-NOPE/macbox/blob/main/docs/performance.md))

```bash
# Linux bash; load Blackburn's actual trusted configuration first.
source "${MACBOX_CONFIG:-$HOME/.config/macbox/blackburn.env}"
live="${MACBOX_HOME:?Configuration must set MACBOX_HOME}/run/live.conf"
cat "$live"                     # inspect; requires an existing patched setup
backup="$live.blackburn-$(date +%Y%m%d-%H%M%S)"
cp -p -- "$live" "$backup" &&
  sed -i '/^[[:space:]]*host_rt_in_place[[:space:]]*=/d' "$live" &&
  printf '\nhost_rt_in_place=1\n' >> "$live"
# Recreate surfaces/restart VM deliberately before measuring.
# Rollback in this shell: cp -p -- "$backup" "$live"; recreate surfaces again.
```

**Do not set `M2V_ARL_FACTS` for 8086:7d55.** The launcher excludes Meteor Lake, and the underlying patch gates the override even more narrowly to ANV/8086:7d67. `M2V_LIVE` supports `facts_on=`/`facts_off=`, but these override correctness facts, not generic speed settings. No validated Meteor Lake list was found: use GPU correctness lanes before any one-fact experiment. Avoid `heap_compressed=1`, `ANV_QUEUE_OVERRIDE=gc=6`, and GPU-identity spoofing as defaults; upstream reports regressions or no improvement, and `fbread` breaks WindowServer. ([Local patch gate](/home/srayansh/Documents/Golang/Blackburn/macbox/patches/metal2vulkan/0001-env-gated-Arrow-Lake-S-facts-and-deviati.patch:64), [live facts](/home/srayansh/Documents/Golang/Blackburn/macbox/patches/metal2vulkan/0003-live-experiment-switches-M2V_LIVE-driver.patch:325), [negative results](https://github.com/TUNA-NOPE/macbox/blob/main/docs/performance.md))

Mesa's shader disk cache is enabled by default. Preserve it and Reims' separate `~/.cache/reims-vgpu-vulkan` under the persisted container HOME; warm the representative scene before measuring. Increase Mesa's default 1 GB only when eviction is implicated. A `VkPipelineCache` additionally needs application load/save logic; environment variables cannot supply that missing implementation. Reims' documented shader directory alone does not prove a separate Vulkan pipeline-cache blob exists. ([Mesa cache controls](https://docs.mesa3d.org/envvars.html#shader-cache-environment-variables), [Khronos implementation](https://github.com/KhronosGroup/Vulkan-Samples/blob/main/samples/performance/pipeline_cache/pipeline_cache.cpp), [Reims patch](/home/srayansh/Documents/Golang/Blackburn/macbox/patches/reims-vgpu/0003-port-the-Vulkan-platform-to-metal2vulkan.patch:1743))

Macbox filters inherited environment variables. Use its actual configured container/HOME/VM values and `reims-ctl set`, rather than assuming host exports reach QEMU:

```bash
# Optional cache experiment; takes effect at next VM restart.
# Defaults shown: replace these with values from blackburn.env if different.
MACBOX_HOME=${MACBOX_HOME:-$HOME/.local/share/macbox}
MACBOX_CONTAINER=${MACBOX_CONTAINER:-macbox}
MACBOX_VM=${MACBOX_VM:-mac}
docker exec -e "HOME=$MACBOX_HOME/home" "$MACBOX_CONTAINER" \
  bash -c '. "$HOME/.cargo/env"; exec reims-ctl set "$1" \
    --env MESA_SHADER_CACHE_DISABLE=false \
    --env MESA_SHADER_CACHE_MAX_SIZE=2G' _ "$MACBOX_VM"
# Rollback: repeat ctl set with --env MESA_SHADER_CACHE_MAX_SIZE=
# and --env MESA_SHADER_CACHE_DISABLE= (if originally unset).
docker exec "$MACBOX_CONTAINER" vulkaninfo --summary
```

`MESA_SHADER_CACHE_SHOW_STATS=true` is temporary diagnostics. `INTEL_DEBUG=stall/sync/noccs`, queue overrides, WSI overrides, submit-thread settings and alternate cache backends have no validated gain here. Confirm the **container's** Mesa/ANV and firmware, not merely the host's; leave debug serialization off. ([Launcher](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:33), [ANV documentation](https://docs.mesa3d.org/drivers/anv.html))

## Supported appearance changes beat unverified Quartz flags

Use Accessibility → Display → **Reduce Transparency** and Accessibility → Motion → **Reduce Motion**. The former reduces desktop blur work; neither changes simulator accessibility preferences. No primary evidence validates `CGDisableBeamSync`, `QuartzGLEnabled`, `CompositorTiming` or similar legacy defaults for Tahoe/Reims. `METAL_ERROR_MODE=5` is already a macbox compatibility setting, not demonstrated acceleration. ([Apple display settings](https://support.apple.com/guide/mac-help/mchl11ddd4b3/mac), [motion](https://support.apple.com/en-au/guide/mac-help/mchlc03f57a1/mac), [guest source](https://github.com/TUNA-NOPE/macbox/blob/main/guest/macbox-guest))

Macbox's `base` provisioner already disables Spotlight indexing, sleep/screensaver and automatic update downloads; it also changes sudo and screen-lock behavior, so do not rerun the full provisioner solely for appearance. Record current settings before narrow changes:

```sh
# Inside macOS guest:
mdutil -as
sudo mdutil -a -i off
# Restore only volumes that originally had indexing enabled: mdutil -i on VOLUME
defaults read com.apple.universalaccess reduceTransparency
defaults write com.apple.universalaccess reduceTransparency -bool true
# Restore the recorded value, or delete this key if originally absent.
```

Disable unused Siri, analytics sharing and account sync through Settings first. Safari's “Preload Top Hit in the background” is a search preference, distinct from launch-agent prewarming. Audit actual CPU use before touching candidate Siri/assistant, Safari, diagnostic, biometric, `bird` or `cloudd` jobs: labels and protection differ by guest version. Keep diagnostics for development unless measured otherwise; keep sync/biometrics when testing those features, and retain security, logging and CoreSimulator infrastructure. ([Apple analytics](https://support.apple.com/guide/mac-help/mh27990/mac), [Safari preference](https://support.apple.com/en-ae/guide/safari/ibrwe75c2a3c/mac), [guest audit](</home/srayansh/Documents/Golang/Blackburn/research_notes/Macbox Meteor Lake performance/guest-audit.sh>))

For one **confirmed, previously enabled and loaded** unused agent, discover its real plist/Label, record `launchctl print-disabled "gui/$(id -u)"` and `launchctl print DOMAIN/LABEL`, then run `launchctl disable DOMAIN/LABEL` and `launchctl bootout DOMAIN/LABEL`. Roll back with `launchctl enable DOMAIN/LABEL` and `launchctl bootstrap DOMAIN PLIST`. System jobs use `DOMAIN=system` and sudo. Consult the guest's `man launchctl`; if protected, stop the experiment. Do not weaken SIP to remove background jobs.

## A 1x pool removes nine times the pixels

The reference GlassBench result is **402×874 at 95.8/92.5 FPS versus 1206×2622 at 38.5/39.5 FPS**: one simulator in both cases, nine times the pixels at 3x. Macbox creates a custom device type that preserves layout points while changing screen scale, dimensions and DPI. Resizing Simulator.app alone does not accomplish this. `full` and `slim-1` are both 1x, allowing a fair service-slimming comparison; `sharp` retains stock scale. ([Performance](https://github.com/TUNA-NOPE/macbox/blob/main/docs/performance.md), [pool implementation](https://github.com/TUNA-NOPE/macbox/blob/main/guest/macbox-guest))

`pool create` runs `~/go/bin/simslim on UUID --preserve-boot-state` for slim devices. Simslim changes **per-device launchd overrides**, not the signed shared runtime; `off` restores services. Keep a full device for push, widgets, Live Activities and system integration tests, or selectively retain required services. Macbox pins v0.12.2; its expected installation is in the **guest** `~/go/bin`. The supplied Linux `~/.macbox/bin/simslim` path was absent; that does not establish whether a guest build exists. ([Simslim](https://github.com/MobAI-App/simslim#usage), [version pin](https://github.com/TUNA-NOPE/macbox/blob/main/guest/share/versions.env))

```bash
ssh macbox 'ls -l ~/.macbox/bin/simslim ~/go/bin/simslim 2>/dev/null; ~/go/bin/simslim --help'
ssh macbox '~/.macbox/macbox-guest pool list; xcrun simctl list devices booted'
# One-time: custom-type registration requires other simulators shut down.
# Shut down only owned devices deliberately; avoid FORCE=1.
ssh macbox 'MACBOX_POOL=slim-1 ~/.macbox/macbox-guest pool create'
# Guest: replace UUID; check installed help before version-dependent options.
# ~/go/bin/simslim status UUID
# ~/go/bin/simslim measure UUID
# Rollback: ~/go/bin/simslim off UUID --preserve-boot-state
MACBOX_CONFIG="$HOME/.config/macbox/blackburn.env" MACBOX_POOL=slim-1 mac-sim up
```

## Fix idle timing separately from streaming overhead

For human desktop interaction, `macbox view local` removes capture/encode/decode stages; schedule its possible container restart. For app automation, `mac-sim ad` uses persistent SSH and direct simulator interaction with Simulator.app closed. QMP is useful for guest GUI setup, but the current wrapper inserts at least **170 ms of sleeps per click**; that is wrapper pacing, not inherent QMP latency. Shorter pacing with a persistent connection needs missed-event testing. Current documented Selkies uses H.264/WebSocket; do not assume every stream is WebRTC. Avoid QEMU VNC's GPU readback path. ([Architecture](https://github.com/TUNA-NOPE/macbox/blob/main/docs/architecture.md), [agent workflow](https://github.com/TUNA-NOPE/macbox/blob/main/docs/agents.md), [QMP wrapper](https://github.com/TUNA-NOPE/macbox/blob/main/lib/vmctl.py))

**An idle-display driver defect adds roughly 100–150 ms before the first command buffer.** Upstream identifies a required guest kext timing fix. Continuous animation masks it but wastes resources; a display-link-only attempt consumed about 3.5 cores. Neither cache tuning nor bitrate changes repairs that gap. No comparative input-to-photon benchmark establishes a QMP/agent-device winner. ([Reims issue 141](https://github.com/steelbrain/reims-vgpu/issues/141))

Run the read-only [Blackburn audit](/home/srayansh/Documents/Golang/Blackburn/scripts/blackburn-performance-audit.sh), optionally `bash scripts/blackburn-performance-audit.sh --guest macbox`. Compare 12 versus 8 vCPUs first; 0–11 are this host's six SMT P cores. Keep 8 GB initially; test 6 GB only while tracking guest memory pressure and host `vmstat` swap-in/out. The observed 6.8 GiB occupied swap does not prove active swapping. Warm the same scene, measure 30 seconds without recording using `simfps` if installed, and compare p95/p99 frame intervals plus first interaction after idle. At 60 Hz, 95 producer FPS cannot display 95 unique frames each second. The main engineering opportunity beyond these controls is correct guest display timing, not more background-job deletion.

```sh
# In the guest, after warming a deterministic animated scene:
command -v simfps              # if absent, build/provision macbox/bench/simfps.swift
simfps '<actual simulator UUID>' 30 blackburn-1x-warm
# Its JSON measures framebuffer production, not input-to-photon latency.
```

The `simfps UUID SECONDS LABEL` interface is verified in [source](https://github.com/TUNA-NOPE/macbox/blob/main/bench/simfps.swift); do not assume historical documentation's `guest/bench` binary path exists.
