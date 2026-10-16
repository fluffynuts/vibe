package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
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

// profileState is how a profile stands against the library, or what
// bringing it up to date came to.
type profileState int

const (
	profileCurrent         profileState = iota // it's up to date
	profileOutOfDate                           // it isn't, and re-composing it loses nothing of anyone's
	profileUpdated                             // it was re-composed
	profileNotComposed                         // it isn't one vibe composes, so there's nothing to update
	profileEdited                              // it was changed by hand since it was composed: re-composing it is the user's call
	profileMissingFeatures                     // it records features the library no longer has: the user must say what to do
)

// needsYou is a profile that's out of date in a way only the user can
// settle.
func (o profileOutcome) needsYou() bool {
	return o.state == profileEdited || o.state == profileMissingFeatures
}

// profileOutcome is how a profile stands, or what updating it came to, and
// why.
type profileOutcome struct {
	state profileState
	why   string
}

// assessProfile says how profile stands against the library, changing
// nothing, and, for a guided one, the features it records.
func assessProfile(lay layout.Layout, profile string) (profileOutcome, []string, error) {
	dir := lay.HomePath("profiles", profile)
	if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
		return profileOutcome{profileNotComposed, "it is vibe's own, not one in " + displayPath(lay.HomePath("profiles"))}, nil, nil
	}
	if !profilegen.IsGuided(dir) {
		return profileOutcome{profileNotComposed, "it wasn't composed from the library"}, nil, nil
	}
	features, _ := profilegen.WithoutBase(profilegen.ComposedFrom(dir), lay.BaseFeatures())
	if _, missing := splitMissingFeatures(features, lay.Features()); len(missing) > 0 {
		return profileOutcome{profileMissingFeatures, fmt.Sprintf(
			"it has features the library no longer has (%s): 'vibe -C' asks what to do", strings.Join(missing, ", "))}, features, nil
	}
	fresh, err := profilegen.Check(lay, dir, profile, features)
	if err != nil {
		return profileOutcome{}, features, err
	}
	switch fresh {
	case profilegen.UpToDate:
		return profileOutcome{state: profileCurrent}, features, nil
	case profilegen.Edited:
		return profileOutcome{profileEdited,
			"it was changed by hand since it was composed: 'vibe -C' re-composes it, replacing those changes (backed up first)"}, features, nil
	}
	return profileOutcome{state: profileOutOfDate}, features, nil
}

// updateProfile re-composes profile when the library has moved on since it
// was composed, and nobody has edited it since: backed up first, under
// backupName (a sandbox it's for), as 'vibe -C' does. A profile it can't
// re-compose without losing something is left for the user, saying why.
func updateProfile(lay layout.Layout, profile, backupName string) (profileOutcome, error) {
	// What re-composing does first, so the profile is assessed as it will be.
	if dir := lay.HomePath("profiles", profile); dir != "" && fileExists(dir) {
		if err := prepareInstructions(lay, profile); err != nil {
			return profileOutcome{}, err
		}
		if profilegen.IsGuided(dir) {
			settleFeatures(lay, profile)
		}
	}
	outcome, features, err := assessProfile(lay, profile)
	if err != nil || (outcome.state != profileCurrent && outcome.state != profileOutOfDate) {
		return outcome, err
	}
	dir := lay.HomePath("profiles", profile)
	if outcome.state == profileCurrent {
		// One from before hashes were recorded, which turned out to be up
		// to date, is adopted as it is.
		if profilegen.RecordedHash(dir) == "" {
			if err := profilegen.RecordHash(dir); err != nil {
				return profileOutcome{}, err
			}
		}
		return outcome, nil
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
	if outcome.needsYou() {
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
	if headlessHere != nil {
		note("  it isn't rebuilt while the headless vibe (%s) is using it — 'vibe --restart' here ends that vibe and brings the sandbox up to date", pidList(headlessHere))
		return nil
	}
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// updateTally counts what --update-sandboxes did.
type updateTally struct {
	updated, current, skipped int
	failed                    []string
}

// confirmUpdate asks once whether to go ahead with what --update-sandboxes
// listed: rebuilding is destructive, so only -f goes ahead unasked (and with
// no terminal, it's refused, saying to add -f). Yes is the default, as
// --update-sandboxes asked for this. A variable so tests can stand in for
// the user.
var confirmUpdate = func(force bool) bool {
	return confirmDefault(force, true, "Re-compose and rebuild these? A rebuilt sandbox keeps its agent's memories, but not what was installed in it by hand.")
}

// pendingUpdates lists, for the user to confirm, what bringing inv up to
// date looks like it will do: the profiles to re-compose, and the sandboxes
// to rebuild, being out of date or built from one of those profiles.
func pendingUpdates(inv inventory) []string {
	var lines []string
	stale := map[string]bool{}
	for _, p := range inv.profiles {
		if p.exists && !p.bundled && p.standing.state == profileOutOfDate {
			stale[p.name] = true
			lines = append(lines, fmt.Sprintf("profile '%s': re-composed from %s (backed up first)", p.name, describeFeatures(p.features)))
		}
	}
	for _, p := range inv.profiles {
		for _, s := range p.sandboxes {
			if !s.inSbx || s.folderGone || s.attached {
				continue
			}
			switch {
			case s.outOfDate != "":
				lines = append(lines, fmt.Sprintf("sandbox '%s': rebuilt (%s)", s.inst.Name, s.outOfDate))
			case stale[p.name]:
				lines = append(lines, fmt.Sprintf("sandbox '%s': rebuilt if its profile's re-composing changes it", s.inst.Name))
			}
		}
	}
	return lines
}

// doUpdateSandboxes is --update-sandboxes: every profile in ~/.vibe, then
// every sandbox vibe has a record of, brought up to date. A sandbox with a
// vibe attached is left for its next session start, and each rebuilt one is
// left running or stopped as it was. No session is started. What it will do
// is listed and asked about first, unless force (-f).
func doUpdateSandboxes(lay layout.Layout, force bool) error {
	inv, err := gatherInventory(context.Background(), lay)
	if err != nil {
		return fmt.Errorf("can't tell which sandboxes sbx has, so nothing was updated: %w", err)
	}
	if pending := pendingUpdates(inv); len(pending) > 0 {
		note("this updates:")
		for _, line := range pending {
			note("  %s", line)
		}
		if !confirmUpdate(force) {
			return fmt.Errorf("aborted — nothing changed")
		}
	}

	var tally updateTally
	for _, p := range inv.profiles {
		if !p.exists || p.bundled {
			continue
		}
		backupName := p.name
		if len(p.sandboxes) > 0 {
			backupName = p.sandboxes[0].inst.Name
		}
		outcome, err := updateProfile(lay, p.name, backupName)
		switch {
		case err != nil:
			note("FAILED: profile '%s': %s", p.name, err)
			tally.failed = append(tally.failed, "profile '"+p.name+"'")
		case outcome.state == profileCurrent:
			note("profile '%s' is up to date", p.name)
			tally.current++
		case outcome.state == profileUpdated:
			tally.updated++
		case outcome.needsYou():
			note("WARNING: profile '%s' skipped: %s", p.name, outcome.why)
			tally.skipped++
		default:
			note("profile '%s' skipped: %s", p.name, outcome.why)
			tally.skipped++
		}
	}

	for _, p := range inv.profiles {
		for _, s := range p.sandboxes {
			name := s.inst.Name
			switch {
			case !s.inSbx:
				note("sandbox '%s' skipped: sbx has no sandbox by that name any more", name)
				tally.skipped++
				continue
			case s.folderGone:
				note("sandbox '%s' skipped: %s", name, folderGone(s.inst.Target))
				tally.skipped++
				continue
			case s.attached:
				note("sandbox '%s' skipped: a vibe is attached to it, so it's brought up to date when its next session starts", name)
				tally.skipped++
				continue
			}
			// Asked afresh by updateSandbox: its profile may have just been
			// re-composed.
			switch updated, err := updateSandbox(context.Background(), lay, s.inst, s.running); {
			case err != nil:
				note("FAILED: sandbox '%s': %s", name, err)
				tally.failed = append(tally.failed, "sandbox '"+name+"'")
			case updated:
				tally.updated++
			default:
				tally.current++
			}
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
// saying whether it was, and leaves it running or stopped as it was.
func updateSandbox(ctx context.Context, lay layout.Layout, inst state.Instance, wasRunning bool) (updated bool, err error) {
	why, err := sandboxOutOfDate(lay, inst)
	if err != nil {
		return false, err
	}
	if why == "" {
		note("sandbox '%s' is up to date", inst.Name)
		return false, nil
	}
	note("sandbox '%s' is out of date (%s): rebuilding it", inst.Name, why)
	return true, rebuildAsItWas(ctx, lay, inst, wasRunning)
}

// rebuildAsItWas rebuilds the sandbox inst records and leaves it running or
// stopped as it was. Its folder is held meanwhile, so that a vibe started
// there knows.
func rebuildAsItWas(ctx context.Context, lay layout.Layout, inst state.Instance, wasRunning bool) error {
	session, err := running.Hold(lay.Home, inst.Target, fmt.Sprintf("rebuilding sandbox '%s'", inst.Name))
	if err != nil {
		return err
	}
	defer session.Release()
	if err := rebuild(ctx, lay, inst.Name, inst.Target); err != nil {
		return err
	}
	if wasRunning {
		if !startRebuilt(inst.Name) {
			return fmt.Errorf("rebuilt, but couldn't start it again, as it was running before — 'sbx ls' shows its state")
		}
	} else if err := stopRebuilt(inst.Name); err != nil {
		return fmt.Errorf("rebuilt, but couldn't stop it again, as it was stopped before: %w", err)
	}
	return nil
}

// startRebuilt and stopRebuilt put a rebuilt sandbox back as it was;
// variables for tests.
var (
	startRebuilt = sbxrun.Start
	stopRebuilt  = sbxrun.StopQuiet
)
