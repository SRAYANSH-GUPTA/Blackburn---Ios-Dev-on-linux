package cli

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackburn/internal/config"
)

func ideProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pubspec.yaml"), []byte("name: demo\ndependencies:\n  flutter:\n    sdk: flutter\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestIDEConfigIntegration(t *testing.T) {
	a, out, calls := testApp(t)
	dir := ideProject(t)
	for i := 0; i < 2; i++ {
		if exit := a.Main([]string{"ide", "config", "--dir=" + dir}); exit != 0 {
			t.Fatal(exit, a.Err)
		}
	}
	if len(*calls) != 0 {
		t.Fatal("generation contacted provider", *calls)
	}
	out.Reset()
	if exit := a.Main([]string{"ide", "status", "--dir=" + dir, "--json"}); exit != 0 {
		t.Fatal(exit, a.Err)
	}
	var status map[string]any
	if err := json.Unmarshal(out.Bytes(), &status); err != nil || status["ok"] != true || status["active_debug_tunnel"] != false {
		t.Fatal(out.String(), err)
	}
}
func TestIDEDryRunDoesNotMutate(t *testing.T) {
	for _, op := range []string{"config", "f5"} {
		t.Run(op, func(t *testing.T) {
			a, out, calls := testApp(t)
			dir := ideProject(t)
			if exit := a.Main([]string{"ide", op, "--dir=" + dir, "--mode=agent", "--dry-run"}); exit != 0 {
				t.Fatal(exit, a.Err)
			}
			if len(*calls) > 0 {
				t.Fatal(*calls)
			}
			for _, path := range []string{a.StateDir, filepath.Join(dir, ".vscode"), filepath.Join(dir, ".dart_tool")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("dry run mutation", path)
				}
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil || result["mode"] != "headless" || result["dry_run"] != true {
				t.Fatal(out.String(), err)
			}
		})
	}
}
func TestIDEF5Readiness(t *testing.T) {
	for _, mode := range []string{"headless", "hybrid"} {
		t.Run(mode, func(t *testing.T) {
			a, out, calls := testApp(t)
			dir := ideProject(t)
			c := config.Defaults()
			ep := c.Endpoints[c.Profiles[mode].Endpoint]
			ep.Architecture = "x86_64"
			c.Endpoints[c.Profiles[mode].Endpoint] = ep
			if err := config.Save(a.ConfigPath, c); err != nil {
				t.Fatal(err)
			}
			if mode == "headless" {
				a.save(Session{Mode: mode, Profile: c.Profiles[mode], Endpoint: ep, Stage: "SIM_READY"})
			}
			base := a.Run
			a.Run = func(ctx context.Context, args, env []string) (string, error) {
				if len(args) < 4 || args[0] != "env" || args[1] != "-C" || args[2] != dir {
					t.Fatal("incorrect project cwd", args)
				}
				return base(ctx, args[3:], env)
			}
			if exit := a.Main([]string{"ide", "f5", "--dir=" + dir, "--mode=" + mode, "--json"}); exit != 0 {
				t.Fatal(exit, a.Err)
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil || result["device"] != "SIM-1" {
				t.Fatal(out.String(), err)
			}
			command := result["command"].([]any)
			if command[len(command)-2] != "SIM-1" {
				t.Fatal(command)
			}
			b, err := os.ReadFile(filepath.Join(dir, ".dart_tool/blackburn/launch.json"))
			if err != nil || !strings.Contains(string(b), "SIM-1") {
				t.Fatal(string(b), err)
			}
			found := false
			for _, call := range *calls {
				found = found || strings.Contains(strings.Join(call, " "), "bootstatus 'SIM-1' -b")
				if mode == "hybrid" && call[0] != "ssh" && !strings.Contains(call[0], "mac-sim") {
					t.Fatal("hybrid used local provider", call)
				}
			}
			if !found {
				t.Fatal("missing boot barrier")
			}
		})
	}
}
func TestIDEF5FailureRemovesStaleReadiness(t *testing.T) {
	a, _, _ := testApp(t)
	dir := ideProject(t)
	path := filepath.Join(dir, ".dart_tool/blackburn/launch.json")
	config.Atomic(path, []byte(`{"device":"old"}`))
	a.Run = func(context.Context, []string, []string) (string, error) { return "", errors.New("offline") }
	if exit := a.Main([]string{"ide", "f5", "--dir=" + dir}); exit == 0 {
		t.Fatal("offline success")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("stale launch retained")
	}
}
func TestIDEValidation(t *testing.T) {
	for _, args := range [][]string{{"ide"}, {"ide", "wat"}, {"ide", "config", "--wat"}, {"ide", "config", "extra"}, {"ide", "config", "--mode=wat"}, {"ide", "f5", "--mode=direct"}, {"ide", "config", "--mode=hybrid"}} {
		a, _, calls := testApp(t)
		dir := ideProject(t)
		args = append(args, "--dir="+dir)
		if exit := a.Main(args); exit == 0 {
			t.Fatal(args, "accepted")
		}
		if len(*calls) > 0 {
			t.Fatal("invalid args mutated provider", *calls)
		}
	}
}

func TestIDEStatusVerifiesServiceHandshake(t *testing.T) {
	a, out, _ := testApp(t)
	dir := ideProject(t)
	if exit := a.Main([]string{"ide", "config", "--dir=" + dir}); exit != 0 {
		t.Fatal(exit)
	}
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth=/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			conn.Close()
		}
	}))
	defer server.Close()
	uri := "ws" + strings.TrimPrefix(server.URL, "http") + "/auth=/ws"
	config.Atomic(filepath.Join(dir, ".dart_tool/blackburn/xcross.log"), []byte("vm-service: "+uri+"\n"))
	out.Reset()
	if exit := a.Main([]string{"ide", "status", "--dir=" + dir, "--json"}); exit != 0 {
		t.Fatal(exit)
	}
	var status map[string]any
	json.Unmarshal(out.Bytes(), &status)
	if status["active_debug_tunnel"] != true {
		t.Fatal(out.String())
	}
	server.Close()
	out.Reset()
	a.Main([]string{"ide", "status", "--dir=" + dir, "--json"})
	json.Unmarshal(out.Bytes(), &status)
	if status["active_debug_tunnel"] != false {
		t.Fatal("stale tunnel reported active", out.String())
	}
}

func TestIDEF5ArchitectureMismatchBeforeStart(t *testing.T) {
	a, _, calls := testApp(t)
	dir := ideProject(t)
	a.Run = func(_ context.Context, args, env []string) (string, error) {
		*calls = append(*calls, args)
		return "arm64", nil
	}
	if exit := a.Main([]string{"ide", "f5", "--dir=" + dir}); exit != 3 {
		t.Fatal(exit, a.Err)
	}
	if len(*calls) != 1 || !strings.HasSuffix(strings.Join((*calls)[0], " "), "uname -m") {
		t.Fatal(*calls)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dart_tool/blackburn/launch.json")); !os.IsNotExist(err) {
		t.Fatal("launch published for incompatible endpoint")
	}
}
