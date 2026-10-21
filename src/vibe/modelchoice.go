package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"vibe/internal/companion"
	"vibe/internal/layout"
	"vibe/internal/modelchoice"
	"vibe/internal/pathresolve"
	"vibe/internal/sbxrun"
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

// own is the choice the profile's own settings.yaml holds.
func (p profileChoice) own() (turnChoice, error) {
	own, err := settings.Load(p.path())
	return turnChoice{Model: own.Model, Effort: own.Effort}, err
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

// askAgent runs script, as the sandbox's agent user, and gives what it wrote.
type askAgent func(ctx context.Context, script string) (string, error)

// agentOptions asks the sandbox's claude which models and efforts it has: the
// efforts from its --help, and the models from a /model prompt, which it
// answers itself, without a model, so it costs nothing. They are asked in the
// folder the sandbox works in, as the turns are, so a project's own settings
// count. An answer that isn't what was expected is an error: a changed claude
// is said, not read as having no models.
func agentOptions(ctx context.Context, ask askAgent, workspace string) (companion.Options, error) {
	in := func(command string) string {
		if workspace == "" {
			return command
		}
		return "cd " + shellQuote(workspace) + " && " + command
	}
	help, err := ask(ctx, in("claude --help"))
	if err != nil {
		return companion.Options{}, fmt.Errorf("asking claude for its --help: %w", err)
	}
	efforts, err := modelchoice.ParseEfforts(help)
	if err != nil {
		return companion.Options{}, err
	}
	answer, err := ask(ctx, in("claude -p /model --output-format json --no-session-persistence"))
	if err != nil {
		return companion.Options{}, fmt.Errorf("asking claude which models it has: %w", err)
	}
	var said struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal([]byte(answer), &said); err != nil {
		return companion.Options{}, fmt.Errorf("claude's answer to /model wasn't what was expected (%v): %.120q", err, answer)
	}
	if said.IsError {
		return companion.Options{}, fmt.Errorf("claude answered /model with an error: %.200s", said.Result)
	}
	models, err := modelchoice.ParseModels(said.Result)
	if err != nil {
		return companion.Options{}, err
	}
	return companion.Options{Models: models.Plain, OtherModels: models.Other, Efforts: efforts, Current: models.Current}, nil
}

// sandboxChoice is how the page chooses the model and effort for sandbox
// name's claude, working in workspace: kept in the profile, and the lists
// asked of the claude in the sandbox.
func sandboxChoice(name, workspace string, kept profileChoice) *companion.Choice {
	ask := func(ctx context.Context, script string) (string, error) {
		return sbxrun.ExecStdout(ctx, name, asAgent(script)...)
	}
	return &companion.Choice{
		Get: func() (string, string, error) {
			c, err := kept.get()
			return c.Model, c.Effort, err
		},
		Set: kept.set,
		Options: func(ctx context.Context) (companion.Options, error) {
			return agentOptions(ctx, ask, workspace)
		},
	}
}
