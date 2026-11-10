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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	// Mode is how vibe starts for this profile when nothing typed decides it:
	// "headless" or "terminal"; unset, the settings' say.
	Mode string `yaml:"mode,omitempty"`

	Others map[string]interface{} `yaml:",inline"`
}

const header = `# Choices made through vibe for this profile. Safe to edit: delete a line, or
# the whole file, to go back to being asked (or to the default).
`

// Path is the preferences' file in the profile folder dir.
func Path(dir string) string { return filepath.Join(dir, File) }

// Load reads the preferences in profile folder dir: none, when there is no
// file.
func Load(dir string) (Prefs, error) {
	data, err := fscopy.ReadFile(Path(dir))
	if os.IsNotExist(err) {
		return Prefs{}, nil
	}
	if err != nil {
		return Prefs{}, err
	}
	return parse(dir, data)
}

func parse(dir string, data []byte) (Prefs, error) {
	var p Prefs
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Prefs{}, fmt.Errorf("can't read %s: %w", Path(dir), err)
	}
	return p, nil
}

// updating holds a lock for each folder being updated: several parts of vibe
// (the page's model choice, the git author question, the migration) change the
// same file from their own goroutines, and an update made from what was read
// before another landed would lose that one.
var updating sync.Map

// Update changes the preferences in profile folder dir with change, and keeps
// them whole or not at all (a turn reads this as it starts). Only the
// preferences that change are touched, so what else the file has stays as the
// user wrote it: a key this vibe doesn't know, and comments. A file that can't
// be read is an error, not written over: the user may have been editing it. A
// folder that isn't there isn't made, as that would hide the real profile. With
// nothing left to keep, the file goes.
func Update(dir string, change func(*Prefs)) error {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("there is no folder %s to keep the preferences in", dir)
	}
	lock, _ := updating.LoadOrStore(filepath.Clean(dir), &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	data, err := fscopy.ReadFile(Path(dir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	current, err := parse(dir, data)
	if err != nil {
		return err
	}
	want := current
	change(&want)
	original := data
	created := len(data) == 0
	for _, k := range []struct {
		key        string
		have, want string
	}{
		{"theme", current.Theme, want.Theme},
		{"gitAuthor", current.GitAuthor, want.GitAuthor},
		{"model", current.Model, want.Model},
		{"effort", current.Effort, want.Effort},
		{"mode", current.Mode, want.Mode},
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
	case !bytes.Equal(data, original):
		if err := fscopy.WriteWholeMode(Path(dir), data, modeOf(Path(dir))); err != nil {
			return err
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
