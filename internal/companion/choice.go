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
	// Set keeps a choice. It is given only what Check passed.
	Set func(model, effort string) error
	// Options asks the agent which models and efforts it has: the page's
	// drop-downs are made of them, so they are as live as the agent is.
	Options func(ctx context.Context) (Options, error)
}

// Options are the agent's own lists, as the page offers them.
type Options struct {
	// Models are the plain names of models, offered at once; OtherModels
	// the rest, offered on request.
	Models      []string `json:"models"`
	OtherModels []string `json:"otherModels"`
	Efforts     []string `json:"efforts"`
	// Current names the model the agent uses with no choice made, as it
	// says it.
	Current string `json:"current,omitempty"`
}

// optionsWithin bounds asking the agent for its lists, past the sandbox
// being woken: they are two quick commands.
const optionsWithin = 2 * time.Minute

// choiceOptions is what the agent said it has, kept once it has said it.
type choiceOptions struct {
	mu   sync.Mutex
	have *Options
}

func (o *choiceOptions) get() *Options {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.have
}

func (o *choiceOptions) put(opts Options) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.have = &opts
}

// choosing is the page's call on the model and effort, which needs a
// conversation to apply to.
var choiceFeature = feature{func(s *Server) bool { return s.cfg.Choice != nil && s.cfg.Converse != nil },
	"this sandbox has no model or effort to choose", 1 << 10}

// choiceBody is what the page is told of the choice.
func (s *Server) choiceBody() (map[string]interface{}, error) {
	model, effort, err := s.cfg.Choice.Get()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"model": model, "effort": effort,
		"running": s.convo.ranModel(),
		"session": s.convo.hasSession(),
		"options": s.options.get(),
	}, nil
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
	if have := s.options.get(); have != nil {
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

// choiceFetchOptions asks the agent what it has, and keeps the answer. It
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
	if have := s.options.get(); have != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"options": have})
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
	s.options.put(opts)
	writeJSON(w, http.StatusOK, map[string]interface{}{"options": s.options.get()})
}
