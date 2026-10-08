// Package timezone picks the timezone a sandbox keeps its local time in —
// by default the host's, named the way the sandbox's /usr/share/zoneinfo
// names it ("Africa/Johannesburg") — and puts the sandbox on it.
package timezone

import (
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"vibe/internal/sbxrun"
)

// Default is what a sandbox is on when nothing better can be told.
const Default = "UTC"

// Host returns this machine's timezone, or Default when it can't tell. A
// variable so tests can stand in for the machine they run on.
var Host = func() string {
	if name := fromTZ(os.Getenv("TZ")); name != "" {
		return name
	}
	if name := detect(); Valid(name) {
		return name
	}
	return Default
}

// fromTZ reads the zone a TZ variable names: a zone name, optionally with
// glibc's leading ':', or a path to a zone file. A POSIX rule such as
// "SAST-2" names no zone, so gives "".
func fromTZ(tz string) string {
	tz = strings.TrimPrefix(strings.TrimSpace(tz), ":")
	if tz == "" {
		return ""
	}
	if strings.Contains(tz, "zoneinfo/") {
		return fromZoneinfoPath(tz)
	}
	if Valid(tz) && (strings.Contains(tz, "/") || tz == "UTC") {
		return tz
	}
	return ""
}

// fromZoneinfoPath reads the zone out of a zone file's path, as
// /etc/localtime links to on Linux ("/usr/share/zoneinfo/Africa/Johannesburg",
// or relatively, "../usr/share/zoneinfo/...") and macOS
// ("/var/db/timezone/zoneinfo/Africa/Johannesburg").
func fromZoneinfoPath(p string) string {
	i := strings.LastIndex(p, "zoneinfo/")
	if i < 0 {
		return ""
	}
	name := p[i+len("zoneinfo/"):]
	for _, variant := range []string{"posix/", "right/"} {
		name = strings.TrimPrefix(name, variant)
	}
	if !Valid(name) {
		return ""
	}
	return name
}

// zoneNameRE is what a zone name can be: dot-free path segments, so a name
// can't climb out of /usr/share/zoneinfo when it's joined onto it.
var zoneNameRE = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+)*$`)

// Valid reports whether name has the shape of a zone name.
func Valid(name string) bool {
	return zoneNameRE.MatchString(name) && !strings.HasPrefix(name, "-")
}

// Zones returns the timezones to choose from, sorted, with current among
// them even when tzdata's list doesn't have it (a backward-compatible name
// like "Asia/Calcutta", say).
func Zones(current string) []string {
	out := append([]string(nil), zones...)
	if current != "" && indexOf(out, current) < 0 {
		out = append(out, current)
		sort.Strings(out)
	}
	return out
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// zoneinfo is where the sandbox keeps its zone files.
const zoneinfo = "/usr/share/zoneinfo"

// Apply puts sandbox name's local time on zone: /etc/localtime links to its
// zone file, and /etc/timezone names it, as Debian has them. Nothing is
// changed when the sandbox is on it already. Each exec is a plain argv —
// sbx exec may join its arguments into one command line, which a
// `sh -c` script doesn't survive.
func Apply(name, zone string) error {
	if !Valid(zone) {
		return fmt.Errorf("%q is not a timezone", zone)
	}
	file := path.Join(zoneinfo, zone)
	if !sbxrun.ExecSilent(name, "test", "-f", file) {
		return fmt.Errorf("the sandbox has no timezone %s (no %s)", zone, file)
	}
	link, _ := sbxrun.ExecCapture(name, "readlink", "/etc/localtime")
	current, _ := sbxrun.ExecCapture(name, "cat", "/etc/timezone")
	if strings.TrimSpace(link) == file && strings.TrimSpace(current) == zone {
		return nil
	}
	if out, err := sbxrun.ExecCapture(name, "sudo", "-n", "ln", "-sfn", file, "/etc/localtime"); err != nil {
		return fmt.Errorf("linking /etc/localtime to %s: %w: %s", file, err, strings.TrimSpace(out))
	}
	if err := sbxrun.ExecInput(name, zone+"\n", "sudo", "-n", "tee", "/etc/timezone"); err != nil {
		return errors.New("writing /etc/timezone: " + err.Error())
	}
	return nil
}
