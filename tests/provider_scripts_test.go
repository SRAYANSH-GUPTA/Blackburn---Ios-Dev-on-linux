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
	cmd.Env = append(os.Environ(), "RUN="+dir, "MACBOX_HEADLESS=true", "MACBOX_VM=mac", "MACBOX_CPUSET=", "MACBOX_SSH_HOST=test-mac", "TEST_LOG="+log)
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
