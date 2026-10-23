package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vibe/internal/backup"
	"vibe/internal/claudetheme"
	"vibe/internal/cliargs"
	"vibe/internal/fscopy"
	"vibe/internal/instructions"
	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/library"
	agentmem "vibe/internal/memory"
	"vibe/internal/pathresolve"
	"vibe/internal/profilegen"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
	"vibe/internal/userprefs"
)

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
	return reInit(lay, args, name, target, false, false)
}

// reInit is --re-init's implementation. skipSandboxConfirm is set by
// doReCreate, which already got one confirmation up front covering both
// the profile and the sandbox, so this must not ask about the sandbox a
// second time. askZone asks which timezone the rebuilt sandbox should be
// on, rather than keeping the one it had.
func reInit(lay layout.Layout, args cliargs.Args, name, target string, skipSandboxConfirm, askZone bool, after ...afterProfile) error {
	if err := rebuildSandbox(context.Background(), lay, args, name, target, skipSandboxConfirm, askZone, after...); err != nil {
		return err
	}
	return finish(lay, args, name)
}

// pickRebuildAgent asks which agent a rebuilt sandbox runs: pickAgent, which
// tests stand in for.
var pickRebuildAgent = pickAgent

// rebuildSandbox is reInit without the session after it: the sandbox is
// removed (its memories kept, if agreed) and created again from its profile.
//
// ctx ending (the companion page's Cancel) ends the sbx command under way,
// and the rebuild stops before its next step: what is left is said by the
// caller, which can ask sbx.
func rebuildSandbox(ctx context.Context, lay layout.Layout, args cliargs.Args, name, target string, skipSandboxConfirm, askZone bool, after ...afterProfile) error {
	return rebuildSandboxWith(ctx, lay, args, name, target, skipSandboxConfirm, askZone, nil, after)
}

// afterProfile is something done once a rebuild has made sure the profile is
// there (made it, if --re-create had deleted it), and before anything is read
// from it.
type afterProfile func(lay layout.Layout, profile string) error

// relocation moves a sandbox's folder, and what vibe keeps for it, once the
// old sandbox is gone, and says its new name and folder.
type relocation func() (name, target string, err error)

// rebuildSandboxTo is rebuildSandbox, with relocate (when not nil) run
// between removing the old sandbox and creating the new one, which is made
// with the name and for the folder relocate gives: --migrate.
func rebuildSandboxTo(ctx context.Context, lay layout.Layout, args cliargs.Args, name, target string, skipSandboxConfirm, askZone bool, relocate relocation) error {
	return rebuildSandboxWith(ctx, lay, args, name, target, skipSandboxConfirm, askZone, relocate, nil)
}

// rebuildSandboxWith is rebuildSandboxTo, with after done once the profile is
// there.
func rebuildSandboxWith(ctx context.Context, lay layout.Layout, args cliargs.Args, name, target string, skipSandboxConfirm, askZone bool, relocate relocation, after []afterProfile) error {
	vibeHome := lay.Home
	// Before anything is removed: a sandbox can't be built for a folder that
	// isn't there, so removing it first would leave nothing at all.
	if why := folderGone(target); why != "" {
		return fmt.Errorf("sandbox '%s' was left as it is: %s", name, why)
	}
	profile, err := resolveReInitProfile(vibeHome, args, name, target)
	if err != nil {
		return err
	}

	// A rebuild starts on the agent the sandbox has, and one started at a
	// terminal asks again, as a rebuild is the way to change it. A migration
	// moves a sandbox, and a rebuild with -f or from the page has no one to
	// ask.
	defaultAgent, err := defaultAgentFor(lay, profile)
	if err != nil {
		return err
	}
	was := recordedAgent(vibeHome, name, defaultAgent)
	agent := was
	if !args.Force && relocate == nil {
		agent = pickRebuildAgent(was, interactive(), fmt.Sprintf("which agent should sandbox '%s' run?", name))
		if agent != was {
			note("  agent: %s, not %s", agent, was)
		}
	}
	if err := ensureProfile(lay, profile, args.Force, agent); err != nil {
		return err
	}
	for _, f := range after {
		if f == nil {
			continue
		}
		if err := f(lay, profile); err != nil {
			return err
		}
	}
	doc, merged, err := loadKit(lay, profile)
	if err != nil {
		return err
	}
	merged.Agent = agent
	warnUnsupportedFeatures(lay, profile, agent)

	restoreAfterCreate := false
	var memoryStore string
	var themeFromSandbox string
	zone := recordedTimezone(vibeHome, name)
	// A rebuilt sandbox keeps the memory and CPUs it was created with, not
	// whatever the settings say now.
	if inst, found, err := state.Load(vibeHome, name); err == nil && found {
		if inst.Memory != "" {
			merged.Memory = inst.Memory
		}
		if inst.CPUs > 0 {
			merged.CPUs = inst.CPUs
		}
	}
	git := recordedGitAccess(vibeHome, name)

	exists, err := sbxrun.Exists(name)
	if err != nil {
		return err
	}
	if exists {
		if !skipSandboxConfirm && !confirmDefault(args.Force, true, fmt.Sprintf("Remove sandbox '%s' (workspace %s)?", name, target)) {
			return fmt.Errorf("aborted — sandbox left alone")
		}
		// What the sandbox being replaced holds is the agent it has now's,
		// whichever agent comes next.
		if agentmem.Supported(was) {
			// Memories can only be read out of a running sandbox, and the
			// one being replaced is often stopped. Since removing it is
			// already agreed, boot it just long enough to ask — otherwise a
			// stopped sandbox's memories would be reported as absent and
			// then destroyed along with it.
			presence := agentmem.Probe(name)
			if presence == agentmem.Unknown {
				note("  '%s' is not running — starting it to check for memories", name)
				if sbxrun.Start(ctx, name) {
					presence = agentmem.Probe(name)
				}
			}
			switch {
			case presence == agentmem.Unknown:
				note("  WARNING: could not start '%s' to check for memories — any it holds will go with it", name)
			case presence == agentmem.None:
				note("  no memories found in '%s' — nothing to preserve", name)
			case confirmDefault(args.Force, true, fmt.Sprintf("Preserve agent memories from '%s'?", name)):
				memoryStore = agentmem.StoreFor(vibeHome, merged.MemoryRoot, name)
				ok, err := agentmem.Backup(ctx, name, memoryStore)
				if err != nil {
					note("  WARNING: %s", err)
				} else if !ok {
					note("  no memories found in '%s' — nothing to preserve", name)
				} else if !agentmem.Supported(agent) {
					note("  memories saved to %s, and left there: %s can't use them", memoryStore, agent)
					memoryStore = ""
				} else {
					note("  memories saved to %s", memoryStore)
					restoreAfterCreate = true
				}
			}
		}
		// Read after the memories: checking for those will have started a
		// stopped sandbox, which is the only kind that can be asked.
		if was == "claude" && agent == "claude" && sbxrun.Reachable(ctx, name) {
			themeCtx, cancel := context.WithTimeout(ctx, themeReadTimeout)
			if c, ok := claudetheme.FromSandbox(themeCtx, name); ok {
				themeFromSandbox = c
			}
			cancel()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// sbx forgets the sandbox's own network rules with it: the new one
		// gets them back from what's kept.
		noteKeptRules(vibeHome, name)
		note("removing sandbox '%s'", name)
		if err := sbxrun.Remove(ctx, name, true, actionOutput()); err != nil {
			return err
		}
		// The instance record deliberately outlives the sandbox: it carries
		// the published ports this name already owns, which is what lets the
		// rebuild below re-claim them and keep the URL stable. createSandbox
		// overwrites it once the new sandbox is up.
	} else {
		note("no existing sandbox '%s' — nothing to remove", name)
	}

	if relocate != nil {
		oldName := name
		if name, target, err = relocate(); err != nil {
			return err
		}
		// The memories went with the rest: a store named after the sandbox
		// is renamed with it (see migrateRecords).
		if dir, base := filepath.Split(memoryStore); restoreAfterCreate && base == oldName {
			memoryStore = filepath.Join(dir, name)
		}
	}

	if agent == "claude" {
		if doc, err = setClaudeTheme(lay, profile, doc, themeFromSandbox, false, args.Force); err != nil {
			return err
		}
	} else {
		kitspec.DropClaudeFiles(doc)
	}

	note("rebuilding '%s' from profile '%s'", name, profile)
	if err := ctx.Err(); err != nil {
		return err
	}
	// A rebuild started at a terminal asks how big the sandbox should be,
	// starting on what it has: it's the one way to change that. A migration
	// moves a sandbox rather than resizing it, and a rebuild with -f, or from
	// the page, has no one to ask.
	askSize := !args.Force && relocate == nil
	return createSandbox(ctx, lay, name, target, profile, doc, merged, askSize, zone, askZone, restoreAfterCreate, memoryStore, git, !args.Force)
}

// folderGone says, when a sandbox's folder target no longer exists, why
// that matters and what to do; "" when it's there.
func folderGone(target string) string {
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return ""
	}
	return fmt.Sprintf("its folder %s no longer exists — 'vibe --cleanup' deletes the sandbox; if the folder moved, 'vibe' in its new place makes a new one", target)
}

// doReCreate deletes the overlay copy of the matched profile (if there is
// one — the bundle's own copy, if any, is never touched), then does
// exactly what --re-init does: with the profile gone, ensureProfile guides
// the user through creating a new one (copy, blank or guided) instead of
// silently reusing the old one, and any existing sandbox is removed and
// rebuilt from it. Both destructive steps share a single confirmation up
// front rather than asking once per step; yes is its default, as -R asked
// for this. The profile is backed up first (the sandbox's last three backups
// are kept, as for a re-compose; 'vibe --restore' lists them). What the user
// chose through vibe for the profile (see userprefs) isn't part of what is
// made again: it is kept, with -f, and otherwise unless they say no.
func doReCreate(lay layout.Layout, args cliargs.Args, name, target string) error {
	profile, err := resolveReInitProfile(lay.Home, args, name, target)
	if err != nil {
		return err
	}

	if !confirmDefault(args.Force, true,
		"This action will destroy any existing sandbox for the current project, and make its profile afresh (the old one is backed up first). Continue?") {
		return fmt.Errorf("aborted — nothing removed")
	}

	keep, err := preferencesToKeep(lay, profile, args.Force)
	if err != nil {
		return err
	}
	dir := lay.HomePath("profiles", profile)
	if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
		// As a re-compose does: what is about to go can be had back, and if it
		// can't be kept, nothing goes.
		zip, err := backup.Create(lay.HomePath("backup"), name, dir, time.Now())
		if err != nil {
			return fmt.Errorf("could not back up profile '%s' first (nothing was removed): %w", profile, err)
		}
		note("backed up profile '%s' to %s ('vibe --restore' rolls it back)", profile, zip)
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		note("deleted profile '%s' from %s", profile, dir)
	} else {
		note("no overlay profile '%s' to delete", profile)
	}

	return reInit(lay, args, name, target, true, !args.Force, restorePreferences(keep))
}

// askKeepPreferences asks whether to keep what the user chose for a profile
// across --re-create, which keeps it unless they say no. Tests stand in for it.
var askKeepPreferences = func(question string) bool { return confirmDefault(false, true, question) }

// preferencesToKeep is the profile's user preferences as they are, to be put
// back in the profile made afresh: all of them, byte for byte, so a comment or a
// key a newer vibe wrote comes through. With -f they are kept; otherwise the user
// is asked, with what is in the file shown, and says no to let them go. Nothing
// to keep, nothing is asked. Read before anything is removed, so a file that
// can't be read stops it with the profile still there.
func preferencesToKeep(lay layout.Layout, profile string, force bool) ([]byte, error) {
	dir := lay.HomePath("profiles", profile)
	if dir == "" || !fileExists(dir) {
		return nil, nil
	}
	// Earlier vibes' files, and the model and effort in settings.yaml, move in first.
	migratePreferences(lay, profile)
	data, err := fscopy.ReadFile(userprefs.Path(dir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't read %s, so can't tell what to keep (nothing was removed): %w", pathresolve.Tilde(userprefs.Path(dir)), err)
	}
	listing, any := describePreferences(dir)
	if !any {
		return nil, nil
	}
	if force {
		note("keeping your choices from the old profile '%s' (%s): delete %s, or lines in it, to drop them", profile, listing, pathresolve.Tilde(userprefs.Path(dir)))
		return data, nil
	}
	if !askKeepPreferences(fmt.Sprintf("Keep your choices for profile '%s' (%s)?", profile, listing)) {
		return nil, nil
	}
	return data, nil
}

// describePreferences lists what the profile's user preferences hold ("theme:
// amber, model: opus"), and whether there is anything: a file with only comments
// holds nothing to keep, and one that can't be read is said so, and is kept.
func describePreferences(dir string) (listing string, any bool) {
	prefs, err := userprefs.Load(dir)
	if err != nil {
		return "a file vibe can't read: " + err.Error(), true
	}
	var parts []string
	for _, kv := range []struct{ key, value string }{{"theme", prefs.Theme}, {"gitAuthor", prefs.GitAuthor}, {"model", prefs.Model}, {"effort", prefs.Effort}} {
		if kv.value != "" {
			parts = append(parts, kv.key+": "+kv.value)
		}
	}
	if n := len(prefs.Others); n > 0 {
		parts = append(parts, fmt.Sprintf("%d other setting(s)", n))
	}
	return strings.Join(parts, ", "), len(parts) > 0
}

// restorePreferences puts the preferences kept (see preferencesToKeep) into
// the profile made afresh; nil when there was nothing to keep.
func restorePreferences(data []byte) afterProfile {
	if data == nil {
		return nil
	}
	return func(lay layout.Layout, profile string) error {
		dir := lay.HomePath("profiles", profile)
		if dir == "" {
			return fmt.Errorf("there is no vibe home to put your choices back in")
		}
		if err := fscopy.WriteWholeMode(userprefs.Path(dir), data, 0o644); err != nil {
			return fmt.Errorf("couldn't put your choices back in %s: %w", pathresolve.Tilde(userprefs.Path(dir)), err)
		}
		return nil
	}
}

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
	return reCompose(lay, args, name, target, true)
}

// reCompose is --re-compose's implementation. warn is off when the user has
// just agreed to a rollback, which this follows on from.
func reCompose(lay layout.Layout, args cliargs.Args, name, target string, warn bool) error {
	if err := recomposeProfile(lay, args, name, target, warn, nil); err != nil {
		return err
	}
	return reInit(lay, args, name, target, true, !args.Force)
}

// recomposeProfile is reCompose up to the rebuild: the profile is backed up
// and composed afresh. chosen is the features to compose it from, already
// picked (as the companion page does); nil asks, unless args.Force keeps
// those it has.
func recomposeProfile(lay layout.Layout, args cliargs.Args, name, target string, warn bool, chosen []string) error {
	profile, err := resolveReInitProfile(lay.Home, args, name, target)
	if err != nil {
		return err
	}
	if !lay.ProfileExists(profile) {
		return fmt.Errorf("no profile '%s' to re-compose — run vibe -R to create one", profile)
	}
	// Before anything else: a profile from before the kits' instructions
	// had a file of their own has them moved out of the user's way.
	if err := prepareInstructions(lay, profile); err != nil {
		return err
	}
	features := settleFeatures(lay, profile)
	defaultAgent, err := defaultAgentFor(lay, profile)
	if err != nil {
		return err
	}
	agent := recordedAgent(lay.Home, name, defaultAgent)
	if !profilegen.IsGuided(lay.ProfileDir(profile)) {
		return fmt.Errorf("profile '%s' does not record which features it was composed from.\n"+
			"Only profiles from guided creation do — add a '# vibe: features: a, b' line to\n"+
			"%s to adopt one, or use vibe -R to build a fresh profile from the library.",
			profile, filepath.Join(lay.ProfileDir(profile), "config.yaml"))
	}
	available := lay.Features()
	present, missing := splitMissingFeatures(features, available)
	if len(missing) > 0 && args.Force && chosen == nil {
		// Nobody to ask, and silently rebuilding without a feature would
		// drop something the profile needs.
		err := library.Validate(missing, available)
		return fmt.Errorf("profile '%s' was composed from a feature that is no longer there: %w", profile, err)
	}

	// Asked first, and defaulting to yes: re-composing is nearly always
	// safe, so the rest of the flow can be a matter of pressing enter.
	if warn && !args.Force {
		fmt.Fprintf(os.Stderr, "\x1b[1;93mvibe: WARNING: this rebuilds profile '%s' and destroys its sandbox '%s'.\x1b[0m\n", profile, name)
		fmt.Fprintf(os.Stderr, "\x1b[1;93m  Your existing profile will be backed up (restore with --restore)\x1b[0m\n")
		fmt.Fprintf(os.Stderr, "\x1b[1;93m  The existing sandbox will be destroyed and cannot be restored\x1b[0m\n")
	}
	if warn && !confirmDefault(args.Force, true, "Continue?") {
		return fmt.Errorf("aborted — nothing changed")
	}

	note("profile '%s' was composed from: %s", profile, describeFeatures(features))
	if len(missing) > 0 {
		note("This sandbox profile refers to features which can no longer be located:")
		for _, name := range missing {
			note("  - %s", name)
		}
		note("Please verify which features are required (the ones still available are selected):")
		features = present
	}
	switch {
	case chosen != nil:
		if err := library.Validate(chosen, available); err != nil {
			return err
		}
		features = chosen
	case !args.Force:
		features, err = pickReComposeFeatures(lay, profile, features, agent)
		if err != nil {
			return err
		}
	}
	return replaceProfile(lay, profile, name, features)
}

// replaceProfile backs profile up, under backupName (the sandbox it's for),
// and composes it afresh from features.
func replaceProfile(lay layout.Layout, profile, backupName string, features []string) error {
	// What earlier vibes kept in files of their own, and the model and effort
	// they kept in settings.yaml, move into the preferences first, so what is
	// carried is all there is.
	migratePreferences(lay, profile)
	// The user's choices (the theme, the git author answer, the model and
	// effort) aren't something the features produce, so they would go with the
	// rest of the old profile; carry them across, as the user's own
	// instructions for the profile are. Both are read now, before anything is
	// removed, so that failing to read them changes nothing.
	userInstructions, err := fscopy.ReadFile(filepath.Join(lay.ProfileDir(profile), instructions.File))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not read profile '%s's %s (nothing changed): %w", profile, instructions.File, err)
	}
	preferences, err := fscopy.ReadFile(userprefs.Path(lay.ProfileDir(profile)))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not read profile '%s's %s (nothing changed): %w", profile, userprefs.File, err)
	}
	dir := lay.HomePath("profiles", profile)
	if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
		if zip, err := backup.Create(lay.HomePath("backup"), backupName, dir, time.Now()); err != nil {
			return fmt.Errorf("could not back up profile '%s' first (nothing changed): %w", profile, err)
		} else {
			note("backed up profile '%s' to %s", profile, zip)
		}
	}
	if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	created, err := profilegen.CreateGuided(lay, profile, features)
	if err != nil {
		return err
	}
	if err := fscopy.WriteWholeMode(filepath.Join(created, instructions.File), userInstructions, 0o644); err != nil {
		return fmt.Errorf("could not carry profile '%s's %s across (the backup above has it): %w", profile, instructions.File, err)
	}
	if preferences != nil {
		if err := fscopy.WriteWholeMode(userprefs.Path(created), preferences, 0o644); err != nil {
			return fmt.Errorf("could not carry profile '%s's %s across (the backup above has it): %w", profile, userprefs.File, err)
		}
	}
	note("re-composed profile '%s' (%s) in %s", profile, strings.Join(features, ", "), created)
	return nil
}

// doRestore is --restore: roll the profile back to one of its backups, then
// offer to re-compose from it.
func doRestore(lay layout.Layout, args cliargs.Args, name, target string) error {
	profile, err := resolveReInitProfile(lay.Home, args, name, target)
	if err != nil {
		return err
	}
	backups, err := backup.List(lay.HomePath("backup"), name)
	if err != nil {
		return err
	}
	if len(backups) == 0 {
		note("There are no backups to restore")
		return nil
	}
	chosen := backups[0]
	if !args.Force {
		labels := make([]string, len(backups))
		for i, b := range backups {
			labels[i] = b.Label()
		}
		idx, ok := chooseFrom(fmt.Sprintf("Backups of profile '%s' (newest first):", profile), labels, 0)
		if !ok {
			return fmt.Errorf("nothing chosen — nothing changed")
		}
		chosen = backups[idx]
	}
	if !confirmDefault(args.Force, true, "Are you sure you want to roll back to this backup? This action cannot be undone") {
		return fmt.Errorf("aborted — nothing changed")
	}
	dir := lay.NewProfileDir(profile)
	if err := backup.Restore(chosen.Path, dir); err != nil {
		return err
	}
	note("restored profile '%s' from the backup of %s", profile, chosen.Label())
	// A profile that was blank, copied or written by hand (which is what a
	// backup made by --re-create can hold) isn't made from the library: it is
	// as it was, and there is nothing to re-compose it from.
	if !profilegen.IsGuided(dir) {
		note("it wasn't composed from the library, so there is nothing to re-compose: it is as it was in the backup")
		return nil
	}
	if !confirmDefault(args.Force, true, "Re-compose now?") {
		return nil
	}
	return reCompose(lay, args, name, target, false)
}

// settleFeatures makes a profile's record of its features agree with what
// vibe has: a base feature it names (it was a choice once) is taken out
// silently, as it's always on; a retired one (vibe has it no longer) is taken
// out saying so, and what replaced it. It returns the features the profile
// records, those excluded.
func settleFeatures(lay layout.Layout, profile string) []string {
	dir := lay.ProfileDir(profile)
	base := lay.BaseFeatures()
	_, retired := library.WithoutRetired(profilegen.ComposedFrom(dir))
	for _, name := range retired {
		note("profile '%s' was composed with '%s', which vibe no longer has (%s): it's left out", profile, name, library.Retired[name])
	}
	features, err := profilegen.DropFromRecord(dir, append(append([]string{}, base...), retired...))
	if err != nil {
		// Not worth stopping for: the same strip happens next time.
		features, _ = profilegen.WithoutBase(profilegen.ComposedFrom(dir), base)
		features, _ = library.WithoutRetired(features)
	}
	return features
}

// noteMissingBase tells the user, at creation, when a profile was built
// without a base feature's files, and how to get them.
func noteMissingBase(lay layout.Layout, profile string) {
	dir := lay.ProfileDir(profile)
	missing := missingBaseFeatures(lay, dir, lay.BaseFeatures())
	if len(missing) == 0 {
		return
	}
	them := "it"
	if len(missing) > 1 {
		them = "them"
	}
	note("every sandbox now has %s, which profile '%s' was built without", describeFeatures(missing), profile)
	if profilegen.IsGuided(dir) {
		note("  run 'vibe --re-compose' to rebuild the profile with %s", them)
		note("  (that recreates the sandbox, so anything that isn't in your workspace is lost)")
	} else {
		note("  it was not composed from the library: 'vibe -R' builds a fresh profile that has %s", them)
	}
}

// missingBaseFeatures names the base features whose agent-files are not all
// in the profile.
func missingBaseFeatures(lay layout.Layout, profileDir string, base []string) (missing []string) {
	for _, name := range base {
		root := filepath.Join(lay.FeatureDir(name), "agent-files")
		complete := true
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if _, err := os.Stat(filepath.Join(profileDir, "agent-files", rel)); err != nil {
				complete = false
			}
			return nil
		})
		if !complete {
			missing = append(missing, name)
		}
	}
	return missing
}

// splitMissingFeatures separates the wanted features that the library still
// has (in their original order) from those it no longer does.
func splitMissingFeatures(wanted, available []string) (present, missing []string) {
	have := make(map[string]bool, len(available))
	for _, name := range available {
		have[name] = true
	}
	for _, name := range wanted {
		if have[name] {
			present = append(present, name)
		} else {
			missing = append(missing, name)
		}
	}
	return present, missing
}

// describeFeatures names features for a sentence, allowing for none.
func describeFeatures(features []string) string {
	if len(features) == 0 {
		return "no features"
	}
	return strings.Join(features, ", ")
}

// pickReComposeFeatures lets the user change which library features a
// profile is rebuilt from: the checklist of the whole library, with the ones
// the profile already has ticked. Features already in the profile keep their
// order, and newly ticked ones follow in library order; with more than one
// the user can then reorder them, as in guided creation. Features that
// don't work with agent aren't offered, unless the profile has them.
var pickReComposeFeatures = func(lay layout.Layout, profile string, current []string, agent string) ([]string, error) {
	names := offeredFor(lay, lay.Features(), agent, current)
	features, labels, checked := featureChecklist(lay, names, current)
	idxs, ok := checklistFrom("pick the features this sandbox needs (deselect any you no longer use)", labels, checked)
	if !ok {
		return nil, fmt.Errorf("aborted — profile '%s' not changed", profile)
	}
	picked := make(map[string]bool, len(idxs))
	for _, idx := range idxs {
		picked[features[idx].Name] = true
	}
	var chosen []string
	for _, name := range current {
		if picked[name] {
			chosen = append(chosen, name)
			delete(picked, name)
		}
	}
	for _, idx := range idxs {
		if name := features[idx].Name; picked[name] {
			chosen = append(chosen, name)
		}
	}
	if len(chosen) > 1 {
		ordered, ok := reorderFrom("order the features — they install and start in this order", chosen)
		if !ok {
			return nil, fmt.Errorf("aborted — profile '%s' not changed", profile)
		}
		chosen = ordered
	}
	return chosen, nil
}
