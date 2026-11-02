package companion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// LoginProcess is a sign-in command run for the page: what it prints comes a line at a time (Lines, closed when it has
// printed all it will), Done says how it ended, Send gives it a line to read (an error when it reads none), and Stop
// ends it.
type LoginProcess interface {
	Lines() <-chan string
	Done() <-chan error
	Send(line string) error
	Stop()
}

// signInJobLimit is how long a sign-in may wait for the person in their browser: the codes sbx and the agent give
// expire, and nothing should wait for ever. A variable for tests.
var signInJobLimit = 10 * time.Minute

const (
	// jobLinesKept is how many of a command's last lines the page is shown, and jobLineLen how much of each.
	jobLinesKept = 40
	jobLineLen   = 500
	// maxSignInCode is the longest code taken to give the command: they are a few hundred characters at most.
	maxSignInCode = 4096
)

var signInKinds = map[string]bool{"docker": true, "agent": true}

// signInJob is the one sign-in under way, or the last that was: what the page shows of it. Its fields are guarded by mu.
type signInJob struct {
	mu       sync.Mutex
	proc     LoginProcess
	kind     string
	running  bool
	done     bool
	ok       bool
	url      string
	urlKnown bool
	code     string
	lines    []string
	err      string
	// reason is why the command was ended from here (cancelled, given up on), when it was; it is what the page is told in
	// place of how the kill looked.
	reason string
}

// jobView is what the page is told. The code the user gives the command is not in it: it isn't kept.
type jobView struct {
	Kind    string `json:"kind"`
	Running bool   `json:"running"`
	Done    bool   `json:"done"`
	OK      bool   `json:"ok"`
	URL     string `json:"url,omitempty"`
	// URLKnown says the address is on a host this sign-in is known to use: only then is it offered as a link. The
	// agent's command runs in the sandbox, which is the agent's to change, so what it prints is not trusted to be the right page.
	URLKnown bool     `json:"urlKnown,omitempty"`
	Code     string   `json:"code,omitempty"`
	Lines    []string `json:"lines"`
	Error    string   `json:"error,omitempty"`
}

func (j *signInJob) view() jobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.viewLocked()
}

var (
	addressPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)
	codePattern    = regexp.MustCompile(`(?i)\bcode\b[^:\n]*:\s*([A-Z0-9]{3,}(?:-[A-Z0-9]{3,})+)`)
)

// signInHosts are the hosts a sign-in's address may be on, by kind: each host and its subdomains. sbx's runs on this machine;
// the agent's runs in the sandbox.
var signInHosts = map[string][]string{
	"docker": {"docker.com"},
	"agent":  {"claude.ai", "claude.com", "anthropic.com"},
}

// knownSignInAddress says address is https, on one of kind's hosts (or a subdomain of one), with no user part that could make
// a different host look like it: "claude.ai.evil.example" and "https://claude.ai@evil.example" are not.
func knownSignInAddress(kind, address string) bool {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range signInHosts[kind] {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// noticing takes an address or a code from a line the command printed, keeping the first of each.
func (j *signInJob) noticing(line string) {
	if len(line) > jobLineLen {
		line = line[:jobLineLen]
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, line)
	if over := len(j.lines) - jobLinesKept; over > 0 {
		j.lines = j.lines[over:]
	}
	// The first address is kept, and shown as text unless it is on a known host; a known one printed later takes its place.
	if j.url == "" || !j.urlKnown {
		if m := addressPattern.FindString(line); m != "" {
			address := strings.TrimRight(m, ".,;:)")
			if known := knownSignInAddress(j.kind, address); j.url == "" || known {
				j.url, j.urlKnown = address, known
			}
		}
	}
	if j.code == "" {
		if m := codePattern.FindStringSubmatch(line); m != nil {
			j.code = m[1]
		}
	}
}

// follow reads the command to its end, then records how it ended. A command that ended because it was given up on or
// cancelled says that, not what the kill looked like.
func (j *signInJob) follow(p LoginProcess) {
	for line := range p.Lines() {
		j.noticing(line)
	}
	err := <-p.Done()
	j.mu.Lock()
	defer j.mu.Unlock()
	j.running, j.done = false, true
	switch {
	case j.reason != "":
		j.err = j.reason
	case err != nil:
		j.err = err.Error()
	default:
		j.ok = true
	}
}

func (s *Server) signInStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !signInKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, `expected {"kind": "docker" or "agent"}`)
		return
	}
	job := &s.signInJob
	job.mu.Lock()
	if job.running {
		job.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "a sign-in is already under way: finish or cancel it first", "code": "signin-running"})
		return
	}
	if s.cfg.SignInStart == nil {
		job.mu.Unlock()
		writeError(w, http.StatusNotFound, signInFeature.missing)
		return
	}
	// The job is there, running, from here: starting the command can be slow (the sandbox is asked whether it is up), and
	// what the page polls, and a second start, must not wait on it. Field by field: replacing the struct would replace the lock.
	job.proc, job.kind, job.running, job.done, job.ok = nil, req.Kind, true, false, false
	job.url, job.code, job.err, job.reason, job.lines = "", "", "", "", []string{}
	job.mu.Unlock()

	p, err := s.cfg.SignInStart(req.Kind)

	job.mu.Lock()
	if err != nil {
		job.running, job.kind = false, ""
		job.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	job.proc = p
	// A cancel that came while it was starting could only mark the job: the command is ended now it exists.
	cancelled := job.reason != ""
	view := job.viewLocked()
	job.mu.Unlock()
	go job.follow(p)
	if cancelled {
		p.Stop()
	}
	time.AfterFunc(signInJobLimit, func() {
		job.mu.Lock()
		stillThis := job.proc == p && job.running
		if stillThis {
			job.reason = fmt.Sprintf("gave up: it waited %s and nothing happened", signInJobLimit)
		}
		job.mu.Unlock()
		if stillThis {
			p.Stop()
		}
	})
	writeJSON(w, http.StatusOK, view)
}

// viewLocked is view for a caller that holds mu.
func (j *signInJob) viewLocked() jobView {
	return jobView{Kind: j.kind, Running: j.running, Done: j.done, OK: j.ok, URL: j.url, URLKnown: j.urlKnown, Code: j.code,
		Lines: append([]string{}, j.lines...), Error: j.err}
}

func (s *Server) signInJobState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.signInJob.view())
}

// signInCode gives the command the code the user pasted, as a line on its stdin, and keeps it nowhere.
func (s *Server) signInCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" || len(req.Code) > maxSignInCode || strings.ContainsAny(req.Code, "\r\n") {
		writeError(w, http.StatusBadRequest, `expected {"code": "..."}: one line, as the sign-in page showed it`)
		return
	}
	job := &s.signInJob
	job.mu.Lock()
	p, running := job.proc, job.running
	job.mu.Unlock()
	if !running || p == nil {
		writeError(w, http.StatusConflict, "no sign-in is waiting for a code")
		return
	}
	if err := p.Send(req.Code); err != nil {
		writeError(w, http.StatusConflict, "this sign-in takes no code: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}

func (s *Server) signInCancel(w http.ResponseWriter, r *http.Request) {
	job := &s.signInJob
	job.mu.Lock()
	p, running := job.proc, job.running
	if running {
		job.reason = "cancelled"
	}
	job.mu.Unlock()
	// Without a process yet (still starting), start ends it once it has one.
	if running && p != nil {
		p.Stop()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": running})
}
