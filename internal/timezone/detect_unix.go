//go:build !windows

package timezone

import (
	"os"
	"strings"
)

// detect reads the zone /etc/localtime links to, or failing that, the one
// /etc/timezone names (Debian's record of it).
func detect() string {
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if name := fromZoneinfoPath(link); name != "" {
			return name
		}
	}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}
