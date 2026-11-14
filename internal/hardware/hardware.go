// Package hardware discovers host capabilities without changing host policy.
package hardware

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type CPU struct {
	ID            int
	Kind          string
	MaxKHz        int
	Package, Core string
}
type GPU struct{ PCI, Vendor, Device, Family, Driver string }
type Report struct {
	CPUVendor                        string
	CPUs                             []CPU
	CPUList                          string
	VCPUs                            int
	TotalMiB, AvailableMiB, GuestMiB int
	GPUs                             []GPU
	Vulkan                           string
	Power                            string
	Governors                        []string
	Warnings                         []string
}

// List parses Linux CPU lists, including noncontiguous logical CPU IDs.
func List(s string) ([]int, error) {
	seen := map[int]bool{}
	for _, part := range strings.Split(strings.TrimSpace(s), ",") {
		lo, hi, rangeOK := strings.Cut(part, "-")
		a, e := strconv.Atoi(lo)
		if e != nil || a < 0 || a > 1048575 {
			return nil, fmt.Errorf("invalid CPU list %q", s)
		}
		b := a
		if rangeOK {
			b, e = strconv.Atoi(hi)
			if e != nil || b < a || b > 1048575 {
				return nil, fmt.Errorf("invalid CPU range %q", part)
			}
		}
		for n := a; n <= b; n++ {
			seen[n] = true
		}
	}
	ids := []int{}
	for n := range seen {
		ids = append(ids, n)
	}
	sort.Ints(ids)
	return ids, nil
}
func Mask(ids []int) string {
	seen := map[int]bool{}
	for _, n := range ids {
		seen[n] = true
	}
	ids = nil
	for n := range seen {
		ids = append(ids, n)
	}
	sort.Ints(ids)
	parts := []string{}
	for i := 0; i < len(ids); i++ {
		j := i
		for j+1 < len(ids) && ids[j+1] == ids[j]+1 {
			j++
		}
		s := strconv.Itoa(ids[i])
		if j > i {
			s += "-" + strconv.Itoa(ids[j])
		}
		parts = append(parts, s)
		i = j
	}
	return strings.Join(parts, ",")
}

// Memory leaves at least 4 GiB (or 25%) for the host and 2 GiB for VM overhead.
// Available memory is checked again at launch; installed RAM sets persistent defaults.
func Memory(total int) int {
	target := 5120
	switch {
	case total >= 60*1024:
		target = 16384
	case total >= 28*1024:
		target = 10240
	case total >= 20*1024:
		target = 8192
	case total >= 14*1024:
		target = 6144
	}
	reserve := max(4096, total/4) + 2048
	return max(0, min(target, (total-reserve)/256*256))
}
func read(root, path string) string {
	b, _ := os.ReadFile(filepath.Join(root, path))
	return strings.TrimSpace(string(b))
}
func number(s string) int { n, _ := strconv.Atoi(s); return n }

// Probe accepts a filesystem root for reproducible tests; production uses "/".
// Intel PMU cpu_core/cpu_atom masks are preferred over frequency heuristics.
func Probe(root string) (Report, error) {
	r := Report{Power: "unknown"}
	online, e := List(read(root, "sys/devices/system/cpu/online"))
	if e != nil {
		return r, e
	}
	allowed := map[int]bool{}
	for _, line := range strings.Split(read(root, "proc/self/status"), "\n") {
		if strings.HasPrefix(line, "Cpus_allowed_list:") {
			ids, err := List(strings.TrimSpace(strings.TrimPrefix(line, "Cpus_allowed_list:")))
			if err != nil {
				return r, err
			}
			for _, n := range ids {
				allowed[n] = true
			}
		}
	}
	pcores, _ := List(read(root, "sys/devices/cpu_core/cpus"))
	atoms, _ := List(read(root, "sys/devices/cpu_atom/cpus"))
	pm := map[int]bool{}
	am := map[int]bool{}
	for _, n := range pcores {
		pm[n] = true
	}
	for _, n := range atoms {
		am[n] = true
	}
	gov := map[string]bool{}
	for _, n := range online {
		if len(allowed) > 0 && !allowed[n] {
			continue
		}
		base := fmt.Sprintf("sys/devices/system/cpu/cpu%d/", n)
		kind := "unknown"
		t := read(root, base+"topology/core_type")
		switch {
		case pm[n] || t == "2":
			kind = "P"
		case am[n] || t == "1":
			kind = "E"
		}
		r.CPUs = append(r.CPUs, CPU{n, kind, number(read(root, base+"cpufreq/cpuinfo_max_freq")), read(root, base+"topology/physical_package_id"), read(root, base+"topology/core_id")})
		if g := read(root, base+"cpufreq/scaling_governor"); g != "" {
			gov[g] = true
		}
	}
	// LP-E is not a distinct architectural core_type. Label only an inferred lower
	// frequency Atom tier, never use it as evidence for P-core selection.
	maxE := 0
	for _, c := range r.CPUs {
		if c.Kind == "E" {
			maxE = max(maxE, c.MaxKHz)
		}
	}
	ids := []int{}
	for i, c := range r.CPUs {
		if c.Kind == "P" {
			ids = append(ids, c.ID)
		}
		if c.Kind == "E" && c.MaxKHz > 0 && c.MaxKHz < maxE {
			r.CPUs[i].Kind = "LP-E (inferred)"
		}
	}
	if len(ids) == 0 {
		r.Warnings = append(r.Warnings, "P-core identity unavailable; affinity left to the scheduler")
		r.VCPUs = min(8, len(r.CPUs))
	} else {
		r.CPUList = Mask(ids)
		r.VCPUs = min(8, len(ids))
	}
	if r.VCPUs < 1 {
		return r, fmt.Errorf("no allowed online CPUs")
	}
	for _, line := range strings.Split(read(root, "proc/cpuinfo"), "\n") {
		if strings.HasPrefix(line, "vendor_id") {
			if _, v, ok := strings.Cut(line, ":"); ok {
				r.CPUVendor = strings.TrimSpace(v)
				break
			}
		}
	}
	for _, line := range strings.Split(read(root, "proc/meminfo"), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			r.TotalMiB = number(f[1]) / 1024
		case "MemAvailable:":
			r.AvailableMiB = number(f[1]) / 1024
		}
	}
	r.GuestMiB = Memory(r.TotalMiB)
	devices, _ := filepath.Glob(filepath.Join(root, "sys/bus/pci/devices/*"))
	intelFamilies := map[string]string{"0x7d55": "Meteor Lake Arc", "0x7d67": "Arrow Lake", "0x7dd1": "Arrow Lake", "0x7d45": "Meteor Lake", "0x9a49": "Tiger Lake Iris Xe", "0x46a6": "Alder Lake Iris Xe"}
	amdFamilies := map[string]string{"0x15bf": "Radeon 780M (RDNA3)", "0x1681": "Radeon 680M (RDNA2)", "0x150e": "Radeon 890M (RDNA3.5)", "0x744c": "Radeon RX 7900 XTX (RDNA3)"}
	for _, d := range devices {
		rel, _ := filepath.Rel(root, d)
		ven := read(root, rel+"/vendor")
		if (ven != "0x8086" && ven != "0x1002") || !strings.HasPrefix(read(root, rel+"/class"), "0x03") {
			continue
		}
		id := read(root, rel+"/device")
		var family string
		if ven == "0x8086" {
			family = intelFamilies[id]
			if family == "" {
				family = "Intel display (unclassified)"
			}
		} else {
			family = amdFamilies[id]
			if family == "" {
				family = "AMD Radeon (unclassified)"
			}
		}
		driver, _ := os.Readlink(filepath.Join(d, "driver"))
		r.GPUs = append(r.GPUs, GPU{PCI: filepath.Base(d), Vendor: ven, Device: id, Family: family, Driver: filepath.Base(driver)})
	}
	supplies, _ := filepath.Glob(filepath.Join(root, "sys/class/power_supply/*"))
	acKnown, ac, battery := false, false, false
	for _, d := range supplies {
		rel, _ := filepath.Rel(root, d)
		typ := read(root, rel+"/type")
		if typ == "Battery" {
			battery = true
			if read(root, rel+"/status") == "Discharging" {
				r.Power = "battery"
			}
		} else if s := read(root, rel+"/online"); s != "" {
			acKnown = true
			ac = ac || s == "1"
		}
	}
	if ac {
		r.Power = "AC"
	} else if acKnown && battery {
		r.Power = "battery"
	}
	for g := range gov {
		r.Governors = append(r.Governors, g)
	}
	sort.Strings(r.Governors)
	if r.Power == "battery" {
		r.Warnings = append(r.Warnings, "running on battery: firmware power limits may throttle QEMU; connect AC for sustained performance (governors: "+strings.Join(r.Governors, ",")+")")
	}
	return r, nil
}
