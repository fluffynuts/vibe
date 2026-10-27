package main

import (
	"os"

	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"vibe/internal/homeinit"
	"vibe/internal/instructions"
	"vibe/internal/layout"
	"vibe/internal/library"
	"vibe/internal/pathresolve"
	"vibe/internal/profilegen"
	"vibe/internal/settings"
	"vibe/internal/upgrade"
	"vibe/internal/userprefs"
)

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
	if slices.Index(res.Files, "settings.yaml") >= 0 {
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
func ensureProfile(lay layout.Layout, profile string, force bool, agent string) error {
	if err := createProfileIfMissing(lay, profile, force, agent); err != nil {
		return err
	}
	ensureReadme(lay, profile)
	return prepareInstructions(lay, profile)
}

// ensureReadme gives a profile vibe generated its README.md when it has none
// (see profilegen.AddReadme), whenever a session starts or a sandbox is made
// from it, whether or not it is being updated: it is for whoever wonders what
// the profile's files are. Only a profile in the user's own vibe home is
// touched, and a failure is said, not fatal.
func ensureReadme(lay layout.Layout, profile string) {
	dir := lay.HomePath("profiles", profile)
	if dir == "" || !fileExists(dir) {
		return
	}
	if _, err := profilegen.AddReadme(dir); err != nil {
		note("WARNING: couldn't add %s to profile '%s': %s", profilegen.ReadmeFile, profile, err)
	}
}

// prepareInstructions brings the user's instructions files up to date
// before anything is built from the profile: the universal one in ~/.vibe
// is made if missing, and so is the profile's own — after a guided profile
// from before has had the kits' text moved out of it (see
// instructions.MigrateProfile). A profile that is only in vibe's bundle is
// left alone: making a folder for it in ~/.vibe would hide the bundle's.
func prepareInstructions(lay layout.Layout, profile string) error {
	if path := lay.HomePath(instructions.File); path != "" {
		if created, err := instructions.EnsureFile(path); err != nil {
			return err
		} else if created {
			note("created %s: instructions for every sandbox's agent go there", pathresolve.Tilde(path))
		}
	}
	dir := lay.HomePath("profiles", profile)
	if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
		return nil
	}
	moved, err := instructions.MigrateProfile(dir, profilegen.IsGuided(dir))
	if err != nil {
		return err
	}
	if moved {
		note("profile '%s': the kits' instructions are in %s now; %s is yours, for this profile's agent",
			profile, instructions.KitFile, instructions.File)
	}
	return nil
}

// createProfileIfMissing makes the profile when there's none, asking how
// (guided, a copy, or blank) when there's a terminal to ask on. agent is the
// agent the sandbox will run: guided creation offers only the features
// that work with it.
func createProfileIfMissing(lay layout.Layout, profile string, force bool, agent string) error {
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
		const (
			guided = iota
			copyExisting
			blank
		)
		choice, ok := chooseFrom("create it how?",
			[]string{"guided profile creation", "copy an existing profile", "create a new blank profile (no optional features)"}, guided)
		if !ok {
			return fmt.Errorf("aborted — no profile '%s' created", profile)
		}
		switch choice {
		case copyExisting:
			pick, ok := chooseFrom(fmt.Sprintf("which profile should '%s' start from?", profile), existing, 0)
			if !ok {
				return fmt.Errorf("aborted — no profile '%s' created", profile)
			}
			source = existing[pick]
		case guided:
			return ensureGuidedProfile(lay, profile, agent)
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
		if carried := choicesCarried(dir); carried != "" {
			note("it carries the choices made for '%s' (%s), which are kept with a profile: change them on the page, or edit or delete %s to be asked or to go back to the defaults", source, carried, filepath.Join(dir, userprefs.File))
		}
	} else {
		note("created blank profile '%s' in %s: no optional features, and the base features every profile has", profile, dir)
	}
	return nil
}

// choicesCarried says which of the choices kept with a profile (see
// userprefs) the profile in dir holds, "" for none or if they can't be read.
func choicesCarried(dir string) string {
	prefs, err := userprefs.Load(dir)
	if err != nil {
		return ""
	}
	var carried []string
	for _, c := range []struct{ what, value string }{
		{"theme", prefs.Theme}, {"git author answer", prefs.GitAuthor}, {"model", prefs.Model}, {"effort", prefs.Effort},
	} {
		if c.value != "" {
			carried = append(carried, c.what+" "+c.value)
		}
	}
	return strings.Join(carried, ", ")
}

// ensureGuidedProfile walks the user through picking which library
// features a new profile should install, lets them reorder the ones they
// picked, and writes the result as a new profile via profilegen.CreateGuided.
// Features that don't work with agent aren't offered.
func ensureGuidedProfile(lay layout.Layout, profile, agent string) error {
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
	base.DefaultFeatures, _ = profilegen.WithoutBase(base.DefaultFeatures, lay.BaseFeatures())
	base.DefaultFeatures = withoutRetiredDefaults(base.DefaultFeatures, settingsPath)
	if err := library.Validate(base.DefaultFeatures, names); err != nil {
		return fmt.Errorf("defaultFeatures in %s: %w", settingsPath, err)
	}
	names = offeredFor(lay, names, agent, nil)
	base.DefaultFeatures = defaultsFor(lay, base.DefaultFeatures, agent)
	if len(names) == 0 {
		return fmt.Errorf("no library features work with %s — pick another option instead", agent)
	}

	features, labels, checked := featureChecklist(lay, names, base.DefaultFeatures)
	if len(base.DefaultFeatures) > 0 {
		note("selected from defaultFeatures (deselect any you don't want): %s", strings.Join(base.DefaultFeatures, ", "))
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

// offeredFor is the features among names to offer for agent's sandbox:
// the ones that work with it, and any of has, which the profile has
// already — taking one of those out is the user's choice, not vibe's.
func offeredFor(lay layout.Layout, names []string, agent string, has []string) []string {
	var offered []string
	for _, f := range library.List(names, lay.FeatureDir) {
		if f.Supports(agent) || slices.Contains(has, f.Name) {
			offered = append(offered, f.Name)
		}
	}
	return offered
}

// defaultsFor is the defaultFeatures that work with agent, saying which
// are left out and why: they're the user's own choice, so going without
// one shouldn't go unremarked.
func defaultsFor(lay layout.Layout, defaults []string, agent string) []string {
	for _, f := range library.Unsupported(defaults, lay.FeatureDir, agent) {
		note("defaultFeatures: leaving out %s", whyUnsupported(f, agent))
	}
	kept, _ := library.ForAgent(defaults, lay.FeatureDir, agent)
	return kept
}

// whyUnsupported says why f isn't for agent's sandbox, for a note.
func whyUnsupported(f library.Feature, agent string) string {
	return fmt.Sprintf("%s: it works only with %s, and this sandbox runs %s", f.Name, strings.Join(f.Agents, ", "), agent)
}

// featureChecklist lists the library features names for a checkbox list:
// each one's label, and whether it starts ticked — the ones in ticked.
func featureChecklist(lay layout.Layout, names, ticked []string) (features []library.Feature, labels []string, checked []bool) {
	features = library.List(names, lay.FeatureDir)
	labels = make([]string, len(features))
	checked = make([]bool, len(features))
	for i, f := range features {
		labels[i] = f.Label()
		if len(f.Agents) > 0 {
			labels[i] += " (" + strings.Join(f.Agents, ", ") + " only)"
		}
		checked[i] = slices.Index(ticked, f.Name) >= 0
	}
	return features, labels, checked
}

// pickDefaultFeatures asks which library features a guided profile should
// start with ticked, starting with defaults ticked. Quitting keeps
// defaults.
func pickDefaultFeatures(lay layout.Layout, defaults []string) ([]string, error) {
	names := lay.Features()
	if len(names) == 0 {
		return defaults, nil
	}
	defaults, _ = profilegen.WithoutBase(defaults, lay.BaseFeatures())
	defaults = withoutRetiredDefaults(defaults, lay.HomePath("settings.yaml"))
	if err := library.Validate(defaults, names); err != nil {
		return nil, fmt.Errorf("defaultFeatures in %s: %w", pathresolve.Tilde(lay.HomePath("settings.yaml")), err)
	}
	features, labels, checked := featureChecklist(lay, names, defaults)
	idxs, ok := checklistFrom("which features should a guided profile start with selected?", labels, checked)
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

// withoutRetiredDefaults is defaults (settings' defaultFeatures, from
// settingsPath) without the features vibe no longer has, saying so: an
// error would stop a guided profile being made at all, for a name that
// was right when it was written.
func withoutRetiredDefaults(defaults []string, settingsPath string) []string {
	kept, retired := library.WithoutRetired(defaults)
	for _, name := range retired {
		note("defaultFeatures in %s names '%s', which vibe no longer has (%s): it's left out (take it out of there to stop this note)", pathresolve.Tilde(settingsPath), name, library.Retired[name])
	}
	return kept
}
