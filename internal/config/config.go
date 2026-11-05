// Package config owns the versioned, validated user configuration.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Profile struct {
	Endpoint     string `toml:"endpoint" json:"endpoint"`
	Memory       int    `toml:"guest_memory_mib" json:"guest_memory_mib"`
	CPUs         int    `toml:"vcpus" json:"vcpus"`
	Presentation string `toml:"presentation" json:"presentation"`
	Display      string `toml:"guest_display" json:"guest_display"`
	Window       bool   `toml:"reims_window" json:"reims_window"`
	SimProfile   string `toml:"sim_profile" json:"sim_profile"`
	Experimental bool   `toml:"experimental" json:"experimental"`
}
type Endpoint struct {
	Provider     string `toml:"provider" json:"provider"`
	SSH          string `toml:"ssh_alias" json:"ssh_alias"`
	Architecture string `toml:"architecture" json:"architecture"`
}
type Config struct {
	Version   int                 `toml:"schema_version" json:"schema_version"`
	Mode      string              `toml:"default_mode" json:"default_mode"`
	Profiles  map[string]Profile  `toml:"profiles" json:"profiles"`
	Endpoints map[string]Endpoint `toml:"endpoints" json:"endpoints"`
}

var Modes = []string{"headless", "stream", "studio", "hybrid", "direct"}

func Normalize(s string) (string, error) {
	switch s {
	case "agent":
		s = "headless"
	case "local":
		s = "studio"
	case "remote":
		s = "hybrid"
	}
	for _, m := range Modes {
		if m == s {
			return s, nil
		}
	}
	return "", fmt.Errorf("unknown mode %q (choose %s)", s, strings.Join(Modes, ", "))
}
func Defaults() Config {
	return Config{1, "headless", map[string]Profile{
		"headless": {"local", 5120, 4, "none", "640x480", false, "slim-1x", false},
		"stream":   {"local", 6144, 6, "browser-desktop", "1280x720", true, "slim-1x", false},
		"studio":   {"local", 8192, 8, "native", "1920x1080", true, "slim-1x", false},
		"hybrid":   {"lan_mac", 0, 0, "remote-stream", "", true, "dedicated", false},
		"direct":   {"experimental_ios", 2048, 2, "provider", "", true, "dedicated", true},
	}, map[string]Endpoint{"local": {"macbox", "macbox", "x86_64"}, "lan_mac": {"remote-mac", "blackburn-air", "auto"}, "experimental_ios": {"direct-ios", "", "auto"}}}
}
func Path() string {
	if s := os.Getenv("BLACKBURN_CONFIG"); s != "" {
		return s
	}
	return filepath.Join(Base("XDG_CONFIG_HOME", ".config"), "blackburn/config.toml")
}
func Base(env, suffix string) string {
	if s := os.Getenv(env); s != "" {
		return s
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, suffix)
}
func Load(path string) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return c, err
	}
	if len(b) > 0 {
		// Decode tables individually so partial profile overrides preserve built-in fields.
		var raw map[string]any
		if err = toml.Unmarshal(b, &raw); err != nil {
			return c, err
		}
		for k, v := range raw {
			if k == "profiles" || k == "endpoints" {
				tables, ok := v.(map[string]any)
				if !ok {
					return c, fmt.Errorf("%s must be a table", k)
				}
				for name, fields := range tables {
					fm, ok := fields.(map[string]any)
					if !ok {
						return c, fmt.Errorf("%s.%s must be a table", k, name)
					}
					for key, value := range fm {
						if err = Set(&c, k+"."+name+"."+key, fmt.Sprint(value)); err != nil {
							return c, err
						}
					}
				}
			} else {
				if err = Set(&c, k, fmt.Sprint(v)); err != nil {
					return c, err
				}
			}
		}
	}
	return c, c.Validate()
}
func (c *Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported schema_version %d", c.Version)
	}
	m, e := Normalize(c.Mode)
	if e != nil {
		return e
	}
	c.Mode = m
	for name, p := range c.Profiles {
		if p.Memory < 0 || p.Memory > 1048576 || p.CPUs < 0 || p.CPUs > 256 {
			return fmt.Errorf("profile %s: resources out of range", name)
		}
		switch p.Presentation {
		case "none", "browser-desktop", "native", "remote-stream", "provider":
		default:
			return fmt.Errorf("profile %s: unknown presentation", name)
		}
		if p.Window != (p.Presentation != "none") {
			return fmt.Errorf("profile %s: presentation and reims_window disagree", name)
		}
		if _, e := Normalize(name); e != nil {
			return e
		}
		if name == "agent" || name == "local" || name == "remote" {
			return fmt.Errorf("profile names must be canonical")
		}
		ep, ok := c.Endpoints[p.Endpoint]
		if !ok {
			return fmt.Errorf("profile %s: unknown endpoint %s", name, p.Endpoint)
		}
		if ep.Provider == "macbox" && (p.Memory < 4096 || p.CPUs < 1 || p.CPUs > 256) {
			return fmt.Errorf("profile %s: local memory must be >=4096 MiB and vcpus 1..256", name)
		}
		if ep.Provider == "remote-mac" && (p.Memory != 0 || p.CPUs != 0) {
			return fmt.Errorf("remote profiles cannot allocate local VM resources")
		}
		if !strings.Contains(p.Display, "x") && ep.Provider == "macbox" {
			return fmt.Errorf("invalid guest_display")
		}
	}
	for name, ep := range c.Endpoints {
		switch ep.Provider {
		case "macbox", "remote-mac", "direct-ios":
		default:
			return fmt.Errorf("endpoint %s: unknown provider", name)
		}
		if ep.Provider != "direct-ios" && (ep.SSH == "" || strings.HasPrefix(ep.SSH, "-") || strings.ContainsAny(ep.SSH, " \n\r\t")) {
			return fmt.Errorf("endpoint %s: invalid ssh_alias", name)
		}
		if ep.Architecture != "auto" && ep.Architecture != "arm64" && ep.Architecture != "x86_64" {
			return fmt.Errorf("invalid architecture")
		}
	}
	return nil
}

// Set accepts only known dotted keys and scalar values; no shell evaluation occurs.
func Set(c *Config, key, value string) error {
	parts := strings.Split(key, ".")
	var target any = c
	var p Profile
	var ep Endpoint
	if len(parts) == 3 {
		switch parts[0] {
		case "profiles":
			var ok bool
			p, ok = c.Profiles[parts[1]]
			if !ok {
				return fmt.Errorf("unknown profile %q", parts[1])
			}
			target = &p
		case "endpoints":
			var ok bool
			ep, ok = c.Endpoints[parts[1]]
			if !ok {
				return fmt.Errorf("unknown endpoint %q", parts[1])
			}
			target = &ep
		default:
			return fmt.Errorf("unknown config key %q", key)
		}
	} else if len(parts) != 1 {
		return fmt.Errorf("unknown config key %q", key)
	}
	v := reflect.ValueOf(target).Elem()
	typ := v.Type()
	found := false
	for i := 0; i < v.NumField(); i++ {
		if typ.Field(i).Tag.Get("toml") == parts[len(parts)-1] {
			f := v.Field(i)
			switch f.Kind() {
			case reflect.String:
				f.SetString(value)
			case reflect.Bool:
				b, e := strconv.ParseBool(value)
				if e != nil {
					return e
				}
				f.SetBool(b)
			case reflect.Int:
				n, e := strconv.Atoi(value)
				if e != nil {
					return e
				}
				f.SetInt(int64(n))
			default:
				return fmt.Errorf("key %s is not scalar", key)
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown config key %q", key)
	}
	if len(parts) == 3 {
		if parts[0] == "profiles" {
			c.Profiles[parts[1]] = p
		} else {
			c.Endpoints[parts[1]] = ep
		}
	}
	return nil
}
func Effective(c Config, mode string, env []string) (Config, string, error) {
	// Clone maps before applying ephemeral overrides.
	b, _ := json.Marshal(c)
	c = Config{}
	_ = json.Unmarshal(b, &c)
	for _, entry := range env {
		k, v, _ := strings.Cut(entry, "=")
		if k == "BLACKBURN_MODE" {
			c.Mode = v
		}
		if strings.HasPrefix(k, "BLACKBURN_PROFILES_") || strings.HasPrefix(k, "BLACKBURN_ENDPOINTS_") {
			prefix := "PROFILES"
			if strings.HasPrefix(k, "BLACKBURN_ENDPOINTS_") {
				prefix = "ENDPOINTS"
			}
			rest := strings.TrimPrefix(k, "BLACKBURN_"+prefix+"_")
			name, field, ok := strings.Cut(rest, "_")
			if !ok {
				return c, "", fmt.Errorf("invalid override %s", k)
			}
			if err := Set(&c, strings.ToLower(prefix+"."+name+"."+field), v); err != nil {
				return c, "", err
			}
		}
	}
	if mode != "" {
		c.Mode = mode
	}
	m, e := Normalize(c.Mode)
	if e != nil {
		return c, "", e
	}
	c.Mode = m
	for _, entry := range env {
		k, v, _ := strings.Cut(entry, "=")
		field := map[string]string{"BLACKBURN_GUEST_MEMORY_MIB": "guest_memory_mib", "BLACKBURN_VCPUS": "vcpus", "BLACKBURN_SSH_ALIAS": "ssh_alias"}[k]
		if field != "" {
			key := "profiles." + m + "." + field
			if field == "ssh_alias" {
				key = "endpoints." + c.Profiles[m].Endpoint + "." + field
			}
			if e = Set(&c, key, v); e != nil {
				return c, "", e
			}
		}
	}
	return c, m, c.Validate()
}
func Save(path string, c Config) error {
	if e := c.Validate(); e != nil {
		return e
	}
	b, e := toml.Marshal(c)
	if e != nil {
		return e
	}
	return Atomic(path, b)
}
func Atomic(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".blackburn-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), path)
}
func JSON(c Config) []byte { var b bytes.Buffer; _ = json.NewEncoder(&b).Encode(c); return b.Bytes() }
