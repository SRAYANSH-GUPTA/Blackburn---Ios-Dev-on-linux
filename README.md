# Blackburn ⚡

> **Full iOS app compilation, linking, GPU-accelerated simulation, and hot-reload debugging on non-Mac Intel Linux laptops — with no physical iPhone required.**

---

## 1. Overview & Mission

iOS development traditionally mandates physical Apple hardware: macOS workstations for Xcode and physical iPhones for testing. **Blackburn** solves this constraint on **Intel Linux laptops** (`x86_64`, Intel Core Ultra 7 155H / Meteor Lake / Arrow Lake / Iris Xe):

- **No physical iPhone needed**: Runs authentic Apple iOS Simulator runtimes (`x86_64-apple-ios-simulator`) locally with Intel Arc hardware graphics acceleration (60–96 FPS).
- **No macOS workstation required for daily coding**: Write, build, and debug your Flutter or native apps directly in Linux VS Code with full F5 one-click execution, Dart DevTools, and stateful hot reload.
- **Multiple Operational Modes**: Switch seamlessly between a high-FPS native Wayland window (`studio`), an in-browser desktop stream (`stream`), a headless low-RAM simulator streamer (`headless`), or remote Mac compilation (`hybrid`).
- **One-Command Portability**: Export your configured, tuned macOS + Xcode appliance and restore it on any new Intel laptop in under 5 minutes with `blackburn appliance export|import`.

```
┌────────────────────────────────────────────────────────────────────────┐
│                      BLACKBURN ARCHITECTURE                            │
├────────────────────────────────────────────────────────────────────────┤
│  LINUX HOST (Intel x86_64, Linux Kernel 6.x+, Mesa ANV Vulkan)        │
│                                                                        │
│  ┌───────────────────────┐         ┌──────────────────────────────┐    │
│  │   Developer IDE       │         │    Blackburn Control Plane   │    │
│  │   VS Code + Dart DAP  │ ◄────── │    Unified Go CLI + Config   │    │
│  │   F5 / Hot-Reload     │         │    P-Core Affinity & Tuning  │    │
│  └───────────┬───────────┘         └──────────────┬───────────────┘    │
│              │                                    │                    │
│              ▼                                    ▼                    │
│  ┌───────────────────────┐         ┌──────────────────────────────┐    │
│  │  xcross Compiler      │         │   macbox QEMU KVM Container  │    │
│  │  Darwin SDK kit       │ ──────► │   reims-vgpu + metal2vulkan  │    │
│  │  Mach-O / Swift/Clang │         │   macOS Tahoe Guest OS       │    │
│  └───────────────────────┘         └──────────────┬───────────────┘    │
│                                                   │                    │
│                                                   ▼                    │
│                                    ┌──────────────────────────────┐    │
│                                    │   Apple CoreSimulator Pool   │    │
│                                    │   iOS 26.5 Universal Runtime │    │
│                                    │   Runner.app (Flutter/Swift) │    │
│                                    └──────────────────────────────┘    │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Hardware Requirements

| Component | Minimum | Recommended |
| :--- | :--- | :--- |
| **CPU** | Intel 11th Gen Core (Tiger Lake `x86_64`) | Intel Core Ultra Series 1/2 (Meteor Lake / Arrow Lake) or 12th–14th Gen Core i7/i9 |
| **Virtualization** | Intel VT-x (`/dev/kvm` accessible) | Intel VT-x enabled, user in `kvm` group |
| **GPU & Vulkan** | Intel Iris Xe (`0x9a49` / `0x46a6`) | Intel Arc Graphics (`0x7d55` / `0x7d45` / `0x7d67`) with Mesa 24.2+ (`vulkan-intel`) |
| **RAM** | 16 GB Physical RAM | 16 GB to 32 GB RAM (Blackburn auto-sizes guest RAM to 6–10 GB) |
| **Storage** | 100 GB NVMe SSD | 150 GB+ NVMe SSD |
| **Host OS** | Any modern x86_64 Linux (Arch, Ubuntu 22.04+, Fedora) | Wayland desktop session (GNOME, Sway, KDE Wayland) |

---

## 3. Operational Modes & Viewer Matrix

Blackburn isolates execution from presentation so you only spend RAM on what you need:

| Mode | Guest RAM | Display Transport | Frame Rate | Host RAM Overhead | Best For |
| :--- | :---: | :--- | :---: | :---: | :--- |
| **`studio`** | 6144 MiB | Native Wayland (SDL3 / Vulkan) | **60–96 FPS** | ~8 GB total | Daily interactive development, UI animation verification |
| **`stream`** | 6144 MiB | Browser WebSocket (`:6081`) | ~60 FPS | ~8 GB total | Headless laptops, remote display, zero client dependencies |
| **`headless`** | 5120 MiB | App-Only Canvas (`:6082`) | ~30–60 FPS | **~4.5 GB total** | Low-RAM laptops (saves ~3.5 GB by closing macOS GUI) |
| **`hybrid`** | 0 MiB | LAN Mac Stream (`ssh`) | Native | ~200 MB host | Offloading compilation/simulation to an available MacBook Air |
| **`direct`** | 2048 MiB | Barebones QEMU iOS (experimental) | TBD | ~2.5 GB host | Experimental iOS kernel emulation |

---

## 4. Quick Start Guide

### 4.1 Install the CLI
Clone this repository and symlink the self-compiling launcher into your `PATH`:

```bash
cd ~/Documents/Golang/Blackburn
mkdir -p ~/.local/bin
ln -sf "$(pwd)/bin/blackburn" ~/.local/bin/blackburn

# Ensure ~/.local/bin is in your PATH (e.g. in ~/.bashrc or ~/.zshrc):
export PATH="$HOME/.local/bin:$PATH"
```

### 4.2 System Diagnostic & Onboarding
Run the hardware doctor to verify KVM, Intel Vulkan Mesa, and Docker:

```bash
# 1. Check prerequisites
blackburn doctor

# 2. Run auto-onboarding (auto-detects CPU P-cores, RAM, and GPU)
blackburn init --auto
```

### 4.3 Start the Simulator
To boot the macOS VM, prewarm the Intel Arc Metal GPU driver, and open the slim iOS Simulator in **one command**:

```bash
# Option A: High-FPS Native Desktop Window (Studio Mode)
blackburn up --mode=studio --wait=simulator

# Option B: In-Browser Desktop Stream on localhost:6081 (Stream Mode)
blackburn up --mode=stream --wait=simulator
# Open http://127.0.0.1:6081/ in your browser

# Option C: Low-RAM Headless Canvas Streamer on localhost:6082 (Headless Mode)
blackburn up --mode=headless --wait=simulator
blackburn sim stream
# Open http://127.0.0.1:6082/ in your browser
```

> [!TIP]
> **Keyboard / Mouse Capture**: Click into the native window to capture input. Press <kbd>Ctrl</kbd>+<kbd>Alt</kbd>+<kbd>Super</kbd> to release focus back to Linux. Closing the window safely detaches presentation without powering off the VM.

---

## 5. VS Code / IDE Integration (1-Click F5 & Hot Reload)

You can build, debug, and hot-reload Flutter apps with **zero terminal commands**:

1. Configure your Flutter project workspace:
   ```bash
   blackburn ide config --dir="/path/to/flutter_project"
   ```
   *(This safely injects `.vscode/launch.json`, `.vscode/tasks.json`, `.vscode/settings.json`, and the dynamic Dart DAP adapter shim `.vscode/blackburn_dap.dart`)*

2. Open the project in VS Code and press **<kbd>F5</kbd>**:
   - Blackburn automatically verifies the VM and boots it if stopped.
   - Prewarms the Metal driver to eliminate demand-paging faults.
   - Leases the slim simulator (`iPhone 17 Pro slim-1`).
   - Compiles via `xcross`, installs the `.app`, and connects to the authenticated Dart VM service.
   - Saves are automatically pushed via stateful Flutter hot reload!

---

## 6. Portability: Onboarding a Brand New Laptop in Minutes

You don't need to spend hours installing macOS and extracting Xcode on every new laptop. Use Blackburn's **Golden Appliance** transfer system:

### 6.1 On Your Existing Configured Machine (Export)
```bash
# 1. Ensure the VM is shut down gracefully
blackburn down

# 2. Export the tuned appliance (zero-blocks omitted, zstd compressed)
blackburn appliance export --output=~/blackburn-golden.appliance.zst
```

### 6.2 On the Brand New Laptop (Import)
1. Install prerequisites on the new machine:
   ```bash
   # Arch Linux:
   sudo pacman -S qemu-base libvirt docker vulkan-intel zstd git go
   sudo usermod -aG docker,kvm $USER
   
   # Ubuntu / Debian:
   sudo apt install qemu-system-x86 libvirt-daemon-system docker.io mesa-vulkan-drivers zstd git golang
   sudo usermod -aG docker,kvm $USER
   ```
2. Clone Blackburn and link the CLI:
   ```bash
   git clone https://github.com/your-username/Blackburn.git ~/Documents/Golang/Blackburn
   mkdir -p ~/.local/bin
   ln -sf ~/Documents/Golang/Blackburn/bin/blackburn ~/.local/bin/blackburn
   ```
3. Copy `blackburn-golden.appliance.zst` to the new laptop and run:
   ```bash
   # Automatically verifies KVM, extracts, validates SHA-256 digests, and re-tunes for new hardware:
   blackburn appliance import ~/blackburn-golden.appliance.zst
   
   # Launch!
   blackburn up --mode=studio --wait=simulator
   ```

---

## 7. CLI Command Reference

```bash
# Lifecycle & Modes
blackburn mode list                           # List available profiles (headless, stream, studio, hybrid, direct)
blackburn mode set <mode> [--apply]           # Set default profile (or apply immediately)
blackburn up [--mode=PRESET] [--wait=simulator] # Start VM (optionally wait for simulator boot)
blackburn down [--timeout=120s]               # Gracefully shut down guest OS
blackburn status [--json]                     # Report VM state, active lease, memory, viewer, and IP
blackburn doctor [--mode=PRESET]              # Check KVM, Vulkan ANV, Docker, and SSH connectivity

# Simulator Automation
blackburn sim up                              # Lease and boot a clean slim simulator
blackburn sim status                          # Show leased device and boot state
blackburn sim shot [output.png]               # Capture lossless screenshot from simulator display
blackburn sim install <app_path>              # Install .app bundle into leased device
blackburn sim run <bundle_id>                 # Launch installed app
blackburn sim stream                          # Launch low-latency app-only canvas streamer on port 6082
blackburn sim touch <x> <y>                   # Inject touch down/up event
blackburn sim tap <x> <y>                     # Inject tap event
blackburn sim key <keysym>                    # Inject key press
blackburn sim text "hello"                    # Inject text input
blackburn sim down                            # Shutdown and release the simulator lease

# Viewer Switching (Decoupled Presentation)
blackburn view list                           # List available viewers (none, browser-desktop, native)
blackburn view get                            # Show currently attached viewer
blackburn view set <viewer> [--allow-restart] # Switch viewer presentation without data loss
blackburn view detach                         # Detach UI presentation (keeps QEMU & Simulator alive)

# IDE & Debugging
blackburn ide config [--dir=DIR]              # Generate VS Code launch.json, tasks.json, settings.json
blackburn ide f5 [--dir=DIR] [--dry-run]      # Orchestrate preflight readiness and output launch command
blackburn ide status [--dir=DIR]              # Audit IDE integration health and active debug tunnels

# Hardware & Appliance Portability
blackburn init [--auto]                       # Probe host hardware, P-cores, RAM, and write config
blackburn appliance export [--output=PATH]    # Export VM, OpenCore, NVRAM, and checksums
blackburn appliance import <FILE> [--force]   # Validate and import appliance on new machine
blackburn config show                         # Show effective TOML configuration
blackburn config set <key> <value>            # Update config key (e.g. profiles.studio.guest_memory_mib)
```

---

## 8. AI Agent Automated Onboarding Prompt

If you are using an AI coding assistant (**Antigravity, Codex, Claude Code, Cursor, Windsurf, Aider**) to set up Blackburn on a new laptop, copy and paste the prompt below into the agent chat:

<details open>
<summary><b>📋 Click to expand: AI Agent Onboarding Prompt</b></summary>

```markdown
You are an expert Systems & Release Engineer. Your task is to set up and verify Blackburn (iOS Development & Simulator Kit on Intel Linux) on this laptop.

Repository Directory: /home/srayansh/Documents/Golang/Blackburn (or current working directory)

Follow these strict step-by-step instructions:

### Step 1: Host Prerequisites Check
1. Verify operating system is Linux x86_64 (`uname -m`).
2. Verify CPU is Intel (`grep -m1 "model name" /proc/cpuinfo`).
3. Check KVM accessibility: verify `/dev/kvm` exists and is readable/writable (`test -r /dev/kvm && test -w /dev/kvm`). If permission denied, advise adding the user to the `kvm` group.
4. Check Docker daemon status (`docker info`). Ensure user is in the `docker` group.
5. Check Intel Vulkan driver: run `vulkaninfo --summary` and verify that the driver is `Intel open-source Mesa driver` (`anv`).

### Step 2: Install / Link the Blackburn CLI
1. Ensure the executable launcher is linked:
   mkdir -p ~/.local/bin
   ln -sf "$(pwd)/bin/blackburn" ~/.local/bin/blackburn
2. Ensure `~/.local/bin` is in PATH.
3. Test the CLI: run `blackburn --help` and verify all commands (`init`, `appliance`, `up`, `down`, `sim`, `ide`, `view`, `doctor`, `status`) are present.

### Step 3: Hardware Discovery & Auto-Tuning
1. Run `blackburn doctor`. If any check fails, resolve it before proceeding.
2. Run hardware discovery:
   blackburn init --auto
   This probes CPU topology (P-cores vs E-cores), sizes guest RAM safely according to host capacity (e.g. 6 GB on 16 GB laptops), configures Mesa shader caches (2 GB), and writes `~/.config/blackburn/config.toml`.

### Step 4: Provision or Import the macOS Appliance
Check if the local macbox VM already exists at `~/.local/share/macbox/home/.local/share/reims-ctl/vms/mac/vm.toml`:
- If YES: Ensure permissions in `~/.local/share/macbox/home` are owned by the current user (`chown -R $(id -u):$(id -g) ~/.local/share/macbox/home`).
- If an appliance archive (`*.appliance.zst`) is available: Run `blackburn appliance import /path/to/archive.appliance.zst`.
- If starting from scratch: Pull or build the container image via `./macbox/macbox build` and follow base disk initialization.

### Step 5: Verification & Simulator Launch
1. Ensure capability flag is set:
   test -f ~/.local/share/macbox/home/.local/share/reims-ctl/qemu/current/blackburn-viewer-capability || echo detach-v1 > ~/.local/share/macbox/home/.local/share/reims-ctl/qemu/current/blackburn-viewer-capability
2. Boot the environment in Studio (native Wayland) or Stream mode with automatic simulator wait:
   blackburn up --mode=studio --wait=simulator
3. Check status:
   blackburn status
   Ensure VM is running (`stage: SIM_READY`) and an explicit UDID is leased.
4. Capture a verification screenshot:
   blackburn sim shot ./verification_screenshot.png
   Verify the image is a valid PNG showing the iOS home screen.

### Step 6: Target Flutter Project Setup
If a target Flutter project exists:
1. Run:
   blackburn ide config --dir="/path/to/flutter_project"
2. Check IDE status:
   blackburn ide status --dir="/path/to/flutter_project"
3. Run a dry-run test:
   blackburn ide f5 --dir="/path/to/flutter_project" --dry-run --json

Output a concise summary of the discovered hardware, active mode, leased simulator UDID, and confirmation that the environment is ready for F5 debugging.
```
</details>

---

## 9. Documentation Index

- **Master Operational Manual**: [`docs/BLACKBURN_IOS_DEVELOPMENT_MANUAL.md`](file:///home/srayansh/Documents/Golang/Blackburn/docs/BLACKBURN_IOS_DEVELOPMENT_MANUAL.md) — 688 lines, troubleshooting traps (`SIGBUS`, `METAL_ERROR_MODE=5`, APFS page-in, QMP input, Impeller patches).
- **Multi-Mode Kit Architecture**: [`docs/BLACKBURN_MULTI_MODE_KIT_PLAN.md`](file:///home/srayansh/Documents/Golang/Blackburn/docs/BLACKBURN_MULTI_MODE_KIT_PLAN.md) — 5 operational presets, memory quotas, lifecycle boundaries.
- **Laptop Optimization & Packaging**: [`docs/BLACKBURN_LAPTOP_OPTIMIZATION_AND_PACKAGING.md`](file:///home/srayansh/Documents/Golang/Blackburn/docs/BLACKBURN_LAPTOP_OPTIMIZATION_AND_PACKAGING.md) — Hybrid CPU topology, P-core benchmarks, sparse streaming format.
- **CLI Reference Guide**: [`docs/BLACKBURN_CLI.md`](file:///home/srayansh/Documents/Golang/Blackburn/docs/BLACKBURN_CLI.md) — Syntax, exit codes, and JSON schemas.
- **Research Reports**: [`reports/`](file:///home/srayansh/Documents/Golang/Blackburn/reports/) — Headless graphics decoupling, compilation pipelines, Apple signing/keychain CLI without GUI.

---

## 10. License & Intellectual Property Notice

Blackburn's tooling, scripts, patches, Go CLI, and documentation are provided under the MIT License. macOS, Xcode, CoreSimulator, Metal, and iOS are proprietary trademarks and software of Apple Inc. Blackburn does not distribute Apple proprietary binaries, disks, or SDKs; users acquire all Apple runtime media through standard authorized developer channels.
