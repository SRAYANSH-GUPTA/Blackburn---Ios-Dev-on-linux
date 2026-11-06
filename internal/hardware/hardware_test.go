package hardware

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, root, path, value string) {
	t.Helper()
	p := filepath.Join(root, path)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(value), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestMemory(t *testing.T) {
	for _, tt := range []struct{ total, want int }{{16384, 6144}, {32768, 10240}, {65536, 16384}, {15872, 6144}, {8192, 2048}, {4096, 0}, {0, 0}} {
		if n := Memory(tt.total); n != tt.want {
			t.Errorf("%d: %d want %d", tt.total, n, tt.want)
		}
	}
}
func TestLists(t *testing.T) {
	ids, e := List("8,2-4,3,11-12")
	if e != nil || Mask(ids) != "2-4,8,11-12" {
		t.Fatal(ids, e)
	}
	for _, s := range []string{"", "-1", "4-2", "1,", "2-x", "1048576", "1-9999999999"} {
		if _, e = List(s); e == nil {
			t.Fatal(s)
		}
	}
}
func TestMeteorLakeProbe(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "sys/devices/system/cpu/online", "0-21")
	fixture(t, root, "sys/devices/cpu_core/cpus", "0-11")
	fixture(t, root, "sys/devices/cpu_atom/cpus", "12-21")
	fixture(t, root, "proc/self/status", "Cpus_allowed_list:\t0-21\n")
	fixture(t, root, "proc/meminfo", "MemTotal: 33554432 kB\nMemAvailable: 16777216 kB")
	for n := 0; n < 22; n++ {
		freq := "3800000"
		if n >= 20 {
			freq = "2500000"
		}
		fixture(t, root, fmt.Sprintf("sys/devices/system/cpu/cpu%d/cpufreq/cpuinfo_max_freq", n), freq)
		fixture(t, root, fmt.Sprintf("sys/devices/system/cpu/cpu%d/cpufreq/scaling_governor", n), "powersave")
	}
	fixture(t, root, "sys/bus/pci/devices/0000:00:02.0/vendor", "0x8086")
	fixture(t, root, "sys/bus/pci/devices/0000:00:02.0/device", "0x7d55")
	fixture(t, root, "sys/bus/pci/devices/0000:00:02.0/class", "0x030000")
	fixture(t, root, "sys/class/power_supply/BAT0/type", "Battery")
	fixture(t, root, "sys/class/power_supply/BAT0/status", "Discharging")
	fixture(t, root, "sys/class/power_supply/AC/type", "Mains")
	fixture(t, root, "sys/class/power_supply/AC/online", "0")
	r, e := Probe(root)
	if e != nil || r.CPUList != "0-11" || r.VCPUs != 8 || r.GuestMiB != 10240 || r.Power != "battery" || len(r.GPUs) != 1 || !strings.Contains(r.CPUs[20].Kind, "LP-E") || len(r.Warnings) != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	fixture(t, root, "proc/self/status", "Cpus_allowed_list:\t4,8,20")
	fixture(t, root, "sys/class/power_supply/AC/online", "1")
	r, e = Probe(root)
	if e != nil || r.CPUList != "4,8" || r.VCPUs != 2 || r.Power != "AC" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestNoSMTCoreTypeAndFallback(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "sys/devices/system/cpu/online", "0,2,4")
	for _, n := range []int{0, 2} {
		fixture(t, root, fmt.Sprintf("sys/devices/system/cpu/cpu%d/topology/core_type", n), "2")
	}
	r, e := Probe(root)
	if e != nil || r.CPUList != "0,2" {
		t.Fatal(r, e)
	}
	for _, n := range []int{0, 2} {
		fixture(t, root, fmt.Sprintf("sys/devices/system/cpu/cpu%d/topology/core_type", n), "0")
	}
	r, e = Probe(root)
	if e != nil || r.CPUList != "" || len(r.Warnings) != 1 || r.VCPUs != 3 {
		t.Fatal(r, e)
	}
}
