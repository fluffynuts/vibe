package main

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"vibe/internal/layout"
	"vibe/internal/state"
)

func TestParseUptime(t *testing.T) {
	got, err := parseUptime("12948.18 310606.19\n")
	if err != nil || got != 12948180*time.Millisecond {
		t.Errorf("parseUptime = %v, %v", got, err)
	}
	for _, bad := range []string{"", "cat: /proc/uptime: No such file or directory\n", "-5 1\n"} {
		if _, err := parseUptime(bad); err == nil {
			t.Errorf("parseUptime(%q) accepted it", bad)
		}
	}
}

func TestFormatUptime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "under a minute",
		7 * time.Minute:  "7m",
		3*time.Hour + 35*time.Minute + 48*time.Second: "3h 35m",
		48*time.Hour + 7*time.Minute:                  "2d 0h 7m",
	} {
		if got := formatUptime(d); got != want {
			t.Errorf("formatUptime(%v) = %q, want %q", d, got, want)
		}
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
		uptime:   3*time.Hour + 35*time.Minute,
		memTotal: 12 << 30, memAvailable: 9 << 30,
		diskSize: 20 << 30, diskUsed: 5 << 30,
		dockerSize: 50 << 30, dockerUsed: 10 << 30,
	}
	got := print(running, true)
	for _, want := range []string{
		"sandbox   proj\n", "profile   yumbi\n", "agent     claude\n", "memory    12g\n",
		"features  go, node\n", "status    running\n", "uptime    3h 35m\n",
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

	running.usage.uptime = 0
	if got := print(running, true); strings.Contains(got, "uptime") {
		t.Errorf("a sandbox whose uptime couldn't be read shows one:\n%s", got)
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

func TestInfoSaysHowToReachThePageFromAnotherMachine(t *testing.T) {
	info := sandboxInfo{name: "proj", target: "/code/proj", profile: "p", exists: true,
		page: "http://localhost:10000/#tok", pageServed: true, forwards: []int{10000, 5390, 5391}, sshTarget: "dave@box"}
	print := func() string {
		var out strings.Builder
		printInfo(&out, info, false)
		return out.String()
	}
	got := print()
	for _, want := range []string{
		"page      http://localhost:10000/#tok (being served now)\n",
		// sbx binds published ports on 127.0.0.1 alone: "localhost" could
		// be ::1 on the far side.
		"ssh -N -L 10000:127.0.0.1:10000 -L 5390:127.0.0.1:5390 -L 5391:127.0.0.1:5391 dave@box\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("info lacks %q:\n%s", want, got)
		}
	}

	info.pageServed = false
	if got := print(); !strings.Contains(got, "not being served: 'vibe --headless' here serves it") {
		t.Errorf("a page nobody serves:\n%s", got)
	}

	info.page = ""
	if got := print(); strings.Contains(got, "page ") || strings.Contains(got, "ssh") {
		t.Errorf("a sandbox with no page yet shows one:\n%s", got)
	}
}

func recordWithPage(t *testing.T, lay layout.Layout) state.Instance {
	t.Helper()
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.CompanionPort = 10000
	inst.Publish = []state.PublishRecord{{Name: "clipboard", ContainerPort: 5390, HostPort: 5402}, {Name: "dashboard", ContainerPort: 5391, HostPort: 5403}}
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestInfoForwardsThePagesPortThenEveryPublishedOne(t *testing.T) {
	lay := updateLayout(t)
	recordWithPage(t, lay)
	token, err := state.CompanionToken(lay.Home, "box")
	if err != nil {
		t.Fatal(err)
	}
	var info sandboxInfo
	addPage(&info, lay.Home, "box")
	if want := []int{10000, 5402, 5403}; !slices.Equal(info.forwards, want) {
		t.Errorf("forwards %v, want %v", info.forwards, want)
	}
	if info.page != "http://localhost:10000/#"+token {
		t.Errorf("page %q", info.page)
	}
	if info.pageServed {
		t.Error("the page is said to be served, with nothing serving it")
	}
}

// The Admin tab shows what --info does of the sandbox, on the page itself:
// its own address, token and all, is no news there.
func TestTheAdminTabDoesntShowThePagesOwnAddress(t *testing.T) {
	lay := updateLayout(t)
	inst := recordWithPage(t, lay)
	loggingSbx(t, "box stopped\\n")
	token, err := state.CompanionToken(lay.Home, "box")
	if err != nil {
		t.Fatal(err)
	}
	a := &companionAdmin{lay: lay, name: "box", target: inst.Target}
	shown := a.Status().Info
	if strings.Contains(shown, token) || strings.Contains(shown, "ssh -N") {
		t.Errorf("the Admin tab shows the page's address or the ssh line:\n%s", shown)
	}
}
