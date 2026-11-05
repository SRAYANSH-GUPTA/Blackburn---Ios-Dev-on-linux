package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackburn/internal/config"
)

func TestViewerParsing(t *testing.T) {
	for alias, want := range map[string]string{"none": "none", "stream": "browser-desktop", "browser-desktop": "browser-desktop", "native": "native", "studio": "native", "local": "native"} {
		got, e := normalizeViewer(alias)
		if e != nil || got != want {
			t.Fatalf("%s: %s %v", alias, got, e)
		}
	}
	for _, args := range [][]string{{"set"}, {"set", "bad"}, {"get", "extra"}, {"detach", "--allow-restart"}, {"set", "none", "extra"}, {"--wat"}} {
		a, _, calls := testApp(t)
		if exit := a.Main(append([]string{"view"}, args...)); exit != 2 {
			t.Fatalf("%v: exit %d", args, exit)
		}
		if len(*calls) != 0 {
			t.Fatal("invalid arguments invoked provider", *calls)
		}
	}
	a, out, calls := testApp(t)
	if a.Main([]string{"view", "list"}) != 0 {
		t.Fatal(a.Err)
	}
	for _, v := range []string{"none", "browser-desktop", "stream", "native", "studio", "local"} {
		if !strings.Contains(out.String(), v) {
			t.Fatal(out.String())
		}
	}
	if len(*calls) != 0 {
		t.Fatal(*calls)
	}
}

func TestTransitionPlanning(t *testing.T) {
	base := config.Defaults().Profiles["stream"]
	for _, tc := range []struct {
		name    string
		edit    func(*config.Profile)
		restart bool
	}{
		{"unchanged", func(*config.Profile) {}, false},
		{"memory", func(p *config.Profile) { p.Memory++ }, true},
		{"cpus", func(p *config.Profile) { p.CPUs++ }, true},
		{"wayland", func(p *config.Profile) { p.Presentation = "native" }, true},
		{"window", func(p *config.Profile) { p.Window = false; p.Presentation = "none" }, true},
		{"display", func(p *config.Profile) { p.Display = "640x480" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := base
			tc.edit(&target)
			plan, e := PlanTransition(base, target)
			if e != nil || plan.Restart != tc.restart {
				t.Fatal(plan, e)
			}
		})
	}
	target := base
	target.Endpoint = "lan_mac"
	if _, e := PlanTransition(base, target); e == nil {
		t.Fatal("cross endpoint accepted")
	}
}

func managedView(t *testing.T, mode string) (*App, *string, *[][]string) {
	a, _, calls := testApp(t)
	c := config.Defaults()
	s := Session{Mode: mode, Profile: c.Profiles[mode], Endpoint: c.Endpoints["local"], Stage: "SIM_READY", Device: "SIM-1"}
	if e := a.save(s); e != nil {
		t.Fatal(e)
	}
	current := s.Profile.Presentation
	run := a.Run
	a.Run = func(ctx context.Context, args, env []string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "macbox/macbox view ") {
			*calls = append(*calls, append([]string{}, args...))
			v := args[len(args)-1]
			if v == "get" {
				return current + "\n", nil
			}
			if v == "capabilities" {
				return "detach-v1\n", nil
			}
			current = v
			return "", nil
		}
		return run(ctx, args, env)
	}
	return a, &current, calls
}

func TestDetachAndReattachPreserveExecution(t *testing.T) {
	for _, mode := range []string{"stream", "studio"} {
		t.Run(mode, func(t *testing.T) {
			a, current, calls := managedView(t, mode)
			if a.Main([]string{"view", "detach"}) != 0 {
				t.Fatal(a.Err)
			}
			if *current != "none" {
				t.Fatal(*current)
			}
			s, _ := a.session()
			if s.Device != "SIM-1" || s.Stage != "SIM_READY" || s.Profile != config.Defaults().Profiles[mode] {
				t.Fatal(s)
			}
			if a.Main([]string{"view", "set", "none"}) != 0 {
				t.Fatal(a.Err)
			}
			for _, call := range *calls {
				v := strings.Join(call, " ")
				if strings.Contains(v, "shutdown") || strings.HasSuffix(v, " up") || strings.Contains(v, " stop") {
					t.Fatal("execution changed", v)
				}
			}
			if mode == "stream" && a.Main([]string{"view", "set", "stream"}) != 0 {
				t.Fatal(a.Err)
			}
		})
	}
}

func TestViewerStatusObservation(t *testing.T) {
	a, current, _ := managedView(t, "stream")
	for _, v := range []string{"none", "browser-desktop", "native"} {
		*current = v
		// A fresh buffer is needed for each versioned JSON response.
		b := new(strings.Builder)
		a.Out = b
		if a.Main([]string{"status", "--json"}) != 0 {
			t.Fatal(a.Err)
		}
		var result map[string]any
		if e := json.Unmarshal([]byte(b.String()), &result); e != nil {
			t.Fatal(e)
		}
		if result["viewer_type"] != v {
			t.Fatal(result)
		}
		b.Reset()
		if a.Main([]string{"view", "get"}) != 0 || b.String() != v+"\n" {
			t.Fatal(b.String(), a.Err)
		}
	}
}

func TestRestartPermissionAndCapabilitiesBeforeMutation(t *testing.T) {
	a, _, calls := managedView(t, "headless")
	if a.Main([]string{"view", "set", "stream"}) != 6 {
		t.Fatal(a.Err)
	}
	for _, args := range *calls {
		if strings.HasSuffix(strings.Join(args, " "), " up") {
			t.Fatal(args)
		}
	}
	if _, e := os.Stat(filepath.Join(a.StateDir, "view-transition.json")); !os.IsNotExist(e) {
		t.Fatal("journal changed without restart permission")
	}
	a.Run = func(context.Context, []string, []string) (string, error) { return "", errors.New("unpatched") }
	if a.Main([]string{"view", "set", "native", "--allow-restart"}) == 0 {
		t.Fatal("unpatched viewer accepted")
	}
}

func TestLiveViewerRollback(t *testing.T) {
	a, current, _ := managedView(t, "stream")
	run := a.Run
	a.Run = func(ctx context.Context, args, env []string) (string, error) {
		if strings.HasSuffix(strings.Join(args, " "), "view none") {
			*current = "none"
			return "", errors.New("failed after detach")
		}
		return run(ctx, args, env)
	}
	if a.Main([]string{"view", "detach"}) != 5 {
		t.Fatal(a.Err)
	}
	if *current != "browser-desktop" {
		t.Fatal("rollback failed", *current)
	}
	s, _ := a.session()
	if s.Viewer != "" || s.Device != "SIM-1" {
		t.Fatal(s)
	}
	b, _ := os.ReadFile(filepath.Join(a.StateDir, "view-transition.json"))
	if !strings.Contains(string(b), "rolled_back") {
		t.Fatal(string(b))
	}
}

func TestGracefulViewerRestartRestoresLease(t *testing.T) {
	a, current, calls := managedView(t, "headless")
	a.AvailableMiB = func() int64 { return 100000 }
	path := filepath.Join(a.home(), "home/.local/share/reims-ctl/vms/mac/vm.toml")
	if e := config.Atomic(path, []byte("cpus=4\nmemory_mib=5120\n")); e != nil {
		t.Fatal(e)
	}
	run := a.Run
	running := true
	a.Run = func(ctx context.Context, args, env []string) (string, error) {
		v := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(v, "sudo -n shutdown -h now"):
			running = false
		case strings.Contains(v, "reims-ctl list"):
			if running {
				return "mac running\n", nil
			}
			return "mac stopped\n", nil
		case strings.HasSuffix(v, "macbox/macbox up"):
			running = true
			*current = "browser-desktop"
		}
		return run(ctx, args, env)
	}
	if a.Main([]string{"view", "set", "stream", "--allow-restart"}) != 0 {
		t.Fatal(a.Err)
	}
	s, _ := a.session()
	if s.Device != "SIM-1" || s.Stage != "SIM_READY" || s.Profile.Memory != 5120 || s.Profile.CPUs != 4 || s.Profile.Presentation != "browser-desktop" {
		t.Fatal(s)
	}
	all := ""
	for _, call := range *calls {
		all += strings.Join(call, " ") + "\n"
	}
	for _, want := range []string{"simctl shutdown 'SIM-1'", "shutdown -h now", "bootstatus 'SIM-1' -b"} {
		if !strings.Contains(all, want) {
			t.Fatal(want, all)
		}
	}
	if strings.Contains(all, "--force") || strings.Contains(all, "macbox/macbox down") {
		t.Fatal(all)
	}
}

func TestRestartShutdownTimeoutNeverForcesOrRetries(t *testing.T) {
	a, _, calls := managedView(t, "headless")
	a.Timeout = 1
	if a.Main([]string{"view", "set", "stream", "--allow-restart"}) != 5 {
		t.Fatal(a.Err)
	}
	shutdowns := 0
	for _, args := range *calls {
		v := strings.Join(args, " ")
		if strings.Contains(v, "shutdown -h now") {
			shutdowns++
		}
		if strings.Contains(v, "--force") || strings.HasSuffix(v, "macbox/macbox up") || strings.Contains(v, "docker stop") {
			t.Fatal("timeout mutated execution", v)
		}
	}
	if shutdowns != 1 {
		t.Fatal("shutdown retried", shutdowns)
	}
	s, _ := a.session()
	if s.Device != "SIM-1" || s.Stage != "QUIESCING" {
		t.Fatal(s)
	}
}

func TestRestartFailureRecoversPreviousProfileOnce(t *testing.T) {
	a, current, _ := managedView(t, "headless")
	a.AvailableMiB = func() int64 { return 100000 }
	path := filepath.Join(a.home(), "home/.local/share/reims-ctl/vms/mac/vm.toml")
	if e := config.Atomic(path, []byte("cpus=4\nmemory_mib=5120\n")); e != nil {
		t.Fatal(e)
	}
	run := a.Run
	running := true
	starts := 0
	a.Run = func(ctx context.Context, args, env []string) (string, error) {
		v := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(v, "sudo -n shutdown -h now"):
			running = false
		case strings.Contains(v, "reims-ctl list"):
			if running {
				return "mac running\n", nil
			}
			return "mac stopped\n", nil
		case strings.HasSuffix(v, "macbox/macbox up"):
			starts++
			if starts == 1 {
				return "", errors.New("new presentation failed")
			}
			running = true
			*current = "none"
		}
		return run(ctx, args, env)
	}
	if a.Main([]string{"view", "set", "stream", "--allow-restart"}) != 5 {
		t.Fatal(a.Err)
	}
	if starts != 2 || !running {
		t.Fatal("bounded recovery failed", starts, running)
	}
	s, _ := a.session()
	if s.Profile != config.Defaults().Profiles["headless"] || s.Device != "SIM-1" || s.Stage != "SIM_READY" {
		t.Fatal(s)
	}
	b, _ := os.ReadFile(filepath.Join(a.StateDir, "view-transition.json"))
	if !strings.Contains(string(b), "rolled_back") {
		t.Fatal(string(b))
	}
}

func TestFailedQuiesceDoesNotShutdownAgain(t *testing.T) {
	a, _, calls := managedView(t, "headless")
	run := a.Run
	a.Run = func(ctx context.Context, args, env []string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "simctl shutdown") {
			return "", errors.New("simulator busy")
		}
		return run(ctx, args, env)
	}
	if a.Main([]string{"view", "set", "stream", "--allow-restart"}) != 5 {
		t.Fatal(a.Err)
	}
	for _, args := range *calls {
		v := strings.Join(args, " ")
		if strings.Contains(v, "shutdown -h") || strings.HasSuffix(v, "macbox/macbox up") {
			t.Fatal("quiesce failure restarted guest", v)
		}
	}
}
