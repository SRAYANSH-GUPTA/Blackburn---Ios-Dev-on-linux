package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blackburn/internal/config"
	"github.com/pelletier/go-toml/v2"
)

func testApp(t *testing.T) (*App, *bytes.Buffer, *[][]string) {
	t.Helper()
	dir := t.TempDir()
	out := &bytes.Buffer{}
	calls := [][]string{}
	a := &App{Out: out, Err: &bytes.Buffer{}, Root: dir, ConfigPath: filepath.Join(dir, "config.toml"), StateDir: filepath.Join(dir, "state"), Env: []string{"MACBOX_CONFIG=" + filepath.Join(dir, "absent.env"), "MACBOX_HOME=" + dir, "HOME=" + dir}, Timeout: time.Second}
	a.Run = func(_ context.Context, args, env []string) (string, error) {
		calls = append(calls, append([]string{}, args...))
		joined := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(joined, "view get"):
			return "browser-desktop\n", nil
		case strings.Contains(joined, "inspect"):
			return "true\n", nil
		case strings.Contains(joined, "reims-ctl list"):
			return "mac running\n", nil
		case strings.HasSuffix(joined, "uname -m"):
			return "x86_64\n", nil
		case strings.Contains(joined, "list devices booted"):
			return `{"devices":{"runtime":[{"udid":"SIM-1","state":"Booted"}]}}`, nil
		case strings.HasSuffix(joined, " udid"):
			return "SIM-1\n", nil
		default:
			return "ok\n", nil
		}
	}
	return a, out, &calls
}
func TestCLIInvalidArgsNoMutation(t *testing.T) {
	for _, args := range [][]string{{"up", "--mode=unknown"}, {"up", "--wat"}, {"up", "junk"}, {"down", "--timeout=-1s"}, {"mode", "set", "wat"}, {"mode", "set", "studio", "--apply"}, {"config", "set", "bad", "1"}, {"sim", "run"}, {"sim", "wat"}, {"bogus"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			a, _, calls := testApp(t)
			if exit := a.Main(args); exit != 2 {
				t.Fatalf("exit %d", exit)
			}
			if len(*calls) > 0 {
				t.Fatal("provider invoked", *calls)
			}
			if _, e := os.Stat(a.ConfigPath); !os.IsNotExist(e) {
				t.Fatal("config mutated")
			}
		})
	}
}
func TestModeAndConfigCommands(t *testing.T) {
	a, out, _ := testApp(t)
	for _, args := range [][]string{{"mode", "set", "local"}, {"mode", "get"}, {"config", "set", "profiles.studio.vcpus", "7"}, {"config", "get", "profiles.studio.vcpus"}, {"config", "show", "--effective"}, {"config", "validate"}, {"mode", "list"}} {
		if exit := a.Main(args); exit != 0 {
			t.Fatal(args, exit, a.Err)
		}
	}
	c, e := config.Load(a.ConfigPath)
	if e != nil || c.Mode != "studio" || c.Profiles["studio"].CPUs != 7 {
		t.Fatal(c, e)
	}
	if !strings.Contains(out.String(), "studio") {
		t.Fatal(out.String())
	}
}
func TestStatusJSON(t *testing.T) {
	a, out, _ := testApp(t)
	c := config.Defaults()
	_ = a.save(Session{Mode: "stream", Profile: c.Profiles["stream"], Endpoint: c.Endpoints["local"], Stage: "SIM_READY", Device: "SIM-1"})
	if exit := a.Main([]string{"status", "--json"}); exit != 0 {
		t.Fatal(exit)
	}
	var v map[string]any
	if e := json.Unmarshal(out.Bytes(), &v); e != nil {
		t.Fatal(e, out.String())
	}
	if v["schema_version"] != float64(1) || v["running_mode"] != "stream" || v["active_simulator"] != "SIM-1" || v["view_transport"] != "websocket" {
		t.Fatal(v)
	}
	if v["desired_mode"] != "headless" {
		t.Fatal("desired and running conflated")
	}
}
func TestDoctorJSONFailureSingleObject(t *testing.T) {
	a, out, _ := testApp(t)
	a.Run = func(context.Context, []string, []string) (string, error) { return "", errors.New("unavailable") }
	if exit := a.Main([]string{"doctor", "--json"}); exit != 3 {
		t.Fatal(exit)
	}
	var result struct {
		OK     bool    `json:"ok"`
		Checks []Check `json:"checks"`
	}
	if e := json.Unmarshal(out.Bytes(), &result); e != nil {
		t.Fatal(e, out.String())
	}
	if result.OK || len(result.Checks) < 8 {
		t.Fatal(result)
	}
}
func TestDoctorHybridSkipsLocalRequirements(t *testing.T) {
	a, out, calls := testApp(t)
	os.MkdirAll(filepath.Join(a.Root, "bin"), 0755)
	os.WriteFile(filepath.Join(a.Root, "bin", "xcross"), []byte("#!/bin/sh\n"), 0755)
	if err := a.installXcross(); err != nil {
		t.Fatal(err)
	}
	a.Run = func(_ context.Context, args, env []string) (string, error) {
		*calls = append(*calls, args)
		return "ok", nil
	}
	if exit := a.Main([]string{"doctor", "--mode=hybrid", "--json"}); exit != 0 {
		t.Fatal(exit, out.String())
	}
	for _, c := range *calls {
		if c[0] != "ssh" && c[0] != filepath.Join(a.Root, "bin", "xcross") {
			t.Fatal("local dependency in hybrid", c)
		}
	}
	var v map[string]any
	if e := json.Unmarshal(out.Bytes(), &v); e != nil || v["ok"] != true {
		t.Fatal(v, e)
	}
}
func TestJSONArgumentError(t *testing.T) {
	a, out, _ := testApp(t)
	if exit := a.Main([]string{"doctor", "--json", "--mode=wat"}); exit != 2 {
		t.Fatal(exit)
	}
	var v map[string]any
	if e := json.Unmarshal(out.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	if v["error"].(map[string]any)["code"] != "INVALID_CONFIG" {
		t.Fatal(v)
	}
}
func TestRepeatedUpDoesNotStartOrPrewarm(t *testing.T) {
	a, _, calls := testApp(t)
	c := config.Defaults()
	_ = a.save(Session{Mode: "headless", Profile: c.Profiles["headless"], Endpoint: c.Endpoints["local"]})
	if e := a.prewarm(c.Endpoints["local"]); e != nil {
		t.Fatal(e)
	}
	*calls = nil
	for i := 0; i < 2; i++ {
		if exit := a.Main([]string{"up"}); exit != 0 {
			t.Fatal(exit)
		}
	}
	for _, call := range *calls {
		j := strings.Join(call, " ")
		if strings.Contains(j, "macbox/macbox") || strings.Contains(j, "prewarm") {
			t.Fatal(call)
		}
	}
}
func TestIncompatibleUpDoesNotRestart(t *testing.T) {
	a, _, calls := testApp(t)
	c := config.Defaults()
	_ = a.save(Session{Mode: "headless", Profile: c.Profiles["headless"], Endpoint: c.Endpoints["local"]})
	if exit := a.Main([]string{"up", "--mode=studio"}); exit != 6 {
		t.Fatal(exit)
	}
	for _, call := range *calls {
		if strings.Contains(strings.Join(call, " "), "shutdown") {
			t.Fatal(call)
		}
	}
}
func TestUpWaitSimulator(t *testing.T) {
	a, out, calls := testApp(t)
	c := config.Defaults()
	_ = a.save(Session{Mode: "headless", Profile: c.Profiles["headless"], Endpoint: c.Endpoints["local"]})
	if exit := a.Main([]string{"up", "--wait=simulator"}); exit != 0 {
		t.Fatal(exit, a.Err)
	}
	if !strings.Contains(out.String(), "SIM-1") {
		t.Fatal(out.String())
	}
	s, _ := a.session()
	if s.Device != "SIM-1" || s.Stage != "SIM_READY" {
		t.Fatal(s)
	}
	hasSimUp := false
	for _, call := range *calls {
		if strings.Contains(strings.Join(call, " "), "mac-sim up") {
			hasSimUp = true
		}
	}
	if !hasSimUp {
		t.Fatal(*calls)
	}
}
func TestHybridNeverAllocatesLocalVM(t *testing.T) {
	a, _, calls := testApp(t)
	if exit := a.Main([]string{"up", "--mode=remote"}); exit != 0 {
		t.Fatal(exit)
	}
	for _, call := range *calls {
		if call[0] != "ssh" {
			t.Fatal(call)
		}
	}
	s, _ := a.session()
	if s.Profile.Memory != 0 || s.Mode != "hybrid" {
		t.Fatal(s)
	}
}
func TestDirectFailsBeforeMutation(t *testing.T) {
	a, _, calls := testApp(t)
	if exit := a.Main([]string{"up", "--mode=direct"}); exit != 3 {
		t.Fatal(exit)
	}
	if len(*calls) != 0 {
		t.Fatal(*calls)
	}
}
func TestPrewarmFailureBlocksSimulatorBoot(t *testing.T) {
	a, _, calls := testApp(t)
	a.Run = func(_ context.Context, args, env []string) (string, error) {
		*calls = append(*calls, args)
		return "", errors.New("signature failure")
	}
	if exit := a.Main([]string{"sim", "up"}); exit != 5 {
		t.Fatal(exit)
	}
	if len(*calls) != 1 || (*calls)[0][0] != "ssh" {
		t.Fatal(*calls)
	}
}
func TestSimUpReturnsExplicitUDIDAndPersists(t *testing.T) {
	a, out, calls := testApp(t)
	if exit := a.Main([]string{"sim", "up"}); exit != 0 {
		t.Fatal(exit, a.Err)
	}
	if out.String() != "SIM-1\n" {
		t.Fatal(out.String())
	}
	s, _ := a.session()
	if s.Device != "SIM-1" || s.Stage != "SIM_READY" {
		t.Fatal(s)
	}
	if !strings.Contains((*calls)[1][0], "prewarm") {
		t.Fatal(*calls)
	}
}
func TestSimRunLaunchesBundleNotBuild(t *testing.T) {
	a, _, calls := testApp(t)
	if exit := a.Main([]string{"sim", "run", "com.example.app"}); exit != 0 {
		t.Fatal(exit, a.Err)
	}
	last := (*calls)[len(*calls)-1]
	if !strings.Contains(strings.Join(last, " "), "simctl launch 'SIM-1' 'com.example.app'") {
		t.Fatal(last)
	}
	for _, call := range *calls {
		if strings.HasSuffix(strings.Join(call, " "), "mac-sim run") {
			t.Fatal("build command used")
		}
	}
}
func TestStatusAndSimDownDoNotPrewarm(t *testing.T) {
	for _, cmd := range []string{"status", "down"} {
		a, _, calls := testApp(t)
		if exit := a.Main([]string{"sim", cmd}); exit != 0 {
			t.Fatal(exit)
		}
		for _, call := range *calls {
			if strings.Contains(strings.Join(call, " "), "prewarm") {
				t.Fatal(call)
			}
		}
	}
}
func TestOperationLock(t *testing.T) {
	a, _, calls := testApp(t)
	unlock, e := a.lock()
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	if exit := a.Main([]string{"up"}); exit != 4 {
		t.Fatal(exit)
	}
	if len(*calls) != 0 {
		t.Fatal(*calls)
	}
}
func TestGracefulDownNeverForces(t *testing.T) {
	a, _, calls := testApp(t)
	c := config.Defaults()
	_ = a.save(Session{Mode: "headless", Profile: c.Profiles["headless"], Endpoint: c.Endpoints["local"]})
	if exit := a.Main([]string{"down", "--timeout=1ns"}); exit != 5 {
		t.Fatal(exit)
	}
	for _, call := range *calls {
		j := strings.Join(call, " ")
		if strings.Contains(j, "--force") || strings.Contains(j, "rm -f") || strings.Contains(j, "macbox/macbox") {
			t.Fatal(call)
		}
	}
	s, _ := a.session()
	if s.Stage != "QUIESCING" {
		t.Fatal(s)
	}
}
func TestStoppedProfilePreservesProviderFlags(t *testing.T) {
	a, _, _ := testApp(t)
	path := filepath.Join(a.home(), "home/.local/share/reims-ctl/vms/mac/vm.toml")
	original := []byte("name='mac'\ncpus=16\nmemory_mib=8192\nextra_args=['-netdev','passt','-display','sdl']\n[extra_env]\nM2V_LIVE='keep'\n[display]\nresolution='1920x1080'\n")
	_ = config.Atomic(path, original)
	if e := a.applyProfile(config.Defaults().Profiles["headless"]); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(path)
	var v map[string]any
	if e := toml.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	if v["cpus"] != int64(4) || v["memory_mib"] != int64(5120) || v["extra_env"].(map[string]any)["REIMS_VGPU_WINDOW"] != "0" || v["extra_env"].(map[string]any)["M2V_LIVE"] != "keep" {
		t.Fatal(v)
	}
	extra := v["extra_args"].([]any)
	if len(extra) != 4 || extra[1] != "passt" || extra[3] != "none" {
		t.Fatal(extra)
	}
	backup, _ := os.ReadFile(path + ".blackburn-backup")
	if string(backup) != string(original) {
		t.Fatal("backup changed")
	}
}
func TestLegacyEnvLiteralAndPrecedence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "legacy.env")
	_ = os.WriteFile(p, []byte("MACBOX_HOME='/custom/home'\nMACBOX_CPUS=16\n"), 0600)
	env, e := LegacyEnv([]string{"MACBOX_CONFIG=" + p, "MACBOX_CPUS=4"})
	if e != nil {
		t.Fatal(e)
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "MACBOX_HOME=/custom/home") || strings.Contains(joined, "MACBOX_CPUS=16") {
		t.Fatal(env)
	}
	_ = os.WriteFile(p, []byte("MACBOX_HOME=$(touch /tmp/no)"), 0600)
	if _, e = LegacyEnv([]string{"MACBOX_CONFIG=" + p}); e == nil {
		t.Fatal("accepted shell interpolation")
	}
}
func writeMachO(t *testing.T, path string, cpu, platform uint32) {
	t.Helper()
	b := make([]byte, 56)
	for i, v := range []uint32{0xfeedfacf, cpu, 3, 2, 1, 24, 0, 0, 0x32, 24, platform, 0, 0, 0} {
		binary.LittleEndian.PutUint32(b[i*4:], v)
	}
	if e := os.WriteFile(path, b, 0755); e != nil {
		t.Fatal(e)
	}
}
func TestAppArchitectureAndPlatform(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Demo.app")
	_ = os.Mkdir(app, 0700)
	for _, tt := range []struct {
		name          string
		cpu, platform uint32
		arch          string
		valid         bool
	}{{"intel simulator", 0x1000007, 7, "x86_64", true}, {"wrong cpu", 0x100000c, 7, "x86_64", false}, {"device slice", 0x100000c, 2, "arm64", false}, {"arm simulator", 0x100000c, 7, "arm64", true}} {
		t.Run(tt.name, func(t *testing.T) {
			writeMachO(t, filepath.Join(app, "Demo"), tt.cpu, tt.platform)
			e := ValidateApp(app, tt.arch)
			if (e == nil) != tt.valid {
				t.Fatal(e)
			}
		})
	}
}
func TestPrewarmCacheInvalidatesOnReboot(t *testing.T) {
	a, _, _ := testApp(t)
	boot := "boot1"
	scripts := 0
	a.Run = func(_ context.Context, args, env []string) (string, error) {
		if args[0] == "ssh" {
			return boot + " hash", nil
		}
		scripts++
		return "hash", nil
	}
	ep := config.Defaults().Endpoints["local"]
	for i := 0; i < 2; i++ {
		if e := a.prewarm(ep); e != nil {
			t.Fatal(e)
		}
	}
	if scripts != 1 {
		t.Fatal(scripts)
	}
	boot = "boot2"
	if e := a.prewarm(ep); e != nil {
		t.Fatal(e)
	}
	if scripts != 2 {
		t.Fatal(scripts)
	}
}
func TestRepeatedUpRetriesFailedPrewarm(t *testing.T) {
	a, _, calls := testApp(t)
	c := config.Defaults()
	_ = a.save(Session{Mode: "headless", Profile: c.Profiles["headless"], Endpoint: c.Endpoints["local"], Stage: "GUEST_READY"})
	if exit := a.Main([]string{"up"}); exit != 0 {
		t.Fatal(exit)
	}
	found := false
	for _, call := range *calls {
		found = found || strings.Contains(call[0], "prewarm-metal")
	}
	if !found {
		t.Fatal("readiness bypassed")
	}
	s, _ := a.session()
	if s.Stage != "PREWARMED" {
		t.Fatal(s)
	}
}
func TestHybridCannotOrphanActiveLocalSession(t *testing.T) {
	a, _, calls := testApp(t)
	c := config.Defaults()
	_ = a.save(Session{Mode: "headless", Profile: c.Profiles["headless"], Endpoint: c.Endpoints["local"], Stage: "SIM_READY"})
	if exit := a.Main([]string{"up", "--mode=hybrid"}); exit != 4 {
		t.Fatal(exit)
	}
	if len(*calls) != 0 {
		t.Fatal(*calls)
	}
}
func TestColdHeadlessLaunchAppliesSettingsAndReadiness(t *testing.T) {
	a, _, calls := testApp(t)
	a.AvailableMiB = func() int64 { return 32768 }
	path := filepath.Join(a.home(), "home/.local/share/reims-ctl/vms/mac/vm.toml")
	_ = config.Atomic(path, []byte("name='mac'\ncpus=8\nmemory_mib=8192\n"))
	base := a.Run
	launches := 0
	a.Run = func(ctx context.Context, args, env []string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "inspect") {
			*calls = append(*calls, args)
			return "false", nil
		}
		if strings.HasSuffix(args[0], "macbox/macbox") {
			launches++
			joined := strings.Join(env, "\n")
			if !strings.Contains(joined, "MACBOX_HEADLESS=true") || !strings.Contains(joined, "MACBOX_RESOLUTION=640x480") {
				t.Fatal(env)
			}
		}
		return base(ctx, args, env)
	}
	if exit := a.Main([]string{"up"}); exit != 0 {
		t.Fatal(exit, a.Err)
	}
	if launches != 1 {
		t.Fatal(launches)
	}
	s, _ := a.session()
	if s.Stage != "PREWARMED" || s.Mode != "headless" {
		t.Fatal(s)
	}
	if _, e := os.Stat(a.ConfigPath); !os.IsNotExist(e) {
		t.Fatal("up rewrote default")
	}
}
func TestMemoryAdmissionBeforeMutation(t *testing.T) {
	a, _, calls := testApp(t)
	a.AvailableMiB = func() int64 { return 100 }
	a.Run = func(_ context.Context, args, env []string) (string, error) {
		*calls = append(*calls, args)
		return "false", nil
	}
	if exit := a.Main([]string{"up"}); exit != 4 {
		t.Fatal(exit)
	}
	if len(*calls) != 1 {
		t.Fatal(*calls)
	}
	if _, e := os.Stat(filepath.Join(a.StateDir, "session.json")); !os.IsNotExist(e) {
		t.Fatal("session mutated")
	}
}
func TestHybridDownKeepsMacRunning(t *testing.T) {
	a, _, calls := testApp(t)
	c := config.Defaults()
	_ = a.save(Session{Mode: "hybrid", Profile: c.Profiles["hybrid"], Endpoint: c.Endpoints["lan_mac"], Stage: "GUEST_READY"})
	if exit := a.Main([]string{"down"}); exit != 0 {
		t.Fatal(exit)
	}
	if len(*calls) != 0 {
		t.Fatal(*calls)
	}
}
func TestGracefulShutdownRetiresContainerOnlyAfterVMStops(t *testing.T) {
	a, _, calls := testApp(t)
	c := config.Defaults()
	_ = a.save(Session{Mode: "stream", Profile: c.Profiles["stream"], Endpoint: c.Endpoints["local"], Stage: "SIM_READY"})
	running := true
	a.Run = func(_ context.Context, args, env []string) (string, error) {
		*calls = append(*calls, args)
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "inspect"):
			return "true", nil
		case strings.Contains(joined, "reims-ctl list"):
			if running {
				return "mac running", nil
			}
			return "mac stopped", nil
		case strings.Contains(joined, "shutdown -h now"):
			running = false
			return "", nil
		case len(args) > 1 && (args[1] == "stop" || args[1] == "rm"):
			if running {
				t.Fatal("container stopped before VM")
			}
			return "", nil
		}
		return "", nil
	}
	if exit := a.Main([]string{"down"}); exit != 0 {
		t.Fatal(exit, a.Err)
	}
	s, _ := a.session()
	if s.Stage != "STOPPED" {
		t.Fatal(s)
	}
	last := (*calls)[len(*calls)-1]
	if len(last) < 2 || last[1] != "rm" {
		t.Fatal(*calls)
	}
}
