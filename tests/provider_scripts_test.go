package blackburn_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureTool(t *testing.T, dir, name, body string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/bash\n"+body+"\n"), 0755); e != nil {
		t.Fatal(e)
	}
}
func TestRemoteBootFrontendPolicy(t *testing.T) {
	root, e := filepath.Abs("..")
	if e != nil {
		t.Fatal(e)
	}
	for _, keep := range []string{"0", "1"} {
		t.Run("keep="+keep, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "calls")
			fixtureTool(t, dir, "xcrun", `if [[ "$*" == "simctl list devices" ]]; then echo 'SIM-1 (Booted)'; fi`)
			fixtureTool(t, dir, "pgrep", `echo pgrep >> "$TEST_LOG"; exit 0`)
			fixtureTool(t, dir, "osascript", `echo osascript >> "$TEST_LOG"`)
			cmd := exec.Command("bash", filepath.Join(root, "macbox/sims/bin/mac-sim-remote"), "boot", "SIM-1")
			cmd.Env = append(os.Environ(), "HOME="+dir, "PATH="+dir+":"+os.Getenv("PATH"), "TEST_LOG="+log, "MAC_SIM_KEEP_SIMULATOR_APP="+keep)
			if b, e := cmd.CombinedOutput(); e != nil {
				t.Fatal(e, string(b))
			}
			b, _ := os.ReadFile(log)
			if keep == "1" && len(b) != 0 {
				t.Fatal("shared frontend touched", string(b))
			}
			if keep == "0" && !strings.Contains(string(b), "osascript") {
				t.Fatal("legacy policy changed")
			}
		})
	}
}
func TestHeadlessMacboxSkipsDisplayStack(t *testing.T) {
	root, _ := filepath.Abs("..")
	b, e := os.ReadFile(filepath.Join(root, "macbox/macbox"))
	if e != nil {
		t.Fatal(e)
	}
	source := string(b)
	start := strings.Index(source, "cmd_up() {")
	end := strings.Index(source[start:], "\ncmd_down() {")
	if start < 0 || end < 0 {
		t.Fatal("missing provider function")
	}
	function := source[start : start+end]
	dir := t.TempDir()
	script := `
set -eu
need_init() { :; }
vm_exists() { return 0; }
container_start() { :; }
in_c() { echo "in_c $*" >> "$TEST_LOG"; }
vm_running() { return 1; }
ctl() { echo "ctl $*" >> "$TEST_LOG"; }
apply_tuning() { :; }
qemu_pid() { :; }
say() { :; }
` + function + "\ncmd_up\n"
	cmd := exec.Command("bash", "-c", script)
	log := filepath.Join(dir, "calls")
	cmd.Env = append(os.Environ(), "RUN="+dir, "DATA="+dir, "MACBOX_HEADLESS=true", "MACBOX_VM=mac", "MACBOX_CPUSET=", "MACBOX_SSH_HOST=test-mac", "TEST_LOG="+log)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	b, _ = os.ReadFile(log)
	if strings.Contains(string(b), "display-up.sh") || strings.Contains(string(b), "test -S") {
		t.Fatal("headless allocated presentation", string(b))
	}
	if !strings.Contains(string(b), "ctl start mac") {
		t.Fatal("VM not launched", string(b))
	}
}
func TestPrewarmScriptUsesSelectedEndpoint(t *testing.T) {
	root, _ := filepath.Abs("..")
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	fixtureTool(t, dir, "ssh", `printf '%s\n' "$@" > "$TEST_LOG"; cat >> "$TEST_LOG"`)
	cmd := exec.Command("bash", filepath.Join(root, "scripts/blackburn-prewarm-metal.sh"))
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "BLACKBURN_SSH_ALIAS=chosen-mac", "TEST_LOG="+log)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	b, _ := os.ReadFile(log)
	for _, want := range []string{"chosen-mac", "BatchMode=yes", "shasum -a 256", "codesign --verify", "xcrun simctl list devices"} {
		if !strings.Contains(string(b), want) {
			t.Fatal("missing", want, string(b))
		}
	}
}

// Run the actual provider functions with instrumented execution boundaries. Any
// accidental coupling of view operations to VM lifecycle fails these tests.
func TestViewerProviderKeepsExecutionAlive(t *testing.T) {
	root, _ := filepath.Abs("..")
	b, e := os.ReadFile(filepath.Join(root, "macbox/macbox"))
	if e != nil {
		t.Fatal(e)
	}
	source := string(b)
	start := strings.Index(source, "viewer_capable() {")
	end := strings.Index(source[start:], "resolution() {")
	functions := source[start : start+end]
	for _, tc := range []struct {
		backend, target, want string
		ok                    bool
	}{
		{"local", "detach", "none", true}, {"local", "local", "native", true},
		{"stream", "stream", "browser-desktop", true}, {"stream", "none", "none", true},
		{"none", "stream", "none", false}, {"stream", "local", "none", false},
	} {
		t.Run(tc.backend+"/"+tc.target, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "calls")
			if e := os.MkdirAll(filepath.Join(dir, "qemu/current"), 0700); e != nil {
				t.Fatal(e)
			}
			for _, name := range []string{"qemu/current/blackburn-viewer-capability", "viewer-capability"} {
				if e := os.WriteFile(filepath.Join(dir, name), []byte("detach-v1\n"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			script := `set -eu
say() { echo "$*"; }
die() { echo "$*" >&2; exit 1; }
vm_running() { return 0; }
view() { echo "$BACKEND"; }
in_c() { echo "in_c $*" >> "$TEST_LOG"; }
cmd_down() { echo forbidden >> "$TEST_LOG"; exit 19; }
ctl() { echo forbidden >> "$TEST_LOG"; exit 19; }
guest() { echo forbidden >> "$TEST_LOG"; exit 19; }
` + functions + "\ncmd_view \"$TARGET\"\n"
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "RUN="+dir, "DATA="+dir, "REPO="+root, "BACKEND="+tc.backend, "TARGET="+tc.target, "TEST_LOG="+log)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok {
				t.Fatal(err, string(out))
			}
			calls, _ := os.ReadFile(log)
			if strings.Contains(string(calls), "forbidden") {
				t.Fatal(string(calls))
			}
			if tc.ok {
				state, _ := os.ReadFile(filepath.Join(dir, "view-state"))
				if strings.TrimSpace(string(state)) != tc.want {
					t.Fatal(string(state))
				}
			}
		})
	}
}

func TestAppleGraphicsCloseGuard(t *testing.T) {
	if _, e := exec.LookPath("cc"); e != nil {
		t.Skip("C compiler unavailable")
	}
	root, _ := filepath.Abs("..")
	patch, e := os.ReadFile(filepath.Join(root, "macbox/patches/qemu/0001-guard-native-close-poweroff.patch"))
	if e != nil {
		t.Fatal(e)
	}
	// Reconstruct the patched function from context/addition lines and execute it
	// with a shutdown spy. This tests both detach and legacy callback behavior.
	lines := []string{}
	for _, line := range strings.Split(string(patch), "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			lines = append(lines, line[1:])
		}
	}
	s := strings.Join(lines, "\n")
	start := strings.Index(s, "static void apple_gfx_input_close(void)")
	end := strings.Index(s[start:], "\n}") + 2
	if start < 0 || end < 2 {
		t.Fatal("guard function absent")
	}
	code := `#include <stdlib.h>
#include <string.h>
static int shutdown_action, calls;
#define SHUTDOWN_ACTION_POWEROFF 1
#define SHUTDOWN_CAUSE_HOST_UI 1
static void qemu_system_shutdown_request(int cause) { calls++; }
` + s[start:start+end] + `
int main(void) {
 setenv("REIMS_VGPU_CLOSE_POLICY", "detach", 1);
 apple_gfx_input_close(); if (calls != 0) return 1;
 unsetenv("REIMS_VGPU_CLOSE_POLICY");
 apple_gfx_input_close(); return calls != 1;
}
`
	dir := t.TempDir()
	file := filepath.Join(dir, "guard.c")
	bin := filepath.Join(dir, "guard")
	if e := os.WriteFile(file, []byte(code), 0600); e != nil {
		t.Fatal(e)
	}
	if out, e := exec.Command("cc", file, "-o", bin).CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	if out, e := exec.Command(bin).CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
}

func TestArrowLakeFactsExcludeMeteorLake(t *testing.T) {
	root, _ := filepath.Abs("..")
	b, e := os.ReadFile(filepath.Join(root, "macbox/macbox"))
	if e != nil {
		t.Fatal(e)
	}
	source := string(b)
	start := strings.Index(source, "gpu_is_arl() {")
	end := strings.Index(source[start:], "\ntuning_on() {")
	if start < 0 || end < 0 {
		t.Fatal("missing GPU gate")
	}
	function := source[start : start+end]
	for _, tc := range []struct {
		id  string
		arl bool
	}{{"0x7d55", false}, {"0x7d45", false}, {"0x7d67", true}, {"0x7dd1", true}, {"0x9a49", false}, {"0x46a6", false}} {
		cmd := exec.Command("bash", "-c", `cat() { echo "$TEST_PCI"; }; `+function+"\ngpu_is_arl\n")
		cmd.Env = append(os.Environ(), "TEST_PCI="+tc.id, "MACBOX_RENDER_NODE=/dev/dri/renderD128")
		out, e := cmd.CombinedOutput()
		if (e == nil) != tc.arl {
			t.Fatal(tc, e, string(out))
		}
	}
}
