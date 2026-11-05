package ide

import (
	"encoding/json"
	"fmt"
)

// Decode accepts VS Code JSONC, keeping comment markers inside strings intact.
func Decode(b []byte) (map[string]any, error) {
	clean := append([]byte(nil), b...)
	in, escape := false, false
	for i := 0; i < len(clean); i++ {
		c := clean[i]
		if in {
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				in = false
			}
			continue
		}
		if c == '"' {
			in = true
			continue
		}
		if c == '/' && i+1 < len(clean) {
			if clean[i+1] == '/' {
				for i < len(clean) && clean[i] != '\n' {
					clean[i] = ' '
					i++
				}
				i--
				continue
			}
			if clean[i+1] == '*' {
				clean[i], clean[i+1] = ' ', ' '
				i += 2
				for i+1 < len(clean) && !(clean[i] == '*' && clean[i+1] == '/') {
					clean[i] = ' '
					i++
				}
				if i+1 >= len(clean) {
					return nil, fmt.Errorf("unterminated JSONC comment")
				}
				clean[i], clean[i+1] = ' ', ' '
				i++
			}
		}
	}
	in, escape = false, false
	for i, c := range clean {
		if in {
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				in = false
			}
			continue
		}
		if c == '"' {
			in = true
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(clean) && (clean[j] == ' ' || clean[j] == '\n' || clean[j] == '\r' || clean[j] == '\t') {
				j++
			}
			if j < len(clean) && (clean[j] == ']' || clean[j] == '}') {
				clean[i] = ' '
			}
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(clean, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return doc, nil
}
func merge(dst, src map[string]any) {
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if old, ok := dst[k].(map[string]any); ok {
				merge(old, sub)
				continue
			}
		}
		dst[k] = v
	}
}
func upsert(doc map[string]any, key, identity string, values ...map[string]any) error {
	list := []any{}
	if old, exists := doc[key]; exists {
		var ok bool
		list, ok = old.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", key)
		}
	}
	for _, value := range values {
		found := false
		for _, entry := range list {
			if old, ok := entry.(map[string]any); ok && old[identity] == value[identity] {
				merge(old, value)
				found = true
			}
		}
		if !found {
			list = append(list, value)
		}
	}
	doc[key] = list
	return nil
}
