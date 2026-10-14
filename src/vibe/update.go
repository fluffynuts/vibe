package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vibe/internal/cliargs"
	"vibe/internal/gittoken"
	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/profilegen"
	"vibe/internal/prompt"
	"vibe/internal/running"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
)

// kitFingerprint fingerprints what a sandbox on profile, running agent, is
// built from: the kit sbx is handed, the agent, the environment and the
// published ports. A sandbox whose recorded fingerprint differs from this
// is out of date. The Claude theme is left out: it's a choice rather than a
// feature, and sandboxes sharing a profile can differ in it.
func kitFingerprint(lay layout.Layout, profile, agent string) (string, error) {
	doc, merged, err := loadKit(lay, profile)
	if err != nil {
		return "", err
	}
	if agent != "claude" {
		kitspec.DropClaudeFiles(doc)
	}
	kitspec.AppendAgentInstructions(doc, gittoken.KitInstructions)
	spec, err := kitspec.Marshal(doc)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "agent=%s\x00", agent)
	keys := make([]string, 0, len(merged.Env))
	for k := range merged.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "env %s=%s\x00", k, merged.Env[k])
	}
	for _, p := range merged.Publish {
		fmt.Fprintf(h, "publish %s %v %s\x00", p.Name, p.Ports, p.UrlEnv)
	}
	h.Write(spec)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// profileState is what bringing a profile up to date came to.
type profileState int

const (
	profileCurrent     profileState = iota // it was up to date
	profileUpdated                         // it was re-composed
	profileNotComposed                     // it isn't one vibe composes, so there's nothing to update
	profileNeedsYou                        // it's out of date in a way only the user can settle
)

// profileOutcome is what bringing a profile up to date came to, and why.
type profileOutcome struct {
	state profileState
	why   string
}

// updateProfile re-composes profile when the library has moved on since it
// was composed, and nobody has edited it since: backed up first, under
// backupName (a sandbox it's for), as 'vibe -C' does. A profile it can't
// re-compose without losing something is left for the user, saying why.
func updateProfile(lay layout.Layout, profile, backupName string) (profileOutcome, error) {
	dir := lay.HomePath("profiles", profile)
	if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
		return profileOutcome{profileNotComposed, "it is vibe's own, not one in " + displayPath(lay.HomePath("profiles"))}, nil
	}
	if err := prepareInstructions(lay, profile); err != nil {
		return profileOutcome{}, err
	}
	if !profilegen.IsGuided(dir) {
		return profileOutcome{profileNotComposed, "it wasn't composed from the library"}, nil
	}
	features := settleBaseFeatures(lay, profile)
	if _, missing := splitMissingFeatures(features, lay.Features()); len(missing) > 0 {
		return profileOutcome{profileNeedsYou, fmt.Sprintf(
			"it has features the library no longer has (%s): 'vibe -C' asks what to do", strings.Join(missing, ", "))}, nil
	}
	fresh, err := profilegen.Check(lay, dir, profile, features)
	if err != nil {
		return profileOutcome{}, err
	}
	switch fresh {
	case profilegen.UpToDate:
		// One from before hashes were recorded, which turned out to be up
		// to date, is adopted as it is.
		if profilegen.RecordedHash(dir) == "" {
			if err := profilegen.RecordHash(dir); err != nil {
				return profileOutcome{}, err
			}
		}
		return profileOutcome{state: profileCurrent}, nil
	case profilegen.Edited:
		return profileOutcome{profileNeedsYou,
			"it was changed by hand since it was composed: 'vibe -C' re-composes it, replacing those changes (backed up first)"}, nil
	}
	note("profile '%s' is out of date: re-composing it from %s", profile, describeFeatures(features))
	if err := replaceProfile(lay, profile, backupName, features); err != nil {
		return profileOutcome{}, err
	}
	return profileOutcome{state: profileUpdated}, nil
}

// sandboxOutOfDate says why the sandbox inst records is out of date, or ""
// when it isn't.
func sandboxOutOfDate(lay layout.Layout, inst state.Instance) (string, error) {
	agent := inst.Agent
	if agent == "" {
		var err error
		if agent, err = defaultAgentFor(lay, inst.Profile); err != nil {
			return "", err
		}
	}
	now, err := kitFingerprint(lay, inst.Profile, agent)
	if err != nil {
		return "", err
	}
	switch inst.KitHash {
	case now:
		return "", nil
	case "":
		return "it was built before vibe kept track of what it was built from", nil
	default:
		return "what it's built from has changed", nil
	}
}

// rebuildCountdown is how many seconds a session start gives the user to
// skip rebuilding an out-of-date sandbox, or to quit.
const rebuildCountdown = 5

// confirmRebuild counts down to rebuilding a sandbox on the terminal; with
// none, it's rebuilt at once. A variable so tests can stand in for the user.
var confirmRebuild = func() prompt.CountdownAnswer {
	tty, closeFn, ok := ttyRW()
	if !ok {
		return prompt.CountdownGo
	}
	defer closeFn()
	answer, _ := prompt.Countdown(tty, func(left int) string {
		return fmt.Sprintf("rebuilding it in %ds — Enter to go now, s to skip this time, Ctrl-C to quit", left)
	}, rebuildCountdown)
	return answer
}

// rebuild rebuilds the sandbox name for target from its profile, without
// asking anything: what bringing it up to date means. A variable for tests.
var rebuild = func(ctx context.Context, lay layout.Layout, name, target string) error {
	return rebuildSandbox(ctx, lay, cliargs.Args{Force: true}, name, target, true, false)
}

// autoUpdate brings the profile and sandbox a session is about to start
// up to date, unless --no-auto-update or 'autoUpdate: false' says not to.
// What it can't update is warned about and the session goes on; it fails
// only when the user quits, or a rebuild it started fails.
func autoUpdate(lay layout.Layout, args cliargs.Args, name, target string) error {
	if args.NoAutoUpdate {
		return nil
	}
	inst, found, err := state.Load(lay.Home, name)
	if err != nil || !found || !lay.ProfileExists(inst.Profile) {
		return err
	}
	merged, err := loadSettings(lay, inst.Profile)
	if err != nil {
		return err
	}
	if merged.AutoUpdate != nil && !*merged.AutoUpdate {
		return nil
	}
	outcome, err := updateProfile(lay, inst.Profile, name)
	if err != nil {
		note("WARNING: couldn't bring profile '%s' up to date, so sandbox '%s' is left as it is: %s", inst.Profile, name, err)
		return nil
	}
	if outcome.state == profileNeedsYou {
		note("WARNING: profile '%s' wasn't brought up to date: %s", inst.Profile, outcome.why)
	}
	why, err := sandboxOutOfDate(lay, inst)
	if err != nil {
		note("WARNING: couldn't tell whether sandbox '%s' is up to date, so it's left as it is: %s", name, err)
		return nil
	}
	if why == "" {
		return nil
	}
	note("sandbox '%s' is out of date: %s", name, why)
	switch confirmRebuild() {
	case prompt.CountdownQuit:
		return fmt.Errorf("quit — sandbox '%s' left as it is ('vibe --no-auto-update' starts it without updating it)", name)
	case prompt.CountdownSkip:
		note("starting sandbox '%s' as it is — 'vibe -r' rebuilds it", name)
		return nil
	}
	note("rebuilding sandbox '%s' to bring it up to date", name)
	return rebuild(context.Background(), lay, name, target)
}

// homeProfiles names the profiles in ~/.vibe, sorted.
func homeProfiles(lay layout.Layout) []string {
	entries, err := os.ReadDir(lay.HomePath("profiles"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && fileExists(filepath.Join(lay.HomePath("profiles"), e.Name(), "config.yaml")) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// updateTally counts what --update-sandboxes did.
type updateTally struct {
	updated, current, skipped int
	failed                    []string
}

// doUpdateSandboxes is --update-sandboxes: every profile in ~/.vibe, then
// every sandbox vibe has a record of, brought up to date. A sandbox with a
// vibe attached is left for its next session start, and each rebuilt one is
// left running or stopped as it was. No session is started.
func doUpdateSandboxes(lay layout.Layout) error {
	instances, err := state.List(lay.Home)
	if err != nil {
		return err
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].Name < instances[j].Name })
	statuses, err := sbxrun.List()
	if err != nil {
		return fmt.Errorf("can't tell which sandboxes sbx has, so nothing was updated: %w", err)
	}
	isRunning := map[string]bool{}
	for _, s := range statuses {
		isRunning[s.Name] = s.Running
	}

	var tally updateTally
	backupFor := map[string]string{}
	for _, inst := range instances {
		if _, taken := backupFor[inst.Profile]; !taken {
			backupFor[inst.Profile] = inst.Name
		}
	}
	for _, profile := range homeProfiles(lay) {
		backupName := backupFor[profile]
		if backupName == "" {
			backupName = profile
		}
		outcome, err := updateProfile(lay, profile, backupName)
		switch {
		case err != nil:
			note("FAILED: profile '%s': %s", profile, err)
			tally.failed = append(tally.failed, "profile '"+profile+"'")
		case outcome.state == profileCurrent:
			note("profile '%s' is up to date", profile)
			tally.current++
		case outcome.state == profileUpdated:
			tally.updated++
		case outcome.state == profileNeedsYou:
			note("WARNING: profile '%s' skipped: %s", profile, outcome.why)
			tally.skipped++
		default:
			note("profile '%s' skipped: %s", profile, outcome.why)
			tally.skipped++
		}
	}

	for _, inst := range instances {
		wasRunning, exists := isRunning[inst.Name]
		switch {
		case !exists:
			note("sandbox '%s' skipped: sbx has no sandbox by that name any more", inst.Name)
			tally.skipped++
			continue
		case folderGone(inst.Target) != "":
			note("sandbox '%s' skipped: %s", inst.Name, folderGone(inst.Target))
			tally.skipped++
			continue
		case len(running.Others(lay.Home, inst.Target)) > 0:
			note("sandbox '%s' skipped: a vibe is attached to it, so it's brought up to date when its next session starts", inst.Name)
			tally.skipped++
			continue
		}
		switch updated, err := updateSandbox(lay, inst, wasRunning); {
		case err != nil:
			note("FAILED: sandbox '%s': %s", inst.Name, err)
			tally.failed = append(tally.failed, "sandbox '"+inst.Name+"'")
		case updated:
			tally.updated++
		default:
			tally.current++
		}
	}
	return tally.report()
}

// report says what was done, failing when anything failed.
func (t updateTally) report() error {
	note("")
	note("updated %d, already up to date %d, skipped %d, failed %d", t.updated, t.current, t.skipped, len(t.failed))
	if len(t.failed) > 0 {
		return fmt.Errorf("couldn't bring %s up to date — see above", strings.Join(t.failed, ", "))
	}
	return nil
}

// updateSandbox rebuilds the sandbox inst records if it's out of date,
// saying whether it was, and leaves it running or stopped as it was. Its
// folder is held meanwhile, so that a vibe started there knows.
func updateSandbox(lay layout.Layout, inst state.Instance, wasRunning bool) (updated bool, err error) {
	why, err := sandboxOutOfDate(lay, inst)
	if err != nil {
		return false, err
	}
	if why == "" {
		note("sandbox '%s' is up to date", inst.Name)
		return false, nil
	}
	note("sandbox '%s' is out of date (%s): rebuilding it", inst.Name, why)
	session, err := running.Register(lay.Home, inst.Target)
	if err != nil {
		return false, err
	}
	defer session.Release()
	if err := rebuild(context.Background(), lay, inst.Name, inst.Target); err != nil {
		return false, err
	}
	if wasRunning {
		if !startRebuilt(inst.Name) {
			return true, fmt.Errorf("rebuilt, but couldn't start it again, as it was running before — 'sbx ls' shows its state")
		}
	} else if err := stopRebuilt(inst.Name); err != nil {
		return true, fmt.Errorf("rebuilt, but couldn't stop it again, as it was stopped before: %w", err)
	}
	return true, nil
}

// startRebuilt and stopRebuilt put a rebuilt sandbox back as it was;
// variables for tests.
var (
	startRebuilt = sbxrun.Start
	stopRebuilt  = sbxrun.StopQuiet
)
