---
name: macbox-ios-development
description: >-
  Operational runbook and workflow guide for compiling, signing, simulating, and debugging iOS apps
  (Flutter, native Swift/ObjC, UIKit) on an Intel Linux host without a physical Mac or iPhone.
  Covers macbox VM lifecycle (local Wayland window vs WebRTC/noVNC stream), GPU acceleration
  (reims-vgpu + metal2vulkan on Intel Arc/Iris), xcross x86_64 simulator compilation, simctl control,
  QMP input automation, and troubleshooting common traps (METAL_ERROR_MODE=5, Python 3.14 socket modes,
  arm64 vs x86_64 Xcode architecture mismatches).
---

# macOS & iOS Development on Intel Linux (macbox + xcross)

This skill provides AI coding agents and human developers with an end-to-end runbook for developing, compiling, simulating, and hot-reloading iOS applications on an **Intel Linux laptop (`x86_64`)** without a physical iPhone.

---

## 1. System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                             Intel Linux Host (x86_64)                       │
│                                                                             │
│  [xcross CLI / Dart] ──(Native Linux Cross-Compile)──> Runner.app (x86_64)   │
│         │                                                    │              │
│         │ (Port Forward / SSH)                               │ (scp / rsync)│
│         ▼                                                    ▼              │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                        macbox Container (Docker)                      │  │
│  │                                                                       │  │
│  │  ┌─────────────────────────────────────────────────────────────────┐  │  │
│  │  │                      QEMU / KVM macOS Guest                     │  │  │
│  │  │                                                                 │  │  │
│  │  │   • macOS Tahoe (x86_64)                                        │  │  │
│  │  │   • GPU: reims-vgpu + metal2vulkan (Vulkan via Intel Arc iGPU)  │  │  │
│  │  │   • Apple Command Line Tools (clang, git, simctl)               │  │  │
│  │  │   • CoreSimulator Runtime (x86_64-apple-ios-simulator)          │  │  │
│  │  └─────────────────────────────────────────────────────────────────┘  │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│         │                                                    │              │
│         ▼ (Wayland Socket passthrough)                       ▼ (WebRTC/HTTP)│
│  Native Desktop Window (wayland-0)                 Browser Stream (:6081)    │
│  [macbox view local]                               [macbox view stream]     │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Essential Commands & Lifecycle Management

`macbox` is symlinked globally to `~/.local/bin/macbox` (available from any directory in `$PATH`).
> [!IMPORTANT]
> **NEVER run `macbox` with `sudo`**. Running with `sudo` strips Linux Wayland session environment variables (`WAYLAND_DISPLAY`, `XDG_RUNTIME_DIR`) and breaks local desktop window rendering.

### Basic Status & Control
```bash
# Check container and VM state, CPU/RSS, active display mode, and SSH status
macbox status

# Start or stop the VM
macbox up
macbox down

# View logs
macbox logs qemu       # QEMU / vGPU / Metal logs
macbox logs selkies    # Browser stream logs
macbox logs sway       # Wayland compositor logs
```

### Display & View Modes
```bash
# Mode 1: Native Desktop Window (Fastest: 60-96 FPS, zero latency, no browser)
# Renders directly on your Linux desktop via Wayland (WAYLAND_DISPLAY=wayland-0)
macbox view local
# Note: Click inside the window to capture mouse/keyboard.
# Press Ctrl + Alt + Super (or Ctrl + Option + Command) to release capture.

# Mode 2: Browser Stream (WebRTC / H.264 video feed)
# Streams the desktop to http://127.0.0.1:6081/
macbox view stream

# Mode 3: Alternative noVNC Stream (Standard HTML5 Canvas VNC)
macbox stream novnc
```

### Headless GUI Automation via QMP
```bash
# Take a screenshot without any screen interaction (outputs PPM format)
macbox screenshot /tmp/screen.ppm
python3 -c "from PIL import Image; Image.open('/tmp/screen.ppm').save('/tmp/screen.png')"

# Send keyboard events or text
macbox key esc                # Press Escape key
macbox key ret                # Press Return / Enter key
macbox type "password123"     # Type string into focused window

# Send mouse events
macbox click 960 540          # Left click at X, Y
macbox click 960 540 right    # Right click
macbox click 960 540 double   # Double click
macbox move 500 500           # Move cursor
```

### SSH into macOS Guest
```bash
# Connect directly to the macOS guest (user: maac, passwordless sudo)
ssh macbox

# Execute commands remotely
ssh macbox "sw_vers && uptime"
```

---

## 3. iOS Compilation Pipeline (`xcross` on Linux)

### Target Architectures
When compiling for the local `macbox` simulator on Intel Linux:
- **Build Target**: `x86_64-apple-ios-simulator` (Mach-O 64-bit executable `x86_64`).
- **Do NOT compile for `arm64-apple-ios-simulator`** when targeting the local Intel `macbox` VM (Intel KVM CPUs cannot execute ARM64 instructions).

### Building a Flutter App for Simulator
```bash
cd /path/to/flutter_app

# 1. Compile Flutter bundle targeting x86_64 simulator
xcross build iphonesimulator --simulator-arch x86_64

# 2. Output location
# build/ios/iphonesimulator/Runner.app
```

### Deploying & Running on Simulator
```bash
# 1. Check available simulator devices
ssh macbox "xcrun simctl list devices available"

# 2. Boot simulator (if not already running)
ssh macbox "xcrun simctl boot 'iPhone 16' || true"

# 3. Transfer and install Runner.app
rsync -avz -e "ssh -p 2226" build/ios/iphonesimulator/Runner.app maac@127.0.0.1:/tmp/
ssh macbox "xcrun simctl install booted /tmp/Runner.app"

# 4. Launch the app and capture output
ssh macbox "xcrun simctl launch --console-pty booted com.example.app"
```

### Dart VM Service & Hot Reload
- `xcross` sets up SSH port-forwarding to tunnel the Dart VM Service port (e.g. `8181`) from `127.0.0.1:8181` inside the macOS simulator back to the Linux host.
- Press `r` in `xcross run` or send `reloadSources` JSON-RPC request to trigger instantaneous Hot Reload.

---

## 4. Critical Traps & Troubleshooting Runbook

### Trap 1: `xcodebuild: (errno=Bad CPU type in executable)`
- **Symptom**: Unpacking Xcode 27+ `.xip` and running `xcodebuild` fails with `Bad CPU type in executable`. Running `file .../xcodebuild` shows `Mach-O 64-bit executable arm64`.
- **Root Cause**: Xcode 27 dropped `x86_64` host slices and is Apple Silicon only. macOS running under QEMU on an Intel host cannot execute `arm64` binaries (Rosetta only does x86_64 -> arm64 on Apple Silicon, not arm64 -> x86_64 on Intel).
- **Solution**:
  1. Use Apple Command Line Tools (`/Library/Developer/CommandLineTools`), which retains native `x86_64-apple-darwin` binaries:
     ```bash
     ssh macbox "sudo xcode-select -s /Library/Developer/CommandLineTools"
     ```
  2. For Xcode IDE, use `Xcode_26.6.xip` (last universal Xcode release supporting Intel hosts).
  3. Standalone simulator runtimes can be added directly via `xcrun simctl runtime add <runtime.dmg>`.

### Trap 2: vGPU / Metal Shader Crash (`newRenderPipelineDescriptorWithSerializedData`)
- **Symptom**: QEMU crashes after 30–60 seconds with:
  `reims-vgpu: fatal: platform call newRenderPipelineDescriptorWithSerializedData... Metal aborts at a render descriptor with no vertex function`.
- **Root Cause**: macOS Tahoe compiles complex desktop blur/transparency shaders using newer Metal IR that `metal2vulkan` rejects, causing `reims-vgpu` to abort.
- **Solution**:
  1. Set `METAL_ERROR_MODE=5` in **both** system and user domain:
     ```bash
     ssh macbox "sudo defaults write /Library/Preferences/.GlobalPreferences METAL_ERROR_MODE -int 5"
     ssh macbox "defaults write NSGlobalDomain METAL_ERROR_MODE -int 5"
     ```
  2. Enable Reduce Transparency (bypasses heavy blur shaders):
     ```bash
     ssh macbox "defaults write com.apple.universalaccess reduceTransparency -bool true"
     ```
  3. Run base provisioning:
     ```bash
     macbox provision base
     ```

### Trap 3: Python 3.14 QMP Socket `[Errno 22] Invalid argument`
- **Symptom**: `macbox screenshot` or `cmd_gui` fails with `OSError: [Errno 22] Invalid argument` in `socket.py:readinto`.
- **Root Cause**: In Python 3.14, `socket.makefile("rw")` is invalid on AF_UNIX sockets and raises `EINVAL`.
- **Solution**: In `macbox/lib/vmctl.py`, create separate read and write streams:
  ```python
  self.r = self.s.makefile("r")
  self.w = self.s.makefile("w")
  ```

### Trap 4: Selkies Stream "Video Paused" / Black Screen
- **Symptom**: Connecting to `http://127.0.0.1:6081/` shows black screen or says disconnected. Logs show `STOP_VIDEO from shared client: pausing its video feed`.
- **Root Cause**: Multiple browser tabs were opened to the stream. Selkies only streams video to the primary controller tab; duplicate tabs are automatically paused.
- **Solution**:
  1. Close all duplicate browser tabs.
  2. Restart stream: `macbox stream restart`.
  3. Or switch to native desktop window: `macbox view local`.

### Trap 5: High Memory Usage (7.9 GB Allocated)
- **Symptom**: Host memory monitor shows QEMU occupying ~7.9 GB RAM.
- **Explanation**: `macbox.env` defaults to `MACBOX_MEMORY=8G` to ensure macOS has ample headroom during OS installation.
- **Tuning**: Once setup is complete, you can safely reduce memory to `6G` or `4G`:
  Edit `~/.config/macbox/blackburn.env` -> `MACBOX_MEMORY=6G`, then restart the VM.
