package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"time"

	"vibe/internal/modelchoice"
)

// Choice is how the page chooses the model and effort the agent is asked to
// use (see docs/plan-model-effort.md): kept by vibe, read for every turn.
type Choice struct {
	// Get is what is chosen now; empty for either is the agent's own default.
	Get func() (model, effort string, err error)
	// Own is the part of Get that the profile itself holds, the rest coming
	// from a layer under it (the global settings), which Set can't take
	// away: the page says so when a choice is inherited. Nil means Get is all
	// the profile's.
	Own func() (model, effort string, err error)
	// Set keeps a choice, once the page has checked it (CheckModel and
	// CheckEffort in choiceSet): what's not well-formed never gets here.
	Set func(model, effort string) error
	// Options asks the agent which models and efforts it has: the page's
	// drop-downs are made of them, so they are as live as the agent is.
	Options func(ctx context.Context) (Options, error)
	// Cached is what was last remembered of the agent's lists, and when it
	// was asked, from an earlier run: the page has it to show at once, while
	// the agent is asked again. Nil means nothing is kept between runs.
	Cached func() (opts Options, at time.Time, ok bool)
	// Remember keeps the agent's lists for the next run.
	Remember func(Options) error
	// Limits is what the agent's plan can't run, as far as is known now: nil
	// when nothing is known to be out of reach, and for an agent that has no
	// such thing. Asked for each time the choice is read, since it is learned
	// as the agent is used and forgotten as time goes by.
	Limits func() *Limits
	// Unlock is the user saying the plan has changed (an upgrade, say): what
	// was learned of its limits is taken away, and learned again if it was
	// wrong. Nil for an agent that has no limits to speak of.
	Unlock func() error
}

// Limits are the models the agent's plan can't run, which the page shows as
// they are but won't let be chosen.
type Limits struct {
	// Unavailable are the listed models that can't be chosen.
	Unavailable []string `json:"unavailable"`
	// Why is said to the user of them.
	Why string `json:"why"`
	// Since is when it was learned, in milliseconds since the epoch.
	Since int64 `json:"since,omitempty"`
}

// Options are the agent's own lists, as the page offers them.
type Options struct {
	// Models are the plain names of models, offered at once; OtherModels
	// the rest, offered on request.
	Models      []string `json:"models"`
	OtherModels []string `json:"otherModels"`
	Efforts     []string `json:"efforts"`
	// OnlyListed says the agent takes only the models listed, so the page
	// offers no Other… to name one that isn't (as claude takes any full
	// model name).
	OnlyListed bool `json:"onlyListed,omitempty"`
	// Note is something the agent's choice needs the user to know, said in
	// the bubble beside the drop-downs.
	Note string `json:"note,omitempty"`
	// EffortsByModel, for an agent whose efforts differ from one model to the
	// next, are the efforts each model takes (none, for one that takes none:
	// an empty list, not a missing one); Efforts is then all of them, for what
	// can't be told by model. Nil for an agent whose efforts are the same for
	// every model.
	EffortsByModel map[string][]string `json:"effortsByModel,omitempty"`
	// EffortNames are what the page calls an effort ("High effort") where
	// that isn't just its name in title case.
	EffortNames map[string]string `json:"effortNames,omitempty"`
	// Names are what the agent calls each of those models ("Opus 5.5" for
	// "opus"), for the page to show their versions. A model it couldn't
	// say the name of isn't in it.
	Names map[string]string `json:"names,omitempty"`
	// DefaultAlias is which of the listed models is claude's default now (it is
	// worked out here, from Current and Names, so the page needn't): ""
	// when that can't be told.
	DefaultAlias string `json:"defaultAlias,omitempty"`
	// Rules are the listed names that pick a model by a rule ("best") and
	// aren't one model: what ran can't be held against them.
	Rules []string `json:"rules,omitempty"`
	// Current names the model the agent uses with no choice made, as it
	// says it.
	Current string `json:"current,omitempty"`
}

// optionsFreshFor is how long the agent's answer is taken to be right: its
// default model and its lists can change under a long-running vibe (claude
// picks up server-side settings as it runs), so after this the next ask goes
// to the agent again. Short enough to follow such a change within a session,
// long enough that asking (about a dozen quick local commands) is a rare cost.
const optionsFreshFor = 10 * time.Minute

// optionsWithin bounds asking the agent for its lists, past the sandbox
// being woken: three commands in the sandbox, the last of them one claude run
// for each model listed (about a dozen), all quick, and local to claude.
const optionsWithin = 2 * time.Minute

// choiceOptions is what the page has of the agent's lists: from an earlier
// run (kept, and perhaps out of date) until the agent has been asked in this
// one, and then fresh.
type choiceOptions struct {
	loaded sync.Once
	// asking has room for one: the page that is asking the agent holds it, and
	// a second waits for the answer rather than ask again.
	askOnce sync.Once
	asking  chan struct{}
	mu      sync.Mutex
	have    *Options
	at      time.Time
	fresh   bool
}

// startAsking takes the place of the one asking the agent, or gives up when
// ctx ends first.
func (o *choiceOptions) startAsking(ctx context.Context) bool {
	o.askOnce.Do(func() { o.asking = make(chan struct{}, 1) })
	select {
	case o.asking <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (o *choiceOptions) doneAsking() { <-o.asking }

func (o *choiceOptions) get() (opts *Options, at time.Time, fresh bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.have, o.at, o.fresh
}

func (o *choiceOptions) put(opts Options, at time.Time, fresh bool) {
	opts.DefaultAlias = modelchoice.DefaultAlias(opts.Current, opts.Models, opts.OtherModels, opts.Names)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.have, o.at, o.fresh = &opts, at, fresh
}

// keptOptions gives the lists, from the earlier run if the agent hasn't been
// asked in this one.
func (s *Server) keptOptions() (*Options, time.Time, bool) {
	s.options.loaded.Do(func() {
		if s.cfg.Choice != nil && s.cfg.Choice.Cached != nil {
			if opts, at, ok := s.cfg.Choice.Cached(); ok {
				s.options.put(opts, at, false)
			}
		}
	})
	return s.options.get()
}

// freshOptions is keptOptions, with fresh true only while the agent's answer
// is young enough (optionsFreshFor): one that is older is asked for again.
func (s *Server) freshOptions() (*Options, time.Time, bool) {
	opts, at, fresh := s.keptOptions()
	return opts, at, fresh && s.clock().Sub(at) < optionsFreshFor
}

// optionsBody is what the page is told of the lists: when they're from, and
// whether the agent was asked for them lately.
func (s *Server) optionsBody() map[string]interface{} {
	opts, at, fresh := s.freshOptions()
	body := map[string]interface{}{"options": opts, "optionsFresh": fresh}
	if opts != nil {
		body["optionsAt"] = at.UnixMilli()
	}
	return body
}

// choiceFeature is the page's call on the model and effort, which needs a
// conversation to apply to.
var choiceFeature = feature{func(s *Server) bool { return s.cfg.Choice != nil && s.cfg.Converse != nil },
	"this sandbox has no model or effort to choose", 1 << 10}

// choiceBody is what the page is told of the choice.
func (s *Server) choiceBody() (map[string]interface{}, error) {
	model, effort, err := s.cfg.Choice.Get()
	if err != nil {
		return nil, err
	}
	body := s.optionsBody()
	body["model"], body["effort"] = model, effort
	// What the profile doesn't hold itself but is in force all the same, from
	// the global settings: Default can't take it away, and the page says so.
	inherited := map[string]bool{"model": false, "effort": false}
	if s.cfg.Choice.Own != nil {
		ownModel, ownEffort, err := s.cfg.Choice.Own()
		if err != nil {
			return nil, err
		}
		inherited["model"], inherited["effort"] = ownModel == "" && model != "", ownEffort == "" && effort != ""
	}
	body["inherited"] = inherited
	if s.cfg.Choice.Limits != nil {
		if limits := s.cfg.Choice.Limits(); limits != nil {
			body["limits"] = limits
		}
	}
	body["running"] = s.convo.ranModel()
	body["session"] = s.convo.hasSession()
	return body, nil
}

func (s *Server) choiceState(w http.ResponseWriter, r *http.Request) {
	body, err := s.choiceBody()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "couldn't read the model and effort chosen: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// choiceSet keeps the model and effort the page chose. Changing the model
// puts the effort back to the default whatever it was sent: what an effort
// means differs between models, so it's chosen afresh.
func (s *Server) choiceSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `expected {"model": ..., "effort": ...}`)
		return
	}
	if err := modelchoice.CheckModel(req.Model); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var err error
	// Held to the agent's own list only once it has been asked in this run:
	// one from an earlier run may be out of date, and refusing what the agent
	// now takes would be wrong (an effort it doesn't take it ignores, saying so).
	if have, _, fresh := s.freshOptions(); have != nil && fresh {
		allowed := have.Efforts
		if own, ok := have.EffortsByModel[req.Model]; ok {
			allowed = own
		}
		err = modelchoice.CheckEffort(req.Effort, allowed)
	} else {
		err = modelchoice.CheckEffortName(req.Effort)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The page doesn't offer what the plan can't run, but another page may not
	// have been told.
	if s.cfg.Choice.Limits != nil {
		if limits := s.cfg.Choice.Limits(); limits != nil && slices.Contains(limits.Unavailable, req.Model) {
			writeError(w, http.StatusConflict, limits.Why)
			return
		}
	}
	if !s.keepChoice(w, req.Model, req.Effort) {
		return
	}
	s.choiceState(w, r)
}

// choiceUnlock is the user saying the plan has changed: what was learned of
// its limits is taken away.
func (s *Server) choiceUnlock(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Choice.Unlock == nil {
		writeError(w, http.StatusNotFound, "this agent's plan has no limits to take away")
		return
	}
	if err := s.cfg.Choice.Unlock(); err != nil {
		writeError(w, http.StatusInternalServerError, "the limits couldn't be taken away: "+err.Error())
		return
	}
	s.choiceState(w, r)
}

// keepChoice keeps model and effort as the choice, saying why and returning
// false if it can't.
func (s *Server) keepChoice(w http.ResponseWriter, model, effort string) bool {
	s.choosing.Lock()
	defer s.choosing.Unlock()
	was, _, err := s.cfg.Choice.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "couldn't read the model and effort chosen: "+err.Error())
		return false
	}
	if model != was {
		effort = ""
	}
	if err := s.cfg.Choice.Set(model, effort); err != nil {
		writeError(w, http.StatusInternalServerError, "the choice couldn't be kept: "+err.Error())
		return false
	}
	return true
}

// choiceFetchOptions asks the agent what it has, and keeps the answer, in
// memory and for the next run (the page shows what was kept meanwhile). It
// doesn't wake a sleeping sandbox to ask, as opening the page shouldn't;
// the page says wake when the user has asked for that. A failure is said and
// not kept, so the next try asks again.
func (s *Server) choiceFetchOptions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Wake bool `json:"wake"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `expected {"wake": true or false}`)
		return
	}
	if _, _, fresh := s.freshOptions(); fresh {
		writeJSON(w, http.StatusOK, s.optionsBody())
		return
	}
	// One page asks the agent at a time: another waits for that answer, and
	// asks for itself only if it fails.
	if !s.options.startAsking(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "this page stopped waiting for the agent's lists")
		return
	}
	defer s.options.doneAsking()
	if _, _, fresh := s.freshOptions(); fresh {
		writeJSON(w, http.StatusOK, s.optionsBody())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), optionsWithin)
	defer cancel()
	if req.Wake {
		if err := s.wake(ctx); err != nil {
			writeError(w, http.StatusBadGateway, "the sandbox couldn't be woken to ask the agent what it has: "+err.Error())
			return
		}
	}
	opts, err := s.cfg.Choice.Options(ctx)
	if err != nil {
		// The page may be asking quietly (see the page's refresh at a turn's end)
		// and say nothing of this, so it is said here.
		if s.cfg.Log != nil {
			s.cfg.Log("couldn't ask the agent for its models and efforts: " + err.Error())
		}
		writeError(w, http.StatusBadGateway, "the agent couldn't say what it has: "+err.Error())
		return
	}
	s.options.put(opts, s.clock(), true)
	if s.cfg.Choice.Remember != nil {
		// Only a cache: not worth failing for, but not to go unsaid.
		if err := s.cfg.Choice.Remember(opts); err != nil && s.cfg.Log != nil {
			s.cfg.Log("couldn't keep the list of models for next time: " + err.Error())
		}
	}
	writeJSON(w, http.StatusOK, s.optionsBody())
}

// modelInUse is the model the conversation is on while none is chosen: the one
// that its last turn ran, if that is one of the listed, and else claude's
// default, by what the lists say. "" when there's nothing to go by.
func (s *Server) modelInUse() string {
	opts, _, _ := s.keptOptions()
	if opts == nil {
		return ""
	}
	if alias := modelchoice.AliasOfRan(s.convo.ranModel(), opts.Models); alias != "" {
		return alias
	}
	return opts.DefaultAlias
}

// choiceAdopt records the model the conversation is on, when none is chosen
// or inherited. Not choosing is no choice of "whatever claude's default is":
// the user is on a model, and a change of claude's default must not move them
// to another, so the model they are on is kept as theirs, with nothing said as
// nothing has changed (see docs/plan-model-effort.md). The profile's own effort
// is kept as it is. It does nothing when there's nothing to go by.
func (s *Server) choiceAdopt(w http.ResponseWriter, r *http.Request) {
	s.choosing.Lock()
	defer s.choosing.Unlock()
	model, effort, err := s.cfg.Choice.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "couldn't read the model and effort chosen: "+err.Error())
		return
	}
	adopted := ""
	if model == "" {
		if alias := s.modelInUse(); alias != "" {
			if s.cfg.Choice.Own != nil {
				if _, effort, err = s.cfg.Choice.Own(); err != nil {
					writeError(w, http.StatusInternalServerError, "couldn't read the effort chosen: "+err.Error())
					return
				}
			}
			if err := s.cfg.Choice.Set(alias, effort); err != nil {
				if s.cfg.Log != nil {
					s.cfg.Log("couldn't keep the model the conversation is on: " + err.Error())
				}
				writeError(w, http.StatusInternalServerError, "the model in use couldn't be kept: "+err.Error())
				return
			}
			adopted = alias
		}
	}
	body, err := s.choiceBody()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "couldn't read the model and effort chosen: "+err.Error())
		return
	}
	body["adopted"] = adopted
	writeJSON(w, http.StatusOK, body)
}
