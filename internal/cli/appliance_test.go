package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackburn/internal/config"
	"blackburn/internal/hardware"
	"github.com/pelletier/go-toml/v2"
)

func portableApp(t *testing.T) *App {
	t.Helper()
	a, _, _ := testApp(t)
	a.ProbeHardware = func() (hardware.Report, error) {
		return hardware.Report{TotalMiB: 32768, GuestMiB: 10240, VCPUs: 2, CPUList: "0-1", CPUs: []hardware.CPU{{ID: 0, Kind: "P"}, {ID: 1, Kind: "P"}}, GPUs: []hardware.GPU{{Device: "0x7d55"}}, Power: "AC"}, nil
	}
	a.Run = func(_ context.Context, args, env []string) (string, error) {
		s := strings.Join(args, " ")
		switch {
		case strings.Contains(s, "inspect"):
			return "false", nil
		case args[0] == "vulkaninfo":
			return "driverName = Intel open-source Mesa driver\ndriverInfo = Mesa 25.3.0", nil
		default:
			return "ok", nil
		}
	}
	return a
}
func seedVM(t *testing.T, a *App) {
	t.Helper()
	if e := os.MkdirAll(a.vmDir(), 0700); e != nil {
		t.Fatal(e)
	}
	for n, b := range map[string]string{"vm.toml": "name='mac'\ndisk='disk.qcow2'\ncpus=4\nmemory_mib=5120\n[q35]\nopencore='opencore.qcow2'\nfirmware_code='OVMF_CODE.fd'\nfirmware_vars='OVMF_VARS.fd'\n[extra_env]\nWAYLAND_DISPLAY='/old/host'\n", "disk.qcow2": "disk data", "opencore.qcow2": "opencore", "OVMF_CODE.fd": "code", "OVMF_VARS.fd": "nvram"} {
		if e := os.WriteFile(filepath.Join(a.vmDir(), n), []byte(b), 0600); e != nil {
			t.Fatal(e)
		}
	}
}
func exported(t *testing.T) string {
	t.Helper()
	a := portableApp(t)
	seedVM(t, a)
	out := filepath.Join(t.TempDir(), "golden.zst")
	if n := a.Main([]string{"appliance", "export", "--output=" + out}); n != 0 {
		t.Fatal(n, a.Err)
	}
	return out
}
func TestApplianceExportImportCLI(t *testing.T) {
	out := exported(t)
	a := portableApp(t)
	if n := a.Main([]string{"appliance", "import", out}); n != 0 {
		t.Fatal(n, a.Err)
	}
	b, _ := os.ReadFile(filepath.Join(a.vmDir(), "OVMF_VARS.fd"))
	if string(b) != "nvram" {
		t.Fatal(string(b))
	}
	c, e := config.Load(a.ConfigPath)
	if e != nil || c.CPUSet != "0-1" || c.Profiles["headless"].Memory != 10240 {
		t.Fatal(c, e)
	}
	b, _ = os.ReadFile(filepath.Join(a.vmDir(), "vm.toml"))
	var v map[string]any
	if e = toml.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	if v["memory_mib"] != int64(10240) || v["extra_env"].(map[string]any)["WAYLAND_DISPLAY"] != nil {
		t.Fatal(v)
	}
	if n := a.Main([]string{"appliance", "import", out}); n != 4 {
		t.Fatal(n, a.Err)
	}
	if n := a.Main([]string{"appliance", "import", out, "--force"}); n != 0 {
		t.Fatal(n, a.Err)
	}
	if _, e = os.Stat(a.vmDir() + ".pre-import/OVMF_VARS.fd"); e != nil {
		t.Fatal(e)
	}
	if n := a.Main([]string{"appliance", "import", out, "--force"}); n == 0 {
		t.Fatal("overwrote backup")
	}
}
func TestApplianceImportCorruptionPreservesDestination(t *testing.T) {
	a := portableApp(t)
	seedVM(t, a)
	bad := filepath.Join(t.TempDir(), "bad")
	os.WriteFile(bad, []byte("not an appliance"), 0600)
	if n := a.Main([]string{"appliance", "import", bad, "--force"}); n == 0 {
		t.Fatal("accepted corruption")
	}
	b, _ := os.ReadFile(filepath.Join(a.vmDir(), "disk.qcow2"))
	if string(b) != "disk data" {
		t.Fatal("destination changed")
	}
}
func TestInitAutoAndInteractive(t *testing.T) {
	for _, auto := range []bool{false, true} {
		a := portableApp(t)
		args := []string{"init"}
		if auto {
			args = append(args, "--auto")
		} else {
			a.In = strings.NewReader("yes\n")
		}
		if n := a.Main(args); n != 0 {
			t.Fatal(n, a.Err)
		}
		c, e := config.Load(a.ConfigPath)
		if e != nil || c.CPUSet != "0-1" || c.Profiles["studio"].CPUs != 2 || c.Profiles["hybrid"].Memory != 0 {
			t.Fatal(c, e)
		}
		if _, e = os.Stat(filepath.Join(a.StateDir, "hardware.json")); e != nil {
			t.Fatal(e)
		}
	}
	a := portableApp(t)
	a.In = strings.NewReader("no\n")
	if n := a.Main([]string{"init"}); n != 0 {
		t.Fatal(n)
	}
	if _, e := os.Stat(a.ConfigPath); !os.IsNotExist(e) {
		t.Fatal("decline wrote config")
	}
}
func TestPrerequisitesAndRunningGuards(t *testing.T) {
	for _, cmd := range [][]string{{"init", "--auto"}, {"appliance", "import", "missing"}} {
		a := portableApp(t)
		a.ProbeHardware = func() (hardware.Report, error) { return hardware.Report{}, errors.New("probe failed") }
		if n := a.Main(cmd); n != 3 {
			t.Fatal(cmd, n, a.Err)
		}
		if _, e := os.Stat(a.ConfigPath); !os.IsNotExist(e) {
			t.Fatal("mutated config")
		}
	}
	a := portableApp(t)
	base := a.Run
	a.Run = func(ctx context.Context, args, env []string) (string, error) {
		s := strings.Join(args, " ")
		if strings.Contains(s, "inspect") {
			return "true", nil
		}
		if strings.HasSuffix(s, "list") {
			return "mac running", nil
		}
		return base(ctx, args, env)
	}
	if n := a.Main([]string{"appliance", "import", "missing", "--force"}); n != 4 {
		t.Fatal(n, a.Err)
	}
	if n := a.Main([]string{"init", "--auto"}); n != 4 {
		t.Fatal(n, a.Err)
	}
}
func TestPausedExportNeverStopsContainer(t *testing.T) {
	a := portableApp(t)
	seedVM(t, a)
	stops := 0
	a.Run = func(_ context.Context, args, env []string) (string, error) {
		s := strings.Join(args, " ")
		switch {
		case strings.Contains(s, "inspect"):
			return "true", nil
		case strings.HasSuffix(s, "list"):
			return "mac paused", nil
		default:
			stops++
			return "", nil
		}
	}
	if n := a.Main([]string{"appliance", "export"}); n != 4 || stops != 0 {
		t.Fatal(n, stops, a.Err)
	}
}
func TestExportGracefulShutdown(t *testing.T) {
	a := portableApp(t)
	seedVM(t, a)
	c := config.Defaults()
	a.save(Session{Mode: "headless", Profile: c.Profiles["headless"], Endpoint: c.Endpoints["local"]})
	running := true
	stopped := false
	a.Run = func(_ context.Context, args, env []string) (string, error) {
		s := strings.Join(args, " ")
		switch {
		case strings.Contains(s, "inspect"):
			if stopped {
				return "false", nil
			}
			return "true", nil
		case strings.HasSuffix(s, "list"):
			if running {
				return "mac running", nil
			}
			return "mac stopped", nil
		case strings.Contains(s, "shutdown -h now"):
			running = false
		case len(args) > 1 && args[1] == "stop":
			if running {
				t.Fatal("force stop")
			}
			stopped = true
		}
		return "", nil
	}
	out := filepath.Join(t.TempDir(), "out")
	if n := a.Main([]string{"appliance", "export", "--compress=none", "--output=" + out}); n != 0 {
		t.Fatal(n, a.Err)
	}
	if !stopped {
		t.Fatal("supervisor retained")
	}
}
func TestApplianceArgumentValidation(t *testing.T) {
	for _, args := range [][]string{{"appliance"}, {"appliance", "wat"}, {"appliance", "export", "--compress=gzip"}, {"appliance", "import"}, {"init", "--wat"}, {"init", "extra"}} {
		a := portableApp(t)
		a.Run = func(context.Context, []string, []string) (string, error) {
			t.Fatal("provider called for invalid args")
			return "", nil
		}
		if n := a.Main(args); n != 2 {
			t.Fatal(args, n, a.Err)
		}
	}
}
func TestImportRollsBackOnConfigFailure(t *testing.T) {
	out := exported(t)
	a := portableApp(t)
	seedVM(t, a) // Force atomic config publication to fail after VM installation.
	blocker := filepath.Join(t.TempDir(), "parent")
	os.WriteFile(blocker, []byte("file"), 0600)
	a.ConfigPath = filepath.Join(blocker, "config.toml")
	// dispatch rejects the unreadable config parent first; exercise transaction directly.
	e := a.appliance(config.Defaults(), []string{"import", out, "--force"})
	if e == nil {
		t.Fatal("expected config error")
	}
	b, _ := os.ReadFile(filepath.Join(a.vmDir(), "disk.qcow2"))
	if string(b) != "disk data" {
		t.Fatal("old VM lost")
	}
	if _, e = os.Stat(a.vmDir() + ".pre-import"); !os.IsNotExist(e) {
		t.Fatal("backup not restored", e)
	}
}
func TestTunedLaunchAffinityAndCache(t *testing.T) {
	a := portableApp(t)
	seedVM(t, a)
	a.AvailableMiB = func() int64 { return 32768 }
	c := config.Defaults()
	c.CPUSet = "0-1"
	if e := config.Save(a.ConfigPath, c); e != nil {
		t.Fatal(e)
	}
	base := a.Run
	launch := false
	a.Run = func(ctx context.Context, args, env []string) (string, error) {
		if strings.HasSuffix(args[0], "macbox/macbox") {
			launch = true
			s := strings.Join(env, "\n")
			if !strings.Contains(s, "MACBOX_CPUSET=0-1") {
				t.Fatal(env)
			}
		}
		return base(ctx, args, env)
	}
	if n := a.Main([]string{"up"}); n != 0 {
		t.Fatal(n, a.Err)
	}
	if !launch {
		t.Fatal("no launch")
	}
	b, _ := os.ReadFile(filepath.Join(a.vmDir(), "vm.toml"))
	var v map[string]any
	toml.Unmarshal(b, &v)
	if v["extra_env"].(map[string]any)["MESA_SHADER_CACHE_MAX_SIZE"] != "2G" {
		t.Fatal(v)
	}
	c.CPUSet = "99"
	config.Save(a.ConfigPath, c)
	before := string(b)
	if n := a.Main([]string{"up"}); n == 0 {
		t.Fatal("stale affinity allowed")
	}
	b, _ = os.ReadFile(filepath.Join(a.vmDir(), "vm.toml"))
	if string(b) != before {
		t.Fatal("VM mutated before affinity validation")
	}
}
func TestPrerequisiteDockerAndVulkanFailures(t *testing.T) {
	for _, kind := range []string{"docker", "vulkan", "software"} {
		a := portableApp(t)
		base := a.Run
		a.Run = func(ctx context.Context, args, env []string) (string, error) {
			if args[0] == "docker" && kind == "docker" {
				return "", errors.New("denied")
			}
			if args[0] == "vulkaninfo" {
				if kind == "vulkan" {
					return "", errors.New("missing")
				}
				return "llvmpipe (software)", nil
			}
			return base(ctx, args, env)
		}
		if n := a.Main([]string{"init", "--auto"}); n != 3 {
			t.Fatal(kind, n, a.Err)
		}
	}
}
