package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"gopkg.in/yaml.v3"

	"vibe/internal/fscopy"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/profilegen"
	"vibe/internal/settings"
	"vibe/internal/userprefs"
)

// settingsFile is a profile's own settings.
const settingsFile = "settings.yaml"

// migratedProfiles are those whose preferences have been moved this run, so
// what is said of them is said once.
var migratedProfiles sync.Map

// migratePreferences moves what earlier vibes kept for a profile in places of
// their own (the theme's file, the git author answer's file, and the model
// and effort in its settings.yaml) into its user-preferences.yaml. It's done
// before a profile is looked at for being out of date and before one is
// re-composed, where leaving a choice in a file that is part of what composing
// wrote would make the profile look edited by hand. Only a profile in the
// user's own vibe home is touched: the bundle's isn't theirs to change.
func migratePreferences(lay layout.Layout, profile string) {
	dir := lay.HomePath("profiles", profile)
	if dir == "" || !fileExists(dir) {
		return
	}
	_, said := migratedProfiles.LoadOrStore(dir, true)
	if err := userprefs.Migrate(dir); err != nil && !said {
		note("WARNING: couldn't move profile '%s's theme and git author answer into %s: %s", profile, userprefs.File, err)
	}
	if _, err := profilegen.AddReadme(dir); err != nil && !said {
		note("WARNING: couldn't add %s to profile '%s': %s", profilegen.ReadmeFile, profile, err)
	}
	left, err := migrateChoiceKeys(lay, profile)
	switch {
	case err != nil && !said:
		note("WARNING: couldn't move profile '%s's model and effort into %s: %s", profile, userprefs.File, err)
	case left && !said:
		note("profile '%s': its model and effort are now kept in %s, but are also still in %s, which holds other changes of yours: taking them out would leave the profile looking as if it were edited by hand (they do no harm there, and 'vibe -C' re-composes it without them)", profile, userprefs.File, settingsFile)
	}
}

// migrateChoiceKeys moves the model and effort a profile's settings.yaml holds
// (which is where earlier vibes kept what the user chose on the page) into its
// user preferences, where an answer already there wins, and takes them out of
// settings.yaml, as long as that leaves the profile no more edited by hand
// than it is: settings.yaml is part of what composing wrote, so a profile
// composed with it as it was, and still so but for these keys, is current again
// without them. When taking them out wouldn't do that (the user changed
// something else there too), they are left, and left says so: what's in the
// preferences is read first, so nothing is lost either way.
func migrateChoiceKeys(lay layout.Layout, profile string) (left bool, err error) {
	dir := lay.HomePath("profiles", profile)
	if dir == "" || !fileExists(dir) {
		return false, nil
	}
	path := filepath.Join(dir, settingsFile)
	data, err := fscopy.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	own, err := settings.Parse(data)
	if err != nil {
		return false, fmt.Errorf("can't read %s: %w", pathresolve.Tilde(path), err)
	}
	if own.Model == "" && own.Effort == "" {
		return false, nil
	}
	stripped, err := settings.Unset(data, "model")
	if err == nil {
		stripped, err = settings.Unset(stripped, "effort")
	}
	if err != nil {
		return false, fmt.Errorf("updating %s: %w", pathresolve.Tilde(path), err)
	}
	if err := userprefs.Update(dir, func(p *userprefs.Prefs) {
		if p.Model == "" {
			p.Model = own.Model
		}
		if p.Effort == "" {
			p.Effort = own.Effort
		}
	}); err != nil {
		return false, err
	}
	recorded := profilegen.RecordedHash(dir)
	// What settings.yaml could become: as it is with the keys out, or, for a
	// composed profile (whose file earlier vibes rewrote whole, in their own
	// layout, so it won't match by the bytes), what composing writes now if
	// that says the same. The first that leaves the profile as it was composed
	// is the one.
	var candidates [][]byte
	if recorded != "" {
		if fresh, ok, err := profilegen.ComposedFile(lay, profile, profilegen.ComposedFrom(dir), settingsFile); err == nil && sameSettings(fresh, stripped) {
			if ok {
				candidates = append(candidates, fresh)
			} else {
				candidates = append(candidates, nil)
			}
		}
	}
	nothingLeft := sameSettings(nil, stripped)
	if nothingLeft {
		candidates = append(candidates, nil)
	} else {
		candidates = append(candidates, stripped)
	}
	chosen := candidates[len(candidates)-1]
	if recorded != "" {
		found := false
		for _, candidate := range candidates {
			with, err := profilegen.ContentHashWith(dir, map[string][]byte{settingsFile: candidate})
			if err != nil {
				return false, err
			}
			if with == recorded {
				chosen, found = candidate, true
				break
			}
		}
		if !found {
			return true, nil
		}
	}
	stripped = chosen
	nothingLeft = stripped == nil
	if nothingLeft {
		return false, os.Remove(path)
	}
	perm := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	return false, fscopy.WriteWholeMode(path, stripped, perm)
}

// sameSettings says two settings.yaml files hold the same settings, however
// they are laid out; a file that isn't there or is empty holds none.
func sameSettings(a, b []byte) bool {
	parse := func(data []byte) map[string]interface{} {
		var m map[string]interface{}
		if yaml.Unmarshal(data, &m) != nil {
			return map[string]interface{}{"unreadable": string(data)}
		}
		if m == nil {
			m = map[string]interface{}{}
		}
		return m
	}
	return reflect.DeepEqual(parse(a), parse(b))
}
