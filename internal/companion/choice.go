package companion

import (
	"context"
	"encoding/json"
	"net/http"
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
}

// Options are the agent's own lists, as the page offers them.
type Options struct {
	// Models are the plain names of models, offered at once; OtherModels
	// the rest, offered on request.
	Models      []string `json:"models"`
	OtherModels []string `json:"otherModels"`
	Efforts     []string `json:"efforts"`
	// Names are what the agent calls each of those models ("Opus 5.5" for
	// "opus"), for the page to show their versions. A model it couldn't
	// say the name of isn't in it.
	Names map[string]string `json:"names,omitempty"`
	// Rules are the listed names that pick a model by a rule ("best") and
	// aren't one model: what ran can't be held against them.
	Rules []string `json:"rules,omitempty"`
	// Current names the model the agent uses with no choice made, as it
	// says it.
	Current string `json:"current,omitempty"`
}

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

// optionsBody is what the page is told of the lists: when they're from, and
// whether the agent was asked for them in this run.
func (s *Server) optionsBody() map[string]interface{} {
	opts, at, fresh := s.keptOptions()
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
	if have, _, fresh := s.keptOptions(); have != nil && fresh {
		err = modelchoice.CheckEffort(req.Effort, have.Efforts)
	} else {
		err = modelchoice.CheckEffortName(req.Effort)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	was, _, err := s.cfg.Choice.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "couldn't read the model and effort chosen: "+err.Error())
		return
	}
	if req.Model != was {
		req.Effort = ""
	}
	if err := s.cfg.Choice.Set(req.Model, req.Effort); err != nil {
		writeError(w, http.StatusInternalServerError, "the choice couldn't be kept: "+err.Error())
		return
	}
	s.choiceState(w, r)
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
	if _, _, fresh := s.keptOptions(); fresh {
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
	if _, _, fresh := s.keptOptions(); fresh {
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
