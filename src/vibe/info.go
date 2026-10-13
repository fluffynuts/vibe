package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/gittoken"
	"vibe/internal/layout"
	"vibe/internal/profilegen"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// sandboxInfo is what --info shows about one folder's sandbox.
type sandboxInfo struct {
	name, target, profile string
	exists, running       bool
	agent                 string
	memory                string    // the memory setting; empty for sbx's default
	timezone              string    // the zone the sandbox keeps its time in
	gitToken              string    // where its GitHub token came from; empty for none
	gitRules              []string  // its agent's rules for pushing
	gitTokenExpires       time.Time // when its GitHub token expires; zero when never, or not known
	features              []string  // what a guided profile was composed from, base features aside
	guided                bool      // the profile records what it was composed from
	usage                 *sandboxUsage
	usageErr              error
}

// sandboxUsage is what a running sandbox reports of how long it has been up,
// and of its memory and disk, in bytes. Memory in use is what the kernel couldn't hand back on demand —
// MemTotal less MemAvailable — so the page cache, which grows to fill
// whatever it is given, doesn't count against it.
//
// Disk is the root filesystem: the sandbox's image and everything written
// over it. Docker inside the sandbox keeps its images and containers on a
// disk of their own, reported apart when it has one (dockerSize non-zero).
//
// Uptime is the sandbox's own kernel's, since each sandbox is a VM of its
// own: how long since it was last started. Zero when it couldn't be read.
type sandboxUsage struct {
	uptime                 time.Duration
	memTotal, memAvailable uint64
	diskSize, diskUsed     uint64
	dockerSize, dockerUsed uint64
}

// doInfo prints the settings of the sandbox for target and, if it is
// running, its memory and disk use. A stopped sandbox isn't started: that
// would be a slow and surprising side effect of asking a question.
func doInfo(lay layout.Layout, args cliargs.Args, name, target string) error {
	info, profileKnown, err := gatherInfo(context.Background(), lay, args, name, target)
	if err != nil {
		return err
	}
	printInfo(os.Stdout, info, profileKnown)
	return nil
}

// gatherInfo is what --info shows of the sandbox for target, and whether its
// profile is known; ctx ending gives up on sbx.
func gatherInfo(ctx context.Context, lay layout.Layout, args cliargs.Args, name, target string) (sandboxInfo, bool, error) {
	profile, err := resolveReInitProfile(lay.Home, args, name, target)
	if err != nil {
		return sandboxInfo{}, false, err
	}
	info := sandboxInfo{name: name, target: target, profile: profile, timezone: recordedTimezone(lay.Home, name)}
	if inst, found, err := state.Load(lay.Home, name); err == nil && found {
		info.gitToken, info.gitRules, info.gitTokenExpires = inst.GitToken, inst.GitRules, inst.GitTokenExpires
	}
	if lay.ProfileExists(profile) {
		_, merged, err := loadKit(lay, profile)
		if err != nil {
			return info, false, err
		}
		info.agent = recordedAgent(lay.Home, name, agentOf(merged))
		info.memory = merged.Memory
		info.guided = profilegen.IsGuided(lay.ProfileDir(profile))
		info.features, _ = profilegen.WithoutBase(profilegen.ComposedFrom(lay.ProfileDir(profile)), lay.BaseFeatures())
	}
	statuses, err := sbxrun.ListContext(ctx)
	if err != nil {
		return info, false, err
	}
	for _, s := range statuses {
		if s.Name == name {
			info.exists, info.running = true, s.Running
		}
	}
	if info.running {
		u, err := liveUsage(ctx, name)
		if err != nil {
			info.usageErr = err
		} else {
			info.usage = &u
		}
	}
	return info, lay.ProfileExists(profile), nil
}

// liveUsage reads a running sandbox's uptime, memory and root disk use from
// inside it.
func liveUsage(ctx context.Context, name string) (sandboxUsage, error) {
	var u sandboxUsage
	// Uptime is a nicety: not being able to read it is no reason to hide the
	// memory and disk figures, which are what --info is for.
	if out, err := sbxrun.ExecCaptureContext(ctx, name, "cat", "/proc/uptime"); err == nil {
		u.uptime, _ = parseUptime(out)
	}
	out, err := sbxrun.ExecCaptureContext(ctx, name, "cat", "/proc/meminfo")
	if err != nil {
		return u, fmt.Errorf("reading /proc/meminfo: %w", err)
	}
	if u.memTotal, u.memAvailable, err = parseMeminfo(out); err != nil {
		return u, err
	}
	out, err = sbxrun.ExecCaptureContext(ctx, name, "df", "-Pk", "/")
	if err != nil {
		return u, fmt.Errorf("running df: %w", err)
	}
	if u.diskSize, u.diskUsed, _, err = parseDf(out); err != nil {
		return u, err
	}
	// No Docker in the sandbox, or Docker's data on the root filesystem:
	// either way, nothing to report apart.
	if out, err := sbxrun.ExecCaptureContext(ctx, name, "df", "-Pk", dockerDataDir); err == nil {
		if size, used, mount, err := parseDf(out); err == nil && mount == dockerDataDir {
			u.dockerSize, u.dockerUsed = size, used
		}
	}
	return u, nil
}

// dockerDataDir is where Docker inside a sandbox keeps its images.
const dockerDataDir = "/var/lib/docker"

// parseUptime reads the first field of /proc/uptime: seconds since boot.
func parseUptime(out string) (time.Duration, error) {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0, fmt.Errorf("can't read /proc/uptime: %q", out)
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || secs < 0 {
		return 0, fmt.Errorf("can't read /proc/uptime: %q", out)
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// parseMeminfo reads MemTotal and MemAvailable, in bytes, from /proc/meminfo.
func parseMeminfo(out string) (total, available uint64, err error) {
	var haveTotal, haveAvailable bool
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, haveTotal = n*1024, true
		case "MemAvailable:":
			available, haveAvailable = n*1024, true
		}
	}
	if !haveTotal || !haveAvailable {
		return 0, 0, fmt.Errorf("no MemTotal and MemAvailable in /proc/meminfo")
	}
	return total, available, nil
}

// parseDf reads the size and use, in bytes, of the one filesystem
// `df -Pk` was asked about, and where it is mounted.
func parseDf(out string) (size, used uint64, mount string, err error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(lines) < 2 || len(fields) < 6 {
		return 0, 0, "", fmt.Errorf("can't read df's output: %q", out)
	}
	size, err1 := strconv.ParseUint(fields[1], 10, 64)
	used, err2 := strconv.ParseUint(fields[2], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, "", fmt.Errorf("can't read df's output: %q", out)
	}
	return size * 1024, used * 1024, fields[len(fields)-1], nil
}

// printInfo writes info to w; profileKnown says whether the profile exists
// to have settings.
func printInfo(w io.Writer, info sandboxInfo, profileKnown bool) {
	row := func(label, format string, a ...interface{}) {
		fmt.Fprintf(w, "%-10s%s\n", label, fmt.Sprintf(format, a...))
	}
	row("sandbox", "%s", info.name)
	row("folder", "%s", displayPath(info.target))
	if !profileKnown {
		row("profile", "%s (doesn't exist yet: vibe will offer to create it)", info.profile)
	} else {
		row("profile", "%s", info.profile)
		row("agent", "%s", info.agent)
		if info.memory == "" {
			row("memory", "sbx's default")
		} else {
			row("memory", "%s", info.memory)
		}
		row("timezone", "%s", info.timezone)
		if info.gitToken == "" {
			row("github", "no token: you commit and push yourself")
		} else {
			row("github", "the agent can push (%s token%s); it's told never to force-push%s", info.gitToken,
				prefixed(", ", gittoken.DescribeExpiry(info.gitTokenExpires, timeNow())), rulesSuffix(info.gitRules))
		}
		if len(info.features) == 0 && info.guided {
			row("features", "none beyond the base ones")
		} else if len(info.features) == 0 {
			row("features", "none recorded (not a guided profile)")
		} else {
			row("features", "%s", strings.Join(info.features, ", "))
		}
	}

	switch {
	case !info.exists:
		row("status", "no sandbox yet: running vibe here creates one")
		return
	case !info.running:
		row("status", "not running: start it to see its memory and disk use")
		return
	case info.usageErr != nil:
		row("status", "running, but its use couldn't be read: %s", info.usageErr)
		return
	}
	row("status", "running")
	u := info.usage
	if u.uptime > 0 {
		row("uptime", "%s", formatUptime(u.uptime))
	}
	memUsed := u.memTotal - u.memAvailable
	row("mem used", "%s of %s (%d%%)", formatBytes(memUsed), formatBytes(u.memTotal), percent(memUsed, u.memTotal))
	row("disk used", "%s of %s (%d%%)", formatBytes(u.diskUsed), formatBytes(u.diskSize), percent(u.diskUsed, u.diskSize))
	if u.dockerSize > 0 {
		row("docker", "%s of %s (%d%%), on a disk of its own", formatBytes(u.dockerUsed), formatBytes(u.dockerSize), percent(u.dockerUsed, u.dockerSize))
	}
	fmt.Fprintln(w, "\nmemory use is a snapshot: check it while the sandbox is at its busiest\n"+
		"(building, running tests) before deciding it needs less")
}

func percent(part, whole uint64) uint64 {
	if whole == 0 {
		return 0
	}
	return part * 100 / whole
}

// formatUptime writes d to the minute, leaving out leading units it doesn't
// reach ("3h 35m", "2d 0h 7m", "under a minute").
func formatUptime(d time.Duration) string {
	mins := int64(d / time.Minute)
	days, hours, mins := mins/(24*60), mins/60%24, mins%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm", mins)
	}
	return "under a minute"
}

// formatBytes writes n in the largest binary unit it reaches, to one
// decimal place ("1.9 GiB", "512.0 MiB").
func formatBytes(n uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	v, i := float64(n), 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
