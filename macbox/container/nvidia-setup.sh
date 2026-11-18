#!/bin/bash
# Runs inside the macbox container (as root) when MACBOX_NVIDIA=1: makes the host's NVIDIA
# userspace driver, bind-mounted file by file into /usr/lib/x86_64-linux-gnu by macbox, loadable.
# The host's driver must match its kernel module exactly, so it is mounted, never installed here.
# NVIDIA's Vulkan ICD initializes through glvnd's EGL, so libEGL_nvidia's vendor file is mounted too.
set -euo pipefail
d=/usr/lib/x86_64-linux-gnu
cd "$d"
shopt -s nullglob
libs=(*.so.[0-9]*.[0-9]*.[0-9]*)
glx=(libGLX_nvidia.so.[0-9]*.*)
[ -e "${glx[0]:-}" ] || { echo "nvidia-setup: libGLX_nvidia is not mounted in $d" >&2; exit 1; }
for f in "${libs[@]}"; do
  case "$f" in libnvidia-*|libGLX_nvidia*|libEGL_nvidia*|libGLESv*_nvidia*|libcuda*|libnvcuvid*|libnvoptix*) ;; *) continue ;; esac
  so=${f%.so.*}.so
  for s in "$so" "$so.0" "$so.1" "$so.2" "$so.4"; do [ -e "$s" ] || ln -s "$f" "$s"; done
done
# sway (wlroots) allocates its buffers through GBM: Mesa's libgbm loads NVIDIA's backend by the
# DRM driver's name, and that backend is libnvidia-allocator.
mkdir -p "$d/gbm"
alloc=(libnvidia-allocator.so.[0-9]*.*)
[ -e "${alloc[0]:-}" ] && ln -sfn "../${alloc[0]}" "$d/gbm/nvidia-drm_gbm.so"
mkdir -p /usr/share/vulkan/icd.d
api=$(sed -n 's/.*"api_version"[^"]*"\([^"]*\)".*/\1/p' /run/nvidia-host/nvidia_icd.json 2>/dev/null || true)
printf '{ "file_format_version": "1.0.1", "ICD": { "library_path": "libGLX_nvidia.so.0", "api_version": "%s" } }\n' \
  "${api:-1.4.0}" > /usr/share/vulkan/icd.d/nvidia_icd.json
ldconfig
echo "nvidia-setup: ${glx[0]#libGLX_nvidia.so.} ready, ICD /usr/share/vulkan/icd.d/nvidia_icd.json"
