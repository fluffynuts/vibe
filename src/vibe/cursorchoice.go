package main

import (
	"context"
	"fmt"
	"time"

	"vibe/internal/companion"
	"vibe/internal/cursormodels"
	"vibe/internal/fscopy"
	"vibe/internal/modelchoice"
	"vibe/internal/pathresolve"
	"vibe/internal/sbxrun"
)

// cursorChoiceNote is what the page says of choosing: a model asked for with
// --model is kept by Cursor as its own.
const cursorChoiceNote = "A model chosen here is also the one Cursor uses in its own terminal from then on."

// maxCursorList is the most Cursor's list may be: it is a few hundred lines.
const maxCursorList = 512 << 10

// cursorChoice is how the page chooses the model and effort for sandbox
// Cursor, working in workspace: kept in the profile, as the id of Cursor's
// own model (which has the effort in it: see cursormodels), and the lists
// asked of the Cursor in the sandbox.
//
// What the profile holds can be put there by another agent, or by hand, so a
// model that isn't in Cursor's list is neither shown as chosen nor run.
func cursorChoice(ask askAgent, workspace string, kept profileChoice, optionsFile, listFile string, plan *cursorPlan) *companion.Choice {
	cached, remember := keptOptionsTidied(optionsFile, time.Now, func(*companion.Options) {})
	// shown is a stored model as the page has it: its family and effort.
	shown := func(c turnChoice) (string, string) {
		if c.Model == "" {
			return "", ""
		}
		if cat, ok := loadCursorList(listFile); ok && !cat.Has(c.Model) {
			return "", ""
		}
		return cursormodels.Split(c.Model)
	}
	return &companion.Choice{
		Get: func() (string, string, error) {
			c, err := kept.get()
			model, effort := shown(c)
			return model, effort, err
		},
		Own: func() (string, string, error) {
			c, err := kept.own()
			model, effort := shown(c)
			return model, effort, err
		},
		Set: func(model, effort string) error {
			if model == "" {
				return kept.set("", "")
			}
			cat, ok := loadCursorList(listFile)
			if !ok {
				return fmt.Errorf("Cursor's list of models hasn't been read yet, so there is nothing to choose from")
			}
			id, ok := cat.ID(model, effort)
			if !ok {
				return fmt.Errorf("Cursor has no %q with %q", model, effort)
			}
			return kept.set(id, "")
		},
		Options: func(ctx context.Context) (companion.Options, error) {
			return cursorOptions(ctx, ask, workspace, listFile)
		},
		Cached:   cached,
		Remember: remember,
		Limits:   func() *companion.Limits { return cursorLimits(plan, listFile) },
		Unlock:   plan.forget,
	}
}

// cursorOnlyAutoWhy is what the page says of the models out of reach.
const cursorOnlyAutoWhy = "Your Cursor plan can only use Auto: a prompt on another model was refused."

// cursorLimits are the models out of reach, as learned: every one but Auto
// while the plan is believed to run only that (see cursorPlan), named from
// the list kept. None when nothing is believed, or the list isn't known (the
// page has no models to mark then either).
func cursorLimits(plan *cursorPlan, listFile string) *companion.Limits {
	since, only := plan.onlyAuto()
	if !only {
		return nil
	}
	cat, ok := loadCursorList(listFile)
	if !ok {
		return nil
	}
	limits := &companion.Limits{Unavailable: []string{}, Why: cursorOnlyAutoWhy, Since: since.UnixMilli()}
	for _, f := range cat.Families {
		if f.Key != cursorAuto {
			limits.Unavailable = append(limits.Unavailable, f.Key)
		}
	}
	return limits
}

// loadCursorList is what Cursor's list of models, as kept in file, makes: not
// ok when none is kept, or what is kept has no model in it.
func loadCursorList(file string) (cursormodels.Catalogue, bool) {
	data, err := fscopy.ReadFile(file)
	if err != nil {
		return cursormodels.Catalogue{}, false
	}
	entries, err := cursormodels.Parse(string(data))
	if err != nil {
		return cursormodels.Catalogue{}, false
	}
	return cursormodels.Build(entries), true
}

// cursorOptions asks the Cursor in the sandbox which models it has, in the
// folder the sandbox works in, keeps what it said in listFile, and gives the
// page's lists: a model for each family, and the efforts each takes. A list
// with no model in it is an error: a Cursor that isn't signed in says so
// there, and that is said, not shown as having no models.
func cursorOptions(ctx context.Context, ask askAgent, workspace, listFile string) (companion.Options, error) {
	command := cursorPrelude + `"$bin" --list-models`
	if workspace != "" {
		command = "cd " + shellQuote(workspace) + " && " + command
	}
	text, err := within(ctx, ask, command, maxCursorList, "--list-models")
	if err != nil {
		return companion.Options{}, fmt.Errorf("asking Cursor which models it has: %w", err)
	}
	entries, err := cursormodels.Parse(text)
	if err != nil {
		return companion.Options{}, err
	}
	if err := fscopy.WriteWhole(listFile, []byte(text)); err != nil {
		return companion.Options{}, fmt.Errorf("the list of models couldn't be kept (so a choice couldn't be turned into one): %w", err)
	}
	cat := cursormodels.Build(entries)
	opts := companion.Options{
		OnlyListed:     true,
		Note:           cursorChoiceNote,
		Efforts:        cat.AllEfforts(),
		EffortsByModel: map[string][]string{},
		EffortNames:    map[string]string{},
		Names:          map[string]string{},
		Current:        cat.Current,
	}
	for _, f := range cat.Families {
		opts.Models = append(opts.Models, f.Key)
		opts.Names[f.Key] = f.Name
		opts.EffortsByModel[f.Key] = append([]string{}, cat.Efforts(f.Key)...)
	}
	for _, e := range opts.Efforts {
		opts.EffortNames[e] = cursormodels.EffortLabel(e)
	}
	return opts, nil
}

// cursorTurnModel is the model a turn asks Cursor for: the id the profile
// holds, when Cursor has it, else none, which leaves Cursor to its own. One
// that can't be vouched for (not in the list, or the list isn't known yet)
// isn't sent. One that isn't safe to put on a command line is refused, naming
// the file to fix, as claude's is.
func cursorTurnModel(kept profileChoice, listFile string) func() (string, error) {
	return func() (string, error) {
		c, err := kept.resolve()
		if err != nil {
			return "", err
		}
		if c.Model == "" {
			return "", nil
		}
		if err := modelchoice.CheckModel(c.Model); err != nil {
			return "", fmt.Errorf("the model chosen in %s can't be used (nothing was sent): %w", pathresolve.Tilde(c.modelFrom), err)
		}
		if cat, ok := loadCursorList(listFile); ok && cat.Has(c.Model) {
			return c.Model, nil
		}
		return "", nil
	}
}

// cursorAsk is how Cursor in sandbox name is asked for something: as the
// sandbox's agent user.
func cursorAsk(name string) askAgent {
	return func(ctx context.Context, script string) (string, error) {
		return sbxrun.ExecStdout(ctx, name, asAgent(script)...)
	}
}
