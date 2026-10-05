// Command vibe creates, starts and attaches to docker sbx sandboxes for
// contained agentic coding, driven by profiles bundled alongside the binary.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vibe"
	"vibe/internal/claudetheme"
	"vibe/internal/cliargs"
	"vibe/internal/clockskew"
	"vibe/internal/fscopy"
	"vibe/internal/homeinit"
	"vibe/internal/hostmem"
	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/library"
	agentmem "vibe/internal/memory"
	"vibe/internal/pathresolve"
	"vibe/internal/portalloc"
	"vibe/internal/profilegen"
	"vibe/internal/prompt"
	"vibe/internal/running"
	"vibe/internal/sbxinstall"
	"vibe/internal/sbxrun"
	"vibe/internal/selfupdate"
	"vibe/internal/settings"
	"vibe/internal/sidebyside"
	"vibe/internal/state"
	"vibe/internal/upgrade"
)

const usage = `vibe — open (creating if needed) a sandbox for a project folder.

  vibe                       use $PWD, profile derived from its folder name
  vibe /path/to/code         use an explicit folder
  vibe -n/--name custom .    override the derived sandbox name
  vibe -p/--profile foo      use profile "foo" instead of the derived one
  vibe -s/--stop [path]      stop the sandbox for path (default: $PWD)
  vibe -c/--ssh [path]       ssh into the sandbox for path
  vibe -r/--re-init [path]   remove and recreate the sandbox from its profile
  vibe -r -f/--force         ...without prompting
  vibe -f                    ...and, for an unknown profile, create a blank
                             one instead of asking
  vibe -R/--re-create [path] delete the profile too, then re-init — the
                             profile is gone, so this always re-prompts
  vibe -C/--re-compose [path] rebuild a guided profile from the library
                             features it was composed from, picking up
                             whatever they have gained since, then re-init
  vibe -l/--list             list every known sandbox and its status
  vibe -a/--info [path]      show the sandbox's settings (memory, agent,
                             features) and, while it runs, how much memory
                             and disk it is using
  vibe -x/--cleanup          pick sandboxes from a checklist and delete them;
                             the profiles they were built from are kept
  vibe -d/--delete [path]    pick from a checklist whether to delete the
                             sandbox for path (ticked), its profile
                             (unticked), or both; -f deletes both unasked
  vibe -U/--upgrade          check GitHub for newer releases of vibe and
                             Docker SBX, pick which to upgrade (all are
                             ticked), and install them; -f upgrades them all
                             without asking (-f and -u are passed on to
                             vibe's --install)
  vibe -I/--install-sbx      download the latest stable Docker Sandboxes (sbx)
                             release and install it: on Linux into
                             ~/.docker/sbx; on macOS (Apple Silicon) and
                             Windows, asking how (-f takes the first way,
                             and reinstalls when already up to date)
  vibe -v/--version          print the version and the commit it was built from
  vibe -i/--install          copy defaults/profiles/library, config.yaml and
                             settings.yaml into ~/.vibe, and the vibe binary
                             into ~/.local/bin, so the unpacked bundle this
                             was run from can be deleted afterward. Run from
                             a newer release, it upgrades: new files are
                             copied, files you never edited are updated, and
                             ones changed on both sides are merged or asked
                             about
  vibe -i -u/--update-strategy S
                             settle files changed on both sides without
                             asking: keep, update, merge,keep or merge,update

-s, -c, -a, -r, -R, -C and -d resolve the sandbox name exactly as a normal run
would, so "vibe -s && vibe" restarts whatever you were working on. -l and -x
work on every sandbox at once and take no path.

When a folder has no profile yet, vibe offers to create one: a copy of an
existing profile, a blank profile to grow yourself, or a guided walk
through picking features from the library. Profiles live in profiles/<name>
next to the vibe binary. ~/.vibe (or $VIBE_HOME) overrides the bundle: a
single file or a whole profile/feature overrides one at a time, so you can
override just one library feature (say, library/mysql) without affecting
any other, and pick up new ones the bundle adds later.

-c requires 'sbx setup ssh' to have been run once on this machine.
`

// session is this process's claim on the folder it has open, if any; see
// guardFolder. It lives for the whole run, since dropping it drops the claim.
var session *running.Session

func main() {
	prompt.EnableVT(os.Stdout)
	prompt.EnableVT(os.Stderr)
	err := run(os.Args[1:])
	session.Release()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vibe: %s\n", err)
		os.Exit(1)
	}
}

func note(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "vibe: "+format+"\n", a...)
}

func run(argv []string) error {
	args, err := cliargs.Parse(argv)
	if err != nil {
		return err
	}
	if args.Help {
		fmt.Print(usage)
		return nil
	}
	if args.Version {
		fmt.Println(vibe.String())
		return nil
	}
	if args.UpdateStrategy != "" && !args.Install && !args.Upgrade {
		return fmt.Errorf("--update-strategy only applies to --install and --upgrade")
	}
	if args.ExclusiveActions() > 1 {
		return fmt.Errorf("--stop, --ssh, --re-init, --re-create, --re-compose, --list, --install, --upgrade, --install-sbx, --cleanup, --delete and --info are mutually exclusive")
	}

	vibeHome := vibeHomeDir()

	if args.List {
		if args.Path != "" {
			return fmt.Errorf("--list takes no path argument")
		}
		return doList()
	}

	if args.Upgrade {
		if args.Path != "" {
			return fmt.Errorf("--upgrade takes no path argument")
		}
		return doUpgrade(args.Force, args.UpdateStrategy)
	}

	if args.InstallSbx {
		if args.Path != "" {
			return fmt.Errorf("--install-sbx takes no path argument")
		}
		return doInstallSbx(args.Force)
	}

	if args.Install {
		if args.Path != "" {
			return fmt.Errorf("--install takes no path argument")
		}
		bundleRoot, err := pathresolve.BundleRoot()
		if err != nil {
			return err
		}
		if err := doInstall(layout.New(vibeHome, bundleRoot), args.Force, args.UpdateStrategy); err != nil {
			return err
		}
		return offerSbxInstall(args.Force)
	}

	if !sbxrun.Available() {
		hint := "Install it from " + sbxinstall.Releases + ",\n"
		if sbxinstall.Supported(runtime.GOOS, runtime.GOARCH) {
			hint = "Install it with 'vibe --install-sbx', or from " + sbxinstall.Releases + ",\n"
		}
		return fmt.Errorf("'sbx' is not on PATH — vibe needs Docker Sandboxes.\n%s"+
			"then make sure its bin directory is on PATH (e.g. ${HOME}/.docker/sbx/bin).", hint)
	}

	// Before the bundle is resolved and ~/.vibe is seeded: cleanup is about
	// sandboxes sbx holds, and needs neither a profile nor a target folder.
	if args.Cleanup {
		if args.Path != "" {
			return fmt.Errorf("--cleanup takes no path argument")
		}
		return doCleanup(vibeHome)
	}

	bundleRoot, err := pathresolve.BundleRoot()
	if err != nil {
		return err
	}
	lay := layout.New(vibeHome, bundleRoot)
	if err := initHome(lay, args.Force); err != nil {
		return err
	}

	// Windows PowerShell hands "~\project" over as it is, and vibe shows
	// folders that way itself, so the "~" is vibe's to expand.
	target := expandHome(args.Path)
	if target == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		target = wd
	}
	info, statErr := os.Stat(target)
	if statErr != nil || !info.IsDir() {
		return fmt.Errorf("not a directory: %s", target)
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}

	name := args.Name
	if name == "" {
		name = pathresolve.DeriveName(target)
	}

	switch {
	case args.Stop:
		return doStop(name, target)
	case args.Ssh:
		return doSsh(name, target)
	case args.Info:
		return doInfo(lay, args, name, target)
	}

	if err := guardFolder(vibeHome, target, args.Force); err != nil {
		return err
	}

	switch {
	case args.ReInit:
		return doReInit(lay, args, name, target)
	case args.ReCreate:
		return doReCreate(lay, args, name, target)
	case args.ReCompose:
		return doReCompose(lay, args, name, target)
	case args.Delete:
		return doDelete(lay, args, name, target)
	default:
		return doCreateOrAttach(lay, args, name, target)
	}
}

func vibeHomeDir() string {
	if v := os.Getenv("VIBE_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".vibe"
	}
	return filepath.Join(home, ".vibe")
}

// expandHome expands a leading "~", "~/" or "~\" in a path: a settings.yaml
// value, or a folder given on the command line.
func expandHome(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}

func resolveProfileName(args cliargs.Args, target string) string {
	if args.Profile != "" {
		return args.Profile
	}
	if args.Name != "" {
		return args.Name
	}
	return pathresolve.DeriveName(target)
}

// --- one vibe per folder ----------------------------------------------

// stopOtherTimeout is how long to wait for another vibe to wind down after
// being asked to stop: long enough for it to save its agent's memories.
const stopOtherTimeout = 60 * time.Second

// guardFolder checks for other vibe processes with target open — two agents
// taking instructions against one workspace is easy to do by accident — and
// asks what to do if there are any. It then claims target for this process.
// With force, or no terminal to ask on, it says so and carries on.
func guardFolder(vibeHome, target string, force bool) error {
	if others := running.Others(vibeHome, target); len(others) > 0 {
		if err := resolveOthers(vibeHome, target, others, force); err != nil {
			return err
		}
	}
	s, err := running.Register(vibeHome, target)
	if err != nil {
		note("WARNING: could not record this vibe as running for %s: %s", target, err)
		return nil
	}
	session = s
	go exitOnStopRequest(s)
	return nil
}

// sessionMu guards inSession, which records whether finish has handed the
// terminal to sbx: from then on, a stop request is finish's to act on.
var (
	sessionMu sync.Mutex
	inSession bool
)

// exitOnStopRequest handles another vibe asking this one to stop before its
// session has started — at a prompt, say — by simply exiting. Once the
// session is running, finish ends it properly instead.
func exitOnStopRequest(s *running.Session) {
	<-s.StopRequested()
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if inSession {
		return
	}
	// A picker waiting on a keypress has the terminal in raw mode, with the
	// cursor at the end of its hint line: wipe it and put the terminal back
	// before saying why. Any other question has the cursor after it.
	if !prompt.Abort() {
		fmt.Fprintln(os.Stderr)
	}
	note("asked to stop by another vibe for this folder — exiting")
	s.Release()
	os.Exit(1)
}

func resolveOthers(vibeHome, target string, others []int, force bool) error {
	pids := make([]string, len(others))
	for i, pid := range others {
		pids[i] = fmt.Sprint(pid)
	}
	which, instance := "PID", "that instance"
	if len(others) > 1 {
		which, instance = "PIDs", "those instances"
	}
	already := fmt.Sprintf("vibe is already running for %s (%s %s)", target, which, strings.Join(pids, ", "))

	if force {
		note("WARNING: %s — continuing anyway (--force)", already)
		return nil
	}
	if !interactive() {
		return fmt.Errorf("%s", already)
	}
	note("%s", already)
	choice, ok := chooseFrom("WARNING: vibe is already running in this folder! What would you like to do?", []string{
		"exit",
		"continue anyway",
		"stop " + instance + ", then continue",
	}, 0)
	switch {
	case !ok || choice == 0:
		return fmt.Errorf("already running for %s — exiting", target)
	case choice == 1:
		return nil
	}
	for _, pid := range others {
		status := newStatusLine()
		status.show("Stopping vibe (PID %d)", pid)
		killed, err := running.Stop(vibeHome, pid, stopOtherTimeout)
		switch {
		case err != nil:
			status.done("Stopping vibe (PID %d) failed: %s", pid, err)
			return fmt.Errorf("could not stop the other vibe for %s", target)
		case killed:
			status.done("Killed vibe (PID %d): it did not stop within %s, so its memories may not have been saved", pid, stopOtherTimeout)
		default:
			status.done("Stopped vibe (PID %d)", pid)
		}
	}
	return nil
}

// --- stop / ssh -------------------------------------------------------

func doStop(name, target string) error {
	if !sbxrun.Exists(name) {
		return fmt.Errorf("no sandbox named '%s' for %s — nothing to stop", name, target)
	}
	note("stopping sandbox '%s'", name)
	code, err := sbxrun.Stop(name)
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}

func doSsh(name, target string) error {
	if !sbxrun.Exists(name) {
		return fmt.Errorf("no sandbox named '%s' for %s — run vibe first", name, target)
	}
	host := name + ".sbx"
	if !sbxrun.SshConfigured(host) {
		return fmt.Errorf("no ssh configuration for %s\n"+
			"(checked ~/.ssh/config and everything it Includes, via 'ssh -G')\n"+
			"Run 'sbx setup ssh' once on this machine.", host)
	}
	note("connecting to %s", host)
	code, err := sbxrun.Ssh(host)
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}

// --- list ---------------------------------------------------------------

func doList() error {
	statuses, err := sbxrun.List()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		note("no sandboxes found")
		return nil
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	for _, s := range statuses {
		text := "not running"
		if s.Running {
			text = "running"
		}
		fmt.Printf("%-16s%s\n", s.Name, text)
	}
	return nil
}

// --- info -------------------------------------------------------------------

// sandboxInfo is what --info shows about one folder's sandbox.
type sandboxInfo struct {
	name, target, profile string
	exists, running       bool
	agent                 string
	memory                string   // the memory setting; empty for sbx's default
	features              []string // what a guided profile was composed from
	usage                 *sandboxUsage
	usageErr              error
}

// sandboxUsage is what a running sandbox reports of its memory and disk, in
// bytes. Memory in use is what the kernel couldn't hand back on demand —
// MemTotal less MemAvailable — so the page cache, which grows to fill
// whatever it is given, doesn't count against it.
//
// Disk is the root filesystem: the sandbox's image and everything written
// over it. Docker inside the sandbox keeps its images and containers on a
// disk of their own, reported apart when it has one (dockerSize non-zero).
type sandboxUsage struct {
	memTotal, memAvailable uint64
	diskSize, diskUsed     uint64
	dockerSize, dockerUsed uint64
}

// doInfo prints the settings of the sandbox for target and, if it is
// running, its memory and disk use. A stopped sandbox isn't started: that
// would be a slow and surprising side effect of asking a question.
func doInfo(lay layout.Layout, args cliargs.Args, name, target string) error {
	profile, err := resolveReInitProfile(lay.Home, args, name, target)
	if err != nil {
		return err
	}
	info := sandboxInfo{name: name, target: target, profile: profile}
	if lay.ProfileExists(profile) {
		_, merged, err := loadKit(lay, profile)
		if err != nil {
			return err
		}
		info.agent = agentOf(merged)
		info.memory = merged.Memory
		info.features = profilegen.ComposedFrom(lay.ProfileDir(profile))
	}
	statuses, err := sbxrun.List()
	if err != nil {
		return err
	}
	for _, s := range statuses {
		if s.Name == name {
			info.exists, info.running = true, s.Running
		}
	}
	if info.running {
		u, err := liveUsage(name)
		if err != nil {
			info.usageErr = err
		} else {
			info.usage = &u
		}
	}
	printInfo(os.Stdout, info, lay.ProfileExists(profile))
	return nil
}

// liveUsage reads a running sandbox's memory and root disk use from inside it.
func liveUsage(name string) (sandboxUsage, error) {
	var u sandboxUsage
	out, err := sbxrun.ExecCapture(name, "cat", "/proc/meminfo")
	if err != nil {
		return u, fmt.Errorf("reading /proc/meminfo: %w", err)
	}
	if u.memTotal, u.memAvailable, err = parseMeminfo(out); err != nil {
		return u, err
	}
	out, err = sbxrun.ExecCapture(name, "df", "-Pk", "/")
	if err != nil {
		return u, fmt.Errorf("running df: %w", err)
	}
	if u.diskSize, u.diskUsed, _, err = parseDf(out); err != nil {
		return u, err
	}
	// No Docker in the sandbox, or Docker's data on the root filesystem:
	// either way, nothing to report apart.
	if out, err := sbxrun.ExecCapture(name, "df", "-Pk", dockerDataDir); err == nil {
		if size, used, mount, err := parseDf(out); err == nil && mount == dockerDataDir {
			u.dockerSize, u.dockerUsed = size, used
		}
	}
	return u, nil
}

// dockerDataDir is where Docker inside a sandbox keeps its images.
const dockerDataDir = "/var/lib/docker"

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
		if len(info.features) == 0 {
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

// --- cleanup ----------------------------------------------------------------

// cleanupRow is one sandbox offered for deletion: what sbx knows about it,
// plus what vibe's instance record adds — the profile it was built from and
// the folder it was built for, which is what makes a list of terse sandbox
// names identifiable months later.
type cleanupRow struct {
	name    string
	running bool
	profile string
	target  string
}

// cleanupRows pairs every sandbox sbx knows about with vibe's record of it,
// by name, sorted. A sandbox with no record — made by hand, by an older
// vibe, or left behind when ~/.vibe was cleared — is offered all the same:
// cleanup is about what sbx is holding, not about what vibe remembers
// creating, and an unrecognised sandbox is exactly the kind that accumulates.
func cleanupRows(statuses []sbxrun.Status, instances []state.Instance) []cleanupRow {
	known := make(map[string]state.Instance, len(instances))
	for _, inst := range instances {
		known[inst.Name] = inst
	}
	rows := make([]cleanupRow, 0, len(statuses))
	for _, s := range statuses {
		row := cleanupRow{name: s.Name, running: s.Running}
		if inst, ok := known[s.Name]; ok {
			row.profile, row.target = inst.Profile, inst.Target
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	return rows
}

// label renders a row for the checklist, in the same name/status columns
// --list prints, with whatever vibe knows about the sandbox after them.
func (r cleanupRow) label() string {
	status := "not running"
	if r.running {
		status = "running"
	}
	detail := "vibe has no record of it"
	switch {
	case r.profile != "" && r.target != "":
		detail = fmt.Sprintf("profile %s, %s", r.profile, r.target)
	case r.profile != "":
		detail = "profile " + r.profile
	case r.target != "":
		detail = r.target
	}
	return fmt.Sprintf("%-16s %-11s  (%s)", r.name, status, detail)
}

// doCleanup offers every sandbox sbx knows about as a checklist and deletes
// the ones the user checks — and nothing else. Profiles are deliberately
// left alone: a profile outlives the sandboxes built from it, so deleting
// one here would silently turn the next plain `vibe` in that folder back
// into a guided-creation prompt. Removing a profile is what --re-create is
// for, and it says so up front.
//
// The checklist's own confirmation — which lists what is checked and
// defaults to going ahead — is the only one asked. A second question after
// it would be asking the same thing twice: the user came here to delete
// sandboxes and has just read back the list of them. What that leaves no
// room for is the warning, so it goes above the list, where it is on screen
// the whole time the choice is being made. --re-init and friends still ask
// their own question, because there the deletion is a side effect of
// something else the user asked for.
func doCleanup(vibeHome string) error {
	statuses, err := sbxrun.List()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		note("no sandboxes found — nothing to clean up")
		return nil
	}
	instances, err := state.List(vibeHome)
	if err != nil {
		return err
	}
	rows := cleanupRows(statuses, instances)
	labels := make([]string, len(rows))
	for i, r := range rows {
		labels[i] = r.label()
	}
	if !interactive() {
		return fmt.Errorf("--cleanup needs a terminal to pick sandboxes on")
	}
	// Unlike --re-init, nothing is rebuilt afterwards: whatever is only
	// inside these sandboxes — uncommitted work, agent memories no re-init
	// ever backed up — goes with them.
	note("deleting a sandbox cannot be undone, and takes any agent memories it holds with it")
	picked, ok := checklistFrom("Check the sandboxes to delete (their profiles are kept):", labels, nil)
	if !ok {
		return fmt.Errorf("aborted — nothing deleted")
	}
	if len(picked) == 0 {
		note("nothing checked — nothing deleted")
		return nil
	}
	names := make([]string, len(picked))
	for i, idx := range picked {
		names[i] = rows[idx].name
	}
	return removeSandboxes(vibeHome, names, sbxrun.Remove)
}

// removeSandboxes deletes each named sandbox and drops vibe's record of it,
// carrying on past a failure so one stuck sandbox doesn't strand the rest
// and reporting at the end what it could not remove.
//
// The instance record has to go with the sandbox here. --re-init keeps it on
// purpose, because the sandbox it rebuilds re-claims the host ports the
// record remembers — but nothing is coming back from a cleanup, and
// allocatePublish treats every port a record claims as taken whether or not
// anything is listening on it, so a record left behind would reserve that
// sandbox's published ports against every future sandbox, forever.
//
// remove is sbxrun.Remove in production, and a stub under test.
func removeSandboxes(vibeHome string, names []string, remove func(name string, force bool) error) error {
	var failed []string
	for _, name := range names {
		note("removing sandbox '%s'", name)
		// Force: the checklist shows which are running, and having checked
		// one the user does not want to be told it is up.
		if err := remove(name, true); err != nil {
			note("  WARNING: could not remove '%s': %s", name, err)
			failed = append(failed, name)
			continue
		}
		if err := state.Remove(vibeHome, name); err != nil {
			note("  WARNING: %s", err)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not remove: %s", strings.Join(failed, ", "))
	}
	if len(names) == 1 {
		note("removed sandbox '%s'; the profile it was built from was left alone", names[0])
	} else {
		note("removed %d sandboxes; the profiles they were built from were left alone", len(names))
	}
	return nil
}

// --- install ----------------------------------------------------------------

// doInstall makes vibe fully self-contained under lay.Home (~/.vibe or
// $VIBE_HOME) plus a copy of the binary on PATH, so the unpacked bundle it
// was run from — lay.Bundle — can be deleted afterward. It never
// overwrites anything already at the destination, so it's safe to re-run
// (e.g. after fetching a newer bundle release) without losing local edits;
// re-running only fills in what's missing.
func doInstall(lay layout.Layout, force bool, strategyValue string) error {
	if lay.Home == "" {
		return fmt.Errorf("no home directory to install into, and $VIBE_HOME is not set")
	}
	var strategy upgrade.Strategy
	if strategyValue != "" {
		var err error
		if strategy, err = upgrade.ParseStrategy(strategyValue); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(lay.Home, 0o755); err != nil {
		return err
	}
	note("installing into %s", lay.Home)
	_, statErr := os.Stat(lay.HomePath("settings.yaml"))
	firstInstall := os.IsNotExist(statErr)

	// A file changed both in ~/.vibe and in the package is settled by the
	// strategy if one was given, else by asking — and with -f, or nobody
	// to ask, it's left alone and reported below.
	var decide func(upgrade.Conflict) (upgrade.Choice, error)
	if !strategy.Given && !force && interactive() {
		decide = decideConflict
	}
	res, err := upgrade.Run(upgrade.Options{
		Package:  lay.Bundle,
		Home:     lay.Home,
		Strategy: strategy,
		Decide:   decide,
		Log:      func(format string, a ...interface{}) { note("  "+format, a...) },
	})
	if err != nil {
		return err
	}
	if res.Identical > 0 {
		note("  %d file(s) already up to date", res.Identical)
	}

	// A first install sets up ~/.vibe/settings.yaml for this machine; a
	// later one only makes sure its memory is still something this
	// machine can give a sandbox.
	if firstInstall {
		if err := setUpSettings(lay, !force); err != nil {
			return err
		}
	} else if err := lowerMemorySetting(lay.HomePath("settings.yaml")); err != nil {
		note("  WARNING: couldn't check the memory setting in %s: %s", displayPath(lay.HomePath("settings.yaml")), err)
	}

	binErr := installBinary()

	if len(res.UpdatedCopies) > 0 {
		note("")
		note("these couldn't be merged automatically — merge each by hand, then delete the .updated copy:")
		for _, rel := range res.UpdatedCopies {
			mine := filepath.Join(lay.Home, strings.TrimSuffix(rel, upgrade.UpdatedSuffix))
			note("  yours: %s", displayPath(mine))
			note("  new:   %s", displayPath(mine+upgrade.UpdatedSuffix))
		}
	}
	if len(res.Unresolved) > 0 {
		note("")
		note("WARNING: %d file(s) changed both in %s and upstream in the package were left as they are:",
			len(res.Unresolved), displayPath(lay.Home))
		for _, rel := range res.Unresolved {
			note("  %s", displayPath(filepath.Join(lay.Home, rel)))
		}
		note("re-run with --update-strategy to settle them: %s", upgrade.StrategyValues)
		note("  merge,keep    merge what merges; keep yours for the rest, with the new version beside it as .updated")
		note("  merge,update  merge what merges; take the package's version for the rest")
		note("  update        take the package's version of every one")
		note("  keep          keep yours (re-run with another strategy to take the changes later)")
		return fmt.Errorf("upstream changes left unmerged in %d file(s)", len(res.Unresolved))
	}
	return binErr
}

// sbxOnPath reports whether sbx is on PATH. A variable so tests can say
// either way.
var sbxOnPath = func() bool {
	_, err := exec.LookPath("sbx")
	return err == nil
}

// offerSbxInstall, run after --install, offers to install Docker SBX when
// sbx isn't on PATH, since vibe can't start a sandbox without it. With -f,
// or no terminal to ask on, it only says how.
func offerSbxInstall(force bool) error {
	if sbxOnPath() {
		return nil
	}
	if !sbxinstall.Supported(runtime.GOOS, runtime.GOARCH) {
		note("sbx was not found on your PATH — install it from %s", sbxinstall.Releases)
		return nil
	}
	if force || !interactive() {
		note("sbx was not found on your PATH — 'vibe --install-sbx' installs it")
		return nil
	}
	if !confirmDefault(false, true, "sbx was not found on your PATH — install now?") {
		note("not installing sbx — 'vibe --install-sbx' installs it later")
		return nil
	}
	return doInstallSbx(false)
}

// pendingUpgrade is something --upgrade found a newer release of, and how to
// install it.
type pendingUpgrade struct {
	label string
	run   func() error
}

// doUpgrade checks GitHub for newer releases of vibe and of Docker SBX,
// offers whichever there are in a checklist (all ticked), and installs the
// ones picked. With -f, or no terminal to ask on, it installs them all; -f
// and --update-strategy are passed on to the --install that upgrades vibe.
func doUpgrade(force bool, strategyValue string) error {
	if strategyValue != "" {
		if _, err := upgrade.ParseStrategy(strategyValue); err != nil {
			return err // before downloading anything
		}
	}
	var found []pendingUpgrade
	var unchecked []string
	status := newStatusLine()

	status.show("Checking for a newer vibe")
	if u, summary, err := checkVibeUpgrade(force, strategyValue); err != nil {
		status.done("Checking for a newer vibe failed: %s", err)
		unchecked = append(unchecked, "vibe")
	} else {
		status.done("%s", summary)
		if u != nil {
			found = append(found, *u)
		}
	}

	status.show("Checking for a newer Docker SBX")
	if u, summary, err := checkSbxUpgrade(); err != nil {
		status.done("Checking for a newer Docker SBX failed: %s", err)
		unchecked = append(unchecked, "sbx")
	} else {
		status.done("%s", summary)
		if u != nil {
			found = append(found, *u)
		}
	}

	if len(found) == 0 {
		if len(unchecked) > 0 {
			return fmt.Errorf("couldn't check for a newer %s (see above)", strings.Join(unchecked, " or "))
		}
		fmt.Println("vibe and sbx are up to date")
		return nil
	}

	chosen, err := chooseUpgrades(found, force)
	if err != nil {
		return err
	}
	var errs []error
	for _, u := range chosen {
		if err := u.run(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// availableUpgrades checks, quietly, for newer releases of vibe and of
// Docker SBX, returning the labels of those found. A check that fails finds
// nothing: this only ever offers a hint, so it never complains.
func availableUpgrades() []string {
	var labels []string
	// A build with no build number was made from source, not released: any
	// release of the same version counts as newer than it (which is right
	// for an explicit --upgrade), so suggesting one would be noise to anyone
	// building vibe themselves — very likely of the commit just released.
	if vibe.Build != "" {
		if u, _, err := checkVibeUpgrade(false, ""); err == nil && u != nil {
			labels = append(labels, u.label)
		}
	}
	if u, _, err := checkSbxUpgrade(); err == nil && u != nil {
		labels = append(labels, u.label)
	}
	return labels
}

// checkUpgradesInBackground starts availableUpgrades while a session runs,
// returning a function to call once it has ended, which says what's newer
// and recommends --upgrade. It says nothing when there's nothing newer, or
// the check hasn't finished: a session isn't kept waiting on GitHub.
func checkUpgradesInBackground() (report func()) {
	found := make(chan []string, 1)
	go func() { found <- availableUpgrades() }()
	return func() {
		select {
		case labels := <-found:
			reportUpgrades(os.Stderr, labels)
		default:
		}
	}
}

// reportUpgrades tells w of the upgrades labels lists, if any.
func reportUpgrades(w io.Writer, labels []string) {
	if len(labels) == 0 {
		return
	}
	fmt.Fprintln(w, "vibe: newer releases are available:")
	for _, l := range labels {
		fmt.Fprintf(w, "vibe:   %s\n", l)
	}
	fmt.Fprintln(w, "vibe: run 'vibe --upgrade' to install them")
}

// pickUpgrades asks which of labels to upgrade, all ticked to start with,
// returning the indices picked; ok is false when the user quit. A variable
// so tests can stand in for the user.
var pickUpgrades = func(labels []string) ([]int, bool) {
	checked := make([]bool, len(labels))
	for i := range checked {
		checked[i] = true
	}
	// Each item is the upgrade itself, done as soon as it's picked: a
	// "continue?" after that would only repeat the list.
	return checklistNoConfirmFrom("pick what to upgrade", labels, checked)
}

// chooseUpgrades settles which of found to install: all of them with -f
// or no terminal to ask on, else the ones the user picks.
func chooseUpgrades(found []pendingUpgrade, force bool) ([]pendingUpgrade, error) {
	if force || !interactive() {
		return found, nil
	}
	labels := make([]string, len(found))
	for i, u := range found {
		labels[i] = u.label
	}
	idxs, ok := pickUpgrades(labels)
	if !ok {
		return nil, fmt.Errorf("aborted — nothing upgraded")
	}
	if len(idxs) == 0 {
		return nil, errors.New("nothing selected to update")
	}
	chosen := make([]pendingUpgrade, len(idxs))
	for i, idx := range idxs {
		chosen[i] = found[idx]
	}
	return chosen, nil
}

// checkVibeUpgrade checks for a newer release of vibe, returning how to
// upgrade to it (nil when there's none) and a line saying what was found.
func checkVibeUpgrade(force bool, strategyValue string) (*pendingUpgrade, string, error) {
	tag, err := selfupdate.LatestTag()
	if err != nil {
		return nil, "", err
	}
	latest := strings.TrimPrefix(tag, "v")
	running := vibe.FullVersion()
	if !selfupdate.Newer(latest, running) {
		return nil, fmt.Sprintf("vibe %s is the latest", running), nil
	}
	return &pendingUpgrade{
		label: fmt.Sprintf("vibe %s → %s", running, latest),
		run: func() error {
			note("upgrading vibe %s to %s", running, latest)
			return upgradeVibe(tag, force, strategyValue)
		},
	}, fmt.Sprintf("vibe %s is available (you have %s)", latest, running), nil
}

// checkSbxUpgrade checks for a newer stable release of Docker SBX than the
// one installed, returning how to upgrade to it — the same way it was
// installed — (nil when there's none, or vibe can't) and a line saying
// what was found.
func checkSbxUpgrade() (*pendingUpgrade, string, error) {
	if !sbxinstall.Supported(runtime.GOOS, runtime.GOARCH) {
		return nil, fmt.Sprintf("Docker SBX has no release for %s/%s to upgrade to", runtime.GOOS, runtime.GOARCH), nil
	}
	methods, err := sbxMethods()
	if err != nil {
		return nil, "", err
	}
	m, ok := installedSbxMethod(methods)
	if !ok {
		if p, err := exec.LookPath("sbx"); err == nil {
			return nil, fmt.Sprintf("Docker SBX at %s wasn't installed by vibe --install-sbx — upgrade it the way it was installed", p), nil
		}
		return nil, "Docker SBX isn't installed — vibe --install-sbx installs it", nil
	}
	installed, err := sbxinstall.Version(m.sbx)
	if err != nil {
		return nil, "", fmt.Errorf("'%s version' failed: %w", m.sbx, err)
	}
	tag, err := sbxinstall.LatestStableTag()
	if err != nil {
		return nil, "", err
	}
	if !selfupdate.Newer(tag, installed) {
		return nil, fmt.Sprintf("Docker SBX %s is the latest", installed), nil
	}
	return &pendingUpgrade{
		label: fmt.Sprintf("Docker SBX %s → %s", installed, tag),
		run: func() error {
			note("upgrading Docker SBX %s to %s", installed, tag)
			if m.asset == "" {
				if err := m.install("", "", false); err != nil {
					return err
				}
				return reportSbx(m)
			}
			return installSbxRelease(m, tag, false)
		},
	}, fmt.Sprintf("Docker SBX %s is available (you have %s)", tag, installed), nil
}

// upgradeVibe fetches release tag for this machine from GitHub into a
// temporary folder, checks it against the release's checksums, unpacks it
// into another, and runs --install from there — which replaces this vibe
// and upgrades ~/.vibe. -f and --update-strategy are passed on to that
// --install.
func upgradeVibe(tag string, force bool, strategyValue string) error {
	downloads, err := os.MkdirTemp("", "vibe-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(downloads)
	asset := selfupdate.AssetName(runtime.GOOS, runtime.GOARCH)
	status := newStatusLine()
	status.show("Downloading %s", asset)
	zipPath, err := selfupdate.Download(tag, asset, downloads)
	if err != nil {
		status.done("Downloading %s failed", asset)
		return err
	}
	status.done("Downloaded %s and checked it against %s", asset, selfupdate.SumsFile)

	unpacked, err := os.MkdirTemp("", "vibe-upgrade-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(unpacked)
	bundle, err := selfupdate.Unzip(zipPath, unpacked)
	if err != nil {
		return err
	}
	exe := filepath.Join(bundle, "vibe")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}

	args := []string{"--install"}
	if force {
		args = append(args, "--force")
	}
	if strategyValue != "" {
		args = append(args, "--update-strategy", strategyValue)
	}
	note("running %s from the new release", strings.Join(args, " "))
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the new release's --install didn't finish cleanly (see above): %w", err)
	}
	return nil
}

// decideConflict asks the user to settle a file both they and the package
// have changed. It first finds out whether the two merge (upgrade has
// already tried), and offers what that allows: with a clean merge, their
// file beside the merged result; without one, their file beside the
// package's. Taking the package's version wholesale shows those two side by
// side first, to confirm.
func decideConflict(c upgrade.Conflict) (upgrade.Choice, error) {
	path := displayPath(c.Mine)
	for {
		if c.CanMerge {
			note("%s already exists and is different from the package source.", path)
			note("Your changes and the package's merge cleanly — here is your version beside the merged result:")
			showSideBySide("your version", "merged result", c.MineText, c.Merged)
			picked, ok := chooseFrom("What would you like to do?",
				[]string{"keep my version", "use the merged version", "overwrite my version with the updated version"}, 0)
			switch {
			case !ok:
				return 0, fmt.Errorf("aborted — %s and any files after it left as they were", path)
			case picked == 0:
				return upgrade.KeepMine, nil
			case picked == 1:
				return upgrade.TakeMerged, nil
			}
			note("Your version beside the updated package version, which would replace it:")
			showSideBySide("your version", "updated package version", c.MineText, c.TheirsText)
			yes, ok := chooseFrom("Overwrite your version with the updated version?", []string{"yes", "no"}, 1)
			if ok && yes == 0 {
				return upgrade.TakeTheirs, nil
			}
			continue // back to the choice above
		}

		note("%s already exists and is different from the package source,", path)
		note("and can't be merged automatically: %s.", c.Why)
		showSideBySide("your version", "updated package version", c.MineText, c.TheirsText)
		picked, ok := chooseFrom("What would you like to do?",
			[]string{"keep my version", "overwrite my version with the updated version"}, 0)
		switch {
		case !ok:
			return 0, fmt.Errorf("aborted — %s and any files after it left as they were", path)
		case picked == 1:
			return upgrade.TakeTheirs, nil
		default:
			return upgrade.KeepMine, nil
		}
	}
}

// showSideBySide prints two versions of a file side by side, as wide as the
// terminal allows.
func showSideBySide(leftName, rightName string, left, right []byte) {
	color := prompt.IsTerminal(os.Stderr)
	fmt.Fprint(os.Stderr, sidebyside.Render(leftName, rightName, string(left), string(right),
		prompt.Width(os.Stderr, 120), color))
}

// displayPath shortens a path under the home directory to ~/..., the way
// the user thinks of it.
func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return path
}

// installBinary copies the running executable to ~/.local/bin, creating
// that directory if needed, then warns (without failing) if it isn't on
// $PATH — the freshly installed binary would otherwise be invisible to the
// shell with no explanation why. An existing one is replaced without
// asking: installing or upgrading vibe is what --install is for.
func installBinary() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating vibe executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolving vibe executable path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	binDir := filepath.Join(home, ".local", "bin")
	dest := filepath.Join(binDir, filepath.Base(exe))

	if srcInfo, err := os.Stat(exe); err == nil {
		if dstInfo, err := os.Stat(dest); err == nil && os.SameFile(srcInfo, dstInfo) {
			note("  already installed at %s", dest)
			return warnIfNotOnPath(binDir, "vibe")
		}
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	_, statErr := os.Stat(dest)
	replacing := statErr == nil
	if err := replaceFile(exe, dest); err != nil {
		return fmt.Errorf("copying the vibe binary to %s: %w", dest, err)
	}
	if replacing {
		note("  replaced the vibe binary at %s", dest)
	} else {
		note("  copied the vibe binary to %s", dest)
	}
	return warnIfNotOnPath(binDir, "vibe")
}

// replaceFile puts a copy of src at dest, safely even while dest is running
// — another vibe session, say, which an upgrade can't expect to be closed.
// Writing into a running binary fails ("text file busy" on Linux, a sharing
// violation on Windows), so the copy is written beside dest and renamed into
// place. Windows won't rename onto a running executable either, but it will
// rename one out of the way, so the old binary is moved aside first there;
// it is removed if it can be, and otherwise left as dest+".old" to go the
// next time.
func replaceFile(src, dest string) error {
	tmp := dest + ".new"
	if err := fscopy.File(src, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err == nil {
		return nil
	}
	old := dest + ".old"
	os.Remove(old)
	if err := os.Rename(dest, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Rename(old, dest) // put the old one back
		os.Remove(tmp)
		return err
	}
	os.Remove(old)
	return nil
}

// warnIfNotOnPath makes sure dir — where cmd was just installed — is on
// PATH, warning (never failing) when it isn't. On Windows there's one place
// a user's PATH is kept, so it's added there if need be; that, like any
// installer's change to PATH, only reaches terminals opened from now on.
// Elsewhere PATH is whatever the user's shell profile makes it, so vibe
// only says what to add.
func warnIfNotOnPath(dir, cmd string) error {
	if onPath(runtime.GOOS, os.Getenv("PATH"), dir) {
		return nil
	}
	if runtime.GOOS == "windows" {
		if onPath(runtime.GOOS, sbxinstall.PersistentPath(), dir) {
			note("  %s is on your PATH, but only for terminals opened from here on — open a new one to use %s", dir, cmd)
			return nil
		}
		added, err := addToUserPath(dir)
		switch {
		case err == nil && added:
			note("  added %s to your PATH — open a new terminal to use %s", dir, cmd)
			return nil
		case err == nil:
			// Already in the user's PATH, but PersistentPath didn't see it:
			// spelled in a way only the registry check understood.
			note("  %s is on your PATH, but only for terminals opened from here on — open a new one to use %s", dir, cmd)
			return nil
		}
		note("  WARNING: %s is not on your PATH, and adding it failed: %s", dir, err)
		note("  add it for your user, e.g. in PowerShell:")
		note("    [Environment]::SetEnvironmentVariable('Path', \"$([Environment]::GetEnvironmentVariable('Path', 'User'));%s\", 'User')", dir)
		note("  then open a new terminal")
		return nil
	}
	note("  WARNING: %s is not on your PATH", dir)
	note("  add it, e.g. in ~/.bashrc or ~/.zshrc:  export PATH=\"%s:$PATH\"", dir)
	return nil
}

// addToUserPath is sbxinstall.AddToUserPath, as a variable so tests never
// touch the real registry.
var addToUserPath = sbxinstall.AddToUserPath

// onPath reports whether dir is one of the entries in pathList. Windows
// paths are case-insensitive, and its PATH entries often carry a trailing
// separator.
func onPath(goos, pathList, dir string) bool {
	dir = filepath.Clean(dir)
	for _, entry := range filepath.SplitList(pathList) {
		if entry == "" {
			continue
		}
		entry = filepath.Clean(entry)
		if entry == dir || (goos == "windows" && strings.EqualFold(entry, dir)) {
			return true
		}
	}
	return false
}

// --- kit loading ----------------------------------------------------------

func loadKit(lay layout.Layout, profile string) (kitspec.Doc, settings.Settings, error) {
	if !lay.ProfileExists(profile) {
		return nil, settings.Settings{}, fmt.Errorf(
			"unknown profile '%s' — expected %s/config.yaml", profile, lay.ProfileDir(profile))
	}
	profileDir := lay.ProfileDir(profile)

	baseSettings, err := settings.Load(lay.File("settings.yaml"))
	if err != nil {
		return nil, settings.Settings{}, err
	}
	profileSettings, err := settings.Load(filepath.Join(profileDir, "settings.yaml"))
	if err != nil {
		return nil, settings.Settings{}, err
	}
	merged := settings.Merge(baseSettings, profileSettings)
	merged.NugetDir = expandHome(merged.NugetDir)
	merged.MemoryRoot = expandHome(merged.MemoryRoot)

	baseConfig, err := kitspec.LoadDoc(lay.File("config.yaml"))
	if err != nil {
		return nil, settings.Settings{}, err
	}
	profileConfig, err := kitspec.LoadDoc(filepath.Join(profileDir, "config.yaml"))
	if err != nil {
		return nil, settings.Settings{}, err
	}

	var agentInstructions string
	if data, err := os.ReadFile(filepath.Join(profileDir, "agent-instructions.md")); err == nil {
		agentInstructions = string(data)
	} else if !os.IsNotExist(err) {
		return nil, settings.Settings{}, err
	}

	doc, err := kitspec.Build(baseConfig, profileConfig, lay.DefaultsDir(), profileDir, agentInstructions)
	if err != nil {
		return nil, settings.Settings{}, err
	}
	return doc, merged, nil
}

func writeKit(doc kitspec.Doc, name string) (string, error) {
	dir, err := os.MkdirTemp("", "vibe-kit-"+name+"-")
	if err != nil {
		return "", err
	}
	data, err := kitspec.Marshal(doc)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.yaml"), data, 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// --- publish port allocation ----------------------------------------------

type publishMapping struct {
	name          string
	containerPort int
	hostPort      int
	urlEnv        string
}

// portSearchSpan is how far above a container port allocatePublish will look
// for a free host port before giving up.
const portSearchSpan = 60

// allocatePublish assigns a host port to every container port the merged
// settings publish. The instance records are the only source of truth for who
// holds what: a sandbox re-claims the ports its own record remembers, so its
// URL survives a re-init, and every port another record claims is excluded
// even when nothing is listening on it — that sandbox may simply be stopped.
// Ports handed out earlier in this same pass are excluded on the same
// grounds, since the sandbox that will bind them does not exist yet.
func allocatePublish(vibeHome, name string, merged settings.Settings) ([]publishMapping, error) {
	instances, err := state.List(vibeHome)
	if err != nil {
		return nil, err
	}
	taken := map[int]bool{}
	remembered := map[int]int{}
	for _, inst := range instances {
		for _, rec := range inst.Publish {
			if inst.Name == name {
				remembered[rec.ContainerPort] = rec.HostPort
				continue
			}
			taken[rec.HostPort] = true
		}
	}

	var mappings []publishMapping
	for _, entry := range merged.Publish {
		for i, cp := range entry.Ports {
			hostPort := remembered[cp]
			if hostPort == 0 || taken[hostPort] || portalloc.InUse(hostPort) {
				hostPort, err = portalloc.FindFree(cp, cp+portSearchSpan, taken)
				if err != nil {
					return nil, err
				}
			}
			taken[hostPort] = true
			urlEnv := ""
			if i == 0 {
				urlEnv = entry.UrlEnv
			}
			mappings = append(mappings, publishMapping{name: entry.Name, containerPort: cp, hostPort: hostPort, urlEnv: urlEnv})
		}
	}
	return mappings, nil
}

// --- create / attach --------------------------------------------------

func doCreateOrAttach(lay layout.Layout, args cliargs.Args, name, target string) error {
	vibeHome := lay.Home
	if sbxrun.Exists(name) {
		note("attaching to existing sandbox '%s'", name)
		inst, found, err := state.Load(vibeHome, name)
		if err != nil {
			return err
		}
		if found {
			if !sbxrun.Reachable(name) {
				for _, rec := range inst.Publish {
					if portalloc.InUse(rec.HostPort) {
						reportPortHolder(rec.HostPort)
						return fmt.Errorf("host port %d is in use, and sandbox '%s' is stopped.\n"+
							"sbx would prompt for an alternative port and lose your stable URL.\n"+
							"free the port, then re-run", rec.HostPort, name)
					}
				}
			}
			for _, rec := range inst.Publish {
				note("  %s: http://localhost:%d", rec.Name, rec.HostPort)
			}
		}
		return finish(vibeHome, name)
	}

	profile := resolveProfileName(args, target)
	if err := ensureProfile(lay, profile, args.Force); err != nil {
		return err
	}
	doc, merged, err := loadKit(lay, profile)
	if err != nil {
		return err
	}
	if agentOf(merged) == "claude" {
		if doc, err = setClaudeTheme(lay, profile, doc, "", true, args.Force); err != nil {
			return err
		}
	}
	if err := createSandbox(vibeHome, name, target, profile, doc, merged, !args.Force, false, ""); err != nil {
		return err
	}
	return finish(vibeHome, name)
}

func agentOf(merged settings.Settings) string {
	if merged.Agent == "" {
		return "claude"
	}
	return merged.Agent
}

// setClaudeTheme settles which Claude theme a sandbox about to be created
// starts with, sets it in doc, and records it in the profile's directory.
//
// The preference comes from the sandbox being replaced (fromSandbox, read
// before it was removed — the truest answer, since /theme may have changed
// it since), else from what the profile last recorded. ask says to ask
// anyway, starting on that preference: a brand new sandbox always asks,
// while a rebuild asks only when nothing is known. -f, or no terminal,
// takes the preference (or the default) without asking.
func setClaudeTheme(lay layout.Layout, profile string, doc kitspec.Doc, fromSandbox string, ask, force bool) (kitspec.Doc, error) {
	// ~/.vibe/defaults (or, without one, the bundle's) is the source of
	// truth for which themes are offered.
	known := claudetheme.Available(lay.DefaultsDir())
	profileDir := lay.ProfileDir(profile)

	preferred, source := fromSandbox, "the sandbox being replaced"
	if preferred == "" {
		if c, ok := claudetheme.FromProfile(profileDir); ok {
			preferred, source = c, "profile '"+profile+"'"
		}
	}
	choice := claudetheme.Default
	if preferred != "" {
		resolved, ok := claudetheme.Resolve(preferred, known)
		if !ok {
			note("  claude theme '%s' (from %s) isn't among the themes in %s — using the default",
				preferred, source, displayPath(filepath.Join(lay.DefaultsDir(), "agent-files", ".claude", "themes")))
		}
		choice = resolved
	}

	choices := claudetheme.Choices(known)
	if indexOf(choices, choice) < 0 {
		// A built-in theme the sandbox was switched to: offer to keep it.
		choices = append([]string{choices[0], choice}, choices[1:]...)
	}
	switch {
	case (ask || preferred == "") && len(choices) > 1 && !force && interactive():
		picked, ok := chooseFrom("select claude theme", choices, indexOf(choices, choice))
		if !ok {
			return nil, fmt.Errorf("aborted — no theme chosen")
		}
		choice = choices[picked]
	case preferred != "":
		note("  claude theme: %s (from %s)", choice, source)
	}

	doc, err := claudetheme.Apply(doc, choice)
	if err != nil {
		return nil, err
	}
	if err := claudetheme.SaveToProfile(profileDir, choice); err != nil {
		note("  WARNING: could not record the claude theme in %s: %s", profileDir, err)
	}
	return doc, nil
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// askMemory offers a choice of how much memory the sandbox gets, starting
// on the memory setting; without it, that setting is used as long as this
// machine can give it.
func createSandbox(vibeHome, name, target, profile string, doc kitspec.Doc, merged settings.Settings, askMemory, restoreAfterCreate bool, memoryStore string) error {
	kitDir, err := writeKit(doc, name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(kitDir)

	agent := merged.Agent
	if agent == "" {
		agent = "claude"
	}
	memSize := merged.Memory
	if memSize == "" {
		memSize = "12g"
	}
	memSize = pickMemory(memSize, askMemory && interactive(), fmt.Sprintf("how much memory should sandbox '%s' get?", name))

	note("creating sandbox '%s'", name)
	note("  workspace: %s", target)
	note("  memory:    %s", memSize)

	var mounts []string
	var env []string

	if merged.NugetDir != "" {
		if info, err := os.Stat(merged.NugetDir); err == nil && info.IsDir() {
			mounts = append(mounts, merged.NugetDir)
			env = append(env, "SBX_HOST_NUGET="+merged.NugetDir)
			note("  nuget:     %s (shared with host)", merged.NugetDir)
		} else {
			note("  nuget:     %s not found on host — not mounting", merged.NugetDir)
		}
	}

	if agentmem.Supported(agent) {
		root := merged.MemoryRoot
		if root == "" {
			root = filepath.Join(vibeHome, "memories")
		}
		if memoryStore == "" {
			memoryStore = agentmem.StoreFor(root, name)
		}
		if err := os.MkdirAll(memoryStore, 0o755); err != nil {
			return err
		}
		mounts = append(mounts, memoryStore)
		env = append(env, "VIBE_MEMORY_STORE="+memoryStore)
		note("  memories:  %s", memoryStore)
	}

	publishMappings, err := allocatePublish(vibeHome, name, merged)
	if err != nil {
		return err
	}
	var publishArgs []string
	var publishRecords []state.PublishRecord
	for _, m := range publishMappings {
		publishArgs = append(publishArgs, fmt.Sprintf("%d:%d", m.hostPort, m.containerPort))
		publishRecords = append(publishRecords, state.PublishRecord{Name: m.name, ContainerPort: m.containerPort, HostPort: m.hostPort})
		if m.urlEnv != "" {
			env = append(env, fmt.Sprintf("%s=http://localhost:%d", m.urlEnv, m.hostPort))
		}
		note("  %s: http://localhost:%d", m.name, m.hostPort)
	}

	envKeys := make([]string, 0, len(merged.Env))
	for k := range merged.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		env = append(env, k+"="+merged.Env[k])
	}

	if err := sbxrun.Create(sbxrun.CreateOpts{
		Name:    name,
		KitDir:  kitDir,
		Memory:  memSize,
		Publish: publishArgs,
		Env:     env,
		Agent:   agent,
		Target:  target,
		Mounts:  mounts,
	}); err != nil {
		warnIfClockIsOff()
		return err
	}

	if restoreAfterCreate {
		if err := agentmem.Restore(name, memoryStore); err != nil {
			note("WARNING: %s", err)
		}
	}

	if err := state.Save(vibeHome, state.Instance{
		Name: name, Profile: profile, Target: target, Publish: publishRecords, MemoryStore: memoryStore, CreatedAt: time.Now(),
	}); err != nil {
		return err
	}
	return nil
}

// --- re-init --------------------------------------------------------------

// resolveReInitProfile returns the profile a re-init (or re-create) should
// use: the one explicitly given, else the one already recorded for an
// existing sandbox of this name, else the one a plain run would derive.
func resolveReInitProfile(vibeHome string, args cliargs.Args, name, target string) (string, error) {
	if args.Profile != "" {
		return args.Profile, nil
	}
	inst, found, err := state.Load(vibeHome, name)
	if err != nil {
		return "", err
	}
	if found {
		return inst.Profile, nil
	}
	return resolveProfileName(args, target), nil
}

func doReInit(lay layout.Layout, args cliargs.Args, name, target string) error {
	return reInit(lay, args, name, target, false)
}

// reInit is --re-init's implementation. skipSandboxConfirm is set by
// doReCreate, which already got one confirmation up front covering both
// the profile and the sandbox, so this must not ask about the sandbox a
// second time.
func reInit(lay layout.Layout, args cliargs.Args, name, target string, skipSandboxConfirm bool) error {
	vibeHome := lay.Home
	profile, err := resolveReInitProfile(vibeHome, args, name, target)
	if err != nil {
		return err
	}

	if err := ensureProfile(lay, profile, args.Force); err != nil {
		return err
	}
	doc, merged, err := loadKit(lay, profile)
	if err != nil {
		return err
	}
	agent := agentOf(merged)

	restoreAfterCreate := false
	var memoryStore string
	var themeFromSandbox string

	if sbxrun.Exists(name) {
		if !skipSandboxConfirm && !confirmDefault(args.Force, true, fmt.Sprintf("Remove sandbox '%s' (workspace %s)?", name, target)) {
			return fmt.Errorf("aborted — sandbox left alone")
		}
		if agentmem.Supported(agent) {
			// Memories can only be read out of a running sandbox, and the
			// one being replaced is often stopped. Since removing it is
			// already agreed, boot it just long enough to ask — otherwise a
			// stopped sandbox's memories would be reported as absent and
			// then destroyed along with it.
			presence := agentmem.Probe(name)
			if presence == agentmem.Unknown {
				note("  '%s' is not running — starting it to check for memories", name)
				if sbxrun.Start(name) {
					presence = agentmem.Probe(name)
				}
			}
			switch {
			case presence == agentmem.Unknown:
				note("  WARNING: could not start '%s' to check for memories — any it holds will go with it", name)
			case presence == agentmem.None:
				note("  no memories found in '%s' — nothing to preserve", name)
			case confirmDefault(args.Force, true, fmt.Sprintf("Preserve agent memories from '%s'?", name)):
				root := merged.MemoryRoot
				if root == "" {
					root = filepath.Join(vibeHome, "memories")
				}
				memoryStore = agentmem.StoreFor(root, name)
				ok, err := agentmem.Backup(name, memoryStore)
				if err != nil {
					note("  WARNING: %s", err)
				} else if !ok {
					note("  no memories found in '%s' — nothing to preserve", name)
				} else {
					note("  memories saved to %s", memoryStore)
					restoreAfterCreate = true
				}
			}
		}
		// Read after the memories: checking for those will have started a
		// stopped sandbox, which is the only kind that can be asked.
		if agent == "claude" && sbxrun.Reachable(name) {
			if c, ok := claudetheme.FromSandbox(name); ok {
				themeFromSandbox = c
			}
		}
		note("removing sandbox '%s'", name)
		if err := sbxrun.Remove(name, true); err != nil {
			return err
		}
		// The instance record deliberately outlives the sandbox: it carries
		// the published ports this name already owns, which is what lets the
		// rebuild below re-claim them and keep the URL stable. createSandbox
		// overwrites it once the new sandbox is up.
	} else {
		note("no existing sandbox '%s' — nothing to remove", name)
	}

	if agent == "claude" {
		if doc, err = setClaudeTheme(lay, profile, doc, themeFromSandbox, false, args.Force); err != nil {
			return err
		}
	}

	note("rebuilding '%s' from profile '%s'", name, profile)
	if err := createSandbox(vibeHome, name, target, profile, doc, merged, false, restoreAfterCreate, memoryStore); err != nil {
		return err
	}
	return finish(vibeHome, name)
}

// --- re-create --------------------------------------------------------------

// doReCreate deletes the overlay copy of the matched profile (if there is
// one — the bundle's own copy, if any, is never touched), then does
// exactly what --re-init does: with the profile gone, ensureProfile guides
// the user through creating a new one (copy, blank or guided) instead of
// silently reusing the old one, and any existing sandbox is removed and
// rebuilt from it. Both destructive steps share a single confirmation up
// front rather than asking once per step.
func doReCreate(lay layout.Layout, args cliargs.Args, name, target string) error {
	profile, err := resolveReInitProfile(lay.Home, args, name, target)
	if err != nil {
		return err
	}

	if !confirmDefault(args.Force, false,
		"This action will destroy any existing profile or sandbox for the current project. Continue?") {
		return fmt.Errorf("aborted — nothing removed")
	}

	dir := lay.HomePath("profiles", profile)
	if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		note("deleted profile '%s' from %s", profile, dir)
	} else {
		note("no overlay profile '%s' to delete", profile)
	}

	return reInit(lay, args, name, target, true)
}

// --- delete -----------------------------------------------------------------

// deletion is what --delete can remove for one folder: its sandbox, when sbx
// has it, and its profile, when ~/.vibe has a copy to delete — a profile that
// only ships with vibe would come straight back with the next upgrade.
type deletion struct {
	name       string
	sandbox    bool   // sbx has a sandbox called name
	profile    string // the profile name was (or would be) built from
	profileDir string // the overlay copy of profile; empty when there is none
	sharedWith []string
}

// options lists what d offers, as checklist labels with whether each starts
// ticked, and which it is: the sandbox, ticked, since a re-run of vibe
// rebuilds it; the profile, not, since that is work that can't be redone
// as easily.
func (d deletion) options() (labels []string, checked []bool, isSandbox []bool) {
	if d.sandbox {
		labels = append(labels, fmt.Sprintf("remove the sandbox %s", d.name))
		checked = append(checked, true)
		isSandbox = append(isSandbox, true)
	}
	if d.profileDir != "" {
		label := fmt.Sprintf("remove the profile %s", d.profile)
		if len(d.sharedWith) > 0 {
			label += fmt.Sprintf(" (also used by %s)", strings.Join(d.sharedWith, ", "))
		}
		labels = append(labels, label)
		checked = append(checked, false)
		isSandbox = append(isSandbox, false)
	}
	return labels, checked, isSandbox
}

// pickDeletions asks which of labels to delete, starting from checked,
// returning the indices picked; ok is false when the user quit. A variable
// so tests can stand in for the user.
var pickDeletions = func(labels []string, checked []bool) ([]int, bool) {
	return checklistFrom("Check what to delete (this cannot be undone):", labels, checked)
}

// choose settles what to delete: everything d offers with -f, else what
// the user ticks.
func (d deletion) choose(force bool) (sandbox, profile bool, err error) {
	labels, checked, isSandbox := d.options()
	if len(labels) == 0 {
		return false, false, nil
	}
	if force {
		return d.sandbox, d.profileDir != "", nil
	}
	if !interactive() {
		return false, false, fmt.Errorf("--delete needs a terminal to confirm on, or -f to delete the sandbox and its profile unasked")
	}
	picked, ok := pickDeletions(labels, checked)
	if !ok {
		return false, false, fmt.Errorf("aborted — nothing deleted")
	}
	for _, i := range picked {
		if isSandbox[i] {
			sandbox = true
		} else {
			profile = true
		}
	}
	return sandbox, profile, nil
}

// run deletes the sandbox and/or profile, saying what it removed and what it
// left. The sandbox's instance record goes with it, as with --cleanup, so
// it doesn't hold on to its published ports. remove is sbxrun.Remove in
// production, and a stub under test.
func (d deletion) run(vibeHome string, sandbox, profile bool, remove func(name string, force bool) error) error {
	if sandbox {
		note("removing sandbox '%s'", d.name)
		// Force: the user has just ticked it, and doesn't want to be told
		// it is running.
		if err := remove(d.name, true); err != nil {
			return fmt.Errorf("could not remove sandbox '%s': %w", d.name, err)
		}
		if err := state.Remove(vibeHome, d.name); err != nil {
			note("WARNING: %s", err)
		}
		note("removed sandbox '%s'", d.name)
	} else if d.sandbox {
		note("kept sandbox '%s'", d.name)
	}
	if profile {
		if err := os.RemoveAll(d.profileDir); err != nil {
			return fmt.Errorf("could not delete profile '%s': %w", d.profile, err)
		}
		note("deleted profile '%s' from %s", d.profile, displayPath(d.profileDir))
	} else if d.profileDir != "" {
		note("kept profile '%s'", d.profile)
	}
	return nil
}

// doDelete deletes the sandbox for target, its profile, or both, as picked
// from a checklist — or both with -f.
func doDelete(lay layout.Layout, args cliargs.Args, name, target string) error {
	profile, err := resolveReInitProfile(lay.Home, args, name, target)
	if err != nil {
		return err
	}
	d := deletion{name: name, sandbox: sbxrun.Exists(name), profile: profile}
	if dir := lay.HomePath("profiles", profile); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			d.profileDir = dir
		}
	}
	if d.profileDir == "" {
		if info, err := os.Stat(lay.BundlePath("profiles", profile)); err == nil && info.IsDir() {
			note("profile '%s' ships with vibe, so it isn't offered for deletion", profile)
		}
	}
	instances, err := state.List(lay.Home)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if inst.Profile == profile && inst.Name != name {
			d.sharedWith = append(d.sharedWith, inst.Name)
		}
	}

	sandbox, prof, err := d.choose(args.Force)
	if err != nil {
		return err
	}
	if !sandbox && !prof {
		if !d.sandbox && d.profileDir == "" {
			note("no sandbox '%s' and no profile '%s' in %s — nothing to delete", name, profile, displayPath(lay.HomePath("profiles")))
		} else {
			note("nothing checked — nothing deleted")
		}
		return nil
	}
	return d.run(lay.Home, sandbox, prof, sbxrun.Remove)
}

// --- re-compose -------------------------------------------------------------

// doReCompose rebuilds a guided profile from the library features it records
// having been composed from, then re-inits its sandbox so the result is
// actually in effect. It is the answer to a feature gaining something after
// the profiles built from it were generated: composing happens once, at
// creation, and nothing reads the library again afterwards — so a profile
// generated last week carries last week's version of its features, whatever
// library/ says today.
//
// The profile is regenerated rather than merged into: re-running the
// composition over the existing files would append each feature's fragments
// a second time, duplicating its permissions, its published ports and its
// instructions. That means hand-edits to the profile are lost, so this asks
// first.
func doReCompose(lay layout.Layout, args cliargs.Args, name, target string) error {
	profile, err := resolveReInitProfile(lay.Home, args, name, target)
	if err != nil {
		return err
	}
	if !lay.ProfileExists(profile) {
		return fmt.Errorf("no profile '%s' to re-compose — run vibe -R to create one", profile)
	}
	features := profilegen.ComposedFrom(lay.ProfileDir(profile))
	if len(features) == 0 {
		return fmt.Errorf("profile '%s' does not record which features it was composed from.\n"+
			"Only profiles from guided creation do — add a '# vibe: features: a, b' line to\n"+
			"%s to adopt one, or use vibe -R to build a fresh profile from the library.",
			profile, filepath.Join(lay.ProfileDir(profile), "config.yaml"))
	}
	if err := library.Validate(features, lay.Features()); err != nil {
		return fmt.Errorf("profile '%s' was composed from a feature that is no longer there: %w", profile, err)
	}

	note("profile '%s' was composed from: %s", profile, strings.Join(features, ", "))
	if !confirmDefault(args.Force, false, fmt.Sprintf(
		"Rebuild profile '%s' from those features (losing any edits to it) and re-init its sandbox?", profile)) {
		return fmt.Errorf("aborted — nothing changed")
	}

	// The recorded theme isn't something the features produce, so it would
	// go with the rest of the old profile; carry it across.
	theme, hadTheme := claudetheme.FromProfile(lay.ProfileDir(profile))
	dir := lay.HomePath("profiles", profile)
	if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	created, err := profilegen.CreateGuided(lay, profile, features)
	if err != nil {
		return err
	}
	if hadTheme {
		if err := claudetheme.SaveToProfile(created, theme); err != nil {
			note("  WARNING: could not carry the claude theme across: %s", err)
		}
	}
	note("re-composed profile '%s' (%s) in %s", profile, strings.Join(features, ", "), created)

	return reInit(lay, args, name, target, true)
}

// openTerminal and openTerminalInput are how vibe reaches the terminal to
// ask a question on. They are variables so the tests can take the terminal
// away: whether one is there otherwise depends on how the tests were run — a
// Windows CI runner has a console, and a question asked on it waits forever.
var (
	openTerminal      = prompt.Open
	openTerminalInput = prompt.OpenInput
)

// ttyReader opens whatever this process can ask a question on: the
// terminal itself (/dev/tty, or the Windows console), so a redirected stdin
// can't silently answer for the user. The returned close function must be
// called once the answer has been read; ok is false when there is no
// terminal at all.
func ttyReader() (reader *bufio.Reader, closeFn func(), ok bool) {
	in, closeFn, ok := openTerminalInput()
	if !ok {
		return nil, closeFn, false
	}
	return bufio.NewReader(in), closeFn, true
}

// confirm asks a yes/no question on the terminal, defaulting to no. force
// answers yes without asking.
func confirm(force bool, question string) bool {
	return confirmDefault(force, false, question)
}

// confirmDefault asks a yes/no question on the terminal, answering def when
// the user just presses enter. force answers yes without asking.
func confirmDefault(force, def bool, question string) bool {
	if force {
		return true
	}
	reader, closeFn, ok := ttyReader()
	if !ok {
		note("no terminal to confirm on — re-run with -f")
		os.Exit(1)
	}
	defer closeFn()
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	fmt.Fprintf(os.Stderr, "vibe: %s [%s] ", question, hint)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	switch line {
	case "":
		return def
	case "y", "yes":
		return true
	default:
		return false
	}
}

// interactive reports whether there is a terminal to ask questions on.
func interactive() bool {
	_, closeFn, ok := ttyReader()
	closeFn()
	return ok
}

// ttyRW opens the same terminal ttyReader would, for reading keys and
// drawing — what an interactive list picker needs to put the terminal into
// raw mode and redraw itself in place.
func ttyRW() (tty *prompt.Terminal, closeFn func(), ok bool) {
	tty, ok = openTerminal()
	if !ok {
		return nil, func() {}, false
	}
	return tty, tty.Close, true
}

// chooseFrom asks the user to pick one of labels with an interactive,
// arrow-key-navigable list (inquirer-style). def is the 0-based index
// highlighted first. It returns the chosen index, and ok=false when the
// user quit or there is no terminal to ask on.
//
// Falling back to numbered/typed input when Select can't put the terminal
// into raw mode is deliberately not attempted: interactive() already gates
// every chooseFrom call site on there being a real terminal, and a terminal
// that can't go raw is rare enough (and unhelpful enough for scripting) not
// to be worth a second input mode.
func chooseFrom(question string, labels []string, def int) (int, bool) {
	tty, closeFn, ok := ttyRW()
	if !ok {
		return 0, false
	}
	defer closeFn()
	note("%s", question)
	return prompt.Select(tty, labels, def)
}

// initHome seeds the user's ~/.vibe overlay on first run: it is where the
// master copy of vibe's configuration is meant to live, so the bundle's base
// config is copied there to be edited, and the bundled profiles are offered
// one at a time for the user to take their own copy of.
func initHome(lay layout.Layout, force bool) error {
	if !homeinit.Needed(lay) {
		return nil
	}
	note("first run — setting up %s", lay.Home)

	var ask func(string) bool
	switch {
	case force || interactive():
		ask = func(profile string) bool {
			return confirm(force, fmt.Sprintf("copy profile '%s' into %s?",
				profile, filepath.Join(lay.Home, "profiles")))
		}
	default:
		note("  no terminal to ask on — leaving the bundled profiles where they are")
	}

	res, err := homeinit.Run(lay, ask)
	if err != nil {
		return err
	}
	if len(res.Files) > 0 {
		note("  copied %s", strings.Join(res.Files, ", "))
	}
	for _, profile := range res.Profiles {
		note("  copied profile '%s'", profile)
	}
	// What was just copied is the package's version, so it is also the
	// original the next vibe --install merges against.
	if err := upgrade.SeedBase(lay.Bundle, lay.Home); err != nil {
		note("  WARNING: could not record the package's files in %s: %s",
			filepath.Join(lay.Home, upgrade.BaseDir), err)
	}
	if indexOf(res.Files, "settings.yaml") >= 0 {
		if err := setUpSettings(lay, !force); err != nil {
			return err
		}
	}
	note("  %s now overrides the bundle at %s — edit it, not the bundle", lay.Home, lay.Bundle)
	return nil
}

// ensureProfile makes sure the profile a sandbox is about to be built from
// exists. A folder vibe hasn't seen before derives a profile name that has
// no profile behind it yet, so rather than failing, offer to seed one: a
// copy of an existing profile, a blank one, or a guided walk through
// picking library features.
func ensureProfile(lay layout.Layout, profile string, force bool) error {
	if lay.ProfileExists(profile) {
		return nil
	}
	if err := profilegen.Validate(profile); err != nil {
		return err
	}
	existing := lay.Profiles()
	note("no profile '%s' yet in %s", profile, filepath.Dir(lay.NewProfileDir(profile)))

	source := ""
	switch {
	case len(existing) == 0:
		note("there is no profile to copy — creating a blank profile '%s'", profile)
	case force:
		note("--force given — creating a blank profile '%s'", profile)
	default:
		if !interactive() {
			return fmt.Errorf("no terminal to ask on — re-run with -p <profile> to use an existing one (%s), "+
				"with -f to create a blank profile, or write %s by hand",
				strings.Join(existing, ", "),
				filepath.Join(lay.NewProfileDir(profile), "config.yaml"))
		}
		choice, ok := chooseFrom("create it how?",
			[]string{"copy an existing profile", "create a new blank profile", "guided profile creation"}, 0)
		if !ok {
			return fmt.Errorf("aborted — no profile '%s' created", profile)
		}
		switch choice {
		case 0:
			pick, ok := chooseFrom(fmt.Sprintf("which profile should '%s' start from?", profile), existing, 0)
			if !ok {
				return fmt.Errorf("aborted — no profile '%s' created", profile)
			}
			source = existing[pick]
		case 2:
			return ensureGuidedProfile(lay, profile)
		}
	}

	var dir string
	var err error
	if source != "" {
		dir, err = profilegen.CopyFrom(lay, source, profile)
	} else {
		dir, err = profilegen.CreateBlank(lay, profile)
	}
	if err != nil {
		return err
	}
	if source != "" {
		note("created profile '%s' as a copy of '%s' in %s", profile, source, dir)
	} else {
		note("created blank profile '%s' in %s", profile, dir)
	}
	return nil
}

// ensureGuidedProfile walks the user through picking which library
// features a new profile should install, lets them reorder the ones they
// picked, and writes the result as a new profile via profilegen.CreateGuided.
func ensureGuidedProfile(lay layout.Layout, profile string) error {
	names := lay.Features()
	if len(names) == 0 {
		return fmt.Errorf("no library features found — pick another option instead")
	}
	settingsPath := lay.File("settings.yaml")
	base, err := settings.Load(settingsPath)
	if err != nil {
		return err
	}
	// A defaultFeatures entry naming a feature that isn't there is a
	// mistake worth stopping for: carrying on would quietly build a
	// sandbox without the tooling it was told to always start with.
	if err := library.Validate(base.DefaultFeatures, names); err != nil {
		return fmt.Errorf("defaultFeatures in %s: %w", settingsPath, err)
	}

	features, labels, checked := featureChecklist(lay, names, base.DefaultFeatures)
	if len(base.DefaultFeatures) > 0 {
		note("ticked from defaultFeatures (untick any you don't want): %s", strings.Join(base.DefaultFeatures, ", "))
	}

	idxs, ok := checklistFrom("pick the features this sandbox needs", labels, checked)
	if !ok {
		return fmt.Errorf("aborted — no profile '%s' created", profile)
	}
	chosen := make([]string, len(idxs))
	for i, idx := range idxs {
		chosen[i] = features[idx].Name
	}

	if len(chosen) > 1 {
		ordered, ok := reorderFrom("order the features — they install and start in this order", chosen)
		if !ok {
			return fmt.Errorf("aborted — no profile '%s' created", profile)
		}
		chosen = ordered
	}

	dir, err := profilegen.CreateGuided(lay, profile, chosen)
	if err != nil {
		return err
	}
	if len(chosen) > 0 {
		note("created guided profile '%s' (%s) in %s", profile, strings.Join(chosen, ", "), dir)
	} else {
		note("created guided profile '%s' (no features picked) in %s", profile, dir)
	}
	return nil
}

// featureChecklist lists the library features names for a checkbox list:
// each one's label, and whether it starts ticked — the ones in ticked.
func featureChecklist(lay layout.Layout, names, ticked []string) (features []library.Feature, labels []string, checked []bool) {
	features = library.List(names, lay.FeatureDir)
	labels = make([]string, len(features))
	checked = make([]bool, len(features))
	for i, f := range features {
		labels[i] = f.Label()
		checked[i] = indexOf(ticked, f.Name) >= 0
	}
	return features, labels, checked
}

// --- settings.yaml ---------------------------------------------------------

// fallbackAgents are offered when sbx can't be asked which agents it runs —
// as on a first --install, which can come before --install-sbx. The list is
// sbx v0.46.0's.
var fallbackAgents = []string{"claude", "codex", "copilot", "cursor", "devin", "docker-agent", "droid", "gemini", "kiro", "opencode", "shell"}

// setUpSettings writes ~/.vibe/settings.yaml for this machine, from the
// copy just put there (the package's). With ask and a terminal, it asks how
// much memory sandboxes get, which agent they run, and which features a
// guided profile starts with ticked, starting on the package's choices;
// otherwise it keeps those, with the memory lowered to what this machine
// can give. Everything else in the file is left as it is.
func setUpSettings(lay layout.Layout, ask bool) error {
	path := lay.HomePath("settings.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // the package ships none, so there's nothing to set up
	}
	if err != nil {
		return err
	}
	current, err := settings.Parse(data)
	if err != nil {
		note("  WARNING: can't set up %s, which doesn't parse: %s", displayPath(path), err)
		return nil
	}
	ask = ask && interactive()
	if ask {
		note("setting up %s: the defaults every sandbox starts from", displayPath(path))
	}

	values := []struct {
		key   string
		value interface{}
	}{
		{"memory", pickMemory(current.Memory, ask, "how much memory should each sandbox get? (you're asked again for each new sandbox)")},
		{"agent", pickAgent(current.Agent, ask)},
	}
	if ask {
		features, err := pickDefaultFeatures(lay, current.DefaultFeatures)
		if err != nil {
			return err
		}
		values = append(values, struct {
			key   string
			value interface{}
		}{"defaultFeatures", features})
	}

	changed := data
	for _, v := range values {
		if changed, err = settings.Set(changed, v.key, v.value); err != nil {
			return fmt.Errorf("updating %s: %w", path, err)
		}
	}
	if bytes.Equal(changed, data) {
		return nil
	}
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		return err
	}
	note("  saved your choices in %s", displayPath(path))
	return nil
}

// lowerMemorySetting lowers the memory setting in the settings.yaml at path
// to what this machine can give a sandbox, when it's more than that.
func lowerMemorySetting(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	current, err := settings.Parse(data)
	if err != nil || current.Memory == "" {
		return err
	}
	memory := pickMemory(current.Memory, false, "")
	if memory == current.Memory {
		return nil
	}
	if data, err = settings.Set(data, "memory", memory); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	note("  saved memory: %s in %s", memory, displayPath(path))
	return nil
}

// pickMemory settles how much memory a sandbox gets, starting from want:
// with ask, the user picks from 4g steps up to half this machine's memory
// (see hostmem.Options); without, want is kept unless it's more than that,
// when it's lowered to the largest step. A machine whose memory can't be
// read keeps want as it is.
func pickMemory(want string, ask bool, question string) string {
	total, err := hostmem.Total()
	if err != nil {
		note("  WARNING: can't tell how much memory this machine has (%s) — using memory: %s", err, want)
		return want
	}
	options, idx, over := hostmem.Pick(total, want)
	host := hostmem.Format(2 * hostmem.Limit(total))
	switch {
	case over:
		note("  memory: %s is more than half this machine's %s — lowering it to %s", want, host, options[idx])
	case want != "" && options[idx] != want:
		note("  memory: can't read %q as a size — using %s", want, options[idx])
	}
	if !ask {
		return options[idx]
	}
	i, ok := chooseFrom(fmt.Sprintf("%s (this machine has %s)", question, host), options, idx)
	if !ok {
		note("  keeping memory: %s", options[idx])
		return options[idx]
	}
	return options[i]
}

// pickAgent asks which agent sandboxes run, from the ones sbx lists,
// starting on current (claude when unset). Without ask, current is kept.
func pickAgent(current string, ask bool) string {
	if current == "" {
		current = "claude"
	}
	if !ask {
		return current
	}
	agents, err := sbxrun.Agents()
	if err != nil {
		agents = fallbackAgents
	}
	idx := indexOf(agents, current)
	if idx < 0 {
		agents = append([]string{current}, agents...)
		idx = 0
	}
	i, ok := chooseFrom("which agent should sandboxes run?", agents, idx)
	if !ok {
		note("  keeping agent: %s", current)
		return current
	}
	return agents[i]
}

// pickDefaultFeatures asks which library features a guided profile should
// start with ticked, starting with defaults ticked. Quitting keeps
// defaults.
func pickDefaultFeatures(lay layout.Layout, defaults []string) ([]string, error) {
	names := lay.Features()
	if len(names) == 0 {
		return defaults, nil
	}
	if err := library.Validate(defaults, names); err != nil {
		return nil, fmt.Errorf("defaultFeatures in %s: %w", displayPath(lay.HomePath("settings.yaml")), err)
	}
	features, labels, checked := featureChecklist(lay, names, defaults)
	idxs, ok := checklistFrom("which features should a guided profile start with ticked?", labels, checked)
	if !ok {
		note("  keeping defaultFeatures: %s", strings.Join(defaults, ", "))
		return defaults, nil
	}
	chosen := make([]string, len(idxs))
	for i, idx := range idxs {
		chosen[i] = features[idx].Name
	}
	return chosen, nil
}

// checklistFrom asks the user to check zero or more of labels with an
// interactive checkbox list. checked is the initial checked state. It
// returns the checked indices, and ok=false when the user quit or there is
// no terminal to ask on.
func checklistFrom(question string, labels []string, checked []bool) ([]int, bool) {
	tty, closeFn, ok := ttyRW()
	if !ok {
		return nil, false
	}
	defer closeFn()
	note("%s", question)
	return prompt.MultiSelect(tty, labels, checked)
}

// checklistNoConfirmFrom is checklistFrom without the step confirming the
// selection: enter answers straight away.
func checklistNoConfirmFrom(question string, labels []string, checked []bool) ([]int, bool) {
	tty, closeFn, ok := ttyRW()
	if !ok {
		return nil, false
	}
	defer closeFn()
	note("%s", question)
	return prompt.MultiSelectNoConfirm(tty, labels, checked)
}

// reorderFrom asks the user to rearrange labels with an interactive,
// arrow-key-movable list. It returns the labels in their final order, and
// ok=false when the user quit or there is no terminal to ask on.
func reorderFrom(question string, labels []string) ([]string, bool) {
	tty, closeFn, ok := ttyRW()
	if !ok {
		return nil, false
	}
	defer closeFn()
	note("%s", question)
	return prompt.Reorder(tty, labels)
}

// --- finishing the foreground session --------------------------------------

// sbxFailureHint is shown when sbx exits non-zero, which is most often a
// sandbox that failed to start.
const sbxFailureHint = "sbx will fail to start if other virtualisation (eg virtualbox) is running - check that you have no such process running"

// virtualBoxProcesses are the names of processes that mean a VirtualBox VM
// is running, and so holding the CPU virtualisation sbx needs.
var virtualBoxProcesses = []string{"VBoxHeadless", "VirtualBoxVM"}

const virtualBoxHint = "You should stop VirtualBox - sbx and virtualbox do not play well together without cpu pinning"

// finish nudges the on-start script (idempotent — safe on every attach, and
// needed because setup.startup does not fire on a sandbox's very first boot,
// before the launcher is on disk) then hands off to `sbx run` in the
// foreground.
func finish(vibeHome, name string) error {
	go nudgeOnStart(vibeHome, name)
	noteUpgrades := checkUpgradesInBackground()
	sessionMu.Lock()
	inSession = true
	sessionMu.Unlock()
	code, err := sbxrun.Run(name, session.StopRequested())
	if err != nil {
		return err
	}
	stopped := false
	select {
	case <-session.StopRequested():
		stopped = true
		note("session ended: asked to stop by another vibe for this folder")
	default:
	}
	// A session we ended ourselves exits non-zero, but its sandbox is fine
	// to copy from; any other non-zero exit means sbx failed, and the
	// sandbox is likely unreachable.
	if code == 0 || stopped {
		saveMemoriesOnExit(vibeHome, name)
	} else {
		note("cannot copy memories: sbx exited with code %d", code)
		note("%s", sbxFailureHint)
		warnIfClockIsOff()
		if len(runningProcesses(runtime.GOOS, virtualBoxProcesses)) > 0 {
			note("%s", virtualBoxHint)
		}
	}
	noteUpgrades()
	session.Release()
	os.Exit(code)
	return nil
}

// warnIfClockIsOff, run when sbx has failed, says so if this machine's clock
// is off the real time: sbx's sign-in to Docker then fails with only "token
// has invalid claims: token is expired" to go on. A check that can't be made
// says nothing — it's only ever a hint.
func warnIfClockIsOff() {
	skew, err := clockskew.Skew()
	if err != nil {
		return
	}
	if w := clockskew.Warning(skew); w != "" {
		note("WARNING: %s", w)
	}
}

// saveMemoriesOnExit copies the agent's memories back out to the sandbox's
// host store once a session ends, so the host copy tracks the sandbox rather
// than only being refreshed by a --re-init.
func saveMemoriesOnExit(vibeHome, name string) {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found {
		return
	}
	store := inst.MemoryStore
	if store == "" {
		// Records written before the store was remembered: the default
		// location is the one such a sandbox will have had mounted.
		store = agentmem.StoreFor(filepath.Join(vibeHome, "memories"), name)
		if info, err := os.Stat(store); err != nil || !info.IsDir() {
			return
		}
	}
	status := newStatusLine()
	snap, err := agentmem.TakeSnapshot(store)
	if err != nil {
		status.done("Memories not backed up: could not first keep a copy of the ones in %s: %s", store, err)
		return
	}

	// Ctrl-C is how a session is left, so it is easily still being pressed
	// once the backup has started. Rather than dying half-way through the
	// copy, vibe asks; quitting puts back what the store held before.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type outcome struct {
		saved bool
		err   error
	}
	finished := make(chan outcome, 1)
	status.show("Backing up memories to %s", store)
	go func() {
		saved, err := agentmem.BackupContext(ctx, name, store)
		finished <- outcome{saved, err}
	}()

	for {
		select {
		case o := <-finished:
			snap.Discard()
			reportMemoryBackup(status, store, o.saved, o.err)
			return
		case <-interrupts:
			if status.tty {
				fmt.Fprint(os.Stderr, "\r\x1b[2K")
			}
			if confirmQuitDuringBackup(interrupts) {
				cancel()
				<-finished
				if err := snap.Restore(); err != nil {
					note("Could not restore the original local memories: %s", err)
					return
				}
				note("Original local memories restored")
				return
			}
			select {
			case o := <-finished:
				snap.Discard()
				if o.saved {
					note("Memories were saved to %s", store)
				} else {
					reportMemoryBackup(status, store, o.saved, o.err)
				}
				return
			default:
				status.show("Backing up memories to %s", store)
			}
		}
	}
}

func reportMemoryBackup(status statusLine, store string, saved bool, err error) {
	switch {
	case err != nil:
		status.done("Backup of memories failed: %s", err)
	case saved:
		status.done("Backed up memories to %s", store)
	default:
		status.done("No memories to back up to %s", store)
	}
}

// confirmQuitDuringBackup asks whether to abandon the memory backup,
// defaulting to no. Further Ctrl-Cs while it waits only ask again: whoever
// is still pressing the key that left the sandbox hasn't read the question
// yet. With no terminal to ask on, the interrupt came from somewhere that
// meant it, and is taken as a yes.
func confirmQuitDuringBackup(interrupts <-chan os.Signal) bool {
	reader, closeFn, ok := ttyReader()
	if !ok {
		return true
	}
	defer closeFn()
	ask := func() {
		fmt.Fprint(os.Stderr, "vibe: vibe is currently exporting memories from the sandbox - are you sure you want to quit? [y/N] ")
	}
	type answer struct {
		line string
		err  error
	}
	answers := make(chan answer, 1)
	read := func() {
		go func() {
			line, err := reader.ReadString('\n')
			answers <- answer{line, err}
		}()
	}

	ask()
	read()
	for {
		select {
		case <-interrupts:
			fmt.Fprintln(os.Stderr)
			ask()
		case a := <-answers:
			if a.err != nil && a.line == "" {
				// A Windows console read ends on Ctrl-C rather than
				// carrying on; if that is what ended this one, ask again.
				select {
				case <-interrupts:
					fmt.Fprintln(os.Stderr)
					ask()
					read()
					continue
				case <-time.After(100 * time.Millisecond):
					return false
				}
			}
			switch strings.ToLower(strings.TrimSpace(a.line)) {
			case "y", "yes":
				return true
			}
			return false
		}
	}
}

// statusLine is a single vibe: line on stderr that is shown while work runs
// and then replaced in place by its outcome. Off a terminal, where the line
// can't be rewritten, the outcome is simply printed on the next line.
type statusLine struct {
	tty bool
}

func newStatusLine() statusLine {
	return statusLine{tty: prompt.IsTerminal(os.Stderr)}
}

func (s statusLine) show(format string, a ...interface{}) {
	if s.tty {
		fmt.Fprintf(os.Stderr, "vibe: "+format, a...)
		return
	}
	note(format, a...)
}

func (s statusLine) done(format string, a ...interface{}) {
	if s.tty {
		fmt.Fprint(os.Stderr, "\r\x1b[2K")
	}
	note(format, a...)
}

func nudgeOnStart(vibeHome, name string) {
	logPath := filepath.Join(os.TempDir(), "vibe-nudge.log")
	logf := func(format string, a ...interface{}) {
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "[%s] %s: "+format+"\n",
			append([]interface{}{time.Now().Format(time.RFC3339), name}, a...)...)
	}
	if !sbxrun.WaitReachable(name, 180) {
		logf("gave up waiting for reachability")
		return
	}
	writePublishedURLs(vibeHome, name, logf)
	if err := sbxrun.ExecDetached(name, "/home/agent/.local/bin/on-start"); err != nil {
		logf("on-start nudge failed: %v", err)
		return
	}
	logf("on-start nudge dispatched")
}

// publishedURLDir is where, inside the sandbox, each published service's
// URL is written at every session start — one file per publish entry,
// named after it (".../published/diffity"), holding the URL the host
// reaches it on (and a second, "unverified: ..." line when sbx couldn't
// confirm it). It is what the sandbox's own helpers (diffity-url) read
// first: unlike the urlEnv variable, fixed when the sandbox was created, it
// is checked against sbx's live port mappings every time.
const publishedURLDir = kitspec.AgentHome + "/.local/state/vibe/published"

// writePublishedURLs writes publishedURLDir for a running sandbox. Each URL
// is built from the host port sbx reports the mapping actually has; the
// port vibe recorded at creation is used only when sbx can't say, and the
// log records which it was.
func writePublishedURLs(vibeHome, name string, logf func(string, ...interface{})) {
	inst, found, err := state.Load(vibeHome, name)
	if err != nil || !found || len(inst.Publish) == 0 {
		return
	}
	live, err := sbxrun.PublishedPorts(name)
	if err != nil {
		logf("could not read live port mappings, using recorded ones: %v", err)
	}
	for _, u := range publishedURLs(inst.Publish, live) {
		switch {
		case u.unverified:
			logf("%s: sbx reports no mapping for %d; using recorded host port %d", u.name, u.containerPort, u.hostPort)
		case u.hostPort != u.recordedPort:
			logf("%s: sbx maps %d to host port %d, not the recorded %d", u.name, u.containerPort, u.hostPort, u.recordedPort)
		}
		if err := sbxrun.WriteFile(name, publishedURLDir+"/"+u.name, u.fileContent()); err != nil {
			logf("%s: %v", u.name, err)
		}
	}
}

type publishedURL struct {
	name                    string
	containerPort, hostPort int
	recordedPort            int
	unverified              bool
}

func (u publishedURL) url() string {
	return fmt.Sprintf("http://localhost:%d", u.hostPort)
}

// fileContent is the URL on the first line, followed — when sbx couldn't
// confirm the port — by a line saying so, which the sandbox's helpers pass
// on as a warning rather than presenting the URL as certain.
func (u publishedURL) fileContent() string {
	if u.unverified {
		return u.url() + "\nunverified: sbx did not report this mapping; this is the port recorded at creation\n"
	}
	return u.url() + "\n"
}

// publishedURLs picks, for each publish entry, the URL of its first port —
// the one urlEnv names — preferring the live mapping over the recorded one.
func publishedURLs(records []state.PublishRecord, live map[int]int) []publishedURL {
	var urls []publishedURL
	seen := map[string]bool{}
	for _, rec := range records {
		if rec.Name == "" || seen[rec.Name] {
			continue
		}
		seen[rec.Name] = true
		u := publishedURL{name: rec.Name, containerPort: rec.ContainerPort, hostPort: rec.HostPort, recordedPort: rec.HostPort}
		if hostPort, ok := live[rec.ContainerPort]; ok {
			u.hostPort = hostPort
		} else {
			u.unverified = true
		}
		urls = append(urls, u)
	}
	return urls
}

func reportPortHolder(port int) {
	note("  processes holding %d:", port)
	for _, out := range portHolders(runtime.GOOS, port) {
		fmt.Fprintln(os.Stderr, out)
	}
}

// portHolders asks whatever this OS offers which processes listen on port.
// Each tool that is present and answers contributes its output; one that
// isn't is skipped quietly, since this is only ever a diagnostic aside.
func portHolders(goos string, port int) []string {
	var outputs []string
	try := func(name string, args ...string) {
		if _, err := exec.LookPath(name); err != nil {
			return
		}
		if out, err := exec.Command(name, args...).CombinedOutput(); err == nil {
			outputs = append(outputs, string(out))
		}
	}
	switch goos {
	case "windows":
		if _, err := exec.LookPath("netstat"); err == nil {
			if out, err := exec.Command("netstat", "-ano", "-p", "TCP").CombinedOutput(); err == nil {
				if lines := netstatListeners(string(out), port); lines != "" {
					outputs = append(outputs, "  Proto  Local Address  Foreign Address  State  PID\n"+lines)
				}
			}
		}
	case "linux":
		try("ss", "-tlnp", fmt.Sprintf("sport = :%d", port))
		try("fuser", "-v", fmt.Sprintf("%d/tcp", port))
	default: // macOS and the BSDs
		try("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN")
	}
	return outputs
}

// runningProcesses returns which of names are running as processes, asking
// whatever this OS offers. Like portHolders it is only a diagnostic aside, so
// a process list it cannot get just finds nothing.
func runningProcesses(goos string, names []string) []string {
	var listed []string
	switch goos {
	case "windows":
		out, err := exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
		if err != nil {
			return nil
		}
		listed = tasklistImages(string(out))
	case "linux":
		// /proc needs no tool; comm is the name truncated to 15 bytes,
		// which is long enough for the names asked about here.
		comms, _ := filepath.Glob("/proc/[0-9]*/comm")
		for _, comm := range comms {
			if b, err := os.ReadFile(comm); err == nil {
				listed = append(listed, strings.TrimSpace(string(b)))
			}
		}
	default: // macOS and the BSDs
		out, err := exec.Command("ps", "-A", "-o", "comm=").Output()
		if err != nil {
			return nil
		}
		listed = strings.Split(string(out), "\n")
	}
	return matchProcesses(listed, names)
}

// tasklistImages picks the image names out of `tasklist /FO CSV /NH`
// output, where each line starts with the quoted image name.
func tasklistImages(out string) []string {
	var images []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `"`) {
			continue
		}
		if end := strings.Index(line[1:], `"`); end >= 0 {
			images = append(images, line[1:end+1])
		}
	}
	return images
}

// matchProcesses returns which of names appear in listed, a list of process
// names or paths (macOS's ps gives the full executable path). Names match
// case-insensitively and ignore a Windows .exe suffix.
func matchProcesses(listed, names []string) []string {
	seen := map[string]bool{}
	for _, p := range listed {
		p = strings.TrimSpace(p)
		if i := strings.LastIndexAny(p, `/\`); i >= 0 {
			p = p[i+1:]
		}
		p = strings.TrimSuffix(strings.ToLower(p), ".exe")
		seen[p] = true
	}
	var found []string
	for _, n := range names {
		if seen[strings.ToLower(n)] {
			found = append(found, n)
		}
	}
	return found
}

// netstatListeners picks, out of `netstat -ano` output, the lines for a
// socket listening on port — its PID is the last field.
func netstatListeners(out string, port int) string {
	suffix := fmt.Sprintf(":%d", port)
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		if strings.HasSuffix(fields[1], suffix) {
			b.WriteString(strings.TrimRight(line, "\r") + "\n")
		}
	}
	return b.String()
}
