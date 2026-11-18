# NVIDIA GPU support, native Wayland presentation, and setup fixes

Field report from bringing up branch `feat/blackburn-amd-support` on a hybrid AMD + NVIDIA laptop,
with the VM stored on a secondary NTFS SSD. It covers the code changes, the issues hit along the
way, their root causes, and what is still open.

## 1. Test host

| Item | Value |
| :--- | :--- |
| CPU | AMD Ryzen 5 6600H (6C/12T, AMD-V, AVX2), `kvm_amd` |
| GPUs | NVIDIA GeForce RTX 3050 Laptop (`10de:25a2`, `renderD128`, proprietary 615.78.08) + AMD Radeon 660M (`1002:1681`, `renderD129`, RADV) |
| Display | Hyprland (Wayland) on the AMD iGPU (eDP on the Radeon) |
| RAM | ~14 GiB usable, 29 GiB swap |
| Host OS | Arch-based (Omarchy), kernel 7.2, Docker 29.9 (rootful), glibc 2.44 |
| Storage | `MACBOX_HOME` on an **NTFS** (`ntfs3`) SSD, reached through a symlink without spaces |
| Guest | macOS 26.7 Tahoe, 6 vCPU / 6 GiB / 160 GiB, system disk on NVMe |

Result: macOS boots and runs on the **RTX 3050** in two modes: through the browser stream
(Selkies, NVENC zero-copy), and as a **native Wayland window** on the host compositor with no
private Sway, no stream, and no encode. The Radeon (RADV + udmabuf) path from this branch was also
verified before the NVIDIA switch.

## 2. Changes

| File | Change |
| :--- | :--- |
| `macbox/patches/metal2vulkan/0006-accept-Flutter-shader-debug-module-flags.patch` | Added an mbox header (`From`, `Date`, `Subject`). The file was a bare `git diff`, which `git am` rejects. |
| `macbox/macbox` | New opt-in `MACBOX_NVIDIA=1` mode in `container_start` (devices, driver files, setup hook). |
| `macbox/macbox` | Fixed `vol=(...)` → `vol+=(...)` in the `view local` branch, which discarded earlier mounts. |
| `macbox/macbox` | `MACBOX_STREAM_ARGS` added to the variables `cenv` forwards into the container. |
| `macbox/container/nvidia-setup.sh` | **New.** Runs in the container after start: links the mounted NVIDIA libraries, writes the Vulkan ICD, links the GBM backend, runs `ldconfig`. |
| `macbox/container/stream-up.sh` | Appends `${MACBOX_STREAM_ARGS:-}` to the `selkies` command line (e.g. `--encoder=jpeg`). |
| `internal/cli/cli.go`, `internal/cli/appliance.go` | Docker "container missing" detection made case-insensitive. |

### 2.1 `MACBOX_NVIDIA=1`

The NVIDIA userspace driver must match the host kernel module exactly, so it is **mounted from
the host, never installed in the image**. When `MACBOX_NVIDIA=1`, `container_start` adds:

- Devices: `/dev/nvidia0`, `/dev/nvidiactl`, `/dev/nvidia-modeset`, `/dev/nvidia-uvm`,
  `/dev/nvidia-uvm-tools`.
- Every host `libnvidia-*`, `libGLX_nvidia`, `libEGL_nvidia`, `libcuda`, `libnvcuvid` versioned
  library, bind-mounted read-only **into `/usr/lib/x86_64-linux-gnu/`** (from `/usr/lib` on Arch or
  `/usr/lib/x86_64-linux-gnu` on Debian/Ubuntu hosts).
- `/usr/share/glvnd/egl_vendor.d/10_nvidia.json` (required, see 3.4).
- `/usr/share/egl/egl_external_platform.d` (required for Sway on NVIDIA, see 3.5).
- `/usr/share/nvidia` (application profiles) and the host ICD JSON at `/run/nvidia-host/` (only its
  `api_version` is read).

`nvidia-setup.sh` then creates the soname symlinks, writes
`/usr/share/vulkan/icd.d/nvidia_icd.json`, links `gbm/nvidia-drm_gbm.so → libnvidia-allocator`, and
runs `ldconfig`. It fails loudly if `libGLX_nvidia` is not mounted, so the VM never silently starts on
llvmpipe.

Use: set `MACBOX_NVIDIA=1` and `MACBOX_RENDER_NODE=/dev/dri/renderD128` (the NVIDIA node), then
`macbox down; docker rm -f macbox; macbox up`. With the NVIDIA ICD present, Reims's `auto` GPU choice
picks the discrete RTX automatically. To revert to the Radeon: `MACBOX_NVIDIA=0`,
`MACBOX_RENDER_NODE=/dev/dri/renderD129`, and recreate the container the same way.

### 2.2 Native Wayland (studio) presentation

`macbox up` with `run/view` set to `local` mounts the host's Wayland socket into the container and
Reims opens its SDL/Vulkan window directly on the host compositor. This was verified on Hyprland
with Reims rendering on the RTX 3050 and the compositor on the Radeon (cross-GPU presentation
works). No Sway or Selkies runs in the container in this mode.

```bash
echo local > "$MACBOX_HOME/run/view"   # or: blackburn up --mode=studio
macbox up
```

## 3. Issues encountered

### 3.1 Build failed: patch 0006 not in mbox format — fixed

`macbox build` stopped at `Patch format detection failed. a patch does not apply:
.../0006-accept-Flutter-shader-debug-module-flags.patch`. `build-reims.sh` applies patches with
`git am`, and 0006 was a plain `git diff` with no header. An mbox header was added. All six
metal2vulkan patches then applied cleanly to `0db5825a`, and the build finished.

### 3.2 `blackburn up` failed on Docker 29: `error: no such object: macbox` — fixed

`running()`, `retireStoppedContainer()` and `checkApplianceState()` matched Docker's error text
against `"No such"`. Docker 29 prints `error: no such object: macbox` (lowercase), so a missing
container was reported as `OPERATION_FAILED` instead of "not running". The matches are now
case-insensitive. `go test ./internal/cli/` passes.

### 3.3 NVIDIA not usable in the stock image — fixed by `MACBOX_NVIDIA`

`macbox/README.md` and `doctor` already say the image ships no NVIDIA Vulkan driver. Installing one
in the image is fragile (it must match the host module). Mounting the host's files is the approach
taken (2.1).

### 3.4 NVIDIA Vulkan: `ERROR_INCOMPATIBLE_DRIVER` in the container — fixed

With the libraries and devices mounted, `vulkaninfo` failed with `Could not get 'vkCreateInstance'
via 'vk_icdGetInstanceProcAddr' for ICD libGLX_nvidia.so.0`, even with `--privileged` and the
host's own Vulkan loader. The `__malloc_hook`/`ErrorF` "undefined symbol" lines in `LD_DEBUG` are
red herrings: they appear on the host too.

Root cause, from comparing host and container `LD_DEBUG=libs`: on the host, `libGLX_nvidia`
initializes **through glvnd's EGL** (`libEGL.so.1` → `libEGL_nvidia.so.0`, found through
`/usr/share/glvnd/egl_vendor.d/10_nvidia.json`). The container had no NVIDIA EGL vendor file, so
the ICD returned NULL. Mounting that JSON fixed it. The RTX 3050 then reported
`VK_EXT_external_memory_host`, `VK_EXT_external_memory_dma_buf`, `VK_KHR_external_memory_fd` and
`VK_EXT_image_drm_format_modifier`.

### 3.5 Browser stream blank on NVIDIA (all browsers) — fixed

After the switch, `http://127.0.0.1:6081/` was blank in Brave and Chrome, although QMP screenshots
showed a healthy desktop (they bypass Sway). Selkies logged `Readback capture ... rendered in
software (Pixman) ... (no GPU renderer)` and a loop of `host refused the layout`. Sway logged
`gbm_bo_create_with_modifiers failed` / `Failed to allocate buffer`.

Root cause: wlroots allocates through GBM. The container's Mesa `libgbm` loads backends by DRM
driver name and had no `nvidia-drm_gbm.so`. NVIDIA's EGL also needs its external-platform JSONs.
Fixed by linking `gbm/nvidia-drm_gbm.so → libnvidia-allocator` and mounting
`egl_external_platform.d`. Selkies then logged `Zero-copy capture: output 1 1280x800 rendered on
/dev/dri/renderD128, encoded in place on NVENC.`

### 3.6 Native mode silently fell back to llvmpipe — fixed

In `view local`, the macOS window appeared but `nvidia-smi` listed no processes and
`reims-ctl gpus` showed only llvmpipe. The `view local` branch assigned `vol=(...)`, which wiped
the NVIDIA mounts added earlier in `container_start`. Changed to `vol+=`. `nvidia-setup.sh` now also
checks specifically for `libGLX_nvidia`; it used to pass because its glob matched unrelated
libraries.

### 3.7 `blackburn up --mode=studio` refuses on 14–16 GiB laptops — open

`MEMORY_ADMISSION_FAILED: need 11264 MiB available; have 8184 MiB` (6 GiB guest + ~5 GiB reserve).
The VM runs fine at 6 GiB with swap. Workaround: write `local` to `run/view` and run `macbox up`
directly. Suggestion: make the reserve configurable, or add a `--force`/warning mode, since the
README lists 16 GiB as the minimum and that can never pass this check.

### 3.8 `.env.blackburn` defaults assume a 22-thread Intel host — open

`MACBOX_CPUS=16`, `MACBOX_BUILD_JOBS=22`, `MAKEFLAGS=-j22`, etc. `scripts/blackburn-macbox.sh`
exports them before `macbox` reads its config, and macbox gives the environment precedence over the
config file. So on smaller machines these values override `macbox.env`. Worked around with a
wrapper exporting host-appropriate values. Suggestion: derive them from `nproc` or from
`blackburn init --auto`.

### 3.9 Guest user is hard-wired to `dev` in docs — minor

Setup Assistant was completed with another account name. `MACBOX_GUEST_USER` must then be changed
and `macbox init` rerun to rewrite `~/.ssh/config`. Worth stating in `docs/setup.md` step 7.

### 3.10 `macbox-guest base` needs a TTY for the first sudo — minor

`sudo_ready` resets `PASSWORD=""` at load, so a password from the caller's environment is ignored
unless `base --autologin` reads it from stdin. Non-interactive automation must create
`/etc/sudoers.d/macbox-$USER` itself first. Suggestion: honor an inherited `PASSWORD`, or read it
from stdin for plain `base` too.

### 3.11 `macbox-guest tools` fails before Xcode — expected, minor

CocoaPods' native gems (`nkf`) need a compiler, and `xcode-select` triggers the Command Line Tools
dialog in the guest. Rerun `tools` after `macbox xcode`. Suggestion: order `tools` after Xcode in
`docs/setup.md`, or skip CocoaPods with a message when no developer directory exists.

### 3.12 Recovery Metal fix not needed when `base` runs first — note

The desktop came up with Dock and icons before `METAL_ERROR_MODE` was set. `macbox-guest base` sets
it through `sudo`, with no Recovery trip, and it applies after a restart.

### 3.13 NTFS host storage — works, with caveats

`MACBOX_HOME` on `ntfs3` supports what macbox needs: symlinks, hardlinks, Unix sockets (QMP),
FIFOs, chmod and xattrs. Caveats:

- Sparse files are not effective, so the 160 GiB disk is fully allocated.
- Files written by the root container appear root-owned. Host-side git needs
  `-c safe.directory=...` to read `src/` checkouts.
- `mv` into some filesystems warns `setting attributes ... Operation not supported`; harmless.
- Paths with spaces were avoided by pointing `MACBOX_HOME` at a symlink.

## 4. Verification

- `macbox build`: Reims vGPU `7198fe6` + patches, metal2vulkan `0db5825a` + 6 patches, QEMU
  `d3acddf54-macbox`, all built.
- `go vet ./internal/cli/` and `go test ./internal/cli/`: pass.
- NVIDIA in container: `vulkaninfo` lists the RTX 3050 / 615.78.08 with the external-memory extensions.
- Stream mode: QEMU and Selkies on the RTX 3050 (`nvidia-smi`), zero-copy NVENC capture, no Sway errors.
- Native mode: Reims window mapped in Hyprland, QEMU ~800 MiB on the RTX 3050, no Sway/Selkies in
  the container, guest `Dock` and `iconservicesagent` running, `METAL_ERROR_MODE=5`.
- Not yet verified on NVIDIA: Xcode, simulator pool, Flutter/Impeller workloads, long soak, sleep/resume.

## 5. Suggested follow-ups

1. Add `MACBOX_NVIDIA` to `macbox init` (auto-enable when the chosen render node is `0x10de`) and to
   `doctor`: check `/dev/nvidia*`, the host driver files, and an in-container `vulkaninfo`.
2. Teach `internal/hardware` and `blackburn init --auto` about NVIDIA `0x10de` and hybrid
   iGPU + dGPU laptops (which node renders, which drives the display).
3. Make the studio/stream memory admission reserve configurable (3.7) and derive build/CPU defaults
   from the host (3.8).
4. Document the guest-user and ordering notes (3.9–3.11) in `docs/setup.md`.
