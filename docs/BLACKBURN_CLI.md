# Blackburn CLI

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
values take precedence. Existing legacy files are preserved; onboarding creates only a missing minimal
provider config. The provider continues to own
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

## IDE configuration and F5 preflight (Phase 4)

Install the Dart/Flutter VS Code extensions, the Dart SDK, and the simulator-capable
`xcross` executable on the Linux extension host's PATH. `blackburn` must also be on
that PATH. VS Code Remote uses the remote Linux extension host's installation.
Use this checkout's xcross implementation: its DAP forwards launch environment
variables, consumes simulator flags from `args`, and recognizes the simulator's
forwarded `vm-service:` marker. An older installed xcross may lack these changes.

```bash
blackburn ide config --dir=/path/to/flutter_app --mode=headless
blackburn ide config --dir=/path/to/flutter_app --mode=headless --dry-run
blackburn ide f5 --dir=/path/to/flutter_app --mode=headless --dry-run
blackburn ide f5 --dir=/path/to/flutter_app --mode=headless --json
blackburn ide status --dir=/path/to/flutter_app --json
```

`--dir` defaults to the current directory, which must contain a Flutter
`pubspec.yaml`. Mode aliases and config/environment precedence match `up`.
`config` generates `.vscode/launch.json`, `tasks.json`, `settings.json`, and
`blackburn_dap.dart`. It merges the named Blackburn launch/task entries, retains
other configurations and settings, and preserves extra fields in managed entries.
JSONC comments and trailing commas are accepted. Changed files are normalized to
JSON, with the first original retained as `.blackburn-backup` (including comments).
Malformed files, symlinks, non-array task/configuration lists, existing unrelated
adapter settings, and an unrelated file at the shim path fail before writes.
Resolve adapter-setting conflicts explicitly before retrying. Generation is
idempotent; changing `--mode` updates the managed task and launch environment.
Dry-run emits a JSON plan without writes, provider calls, leases, or builds.

Open the Flutter project folder in VS Code. Select **Blackburn: iOS Simulator
(xcross DAP)** in Run and Debug and press F5. The launch uses `type: dart` and
`debuggerType: dart` to select the custom adapter without relying on stock Flutter's
Linux iOS device discovery. Both Dart adapter settings point to the generated
source shim, which delegates to `xcross flutter dap`; unmarked launches remain
routed by xcross to its stock adapter path.

The pre-launch task invokes `blackburn ide f5` with the configured mode and project
path. It acquires the CLI operation lock, ensures the VM/profile is compatible,
checks endpoint architecture, verifies the Metal prewarm barrier, leases/boots the
simulator through mac-sim, and waits for `simctl bootstatus` on that explicit UDID.
Provider processes run in the selected project directory so mac-sim's checkout
claim resolves correctly. Profile conflicts return the existing resource/restart
errors; this command never grants restart permission. Direct iOS is rejected.
Preflight diagnostics go to stderr; `--json` emits one launch-result object.
Human output supplies exported environment variables and a shell-quoted run line.

Preflight writes a private `.dart_tool/blackburn/launch.json` containing the leased
device, launch environment, and argv. The shim reads it when the DAP launch arrives,
replaces `__BLACKBURN_LEASED_UDID__`, and applies the resolved endpoint environment.
No UUID needs editing in launch.json. Failed readiness removes the previous launch
record before starting lifecycle work. xcross then owns compilation, deployment,
the app, authenticated loopback VM-service forwarding, and hot reload/restart.
The separate **Blackburn: build simulator** task runs
`xcross flutter build --target-platform simulator --debug` after preflight for
manual build verification; F5 lets xcross's run path perform its own build.

For a remote Mac, configure the verified simulator architecture before offline
configuration generation (Intel `x86_64`, Apple Silicon `arm64`):

```bash
blackburn config set endpoints.lan_mac.ssh_alias my-mac
blackburn config set endpoints.lan_mac.architecture arm64
blackburn ide config --dir=/path/to/flutter_app --mode=hybrid
```

An `auto` architecture is probed by real `ide f5`; offline config/dry-run requires
an explicit architecture. The selected architecture must match the observed Mac
host architecture. Hybrid prepares the remote simulator without allocating local
QEMU RAM or prewarming Apple's VM-specific graphics plugin. Remote stream/viewer
setup remains separate.

The shim forwards DAP frames with byte-counted UTF-8 framing and records DAP
`output` event text under `.dart_tool/blackburn/xcross.log`. This log can include
VM authentication tokens; preflight creates it with mode 0600 and resets it for
the next debug session. `ide status` checks the managed VS Code entries and shim,
parses forwarded `vm-service:` / `BLACKBURN_READY` markers, retains the exact auth
path/query, deduplicates endpoints, and probes their authenticated WebSocket
handshakes. Raw engine HTTP announcements refer to guest ports and are ignored.
Saved endpoints with failed handshakes are reported as unreachable, not active.
Tunnel process ownership is unverified; xcross owns it. Status reports configuration
health separately from an active debug connection and does not boot/prewarm.

Restarting viewer transitions emit versioned JSONL `session.restarting` and
`debug.invalidated` events on stderr with reason, UDID, and `reattach_required`.
Editors can consume these notifications; automatic debugger reconnect and
breakpoint reapplication are not implemented in this CLI slice. Stop debugging
through xcross's adapter; the shim only terminates its own adapter child on
cancellation and never stops the VM or releases a persistent simulator itself.

Tests exercise generated JSONC merges/idempotence, conflict refusal, dry-run,
mocked local/hybrid readiness and failures, forwarded-service discovery and live
WebSocket reachability, plus the generated Dart shim against a fragmented fake
DAP child (when Dart/Python are installed). These checks do not certify real F5
breakpoints, exceptions, source mapping, widget hot reload, Inspector/toolbar
support, network recovery, or xcross app/tunnel cleanup. Validate those on the
installed xcross and each supported mode before claiming Phase 4 certification.
A VS Code session selector/status extension and native LLDB integration remain
separate work; this change supplies the CLI/configuration bridge.

## Laptop onboarding and appliances

```bash
blackburn init                 # inspect host; confirm optimized local profiles
blackburn init --auto          # apply detected defaults without prompting
blackburn appliance export --output=/fast-storage/golden.appliance.zst
blackburn appliance export --compress=none --output=/fast-storage/golden.appliance
blackburn appliance import /downloads/golden.appliance.zst
blackburn appliance import /downloads/golden.appliance.zst --force
```

Onboarding validates Intel amd64 Linux/KVM, Docker and Intel Mesa/ANV Vulkan,
computes guest memory/vCPU defaults and P-core affinity, and prepares user data
and missing provider configuration. It requires a stopped VM and retains existing
endpoint/provider settings. `cpu_set` is a top-level Linux CPU list (empty leaves
provider/scheduler affinity unchanged). `shader_cache_max_size` accepts `2G` or
empty and controls the QEMU Mesa disk shader cache. Local launch revalidates saved
CPU availability and warns on battery power. `init` updates all macbox profiles;
remote profiles and the selected default mode remain unchanged.

Export gracefully shuts down a managed local VM and retires its container;
unmanaged/paused/suspended VMs are refused. The default output is
`./blackburn.appliance.zst`, and existing output files are never overwritten.
Both `zstd` (default) and `none` omit zero disk blocks. Only standalone relative
disk/firmware references and portable QEMU options are supported.

Import verifies all logical-file SHA-256 hashes before installing the VM, retunes
resources for the recipient, and registers through reims-ctl's VM directory. It
never starts the guest. `--force` permits replacing an existing stopped VM,
retaining `<VM>.pre-import`; an existing backup blocks further replacement.
Fresh hosts still need the provider runtime/container and guest SSH access.
Hardware reports live under the Blackburn state directory as `hardware.json`
after `init`. See [laptop optimization and packaging](BLACKBURN_LAPTOP_OPTIMIZATION_AND_PACKAGING.md)
for the host matrix, archive format, benchmark protocol and setup prerequisites.
