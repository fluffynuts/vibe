package main

import (
	"errors"
	"strings"
	"testing"
)

// Read from inside a 12g sandbox.
const sampleMeminfo = `MemTotal:       12328776 kB
MemFree:        10300996 kB
MemAvailable:   11649836 kB
Buffers:          123456 kB
`

func TestParseMeminfo(t *testing.T) {
	total, available, err := parseMeminfo(sampleMeminfo)
	if err != nil || total != 12328776*1024 || available != 11649836*1024 {
		t.Errorf("parseMeminfo = %d, %d, %v", total, available, err)
	}
	if _, _, err := parseMeminfo("MemTotal: 1 kB\n"); err == nil {
		t.Error("parseMeminfo accepted output with no MemAvailable")
	}
}

func TestParseDf(t *testing.T) {
	for _, tt := range []struct {
		out        string
		size, used uint64
		mount      string
	}{
		{"Filesystem     1024-blocks    Used Available Capacity Mounted on\n" +
			"overlay           20466256 1555916  17845380       9% /\n",
			20466256 * 1024, 1555916 * 1024, "/"},
		{"Filesystem     1024-blocks    Used Available Capacity Mounted on\n" +
			"/dev/vdd          51290592     340  48652428       1% /var/lib/docker\n",
			51290592 * 1024, 340 * 1024, "/var/lib/docker"},
	} {
		size, used, mount, err := parseDf(tt.out)
		if err != nil || size != tt.size || used != tt.used || mount != tt.mount {
			t.Errorf("parseDf(%q) = %d, %d, %q, %v", tt.out, size, used, mount, err)
		}
	}
	for _, bad := range []string{"", "Filesystem 1024-blocks Used Available Capacity Mounted on\n", "df: /nope: No such file or directory\n"} {
		if _, _, _, err := parseDf(bad); err == nil {
			t.Errorf("parseDf(%q) accepted it", bad)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[uint64]string{
		0:               "0 B",
		1023:            "1023 B",
		1024:            "1.0 KiB",
		512 << 20:       "512.0 MiB",
		12328776 * 1024: "11.8 GiB",
		3 << 40:         "3.0 TiB",
	} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestPrintInfo(t *testing.T) {
	base := sandboxInfo{name: "proj", target: "/code/proj", profile: "yumbi", agent: "claude", memory: "12g", features: []string{"go", "node"}}
	print := func(info sandboxInfo, profileKnown bool) string {
		var out strings.Builder
		printInfo(&out, info, profileKnown)
		return out.String()
	}

	running := base
	running.exists, running.running = true, true
	running.usage = &sandboxUsage{
		memTotal: 12 << 30, memAvailable: 9 << 30,
		diskSize: 20 << 30, diskUsed: 5 << 30,
		dockerSize: 50 << 30, dockerUsed: 10 << 30,
	}
	got := print(running, true)
	for _, want := range []string{
		"sandbox   proj\n", "profile   yumbi\n", "agent     claude\n", "memory    12g\n",
		"features  go, node\n", "status    running\n",
		"mem used  3.0 GiB of 12.0 GiB (25%)\n",
		"disk used 5.0 GiB of 20.0 GiB (25%)\n",
		"docker    10.0 GiB of 50.0 GiB (20%), on a disk of its own\n",
		"snapshot",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("running sandbox's info lacks %q:\n%s", want, got)
		}
	}

	running.usage.dockerSize = 0
	if got := print(running, true); strings.Contains(got, "docker") {
		t.Errorf("a sandbox with no Docker disk of its own shows one:\n%s", got)
	}

	plain := base
	plain.memory, plain.features = "", nil
	plain.exists = true
	got = print(plain, true)
	for _, want := range []string{"memory    sbx's default\n", "none recorded", "not running: start it"} {
		if !strings.Contains(got, want) {
			t.Errorf("stopped sandbox's info lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "mem used") {
		t.Errorf("a stopped sandbox shows usage:\n%s", got)
	}

	if got := print(base, true); !strings.Contains(got, "no sandbox yet") {
		t.Errorf("info with no sandbox:\n%s", got)
	}

	failed := base
	failed.exists, failed.running, failed.usageErr = true, true, errors.New("sbx said no")
	if got := print(failed, true); !strings.Contains(got, "couldn't be read: sbx said no") {
		t.Errorf("info when usage can't be read:\n%s", got)
	}

	if got := print(base, false); !strings.Contains(got, "doesn't exist yet") || strings.Contains(got, "agent") {
		t.Errorf("info with no profile:\n%s", got)
	}
}
