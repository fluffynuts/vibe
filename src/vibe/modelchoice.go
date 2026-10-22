package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vibe/internal/companion"
	"vibe/internal/fscopy"
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
	// Whole or not at all: a turn reads this file as it starts, and a file
	// being rewritten in place can be read empty, which would mean the default
	// model, with no word of it. The mode it had stays; a new one is for others to read.
	perm := os.FileMode(0o644)
	if info, err := os.Stat(p.path()); err == nil {
		perm = info.Mode().Perm()
	}
	return fscopy.WriteWholeMode(p.path(), data, perm)
}

// askAgent runs script, as the sandbox's agent user, and gives what it wrote.
type askAgent func(ctx context.Context, script string) (string, error)

// What the sandbox's claude may answer with, at most: the agent in the
// sandbox can put another program where claude is, and what it says is held
// in memory, kept in a file and sent to the page. Real answers are a few KB
// (the help 22, the list 1, the names about 15).
const (
	maxAnswer      = 64 << 10
	maxHelp        = 256 << 10
	maxNamesAnswer = 256 << 10
	// maxName is the longest name of a model that's taken.
	maxName = 100
)

// within asks script and refuses an answer larger than limit, as nothing real
// is.
func within(ctx context.Context, ask askAgent, script string, limit int, what string) (string, error) {
	out, err := ask(ctx, script)
	if err != nil {
		return "", err
	}
	if len(out) > limit {
		return "", fmt.Errorf("claude's answer for %s is larger (%d bytes) than any real one (at most %d): not used", what, len(out), limit)
	}
	return out, nil
}

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
	help, err := within(ctx, ask, in("claude --help"), maxHelp, "--help")
	if err != nil {
		return companion.Options{}, fmt.Errorf("asking claude for its --help: %w", err)
	}
	efforts, err := modelchoice.ParseEfforts(help)
	if err != nil {
		return companion.Options{}, err
	}
	answer, err := within(ctx, ask, in("claude -p /model --output-format json --no-session-persistence"), maxAnswer, "/model")
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
	if models.Dropped > 0 {
		note("claude lists more models than expected (or names far too long to be one): %d left out, the rest are offered", models.Dropped)
	}
	opts := companion.Options{Models: models.Plain, OtherModels: models.Other, Efforts: efforts, Current: models.Current, Rules: models.Rules}
	// The versions are what the page shows beside each, but the lists are
	// good without them: so a failure to ask is said, not made the lists'.
	if opts.Names, err = modelNames(ctx, ask, in, append(append([]string{}, models.Plain...), models.Other...)); err != nil {
		note("couldn't ask claude what the models are called, so the lists show their aliases: %v", err)
	}
	return opts, nil
}

// modelNames asks claude, in one command in the sandbox, what each alias
// stands for ("opus" is "Opus 5.5"): a /model prompt answers that itself, with
// no model. What can't be read is left out of the answer.
func modelNames(ctx context.Context, ask askAgent, in func(string) string, aliases []string) (map[string]string, error) {
	quoted := make([]string, len(aliases))
	for i, a := range aliases {
		quoted[i] = shellQuote(a)
	}
	out, err := within(ctx, ask, in(`for m in `+strings.Join(quoted, " ")+`; do printf '=== %s\n' "$m"; claude -p "/model $m" --output-format json --no-session-persistence </dev/null; echo; done`), maxNamesAnswer, "the names")
	if err != nil {
		return nil, err
	}
	asked := map[string]bool{}
	for _, a := range aliases {
		asked[a] = true
	}
	names := map[string]string{}
	for _, section := range strings.Split(out, "=== ")[1:] {
		alias, body, _ := strings.Cut(section, "\n")
		alias = strings.TrimSpace(alias)
		var said struct {
			Result string `json:"result"`
		}
		// Only what was asked, and a name that could be one: the answer is the
		// agent's to write.
		if !asked[alias] || json.Unmarshal([]byte(strings.TrimSpace(body)), &said) != nil {
			continue
		}
		if name, err := modelchoice.ParseModelName(said.Result); err == nil && len(name) <= maxName {
			names[alias] = name
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("none of its %d answers named a model", len(aliases))
	}
	return names, nil
}

// keptOptionsVersion is the layout of the file the agent's lists are kept in.
const keptOptionsVersion = 1

// keptOptionsFile is what is kept of the lists between runs: so the page has
// them to show at once, while the agent is asked again.
type keptOptionsFile struct {
	Version int               `json:"version"`
	At      time.Time         `json:"at"`
	Options companion.Options `json:"options"`
}

// keptOptions is how the agent's lists are kept in file, for a sandbox: what
// was last remembered, and a way to remember. It is only a cache, so a file
// that can't be read is said, and not used, and the agent is asked as if
// nothing were kept.
func keptOptions(file string, now func() time.Time) (cached func() (companion.Options, time.Time, bool), remember func(companion.Options) error) {
	cached = func() (companion.Options, time.Time, bool) {
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			return companion.Options{}, time.Time{}, false
		}
		var kept keptOptionsFile
		if err == nil {
			err = json.Unmarshal(data, &kept)
		}
		if err == nil && kept.Version != keptOptionsVersion {
			err = fmt.Errorf("it is version %d, and this vibe reads version %d", kept.Version, keptOptionsVersion)
		}
		if err == nil && len(kept.Options.Models) == 0 {
			err = fmt.Errorf("it lists no models")
		}
		if err != nil {
			note("the list of models kept in %s can't be used (%v): the agent is asked afresh", file, err)
			return companion.Options{}, time.Time{}, false
		}
		// Kept in whatever order it was in then.
		kept.Options.Models, kept.Options.OtherModels = modelchoice.BySize(kept.Options.Models), modelchoice.BySize(kept.Options.OtherModels)
		// Said by the list itself since lists kept earlier were made.
		kept.Options.Rules = modelchoice.RulesAmong(kept.Options.OtherModels)
		return kept.Options, kept.At, true
	}
	remember = func(opts companion.Options) error {
		data, err := json.Marshal(keptOptionsFile{Version: keptOptionsVersion, At: now(), Options: opts})
		if err != nil {
			return err
		}
		return fscopy.WriteWhole(file, data)
	}
	return cached, remember
}

// sandboxChoice is how the page chooses the model and effort for sandbox
// name's claude, working in workspace: kept in the profile, and the lists
// asked of the claude in the sandbox.
func sandboxChoice(name, workspace string, kept profileChoice, optionsFile string) *companion.Choice {
	cached, remember := keptOptions(optionsFile, time.Now)
	ask := func(ctx context.Context, script string) (string, error) {
		return sbxrun.ExecStdout(ctx, name, asAgent(script)...)
	}
	return &companion.Choice{
		Get: func() (string, string, error) {
			c, err := kept.get()
			return c.Model, c.Effort, err
		},
		Own: func() (string, string, error) {
			c, err := kept.own()
			return c.Model, c.Effort, err
		},
		Set: kept.set,
		Options: func(ctx context.Context) (companion.Options, error) {
			return agentOptions(ctx, ask, workspace)
		},
		Cached:   cached,
		Remember: remember,
	}
}
