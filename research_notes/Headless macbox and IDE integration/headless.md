# Headless macbox and Reims graphics

## Can macbox run without GNOME or a host desktop?

### Takeaway

Yes. The existing `stream` mode already uses a private headless Sway, independent of the host GNOME session. Removing even that compositor is supported by the installed Reims/QEMU source: `-display none` plus the VM-config environment override `REIMS_VGPU_WINDOW=0`. GPU execution continues through Vulkan; no display socket is needed. This is source-verified, runtime-untested in this research session.

### Cited Findings

- Repository pins: Reims base `7198fe60c9df14593e86d090cb4640c3e45ac65b`, QEMU `bd95509aa307f48b13fe8497d8a1b64267cb4c3e`, metal2vulkan `0db5825a6092ed6ab52e0a3b6e0f10d6e92928dd`; the locally patched Reims checkout HEAD is `4743cc8497605e6b51c7cfcf662c1822ec90251d`. Sources: [versions.env](/home/srayansh/Documents/Golang/Blackburn/macbox/versions.env:1), local read-only `git -c safe.directory=PATH rev-parse HEAD`.
- Docker currently exposes `/dev/kvm` and only the selected render node; it mounts the host Wayland socket only for `view local`. Thus stream mode does not require GNOME, host Wayland, Xorg, a monitor, or KMS master access. Sources: [macbox](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:54), [display-up.sh](/home/srayansh/Documents/Golang/Blackburn/macbox/container/display-up.sh:12).
- Private Sway uses `WLR_BACKENDS=headless`, `WLR_HEADLESS_OUTPUTS=1`, `WLR_LIBINPUT_NO_DEVICES=1`, and `WLR_RENDER_DRM_DEVICE=$RENDER`. It has one virtual output, no Xwayland and fullscreen windows. Sources: [display-up.sh](/home/srayansh/Documents/Golang/Blackburn/macbox/container/display-up.sh:14), [sway.conf](/home/srayansh/Documents/Golang/Blackburn/macbox/docker/sway.conf:1).
- Render nodes allow rendering without any graphics server or DRM master and prohibit modesetting. Physical output ownership is a separate operation through a primary `/dev/dri/cardN` node. [Linux DRM UAPI](https://docs.kernel.org/gpu/drm-uapi.html#render-nodes).
- Installed `apple_gfx_window_wanted()` returns true only when QEMU display type is `NONE` and `REIMS_VGPU_WINDOW` differs from `0`. It creates the GPU regardless and sets `display_config.window=NULL` in the offscreen case. Sources: [installed apple-gfx.c](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:869), [device creation](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:1090); current upstream also documents the override [Reims README](https://github.com/steelbrain/reims-vgpu#environment-overrides).
- Merely exporting `REIMS_VGPU_WINDOW=0` around `reims-ctl start` does not suffice: ctl removes inherited `REIMS_VGPU_WINDOW`, then applies explicitly configured `extra_env`. Use `reims-ctl set mac --env REIMS_VGPU_WINDOW=0`. Empty values remove an override. Sources: [qemu.rs](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/qemu.rs:119), [cli.rs](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/cli.rs:1084).
- Vulkan driver selection is independent of `MACBOX_RENDER_NODE`: the latter selects exposed Docker device, Sway render GPU and Selkies encoder. ctl selects ICD and optional device filter through `VK_DRIVER_FILES`, `VK_ICD_FILENAMES`, `VK_LOADER_DEVICE_ID_FILTER`. Do not imply Reims itself consumes `MACBOX_RENDER_NODE`. Source: [qemu.rs](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-ctl/src/qemu.rs:135).
- QEMU `egl-headless,rendernode=...` supplies an OpenGL display backend and should be paired with VNC/SPICE for viewing. That is not the Metal-to-Vulkan acceleration path. [QEMU display options](https://www.qemu.org/docs/master/system/qemu-manpage.html#display-options).
- Existing Linux presentation asks SDL to construct a Vulkan surface; no explicit bespoke KMS scanout backend is present in that code. `SDL_VIDEO_DRIVER=wayland` is the correct SDL3 variable already used here. Sources: [render.rs](/home/srayansh/.local/share/macbox/src/reims-vgpu/crates/reims-vgpu-window/src/native/linux/render.rs:66), [SDL3 hint documentation](https://wiki.libsdl.org/SDL3/SDL_HINT_VIDEO_DRIVER).

### Inferences

Recommended modes:

| Mode | Host GNOME needed | Private compositor | Encoder/capture | Best use |
| --- | --- | --- | --- | --- |
| `local` | A host Wayland session | No | No | Interactive desktop development |
| proposed `private` | No | Headless Sway | Off until requested | Daily IDE work, occasional simulator view |
| proposed `headless` | No | None | None | CI, CLI, background simulator, screenshots |
| KMS kiosk | No | Minimal DRM compositor or new direct-scanout implementation | Optional | Dedicated physical monitor |

“Directly against the kernel” still includes userspace Mesa/ANV Vulkan and Reims/metal2vulkan. The clean compositor-free pipeline is guest Metal → Reims → metal2vulkan → Mesa Vulkan → render-node ioctls → kernel GPU driver. It needs no KMS for offscreen rendering. KMS is relevant only when putting pixels onto a physical connector; it needs primary-node/seat access and coordination with any existing DRM master. Do not stop GDM or expose `/dev/dri/cardN` merely to run CI.

Do not present `SDL_VIDEO_DRIVER=kmsdrm` as a verified drop-in. Although SDL has platform drivers, this build's SDL configuration, Vulkan WSI extensions, seat handling and exclusive monitor ownership must be established. A small Sway/Weston DRM session on a dedicated VT is a lower-risk kiosk design; it is still a compositor and needs additional device/seat permissions. No KMS work is necessary for the two requested workflows.

`egl-headless` could choose the no-own-window branch of this QEMU fork, but introduces EGL/GL initialization without replacing the actual Vulkan GPU implementation. There is no verified benefit over `REIMS_VGPU_WINDOW=0`; do not confuse virgl/virtio-gpu recipes with Apple's `apple-gfx-pci` device.

### Gaps

No new VM boot, GPU benchmark, KMS trial or CI run was performed. Headless CoreSimulator behavior depends on the existing guest provisioning, logged-in GUI session and compatible runtime. Removing the host compositor does not remove guest macOS WindowServer, SpringBoard, backboardd or rendering by the iOS app. A server can have no Linux desktop while the guest retains its graphical session.

## Exact lifecycle design and code

### Takeaway

Separate VM execution, private display, and stream services. Source-supported true headless operation is one environment override plus bypassing `display-up.sh`; the useful daily compromise retains private Sway but starts no encoder until requested. Switching the existing `view local|stream` restarts the VM, so it is not live viewer attachment.

### Cited Findings

- `cmd_up` unconditionally calls `display-up.sh` when no host Wayland socket exists; that script eagerly starts both wayvnc and the chosen stream. Sources: [cmd_up](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:298), [display-up tail](/home/srayansh/Documents/Golang/Blackburn/macbox/container/display-up.sh:29).
- Current streaming uses Selkies H.264 over a WebSocket, not the conventional Selkies WebRTC design. The CLI explicitly uses `PIXELFLUX_HOST_CAPTURE=zwlr`, a private Wayland display and the render node encoder. Audio is disabled. Source: [stream-up.sh](/home/srayansh/Documents/Golang/Blackburn/macbox/container/stream-up.sh:1).
- `view local|stream` calls `cmd_down; cmd_up` if the container runs. `stream-up.sh` supports selkies/novnc/restart/status but has no off operation. Source: [view dispatcher](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:605), [stream script](/home/srayansh/Documents/Golang/Blackburn/macbox/container/stream-up.sh:28).
- Offscreen QEMU marks new frames dirty and does not render/read back its display until a console listener or screendump asks. When watched, frames are copied to QEMU's framebuffer. Source: [installed apple-gfx.c](/home/srayansh/.local/share/macbox/src/reims-vgpu/vendor/qemu/hw/display/apple-gfx.c:105).

### Inferences

Proposed patch sites (not applied):

1. Extend `view` values to `local|stream|private|headless`. Preserve current restart semantics for transitions. Add `MACBOX_STREAM_MODE` to `cenv()` forwarded variable names. For `private`, use off; `stream`, use persisted chosen stream.
2. Replace `cmd_up` display selection with this block, keeping `apply_tuning`, start and affinity code afterward:

```bash
local wd="" window=0
case "$(view)" in
  headless)
    # No display-up call: no Sway, wayvnc, Selkies, or host Wayland socket.
    ;;
  local)
    window=1
    if in_c test -S /tmp/host-wayland/wayland-0; then
      wd=/tmp/host-wayland/wayland-0
      in_c mkdir -p -m 700 /tmp/xdg
    else
      wd=$(in_c env MACBOX_STREAM_MODE=off "$REPO/container/display-up.sh" |
        sed -n 's/^WAYLAND_DISPLAY=//p')
    fi
    ;;
  private|stream)
    window=1
    local stream_mode=off
    if [ "$(view)" = stream ]; then
      stream_mode=$(cat "$RUN/stream" 2>/dev/null || echo selkies)
    fi
    wd=$(in_c env "MACBOX_STREAM_MODE=$stream_mode" "$REPO/container/display-up.sh" |
      sed -n 's/^WAYLAND_DISPLAY=//p')
    ;;
  *) die "unsupported view mode: $(view)" ;;
esac
if ! vm_running; then
  ctl set "$MACBOX_VM" --env "REIMS_VGPU_WINDOW=$window" \
    --env "WAYLAND_DISPLAY=$wd" >/dev/null
  apply_tuning
  ctl start "$MACBOX_VM"
fi
```

For production, print mode-specific status and have `cmd_status` detect headless explicitly. Do not print a browser URL when no stream runs. Host socket detection must be gated by selected mode to avoid a stale mounted socket selecting local accidentally. Environment changes affect the next VM start.

3. Reviewable standalone proposals are saved beside this report: [display-up-proposal.sh](examples/display-up-proposal.sh) and [stream-up-proposal.sh](examples/stream-up-proposal.sh). They preserve current startup logic but default streaming to off, launch wayvnc only for noVNC, and add stream off. `display-up-proposal.sh` needs `BLACKBURN_STREAM_SCRIPT` to name the companion proposal or, after integration, the actual stream-up.sh. For late noVNC startup call display-up with `MACBOX_STREAM_MODE=novnc` so wayvnc is created. For Selkies call it with `MACBOX_STREAM_MODE=selkies`. Stream off stops encoder/WebSocket servers and leaves Sway plus Reims' connected window alive. These are proposal files, not installed commands. `bash -n` passed for both.

Expected resulting interface after applying the patch:

```bash
macbox view private       # restart once into private Sway, stream initially off
macbox up
# Run simctl over SSH and attach debugger; no browser required.
macbox stream selkies     # start live viewer; stream command must call display-up
macbox stream off         # stop encoder; keep VM and Sway alive
macbox view headless      # restart once into no-host-compositor mode
```

True-headless late viewing options:

- Safest no-reboot inspection: QMP screendump (`macbox screenshot`) or `xcrun simctl io UDID screenshot` over SSH. Guest simulator screenshots avoid copying the whole macOS desktop.
- Live viewing without compositor: configure QEMU `-vnc unix:/tmp/macbox-vnc.sock` at initial start with `REIMS_VGPU_WINDOW=0`, then start `websockify --web /usr/share/novnc --unix-target=/tmp/macbox-vnc.sock 6080` only when wanted. Keep the socket inside the container; Docker already binds host 127.0.0.1:6081 to container 6080. The Unix-target syntax is supported by [websockify's option parser](https://github.com/novnc/websockify/blob/master/websockify/websocketproxy.py). This uses QEMU's GPU-to-CPU framebuffer readback path, so expect a different performance profile from private Sway/VA-API, not zero-copy streaming. Preserve existing network/disk extra args when adding VNC: `--extra-args` replaces the full list.
- Do not claim an SDL/Reims window can be attached later by exporting WAYLAND_DISPLAY: `s->window` is selected during GPU realization. The existing API/config has no hot-toggle for it. Starting Selkies later cannot capture a window that was never created. Use a guest remote-desktop service, preconfigured QEMU VNC, or reboot to private display mode.

For CI boot the provisioned VM, wait for SSH with a bounded timeout, use `simctl bootstatus UDID -b` after booting the exact simulator UDID, install/launch/test via CLI and archive simulator screenshots/logs. `open -a Simulator` is optional for CLI simulator operations; do not log out the guest GUI user or stop WindowServer to save host overhead. Whether this particular VM completes these steps without a visible host window needs the acceptance run below.

### Gaps

Samples have syntax validation only. The noVNC direct-QEMU route needs a build with VNC support (`qemu -display help` and `-vnc help`), functional Reims console capture, and runtime validation. It is a fallback design, not a demonstrated fast streaming implementation. A true zero-copy headless encoder would require a new Reims frame-export/encoder integration and explicit synchronization/lifetime handling; no such integration was implemented here.

## Performance and acceptance measurements

### Takeaway

Measure the three modes with the same guest workload before claiming a numerical improvement. No-host-compositor does not mean no guest rendering.

### Cited Findings

- Reims has per-display frame statistics through `REIMS_VGPU_STATS=1`, already enabled by macbox at creation. Sources: [macbox creation](/home/srayansh/Documents/Golang/Blackburn/macbox/macbox:294), [upstream README](https://github.com/steelbrain/reims-vgpu#environment-overrides).
- Host capture and encode have separate processes from QEMU, allowing mode comparison by process and GPU-engine measurements. Source: [display-up.sh](/home/srayansh/Documents/Golang/Blackburn/macbox/container/display-up.sh:13), [stream-up.sh](/home/srayansh/Documents/Golang/Blackburn/macbox/container/stream-up.sh:43).

### Inferences

Acceptance procedure (proposed, not executed):

1. Record kernel, GPU PCI ID, Mesa Vulkan driver, QEMU build ID, guest/runtime versions, vCPU/RAM/resolution, CPU governor and AC power state. Meteor Lake Core Ultra 155H is not Arrow Lake: do not force the repository's `M2V_ARL_FACTS` tuning on this laptop.
2. Compare local host-Wayland, private Sway stream off, private Sway stream on with one client, and true headless. Use identical Simulator UDID/app, booted warm guest, 60-second warmup then a 120-second sample. Include idle, animation, app launch and hot reload.
3. Capture `pidstat -u -r -p PID 1 120` for QEMU, Sway, Selkies, wayvnc and host GNOME Shell where applicable; `docker stats --no-stream macbox`; available Intel `intel_gpu_top` render/video engine and power metrics (requires host permissions); app launch/test wall-clock; guest process RSS; actual viewer delivered frame rate/latency. Save CPU/GPU thermal data to interpret throttling.
4. Headless acceptance: start from a shell with host DISPLAY/WAYLAND_DISPLAY unset; prove `docker exec macbox pgrep -a 'sway|wayvnc|selkies'` has no results, QEMU environment has REIMS_VGPU_WINDOW=0, no host Wayland socket is mounted, SSH responds, simctl bootstatus completes, app renders a nonblank simulator screenshot, debugger/hot reload work. `pgrep` normally matches short process names; inspect full command lines for Python Selkies as well.
5. Private display acceptance: show a stream after minutes of CLI use without restarting QEMU; stop it and verify the same QEMU PID and booted Simulator survive. Do not kill Sway while Reims uses its Wayland socket; it is not equivalent to turning off streaming.

### Gaps

No measured GNOME overhead, FPS uplift, idle-power reduction or guaranteed headless performance percentage is available from this session. Do not reuse the existing skill's claimed 60–96 FPS as a measurement of these designs.
