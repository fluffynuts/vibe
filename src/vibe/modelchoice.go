package main

import (
	"fmt"
	"os"
	"path/filepath"

	"vibe/internal/layout"
	"vibe/internal/modelchoice"
	"vibe/internal/pathresolve"
	"vibe/internal/settings"
)

// profileChoice is the model and effort the conversation tab asks the agent
// for: kept as "model" and "effort" in the profile's own settings.yaml, as if
// the user had written them there, so they outlive vibe, the page and a
// rebuild, and the settings' usual layering gives a default for every profile
// later (a profile's own choice wins over it).
type profileChoice struct {
	lay     layout.Layout
	profile string
}

func (p profileChoice) path() string {
	return filepath.Join(p.lay.ProfileDir(p.profile), "settings.yaml")
}

// get is the choice that applies: the profile's own over the global settings'.
func (p profileChoice) get() (turnChoice, error) {
	global, err := settings.Load(p.lay.File("settings.yaml"))
	if err != nil {
		return turnChoice{}, err
	}
	own, err := settings.Load(p.path())
	if err != nil {
		return turnChoice{}, err
	}
	merged := settings.Merge(global, own)
	return turnChoice{Model: merged.Model, Effort: merged.Effort}, nil
}

// forTurn is get, refused when what it holds isn't safe to put on a command
// line: the file is the user's to edit, and a mistake there is said, with
// where to fix it, rather than run.
func (p profileChoice) forTurn() (turnChoice, error) {
	choice, err := p.get()
	if err != nil {
		return turnChoice{}, err
	}
	if err := modelchoice.CheckModel(choice.Model); err != nil {
		return turnChoice{}, fmt.Errorf("the model chosen in %s can't be used (nothing was sent): %w", pathresolve.Tilde(p.path()), err)
	}
	if err := modelchoice.CheckEffortName(choice.Effort); err != nil {
		return turnChoice{}, fmt.Errorf("the effort chosen in %s can't be used (nothing was sent): %w", pathresolve.Tilde(p.path()), err)
	}
	return choice, nil
}

// set keeps model and effort in the profile's settings.yaml, taking a key out
// when it is empty (the agent's default). It never makes a profile: a
// directory for one that isn't there would hide the real one, wherever it is.
func (p profileChoice) set(model, effort string) error {
	if !p.lay.ProfileExists(p.profile) {
		return fmt.Errorf("profile '%s' isn't there to keep the choice in", p.profile)
	}
	data, err := os.ReadFile(p.path())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) && model == "" && effort == "" {
		return nil
	}
	for _, kv := range []struct{ key, value string }{{"model", model}, {"effort", effort}} {
		if kv.value == "" {
			data, err = settings.Unset(data, kv.key)
		} else {
			data, err = settings.Set(data, kv.key, kv.value)
		}
		if err != nil {
			return fmt.Errorf("updating %s: %w", pathresolve.Tilde(p.path()), err)
		}
	}
	return os.WriteFile(p.path(), data, 0o644)
}
