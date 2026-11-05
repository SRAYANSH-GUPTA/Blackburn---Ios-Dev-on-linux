package ide

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateMergesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".vscode")
	os.Mkdir(base, 0700)
	original := `{// retain me in backup
 "version":"0.2.0", "inputs":[{"id":"user"}], "configurations":[{"name":"User","type":"go"},{"name":"` + LaunchName + `","presentation":{"hidden":true},"env":{"USER_ENV":"keep"}}],}`
	os.WriteFile(filepath.Join(base, "launch.json"), []byte(original), 0600)
	os.WriteFile(filepath.Join(base, "tasks.json"), []byte(`{"tasks":[{"label":"User","command":"echo"}],"inputs":[{"id":"keep"}]}`), 0600)
	os.WriteFile(filepath.Join(base, "settings.json"), []byte(`{"editor.fontSize":17,"dart.hotReloadOnSave":"all"}`), 0600)
	o := Options{"headless", "macbox", "x86_64"}
	if err := Generate(dir, o); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, name := range []string{"launch.json", "tasks.json", "settings.json", ShimName} {
		b, _ := os.ReadFile(filepath.Join(base, name))
		before[name] = string(b)
	}
	if err := Generate(dir, o); err != nil {
		t.Fatal(err)
	}
	for name, want := range before {
		b, _ := os.ReadFile(filepath.Join(base, name))
		if string(b) != want {
			t.Fatalf("%s not idempotent", name)
		}
	}
	b, _ := os.ReadFile(filepath.Join(base, "launch.json.blackburn-backup"))
	if string(b) != original {
		t.Fatal("original JSONC backup lost")
	}
	launch, _ := Decode([]byte(before["launch.json"]))
	entries := launch["configurations"].([]any)
	if len(entries) != 2 || len(launch["inputs"].([]any)) != 1 {
		t.Fatal(launch)
	}
	generated := entries[1].(map[string]any)
	if generated["env"].(map[string]any)["USER_ENV"] != "keep" || generated["presentation"] == nil || generated["preLaunchTask"] != ReadyTask {
		t.Fatal(generated)
	}
	if !strings.Contains(before["launch.json"], UDIDToken) {
		t.Fatal("static device")
	}
	tasks, _ := Decode([]byte(before["tasks.json"]))
	if len(tasks["tasks"].([]any)) != 3 || tasks["inputs"] == nil {
		t.Fatal(tasks)
	}
	settings, _ := Decode([]byte(before["settings.json"]))
	if settings["editor.fontSize"] != float64(17) || settings["dart.hotReloadOnSave"] != "all" {
		t.Fatal(settings)
	}
}
func TestGenerateRefusesUnsafeInputBeforeWrites(t *testing.T) {
	for _, bad := range []string{`[]`, `{"tasks":{}}`, `{"tasks":[`, `null`, `{/* unterminated`} {
		t.Run(bad, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, ".vscode")
			os.Mkdir(base, 0700)
			os.WriteFile(filepath.Join(base, "tasks.json"), []byte(bad), 0600)
			if err := Generate(dir, Options{"headless", "macbox", "x86_64"}); err == nil {
				t.Fatal("accepted malformed config")
			}
			if _, err := os.Stat(filepath.Join(base, "launch.json")); !os.IsNotExist(err) {
				t.Fatal("partial write")
			}
		})
	}
}
func TestAdapterConflictAndSymlink(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".vscode")
	os.Mkdir(base, 0700)
	settings := filepath.Join(base, "settings.json")
	os.WriteFile(settings, []byte(`{"dart.customDartDapPath":"user.dart"}`), 0600)
	if err := Generate(dir, Options{}); err == nil {
		t.Fatal("adapter overwritten")
	}
	os.Remove(settings)
	other := filepath.Join(t.TempDir(), "user.json")
	os.WriteFile(other, []byte(`{}`), 0600)
	os.Symlink(other, settings)
	if err := Generate(dir, Options{}); err == nil {
		t.Fatal("symlink overwritten")
	}
}
func TestJSONCStringsAndEscapes(t *testing.T) {
	doc, err := Decode([]byte(`{"url":"http://host/a//b/*c*/",/* comment */"quote":"\"", "list":[1,],}`))
	if err != nil {
		t.Fatal(err)
	}
	if doc["url"] != "http://host/a//b/*c*/" || doc["quote"] != "\"" {
		t.Fatal(doc)
	}
}
func TestEvents(t *testing.T) {
	events := RestartEvents("guest restart", "SIM-1")
	if len(events) != 2 || events[0].Event != "session.restarting" || events[1].Event != "debug.invalidated" {
		t.Fatal(events)
	}
	b, err := json.Marshal(events)
	if err != nil || !strings.Contains(string(b), `"reattach_required":true`) {
		t.Fatal(string(b), err)
	}
}
func TestDiscoverForwardedServices(t *testing.T) {
	log := `The Dart VM service is listening on http://127.0.0.1:9999/guest=/
vm-service: ws://127.0.0.1:12345/a%2Fb=/ws?key=keep
vm-service: ws://127.0.0.1:12345/a%2Fb=/ws?key=keep
BLACKBURN_READY http://localhost:8181/auth=/
vm-service: ws://0.0.0.0:8888/no/ws
vm-service: ws://127.0.0.1:99999/no/ws
vm-service: ws://127.0.0.1:0/no/ws
vm-service: ws://127.0.0.1:8181/
`
	got := DiscoverServices(log)
	want := []string{"ws://127.0.0.1:12345/a%2Fb=/ws?key=keep", "ws://localhost:8181/auth=/ws"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatal(got)
		}
	}
}

func TestRegenerateWithAnotherMode(t *testing.T) {
	dir := t.TempDir()
	if err := Generate(dir, Options{"headless", "macbox", "x86_64"}); err != nil {
		t.Fatal(err)
	}
	if err := Generate(dir, Options{"hybrid", "my-air", "arm64"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".vscode/launch.json"))
	doc, _ := Decode(b)
	entries := doc["configurations"].([]any)
	if len(entries) != 1 {
		t.Fatal(entries)
	}
	env := entries[0].(map[string]any)["env"].(map[string]any)
	if env["XCROSS_SIMULATOR_HOST"] != "my-air" || env["XCROSS_SIMULATOR_ARCH"] != "arm64" {
		t.Fatal(env)
	}
	b, _ = os.ReadFile(filepath.Join(dir, ".vscode/tasks.json"))
	if !strings.Contains(string(b), "--mode=hybrid") {
		t.Fatal(string(b))
	}
}
