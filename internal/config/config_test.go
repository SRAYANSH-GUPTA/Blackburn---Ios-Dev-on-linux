package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestModesAndAliases(t *testing.T) {
	for input, want := range map[string]string{"agent": "headless", "local": "studio", "remote": "hybrid", "stream": "stream", "direct": "direct"} {
		got, e := Normalize(input)
		if e != nil || got != want {
			t.Fatalf("%s: %s %v", input, got, e)
		}
	}
	for _, s := range []string{"", "HEADLESS", "unknown", "../studio"} {
		if _, e := Normalize(s); e == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
func TestPresetAllocations(t *testing.T) {
	c := Defaults()
	for name, want := range map[string][2]int{"headless": {5120, 4}, "stream": {6144, 6}, "studio": {8192, 8}, "hybrid": {0, 0}, "direct": {2048, 2}} {
		p := c.Profiles[name]
		if p.Memory != want[0] || p.CPUs != want[1] {
			t.Fatal(name, p)
		}
	}
}
func TestRoundTripAndPartialOverride(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c := Defaults()
	c.Mode = "studio"
	if e := Save(p, c); e != nil {
		t.Fatal(e)
	}
	got, e := Load(p)
	if e != nil || !reflect.DeepEqual(c, got) {
		t.Fatal(got, e)
	}
	if e = os.WriteFile(p, []byte("[profiles.headless]\nguest_memory_mib = 4096\n"), 0600); e != nil {
		t.Fatal(e)
	}
	got, e = Load(p)
	if e != nil || got.Profiles["headless"].Memory != 4096 || got.Profiles["headless"].CPUs != 4 {
		t.Fatal(got, e)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}
func TestPrecedence(t *testing.T) {
	c := Defaults()
	c.Mode = "stream"
	got, m, e := Effective(c, "studio", []string{"BLACKBURN_MODE=agent", "BLACKBURN_PROFILES_STUDIO_VCPUS=7", "BLACKBURN_GUEST_MEMORY_MIB=9216"})
	if e != nil || m != "studio" || got.Profiles[m].CPUs != 7 || got.Profiles[m].Memory != 9216 {
		t.Fatal(got, m, e)
	}
	if c.Profiles["studio"].CPUs != 8 {
		t.Fatal("mutated saved config")
	}
}
func TestInvalidConfiguration(t *testing.T) {
	for _, body := range []string{"schema_version=2", "[profiles.headless]\nguest_memory_mib=999999999", "[profiles.headless]\npresentation='typo'", "[profiles.headless]\nreims_window=true", "default_mode='wat'", "unknown=true", "[profiles.agent]\nvcpus=4", "[profiles.headless]\nvcpus=0", "[profiles.headless]\nreims_window='maybe'", "[profiles.hybrid]\nguest_memory_mib=5120", "[endpoints.local]\nssh_alias='-bad'", "[endpoints.local]\narchitecture='mips'", "[profiles.headless]\nendpoint='missing'"} {
		t.Run(strings.ReplaceAll(body, "\n", " "), func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "c.toml")
			_ = os.WriteFile(p, []byte(body), 0600)
			if _, e := Load(p); e == nil {
				t.Fatal("accepted", body)
			}
		})
	}
}
func TestInvalidEnvironment(t *testing.T) {
	for _, env := range []string{"BLACKBURN_MODE=bad", "BLACKBURN_VCPUS=zero", "BLACKBURN_PROFILES_HEADLESS_TYPO=1"} {
		if _, _, e := Effective(Defaults(), "", []string{env}); e == nil {
			t.Fatal(env)
		}
	}
}
func TestMissingConfigUsesDefaults(t *testing.T) {
	c, e := Load(filepath.Join(t.TempDir(), "missing"))
	if e != nil || !reflect.DeepEqual(c, Defaults()) {
		t.Fatal(c, e)
	}
}
func TestAtomicInvalidWritePreservesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	c := Defaults()
	_ = Save(path, c)
	before, _ := os.ReadFile(path)
	c.Mode = "bad"
	if e := Save(path, c); e == nil {
		t.Fatal("accepted invalid config")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("changed config")
	}
}
