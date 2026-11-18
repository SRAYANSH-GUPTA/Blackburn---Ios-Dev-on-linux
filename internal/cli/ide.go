package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blackburn/internal/config"
	"blackburn/internal/ide"
	"github.com/gorilla/websocket"
)

func flutterProject(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "pubspec.yaml"))
	if err != nil {
		return "", fail("INVALID_ARGUMENT", "target must contain a Flutter pubspec.yaml: "+err.Error(), 2)
	}
	if !strings.Contains(string(b), "flutter:") {
		return "", fail("INVALID_ARGUMENT", "target is not a Flutter project", 2)
	}
	return dir, nil
}
func (a *App) ide(c config.Config, args []string) error {
	if len(args) == 0 {
		return fail("INVALID_ARGUMENT", "usage: ide config|f5|status [--dir=DIR] [--mode=PRESET] [--dry-run] [--json]", 2)
	}
	op := args[0]
	if op != "config" && op != "f5" && op != "status" {
		return fail("INVALID_ARGUMENT", "unknown ide command "+op, 2)
	}
	dir, mode := ".", ""
	dry, asJSON := false, false
	f, err := parse("ide "+op, args[1:], func(f *flag.FlagSet) {
		f.StringVar(&dir, "dir", dir, "Flutter project")
		if op != "status" {
			f.StringVar(&mode, "mode", "", "preset")
			f.BoolVar(&dry, "dry-run", false, "plan without provider calls or writes")
		}
		f.BoolVar(&asJSON, "json", false, "JSON output")
	})
	if err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fail("INVALID_ARGUMENT", "unexpected IDE arguments", 2)
	}
	dir, err = flutterProject(dir)
	if err != nil {
		return err
	}
	if op == "status" {
		return a.ideStatus(dir, asJSON)
	}
	effective, m, err := config.Effective(c, mode, a.Env)
	if err != nil {
		return fail("INVALID_CONFIG", err.Error(), 2)
	}
	p := effective.Profiles[m]
	ep := effective.Endpoints[p.Endpoint]
	if ep.Provider == "direct-ios" {
		return fail("CAPABILITY_UNSUPPORTED", "direct iOS debugging is unavailable", 3)
	}
	arch := ep.Architecture
	// Offline generation cannot safely infer a remote Mac's simulator ABI.
	if arch == "auto" {
		if op == "config" || dry {
			return fail("UNMET_PREREQUISITE", "set endpoints."+p.Endpoint+".architecture to the verified x86_64 or arm64 simulator architecture", 3)
		}
		output, e := a.ssh(ep, "uname -m")
		if e != nil {
			return e
		}
		arch = strings.TrimSpace(output)
	}
	if arch != "x86_64" && arch != "arm64" {
		return fail("INCOMPATIBLE_ARCH", "unsupported simulator architecture "+arch, 3)
	}
	environment := map[string]string{"XCROSS": "true", "XCROSS_SIMULATOR_HOST": ep.SSH, "XCROSS_SIMULATOR_ARCH": arch}
	command := []string{filepath.Join(a.Root, "bin", "xcross"), "flutter", "run", "--target", "lib/main.dart", "--target-platform", "simulator", "--udid", ide.UDIDToken, "--debug"}
	result := map[string]any{"schema_version": 1, "project": dir, "mode": m, "environment": environment, "command": command, "dry_run": dry, "steps": []string{"ensure profile and guest ready", "prewarm Metal", "lease and boot simulator", "xcross owns build, launch, tunnel and reload"}}
	if dry {
		a.json(result)
		return nil
	}
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if op == "config" {
		if err = ide.Generate(dir, ide.Options{Mode: m, Host: ep.SSH, Arch: arch, Root: a.Root}); err != nil {
			return err
		}
		if asJSON {
			a.json(result)
		} else {
			fmt.Fprintf(a.Out, "VS Code configured in %s; select %s and press F5\n", dir, ide.LaunchName)
		}
		return nil
	}
	// mac-sim uses the working directory for checkout ownership. Run f5's
	// subprocesses in the requested project without changing the process cwd.
	originalRun := a.Run
	a.Run = func(ctx context.Context, argv, env []string) (string, error) {
		return originalRun(ctx, append([]string{"env", "-C", dir}, argv...), env)
	}
	defer func() { a.Run = originalRun }()
	// Do not leave a previous launch usable if this preflight fails.
	runtime := filepath.Join(dir, ".dart_tool/blackburn")
	if err = os.Remove(filepath.Join(runtime, "launch.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	s, err := a.session()
	if err != nil {
		return err
	}
	originalOut := a.Out
	a.Out = a.Err
	defer func() { a.Out = originalOut }()
	observed, probeErr := a.ssh(ep, "uname -m")
	if probeErr == nil && strings.TrimSpace(observed) != arch {
		return fail("INCOMPATIBLE_ARCH", "endpoint architecture differs from selected simulator architecture", 3)
	}
	// A cold local VM may not be reachable until up has completed.
	if err = a.up(m, p, ep, s); err != nil {
		return err
	}
	observed, probeErr = a.ssh(ep, "uname -m")
	if probeErr != nil {
		return probeErr
	}
	if strings.TrimSpace(observed) != arch {
		return fail("INCOMPATIBLE_ARCH", "endpoint architecture differs from selected simulator architecture", 3)
	}
	s, err = a.session()
	if err != nil {
		return err
	}
	if err = a.sim(&s, []string{"up"}); err != nil {
		return err
	}
	// Verify the explicit leased device is ready rather than trusting SSH alone.
	if _, err = a.ssh(ep, "xcrun simctl bootstatus "+quote(s.Device)+" -b"); err != nil {
		return err
	}
	command[len(command)-2] = s.Device
	result["device"] = s.Device
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err = config.Atomic(filepath.Join(runtime, "xcross.log"), nil); err != nil {
		return err
	}
	if err = config.Atomic(filepath.Join(runtime, "launch.json"), append(b, '\n')); err != nil {
		return err
	}
	a.Out = originalOut
	if asJSON {
		a.json(result)
	} else {
		for _, key := range []string{"XCROSS", "XCROSS_SIMULATOR_HOST", "XCROSS_SIMULATOR_ARCH"} {
			fmt.Fprintf(a.Out, "export %s=%s\n", key, quote(environment[key]))
		}
		quoted := make([]string, len(command))
		for i, v := range command {
			quoted[i] = quote(v)
		}
		fmt.Fprintf(a.Out, "cd %s\n%s\n", quote(dir), strings.Join(quoted, " "))
	}
	return nil
}

func (a *App) ideStatus(dir string, asJSON bool) error {
	checks := []Check{}
	for _, name := range []string{"launch.json", "tasks.json", "settings.json"} {
		path := filepath.Join(dir, ".vscode", name)
		b, err := os.ReadFile(path)
		if err == nil {
			_, err = ide.Decode(b)
		}
		detail := "valid JSON/JSONC"
		if err != nil {
			detail = err.Error()
		}
		ok := err == nil
		if ok {
			doc, _ := ide.Decode(b)
			switch name {
			case "launch.json":
				ok = hasIDEEntry(doc, "configurations", "name", ide.LaunchName)
			case "tasks.json":
				ok = hasIDEEntry(doc, "tasks", "label", ide.ReadyTask)
			case "settings.json":
				ok = doc["dart.customDartDapPath"] == ".vscode/"+ide.ShimName && doc["dart.customFlutterDapPath"] == ".vscode/"+ide.ShimName
			}
			if !ok {
				detail = "Blackburn configuration missing"
			}
		}
		checks = append(checks, Check{name, ok, detail})
	}
	_, err := os.Stat(filepath.Join(dir, ".vscode", ide.ShimName))
	checks = append(checks, Check{ide.ShimName, err == nil, "generated Dart adapter shim"})
	b, err := os.ReadFile(filepath.Join(dir, ".dart_tool/blackburn/xcross.log"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	endpoints := []map[string]any{}
	active := false
	for _, uri := range ide.DiscoverServices(string(b)) {
		dialer := websocket.Dialer{HandshakeTimeout: time.Second}
		conn, resp, e := dialer.Dial(uri, nil)
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		reachable := e == nil
		if conn != nil {
			conn.Close()
		}
		active = active || reachable
		endpoints = append(endpoints, map[string]any{"uri": uri, "reachable": reachable})
	}
	healthy := true
	for _, check := range checks {
		healthy = healthy && check.OK
	}
	result := map[string]any{"schema_version": 1, "project": dir, "ok": healthy, "checks": checks, "active_debug_tunnel": active, "forwarded_vm_services": endpoints, "tunnel_owner": "xcross", "tunnel_observation": "authenticated WebSocket handshake; process ownership unverified"}
	if asJSON {
		a.json(result)
	} else {
		for _, check := range checks {
			fmt.Fprintf(a.Out, "%s: %t (%s)\n", check.Name, check.OK, check.Detail)
		}
		fmt.Fprintf(a.Out, "Active debug tunnel: %t (xcross-owned; observed WebSocket)\n", active)
		for _, endpoint := range endpoints {
			fmt.Fprintf(a.Out, "VM service: %s reachable=%t\n", endpoint["uri"], endpoint["reachable"])
		}
	}
	return nil
}
func hasIDEEntry(doc map[string]any, key, identity, value string) bool {
	entries, _ := doc[key].([]any)
	for _, entry := range entries {
		if m, ok := entry.(map[string]any); ok && m[identity] == value {
			return true
		}
	}
	return false
}
