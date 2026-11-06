# Laptop optimization and appliance packaging

Blackburn can discover a new Intel Linux laptop, size its local profiles, and
transfer an existing, cleanly shut down macbox VM. Run commands as the desktop
user. Setup changes user-owned files; package installation, kernel policy and
Docker administration remain explicit host preparation steps.

## Host matrix and evidence

| Host family | Intel display PCI examples | CPU selection | Validation status |
| --- | --- | --- | --- |
| Core Ultra 7 155H / Meteor Lake | `8086:7d55`, `8086:7d45` | Discover `cpu_core/cpus`; this development host reports `0-11`, with Atom CPUs `12-21` | Topology read on development host; fixture tests cover 22 threads |
| Arrow Lake | `8086:7d67` (S), `8086:7dd1`, `8086:7d51` (H), `8086:7d41` (U) | Discover P-cores; do not depend on SMT being present | Discovery and policy fixtures; fresh-laptop execution pending |
| Tiger Lake Iris Xe | `8086:9a49` | Homogeneous systems may lack hybrid PMUs; leave affinity unrestricted | PCI recognition; end-to-end execution pending |
| Alder Lake Iris Xe / 12th–14th generation configurations | `8086:46a6` is an Alder Lake example; other IDs are reported as unclassified Intel display devices | Discover P-cores independently of GPU generation | PCI recognition and restricted-CPU fixtures; end-to-end execution pending |

PCI identity does not establish Vulkan compatibility or promise a frame rate.
Onboarding requires an accessible KVM API (version 12), an Intel CPU on amd64
Linux, an Intel display device, a working Docker daemon, and Intel Mesa/ANV in
`vulkaninfo --summary`. The Vulkan summary, including driver version, is saved in
`$XDG_STATE_HOME/blackburn/hardware.json` (default `~/.local/state/blackburn`).
`vulkan-tools` provides `vulkaninfo` on common distributions.

The GPU table follows the [Linux Intel PCI definitions](https://github.com/torvalds/linux/blob/master/include/drm/intel/pciids.h).
In particular, `7d45` belongs to Meteor Lake, not Arrow Lake. The provider's
Arrow Lake driver-fact gate now excludes it. Unknown Intel display IDs are
reported, not rejected solely because they are missing from the example table.

## Auto-tuning policy

`blackburn init --auto` probes `/sys/devices/system/cpu`, Intel hybrid PMU masks,
`/proc/meminfo`, PCI display devices and `/sys/class/power_supply`.
`blackburn init` displays the same proposed defaults and asks before writing them.
Both require the VM to be stopped. Existing local profile resource allocations
are intentionally replaced; remote endpoints, display choices and default mode
are retained. Existing provider configuration is retained.

The CPU selection intersects online CPUs with `/proc/self/status`'s
`Cpus_allowed_list`. Intel `cpu_core/cpus` and `cpu_atom/cpus` identify P and E
threads; topology `core_type` values 2 and 1 provide a fallback. Package/core IDs
and maximum frequencies are recorded per logical CPU. A lower-frequency Atom tier
is labeled `LP-E (inferred)`: Linux does not provide a universal LP-E type here,
and this heuristic is not used to select P-cores. Missing reliable P-core identity
leaves affinity to the scheduler, with a warning. CPU numbering is never assumed.

The resulting CPU list is stored as top-level TOML `cpu_set`. The local vCPU
count is at most eight and no greater than the number of selected logical CPUs
(or allowed online CPUs when P-core classification is unavailable). Launch checks
that configured CPUs remain online and allowed, then passes `MACBOX_CPUSET` to
the provider's `taskset -apc` path. This restricts all QEMU threads, including
renderer and I/O threads. It does not dedicate one host core to each vCPU.

| Installed host RAM | Typical guest default | Minimum retained capacity |
| ---: | ---: | --- |
| 16 GiB | 6 GiB | 4 GiB host + 2 GiB runtime overhead |
| 24 GiB | 8 GiB | max(4 GiB, 25% host RAM) + 2 GiB overhead |
| 32 GiB | 10 GiB | 8 GiB host + 2 GiB overhead |
| 64 GiB | 16 GiB | 16 GiB host + 2 GiB overhead |

Classification tolerates firmware-reserved memory: tier thresholds are 14, 20,
28 and 60 GiB. The target is capped by total RAM minus max(4 GiB, 25% of total)
minus 2 GiB runtime overhead, rounded down to 256 MiB. Below a 4 GiB guest budget,
onboarding fails. Cold launch separately requires `MemAvailable >= guest + 5 GiB`
(2 GiB runtime + 3 GiB host reserve). Installed capacity is not a guarantee that a
busy host can admit the VM.

`shader_cache_max_size = "2G"` writes `MESA_SHADER_CACHE_MAX_SIZE=2G` to the VM's
`extra_env`; this is a disk cache limit, not a RAM allocation. Existing unrelated
provider options survive ordinary profile application. [Mesa's environment-variable
documentation](https://docs.mesa3d.org/envvars.html) describes the cache backend
qualifications. The existing patched-provider tuning remains responsible for
`host_rt_in_place=1` and measured Arrow Lake `M2V_ARL_FACTS`; identity alone does
not justify enabling those facts on Iris Xe or Meteor Lake. Patched build detection
still gates the provider's automatic GPU tuning.

AC state and current governors are recorded. Battery discharge produces a warning
at setup and local launch; Blackburn does not change governors, power profiles,
BIOS settings or battery limits. A `powersave` governor under `intel_pstate` alone
does not prove throttling. See the [kernel topology documentation](https://www.kernel.org/doc/html/latest/admin-guide/cputopology.html)
for topology export semantics.

## CPU pinning benchmarks and scheduler traps

There is no measured P-core versus E-core speedup from this change. Automated
tests establish selection and command behavior, not frame-rate performance. The
existing project performance reports describe earlier local observations; their
numbers are not a portable laptop benchmark.

Use this repeatable comparison on each release candidate laptop:

1. Connect AC, fix the same power profile, guest MiB, eight vCPUs (or fewer if
   needed), simulator resolution, runtime and Flutter build. Record BIOS, kernel,
   Mesa, provider build and ambient conditions. Stop the VM before each change.
2. Run with unrestricted affinity, then P-only, then E-only. For the unrestricted
   run clear TOML `cpu_set` **and** any legacy/environment `MACBOX_CPUSET`.
   Use actual discovered lists for P/E runs; do not copy `0-11` onto another host.
3. Perform one shader warmup, then five identical 60-second scroll/animation runs
   per condition. Record median/p95 frame interval, long frames, QEMU CPU/RSS,
   guest launch time, host temperature and package power. Use Reims stats and the
   existing `scripts/blackburn-performance-audit.sh`.
4. Repeat on battery as a separate condition, then select the policy with the
   best sustained p95 latency within the laptop's power budget.

| Condition | Median frame interval | p95 frame interval | Package power | Status |
| --- | --- | --- | --- | --- |
| Scheduler unrestricted | Pending | Pending | Pending | Requires live benchmark |
| P-only | Pending | Pending | Pending | Default when identity is known |
| E-only | Pending | Pending | Pending | Comparison only |

SMT siblings share execution resources; twelve logical P threads are not twelve
physical cores. Pinning the entire renderer onto too few P-cores can increase
contention. Arrow Lake may expose P-cores without SMT. Firmware, cgroups, CPU
hotplug and different numbering can invalidate saved masks. Re-run onboarding on
each recipient; do not copy donor masks. External scheduler policy can override
expectations even when the affinity syscall succeeds. The existing provider
prints a warning if `taskset` fails; verify the running threads with
`taskset -apc <QEMU_PID>` after starting a new laptop.

## Golden appliance creation

Prepare a self-contained donor VM with the intended Intel-compatible macOS/Xcode
and x86_64 simulator runtime. Finish provisioning, test Metal and simulator launch,
and remove project secrets, signed-in accounts and disposable guest data before
making a distributable golden image. The guest disk contains everything stored
inside it. The archive is not encrypted or authenticated; use trusted release
storage and distribute a separate authenticated release digest.

```bash
blackburn down --timeout=180s
blackburn appliance export --output=/fast-storage/blackburn-golden.appliance.zst
sha256sum /fast-storage/blackburn-golden.appliance.zst
# Optional: skip zstd while retaining zero-block omission.
blackburn appliance export --compress=none --output=/fast-storage/blackburn-golden.appliance
```

Export itself requests a graceful shutdown for a managed running VM. Unmanaged,
paused or suspended VMs are refused. Shutdown failure retains the VM and aborts;
no force-stop path is used. Once stopped, the container is retired to prevent its
supervisor from restarting QEMU during packaging. The VM stays stopped afterward.
Use exclusive access: the operation lock coordinates Blackburn commands, not an
independent direct `macbox` or QEMU invocation.

The package includes `vm.toml`, the referenced guest disk, OpenCore disk, firmware
code and firmware NVRAM, plus a validated snapshot of Blackburn TOML. Files must
be relative filenames inside the VM directory and regular files. A qcow2 backing
chain must first be flattened while stopped, e.g. with `qemu-img convert`, and its
reference updated to the standalone result. QEMU argument portability is limited
to Blackburn's passt network, virtio network device, disk cache/discard options
and offscreen display argument. Other custom flags require review before export.
Sockets, PID files, logs, host SSH private keys and provider binaries are omitted.

## Format, integrity and import transaction

Format v1 is a Blackburn sparse stream, optionally wrapped in zstd; it is not a
`tar` archive. Import detects compression by magic bytes, independent of filename.
The uncompressed stream begins with `BLACKBURN-SPARSE-1\n`, then a little-endian
32-bit JSON length and manifest. The manifest carries version, x86_64 architecture,
file names, logical byte sizes and SHA-256 hashes (including logical zero bytes).
Each file follows in manifest order as `(uint64 offset, uint32 length, data)`
records; a zero-length record at logical EOF terminates the file. Nonzero records
are at most 1 MiB. Zero blocks are omitted. Import writes extents at their offsets
and truncates to logical EOF, preserving filesystem sparseness.

Validation rejects traversal, duplicate names, unsupported versions/architectures,
missing references, unexpected files, invalid/overlapping extents, bad checksums,
truncation and trailing data. Manifests are limited to 1 MiB and 64 files, with a
16 TiB aggregate logical limit. Zstd decoding uses one worker and a 256 MiB memory
limit. Hashes validate integrity, not the identity of the distributor.

Export performs two sequential logical-file passes with bounded buffers, checking
that the second pass matches its manifest. It publishes a 0600 output only after
completion and never overwrites an existing destination. Both compressed and
uncompressed output omit zero blocks. It does not compact allocated qcow2 guest
free space; use filesystem trim and an offline image conversion separately if
needed. Output parent directories must already exist.

Import checks host prerequisites and refuses a running/paused VM even with
`--force`. It extracts and verifies in a sibling staging directory on the target
filesystem, replaces donor environment values, retunes memory/vCPUs and writes
recipient settings. It retains `appliance-manifest.json` as original provenance;
`vm.toml` deliberately differs after tuning. reims-ctl discovers registration
through the VM directory and its `vm.toml`.

An existing nonempty VM needs `--force`; it is renamed to `<VM>.pre-import` and
retained. A preexisting backup blocks replacement. On installation/config-write
failure, the previous VM is restored. Only remove the backup after verifying the
new guest. Existing host endpoint/SSH settings are retained; donor hardware masks
and host display paths are discarded. Imported files are not automatically booted.
Allow space for the extracted allocated data, a staging directory and any retained
old VM. Large disk sizes, slow storage or a failed host prerequisite can extend
setup well beyond five minutes.

## Fresh laptop setup: five-minute operational target

The five-minute target begins **after** host packages, matching patched provider
binaries/container image, SDK/runtime licensing and the local appliance download
are ready. It is not a measured end-to-end installation time. Building Reims/QEMU
and metal2vulkan from source is a separate, longer preparation step; VM appliances
do not bundle the native toolchain. See `macbox/docs/setup.md` and the master
[development manual](BLACKBURN_IOS_DEVELOPMENT_MANUAL.md).

```bash
# Checkout includes provider scripts; Go 1.23+ builds the CLI launcher on demand.
mkdir -p ~/.local/bin
ln -s /absolute/path/to/Blackburn/bin/blackburn ~/.local/bin/blackburn
blackburn init --auto
# Compare against the authenticated distributor's SHA-256 before importing.
sha256sum /downloads/blackburn-golden.appliance.zst
blackburn appliance import /downloads/blackburn-golden.appliance.zst
blackburn doctor --mode=headless
blackburn up --mode=headless
blackburn sim up
```

Default data directory is `~/.local/share/macbox` (honoring XDG data configuration
and `MACBOX_HOME`). Missing provider configuration is prepared without overwriting
an existing `MACBOX_CONFIG`/macbox environment file. Standard `run`, `logs`, `shots`
and VM-parent directories are created. No packages or host services are installed.
The compiled CLI needs `BLACKBURN_ROOT` pointing to the provider checkout if
installed separately.

Prepare guest SSH access before the last two commands: a new host needs its own
SSH identity/alias and a corresponding authorized public key in the golden guest.
Use the existing macbox initialization and SSH-bootstrap workflow as needed;
host private keys are intentionally absent from the appliance. `blackburn init`
does not change `~/.ssh/config`. Preflight also checks provider readiness,
`vm.max_map_count`, render-node access and installed tools through the existing
doctor workflow. Remedy missing permissions/packages explicitly on the recipient.
For Intel systems choose compatible x86_64 simulator runtimes, not arm64 runtimes.

## Release verification

```bash
go test -race -count=1 ./...
go vet ./...
bash -n macbox/macbox
```

Hardware fixtures cover restricted/noncontiguous CPU IDs, SMT-independent core
types, LP-E inference, memory tiers, power/governors and PCI identity. Archive
fixtures cover both compression modes, sparse content, checksums, malformed
manifests/extents, truncated files, backing chains and symlinks. CLI integration
uses temporary files and injected provider/probe boundaries to verify graceful
shutdown, paused/running refusal, fresh onboarding, import replacement, backup
retention, config-failure rollback and real profile/launch argument generation.
These checks do not stop the developer's live VM or certify a different laptop's
GPU renderer. Record fresh-laptop boot, simulator interaction and benchmark
results before declaring that model release-qualified.
