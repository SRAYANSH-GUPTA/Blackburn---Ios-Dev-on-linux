// Package cli implements Blackburn's synchronous control plane.
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"blackburn/internal/config"
	"blackburn/internal/hardware"
	"github.com/pelletier/go-toml/v2"
)

type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Exit    int    `json:"-"`
}

func (f *Failure) Error() string            { return f.Message }
func fail(code, msg string, exit int) error { return &Failure{code, msg, exit} }

type Runner func(context.Context, []string, []string) (string, error)

func Execute(ctx context.Context, args, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = env
	b, e := cmd.CombinedOutput()
	if e != nil {
		return string(b), fmt.Errorf("%s: %w: %s", args[0], e, strings.TrimSpace(string(b)))
	}
	return string(b), nil
}

type App struct {
	In                   io.Reader
	ProbeHardware        func() (hardware.Report, error)
	Out, Err             io.Writer
	Run                  Runner
	Root                 string
	Env                  []string
	ConfigPath, StateDir string
	Timeout              time.Duration
	AvailableMiB         func() int64
}
type Session struct {
	Viewer   string          `json:"viewer,omitempty"`
	Mode     string          `json:"mode"`
	Profile  config.Profile  `json:"profile"`
	Endpoint config.Endpoint `json:"endpoint"`
	Stage    string          `json:"stage"`
	Device   string          `json:"device,omitempty"`
}
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func New(root string) *App {
	a := &App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Run: Execute, Root: root, Env: os.Environ(), ConfigPath: config.Path(), StateDir: filepath.Join(config.Base("XDG_STATE_HOME", ".local/state"), "blackburn"), Timeout: 180 * time.Second}
	a.StateDir = a.env("BLACKBURN_STATE_DIR", a.StateDir)
	return a
}
func (a *App) available() int64 {
	if a.AvailableMiB != nil {
		return a.AvailableMiB()
	}
	return memAvailable()
}
func (a *App) env(key, def string) string {
	for _, s := range a.Env {
		k, v, _ := strings.Cut(s, "=")
		if k == key {
			return v
		}
	}
	return def
}
func (a *App) call(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.Timeout)
	defer cancel()
	return a.Run(ctx, args, a.Env)
}
func (a *App) json(v any) { _ = json.NewEncoder(a.Out).Encode(v) }
func (a *App) lock() (func(), error) {
	if e := os.MkdirAll(a.StateDir, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(a.StateDir, "operation.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fail("RESOURCE_BUSY", "another Blackburn operation is in progress", 4)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
func (a *App) session() (Session, error) {
	var s Session
	b, e := os.ReadFile(filepath.Join(a.StateDir, "session.json"))
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	return s, e
}
func (a *App) save(s Session) error {
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	return config.Atomic(filepath.Join(a.StateDir, "session.json"), b)
}
func (a *App) ssh(ep config.Endpoint, command string) (string, error) {
	return a.call("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", ep.SSH, command)
}
func (a *App) docker(args ...string) (string, error) {
	return a.call(append([]string{a.env("MACBOX_DOCKER", "docker")}, args...)...)
}
func (a *App) container() string { return a.env("MACBOX_CONTAINER", "macbox") }
func (a *App) vm() string        { return a.env("MACBOX_VM", "mac") }
func (a *App) home() string {
	return a.env("MACBOX_HOME", filepath.Join(config.Base("XDG_DATA_HOME", ".local/share"), "macbox"))
}
func (a *App) ctl(args ...string) (string, error) {
	argv := []string{"exec", "-e", "HOME=" + filepath.Join(a.home(), "home"), a.container(), "bash", "-c", `. "$HOME/.cargo/env"; exec reims-ctl "$@"`, "reims-ctl"}
	return a.docker(append(argv, args...)...)
}
func (a *App) running() (bool, error) {
	s, e := a.docker("inspect", "--format", "{{.State.Running}}", a.container())
	if e != nil {
		if strings.Contains(strings.ToLower(s+e.Error()), "no such") {
			return false, nil
		}
		return false, e
	}
	if strings.TrimSpace(s) != "true" {
		return false, nil
	}
	s, e = a.ctl("list")
	if e != nil {
		return false, e
	}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) > 1 && f[0] == a.vm() {
			return f[1] == "running", nil
		}
	}
	return false, nil
}
func (a *App) Main(args []string) int {
	a.Env = append([]string{"PATH=" + filepath.Join(a.Root, "bin") + string(os.PathListSeparator) + a.env("PATH", os.Getenv("PATH"))}, withoutPath(a.Env)...)
	wantJSON := false
	for _, arg := range args {
		if arg == "--json" {
			wantJSON = true
		}
	}
	e := a.dispatch(args)
	if e == nil {
		return 0
	}
	var diagnosed *diagnosedFailure
	if errors.As(e, &diagnosed) {
		return 3
	}
	var f *Failure
	if !errors.As(e, &f) {
		f = &Failure{"OPERATION_FAILED", e.Error(), 5}
	}
	if wantJSON {
		a.json(map[string]any{"schema_version": 1, "error": f})
	} else {
		fmt.Fprintf(a.Err, "blackburn: %s: %s\n", f.Code, f.Message)
	}
	return f.Exit
}
func parse(name string, args []string, setup func(*flag.FlagSet)) (*flag.FlagSet, error) {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	setup(f)
	if e := f.Parse(args); e != nil {
		return nil, fail("INVALID_ARGUMENT", e.Error(), 2)
	}
	return f, nil
}
func (a *App) dispatch(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(a.Out, "Usage: blackburn init [--auto]\n       blackburn appliance export [--output=PATH] [--compress=zstd|none]\n       blackburn appliance import FILE [--force]\n       blackburn ide <config|f5|status> [--dir=DIR] [--mode=PRESET] [--dry-run] [--json]\n       blackburn view list|get|set VIEWER [--allow-restart]|detach\n       blackburn mode [list|get|set PRESET]\n       blackburn up [--mode=PRESET]\n       blackburn down [--timeout=120s]\n       blackburn status [--json]\n       blackburn doctor [--mode=PRESET] [--json]\n       blackburn sim <up|shot|install|run|status|down|stream|touch|tap|key|text> [arguments]\n       blackburn config <get KEY|set KEY VALUE|show|validate>\nPresets: headless (agent), stream, studio (local), hybrid (remote), direct")
		return nil
	}
	env, legacyErr := LegacyEnv(a.Env)
	if legacyErr != nil {
		return fail("INVALID_CONFIG", legacyErr.Error(), 2)
	}
	a.Env = env
	c, e := config.Load(a.ConfigPath)
	if e != nil {
		return fail("INVALID_CONFIG", e.Error(), 2)
	}
	if args[0] == "init" {
		return a.initialize(c, args[1:])
	}
	if args[0] == "appliance" {
		return a.appliance(c, args[1:])
	}
	cmd := args[0]
	args = args[1:]
	if cmd == "ide" {
		return a.ide(c, args)
	}
	if cmd == "view" {
		return a.view(args)
	}
	if cmd == "mode" || cmd == "config" {
		unlock, e := a.lock()
		if e != nil {
			return e
		}
		defer unlock()
		if cmd == "mode" {
			return a.mode(c, args)
		}
		return a.configuration(c, args)
	}
	mode := ""
	asJSON := false
	timeout := 120 * time.Second
	wait := ""
	if cmd == "up" || cmd == "doctor" || cmd == "status" || cmd == "down" {
		f, e := parse(cmd, args, func(f *flag.FlagSet) {
			if cmd == "up" || cmd == "doctor" {
				f.StringVar(&mode, "mode", "", "preset")
			}
			if cmd == "up" {
				f.StringVar(&wait, "wait", "", "wait target: simulator")
			}
			if cmd == "doctor" || cmd == "status" {
				f.BoolVar(&asJSON, "json", false, "JSON output")
			}
			if cmd == "down" {
				f.DurationVar(&timeout, "timeout", timeout, "graceful shutdown deadline")
			}
		})
		if e != nil {
			return e
		}
		if f.NArg() != 0 {
			return fail("INVALID_ARGUMENT", "unexpected arguments", 2)
		}
		if timeout <= 0 {
			return fail("INVALID_ARGUMENT", "timeout must be positive", 2)
		}
	}
	effective, m, e := config.Effective(c, mode, a.Env)
	if e != nil {
		return fail("INVALID_CONFIG", e.Error(), 2)
	}
	p := effective.Profiles[m]
	ep := effective.Endpoints[p.Endpoint]
	if cmd == "status" {
		return a.status(m, asJSON)
	}
	if cmd == "doctor" {
		return a.doctor(p, ep, asJSON)
	}
	if cmd != "up" && cmd != "down" && cmd != "sim" {
		return fail("INVALID_ARGUMENT", "unknown command "+cmd, 2)
	}
	// Streaming observes the lease without holding the operation lock for its
	// lifetime, allowing touch/text and explicit lifecycle commands to proceed.
	if cmd == "sim" && len(args) > 0 && args[0] == "stream" {
		s, err := a.session()
		if err != nil {
			return err
		}
		if s.Mode == "" {
			s = Session{Mode: m, Profile: p, Endpoint: ep}
		}
		return a.sim(&s, args)
	}
	unlock, e := a.lock()
	if e != nil {
		return e
	}
	defer unlock()
	s, e := a.session()
	if e != nil {
		return e
	}
	if cmd == "up" {
		if wait != "" && wait != "simulator" {
			return fail("INVALID_ARGUMENT", "supported wait targets: simulator", 2)
		}
		if err := a.up(m, p, ep, s); err != nil {
			return err
		}
		if wait == "simulator" {
			s, err := a.session()
			if err != nil {
				return err
			}
			if s.Mode == "" {
				s = Session{Mode: m, Profile: p, Endpoint: ep}
			}
			return a.sim(&s, []string{"up"})
		}
		return nil
	}
	if s.Mode == "" && cmd == "sim" {
		s = Session{Mode: m, Profile: p, Endpoint: ep}
	}
	if cmd == "down" {
		return a.down(s, timeout)
	}
	return a.sim(&s, args)
}
func (a *App) mode(c config.Config, args []string) error {
	if len(args) == 0 {
		args = []string{"get"}
	}
	switch {
	case len(args) == 1 && args[0] == "list":
		for _, m := range config.Modes {
			p := c.Profiles[m]
			fmt.Fprintf(a.Out, "%-8s %5d MiB %d vCPUs %s\n", m, p.Memory, p.CPUs, p.Presentation)
		}
		return nil
	case len(args) == 1 && args[0] == "get":
		_, m, e := config.Effective(c, "", a.Env)
		if e != nil {
			return fail("INVALID_CONFIG", e.Error(), 2)
		}
		fmt.Fprintln(a.Out, m)
		return nil
	case len(args) == 2 && args[0] == "set":
		m, e := config.Normalize(args[1])
		if e != nil {
			return fail("INVALID_ARGUMENT", e.Error(), 2)
		}
		c.Mode = m
		if e = config.Save(a.ConfigPath, c); e != nil {
			return e
		}
		fmt.Fprintf(a.Out, "Default mode: %s (active session unchanged)\n", m)
		return nil
	}
	return fail("INVALID_ARGUMENT", "usage: mode list|get|set PRESET", 2)
}
func (a *App) configuration(c config.Config, args []string) error {
	if len(args) == 0 {
		args = []string{"show"}
	}
	switch args[0] {
	case "show":
		if len(args) == 2 && args[1] == "--effective" {
			var e error
			c, _, e = config.Effective(c, "", a.Env)
			if e != nil {
				return fail("INVALID_CONFIG", e.Error(), 2)
			}
		} else if len(args) != 1 {
			return fail("INVALID_ARGUMENT", "usage: config show [--effective]", 2)
		}
		b, e := toml.Marshal(c)
		if e == nil {
			_, e = a.Out.Write(b)
		}
		return e
	case "validate":
		if len(args) == 1 {
			fmt.Fprintln(a.Out, "Configuration valid")
			return nil
		}
	case "get":
		if len(args) == 2 {
			b, _ := toml.Marshal(c)
			var v any
			_ = toml.Unmarshal(b, &v)
			for _, part := range strings.Split(args[1], ".") {
				table, ok := v.(map[string]any)
				if !ok {
					return fail("INVALID_ARGUMENT", "unknown config key", 2)
				}
				v, ok = table[part]
				if !ok {
					return fail("INVALID_ARGUMENT", "unknown config key", 2)
				}
			}
			switch value := v.(type) {
			case map[string]any:
				a.json(value)
			default:
				fmt.Fprintln(a.Out, v)
			}
			return nil
		}
	case "set":
		if len(args) == 3 {
			if e := config.Set(&c, args[1], args[2]); e != nil {
				return fail("INVALID_CONFIG", e.Error(), 2)
			}
			if e := c.Validate(); e != nil {
				return fail("INVALID_CONFIG", e.Error(), 2)
			}
			return config.Save(a.ConfigPath, c)
		}
	}
	return fail("INVALID_ARGUMENT", "usage: config get KEY|set KEY VALUE|show [--effective]|validate", 2)
}
func (a *App) prewarm(ep config.Endpoint) error {
	if ep.Provider == "remote-mac" { // AppleParavirtGPU is a local VM workaround, not a physical-Mac dependency.
		_, e := a.ssh(ep, "xcrun simctl list devices --json")
		return e
	}
	identity, e := a.ssh(ep, "sysctl -n kern.boottime && /usr/bin/shasum -a 256 /System/Library/Extensions/AppleParavirtGPUMetalIOGPUFamily.bundle/Contents/MacOS/AppleParavirtGPUMetalIOGPUFamily")
	if e != nil {
		return fail("PREWARM_FAILED", e.Error(), 5)
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(ep.SSH+"\n"+identity)))
	stamp := filepath.Join(a.StateDir, "prewarm", fmt.Sprintf("%x", sha256.Sum256([]byte(ep.SSH))))
	if old, err := os.ReadFile(stamp); err == nil && string(old) == key {
		return nil
	}
	env := append(append([]string{}, a.Env...), "BLACKBURN_SSH_ALIAS="+ep.SSH)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	_, e = a.Run(ctx, []string{filepath.Join(a.Root, "scripts/blackburn-prewarm-metal.sh")}, env)
	if e != nil {
		return fail("PREWARM_FAILED", e.Error(), 5)
	}
	return config.Atomic(stamp, []byte(key))
}
func (a *App) up(m string, p config.Profile, ep config.Endpoint, s Session) error {
	if ep.Provider == "direct-ios" {
		return fail("CAPABILITY_UNSUPPORTED", "direct iOS provider is experimental and not installed", 3)
	}
	if s.Mode != "" && s.Stage != "STOPPED" && s.Endpoint != ep {
		return fail("RESOURCE_BUSY", "another endpoint session is active; release it with blackburn down first", 4)
	}
	if ep.Provider == "remote-mac" {
		if _, e := a.ssh(ep, "uname -m && xcrun --find simctl"); e != nil {
			return e
		}
		if s.Mode != m || s.Stage == "STOPPED" {
			s = Session{Mode: m, Profile: p, Endpoint: ep, Stage: "GUEST_READY"}
		}
		if e := a.save(s); e != nil {
			return e
		}
		fmt.Fprintln(a.Out, "Remote endpoint ready; no local VM allocated")
		return nil
	}
	running, e := a.running()
	if e != nil {
		return e
	}
	if running {
		if s.Mode != m || !reflect.DeepEqual(s.Profile, p) || s.Endpoint != ep {
			return fail("RESTART_REQUIRED", "running VM has a different or unmanaged profile; run blackburn down before up", 6)
		}
		if p.Presentation == "native" {
			if _, err := a.call(filepath.Join(a.Root, "macbox/macbox"), "view", "capabilities-running"); err != nil {
				return fail("UNMET_PREREQUISITE", err.Error(), 3)
			}
		}
		if e = a.prewarm(ep); e != nil {
			return e
		}
		if s.Stage != "SIM_READY" {
			s.Stage = "PREWARMED"
			if e = a.save(s); e != nil {
				return e
			}
		}
		fmt.Fprintln(a.Out, "VM already running; profile unchanged; prewarm verified")
		return nil
	}
	if p.Presentation == "native" {
		if e := a.nativeSocket(); e != nil {
			return e
		}
		if _, e := a.call(filepath.Join(a.Root, "macbox/macbox"), "view", "capabilities"); e != nil {
			return fail("UNMET_PREREQUISITE", e.Error(), 3)
		}
	}
	// Cold-start admission includes 2 GiB runtime overhead and a 3 GiB host reserve.
	available := a.available()
	if available > 0 && available < int64(p.Memory+5120) {
		return fail("MEMORY_ADMISSION_FAILED", fmt.Sprintf("need %d MiB available; have %d MiB", p.Memory+5120, available), 4)
	}
	c, e := config.Load(a.ConfigPath)
	if e != nil {
		return e
	}
	tuning := []string{"MACBOX_CPUS=" + strconv.Itoa(p.CPUs), "MACBOX_MEMORY=" + strconv.Itoa((p.Memory+1023)/1024) + "G"}
	host, probeErr := a.probeHardware()
	if probeErr == nil {
		for _, warning := range host.Warnings {
			fmt.Fprintln(a.Err, "Warning:", warning)
		}
	}
	if c.CPUSet != "" {
		if probeErr != nil {
			return probeErr
		}
		allowed := map[int]bool{}
		for _, cpu := range host.CPUs {
			allowed[cpu.ID] = true
		}
		ids, _ := hardware.List(c.CPUSet)
		for _, id := range ids {
			if !allowed[id] {
				return fmt.Errorf("configured CPU %d is offline or unavailable; rerun blackburn init", id)
			}
		}
		tuning = append(tuning, "MACBOX_CPUSET="+c.CPUSet)
	}
	if e = a.applyProfile(p); e != nil {
		return e
	}
	env := append(append(append([]string{}, a.Env...), tuning...), "MACBOX_HEADLESS="+strconv.FormatBool(!p.Window), "MACBOX_SSH_HOST="+ep.SSH, "MACBOX_RESOLUTION="+p.Display)
	view := "stream"
	if !p.Window {
		view = "none"
	}
	if p.Presentation == "native" {
		view = "local"
	}
	if e = config.Atomic(filepath.Join(a.home(), "run/view"), []byte(view+"\n")); e != nil {
		return e
	}
	if e = config.Atomic(filepath.Join(a.home(), "run/resolution"), []byte(p.Display+"\n")); e != nil {
		return e
	}
	s = Session{Mode: m, Profile: p, Endpoint: ep, Stage: "STARTING"}
	if e = a.save(s); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.Timeout)
	defer cancel()
	out, e := a.Run(ctx, []string{filepath.Join(a.Root, "macbox/macbox"), "up"}, env)
	fmt.Fprint(a.Err, out)
	if e != nil {
		return e
	}
	deadline := time.Now().Add(a.Timeout)
	for {
		if _, e = a.ssh(ep, "xcrun --find simctl"); e == nil {
			break
		}
		if time.Now().After(deadline) {
			return fail("GUEST_UNAVAILABLE", "guest readiness deadline exceeded", 5)
		}
		time.Sleep(time.Second)
	}
	s.Stage = "GUEST_READY"
	if e = a.save(s); e != nil {
		return e
	}
	if e = a.prewarm(ep); e != nil {
		return e
	}
	s.Stage = "PREWARMED"
	if e = a.save(s); e != nil {
		return e
	}
	fmt.Fprintf(a.Out, "%s ready (Metal prewarm passed)\n", m)
	return nil
}
func (a *App) applyProfile(p config.Profile) error {
	path := filepath.Join(a.home(), "home/.local/share/reims-ctl/vms", a.vm(), "vm.toml")
	b, e := os.ReadFile(path)
	if e != nil {
		return fail("UNMET_PREREQUISITE", "existing macbox VM required: "+e.Error(), 3)
	}
	var v map[string]any
	if e = toml.Unmarshal(b, &v); e != nil {
		return e
	}
	v["cpus"] = p.CPUs
	v["memory_mib"] = p.Memory
	display, ok := v["display"].(map[string]any)
	if !ok {
		display = map[string]any{}
		v["display"] = display
	}
	display["resolution"] = p.Display
	env, ok := v["extra_env"].(map[string]any)
	if !ok {
		env = map[string]any{}
		v["extra_env"] = env
	}
	c, e := config.Load(a.ConfigPath)
	if e != nil {
		return e
	}
	env["MESA_SHADER_CACHE_MAX_SIZE"] = c.ShaderCache
	env["REIMS_VGPU_CLOSE_POLICY"] = "detach"
	env["REIMS_VGPU_VIEW_STATE"] = filepath.Join(a.home(), "run/view-state")
	env["REIMS_VGPU_WINDOW"] = "1"
	if !p.Window {
		env["REIMS_VGPU_WINDOW"] = "0"
	}
	// Preserve all unrelated provider options and network/disk flags.
	extra := []any{}
	if old, ok := v["extra_args"].([]any); ok {
		for i := 0; i < len(old); i++ {
			if old[i] == "-display" {
				i++
				continue
			}
			extra = append(extra, old[i])
		}
	}
	if !p.Window {
		extra = append(extra, "-display", "none")
	}
	v["extra_args"] = extra
	if e = config.Atomic(path+".blackburn-backup", b); e != nil {
		return e
	}
	b, e = toml.Marshal(v)
	if e != nil {
		return e
	}
	return config.Atomic(path, b)
}

// retireStoppedContainer is called only after QEMU is observed stopped. Recreating
// the container on the next up applies the selected native Wayland mount policy.
func (a *App) retireStoppedContainer() error {
	out, e := a.docker("inspect", "--format", "{{.State.Running}}", a.container())
	if e != nil {
		if strings.Contains(strings.ToLower(out+e.Error()), "no such") {
			return nil
		}
		return e
	}
	if strings.TrimSpace(out) == "true" {
		if _, e = a.docker("stop", a.container()); e != nil {
			return e
		}
	}
	_, e = a.docker("rm", a.container())
	return e
}
func (a *App) down(s Session, timeout time.Duration) error {
	if s.Endpoint.Provider == "remote-mac" {
		if s.Device != "" {
			if e := a.sim(&s, []string{"down"}); e != nil {
				return e
			}
		}
		s.Stage = "STOPPED"
		if e := a.save(s); e != nil {
			return e
		}
		fmt.Fprintln(a.Out, "Remote session released; shared Mac remains running")
		return nil
	}
	if s.Endpoint.Provider == "direct-ios" {
		return fail("CAPABILITY_UNSUPPORTED", "direct provider unavailable", 3)
	}
	running, e := a.running()
	if e != nil {
		return e
	}
	if !running {
		if s.Mode != "" {
			if e = a.retireStoppedContainer(); e != nil {
				return e
			}
		}
		fmt.Fprintln(a.Out, "VM already stopped")
		s.Stage = "STOPPED"
		s.Device = ""
		return a.save(s)
	}
	if s.Mode == "" {
		return fail("RESOURCE_BUSY", "refusing to shut down an unmanaged VM", 4)
	}
	s.Stage = "QUIESCING"
	if e = a.save(s); e != nil {
		return e
	}
	if _, e = a.ssh(s.Endpoint, "sudo -n shutdown -h now"); e != nil {
		return e
	}
	deadline := time.Now().Add(timeout)
	for {
		running, e = a.running()
		if e != nil {
			return e
		}
		if !running {
			break
		}
		if time.Now().After(deadline) {
			return fail("SHUTDOWN_TIMEOUT", "graceful shutdown timed out; VM retained, no forced stop attempted", 5)
		}
		time.Sleep(time.Second)
	}
	if e = a.retireStoppedContainer(); e != nil {
		return e
	}
	s.Stage = "STOPPED"
	s.Device = ""
	if e = a.save(s); e != nil {
		return e
	}
	fmt.Fprintln(a.Out, "VM stopped gracefully")
	return nil
}
func memAvailable() int64 {
	b, _ := os.ReadFile("/proc/meminfo")
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) > 1 && f[0] == "MemAvailable:" {
			n, _ := strconv.ParseInt(f[1], 10, 64)
			return n / 1024
		}
	}
	return 0
}
func (a *App) status(desired string, asJSON bool) error {
	s, e := a.session()
	if e != nil {
		return e
	}
	state := "stopped"
	observation := ""
	viewer := "none"
	devices := []map[string]any{}
	if s.Mode != "" && s.Endpoint.Provider == "remote-mac" && s.Stage == "STOPPED" {
		state = "released"
	} else if s.Mode != "" && s.Endpoint.Provider == "remote-mac" {
		if _, e = a.ssh(s.Endpoint, "true"); e == nil {
			state = "reachable"
		} else {
			state = "unreachable"
			observation = e.Error()
		}
	} else {
		running, e := a.running()
		if e != nil {
			state = "unknown"
			observation = e.Error()
		} else if running {
			state = "running"
		}
	}
	if state == "running" || state == "reachable" {
		viewer = s.Profile.Presentation
		if s.Endpoint.Provider == "macbox" {
			observed, err := a.viewer(s)
			if err != nil {
				viewer = "unknown"
				observation = err.Error()
			} else {
				viewer = observed
			}
		}
		if s.Mode == "" {
			viewer = "unknown"
		}
		if out, e := a.ssh(s.Endpoint, "xcrun simctl list devices booted --json"); e == nil {
			var raw struct {
				Devices map[string][]map[string]any `json:"devices"`
			}
			if json.Unmarshal([]byte(out), &raw) == nil {
				for _, list := range raw.Devices {
					devices = append(devices, list...)
				}
			}
		} else {
			observation = e.Error()
		}
	}
	transport := "none"
	if viewer == "browser-desktop" {
		transport = "websocket"
	}
	if viewer == "remote-stream" {
		transport = "unconfigured"
	}
	memory := map[string]any{"guest_allocation_mib": s.Profile.Memory, "host_available_mib": memAvailable(), "qemu_rss_mib": nil}
	if state == "running" {
		if out, e := a.docker("top", a.container(), "-eo", "comm,rss"); e == nil {
			for _, line := range strings.Split(out, "\n") {
				f := strings.Fields(line)
				if len(f) == 2 && strings.HasPrefix(f[0], "qemu-system") {
					n, e := strconv.ParseInt(f[1], 10, 64)
					if e == nil {
						memory["qemu_rss_mib"] = n / 1024
					}
				}
			}
		}
	}
	var active any
	for _, d := range devices {
		if d["udid"] == s.Device {
			active = s.Device
		}
	}
	result := map[string]any{"schema_version": 1, "leased_simulator": s.Device, "desired_mode": desired, "running_mode": nil, "vm_state": state, "stage": s.Stage, "active_simulator": active, "booted_simulators": devices, "viewer_type": viewer, "view_transport": transport, "viewer_observation": "provider presentation state; client connection not verified", "memory": memory, "diagnostic": observation}
	if s.Mode != "" && (state == "running" || state == "reachable") {
		result["running_mode"] = s.Mode
	}
	if asJSON {
		a.json(result)
	} else {
		fmt.Fprintf(a.Out, "Mode: %v (default %s)\nVM: %s; stage: %s\nSimulator: %s\nViewer: %s (%s; client connection unverified)\nMemory: guest %d MiB; host available %d MiB\n", result["running_mode"], desired, state, s.Stage, s.Device, viewer, transport, s.Profile.Memory, memAvailable())
		if observation != "" {
			fmt.Fprintln(a.Out, observation)
		}
	}
	return nil
}
func (a *App) doctor(p config.Profile, ep config.Endpoint, asJSON bool) error {
	checks := []Check{}
	add := func(name, detail string, e error) {
		if e != nil {
			detail = e.Error()
		}
		checks = append(checks, Check{name, e == nil, strings.TrimSpace(detail)})
	}
	probe := func(name string, args ...string) { s, e := a.call(args...); add(name, s, e) }
	link, linkErr := os.Readlink(a.xcrossLink())
	if linkErr == nil && link != filepath.Join(a.Root, "bin", "xcross") {
		linkErr = fmt.Errorf("xcross points to %s", link)
	}
	if linkErr != nil {
		linkErr = fmt.Errorf("%v; run blackburn init --auto to install bundled xcross", linkErr)
	}
	add("bundled_xcross", link, linkErr)
	probe("xcross", filepath.Join(a.Root, "bin", "xcross"), "--version")
	if ep.Provider == "direct-ios" {
		add("provider", "", errors.New("direct iOS provider unavailable"))
	} else {
		if ep.Provider == "macbox" {
			for _, entry := range []struct{ name, path string }{{"kvm", "/dev/kvm"}, {"render_node", a.env("MACBOX_RENDER_NODE", "/dev/dri/renderD128")}} {
				f, e := os.OpenFile(entry.path, os.O_RDWR, 0)
				if e == nil {
					f.Close()
				}
				add(entry.name, entry.path, e)
			}
			probe("vulkan_icd", "vulkaninfo", "--summary")
			probe("docker", a.env("MACBOX_DOCKER", "docker"), "info")
			s, e := a.docker("inspect", "--format", "{{.State.Status}}", a.container())
			add("container", s, e)
		}
		for _, q := range []struct{ name, command string }{{"architecture", "uname -m"}, {"xcode", "xcode-select -p && xcodebuild -version"}, {"simctl", "xcrun --find simctl"}, {"sdks", "xcodebuild -showsdks"}, {"runtimes", "xcrun simctl list runtimes --json"}} {
			s, e := a.ssh(ep, q.command)
			if q.name == "architecture" && e == nil && ep.Architecture != "auto" && strings.TrimSpace(s) != ep.Architecture {
				e = fmt.Errorf("expected %s, observed %s", ep.Architecture, strings.TrimSpace(s))
			}
			add(q.name, s, e)
		}
	}
	ok := true
	for _, c := range checks {
		ok = ok && c.OK
	}
	result := map[string]any{"schema_version": 1, "ok": ok, "endpoint": p.Endpoint, "checks": checks, "scope": "prerequisites; does not certify GPU rendering or toolchain compatibility"}
	if asJSON {
		a.json(result)
	} else {
		for _, c := range checks {
			label := "OK"
			if !c.OK {
				label = "FAIL"
			}
			fmt.Fprintf(a.Out, "%s %-14s %s\n", label, c.Name, c.Detail)
		}
	}
	if !ok { // Result already carries diagnostics; avoid a second JSON object.
		return &diagnosedFailure{}
	}
	return nil
}

type diagnosedFailure struct{}

func (*diagnosedFailure) Error() string { return "doctor found unmet prerequisites" }
