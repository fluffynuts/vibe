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
	"vibe/internal/userprefs"
)

// profileChoice is the model and effort the conversation tab asks the agent
// for, as the user chose them on the page: kept in the profile's
// user-preferences.yaml (see internal/userprefs), so they outlive vibe, the
// page and a rebuild, and aren't part of what composing the profile wrote.
// What applies is the profile's preferences, over the profile's settings.yaml,
// over the global one: the page's own choice is the newest word, and a
// settings.yaml is the default under it.
type profileChoice struct {
	lay     layout.Layout
	profile string
}

func (p profileChoice) dir() string { return p.lay.ProfileDir(p.profile) }

func (p profileChoice) settingsPath() string { return filepath.Join(p.dir(), settingsFile) }

// chosen is a choice, and where each part of it comes from.
type chosen struct {
	turnChoice
	modelFrom, effortFrom string
}

// resolve works out the choice that applies, from the profile's preferences,
// the profile's settings and the global ones.
func (p profileChoice) resolve() (chosen, error) {
	global, err := settings.Load(p.lay.File(settingsFile))
	if err != nil {
		return chosen{}, err
	}
	own, err := settings.Load(p.settingsPath())
	if err != nil {
		return chosen{}, err
	}
	prefs, err := userprefs.Load(p.dir())
	if err != nil {
		return chosen{}, err
	}
	pick := func(fromPrefs, fromProfile, fromGlobal string) (value, from string) {
		switch {
		case fromPrefs != "":
			return fromPrefs, userprefs.Path(p.dir())
		case fromProfile != "":
			return fromProfile, p.settingsPath()
		}
		return fromGlobal, p.lay.File(settingsFile)
	}
	var out chosen
	out.Model, out.modelFrom = pick(prefs.Model, own.Model, global.Model)
	out.Effort, out.effortFrom = pick(prefs.Effort, own.Effort, global.Effort)
	return out, nil
}

// own is the choice the profile itself holds, in its preferences or its
// settings.yaml: what the global settings don't decide.
func (p profileChoice) own() (turnChoice, error) {
	own, err := settings.Load(p.settingsPath())
	if err != nil {
		return turnChoice{}, err
	}
	prefs, err := userprefs.Load(p.dir())
	if err != nil {
		return turnChoice{}, err
	}
	return turnChoice{Model: firstNonEmpty(prefs.Model, own.Model), Effort: firstNonEmpty(prefs.Effort, own.Effort)}, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// get is the choice that applies.
func (p profileChoice) get() (turnChoice, error) {
	c, err := p.resolve()
	return c.turnChoice, err
}

// forTurn is get, refused when what it holds isn't safe to put on a command
// line: the files are the user's to edit, and a mistake there is said, with
// which file to fix it in, rather than run.
func (p profileChoice) forTurn() (turnChoice, error) {
	c, err := p.resolve()
	if err != nil {
		return turnChoice{}, err
	}
	if err := modelchoice.CheckModel(c.Model); err != nil {
		return turnChoice{}, fmt.Errorf("the model chosen in %s can't be used (nothing was sent): %w", pathresolve.Tilde(c.modelFrom), err)
	}
	if err := modelchoice.CheckEffortName(c.Effort); err != nil {
		return turnChoice{}, fmt.Errorf("the effort chosen in %s can't be used (nothing was sent): %w", pathresolve.Tilde(c.effortFrom), err)
	}
	return c.turnChoice, nil
}

// set keeps model and effort in the profile's user preferences, as it takes
// what is there out when it is empty (the agent's default), and anything the
// profile's settings.yaml says of it too, which would otherwise keep a choice
// the user has just taken away. A choice earlier vibes kept in settings.yaml is
// moved first. It never makes a profile: a directory for one that isn't there
// would hide the real one, wherever it is.
func (p profileChoice) set(model, effort string) error {
	if !p.lay.ProfileExists(p.profile) {
		return fmt.Errorf("profile '%s' isn't there to keep the choice in", p.profile)
	}
	if dir := p.lay.HomePath("profiles", p.profile); dir == "" || !fileExists(dir) {
		return fmt.Errorf("profile '%s' is vibe's own, which can't be written to: make a profile of your own (--profile) to keep the choice in", p.profile)
	}
	migratePreferences(p.lay, p.profile)
	// First, so that a settings.yaml that can't be changed leaves the choice as
	// it was, as the failure says, and not half made.
	if err := p.dropFromSettings(model == "", effort == ""); err != nil {
		return err
	}
	return userprefs.Update(p.dir(), func(u *userprefs.Prefs) { u.Model, u.Effort = model, effort })
}

// dropFromSettings takes the keys out of the profile's settings.yaml that the
// user has just chosen the default over, if they are there (migrating them
// left them, as taking them out would have left the profile looking edited by
// hand: but choosing the default is the user asking for them gone).
func (p profileChoice) dropFromSettings(model, effort bool) error {
	data, err := fscopy.ReadFile(p.settingsPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	own, err := settings.Parse(data)
	if err != nil {
		return fmt.Errorf("can't read %s: %w", pathresolve.Tilde(p.settingsPath()), err)
	}
	changed := false
	for _, kv := range []struct {
		key  string
		drop bool
	}{{"model", model && own.Model != ""}, {"effort", effort && own.Effort != ""}} {
		if !kv.drop {
			continue
		}
		if data, err = settings.Unset(data, kv.key); err != nil {
			return fmt.Errorf("updating %s: %w", pathresolve.Tilde(p.settingsPath()), err)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	if text := strings.TrimSpace(string(data)); text == "{}" || text == "" {
		return os.Remove(p.settingsPath())
	}
	perm := os.FileMode(0o644)
	if info, err := os.Stat(p.settingsPath()); err == nil {
		perm = info.Mode().Perm()
	}
	return fscopy.WriteWholeMode(p.settingsPath(), data, perm)
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
	return keptOptionsTidied(file, now, func(opts *companion.Options) {
		// Kept in whatever order it was in then.
		opts.Models, opts.OtherModels = modelchoice.BySize(opts.Models), modelchoice.BySize(opts.OtherModels)
		// Said by the list itself since lists kept earlier were made.
		opts.Rules = modelchoice.RulesAmong(opts.OtherModels)
	})
}

// keptOptionsTidied is keptOptions for an agent whose lists are tidied as it
// says, on the way out of the file.
func keptOptionsTidied(file string, now func() time.Time, tidy func(*companion.Options)) (cached func() (companion.Options, time.Time, bool), remember func(companion.Options) error) {
	cached = func() (companion.Options, time.Time, bool) {
		data, err := fscopy.ReadFile(file)
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
		tidy(&kept.Options)
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
