package ide

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var serviceURL = regexp.MustCompile(`(?:ws|http)://(?:127\.0\.0\.1|localhost|\[::1\]):[0-9]+/[^\s"<>]*`)

// DiscoverServices accepts forwarded readiness markers only. Engine console HTTP
// announcements describe guest ports and are deliberately not host readiness.
// Authentication paths and queries are preserved; duplicates are reported once.
func DiscoverServices(log string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(log, "\n") {
		marker := strings.Index(line, "vm-service: ")
		if marker < 0 {
			marker = strings.Index(line, "BLACKBURN_READY ")
		}
		if marker < 0 {
			continue
		}
		for _, raw := range serviceURL.FindAllString(line[marker:], -1) {
			u, err := url.Parse(raw)
			if err != nil {
				continue
			}
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				continue
			}
			if u.Path == "/" || u.Path == "" {
				continue
			}
			u.Scheme = "ws"
			if !strings.HasSuffix(u.Path, "/ws") {
				u.Path = strings.TrimRight(u.Path, "/") + "/ws"
			}
			uri := u.String()
			if !seen[uri] {
				seen[uri] = true
				result = append(result, uri)
			}
		}
	}
	return result
}
