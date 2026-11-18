package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"blackburn/internal/appliance"
	"blackburn/internal/config"
	"blackburn/internal/hardware"
	"github.com/pelletier/go-toml/v2"
)

func (a *App) probeHardware() (hardware.Report, error) {
	if a.ProbeHardware != nil {
		return a.ProbeHardware()
	}
	return hardware.Probe("/")
}
func (a *App) prerequisites() (hardware.Report, error) {
	r, e := a.probeHardware()
	if e != nil {
		return r, e
	}
	// Injected probes are test fixtures. Real onboarding verifies KVM with its API ioctl.
	if a.ProbeHardware == nil {
		if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
			return r, fmt.Errorf("x86_64 Linux required")
		}
		cpuinfo, _ := os.ReadFile("/proc/cpuinfo")
		s := string(cpuinfo)
		if !strings.Contains(s, "GenuineIntel") && !strings.Contains(s, "AuthenticAMD") {
			return r, fmt.Errorf("Intel or AMD x86_64 CPU required")
		}
		f, e := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
		if e != nil {
			return r, fmt.Errorf("KVM unavailable: %w", e)
		}
		version, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0xae00, 0)
		f.Close()
		if errno != 0 || version != 12 {
			return r, fmt.Errorf("KVM API unavailable (version %d, errno %d)", version, errno)
		}
		hasAMD := false
		hasIntel := false
		for _, g := range r.GPUs {
			if g.Vendor == "0x1002" {
				hasAMD = true
			}
			if g.Vendor == "0x8086" || g.Vendor == "" {
				hasIntel = true
			}
		}
		if hasAMD && !hasIntel {
			u, err := os.OpenFile("/dev/udmabuf", os.O_RDWR, 0)
			if err != nil {
				return r, fmt.Errorf("AMD Radeon zero-copy guest memory import requires /dev/udmabuf access: %w", err)
			}
			u.Close()
		}
	}
	if r.GuestMiB < 4096 {
		return r, fmt.Errorf("insufficient RAM for host reserve and 4 GiB guest")
	}
	if len(r.GPUs) == 0 {
		return r, fmt.Errorf("supported Intel or AMD display GPU not detected")
	}
	if _, e = a.docker("info", "--format", "{{.ServerVersion}}"); e != nil {
		return r, fmt.Errorf("Docker daemon unavailable: %w", e)
	}
	v, e := a.call("vulkaninfo", "--summary")
	if e != nil {
		return r, fmt.Errorf("Vulkan unavailable: %w", e)
	}
	if !regexp.MustCompile(`(?mi)^\s*driverName\s*=\s*(Intel open-source Mesa driver|anv|radv|AMD open-source driver|Mesa RADV)\s*$`).MatchString(v) {
		return r, fmt.Errorf("Intel Mesa ANV or AMD Mesa RADV Vulkan device required")
	}
	r.Vulkan = v
	return r, nil
}
func optimized(c config.Config, r hardware.Report) config.Config {
	c.CPUSet = r.CPUList
	c.ShaderCache = "2G"
	for name, p := range c.Profiles {
		if c.Endpoints[p.Endpoint].Provider == "macbox" {
			p.Memory = r.GuestMiB
			p.CPUs = r.VCPUs
			c.Profiles[name] = p
		}
	}
	return c
}
func (a *App) initialize(c config.Config, args []string) error {
	auto := false
	f, e := parse("init", args, func(f *flag.FlagSet) { f.BoolVar(&auto, "auto", false, "use detected defaults") })
	if e != nil {
		return e
	}
	if f.NArg() != 0 {
		return fail("INVALID_ARGUMENT", "unexpected arguments", 2)
	}
	if !validVMName(a.vm()) {
		return fail("INVALID_ARGUMENT", "invalid MACBOX_VM", 2)
	}
	unlock, e := a.lock()
	if e != nil {
		return e
	}
	defer unlock()
	r, e := a.prerequisites()
	if e != nil {
		return fail("UNMET_PREREQUISITE", e.Error(), 3)
	}
	fmt.Fprintf(a.Out, "Detected %d MiB RAM; guest %d MiB; %d vCPUs; affinity %q; power %s\n", r.TotalMiB, r.GuestMiB, r.VCPUs, r.CPUList, r.Power)
	for _, w := range r.Warnings {
		fmt.Fprintln(a.Err, "Warning:", w)
	}
	if !auto {
		fmt.Fprint(a.Out, "Write optimized local profiles and prepare workspace? [y/N] ")
		if a.In == nil {
			return fmt.Errorf("interactive input unavailable; use --auto")
		}
		s, e := bufio.NewReader(a.In).ReadString('\n')
		if e != nil {
			return e
		}
		if strings.ToLower(strings.TrimSpace(s)) != "y" && strings.ToLower(strings.TrimSpace(s)) != "yes" {
			return nil
		}
	}
	if e = a.checkApplianceState(); e != nil {
		return e
	}
	running, e := a.running()
	if e != nil {
		return e
	}
	if running {
		return fail("RESOURCE_BUSY", "stop the VM before retuning local profiles", 4)
	}
	if e = a.prepareWorkspace(); e != nil {
		return e
	}
	if e = a.installXcross(); e != nil {
		return e
	}
	fmt.Fprintf(a.Out, "Bundled xcross: %s (existing binary retained as .upstream-backup)\n", a.xcrossLink())
	c = optimized(c, r)
	if e = config.Save(a.ConfigPath, c); e != nil {
		return e
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	if e = config.Atomic(filepath.Join(a.StateDir, "hardware.json"), b); e != nil {
		return e
	}
	fmt.Fprintln(a.Out, "Workspace prepared; import an appliance, then blackburn up. Existing VM files are retained.")
	return nil
}
func (a *App) vmDir() string {
	return filepath.Join(a.home(), "home/.local/share/reims-ctl/vms", a.vm())
}
func (a *App) appliance(c config.Config, args []string) error {
	if len(args) == 0 {
		return fail("INVALID_ARGUMENT", "expected appliance export or import", 2)
	}
	output := "blackburn.appliance.zst"
	compression := "zstd"
	force := false
	sub := args[0]
	rest := args[1:]
	// Go flags stop at the first positional argument; support documented FILE --force.
	if sub == "import" && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		rest = append(append([]string{}, rest[1:]...), rest[0])
	}
	f, e := parse("appliance", rest, func(f *flag.FlagSet) {
		if sub == "export" {
			f.StringVar(&output, "output", output, "output file")
			f.StringVar(&compression, "compress", compression, "zstd or none")
		} else if sub == "import" {
			f.BoolVar(&force, "force", false, "replace existing stopped VM; retain backup")
		}
	})
	if e != nil {
		return e
	}
	if (sub != "export" && sub != "import") || (sub == "export" && (f.NArg() != 0 || output == "" || (compression != "zstd" && compression != "none"))) || (sub == "import" && f.NArg() != 1) {
		return fail("INVALID_ARGUMENT", "usage: appliance export [--output=PATH] [--compress=zstd|none] | import FILE [--force]", 2)
	}
	if !validVMName(a.vm()) {
		return fail("INVALID_ARGUMENT", "MACBOX_VM must be a simple filename", 2)
	}
	unlock, e := a.lock()
	if e != nil {
		return e
	}
	defer unlock()
	if e = a.checkApplianceState(); e != nil {
		return e
	}
	if sub == "export" {
		// A stopped provider container prevents its supervisor from restarting QEMU.
		s, e := a.session()
		if e != nil {
			return e
		}
		if s.Endpoint.Provider == "remote-mac" || s.Endpoint.Provider == "direct-ios" {
			s = Session{}
		}
		if e = a.down(s, a.Timeout); e != nil {
			return e
		}
		if e = a.retireStoppedContainer(); e != nil {
			return e
		}
		tmp, e := os.MkdirTemp(a.StateDir, "export-*")
		if e != nil {
			return e
		}
		defer os.RemoveAll(tmp)
		snapshot := filepath.Join(tmp, "blackburn.toml")
		if e = config.Save(snapshot, c); e != nil {
			return e
		}
		if e = appliance.Export(a.vmDir(), snapshot, output, compression); e != nil {
			return e
		}
		fmt.Fprintln(a.Out, "Appliance exported:", output)
		return nil
	}
	r, e := a.prerequisites()
	if e != nil {
		return fail("UNMET_PREREQUISITE", e.Error(), 3)
	}
	running, e := a.running()
	if e != nil {
		return e
	}
	if running {
		return fail("RESOURCE_BUSY", "stop VM before import (even with --force)", 4)
	}
	// Retire any stopped supervisor before replacing its VM files.
	if e = a.retireStoppedContainer(); e != nil {
		return e
	}
	parent := filepath.Dir(a.vmDir())
	if e = os.MkdirAll(parent, 0700); e != nil {
		return e
	}
	stage, e := os.MkdirTemp(parent, ".import-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	m, e := appliance.Extract(f.Arg(0), stage)
	if e != nil {
		return e
	}
	if _, e = config.Load(filepath.Join(stage, "blackburn.toml")); e != nil {
		return fmt.Errorf("invalid packaged configuration: %w", e)
	}
	// Imported profiles are hardware-specific; preserve recipient endpoint settings
	// and replace local allocations with this host's measured defaults.
	if e = a.installXcross(); e != nil {
		return e
	}
	fmt.Fprintf(a.Out, "Bundled xcross: %s (existing binary retained as .upstream-backup)\n", a.xcrossLink())
	c = optimized(c, r)
	if e = c.Validate(); e != nil {
		return e
	}
	data, e := os.ReadFile(filepath.Join(stage, "vm.toml"))
	if e != nil {
		return e
	}
	var vm map[string]any
	if e = toml.Unmarshal(data, &vm); e != nil {
		return e
	}
	vm["name"] = a.vm()
	vm["cpus"] = r.VCPUs
	vm["memory_mib"] = r.GuestMiB
	vm["extra_env"] = map[string]any{"MESA_SHADER_CACHE_MAX_SIZE": "2G", "REIMS_VGPU_CLOSE_POLICY": "detach", "REIMS_VGPU_WINDOW": "0"}
	data, e = toml.Marshal(vm)
	if e != nil {
		return e
	}
	if e = config.Atomic(filepath.Join(stage, "vm.toml"), data); e != nil {
		return e
	}
	if e = a.prepareWorkspace(); e != nil {
		return e
	}
	// Preserve original manifest as provenance; vm.toml is deliberately retuned.
	b, _ := json.MarshalIndent(m, "", "  ")
	if e = config.Atomic(filepath.Join(stage, "appliance-manifest.json"), b); e != nil {
		return e
	}
	backup := a.vmDir() + ".pre-import"
	if info, err := os.Lstat(a.vmDir()); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("VM destination must be a directory")
		}
		entries, err := os.ReadDir(a.vmDir())
		if err != nil {
			return err
		}
		if len(entries) > 0 && !force {
			return fail("RESOURCE_BUSY", "VM exists; use --force to retain it as .pre-import", 4)
		}
		if _, err = os.Lstat(backup); !os.IsNotExist(err) {
			return fmt.Errorf("backup already exists or inaccessible: %s", backup)
		}
		if e = os.Rename(a.vmDir(), backup); e != nil {
			return e
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	rollback := func() { _ = os.RemoveAll(a.vmDir()); _ = os.Rename(backup, a.vmDir()) }
	if e = os.Rename(stage, a.vmDir()); e != nil {
		_ = os.Rename(backup, a.vmDir())
		return e
	}
	if e = config.Save(a.ConfigPath, c); e != nil {
		rollback()
		return e
	}
	// reims-ctl discovers VMs through this directory and vm.toml; no daemon registry.
	fmt.Fprintln(a.Out, "Appliance verified and registered:", a.vmDir())
	return nil
}
func validVMName(s string) bool {
	return s != "" && s != "." && s != ".." && filepath.Base(s) == s && !strings.ContainsAny(s, "/\\\x00")
}

// The normal lifecycle reports a boolean, but a paused/suspended QEMU must not
// be mistaken for a cleanly stopped disk owner during packaging.
func (a *App) checkApplianceState() error {
	out, e := a.docker("inspect", "--format", "{{.State.Running}}", a.container())
	if e != nil {
		if strings.Contains(strings.ToLower(out+e.Error()), "no such") {
			return nil
		}
		return e
	}
	state := strings.TrimSpace(out)
	if state == "false" {
		return nil
	}
	if state != "true" {
		return fmt.Errorf("unrecognized container state %q", state)
	}
	out, e = a.ctl("list")
	if e != nil {
		return e
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) > 1 && f[0] == a.vm() {
			if f[1] == "running" || f[1] == "stopped" {
				return nil
			}
			return fail("RESOURCE_BUSY", "VM state "+f[1]+" is not a clean shutdown; resume and shut down first", 4)
		}
	}
	return fmt.Errorf("cannot establish VM state from provider list")
}

// prepareWorkspace creates only missing provider configuration; existing settings
// and credentials remain user-owned. The provider requires this marker to boot.
func (a *App) prepareWorkspace() error {
	for _, dir := range []string{filepath.Dir(a.vmDir()), filepath.Join(a.home(), "run"), filepath.Join(a.home(), "logs"), filepath.Join(a.home(), "shots")} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			return e
		}
	}
	path := a.env("MACBOX_CONFIG", filepath.Join(a.env("XDG_CONFIG_HOME", config.Base("XDG_CONFIG_HOME", ".config")), "macbox/blackburn.env"))
	if _, e := os.Stat(path); e == nil {
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	values := [][2]string{{"MACBOX_HOME", a.home()}, {"MACBOX_VM", a.vm()}, {"MACBOX_CONTAINER", a.container()}}
	var b strings.Builder
	b.WriteString("# Blackburn provider workspace. Existing files are never overwritten.\n")
	for _, v := range values {
		if strings.ContainsAny(v[1], "'\"$`\\;\n\r") {
			return fmt.Errorf("%s must be a literal provider value", v[0])
		}
		fmt.Fprintf(&b, "%s='%s'\n", v[0], v[1])
	}
	return config.Atomic(path, []byte(b.String()))
}
