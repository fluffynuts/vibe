package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"vibe/internal/sbxtest"
)

// usageSandbox puts an `sbx` on PATH answering liveUsage's reads as a
// sandbox would; a read named in fail fails instead.
func usageSandbox(t *testing.T, fail ...string) {
	t.Helper()
	answers := map[string]string{
		"cat /proc/uptime":       "3723.5 100.0\n",
		"cat /proc/meminfo":      "MemTotal:       4000000 kB\nMemFree:        1000 kB\nMemAvailable:   3000000 kB\n",
		"df -Pk /":               "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/vda 1000 400 600 40% /\n",
		"df -Pk /var/lib/docker": "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/vdb 2000 500 1500 25% /var/lib/docker\n",
	}
	script := "shift 3\ncase \"$*\" in\n"
	for read, answer := range answers {
		failing := false
		for _, f := range fail {
			failing = failing || f == read
		}
		if failing {
			script += "\"" + read + "\") echo 'no such thing' >&2; exit 1;;\n"
		} else {
			script += "\"" + read + "\") printf '%s' '" + answer + "';;\n"
		}
	}
	script += "*) exit 2;;\nesac\n"
	sbxtest.OnPath(t, script)
}

func TestLiveUsageReadsUptimeMemoryAndDisks(t *testing.T) {
	usageSandbox(t)
	u, err := liveUsage(context.Background(), "box")
	if err != nil {
		t.Fatal(err)
	}
	want := sandboxUsage{uptime: 3723500 * time.Millisecond, memTotal: 4000000 << 10, memAvailable: 3000000 << 10,
		diskSize: 1000 << 10, diskUsed: 400 << 10, dockerSize: 2000 << 10, dockerUsed: 500 << 10}
	if u != want {
		t.Errorf("usage = %+v, want %+v", u, want)
	}
}

func TestLiveUsageDoesWithoutUptimeOrDocker(t *testing.T) {
	usageSandbox(t, "cat /proc/uptime", "df -Pk /var/lib/docker")
	u, err := liveUsage(context.Background(), "box")
	if err != nil {
		t.Fatal(err)
	}
	if u.uptime != 0 || u.dockerSize != 0 || u.memTotal == 0 || u.diskSize == 0 {
		t.Errorf("usage = %+v", u)
	}
}

func TestLiveUsageSaysWhichReadFailed(t *testing.T) {
	usageSandbox(t, "cat /proc/meminfo", "df -Pk /")
	if _, err := liveUsage(context.Background(), "box"); err == nil || !strings.Contains(err.Error(), "reading /proc/meminfo") {
		t.Errorf("err = %v, want the memory read's", err)
	}
	usageSandbox(t, "df -Pk /")
	if _, err := liveUsage(context.Background(), "box"); err == nil || !strings.Contains(err.Error(), "running df") {
		t.Errorf("err = %v, want df's", err)
	}
}
