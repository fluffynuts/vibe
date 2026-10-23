// Package userprefs is the choices a user has made through vibe for a
// profile (the Claude theme, whether a sandbox with no git author is given
// theirs, the model and effort the conversation asks the agent for), kept in
// one file in the profile's folder.
//
// They are choices, not what composing the profile wrote, so the file is left
// out of the profile's content hash (see profilegen.ContentHash): a profile
// isn't taken for edited by hand because the user picked a model. It carries
// across a re-compose, and goes with the profile when that is deleted.
package userprefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"vibe/internal/fscopy"
	"vibe/internal/settings"
)

// File is the preferences' file, in a profile's folder.
const File = "user-preferences.yaml"

// Prefs are the choices. A key this vibe doesn't know (a newer one wrote it)
// is kept as it is when the others change.
type Prefs struct {
	// Theme is the Claude theme the profile's sandbox starts with: "default"
	// or a theme's name.
	Theme string `yaml:"theme,omitempty"`
	// GitAuthor says whether a sandbox with no git author is given the user's:
	// "import" or "leave"; unset asks.
	GitAuthor string `yaml:"gitAuthor,omitempty"`
	// Model and Effort are what the conversation asks the agent for; unset,
	// the agent's own settings decide.
	Model  string `yaml:"model,omitempty"`
	Effort string `yaml:"effort,omitempty"`

	Others map[string]interface{} `yaml:",inline"`
}

const header = `# Choices made through vibe for this profile. Safe to edit: delete a line, or
# the whole file, to go back to being asked (or to the default).
`

// Dead code to remove, with everything that names these files, two releases
// after the one that moved the choices here: see "Dead code to remove" in
// docs/plan-user-preferences.md for the list. LegacyFiles are where earlier
// vibes kept a choice of their own, one line
// each: the theme and the git author answer. They are still read, for what
// the new file doesn't say, and moved into it the first time it is changed.
var LegacyFiles = []string{legacyTheme, legacyGitAuthor}

const (
	legacyTheme     = "claude-theme"
	legacyGitAuthor = "git-author"
)

// Path is the preferences' file in the profile folder dir.
func Path(dir string) string { return filepath.Join(dir, File) }

// Load reads the preferences in profile folder dir: none, when there is no
// file. Choices only an old file has are read too.
func Load(dir string) (Prefs, error) {
	p, err := loadWithLegacy(dir)
	return p, err
}

// loadNew reads the new file alone.
func loadNew(dir string) (Prefs, error) {
	data, err := fscopy.ReadFile(Path(dir))
	if os.IsNotExist(err) {
		return Prefs{}, nil
	}
	if err != nil {
		return Prefs{}, err
	}
	var p Prefs
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Prefs{}, fmt.Errorf("can't read %s: %w", Path(dir), err)
	}
	return p, nil
}

// loadWithLegacy is Load, with what the old files say filling what the file
// doesn't.
func loadWithLegacy(dir string) (Prefs, error) {
	p, err := loadNew(dir)
	if err != nil {
		return Prefs{}, err
	}
	for _, f := range []struct {
		file string
		into *string
	}{{legacyTheme, &p.Theme}, {legacyGitAuthor, &p.GitAuthor}} {
		if *f.into != "" {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(dir, f.file)); err == nil {
			*f.into = strings.TrimSpace(string(data))
		}
	}
	return p, nil
}

// Update changes the preferences in profile folder dir with change, and keeps
// them whole or not at all (a turn reads this as it starts). Only the
// preferences that change are touched, so what else the file has stays as the
// user wrote it: a key this vibe doesn't know, and comments. A file that can't
// be read is an error, not written over: the user may have been editing it. A
// folder that isn't there isn't made, as that would hide the real profile. With
// nothing left to keep, the file goes. What the old files held is moved into
// the file, and they go.
func Update(dir string, change func(*Prefs)) error {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("there is no folder %s to keep the preferences in", dir)
	}
	current, err := loadNew(dir)
	if err != nil {
		return err
	}
	want, err := loadWithLegacy(dir)
	if err != nil {
		return err
	}
	change(&want)
	data, err := fscopy.ReadFile(Path(dir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	created := len(data) == 0
	for _, k := range []struct {
		key        string
		have, want string
	}{
		{"theme", current.Theme, want.Theme},
		{"gitAuthor", current.GitAuthor, want.GitAuthor},
		{"model", current.Model, want.Model},
		{"effort", current.Effort, want.Effort},
	} {
		if k.have == k.want {
			continue
		}
		if k.want == "" {
			data, err = settings.Unset(data, k.key)
		} else {
			data, err = settings.Set(data, k.key, k.want)
		}
		if err != nil {
			return fmt.Errorf("updating %s: %w", Path(dir), err)
		}
	}
	var left map[string]interface{}
	if err := yaml.Unmarshal(data, &left); err != nil {
		return fmt.Errorf("updating %s: %w", Path(dir), err)
	}
	switch {
	case len(left) == 0:
		if err := os.Remove(Path(dir)); err != nil && !os.IsNotExist(err) {
			return err
		}
	case created:
		if err := fscopy.WriteWholeMode(Path(dir), append([]byte(header), data...), 0o644); err != nil {
			return err
		}
	default:
		if err := fscopy.WriteWholeMode(Path(dir), data, modeOf(Path(dir))); err != nil {
			return err
		}
	}
	for _, old := range LegacyFiles {
		if err := os.Remove(filepath.Join(dir, old)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("the preferences are kept in %s, but the old %s couldn't be removed, and would be read again: %w", File, old, err)
		}
	}
	return nil
}

// Migrate moves what the old files held into the file; with none, it does
// nothing, so a file as the user wrote it is left as it is.
func Migrate(dir string) error {
	for _, old := range LegacyFiles {
		if _, err := os.Stat(filepath.Join(dir, old)); err == nil {
			return Update(dir, func(*Prefs) {})
		}
	}
	return nil
}

// modeOf is the mode the file has, which a change keeps (the user may have made
// it private); readable by others for one not there yet.
func modeOf(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0o644
}
