# Blackburn CLI — Phase 1 profile entrypoint

The Go control plane wraps the existing macbox and mac-sim installation. It does
not download Xcode, create a VM, install packages, or start services during setup.
Go 1.23+, Bash, flock, Docker, SSH, and the existing provider installation are required.

```bash
mkdir -p ~/.local/bin
ln -s /absolute/path/to/Blackburn/bin/blackburn ~/.local/bin/blackburn
blackburn --help
blackburn mode list
blackburn mode set agent
blackburn doctor --json
blackburn up --mode=headless
blackburn sim up
blackburn sim install ./build/ios/iphonesimulator/Runner.app
blackburn sim run com.example.app
blackburn sim shot ./screen.png
blackburn status --json
blackburn sim down
blackburn down --timeout=120s
```

The checkout launcher builds a cached binary under `$XDG_CACHE_HOME/blackburn`
(default `~/.cache/blackburn`), rebuilding when Go inputs change. It resolves
symlinks and preserves the caller's working directory for mac-sim checkout claims.
Alternatively, `go build -o bin/blackburn-cli ./cmd/blackburn` produces a standalone
binary; set `BLACKBURN_ROOT` to the checkout when installing that binary elsewhere.

## Profiles and configuration

| Preset | Alias | Guest MiB | vCPUs | Presentation |
| --- | --- | ---: | ---: | --- |
| headless | agent | 5120 | 4 | Offscreen; no host compositor |
| stream | | 6144 | 6 | Browser desktop, WebSocket |
| studio | local | 8192 | 8 | Native Wayland desktop |
| hybrid | remote | 0 locally | 0 locally | SSH endpoint; remote video unconfigured |
| direct | | 2048 experimental | 2 experimental | Provider unavailable; fails before launch |

`mode set` changes only the saved default. `up --mode=...` never rewrites it.
An incompatible or unmanaged running VM returns `RESTART_REQUIRED` (exit 6).
Stop it explicitly before applying new resources. No automatic restart or forced
shutdown occurs. After observing QEMU stopped, `down` retires the container so the
next launch applies the selected Wayland socket mounts. Repeated compatible `up` reuses the VM and verifies its prewarm
barrier, including retrying a previously failed prewarm.

Configuration defaults are supplied in memory; the first `mode set` or `config set`
writes `${XDG_CONFIG_HOME:-~/.config}/blackburn/config.toml` atomically with mode 0600.
Unknown keys, schema versions, modes, resource values, and endpoint architectures
fail before provider mutation. A partial profile table inherits its omitted defaults.

```bash
blackburn config show
blackburn config show --effective
blackburn config get profiles.headless.guest_memory_mib
blackburn config set profiles.headless.guest_memory_mib 4096
blackburn config set endpoints.lan_mac.ssh_alias my-mac
blackburn config set endpoints.lan_mac.architecture arm64
blackburn config validate
```

Precedence: built-in defaults → user TOML → environment → command flags.
Project configuration is not loaded in this implementation. Supported overrides:

- `BLACKBURN_CONFIG`: alternate TOML path.
- `BLACKBURN_STATE_DIR`: alternate session/lock/prewarm directory.
- `BLACKBURN_MODE`: default selection; `up --mode` takes precedence.
- `BLACKBURN_GUEST_MEMORY_MIB`, `BLACKBURN_VCPUS`: selected profile resources.
- `BLACKBURN_SSH_ALIAS`: selected endpoint's SSH alias.
- `BLACKBURN_PROFILES_HEADLESS_GUEST_MEMORY_MIB` and equivalent
  `BLACKBURN_PROFILES_<CANONICAL_MODE>_<FIELD>` overrides for every profile scalar.
- `BLACKBURN_ENDPOINTS_LOCAL_SSH_ALIAS` and equivalent endpoint scalar overrides.
- `BLACKBURN_ROOT`: provider script checkout for an installed compiled binary.

User provider settings are read as literal assignments from `MACBOX_CONFIG`, or
`~/.config/macbox/blackburn.env` when present, then `macbox.env`. Shell substitutions
are rejected; no shell is used to parse them. Existing environment `MACBOX_*`
values take precedence. Legacy files are preserved. The provider continues to own
its existing configuration format; this is a reversible bridge, not a migration
command. `MACBOX_HOME`, `MACBOX_CONTAINER`, `MACBOX_VM`, `MACBOX_DOCKER`,
`MACBOX_RENDER_NODE`, and SSH configuration retain their provider meaning.

## Lifecycle, diagnostics, and simulator behavior

Cold local starts require available host memory of guest allocation + 2 GiB runtime
allowance + 3 GiB host reserve. Settings are applied only after observing the VM
stopped. The previous `vm.toml` is retained as `vm.toml.blackburn-backup`.
CPU count, memory, display resolution, QEMU offscreen display, and persistent
`REIMS_VGPU_WINDOW` are updated without replacing other provider flags.
Studio requires the invoking user's Wayland socket and never requests sudo.

Session state, prewarm stamps, and the nonblocking operation lock live under
`${XDG_STATE_HOME:-~/.local/state}/blackburn`. One active endpoint session is
supported per user/state directory. A second endpoint is refused until the first
session is released. Local simulator operations share the existing mac-sim claim
registry; remote endpoints use separate claim directories. Commands retain the
leased UDID when wrapping mac-sim, and `sim run` invokes `simctl launch` for that
explicit UDID rather than invoking mac-sim's Expo build command.

The Metal guard hashes the local VM plugin, verifies its signature through
`blackburn-prewarm-metal.sh`, and enumerates devices before Metal-dependent work.
Successful verification is cached by SSH endpoint, guest boot identity, and plugin
hash. Reboot or plugin changes invalidate the stamp. A failed guard blocks boot,
install, screenshot, and launch. Physical remote Macs use an endpoint-specific
CoreSimulator readiness probe; they do not require Apple's paravirtual GPU plugin.
Status and shutdown operations do not run the guard.

`sim install` checks Mach-O CPU slices and iOS Simulator platform load commands
before copying a local `.app`; device builds and incompatible slices are rejected.
Run commands from the app checkout to share mac-sim's existing checkout claim.
`sim up` prints its UDID. Interactive local modes open Simulator.app; hybrid never
quits another user's Simulator.app or starts local QEMU. Hybrid `down` releases
its leased simulator and leaves the shared Mac running.

`status --json` emits a schema-versioned object containing desired/running mode,
observed VM state, stage, leased and observed active simulator, booted inventory,
viewer intent/transport, host available memory, and QEMU RSS where observable.
Unobservable memory is null. Viewer attachment is explicitly unverified.
`doctor --json` emits one object with `ok` and per-check diagnostics, returning 3
on unmet prerequisites. Local checks cover read/write KVM/render access, Vulkan
ICD discovery via `vulkaninfo`, Docker/container, SSH architecture, selected Xcode,
simctl, SDK inventory, and runtime inventory. Doctor does not install or repair.

Exit classes: 0 success, 2 arguments/config, 3 unmet prerequisite/unsupported
capability, 4 resource conflict/admission, 5 provider operation failure, and 6
restart required. JSON errors carry `schema_version`, `error.code`, and
`error.message`; provider logs stay on stderr for structured commands.

## Validation and current boundaries

```bash
go test -race ./...
go vet ./...
bash -n bin/blackburn scripts/blackburn-prewarm-metal.sh macbox/macbox \
  macbox/sims/bin/mac-sim macbox/sims/bin/mac-sim-remote
```

Tests use temporary configuration/state and injected provider responses, including
architecture fixtures, readiness failures, locking, mode conflicts, cold-start
settings, and hybrid isolation. They do not restart the workstation's real VM.

This entrypoint is the first CLI/profile slice of the larger Phase 1 roadmap.
The daemon, operation journal, cross-client device lease service, compatibility
lock certification, disk admission, watchdog, explicit migration command, remote
video setup, and direct iOS provider remain future work. Existing mac-sim provides
the simulator pool; this CLI does not provision or certify slim/1x templates.
Native window-close detach safety and real offscreen rendering/hot-reload
certification remain the plan's hardware acceptance gates. Doctor inventory alone
is not proof of toolchain or rendering compatibility. A failed start retains the
VM disk, settings backup, and last successful session stage for diagnosis; automatic
rollback/recovery is not implemented. Graceful shutdown timeout always retains the VM.

## Independent viewers (Phase 2)

```bash
blackburn view list
blackburn view get
blackburn view set stream
blackburn view set native --allow-restart
blackburn view detach
```

Targets are `none`, `browser-desktop` (`stream`), and `native` (`studio`, `local`).
`set` changes presentation on the current managed local session and retains its
memory, vCPU, display resolution, endpoint, and simulator lease. It does not apply
another mode's resource preset or change the saved default mode. Remote and direct
providers currently reject viewer control. `get` and `status` read provider state;
client connection counts and actual delivered frames are not inferred from it.

Detach and reattach to the existing compositor are live operations. Detaching keeps
QEMU, CoreSimulator, private Sway, and the stream service alive. Browser connections
do not own those processes; the supported video transport remains WebSocket.
Switching between native and private Sway, or realizing an offscreen window,
returns `RESTART_REQUIRED` (exit 6) unless `--allow-restart` is supplied. The planner
also treats memory/vCPU/display changes as restart-only operations.

A restart shuts down the leased simulator and guest gracefully, waits for QEMU to
stop, applies settings, prewarms Metal, then boots and waits for the same UDID.
Installed apps and device data persist; app process memory and debugger connections
are interrupted. Restart completion does not automatically relaunch an app because
Phase 1 does not record its bundle identifier or own the debugger. Run `sim run`
and reconnect the debugger as needed. A shutdown timeout retains the VM without
forced termination. A later startup failure attempts one recovery to the previous
profile. The operation lock serializes changes; `view-transition.json` records
previous/desired state, restart reasons, completion, and rollback errors. Failed
operations retain this record for diagnosis. An interrupted CLI is not automatically
replayed by a daemon in this synchronous implementation.

Native presentation requires the patched Reims and QEMU build (`macbox build`),
which installs a `detach-v1` capability marker. Older builds are rejected before
native startup. The running VM's capability is recorded separately so replacing
`qemu/current` cannot certify an already-running older process. Under the detach
policy, native close releases input, disables presentation through renderer
visibility, and hides the SDL window on its UI thread. It retains the surface and
GPU device. The Apple graphics close callback also suppresses host-UI shutdown.
Reopening the same native viewer shows that retained window. Compositor loss and
safe live migration to a different compositor remain hardware validation work.

Validation here covers CLI/provider fault simulations, the executable C shutdown
callback guard, patch application, and Rust compilation. Native close with a real
app/debug tunnel, host Wayland loss, Sway failure, encoder reconnect, and graphics
soak tests require a rebuilt hardware VM before studio can be certified. The CLI
capability marker identifies the patch set; it does not claim hardware certification.

## Experimental simulator-only viewer (Phase 3)

```bash
blackburn up --mode=headless
blackburn sim up
blackburn sim stream --port=6082 --fps=30
# Open http://127.0.0.1:6082/ in a browser.
```

`sim stream` serves an HTML5 canvas over HTTP and WebSocket on **loopback only**.
It captures the leased CoreSimulator screen directly, without launching
Simulator.app or capturing the macOS desktop. The iOS status bar/system UI may
still appear: “app-only” means simulator framebuffer rather than desktop capture,
not cropping iOS content. Stop with Ctrl+C. Stopping the streamer, closing the
browser, or losing its connection does not shut down the VM, app, or simulator.
The browser reconnects automatically. The command observes the simulator lease
without holding the CLI operation lock, so other interaction commands remain
available. Lease loss invalidates capture and input; restart the stream after
acquiring a new lease.

The default backend polls `xcrun simctl io UDID screenshot --type=png -` over SSH.
`--fps` is a **capture rate limit**, between 1 and 60, not a promised delivery rate.
Screenshot process startup, readback, PNG encoding, SSH, and browser decoding
usually prevent 30 FPS. This is a functional screenshot fallback, not a certified
low-latency display subscription, hardware encoder, H.264 stream, or zero-copy
path. The existing desktop viewer remains available through `blackburn view`.
No view transition happens automatically.

The default backing scale is 1 for `slim-1x` devices. Use `--scale=2` or `--scale=3`
when the simulator's screenshot pixels are 2x or 3x its logical point dimensions.
The server derives the current oriented image dimensions from PNG headers and
maps canvas coordinates through aspect-fit letterboxing and the selected scale.
Rotation, dimension changes, or capture failure/recovery advance the stream epoch;
input referring to an earlier epoch is rejected. Viewport coordinates are CSS
pixels, independent of the browser's device pixel ratio.

Click/touch the canvas to tap, drag to swipe between the gesture endpoints, and
focus the canvas to type. Drag is forwarded as one simulator swipe on pointer
release, not a continuous multi-touch HID subscription. The text field sends
Unicode text to the simulator's focused field. Home, Enter/Return, Backspace,
Tab, and individual characters are forwarded through agent-device; other key names
return an unsupported-key error. Keyboard behavior depends on the guest backend
and focused control, and agent-device failures are displayed in the viewer.

```bash
blackburn sim touch 120 240              # logical simulator points
blackburn sim tap 'label="Save"'         # agent-device selector or snapshot ref
blackburn sim tap @e1
blackburn sim key home
blackburn sim key Enter
blackburn sim text 'Hello from Linux'
```

These commands require an active Blackburn simulator lease (`sim up`) and the
repository's `mac-sim` / `mac-sim-remote` agent-device setup on the selected guest
(`macbox/sims/bin/mac-sim setup`). Input uses that bridge's pinned agent-device
version (currently 0.21.31), selected UDID, and isolated session. It does not invent
`simctl touch` or `simctl key` commands. Selectors and snapshot refs follow
[agent-device's command contract](https://agent-device.dev/docs/commands).

Each viewer has a queue of at most **two frames**, dropping the oldest pending
frame on overflow. Browser presentation acknowledgements permit only one frame
in flight per socket; a nonresponsive viewer is disconnected after ten seconds.
Capture pauses when no viewers are connected. The first eligible connection gets
control; other viewers are read-only. A controller disconnect releases control;
a waiting viewer reconnects to acquire it. CLI input is separately authorized by
the local user and does not participate in browser controller arbitration.

The HTTP listener validates loopback Host and same-origin requests. Generated
video/control credentials are separate, scoped to this server process, and
embedded in its local viewer. Do not publish this listener through a reverse
proxy: LAN/TLS authentication and remote controller credentials are not implemented.
The capture path never reads or writes the clipboard.

Endpoints and wire contract:

- `GET /`: embedded viewer; no external assets or dependencies.
- `GET /health`: version, experimental flag, PNG codec, FPS limit, viewer count,
  current sequence/epoch, queued/dropped frame counts, and last capture error.
- `GET /frame?token=VIDEO_TOKEN`: latest PNG; 503 until a viewer causes capture,
  or while capture has failed. Credentials come from the local viewer page.
- `GET /ws?token=VIDEO_TOKEN&control=CONTROL_TOKEN`: protocol v1 JSON messages.
  `hello` identifies controller ownership. `frame` contains base64 PNG, device,
  oriented backing width/height, scale, rotation, epoch, sequence, and host-clock
  capture/delivery timestamps. The client sends `{"type":"ack"}` after rendering
  and `{"type":"ping"}` at least every 30 seconds. These timestamps are not
  guest GPU capture/encode or browser presentation measurements; no cross-host
  clock equivalence is assumed.
- Input messages are `{"type":"input","input":{"type":"touch","epoch":1,
  "x":25,"y":50,"view_width":100,"view_height":200}}`. `drag` also has `end_x`
  and `end_y`; `tap`, `key`, and `text` carry their corresponding named field.
  Input replies contain `ok` and an `error` when routing fails.

To experiment with faster acquisition, set `BLACKBURN_FRAME_HELPER` to the absolute
path of an installed **guest executable**. Its contract is `HELPER frame UDID`,
returning one PNG on stdout and diagnostics on stderr; a helper can keep a
persistent guest CoreSimulator display subscription behind that interface.
Frames must already be oriented, match the selected backing scale, and remain
below 32 MiB / 16384 pixels per dimension. If the helper fails or emits an invalid PNG, it is disabled for the rest of the
process and capture falls back to simctl. The backend and warning are reported in
frames, `/health`, and the viewer. Restart the streamer to retry the helper. A
command timeout is retried on the next capture. With no helper configured, simctl
is used directly. Private display APIs
stay outside the host Go process. No private API helper or compatibility matrix is
bundled or certified by this implementation.

Validation covers bounded queue overload, concurrent producers/consumers, 1x/2x/3x
coordinate mapping, rotations/letterboxing, guest command construction, CLI
validation/device selection, HTTP/WebSocket authorization, input routing,
controller contention, stale epochs, capture recovery, reconnect, and server
cancellation. Run `go test -race -count=1 ./...`.

Phase 3 remains **experimental** until live Simulator.app-closed navigation,
30-minute soak/network-interruption recovery, helper/Xcode/runtime compatibility,
and measured RAM/CPU/frame-pacing comparisons against Phase 2 are archived. Unit
and integration tests with a simulated bridge do not certify those release gates.
