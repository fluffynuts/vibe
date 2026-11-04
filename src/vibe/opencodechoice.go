package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"vibe/internal/companion"
	"vibe/internal/fscopy"
	"vibe/internal/modelchoice"
	"vibe/internal/opencodemodels"
	"vibe/internal/pathresolve"
	"vibe/internal/sbxrun"
)

// opencodeChoiceNote is what the page says of choosing models for OpenCode.
const opencodeChoiceNote = "OpenCode lists the models of every provider the sandbox has a key for, and its free ones. A model needs its provider's key, stored on your machine with: sbx secret set <provider>."

// maxOpencodeList is the most OpenCode's verbose list may be: a few KB for each of a few hundred models.
const maxOpencodeList = 8 << 20

// maxOpencodeConfig is the most the printed config may be.
const maxOpencodeConfig = 1 << 20

// opencodeFreeGroup is the heading over the models the sandbox has no key for.
const opencodeFreeGroup = "Needs a key"

// opencodeWhy is what the page says of them: which providers' keys are missing, and which keys sbx does have, so a
// mistaken reading of sbx can be told from a real lack.
func opencodeWhy(missing, have, login []string) string {
	has := "sbx has no keys stored for this sandbox"
	if len(have) > 0 {
		has = "sbx has keys for: " + strings.Join(have, ", ")
	}
	why := fmt.Sprintf("These models need a key, and sbx has no key for %s (%s). Store one on your machine with: sbx secret set <provider>, then choose Check again.", strings.Join(missing, ", "), has)
	for _, service := range missing {
		if slices.Contains(login, service) {
			why += fmt.Sprintf(" %s is signed in with OAuth, which only the agent it was made for can use: OpenCode can't use it as a key.", service)
		}
	}
	return why
}

// opencodeProviderServices is the sbx service whose secret is the key for an OpenCode provider: only a provider here
// can be told to have no key. One that isn't (a local model, say) can't be known to have none, and nor can OpenCode's
// own paid models, whose key is OpenCode's sign-in.
var opencodeProviderServices = map[string]string{
	"anthropic": "anthropic", "openai": "openai", "google": "google", "groq": "groq", "mistral": "mistral",
	"openrouter": "openrouter", "xai": "xai", "nebius": "nebius", "github-copilot": "copilot",
}

// opencodeTurn is the model and variant (OpenCode's word for effort) a turn asks for.
type opencodeTurn struct{ Model, Variant string }

// opencodeKeys is which providers sbx has a key for, for a sandbox: read from sbx, kept for a short while, and read
// again when the user says to check again. What can't be read is not known, and nothing is then said to lack a key.
type opencodeKeys struct {
	sandbox string
	read    func(ctx context.Context) (string, error)
	now     func() time.Time

	mu       sync.Mutex
	services []string
	logins   []string
	known    bool
	at       time.Time
}

// opencodeKeysFor is how long what sbx said of keys is taken to be right.
const opencodeKeysFor = time.Minute

func newOpencodeKeys(sandbox string, read func(ctx context.Context) (string, error), now func() time.Time) *opencodeKeys {
	return &opencodeKeys{sandbox: sandbox, read: read, now: now}
}

// has says whether sbx has a key for the service: known false when it can't be told.
func (k *opencodeKeys) has(service string) (has, known bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.at.IsZero() || k.now().Sub(k.at) > opencodeKeysFor {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		text, err := k.read(ctx)
		k.at = k.now()
		k.known = err == nil
		k.services, k.logins = nil, nil
		if err == nil {
			k.services, k.logins = sbxrun.SecretKinds(text, k.sandbox)
		}
	}
	return slices.Contains(k.services, service), k.known
}

// stored are the services sbx has a key for, and those it has only a login for, as last read.
func (k *opencodeKeys) stored() (keys, logins []string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.services), slices.Clone(k.logins)
}

// forget is the user saying to check again: what was read is read again.
func (k *opencodeKeys) forget() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.at = time.Time{}
	return nil
}

// opencodeChoice is how the page chooses the model and effort for sandbox OpenCode, working in workspace: kept in
// the profile, as a provider/model and the variant, and the lists asked of the OpenCode in the sandbox.
//
// What the profile holds can be put there by another agent, or by hand, so a model that isn't in OpenCode's list is
// neither shown as chosen nor run.
func opencodeChoice(ask askAgent, workspace string, kept profileChoice, optionsFile, listFile, defaultFile string, keys *opencodeKeys) *companion.Choice {
	cached, remember := keptOptionsTidied(optionsFile, time.Now, func(*companion.Options) {})
	// shown is a stored choice as the page has it: the model if OpenCode has it, and the effort if the model takes it.
	shown := func(c turnChoice) (string, string) {
		cat, ok := loadOpencodeList(listFile)
		if !ok || c.Model == "" || !cat.Has(c.Model) {
			return "", ""
		}
		if c.Effort != "" && !slices.Contains(cat.Efforts(c.Model), c.Effort) {
			return c.Model, ""
		}
		return c.Model, c.Effort
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
			cat, ok := loadOpencodeList(listFile)
			if !ok {
				return fmt.Errorf("OpenCode's list of models hasn't been read yet, so there is nothing to choose from")
			}
			if !cat.Has(model) {
				return fmt.Errorf("OpenCode has no model %q", model)
			}
			if err := modelchoice.CheckEffort(effort, cat.Efforts(model)); err != nil {
				return err
			}
			return kept.set(model, effort)
		},
		Options: func(ctx context.Context) (companion.Options, error) {
			return opencodeOptions(ctx, ask, workspace, listFile, defaultFile)
		},
		Cached:   cached,
		Remember: remember,
		Limits:   func() *companion.Limits { return opencodeLimits(keys, listFile) },
		Unlock:   keys.forget,
	}
}

// opencodeLimits are the models the sandbox has no key for: those that cost something, of a provider sbx holds a
// key for as a service, when sbx has none. None when every model has what it needs, when the list isn't known, and
// when sbx can't be asked (nothing is then said to be out of reach: a wrong "no key" would stop the user choosing a
// model they can use).
func opencodeLimits(keys *opencodeKeys, listFile string) *companion.Limits {
	cat, ok := loadOpencodeList(listFile)
	if !ok {
		return nil
	}
	var out, missing []string
	for _, key := range cat.Keys() {
		if cat.Free(key) {
			continue
		}
		service, ok := opencodeProviderServices[cat.Provider(key)]
		if !ok {
			continue
		}
		has, known := keys.has(service)
		if !known {
			return nil
		}
		if !has {
			out = append(out, key)
			if !slices.Contains(missing, service) {
				missing = append(missing, service)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	have, logins := keys.stored()
	return &companion.Limits{Unavailable: out, Why: opencodeWhy(missing, have, logins), Group: opencodeFreeGroup}
}

// loadOpencodeList is what OpenCode's list of models, as kept in file, makes: not ok when none is kept, or what is
// kept has no model in it.
func loadOpencodeList(file string) (opencodemodels.Catalogue, bool) {
	data, err := fscopy.ReadFile(file)
	if err != nil {
		return opencodemodels.Catalogue{}, false
	}
	entries, err := opencodemodels.Parse(string(data))
	if err != nil {
		return opencodemodels.Catalogue{}, false
	}
	return opencodemodels.Build(entries), true
}

// opencodeOptions asks the OpenCode in the sandbox which models it has, in the folder the sandbox works in, keeps
// what it said in listFile, and gives the page's lists: each model, and the efforts it takes. A list with no model in
// it is an error, said, not shown as having no models.
func opencodeOptions(ctx context.Context, ask askAgent, workspace, listFile, defaultFile string) (companion.Options, error) {
	text, err := within(ctx, ask, opencodeInFolder(workspace, "opencode models --verbose"), maxOpencodeList, "models --verbose")
	if err != nil {
		return companion.Options{}, fmt.Errorf("asking OpenCode which models it has: %w", err)
	}
	entries, err := opencodemodels.Parse(text)
	if err != nil {
		return companion.Options{}, err
	}
	if err := fscopy.WriteWhole(listFile, []byte(text)); err != nil {
		return companion.Options{}, fmt.Errorf("the list of models couldn't be kept (so a choice couldn't be checked against it): %w", err)
	}
	cat := opencodemodels.Build(entries)
	opts := companion.Options{
		OnlyListed:     true,
		Note:           opencodeChoiceNote,
		Efforts:        cat.AllEfforts(),
		EffortsByModel: map[string][]string{},
		EffortNames:    map[string]string{},
		Names:          map[string]string{},
	}
	// What runs with nothing chosen: the model the sandbox's config names, else the free one.
	defaultModel, ok := opencodeFetchDefault(ctx, ask, workspace, defaultFile)
	if !ok {
		defaultModel = opencodeAskDefault(ctx, nil, workspace, defaultFile)
	}
	switch {
	case defaultModel != "" && cat.Has(defaultModel):
		opts.Current = defaultModel
	case cat.Has(opencodeFreeModel):
		opts.Current = opencodeFreeModel
	}
	for _, key := range cat.Keys() {
		opts.Models = append(opts.Models, key)
		name := cat.Name(key)
		if provider := cat.Provider(key); provider != "opencode" {
			name += " (" + provider + ")"
		}
		opts.Names[key] = name
		opts.EffortsByModel[key] = cat.Efforts(key)
	}
	for _, e := range opts.Efforts {
		opts.EffortNames[e] = opencodemodels.EffortLabel(e)
	}
	return opts, nil
}

// opencodeDefaultModel is the model a turn runs when none is chosen: the one the sandbox's OpenCode config names, when
// it is a model OpenCode lists, else the free model. A model the config names is the user's own choice (a local
// model, say: its code is not for a hosted one). cat is the list, known only if listed.
func opencodeDefaultModel(ask askAgent, workspace, defaultFile string, cat opencodemodels.Catalogue, listed bool) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	name := opencodeAskDefault(ctx, ask, workspace, defaultFile)
	if name != "" && (!listed || cat.Has(name)) {
		return name
	}
	return opencodeFreeModel
}

// opencodeFetchDefault asks the sandbox's OpenCode which model its config names as the default (opencode debug config
// prints the config as it resolves, every file in it), and keeps the answer in file: nothing in the file when the config
// names none. ok is false when it couldn't be asked or the answer couldn't be kept.
func opencodeFetchDefault(ctx context.Context, ask askAgent, workspace, file string) (name string, ok bool) {
	if ask == nil {
		return "", false
	}
	text, err := within(ctx, ask, opencodeInFolder(workspace, "opencode debug config"), maxOpencodeConfig, "debug config")
	if err != nil {
		return "", false
	}
	var config struct {
		Model string `json:"model"`
	}
	if json.Unmarshal([]byte(text), &config) == nil && modelchoice.CheckModel(config.Model) == nil {
		name = config.Model
	}
	return name, fscopy.WriteWhole(file, []byte(name)) == nil
}

// opencodeAskDefault is the default model as kept in file, asked of the sandbox first when none is kept. "" when there
// is none, or it can't be told: the free model is then the default.
func opencodeAskDefault(ctx context.Context, ask askAgent, workspace, file string) string {
	if data, err := fscopy.ReadFile(file); err == nil {
		return strings.TrimSpace(string(data))
	}
	name, _ := opencodeFetchDefault(ctx, ask, workspace, file)
	return name
}

// opencodeInFolder is command run in the folder the sandbox works in.
func opencodeInFolder(workspace, command string) string {
	if workspace == "" {
		return command
	}
	return "cd " + shellQuote(workspace) + " && " + command
}

// opencodeTurnChoice is the model and variant a turn asks OpenCode for: those the profile holds, when OpenCode has
// them, else the default (see opencodeDefaultModel), and no variant. One that can't be vouched for (not in the list, or the list isn't known
// yet) isn't sent. One that isn't safe to put on a command line is refused, naming the file to fix.
func opencodeTurnChoice(kept profileChoice, listFile, defaultFile string, ask askAgent, workspace string) func() (opencodeTurn, error) {
	return func() (opencodeTurn, error) {
		c, err := kept.resolve()
		if err != nil {
			return opencodeTurn{}, err
		}
		if err := modelchoice.CheckModel(c.Model); err != nil {
			return opencodeTurn{}, fmt.Errorf("the model chosen in %s can't be used (nothing was sent): %w", pathresolve.Tilde(c.modelFrom), err)
		}
		cat, ok := loadOpencodeList(listFile)
		if c.Model == "" || !ok || !cat.Has(c.Model) {
			return opencodeTurn{Model: opencodeDefaultModel(ask, workspace, defaultFile, cat, ok)}, nil
		}
		turn := opencodeTurn{Model: c.Model}
		if c.Effort != "" && slices.Contains(cat.Efforts(c.Model), c.Effort) {
			turn.Variant = c.Effort
		}
		return turn, nil
	}
}

// opencodeAsk is how OpenCode in sandbox name is asked for something: as the sandbox's agent user.
func opencodeAsk(name string) askAgent {
	return func(ctx context.Context, script string) (string, error) {
		return sbxrun.ExecStdout(ctx, name, asAgent(script)...)
	}
}
