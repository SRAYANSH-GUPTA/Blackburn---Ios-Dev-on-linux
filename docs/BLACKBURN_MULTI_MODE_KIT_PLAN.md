# Blackburn Multi-Mode Developer Kit — Architecture & Implementation Plan

**Status:** Proposed implementation contract; not a declaration of shipped capabilities.  
**Baseline:** Working tree inspected on 2026-11-06; Intel Linux / Meteor Lake reference host.  
**Audience:** Runtime, graphics, toolchain, CLI, IDE, release, and QA engineers.  
**Primary deliverable:** A versioned `blackburn` toolkit with independently managed execution, presentation, compilation, and debugging.

## 1. Vision and scope

Blackburn will provide one supported entry point for iOS development from Linux: select an operational profile, launch its execution endpoint, compile an architecture-correct app, deploy it, inspect its UI, and debug it from the same workstation. Developers should choose how much local RAM and presentation overhead to spend without reconstructing QEMU, Docker, SSH, simulator, and Flutter commands themselves.

The core value is **a consistent development session across hardware constraints**: background work on a 16 GB laptop, browser QA, high-performance desktop interaction, execution on a LAN Mac, and eventually direct iOS emulation. The toolkit owns readiness, configuration validation, resource admission, simulator allocation, lifecycle recovery, and reproducible diagnostics. Individual backends remain replaceable.

“Full iOS development” is the product direction, with capability-qualified support. The initial supported path is Linux-built Flutter debug/JIT apps deployed to a compatible simulator. Native Swift/Objective-C projects may delegate `actool`, `ibtool`, Core Data compilation, signing, or Xcode build steps to macOS. Native debugger integration and distribution builds require separate validation. Physical-device deployment, App Store submission, and universal Linux support for arbitrary Xcode projects are not implied by simulator success.

### 1.1 Product requirements

1. All five modes share CLI verbs, configuration, status, device identities, artifact manifests, and structured errors.
2. Closing a viewer must never terminate the execution backend. Explicit lifecycle commands own shutdown.
3. No command reports readiness solely because a container or SSH socket exists.
4. Profiles describe desired resources; the toolkit reports observed resources and compatibility separately.
5. A mode change either preserves the running session or states exactly which parts must restart before changing anything.
6. Recovery must preserve persistent disks, simulator data, project files, and credentials. Destructive cleanup requires an explicitly named operation.
7. CI and IDE clients use structured interfaces rather than scrape human console output.
8. Installation and upgrades pin compatible component sets and support rollback.

### 1.2 Evidence and terminology

The [operational manual](BLACKBURN_IOS_DEVELOPMENT_MANUAL.md), [headless design report](../reports/Headless%20macbox%20and%20IDE%20integration.md), and [performance report](../reports/Macbox%20Meteor%20Lake%20performance.md) establish the local baseline. This plan adds design decisions and acceptance targets, not new runtime measurements.

| Item | Baseline inspected | Required product behavior |
| --- | --- | --- |
| Local and browser presentation | `macbox view local\|stream` exists; switches call down/up | Independent viewer lifecycle; transactional switches |
| Browser transport | Selkies H.264 over WebSocket, host loopback port 6081 | Preserve working transport; add validated WebRTC option |
| Native close button | QEMU Apple graphics close callback requests host-UI poweroff | Detach/hide presentation; never request shutdown |
| True headless | Source-supported `-display none` and persisted `REIMS_VGPU_WINDOW=0`; launcher integration pending | Shipped headless backend with app-rendering acceptance tests |
| Low-RAM presets | 4–5 GiB optimization objective | Published workload-specific evidence before certification |
| Simulator slimming | Historical baseline of 168 disabled services | Versioned service manifest, actual count, reversible overrides |
| Flutter DAP | Working tree contains simulator `vm-service:` emission and DAP recognition | Audit existing fixes, then prove F5, breakpoints, reload, and cleanup |
| Direct iOS | Future research path | Experimental provider until installation, graphics, and debugging pass gates |

Guest RAM is specified in **GiB** in configuration. Requested product labels such as “4–5 GB” map to these binary allocations. Approximate host footprints are planning targets for the runtime stack, not hard limits or whole-workstation consumption. Producer FPS, delivered FPS, and input latency are separate measurements. “Zero latency” means removing network/video transport stages in studio; literal zero latency is impossible.

## 2. Mode taxonomy and technical specifications

Separate three axes internally even though users select one preset:

- **Execution:** local macOS VM, remote macOS endpoint, or experimental direct iOS VM.
- **Presentation:** none, browser desktop, native desktop, or future simulator-only viewer.
- **Workload:** slim app development, full-system QA, native build, or CI automation.

Canonical mode names are `headless`, `stream`, `studio`, `hybrid`, and `direct`. `agent`, `local`, and `remote` normalize to `headless`, `studio`, and `hybrid`; aliases never create separate profiles or state directories.

### 2.1 Preset matrix

| Mode | Guest allocation | Host runtime footprint objective | Initial vCPUs | Presentation | Primary use |
| --- | --- | --- | --- | --- | --- |
| `headless` / `agent` | 4–5 GiB; default 5 GiB, 4 GiB opt-in | Approximately 4.5 GiB only as a measured low-RAM workload target | 4 | No host compositor, no Simulator.app | CI, agents, background app work on 16 GB laptops |
| `stream` | 6 GiB | Approximately 6.5 GiB target, excluding browser and builds | 6 | Private headless Sway plus Selkies, host port 6081 | UI testing, QA, remote browser work |
| `studio` / `local` | 8 GiB | Measure 8 GiB guest plus graphics/control overhead; no fixed total promised | 8 | Native SDL3/Vulkan window on host Wayland | Local interactive development and frame pacing |
| `hybrid` / `remote` | None locally; remote capacity inventoried | Zero local **VM** RAM; compiler, viewer, and IDE still consume RAM | Remote policy | Remote simulator stream, SSH control | Reuse a MacBook Air or Mac mini on LAN |
| `direct` | Approximately 2 GiB starting experiment, provider-dependent | Approximately 2.5 GiB total target, unverified | Provider/TCG-dependent | Direct iOS framebuffer, future browser/native viewer | Future lightweight execution without macOS |

A 5 GiB configured guest can exceed 4.5 GiB host residency under load. The headless footprint objective is therefore not a guarantee for every 4–5 GiB allocation. Publish allocation, PSS, cgroup usage, GPU allocations, and workload together. Never cap the container at a marketing footprint to force a result.

### 2.2 Mode 1 — headless / agent

**Execution path:** Linux xcross → staged x86_64 simulator app → SSH → macOS CoreSimulator. Guest GPU work remains Metal → Reims → metal2vulkan → Vulkan → Linux render node. The guest retains its graphical login session, WindowServer, SpringBoard, and backboardd; headless removes host presentation, not iOS graphical services.

Required settings and behavior:

- Persist `REIMS_VGPU_WINDOW=0` in the VM's `extra_env`; shell export alone is insufficient because reims-ctl filters presentation environment variables.
- Select QEMU `-display none`; bypass `display-up.sh`, Sway, wayvnc, Selkies, and desktop capture entirely.
- Begin with a dummy 640×480 guest display if offscreen acceleration requires an active display. A no-display variant is enabled only after CoreSimulator boot, Metal rendering, and screenshot tests succeed. Guest desktop resolution and simulator backing-pixel density are independent controls.
- Do not launch Simulator.app. Allocate one named slim pool device with true 1x backing pixels, verified from screenshot dimensions; resizing a viewer does not establish 1x rendering.
- Pin a simslim version and service allow/deny manifest. Target the historical 168-service reduction where supported; derive the actual disabled list per runtime. Never disable critical graphics, input, or debugger services to hit a count.
- Use `simctl` and a guest automation helper for install, launch, screenshot, logs, and supported input. QMP controls the macOS desktop, not a substitute for semantic simulator automation.
- Initial Linux compilation concurrency: two jobs; serialize expensive cold shader/build work with simulator boot on constrained hosts.

Headless CI uses an ephemeral job-owned simulator cloned from an approved seed when capacity allows. Persistent agent sessions lease a dedicated device. Full-system tests may request `sim.profile=full`; this increases the memory admission estimate and never silently changes a slim device shared by another session.

### 2.3 Mode 2 — stream

Run QEMU in a Docker-managed runtime, with Reims presenting to private headless Sway. Host GNOME, Xorg, and physical monitor ownership are unnecessary. The capture/encoder service serves `http://127.0.0.1:6081/` by default; browser clients are disposable.

The desired experience is Selkies H.264 with an optional WebRTC transport. The checked-in implementation uses WebSocket video. Phase 2 must capability-probe the pinned Selkies release and implement/validate WebRTC signaling, ICE, candidate ports, and optional TURN before advertising WebRTC. HTTP port 6081 alone does not describe WebRTC media reachability. Maintain WebSocket as the supported fallback, explicitly reported by status.

- Default 6 GiB, one slim 1x simulator, 1280×720 desktop capture at up to 60 delivered FPS; make 1920×1080 a measured quality option.
- Use VA-API on the validated Intel render node; report hardware versus software encoding. Adapt bitrate and frame delivery to avoid an unbounded queue.
- Until Phase 3, desktop streaming normally needs Simulator.app visible to show the iOS UI. Its frontend overhead is included in stream benchmarks.
- Keep private Sway and the Reims presentation connection alive when the last browser tab disconnects. Pause capture/encoding after an idle grace period, not QEMU or the simulator.
- Reconnect to the same session/device. Resolve controller ownership explicitly; allow spectators, but serialize input to one controller.
- Expose LAN access only through an authenticated TLS endpoint or SSH tunnel; preserve a loopback-only default.

Browser reload, tab closure, encoder crash, and client network failure must leave the VM and app alive. A Sway crash is more serious because it owns the current Reims connection; report it separately from an encoder failure.

### 2.4 Mode 3 — studio / local

Default to 8 GiB, eight vCPUs, and the validated Intel Arc Vulkan device. Native SDL3 presentation uses the invoking user's Wayland socket and `SDL_VIDEO_DRIVER=wayland`; the CLI must not require `sudo`, which breaks session environment and ownership. Keep the selected render node and Vulkan physical-device identity consistent on multi-GPU systems.

The performance objective is **60–96 producer FPS in validated representative workloads**, with presented FPS constrained by display refresh. Published reference results around 95 FPS describe a 1x simulator benchmark on different Intel hardware; they are not a Meteor Lake guarantee. Record p50/p95/p99 frame intervals, displayed FPS, and input-to-display latency. Native presentation removes encode/decode/network stages but still has guest, GPU, compositor, and scanout latency.

Window-close protection is a prerequisite for calling this mode production-ready:

1. Patch the Reims/QEMU close-event boundary so `SDL_EVENT_WINDOW_CLOSE_REQUESTED` and the associated Apple graphics callback cannot request `qemu_system_shutdown_request(SHUTDOWN_CAUSE_HOST_UI)` under Blackburn's detach policy.
2. Separate renderer/device lifetime from SDL surface/window lifetime. Queue presentation changes on their owning threads; quiesce surface work and release input grabs before detaching.
3. Emit a `viewer.closed` event to the supervisor and hide/detach presentation. Preserve guest GPU execution, the QEMU PID, simulator UDID, app PID, and debug tunnel.
4. Prefer stream demotion only when a compatible independent presentation endpoint already exists. Otherwise remain safely backgrounded and report `view=none`; starting private Sway alone cannot migrate the existing native window.
5. Offer an explicit `blackburn down` action for shutdown. A legacy unpatched close path must fail the studio capability gate, with stream available as the safe interactive preset.

Preserving the VM on native window closure is mandatory; live transfer to another compositor is a separate feature. Do not implement “demotion” by issuing today's `macbox view stream`, which restarts the VM.

### 2.5 Mode 4 — hybrid / remote

Compile locally with xcross and execute on a selected remote macOS host. No local QEMU, Docker runtime, or reserved guest RAM is started. SSH carries control, artifact transfer, and debugger tunnels; the UI uses an independently managed simulator/desktop stream.

Inventory the endpoint rather than infer its architecture from the Linux host. Intel Macs generally need `x86_64-apple-ios-simulator`; Apple Silicon endpoints need a compatible `arm64-apple-ios-simulator` artifact. Validate architecture against the installed runtime and toolchain. Never copy an x86_64 simulator app to an ARM device runtime or assume local cross-compilation dependencies support every ARM simulator configuration.

- Set `XCROSS_SIMULATOR_HOST` to a verified SSH alias and `XCROSS_SIMULATOR_ARCH` from endpoint capabilities. Probe local compiler/SDK support before compiling.
- Use per-session staging directories and rsync preserving symlinks/executable modes; publish a content hash and architecture manifest.
- Acquire a simulator lease for the selected macOS user. Avoid helpers that quit Simulator.app globally on a shared Mac. Never reboot or shut down a shared remote Mac as part of a mode change.
- Discover the Dart VM-service port dynamically, bind forwarding to Linux loopback, retain its authentication URI, and let xcross own incremental compilation/reload.
- Recover SSH transport with bounded retries; a changed remote app PID or service URI requires debugger reattachment rather than pretending the old connection survived.
- Probe remote streaming availability. SSH alone provides no live video. If no viewer provider is installed, advertise screenshots/automation only and give an explicit setup requirement for browser UI work.
- Maintain per-project source-path mapping for DAP and separate remote toolchain identities from local SDK identities.

Generic native Swift builds can delegate missing proprietary tools to the remote Mac, with their cost and logs visible. Native Swift does not gain Dart hot reload; its supported loop is build/install/relaunch plus native debugger attachment when available.

### 2.6 Mode 5 — direct, experimental

Define a distinct provider for direct QEMU iOS execution, initially evaluating vphone-linux. Keep device-model names as provider capabilities: legacy `t8030`, possible `t8130`, and newer virtual research models are not interchangeable aliases. Validate exact firmware/model/QEMU combinations; do not advertise a model because a project name mentions it.

This path bypasses macOS and CoreSimulator. On Intel Linux, ARM64 iOS CPU execution requires translation/emulation rather than the x86_64 KVM path used by macbox. Legacy Inferno/t8030 software-rendered configurations and newer GPU-enabled vphone implementations need separate performance and compatibility matrices.

- Require an ARM64 **device** artifact, correct ABI/deployment target, and provider-supported signing/entitlements. Simulator `.app` bundles are incompatible even when both architectures happen to be ARM64.
- Abstract install/launch/log/input/framebuffer behind the provider interface; discover any `vphoned`/vsock protocol and authentication from the pinned provider before enabling it.
- Treat reims-vgpu/metal2vulkan acceleration, approximately 2.5 GiB total RAM, fast install, and reload as experimental objectives until measured locally.
- Verify JIT/debug entitlement behavior independently; if Flutter debug/JIT cannot run, report hot reload unsupported and evaluate AOT rebuild/relaunch. Do not infer CoreDevice or simctl support.
- Isolate image provisioning, firmware provenance, persistent data, and transport permissions from macbox. No automatic migration of CoreSimulator device data into direct iOS images.

`blackburn up --mode=direct` requires the experimental feature flag and a passing provider-specific doctor. Failure must leave stable macOS profiles untouched.

## 3. Architecture and component boundaries

### 3.1 Control and data planes

```text
Linux user / CI / VS Code
          |
     blackburn CLI ---------------- configuration + component lock
          |
     blackburnd (per-user supervisor; Unix socket; operation journal)
          |
          +-- Session / lease / admission / health / transition managers
          +-- Build coordinator ---- xcross / native macOS tool adapter
          +-- Debug coordinator ---- xcross DAP / future native DAP
          +-- View manager --------- stream / native / simulator framebuffer
          |
          +-- ExecutionProvider
                 +-- MacboxProvider ---- Docker -> QEMU/KVM -> macOS
                 +-- RemoteMacProvider - SSH -------------> macOS
                 +-- DirectIOSProvider - QEMU/TCG --------> iOS

App artifacts -> validated staging -> provider install/launch
Frames -> bounded capture/encode -> authenticated viewer
Input -> controller lease -> provider input adapter
Dart VM service -> authenticated SSH tunnel -> xcross/DAP
```

Implement the control plane in Go, with a TOML parser, JSON schemas, bounded subprocess execution, and a single distributable Linux binary. Package `blackburnd` as a subcommand of the same binary (`blackburn daemon serve`) rather than require another installed executable. Reuse the repository's Bash provisioning entry points initially; migrate their lifecycle logic behind typed adapters incrementally. Keep Dart/x cross-compilation ownership in xcross rather than port its build engine into Go.

This is an implementation choice for the new kit, not an assertion that a Go control plane already exists. Validate maintainability with the project team during Phase 1, without coupling external protocols to the implementation language.

### 3.2 Provider contract

All operations take a session ID, deadline, cancellation token, and operation ID. Stable methods are:

| Interface | Operations and required result |
| --- | --- |
| Execution | `Probe`, `EnsureUp`, `GracefulDown`, `Inspect`; endpoint identity, actual state, architecture |
| Simulator/device | `List`, `Lease`, `Boot`, `WaitReady`, `Install`, `Launch`, `StopApp`, `Screenshot`, `Release` |
| Presentation | `Capabilities`, `Attach`, `Detach`, `SetQuality`; transport, view ID, controller owner |
| Debug | `DiscoverService`, `Forward`, `InspectTunnel`, `CloseTunnel`; local authenticated URI |
| Resources | `Estimate`, `Observe`, `ApplyStoppedConfig`; effective quota and restart requirement |
| Recovery | `CollectDiagnostics`, `RecoverLayer`; bounded action with explicit affected sessions |

A direct provider maps “sim” verbs to device equivalents while returning `device_kind=emulated-ios-device`. Unsupported capabilities fail before mutation. Provider processes are supervised by identity and process group/cgroup, never container-wide name matching or broad `pkill`.

### 3.3 Supervisor and persistent state

Use a per-user systemd service when available, with a foreground fallback for CI. A browser, CLI terminal, or VS Code process must not own QEMU lifetime. CI explicitly requests `--ephemeral` and a cleanup policy; interactive sessions survive their launching terminal.

- Configuration: `${XDG_CONFIG_HOME:-~/.config}/blackburn/config.toml`.
- Component cache and images: `${XDG_DATA_HOME:-~/.local/share}/blackburn/`.
- Durable session state/journal: `${XDG_STATE_HOME:-~/.local/state}/blackburn/`.
- Socket, leases, PID identities: `${XDG_RUNTIME_DIR}/blackburn/`, mode 0700; socket mode 0600.
- Logs: structured JSONL with bounded rotation; artifact and debug secrets redacted.

Use one lock per execution endpoint and a separate simulator lease keyed by endpoint, macOS user, runtime, and UDID. Persist lease ownership and expiry; never release another client's device on disconnect. On daemon restart, reconcile QEMU/container/SSH facts against the journal; adopt healthy owned processes instead of spawning duplicates.

## 4. Unified CLI and configuration contract

### 4.1 Command design

The commands below are the proposed public API. They are not commands already implemented by the repository wrapper.

```bash
blackburn init
blackburn doctor --mode=headless --json
blackburn mode list
blackburn mode set agent
blackburn up --mode=headless --wait=simulator
blackburn status --json
blackburn sim up --profile=slim-1x
blackburn sim shot --output=./artifacts/screen.png
blackburn sim install ./build/MyApp.app
blackburn sim run com.example.myapp
blackburn run --project=. --debug
blackburn view attach --type=browser
blackburn view detach
blackburn mode set studio --apply --allow-restart
blackburn config show --effective
blackburn config set profiles.headless.guest_memory_mib 5120
blackburn config validate
blackburn down --timeout=120s
```

`mode set <mode>` changes the saved default only. `--apply` also requests an active transition. `up --mode=<mode>` selects a session without rewriting the default. `up` against a running incompatible session returns a transition plan rather than silently restarting it. `--allow-restart` explicitly permits the planned graceful restart; it does not allow force poweroff. Repeating the same command/profile is idempotent.

`sim up` returns an explicit UDID; subsequent commands select `--session`/`--device` or the current session's lease, never an ambiguous global `booted`. `sim install` validates the artifact without compiling it. `run` owns build/install/debug coordination. `sim run` launches an installed bundle and does not imply an incremental compiler.

Add `mode plan <mode>`, `events --follow`, `logs`, `diagnose --output`, `sim list`, `sim release`, `upgrade --plan`, and `config migrate --from-macbox`. A migration reads legacy env settings, writes validated TOML, and preserves the original files. Do not source arbitrary project-provided shell env files as configuration.

### 4.2 Automation output and errors

Human output is concise; `--json` emits a versioned object on stdout and sends diagnostics to stderr. Long-running operations support event streams with sequence IDs. Status includes desired/effective mode, backend, lifecycle stage, component versions, endpoint/UDID, artifact architecture, guest allocation, memory accounting, view transport, viewer count, health, debug state, and pending restart reasons.

Use stable error codes such as `INCOMPATIBLE_ARCH`, `MEMORY_ADMISSION_FAILED`, `RESTART_REQUIRED`, `DEVICE_LEASED`, `GUEST_GUI_UNAVAILABLE`, `TOOLCHAIN_MISMATCH`, and `CAPABILITY_UNSUPPORTED`. Define process exit classes: 0 success, 2 invalid configuration/arguments, 3 unmet prerequisite, 4 resource conflict, 5 operation failure, and 6 required restart disallowed. Preserve detailed provider errors in JSON without depending on provider-specific numeric exit statuses.

### 4.3 Example TOML configuration

This is an initial schema proposal. Values in MiB make binary memory units explicit. The shipped schema supplies omitted preset defaults; the example overrides selected defaults.

```toml
schema_version = 1
default_mode = "headless"
default_endpoint = "local"

[storage]
data_dir = "~/.local/share/blackburn"
state_dir = "~/.local/state/blackburn"

[resources]
host_reserve_mib = 3072
memory_warning_mib = 1536
memory_recover_mib = 2048
warning_hold_seconds = 10
recovery_hold_seconds = 30
build_jobs = 2

[endpoints.local]
provider = "macbox"
ssh_alias = "macbox"
gpu_selector = "auto"

[endpoints.lan_mac]
provider = "remote-mac"
ssh_alias = "blackburn-air"
architecture = "auto"
stream_provider = "auto"

[profiles.headless]
endpoint = "local"
guest_memory_mib = 5120
vcpus = 4
presentation = "none"
guest_display = "640x480"
reims_window = false
sim_profile = "slim-1x"
max_booted_devices = 1
cpu_policy = "balanced"

[profiles.stream]
endpoint = "local"
guest_memory_mib = 6144
vcpus = 6
presentation = "browser-desktop"
reims_window = true
sim_profile = "slim-1x"

[profiles.stream.video]
bind = "127.0.0.1"
port = 6081
transport = "websocket"
codec = "h264"
width = 1280
height = 720
fps = 60
idle_pause_seconds = 30

[profiles.studio]
endpoint = "local"
guest_memory_mib = 8192
vcpus = 8
presentation = "native"
reims_window = true
sim_profile = "slim-1x"
window_close_policy = "detach"
cpu_policy = "performance"

[profiles.hybrid]
endpoint = "lan_mac"
presentation = "remote-stream"
sim_profile = "dedicated"

[profiles.direct]
endpoint = "experimental_ios"
presentation = "provider"
experimental = true

[endpoints.experimental_ios]
provider = "direct-ios"
provider_manifest = "~/.config/blackburn/providers/vphone.toml"

[features]
direct_ios = false
```

Precedence is built-in defaults → user TOML → explicitly trusted project profile → `BLACKBURN_*` environment overrides → CLI flags. Project profiles may select workloads but cannot replace SSH commands, run hooks, expose network listeners, or lower host safety reserves without user-level authorization. Expand `~` only for documented path fields; do not perform shell interpolation.

Validate unknown fields, ranges, alias normalization, missing endpoints, port collisions, profile/provider consistency, memory limits, incompatible rendering flags, and restart-only changes. Write configuration atomically with schema version and a previous-version backup. `config show --effective` records the origin of each setting and redacts secrets. Desired configuration and effective runtime state remain separate until a successful apply.

## 5. Resource allocation and scheduling

### 5.1 Guest memory and QEMU backing

Use the Reims-compatible memfd-backed shared guest-memory implementation already selected by the pinned graphics stack. Probe effective QEMU arguments and `/proc/<pid>/maps`; do not append a second memory backend to a generated command line. Conceptually, integration must support a shared `memory-backend-memfd` associated with the VM's machine/NUMA configuration, with its size equal to the guest allocation. Exact flags belong to the pinned provider manifest, not an untested universal recipe.

Memfd reserves/maps guest RAM; it does not compress it, enforce a small host footprint, or provide safe guest RAM resizing. Default to demand-backed pages where compatible; avoid mandatory hugepages, `mlock`, or eager preallocation on 16 GB hosts. Verify `/dev/shm` or memory-backed filesystem requirements of the actual backend rather than assume Docker's default shared-memory mount is sufficient.

Guest RAM changes require a graceful VM restart unless a tested balloon/hotplug implementation explicitly advertises support. Do not pretend a cgroup reduction or env update resizes macOS physical RAM. Graphics shared memory and RSS may overlap; report PSS, private/shared residency, cgroup totals, and available GPU accounting separately without double counting.

### 5.2 Host admission and quotas

Before starting a cold local VM, require:

```text
MemAvailable >= projected additional resident demand
                + peak graphics/control allowance
                + concurrent build allowance
                + host reserve
```

Use conservative cold-start estimates based on guest allocation until workload telemetry justifies lower estimates. A running VM's measured residency is already reflected in `MemAvailable`; do not subtract it twice for a live view change. The 1.5 GiB warning is an emergency threshold, not the launch reserve; default launch reserve is 3 GiB.

Create a dedicated cgroup subtree for VM, view services, and builds. Set `memory.high` from the certified profile envelope and a higher `memory.max` only after peak demand testing. A starting test envelope is guest allocation plus 2 GiB runtime overhead, adjusted from evidence; it is not a claim of constant usage. Keep browser and IDE memory visible through host pressure even when outside the runtime cgroup. Do not set `memory.max=guest RAM` or use OOM as normal admission control.

Use bounded swap policy and monitor swap-in/out and Linux PSI, not merely occupied swap. Do not disable host swap globally. Limit compiler workers independently of vCPU count; cache generation and native builds can overwhelm a low-RAM preset even when the simulator is idle. Enforce disk headroom on the storage filesystem, including extraction/build space; sparse disk capacity does not equal free space.

### 5.3 CPU and GPU policy

Select affinity from topology (`lscpu`/sysfs), not hard-coded CPU indices. Reserve at least one physical P core or an equivalent measured host budget for desktop/IDE work. Keep SMT siblings and P/E core distinctions explicit on Core Ultra 7 155H. Start headless with four balanced vCPUs, stream with six, studio with eight; benchmark eight versus twelve before making twelve a performance option.

Apply cpusets to owned QEMU threads/cgroups and separate encoder/build allowances; avoid blanket host-wide scheduling changes. Persist PCI/device identity rather than rely on render-node numbering. Verify Vulkan ICD, Reims device, Sway renderer, and encoder selections independently. Pair in-place-rendering patch 0004 with ordering patch 0005; include Flutter shader patch 0006 where required. Never apply Arrow Lake-only GPU facts to Meteor Lake by brand name.

### 5.4 Simulator quotas

A low-RAM pool may contain multiple **shut-down** templates, but boots at most one leased device. Template manifests record runtime, device type, 1x settings, simslim version, and overrides hash. `full` QA devices retain push, widgets, Live Activities, and other system services as required. Slim test results cannot certify those features. No background deletion of stock or user-created devices; garbage collection targets expired Blackburn-owned ephemeral leases only.

## 6. Lifecycle, transitions, and preservation

### 6.1 Readiness state machine

```text
STOPPED -> PREFLIGHT -> STARTING -> SSH_READY -> GUEST_READY
        -> PREWARMED -> SIM_READY -> APP_READY -> DEBUG_READY

Any active state -> DEGRADED -> bounded layer-specific recovery
Any active state -> QUIESCING -> STOPPED
Unrecoverable condition -> FAILED (diagnostics and persistent data retained)
```

Presentation has its own state machine (`DETACHED`, `ATTACHING`, `ATTACHED`, `FAILED`) and must not drive execution to `STOPPED`. `up --wait=guest|simulator|debug` selects the required barrier. Each stage has a configurable deadline and cancellation behavior. Default starter budgets are SSH 180 seconds, guest/prewarm 120 seconds, simulator 180 seconds; tune after cold-boot measurements. A timeout reports the last successful stage and a diagnostic bundle.

### 6.2 Transition matrix

| Transition | Initial supported behavior | Live behavior prerequisite |
| --- | --- | --- |
| Stream viewer connect/disconnect | Live, unchanged QEMU and simulator | Existing private compositor stays alive |
| Stream encoding on/off | Live, preserve Sway and VM | Individually supervised encoder/capture services |
| Stream ↔ studio | Graceful restart when compositor/window binding changes | Surface recreation or independent detachable presentation broker |
| Headless ↔ stream/studio | Graceful restart for current Reims window realization | Offscreen frame exporter and independent views, or validated runtime window creation |
| Studio close → background | Live hide/detach, no shutdown | Mandatory close-event patch and renderer lifetime separation |
| Any local guest memory/vCPU change | Graceful restart | Explicit tested hotplug capability; no implicit support |
| Local ↔ hybrid | New endpoint session; stage compatible app and reattach | No cross-host transfer of live process memory promised |
| macOS provider ↔ direct | New device/app session | Separate artifact, data migration, and debugger compatibility |

The default presets have different guest RAM allocations. Even a future live stream/studio **view** switch cannot apply 6→8 GiB guest RAM without supported memory resizing. Offer `blackburn view attach --type=native` to change only presentation while retaining the current resource envelope. An effective mode with overrides must show those overrides; it must not claim to have applied the entire preset.

### 6.3 Transactional apply and rollback

1. Lock the endpoint and record desired state, effective state, boot identity, leases, app identifiers, active debug sessions, and transition generation.
2. Compute a plan of live changes, restart-only changes, resource admission, and affected sessions. Return `RESTART_REQUIRED` if restart permission is absent.
3. For a restart, stop new installs/build launches, flush pending artifact transfer, finish/cancel reload safely, notify IDE clients, and capture a best-effort screenshot plus console tail.
4. Record runtime/UDID and installed app metadata. Ask the app to save state through normal lifecycle where supported, shut down the owned simulator, and request guest shutdown. Wait for disk-safe completion before changing QEMU settings.
5. Persist the new VM settings without clobbering disk/network arguments; restart, prewarm, boot the **same** persistent device, and verify readiness.
6. Restore the requested view; relaunch the app and create a new debug connection. Emit state changes so clients do not reuse stale ports or app PIDs.
7. Commit effective state only after checks pass. On failure, restore the previous config and attempt one bounded recovery boot; retain the failed transition journal and report degradation if rollback also fails.

Preservation means device data, installed apps, selected runtime/UDID, logs, and project state. It does not guarantee app process memory, unsaved editor buffers, in-flight tests, or breakpoint execution position. Avoid claiming GPU VM snapshots are portable or supported. Persistent guest disks and stopped-device backups are the initial recovery mechanism.

Do not call the legacy `macbox down` forced fallback automatically during a transition. If graceful shutdown times out, leave the session blocked with a recoverable error. `blackburn down --force` is a separate explicit action. Never restart a shared endpoint to satisfy one client's view request.

## 7. Robustness and safety guards

### 7.1 Automated boot prewarming

Integrate [blackburn-prewarm-metal.sh](../scripts/blackburn-prewarm-metal.sh) at the `GUEST_READY → PREWARMED` barrier. Its current behavior sequentially hashes the AppleParavirtGPU Metal plugin, verifies its code signature, and invokes `xcrun simctl list devices`. This is a per-boot APFS/page-in workaround, not filesystem repair.

Refactor its hard-coded `ssh macbox` into a provider guest-execution operation with explicit endpoint, user, deadlines, and captured stderr. Key success by boot identity plus plugin hash; repeat after reboot or relevant component change, not on every screenshot. Prewarm must precede simulator boot and the first Metal-dependent operation. Optionally add a tiny known Metal render probe after signature verification; validate its output rather than treat simctl enumeration as a GPU test.

If signature verification fails, block app launch and report integrity failure. A transient read/SSH failure gets at most two bounded retries. Do not suppress failures, disable code signing, modify the sealed system volume, or call the hash read a disk repair.

### 7.2 WindowServer and rendering health

Collect health from multiple layers: QEMU liveness/QMP, guest SSH, logged-in GUI domain, WindowServer process identity and crash reports, CoreSimulator responsiveness, screenshot success, and a periodic known scene when permitted. A static app legitimately has no new frames; inactivity alone cannot prove a renderer crash.

Distinguish host Wayland/Sway loss from guest WindowServer failure. Also distinguish a new WindowServer PID after normal session changes from repeated crashes. On suspected failure:

1. Capture bounded diagnostics and correlate guest uptime, WindowServer PID, simulator lease, and last good frame.
2. Allow macOS launchd to restart a crashed WindowServer; probe the restored GUI session before intervening.
3. Recover an owned simulator/service only if its failure is isolated. Do not kill WindowServer indiscriminately, especially on remote shared Macs.
4. If configured for a dedicated local endpoint, permit one controlled GUI-session recovery with a warning that apps/debugging may be interrupted; escalate to graceful VM restart only under the session's restart policy.
5. Trip a circuit breaker after three recovery failures in ten minutes. Keep logs and disks, mark `DEGRADED`, and require an explicit recovery request.

Configured Metal workarounds, Reduce Transparency/Motion, and plugin prewarming are versioned provisioning policy. Apply them explicitly during setup, not as unexplained health-loop mutations. A QEMU/Metal process crash is an execution failure and may require restart; it cannot be repaired by restarting the browser encoder.

### 7.3 Memory watchdog

Sample Linux `MemAvailable`, cgroup usage/events, PSI, swap rates, and optional guest memory pressure every five seconds. Trigger a host RAM alert below **1536 MiB (1.5 GiB)** for ten seconds; clear it only above 2048 MiB for thirty seconds to avoid flapping.

Warn in CLI/IDE and emit `resource.memory.low`. Freeze new boot/build admissions, reduce queued build concurrency, pause unobserved capture/encoding, and offer a lower-resource profile. Do not automatically kill QEMU, reboot, change guest RAM, terminate a developer's app, or delete caches/devices. If memory pressure persists, report actionable measured consumers and a transition plan. A watchdog cannot guarantee recovery if the kernel OOM killer acts first; conservative startup admission remains necessary.

### 7.4 Swift, Xcode, runtime, and receipt compatibility

Maintain a `compatibility.lock.json` keyed by provider and endpoint architecture, including:

- macOS build, Xcode product/build identity, selected `DEVELOPER_DIR`, CLT identity, and simulator runtime identifier/build/architecture;
- extracted SDK identity and hash, Darwin target triple, deployment target, host Swift/Clang/linker builds, and compiled Swift-module compatibility probes;
- Flutter/Dart/engine identities, xcross revision/patch digest, native plugin architecture support, simslim manifest, QEMU/Reims/metal2vulkan/Mesa pins;
- required Apple package receipt identifiers, expected versions, and critical executable/framework slice and integrity checks.

Doctor runs `xcode-select -p`, `xcodebuild -version` where supported, `xcrun --find simctl`, runtime enumeration, `pkgutil --pkg-info-plist` for required receipts, `file`/`lipo -archs` for critical binaries, and non-destructive code-signature checks. Receipts alone do not prove that matching payloads remain installed: verify critical framework slices and paths, especially DVTDownloads, CoreDevice, and ROCKit where relevant to the chosen bundle.

Compile/import a small Swift program against the selected SDK and run a minimal simulator app to detect real module and deployment-target incompatibility. A higher Swift version number is not proof of compatibility. Likewise, do not hard-code “latest Xcode” or a global last-Intel version claim: probe actual Mach-O slices and consult the certified bundle matrix. CLT alone must not be assumed to supply a complete CoreSimulator environment.

On mismatch, block the affected build/start stage with precise evidence. Offer a repair plan using matching packages, package-signature verification, backups, and slice checks. Reuse [repair-macbox-xcode.sh](../scripts/repair-macbox-xcode.sh) only after converting its broad installer behavior into an explicit, scoped repair transaction. Doctor must never run installers or overwrite frameworks itself. Do not mix private frameworks from arbitrary Xcode builds to satisfy receipt checks.

### 7.5 Trust, process, and network boundaries

Use verified SSH host keys and endpoint-specific keys/agent identities. Keep tokens, signing identities, and passwords out of TOML, logs, process arguments, and support bundles; reference OS/keychain/agent storage instead. Forward Dart services to loopback with their auth path intact. Simulator ad-hoc signing is distinct from distribution signing and must be reported as such.

The existing container runs with unconfined seccomp/AppArmor for its networking stack. The kit must document this current exception and evaluate a narrower policy; a production label requires a clear host-device/mount/network boundary. Expose only required KVM/render devices and an explicitly selected workspace. Avoid a privileged container or mounting Docker's socket into the guest runtime. The same-user daemon socket is a local control surface and must not become an unauthenticated TCP API.

TLS/authentication, controller leases, origin checks, and short-lived viewer credentials are required for non-loopback streaming. Keep runtime images, SDKs, firmware, and simulator media obtained through an explicit provenance/licensing process; do not bundle or redistribute Apple artifacts as part of the Linux CLI release.

## 8. Headless simulator framebuffer streamer

Phase 3 removes desktop capture and Simulator.app presentation from the normal interactive path. It must not block Phase 1–2 delivery.

### 8.1 Pipeline and feasibility spike

```text
CoreSimulator display -> guest frame helper -> bounded frame/encode path
                     -> authenticated transport -> host viewer
Host input -> view/controller lease -> guest helper -> target simulator
```

Start with a feasibility spike comparing a maintained simulator streaming implementation, a version-pinned CoreSimulator display helper, and a supported fallback. `simctl io screenshot` provides still images and `recordVideo` provides recording; neither establishes a low-latency interactive display subscription. Private CoreSimulator/SimulatorKit APIs require per-Xcode compatibility tests and must be isolated behind the helper.

IOSurface handles are scoped to the guest OS; they cannot be passed to Linux as if they were ordinary cross-host file descriptors. The first working implementation may encode in the guest and send H.264 over SSH/authenticated transport, with software encoding explicitly budgeted if no working guest encoder is available. A later fast path can export frames at the Reims host boundary, avoiding guest readback, but requires new frame ownership, synchronization/fence, device import, and encoder integration.

### 8.2 Protocol and constraints

- Negotiate protocol version, endpoint/device identity, pixel format, logical/backing dimensions, orientation, timestamp domain, codec, and frame sequence.
- Bound the queue to two frames; drop stale frames under load rather than accumulate input lag. Record capture, encode, delivery, and presentation timestamps with clock-offset handling.
- Handle rotation, app suspension, simulator reboot, renderer replacement, device lease loss, and encoder reconnect with explicit stream epochs and keyframe requests.
- Authenticate control separately from video and map input to the current backing/logical coordinate transform. Validate touch/keyboard/text support against the chosen guest helper; CoreSimulator CLI input capabilities must not be invented.
- Respect capture privacy, clipboard opt-in, and viewer controller ownership. A disconnected viewer does not release the simulator lease.
- Provide screenshot fallback when helper compatibility fails. Desktop streaming remains available through an explicitly planned presentation transition.

Release gates: Simulator.app closed; expected app screenshot and live navigation visible; input mapping correct in both orientations; bounded queues under throttled LAN; stable reconnect without QEMU/app restart; measured incremental RAM/CPU/latency lower than or justified against the desktop path. Zero-copy and hardware encoding are accepted only when instrumentation verifies their actual path.

## 9. IDE and debug integration

Ship a VS Code extension that discovers the local daemon, lists sessions/devices with mode and health, invokes resource admission, and supplies an F5 debug configuration. It should also work against a Linux VS Code Remote extension host. Keep the daemon protocol editor-neutral for future clients.

For Flutter, preserve xcross as the single owner of compilation, artifact sync, app launch, VM-service forwarding, and hot reload. The current working tree already emits a simulator `vm-service:` marker and handles it in DAP; review these existing changes rather than blindly reapply the historical patch. Verify the forwarded URI, exact-once reporting, path/auth preservation, cancellation, and cleanup.

F5 orchestration is:

1. Resolve the project, profile, architecture, endpoint, runtime, and device lease.
2. Start/wait for the required execution barriers and optionally attach a viewer.
3. Launch the xcross simulator run path with `XCROSS_SIMULATOR_HOST` and `XCROSS_SIMULATOR_ARCH`, project root, and explicit UDID.
4. Wait for authenticated local VM-service readiness, then connect the supported xcross/Dart debug adapter.
5. Resolve source paths, verify breakpoints and exceptions, and expose Flutter reload/restart through that same compiler owner.
6. On stop, terminate only the debug session's app/tunnel/staging by policy. Keep persistent VM sessions alive; ephemeral CI sessions clean up their own lease/backend.

Do not rely on Dart-Code's normal Linux iOS device-discovery gate being satisfied automatically. The extension must supply a custom debug/session route or a validated supported integration. Generic Dart attach does not itself guarantee Flutter inspector, reload toolbar, or framework breakpoint behavior.

Mode changes emit `session.restarting` and `debug.invalidated`; the extension reports interruption, reconnects to a new service URI, and reapplies breakpoints where supported. It cannot promise preservation of a stopped stack frame across app/VM restarts. Native Swift/Objective-C LLDB/DAP is a separate capability track, normally with a compatible debugger on the macOS endpoint and tested source mapping; it does not inherit Flutter's reload support. Direct iOS F5 remains gated on the provider's actual debug/JIT support.

## 10. Implementation milestones and roadmap

Durations are indicative engineering estimates, not commitments. Each phase ends with archived evidence and a rollback path. Direct iOS research runs as an isolated future workstream and is not a dependency for a stable macOS-backed kit.

### 10.1 Phase 1 — CLI abstraction and Profile Manager

**Estimate:** 2–3 weeks. **Primary ownership:** control-plane/toolchain engineering.

- Create `cmd/blackburn`, `internal/config`, `internal/session`, `internal/providers`, `internal/resources`, and versioned protocol schemas.
- Implement profile parsing/validation, alias normalization, precedence, effective config reporting, legacy env migration, and stable JSON/error output.
- Wrap existing macbox, mac-sim, xcross, prewarm, and read-only audit operations; preserve current working tree fixes and provider semantics.
- Add endpoint/device leases, operation locking/journal, idempotent up/status/down, staged readiness, and explicit force-stop separation.
- Implement doctor compatibility manifests, architecture checks, disk/memory admission, watchdog alerts, and endpoint-specific prewarm.
- Ship hybrid inventory/SSH staging with explicit remote video capabilities and no local VM allocation.

**Acceptance:** repeated same-profile up creates one VM; concurrent installs cannot collide; unknown config fails before mutation; wrong architecture is rejected; prewarm failure blocks readiness; hybrid never launches local QEMU; config migration is reversible. Headless is released only after real offscreen app launch/screenshot/hot-reload checks pass at its certified RAM setting.

### 10.2 Phase 2 — Dynamic View and Mode Switcher

**Estimate:** 3–5 weeks, including native graphics spike. **Primary ownership:** runtime/graphics engineering.

- Split private display, encoder, viewer, and VM supervision into independent units with owned process identities.
- Implement headless launcher bypass and persisted Reims environment; keep private Sway alive across encoder/browser disconnects.
- Add transaction planning and graceful restart fallback with persistent device restoration.
- Patch native close handling and isolate window/surface lifetime. Prove background detach before attempting live native-to-stream migration.
- Implement stream idle pause, controller arbitration, safe reconnect, explicit transport reporting, and loopback/TLS policy.
- Evaluate WebRTC with the pinned Selkies version; ship only tested signaling/media/firewall behavior.
- Spike a stable presentation broker or Reims frame export. If compositor migration is unsafe, keep restart-required transitions as the supported behavior.

**Acceptance:** browser closure and native close each preserve QEMU, UDID, app, and debug connection; same-view requests never reboot; graceful restart preserves device/app data and reports debugger interruption; no force-stop fallback occurs on timeout. Test host Wayland loss, Sway failure, encoder crash, and daemon restart independently. A native-close test failure blocks studio promotion.

### 10.3 Phase 3 — Headless Simulator Framebuffer Streamer

**Estimate:** 4–6 weeks after a 1-week feasibility gate. **Primary ownership:** guest/graphics/streaming engineering.

- Select and pin a guest display/input helper; establish compatible Xcode/runtime bundles and licensing/API constraints.
- Implement device-scoped capture/input protocol, bounded queues, epochs, frame timestamps, and reconnect.
- Provide a measured guest-encode implementation first where viable; investigate host-side Reims export as a separate optimization.
- Integrate browser/native app-only viewers and graceful fallback to screenshots or desktop streaming.
- Measure whether Simulator.app/frontend removal actually reduces the RAM envelope and whether encoding adds back more overhead.

**Acceptance:** interactive app-only stream with Simulator.app closed; correct rotation/input; no frame queue growth; no VM shutdown on client loss; 30-minute navigation soak and network interruption recovery; documented memory/frame-pacing comparison against Phase 2. If private API compatibility cannot be maintained, retain the supported desktop viewer and mark the streamer experimental.

### 10.4 Phase 4 — Full IDE Integration

**Estimate:** 3–4 weeks; protocol work can begin during Phase 1. **Primary ownership:** IDE/debug engineering.

- Audit xcross DAP changes and formalize readiness/event contracts without breaking existing CLI users.
- Build VS Code session/device selector, F5 configuration provider, task cancellation, health/status UI, logs, and viewer commands.
- Validate breakpoints, exception stops, source mapping, variable inspection, Flutter reload/restart, inspector support, and disconnect cleanup.
- Test native macOS debugger integration as a separate capability; explain rebuild/relaunch semantics.
- Exercise the same IDE flow in headless, stream, studio, and hybrid. Gate direct mode on provider support rather than display a nonworking F5 target.

**Acceptance:** fresh project F5 reaches a verified breakpoint, changes a Dart widget through hot reload, and stops without killing a persistent VM in every certified mode. Restart-required transitions reconnect visibly; network loss yields bounded recovery and no stale tunnel leakage. Unsupported reload/debug combinations are disabled with a specific reason.

### 10.5 Direct iOS graduation track

Run after a reproducible provider/image baseline exists. Produce evidence for boot, rendering, artifact signing/install, launch/logging, touch input, screenshot/video, data persistence, debugger, Flutter JIT or AOT limits, and resource footprint. Require the same watchdog/security/lifecycle contracts. Promote from experimental only when a pinned model/firmware/toolchain matrix and realistic Intel CPU/frame-pacing results exist. The approximately 2.5 GiB target alone is insufficient.

## 11. Validation, performance certification, and release engineering

### 11.1 Verification matrix

| Area | Required scenarios | Pass condition |
| --- | --- | --- |
| Configuration | Aliases, precedence, invalid values, migration, schema upgrade | Deterministic effective profile and reversible writes |
| Concurrency | Two CLIs, duplicate up, install races, daemon restart | One backend owner, explicit device lease, no duplicate VM |
| Headless | Cold boot at 5 GiB, opt-in 4 GiB, app launch/render/reload | Correct screenshot and sustained workload without crash/pressure stalls |
| Streaming | Client close/reconnect, slow link, encoder restart, controller handoff | Backend/app survive; bounded video latency and explicit transport |
| Studio | Native close, minimize, compositor disconnect, reopen | Close never requests poweroff; failed views do not silently stop VM |
| Transitions | Same-mode no-op, changed RAM, restart timeout, failed new profile | Accurate plan, persistent data, bounded rollback |
| Hybrid | Intel and ARM Macs, wrong slice, LAN loss, shared host | Correct artifact/tunnels; no unrelated apps or host lifecycle affected |
| Compatibility | Wrong Swift module, missing receipt, stale payload, ARM-only tool | Useful preflight rejection; no automatic installation/overwrite |
| Recovery | Plugin read failure, WindowServer crash, QEMU crash, low RAM | Layer-specific action, diagnostics, circuit breaker, no restart loop |
| IDE | F5/breakpoint/reload/stop and cancellation in each certified mode | One compiler owner; correct auth URI and cleanup |
| Direct | Provider-specific firmware/model, device signing/JIT, graphics | Advertised capabilities match observed behavior |

### 11.2 Measurement protocol

Use the same reference Flutter scene plus a native UIKit/Metal smoke app; include text entry, scrolling, navigation, background/foreground, screenshot, rebuild, reload, and first interaction after idle. Record host CPU/GPU/Mesa/kernel, power/thermal state, display refresh, component hashes, guest allocation, simulator pixel dimensions, disabled services, and viewer transport.

Collect cold boot/prewarm/simulator/app/debug readiness timings; guest compression/swap/pressure; Linux `MemAvailable`, PSI, swap-in/out, cgroup peaks, and process PSS; GPU memory where available; FPS and p95/p99 intervals; encode/drop counts; input latency and reload latency. Use at least three cold boots and a 30-minute steady workload per preset, then a two-hour soak for release candidates. Do not record video during a frame-pacing baseline unless recording is the workload under test.

Publish headless approximately 4.5 GiB, stream approximately 6.5 GiB, studio 60–96 FPS, and direct approximately 2.5 GiB only when reproducible evidence supports the stated workload and accounting scope. If a target fails, ship the measured safe configuration with a clear limitation rather than reduce safeguards to match the label. Include a 16 GB host scenario with an IDE/browser and a moderate local build running.

### 11.3 Packaging and operations

Release a signed Linux CLI archive/package, shell completions, man/help text, user-service template, versioned preset/schema files, provider manifests, pinned container recipe, compatibility lock, examples, and troubleshooting runbooks. Publish checksums, dependency/SBOM inventory, patch digests, and build provenance. Cache Apple-provided media separately through user-authorized acquisition; never embed credentials or licensed runtime images in the toolkit archive.

Keep a known-good component bundle while testing an upgrade in a separate image/config slot. Back up configuration and stopped-device state before schema/runtime migrations. Test rollback compatibility before switching the active bundle; a guest OS or simulator data upgrade may be irreversible without a prior backup. Avoid live qcow2 copies or unsupported GPU save/restore snapshots as a backup mechanism.

CI runs config/protocol/transition unit tests and provider fault simulations on Linux. Hardware runners execute KVM/Vulkan/Wayland and real CoreSimulator integration checks; a LAN Mac runner exercises hybrid architecture differences. Avoid tests that merely mirror wrappers; prioritize lifetime, concurrency, compatibility, failure recovery, and actual app behavior. Unsupported provider capabilities are explicit skips, never counted as successful coverage.

A diagnostic bundle includes sanitized effective configuration, pins, operation journal, health events, resource samples, selected crash logs, and consented screenshots. Redact source paths where requested, tokens, SSH secrets, signing material, and VM-service authentication. Bound retention and archive size.

## 12. Key decisions, risks, and completion criteria

| Decision/risk | Resolution and release condition |
| --- | --- |
| Presets mix execution and presentation | Keep orthogonal internal axes; report effective resource overrides |
| Native close currently powers off QEMU | Mandatory graphics boundary patch; stream remains available until proved safe |
| Late window creation/migration may be unsafe | Default to explicit graceful restart; investigate independent views separately |
| 4–5 GiB may fail real projects | Certify by workload, retain 6/8 GiB escape path, never treat footprint as a quota |
| Slimming changes test semantics | Versioned overrides and full-device QA option |
| Current stream is not WebRTC | Accurate transport reporting; WebRTC gated on media/network acceptance |
| Xcode/SDK/private API churn | Pinned compatibility bundles, slice/module/runtime probes, scoped repairs |
| Remote Mac can be shared | Per-user device leases; no global app quits, WindowServer recovery, or host reboot |
| Direct iOS may lack practical CPU/JIT performance | Experimental provider, separate device artifacts and capability gates |
| Native tools/debugging are not fully Linux-native | Explicit remote/guest tool delegation and framework-specific debug capabilities |
| Resource and process-accounting overlap | Publish allocation/PSS/cgroup/GPU scopes; avoid double counting |

The initial production kit is complete when headless, stream, studio, and hybrid have reproducible setup, truthful capability reporting, persistent session ownership, lifecycle-safe viewers, tested compatibility guards, bounded recovery, and an F5 workflow for the certified Flutter target matrix. Direct mode may ship as a clearly gated experimental provider. A mode is not certified merely because it boots or takes a screenshot.

## 13. Repository implementation map and supporting evidence

| Existing artifact | Planned use |
| --- | --- |
| [blackburn-macbox.sh](../scripts/blackburn-macbox.sh) | Compatibility wrapper during CLI migration; existing setup/build/run flow |
| [macbox CLI](../macbox/macbox) | Provider lifecycle adapter, restart behavior, render-node/socket/port wiring |
| [stream-up.sh](../macbox/container/stream-up.sh) | Extract independent capture/encoder units; preserve known transport behavior |
| [display-up.sh](../macbox/container/display-up.sh) | Separate private compositor lifetime from encoder/view lifetime |
| [Metal prewarm](../scripts/blackburn-prewarm-metal.sh) | Endpoint-aware bounded prewarm barrier |
| [Performance audit](../scripts/blackburn-performance-audit.sh) | Doctor/diagnostic inventory, adapted to structured output |
| [Xcode repair](../scripts/repair-macbox-xcode.sh) | Evidence for slice-aware package repair; refactor before product automation |
| [xcross simulator launcher](../xcross/packages/xcross/lib/src/composition/simulator/simctl_simulator_launcher.dart) | Build/deploy/service-forwarding/reload owner; audit working tree DAP emission |
| [xcross DAP](../xcross/packages/xcross/lib/src/shared/dap/xcross_dap.dart) | IDE protocol integration and authenticated service connection |
| [mac-sim](../macbox/sims/bin/mac-sim) | Existing simulator pool/claim behavior to adapt behind leases |
| [GPU patches](../macbox/patches/README.md) | Lock compatible patch set and preserve ordering/Flutter fixes |
| [Operational manual](BLACKBURN_IOS_DEVELOPMENT_MANUAL.md) | Baseline commands, recorded incidents, compatibility and measurement caveats |
| [Headless architecture](../reports/Headless%20macbox%20and%20IDE%20integration.md) | Offscreen rendering, window realization, viewer decoupling, DAP research |
| [Meteor Lake performance](../reports/Macbox%20Meteor%20Lake%20performance.md) | CPU/GPU/simulator tuning and reference benchmark limits |
| [Signing and Xcode configuration](../reports/Apple%20signing%20keychain%20and%20xcode%20config%20without%20GUI.md) | CLI-first provisioning, credential, signing, and project configuration boundaries |

This document authorizes no implementation-side VM restart, package repair, device deletion, or host configuration change by itself. Those operations belong to the future CLI's explicit lifecycle and repair commands; drafting this plan changes only documentation.
