package timezone

import (
	"sort"
	"testing"
)

func TestFromZoneinfoPath(t *testing.T) {
	cases := map[string]string{
		"/usr/share/zoneinfo/Africa/Johannesburg":           "Africa/Johannesburg",
		"../usr/share/zoneinfo/Europe/London":               "Europe/London",
		"/var/db/timezone/zoneinfo/America/New_York":        "America/New_York",
		"/usr/share/zoneinfo/posix/America/Argentina/Salta": "America/Argentina/Salta",
		"/usr/share/zoneinfo/Etc/UTC":                       "Etc/UTC",
		"/usr/share/zoneinfo/../../etc/passwd":              "",
		"/etc/somewhere/else":                               "",
	}
	for in, want := range cases {
		if got := fromZoneinfoPath(in); got != want {
			t.Errorf("fromZoneinfoPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromTZ(t *testing.T) {
	cases := map[string]string{
		"":                                "",
		"Africa/Johannesburg":             "Africa/Johannesburg",
		":Europe/Berlin":                  "Europe/Berlin",
		":/usr/share/zoneinfo/Asia/Tokyo": "Asia/Tokyo",
		"UTC":                             "UTC",
		"SAST-2":                          "", // a POSIX rule, not a zone
		"EST5EDT,M3.2.0,M11.1.0":          "",
	}
	for in, want := range cases {
		if got := fromTZ(in); got != want {
			t.Errorf("fromTZ(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidRejectsNamesThatLeaveZoneinfo(t *testing.T) {
	for _, bad := range []string{"", "../etc/passwd", "/etc/passwd", "Europe/../../x", "-rf", "a b"} {
		if Valid(bad) {
			t.Errorf("Valid(%q) = true", bad)
		}
	}
	for _, good := range []string{"UTC", "Etc/GMT+2", "America/Argentina/Buenos_Aires", "America/Port-au-Prince"} {
		if !Valid(good) {
			t.Errorf("Valid(%q) = false", good)
		}
	}
}

func TestHostPrefersTZ(t *testing.T) {
	t.Setenv("TZ", "Pacific/Auckland")
	if got := Host(); got != "Pacific/Auckland" {
		t.Errorf("Host() with TZ set = %q", got)
	}
}

func TestZonesIncludeTheCurrentOne(t *testing.T) {
	list := Zones("Asia/Calcutta")
	if indexOf(list, "Asia/Calcutta") < 0 || indexOf(list, "Africa/Johannesburg") < 0 || indexOf(list, "UTC") < 0 {
		t.Errorf("Zones should have the current zone along with tzdata's")
	}
	if !sort.StringsAreSorted(list) {
		t.Errorf("Zones should be sorted")
	}
	if len(Zones("Africa/Johannesburg")) != len(zones) {
		t.Errorf("a zone already listed shouldn't be added again")
	}
}

func TestFromWindows(t *testing.T) {
	cases := map[string]string{
		"South Africa Standard Time": "Africa/Johannesburg",
		"GMT Standard Time":          "Europe/London",
		"Eastern Standard Time\x00":  "America/New_York",
		"Not A Real Zone":            "",
	}
	for in, want := range cases {
		if got := fromWindows(in); got != want {
			t.Errorf("fromWindows(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWithTZ(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "export TZ=Africa/Johannesburg # vibe: timezone\n"},
		{"export A=1\n\nexport B=2\n", "export A=1\n\nexport B=2\nexport TZ=Africa/Johannesburg # vibe: timezone\n"},
		// The old line goes, wherever it was, and the new one is at the end.
		{"export TZ=UTC # vibe: timezone\nexport A=1\n", "export A=1\nexport TZ=Africa/Johannesburg # vibe: timezone\n"},
	}
	for _, c := range cases {
		if got := withTZ(c.in, "Africa/Johannesburg"); got != c.want {
			t.Errorf("withTZ(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	once := withTZ("export A=1\n", "Europe/London")
	if withTZ(once, "Europe/London") != once {
		t.Errorf("withTZ should leave a file already on the zone as it is")
	}
}
