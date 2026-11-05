package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackburn/internal/config"
)

// LegacyEnv reads literal user-owned provider settings, without executing shell code.
// Blackburn writes no legacy files. Explicit process environment takes precedence.
func LegacyEnv(env []string) ([]string, error) {
	known := map[string]bool{}
	values := map[string]string{}
	for _, s := range env {
		k, v, _ := strings.Cut(s, "=")
		known[k] = true
		values[k] = v
	}
	path := values["MACBOX_CONFIG"]
	if path == "" {
		base := values["XDG_CONFIG_HOME"]
		if base == "" {
			base = config.Base("XDG_CONFIG_HOME", ".config")
		}
		path = filepath.Join(base, "macbox/blackburn.env")
		if _, e := os.Stat(path); os.IsNotExist(e) {
			path = filepath.Join(base, "macbox/macbox.env")
		}
	}
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return env, nil
	}
	if e != nil {
		return nil, e
	}
	defer f.Close()
	result := append([]string{}, env...)
	if !known["MACBOX_CONFIG"] {
		result = append(result, "MACBOX_CONFIG="+path)
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !strings.HasPrefix(key, "MACBOX_") || known[key] {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '\'' && value[len(value)-1] == '\'' || value[0] == '"' && value[len(value)-1] == '"') {
			value = value[1 : len(value)-1]
		}
		if strings.ContainsAny(value, "$`\\;\n\r") {
			return nil, fmt.Errorf("legacy setting %s must be literal; set it explicitly in the environment", key)
		}
		known[key] = true
		result = append(result, key+"="+value)
	}
	return result, scanner.Err()
}
