package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"vibe/internal/companion"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/running"
	"vibe/internal/state"
)

// powerExe is the vibe the desk runs: this one. A variable for tests.
var powerExe = os.Executable

// powerDesk is what the page's power menu and Profiles tab stop, restart and
// start vibes with. Each action is the CLI's own command (vibe --stop,
// --restart, --headless), run apart from this vibe, which it may be ending:
// there is one way to do each, and the page only saves going to a terminal.
type powerDesk struct {
	lay layout.Layout
	// claims are the vibes holding a folder: running.Claims when nil.
	claims func(vibeHome, target string) []running.Claim
}

func (d *powerDesk) claimsOf(target string) []running.Claim {
	if d.claims != nil {
		return d.claims(d.lay.Home, target)
	}
	return running.Claims(d.lay.Home, target)
}

// powerLogPath is where the last action on sandbox name wrote what it said:
// the vibe that comes after a restart serves it too, from its first moment.
func powerLogPath(vibeHome, name string) string {
	return filepath.Join(vibeHome, "logs", name+".power.log")
}

func powerPidPath(vibeHome, name string) string {
	return filepath.Join(vibeHome, "logs", name+".power.pid")
}

func (d *powerDesk) Check(name string) companion.PowerCheck {
	check := companion.PowerCheck{Sandbox: name}
	inst, found, err := state.Load(d.lay.Home, name)
	switch {
	case err != nil:
		check.Error = fmt.Sprintf("vibe's record of sandbox '%s' can't be read: %v", name, err)
		return check
	case !found:
		check.Error = fmt.Sprintf("vibe has no record of a sandbox named '%s'", name)
		return check
	}
	target := inst.Target
	check.Folder = pathresolve.Tilde(target)
	claims := d.claimsOf(target)
	interrupts, busy := interruptionsOf(d.lay.Home, name, target, claims)
	check.Actions = map[string]companion.PowerAction{}
	if busy != nil {
		for _, a := range []string{companion.PowerStop, companion.PowerRestart, companion.PowerStart} {
			check.Actions[a] = companion.PowerAction{Why: busy.Error()}
		}
		return check
	}
	attached, terminal := false, false
	for _, c := range sessions(claims) {
		attached = true
		terminal = terminal || !c.Headless
	}
	check.Actions[companion.PowerStop] = companion.PowerAction{OK: true, Warns: interrupts}
	switch {
	case !attached:
		check.Actions[companion.PowerRestart] = companion.PowerAction{Why: "no vibe is running for " + check.Folder + ": Start one"}
	case terminal:
		check.Actions[companion.PowerRestart] = companion.PowerAction{Why: "a vibe here has a terminal session, which a restart from the page can't give back: run 'vibe --restart " + check.Folder + "' in a terminal"}
	default:
		check.Actions[companion.PowerRestart] = companion.PowerAction{OK: true, Warns: interrupts}
	}
	switch {
	case attached:
		check.Actions[companion.PowerStart] = companion.PowerAction{Why: "a vibe is already running for " + check.Folder}
	case !fileExists(target):
		check.Actions[companion.PowerStart] = companion.PowerAction{Why: folderGone(target)}
	default:
		check.Actions[companion.PowerStart] = companion.PowerAction{OK: true}
	}
	return check
}

// powerCommand is the arguments of the vibe command action stands for, on
// sandbox name in target. Stop and restart are forced because the page has
// asked about what they interrupt: there is nobody at a terminal to ask.
func powerCommand(action, name, target string) ([]string, error) {
	var args []string
	switch action {
	case companion.PowerStop:
		args = []string{"--stop", "--force"}
	case companion.PowerRestart:
		args = []string{"--restart", "--force"}
	case companion.PowerStart:
		args = []string{"--headless"}
	default:
		return nil, fmt.Errorf("there is no %q to do to a sandbox from here", action)
	}
	return append(args, "--name", name, "--", target), nil
}

func (d *powerDesk) Do(name, action string) error {
	check := d.Check(name)
	if check.Error != "" {
		return errors.New(check.Error)
	}
	if a, known := check.Actions[action]; !known || !a.OK {
		return fmt.Errorf("can't %s sandbox '%s': %s", action, name, a.Why)
	}
	inst, _, err := state.Load(d.lay.Home, name)
	if err != nil {
		return err
	}
	args, err := powerCommand(action, name, inst.Target)
	if err != nil {
		return err
	}
	exe, err := powerExe()
	if err != nil {
		return err
	}
	logPath := powerLogPath(d.lay.Home, name)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("couldn't start a log for the action: %w", err)
	}
	defer logFile.Close()
	fmt.Fprintf(logFile, "=== vibe %s, started %s ===\n", strings.Join(args, " "), time.Now().Format("2006-01-02 15:04:05"))
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("couldn't run vibe %s: %w", args[0], err)
	}
	pid := cmd.Process.Pid
	// It carries on without this vibe, which it may be ending. Waited for here
	// while this vibe lives, so it isn't left a zombie that still looks to be running.
	go cmd.Wait()
	if err := os.WriteFile(powerPidPath(d.lay.Home, name), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		note("couldn't note the PID of the action (%d): the page can't tell when it ends: %v", pid, err)
	}
	return nil
}

func (d *powerDesk) Log(name string, from int64) (string, int64, bool) {
	file, err := os.Open(powerLogPath(d.lay.Home, name))
	if err != nil {
		return "", 0, false
	}
	defer file.Close()
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return "", 0, false
	}
	// A shorter log is a new action's: from the start.
	if from < 0 || from > size {
		from = 0
	}
	buf := make([]byte, size-from)
	if _, err := file.ReadAt(buf, from); err != nil && err != io.EOF {
		return "", from, false
	}
	return string(buf), size, d.actionRunning(name)
}

func (d *powerDesk) actionRunning(name string) bool {
	data, err := os.ReadFile(powerPidPath(d.lay.Home, name))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	return err == nil && processAlive(pid)
}
