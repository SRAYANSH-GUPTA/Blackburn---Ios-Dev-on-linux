package cli

import (
	"blackburn/internal/config"
	"context"
	"debug/macho"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (a *App) macsim(s *Session, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.Timeout)
	defer cancel()
	return a.macsimContext(ctx, s, args...)
}
func (a *App) macsimContext(ctx context.Context, s *Session, args ...string) (string, error) {
	state := a.env("MAC_SIM_STATE_DIR", filepath.Join(config.Base("XDG_STATE_HOME", ".local/state"), "macbox/mac-sim"))
	if s.Endpoint.Provider == "remote-mac" {
		state = filepath.Join(a.StateDir, "sim", s.Profile.Endpoint)
	}
	env := append(append([]string{}, a.Env...), "MAC_SIM_HOST="+s.Endpoint.SSH, "MACBOX_SSH_HOST="+s.Endpoint.SSH, "MAC_SIM_STATE_DIR="+state)
	if s.Endpoint.Provider == "remote-mac" || s.Profile.Presentation != "none" {
		env = append(env, "MAC_SIM_KEEP_SIMULATOR_APP=1")
	}
	if s.Device != "" {
		env = append(env, "MAC_SIM_DEVICE="+s.Device)
	}
	return a.Run(ctx, append([]string{filepath.Join(a.Root, "macbox/sims/bin/mac-sim")}, args...), env)
}
func (a *App) sim(s *Session, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "stream", "touch", "tap", "key", "text":
			return a.simInteractive(s, args)
		}
	}
	if len(args) == 0 {
		return fail("INVALID_ARGUMENT", "usage: sim up|shot PATH|install APP|run BUNDLE|status|down", 2)
	}
	op := args[0]
	args = args[1:]
	if op == "shot" && len(args) == 1 && strings.HasPrefix(args[0], "--output=") {
		args[0] = strings.TrimPrefix(args[0], "--output=")
	}
	n := 0
	switch op {
	case "up", "status", "down":
	case "shot", "install", "run":
		n = 1
	default:
		return fail("INVALID_ARGUMENT", "unknown simulator command "+op, 2)
	}
	if len(args) != n || (n == 1 && (args[0] == "" || strings.HasPrefix(args[0], "-"))) {
		return fail("INVALID_ARGUMENT", "invalid arguments for sim "+op, 2)
	}
	if s.Endpoint.Provider == "direct-ios" {
		return fail("CAPABILITY_UNSUPPORTED", "direct iOS provider unavailable", 3)
	}
	if op == "install" {
		arch := s.Endpoint.Architecture
		if arch == "auto" {
			out, e := a.ssh(s.Endpoint, "uname -m")
			if e != nil {
				return e
			}
			arch = strings.TrimSpace(out)
		}
		if e := ValidateApp(args[0], arch); e != nil {
			return fail("INCOMPATIBLE_ARCH", e.Error(), 2)
		}
	}
	if op != "status" && op != "down" {
		if e := a.prewarm(s.Endpoint); e != nil {
			return e
		}
	}
	var out string
	var e error
	switch op {
	case "up":
		out, e = a.macsim(s, "up")
		if e == nil {
			device, err := a.macsim(s, "udid")
			if err != nil {
				return err
			}
			s.Device = strings.TrimSpace(device)
			if s.Device == "" {
				return errors.New("mac-sim returned an empty UDID")
			}
			if s.Endpoint.Provider == "macbox" && s.Profile.Presentation != "none" {
				if _, err = a.ssh(s.Endpoint, "open -a Simulator --args -CurrentDeviceUDID "+quote(s.Device)); err != nil {
					return err
				}
			}
			s.Stage = "SIM_READY"
			if e = a.save(*s); e != nil {
				return e
			}
			fmt.Fprintln(a.Out, s.Device)
		}
	case "run":
		device, err := a.macsim(s, "udid")
		if err != nil {
			return err
		}
		device = strings.TrimSpace(device)
		if device == "" {
			return errors.New("no leased simulator")
		}
		out, e = a.ssh(s.Endpoint, "xcrun simctl launch "+quote(device)+" "+quote(args[0]))
	case "status":
		out, e = a.macsim(s, "status")
	case "down":
		out, e = a.macsim(s, "down")
		if e == nil {
			s.Device = ""
			s.Stage = "PREWARMED"
			e = a.save(*s)
		}
	default:
		out, e = a.macsim(s, append([]string{op}, args...)...)
	}
	if op != "up" {
		fmt.Fprint(a.Out, out)
	} else {
		fmt.Fprint(a.Err, out)
	}
	return e
}

// ValidateApp verifies every Mach-O payload's CPU and simulator platform before staging.
func ValidateApp(path, arch string) error {
	info, e := os.Stat(path)
	if e != nil {
		return e
	}
	if !info.IsDir() || !strings.HasSuffix(path, ".app") {
		return errors.New("expected a local .app bundle")
	}
	count := 0
	e = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if len(b) < 4 {
			return nil
		}
		magic := binary.BigEndian.Uint32(b[:4])
		if magic != 0xcffaedfe && magic != 0xfeedfacf && magic != 0xcafebabe && magic != 0xbebafeca {
			return nil
		}
		count++
		f, err := macho.Open(p)
		if err == nil {
			defer f.Close()
			return checkMachO(f, arch, p)
		}
		fat, err := macho.OpenFat(p)
		if err != nil {
			return err
		}
		defer fat.Close()
		for _, slice := range fat.Arches {
			if cpuMatches(slice.Cpu, arch) {
				return checkMachO(slice.File, arch, p)
			}
		}
		return fmt.Errorf("%s lacks %s slice", p, arch)
	})
	if e != nil {
		return e
	}
	if count == 0 {
		return errors.New("bundle contains no Mach-O payloads")
	}
	return nil
}
func cpuMatches(cpu macho.Cpu, arch string) bool {
	return arch == "x86_64" && cpu == macho.CpuAmd64 || arch == "arm64" && cpu == macho.CpuArm64
}
func checkMachO(f *macho.File, arch, path string) error {
	if !cpuMatches(f.Cpu, arch) {
		return fmt.Errorf("%s: CPU %s incompatible with %s", path, f.Cpu, arch)
	}
	for _, l := range f.Loads {
		b := l.Raw()
		if len(b) >= 12 && f.ByteOrder.Uint32(b[:4]) == 0x32 {
			if f.ByteOrder.Uint32(b[8:12]) != 7 {
				return fmt.Errorf("%s is not an iOS Simulator artifact", path)
			}
			return nil
		}
		if len(b) >= 16 && f.ByteOrder.Uint32(b[:4]) == 0x25 && arch == "x86_64" {
			return nil
		}
	}
	return fmt.Errorf("%s has no supported simulator platform command", path)
}
