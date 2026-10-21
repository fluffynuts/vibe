// Package companion serves the page vibe opens beside a sandbox: tabs for
// the sandbox's clipboard page, its network log and rules, and its
// filesystem rules, so none of that needs the sbx TUI.
//
// The page runs on the host, in the vibe process, and drives the host's own
// sbx. That makes it the one thing in reach of the sandbox that can change
// the sandbox's network rules, so it is built to be unreachable by anything
// but the user's own browser:
//
//   - it listens on 127.0.0.1 only;
//   - it answers only requests whose Host is localhost, 127.0.0.1 or ::1 on
//     its own port, which defeats DNS rebinding. It doesn't stop the
//     sandbox: a request sent through host.docker.internal (where its
//     network rules let one through) can say any Host it likes;
//   - every call to /api (bar the ping that tells two vibes apart) needs the
//     sandbox's secret token in a header, which a page on another site
//     can't add without a CORS preflight this server never allows. The token
//     is what keeps the sandbox out: it is only ever in the URL's fragment,
//     which a browser keeps from the server, from logs and from Referer
//     headers, and it is never written into the sandbox;
//   - changes need POST, a JSON body, and a same-origin request;
//   - it never offers a rule that would match every host, or far more than
//     one site, and it only ever scopes a rule to this sandbox.
//
// What sbx printed — host names, which the sandbox itself chooses — is only
// ever shown as text, never as markup (see ui/app.js).
package companion

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"vibe/internal/pathresolve"
	"vibe/internal/policy"
)

//go:embed ui
var uiFiles embed.FS

// Config is what the page is served for.
type Config struct {
	Sandbox string
	// Agent is the agent the sandbox runs (claude, codex, …), which picks
	// the page's icon; "" gets vibe's own.
	Agent string
	// Token is the secret every /api call must carry.
	Token string
	// Port is the host port to listen on, on 127.0.0.1.
	Port   int
	Policy *policy.Client
	// RulesChanged, when set, is told of every change the page makes to
	// the sandbox's rules: vibe keeps them afresh, so a rebuild gives back
	// what the page left (see policy.Snapshot).
	RulesChanged func() error
	// ClipboardURL is where the sandbox's clipboard page is reachable on the
	// host, or "" when the sandbox has none; it is asked for often, so it
	// should be quick.
	ClipboardURL func() string
	// ClipboardReady says whether the clipboard page at url has come up
	// yet; nil means probeClipboard, which wants a 200 from it.
	ClipboardReady func(url string) bool
	// ClipboardTrouble says why the clipboard page didn't come up, when the
	// sandbox's start-clipboard said ("" when it didn't, or hasn't run);
	// asked for only while the page isn't up. Nil means it can't be asked.
	ClipboardTrouble func() string
	// StartClipboard pokes the sandbox to start its clipboard page's server
	// (it is a no-op when that is already running), returning the sbx
	// arguments it ran; nil means the page can't offer to.
	StartClipboard func() ([]string, error)
	// Workspace is the host folder the sandbox works in, shown in the
	// page's header with its git status; "" shows neither.
	Workspace string
	// Git reads dir's git status, nil when it has none; nil means readGit.
	Git func(dir string) *GitInfo
	// Converse runs a turn of the conversation tab's talk with the agent;
	// nil means the sandbox's agent can't be talked to this way, and the
	// page has no such tab.
	Converse Converser
	// Choice lets the page choose the model and effort the conversation's
	// agent is asked to use; nil means it can't, and the page has no
	// drop-downs for them.
	Choice *Choice
	// Prepare readies the sandbox for a turn before it runs (starting it,
	// if it's stopped), saying what it does through say, which the page
	// shows; an error ends the turn with it, unrun. Nil means nothing is
	// done first.
	Prepare func(ctx context.Context, say func(string)) error
	// SandboxGit runs git, with args, in the sandbox on its folder (the
	// same path as Workspace), giving what it wrote to stdout; a failure
	// carries what it said. Nil means the page can't start a new branch.
	SandboxGit func(ctx context.Context, args ...string) (string, error)
	// Wake gets the sandbox running for something the user asked of it,
	// starting it if it's stopped, and saying what it does through say; an
	// error says why it can't. Nil means nothing is done first.
	Wake func(ctx context.Context, say func(string)) error
	// PreferredDefaultBranch is the name to suggest for a repository's
	// default branch, when origin can't say which it is: the one last set
	// ("" for none). RememberDefaultBranch keeps the one just set, for next
	// time. Nil means nothing is suggested, or kept.
	PreferredDefaultBranch func() string
	RememberDefaultBranch  func(name string) error
	// ReviewTab is what the Review tab shows; nil means the page has no
	// such tab.
	ReviewTab *ReviewTab
	// Admin is what the page's Admin tab does to the sandbox; nil means the
	// page has no such tab.
	Admin Admin
	// Packages holds the sandbox's requests for packages for the user to
	// approve, on the Admin tab; nil means the sandbox can't ask.
	Packages PackageDesk
	// PackageSync is what fetches, for the user's IDE, the packages the
	// sandbox restored into its own cache; nil means none is offered.
	// PackageSyncAuto says to fetch them without asking.
	PackageSync     PackageSync
	PackageSyncAuto func() bool
	// PackageConfig is the folder's nuget.config, to approve on the same
	// tab; nil means none is offered.
	PackageConfig PackageConfig
	// Usage reads how busy the sandbox is, for the page's CPU and memory
	// bars; it may be slow (it asks the sandbox), and says ErrNotRunning for
	// a sandbox that isn't. Nil means the page has no such bars.
	Usage func(ctx context.Context) (UsageReading, error)
	// KeepAwake is what the page asks for while it's in view (and as it
	// comes back into view), to keep the sandbox from being stopped for
	// being idle, or start it again if it was; it returns what it did, if
	// it says, for the log. Nil means nothing is done. It may be slow.
	KeepAwake func() string
	// Log says something in vibe's own output; nil means nowhere.
	Log func(string)
	// Manage is what the page's Profiles tab works on; nil means the page
	// has no such tab. A page with no Sandbox is a Profiles page alone
	// ('vibe --manage').
	Manage Manager
	// ThemeColor is the colour of the agent's theme, for the page to pick
	// itself out with ("" for none: the page's own); nil means none. It may
	// be slow (it can ask the sandbox), so it is asked for seldom.
	ThemeColor func() string
	// History is the file the conversation is kept in between runs, so it
	// survives vibe being started again; "" keeps it only in memory.
	History string
}

// Server is a running companion page.
type Server struct {
	cfg   Config
	ln    net.Listener
	http  *http.Server
	ui    fs.FS
	cache *cache
	// starting is held while a StartClipboard runs, so a second press of
	// the page's button doesn't start a second one.
	starting sync.Mutex
	// awake is held while a KeepAwake runs: pages asking meanwhile needn't
	// ask again.
	awake sync.Mutex
	// done is closed by Close, ending what Start began alongside serving.
	done chan struct{}
	// loops are what Start began that writes (see takeInbox): Close waits
	// for them to end.
	loops sync.WaitGroup
	convo conversation
	// options are what the agent said it has, for the model and effort.
	options choiceOptions
	admin   admin
	asked   asked
	// reviewTurn is the prompt of the turn the review last began (its
	// start, a Submit or an Ask): closing the review may stop that turn,
	// and no other.
	reviewTurn struct {
		mu     sync.Mutex
		prompt int
	}
	// usage is the last reading of the sandbox's load, and clock the
	// clock it's aged by.
	usage   usageCache
	blocked blockedCache
	clock   func() time.Time
	// lifeCtx ends with the server: what it starts for the page (a fetch)
	// ends with it.
	lifeCtx    context.Context
	lifeCancel context.CancelFunc
	// pageVersion stands for the page's files as this vibe serves them: a
	// page left open across a rebuild of vibe can tell it's out of date.
	pageVersion string
	colours     *colourer
}

// asked is the thread of the review the user last asked the agent about,
// and the prompt that asked: while that's the conversation's latest, the
// Review tab shows the agent answering there.
type asked struct {
	mu     sync.Mutex
	thread int
	prompt int
}

// New makes a Server without listening, which is how tests drive Handler.
func New(cfg Config) *Server {
	ui, _ := fs.Sub(uiFiles, "ui")
	life, cancel := context.WithCancel(context.Background())
	return &Server{cfg: cfg, ui: ui, clock: time.Now, lifeCtx: life, lifeCancel: cancel, cache: newCache(time.Second), pageVersion: versionOf(ui), colours: newColourer(colourAtOnce)}
}

// Start listens on 127.0.0.1 at cfg.Port and serves in the background.
func Start(cfg Config) (*Server, error) {
	if cfg.Token == "" {
		return nil, errors.New("companion: no token")
	}
	ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)))
	if err != nil {
		return nil, err
	}
	s := New(cfg)
	if cfg.History != "" {
		if err := s.convo.load(cfg.History); err != nil {
			// A conversation that can't be read is begun anew rather than
			// keeping the page from being served; the file is left as it is.
			s.convo.file = ""
		}
	}
	if cfg.Admin != nil {
		// Known by the time anyone looks at the tab.
		s.startUpgradeCheck(false)
	}
	s.ln = ln
	s.cfg.Port = ln.Addr().(*net.TCPAddr).Port
	s.http = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		// A connection left open between requests is let go: the sandbox
		// may be able to reach this port, and could hold open as many as
		// it liked.
		IdleTimeout: time.Minute,
	}
	go s.http.Serve(ln)
	s.done = make(chan struct{})
	go s.keepAwakeWhileWorking(s.done)
	if cfg.PackageSync != nil {
		s.loops.Add(1)
		go func(done <-chan struct{}) {
			defer s.loops.Done()
			s.pollPackageSync(done, packageSyncEvery)
		}(s.done)
	}
	if cfg.Packages != nil {
		s.loops.Add(1)
		go func(done <-chan struct{}) {
			defer s.loops.Done()
			s.pollPackages(done, packagesPollEvery)
		}(s.done)
	}
	if cfg.ReviewTab != nil {
		s.loops.Add(1)
		go func(done <-chan struct{}) {
			defer s.loops.Done()
			s.takeInbox(done, reviewTakeEvery)
		}(s.done)
	}
	return s, nil
}

// Close stops serving, saving the conversation for the next vibe.
func (s *Server) Close() error {
	s.lifeCancel()
	if s.done != nil {
		close(s.done)
		s.done = nil
	}
	// What's under way (the review being taken in and saved) ends first.
	s.loops.Wait()
	s.convo.close()
	if s.http == nil {
		return nil
	}
	return s.http.Close()
}

// Port is the port the page is served on.
func (s *Server) Port() int { return s.cfg.Port }

// URL is where the user opens the page: the token rides in the fragment.
func (s *Server) URL() string {
	return fmt.Sprintf("http://localhost:%d/#%s", s.cfg.Port, s.cfg.Token)
}

// Running says whether a companion page for sandbox is already being served
// on port — by another vibe attached to the same sandbox.
func Running(port int, sandbox string) bool {
	client := http.Client{Timeout: 700 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/ping", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		App     string `json:"app"`
		Sandbox string `json:"sandbox"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&body); err != nil {
		return false
	}
	return body.App == "vibe-companion" && body.Sandbox == sandbox
}

// Handler is the whole page: its files and its API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static := http.FileServer(http.FS(s.ui))
	mux.Handle("GET /{$}", static)
	mux.Handle("GET /app.js", static)
	mux.Handle("GET /app.css", static)
	mux.Handle("GET /markdown.js", static)
	mux.Handle("GET /manage.js", static)
	mux.Handle("GET /review.js", static)

	for _, r := range s.apiRoutes() {
		mux.HandleFunc(r.pattern, r.handler)
	}

	return s.guarded(mux)
}

// apiRoute is one of the API's calls: its method and path, and what
// answers it.
type apiRoute struct {
	pattern string
	handler http.HandlerFunc
}

// apiRoutes are the API's calls. Every one but ping is behind the token,
// and every change is same-origin only: a test holds each to that.
func (s *Server) apiRoutes() []apiRoute {
	return []apiRoute{
		{"GET /api/ping", s.ping},
		{"GET /api/state", s.authed(s.state)},
		{"GET /api/log", s.authed(s.reading("log", func() (interface{}, error) { return s.cfg.Policy.ResolvedLog() }))},
		{"GET /api/rules", s.authed(s.reading("rules", func() (interface{}, error) { return s.cfg.Policy.NetworkRules() }))},
		{"GET /api/fs-rules", s.authed(s.reading("fs-rules", func() (interface{}, error) { return s.cfg.Policy.FilesystemRules() }))},
		{"POST /api/allow", s.authed(s.changing(s.allow))},
		{"POST /api/deny", s.authed(s.changing(s.deny))},
		{"POST /api/remove", s.authed(s.changing(s.remove))},
		{"POST /api/clipboard/start", s.authed(s.changing(s.startClipboard))},
		{"GET /api/conversation", s.authed(s.conversationEvents)},
		{"GET /api/conversation/choice", s.serving(choiceFeature, s.choiceState)},
		{"POST /api/conversation/choice", s.serving(choiceFeature, s.choiceSet)},
		{"POST /api/conversation/choice/options", s.serving(choiceFeature, s.choiceFetchOptions)},
		{"POST /api/conversation/send", s.serving(conversationFeature, s.conversationSend)},
		{"POST /api/conversation/stop", s.serving(conversationFeature, s.conversationStop)},
		{"POST /api/conversation/new", s.serving(conversationFeature, s.conversationNew)},
		{"POST /api/conversation/new/options", s.serving(conversationFeature, s.conversationNewOptions)},
		{"POST /api/conversation/new/fetch", s.serving(conversationFeature, s.conversationNewFetch)},
		{"GET /api/default-branch", s.serving(defaultBranchFeature, s.defaultBranchState)},
		{"POST /api/default-branch", s.serving(defaultBranchFeature, s.defaultBranchSet)},
		{"GET /api/usage", s.serving(usageFeature, s.usageHandler)},
		{"GET /api/admin", s.authed(s.adminState)},
		{"POST /api/admin/do", s.serving(adminFeature, s.adminDo)},
		{"POST /api/admin/upgrades", s.serving(adminFeature, s.adminCheckUpgrades)},
		{"POST /api/admin/cancel", s.serving(adminFeature, s.adminCancel)},
		{"GET /api/packages/sync", s.serving(packageSyncFeature, s.packageSyncState)},
		{"POST /api/packages/sync/check", s.serving(packageSyncFeature, s.packageSyncCheck)},
		{"POST /api/packages/sync/run", s.serving(packageSyncFeature, s.packageSyncRun)},
		{"GET /api/packages", s.serving(packagesFeature, s.packagesState)},
		{"POST /api/packages/decide", s.serving(packagesFeature, s.packagesDecide)},
		{"POST /api/packages/decline-all", s.serving(packagesFeature, s.packagesDeclineAll)},
		{"POST /api/packages/approve-config", s.serving(packagesFeature, s.packagesApproveConfig)},
		{"GET /api/admin/instructions", s.authed(s.instructionsState)},
		{"POST /api/admin/instructions", s.serving(instructionsFeature, s.instructionsSave)},
		{"POST /api/admin/instructions/optimise", s.serving(instructionsFeature, s.instructionsOptimise)},
		{"POST /api/awake", s.authed(s.changing(s.keepAwake))},
		{"GET /api/review", s.serving(reviewFeature, s.reviewState)},
		{"GET /api/review/file", s.serving(reviewFeature, s.reviewFile)},
		{"POST /api/review/do", s.serving(reviewFeature, s.reviewDo)},
		{"POST /api/review/ask", s.serving(reviewFeature, s.reviewAsk)},
		{"POST /api/review/submit", s.serving(reviewFeature, s.reviewSubmit)},
		{"GET /api/review/options", s.serving(reviewFeature, s.reviewOptions)},
		{"GET /api/review/highlight", s.serving(reviewFeature, s.reviewHighlight)},
		{"POST /api/clipboard/paste", s.authed(s.clipboardPaste)},
		{"POST /api/review/start", s.serving(reviewFeature, s.reviewStart)},
		{"POST /api/review/close", s.serving(reviewFeature, s.reviewClose)},
		{"POST /api/review/afresh", s.serving(reviewFeature, s.reviewAfresh)},
		{"GET /api/manage", s.authed(s.managed(s.manageState))},
		{"POST /api/manage/plan", s.serving(manageFeature, s.managePlan)},
		{"POST /api/manage/delete", s.serving(manageFeature, s.manageDelete)},
		{"POST /api/manage/do", s.serving(manageFeature, s.manageDo)},
		{"POST /api/manage/cancel", s.serving(manageFeature, s.adminCancel)},
	}
}

// guarded refuses requests for any other Host, and sets the headers every
// response carries.
func (s *Server) guarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostOK(r.Host) {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		// frame-src is for the clipboard tab: the sandbox's page, on the
		// host's own loopback.
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src data:; "+
			"connect-src 'self'; frame-src http://localhost:* http://127.0.0.1:*; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostOK(hostport string) bool {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return false
	}
	if port != strconv.Itoa(s.cfg.Port) {
		return false
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// feature is a part of the page a server may or may not have: a tab, or
// what one works on.
type feature struct {
	present func(*Server) bool
	// missing is why a call to it is refused when it isn't there.
	missing string
	// limit is the most a change's body may be.
	limit int64
}

var (
	usageFeature = feature{func(s *Server) bool { return s.cfg.Usage != nil },
		"this page has no way to read the sandbox's load", 1 << 10}
	conversationFeature = feature{func(s *Server) bool { return s.cfg.Converse != nil },
		"this sandbox has no conversation to have", maxPrompt}
	adminFeature = feature{func(s *Server) bool { return s.cfg.Admin != nil },
		"this page has no admin tab", 64 * 1024}
	// Room for the text and the base it was loaded as, escaped.
	instructionsFeature = feature{adminFeature.present, adminFeature.missing, 3 * maxInstructions}
	manageFeature       = feature{func(s *Server) bool { return s.cfg.Manage != nil },
		"this page has no Profiles tab", 64 * 1024}
	packageSyncFeature = feature{func(s *Server) bool { return s.cfg.PackageSync != nil },
		"this sandbox has no packages to make visible to an IDE", 1 << 10}
	packagesFeature = feature{func(s *Server) bool { return s.cfg.Packages != nil },
		"this sandbox can't ask for packages", 16 * 1024}
	reviewFeature = feature{func(s *Server) bool { return s.cfg.ReviewTab != nil },
		"this page has no review tab", maxPrompt}
	defaultBranchFeature = feature{(*Server).repositoryHere,
		"this sandbox's folder isn't a git repository", 1 << 10}
)

// serving wraps a call to feature f: it needs the page's token, and f to be
// there. A change (a POST) also needs a same-origin JSON body, of at most
// f's limit; one from anywhere else is refused before anything is said of
// what the page has.
func (s *Server) serving(f feature, next http.HandlerFunc) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if !sameOriginJSON(w, r) {
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, f.limit)
		}
		if !f.present(s) {
			writeError(w, http.StatusNotFound, f.missing)
			return
		}
		next(w, r)
	})
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Vibe-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"error": "this page doesn't have the right token — open the URL vibe printed",
				"code":  "unauthorized",
			})
			return
		}
		next(w, r)
	}
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"app": "vibe-companion", "sandbox": s.cfg.Sandbox})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	clipboard := s.clipboardAt()
	ready := false
	if clipboard != "" {
		probe := s.cfg.ClipboardReady
		if probe == nil {
			probe = probeClipboard
		}
		// Keyed by the URL: a new one (the sandbox restarted on another
		// port) is probed afresh.
		ready = cached(s.cache, "clipboard-ready "+clipboard, time.Second, func() bool {
			return probe(clipboard)
		})
	}
	var git *GitInfo
	if s.cfg.Workspace != "" {
		read := s.cfg.Git
		if read == nil {
			read = readGit
		}
		git = cachedStale(s.cache, "git", 2*time.Second, func() *GitInfo {
			return read(s.cfg.Workspace)
		})
	}
	theme := ""
	if s.cfg.ThemeColor != nil {
		// /theme in the agent can change it at any time, but that's seldom.
		theme = cachedStale(s.cache, "theme", time.Minute, s.cfg.ThemeColor)
	}
	// attention is, by tab, why that tab wants looking at: the page draws a
	// dot on it, with these as its tooltip, so a dot always says why.
	attention := map[string][]string{}
	if s.cfg.Admin != nil {
		s.admin.mu.Lock()
		if len(s.admin.upgrades) > 0 {
			attention["admin"] = []string{"Upgrades available: " + strings.Join(s.admin.upgrades, ", ") + " — this tab says how to get them"}
		}
		s.admin.mu.Unlock()
	}
	if waiting := s.packagesWaiting(); waiting != "" {
		attention["admin"] = append(attention["admin"], waiting)
	}
	if waiting := s.packageSyncWaiting(); waiting != "" {
		attention["admin"] = append(attention["admin"], waiting)
	}
	// blockedHosts is what the network log says is blocked, for the page to
	// tell the user of the ones they haven't seen; null until it's been read.
	var blockedHosts []string
	if hosts, known := s.blockedHosts(); known {
		blockedHosts = hosts
	}
	// reviewAt is when the agent last updated the review: the page marks the
	// Review tab when it's later than the user has seen. Taken in here, as
	// the Review tab only polls the review while it's open.
	reviewAt := ""
	if s.cfg.ReviewTab != nil {
		if rv, err := s.cfg.ReviewTab.Store.Take(); err != nil {
			attention["review"] = []string{"The review couldn't be read: " + err.Error()}
		} else if !rv.AgentAt.IsZero() {
			reviewAt = rv.AgentAt.UTC().Format(time.RFC3339Nano)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"attention":             attention,
		"packagesWaiting":       s.packagesWaitingCount(),
		"packagesToFetch":       s.packagesToFetch(),
		"packagesStale":         s.packagesStale(),
		"packagesStaleProjects": s.packagesStaleProjects(),
		"blockedHosts":          blockedHosts,
		"reviewAt":              reviewAt,
		"pageVersion":           s.pageVersion,
		"themeColor":            theme,
		"sandbox":               s.cfg.Sandbox,
		"agent":                 s.cfg.Agent,
		"folder":                pathresolve.Tilde(s.cfg.Workspace),
		"git":                   git,
		"clipboardUrl":          clipboard,
		"clipboardReady":        ready,
		"clipboardTrouble":      s.clipboardTrouble(clipboard, ready),
		"clipboardCanStart":     clipboard != "" && s.cfg.StartClipboard != nil,
		"conversation":          s.cfg.Converse != nil,
		"choice":                choiceFeature.present(s),
		"admin":                 s.cfg.Admin != nil,
		"manage":                s.cfg.Manage != nil,
		"review":                s.cfg.ReviewTab != nil,
		"usage":                 s.cfg.Usage != nil,
		// A page for no sandbox has only the Profiles tab.
		"sandboxTabs": s.cfg.Sandbox != "",
	})
}

// clipboardTrouble is why the clipboard page at url isn't up, if the sandbox
// said: shared between the page's polls, since it asks the sandbox.
func (s *Server) clipboardTrouble(url string, ready bool) string {
	if url == "" || ready || s.cfg.ClipboardTrouble == nil {
		return ""
	}
	return cachedStale(s.cache, "clipboard-trouble", 10*time.Second, s.cfg.ClipboardTrouble)
}

// probeClient is for probeClipboard: never through a proxy (the page is on
// the host's own loopback), and quick to give up, since the page asks again
// a couple of seconds later.
var probeClient = &http.Client{
	Timeout:   time.Second,
	Transport: &http.Transport{Proxy: nil},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// probeClipboard says whether the clipboard page answers url with a 200:
// until its server is up there is either no answer at all or something
// other than the page.
func probeClipboard(url string) bool {
	res, err := probeClient.Get(url)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, io.LimitReader(res.Body, 64*1024))
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// workingAwakeEvery is how often the sandbox is kept awake while the agent
// works: as often as the page asks, well within sbx's idle stop.
const workingAwakeEvery = 20 * time.Second

// keepAwakeWhileWorking keeps the sandbox awake for as long as the agent
// works, until done is closed.
func (s *Server) keepAwakeWhileWorking(done <-chan struct{}) {
	tick := time.NewTicker(workingAwakeEvery)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			s.keepAwakeIfWorking()
		}
	}
}

// keepAwakeIfWorking keeps the sandbox awake if the agent is answering, or
// has answered and left something running (a review page, say). The page
// keeps it awake only while it's in view; sbx stopping the sandbox for being
// idle meanwhile would end that work under it.
func (s *Server) keepAwakeIfWorking() {
	if s.convo.working() {
		s.keepAwake(nil)
	}
}

// keepAwake runs KeepAwake in the background, unless it's running already;
// the page doesn't wait on it (starting a sandbox takes a while).
func (s *Server) keepAwake([]byte) ([]string, error) {
	if s.cfg.KeepAwake == nil || !s.awake.TryLock() {
		return nil, nil
	}
	go func() {
		defer s.awake.Unlock()
		if said := s.cfg.KeepAwake(); said != "" && s.cfg.Log != nil {
			s.cfg.Log(said)
		}
	}()
	return nil, nil
}

func (s *Server) startClipboard([]byte) ([]string, error) {
	if s.cfg.StartClipboard == nil {
		return nil, invalid{errors.New("this sandbox has no clipboard page to start")}
	}
	if !s.starting.TryLock() {
		return nil, invalid{errors.New("the clipboard page is already being started")}
	}
	defer s.starting.Unlock()
	return s.cfg.StartClipboard()
}

// reading serves a read of sbx's, briefly cached so several open tabs don't
// each run sbx every couple of seconds.
func (s *Server) reading(key string, read func() (interface{}, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := s.cache.get(key, 0, read)
		if err != nil {
			writeSbxError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

type change func(body []byte) (command []string, err error)

// changing wraps a change to the sandbox's rules: it must be a same-origin
// POST of JSON, and it forgets what was cached, so the page's next read
// shows the result.
func (s *Server) changing(do change) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOriginJSON(w, r) {
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			writeError(w, http.StatusBadRequest, "unreadable body")
			return
		}
		command, err := do(body)
		s.cache.clear()
		if err != nil {
			writeSbxError(w, err)
			return
		}
		answer := map[string]interface{}{
			"ok":      true,
			"command": "sbx " + strings.Join(command, " "),
		}
		if s.cfg.RulesChanged != nil {
			if err := s.cfg.RulesChanged(); err != nil {
				answer["warning"] = "Done, but a rebuild won't keep it: " + err.Error()
			}
		}
		writeJSON(w, http.StatusOK, answer)
	}
}

type invalid struct{ error }

func (s *Server) allow(body []byte) ([]string, error) {
	c := s.cfg.Policy
	return forHost(body, c.AllowCommand, c.Allow)
}

func (s *Server) deny(body []byte) ([]string, error) {
	c := s.cfg.Policy
	return forHost(body, c.DenyCommand, c.Deny)
}

// forHost runs a change to the host named in body, returning the sbx
// command it stands for.
func forHost(body []byte, command func(host string) []string, do func(host string) error) ([]string, error) {
	host, err := hostIn(body)
	if err != nil {
		return nil, err
	}
	return command(host), do(host)
}

func (s *Server) remove(body []byte) ([]string, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, invalid{errors.New("expected {\"id\": ...}")}
	}
	if err := policy.ValidRuleID(req.ID); err != nil {
		return nil, invalid{err}
	}
	c := s.cfg.Policy
	return c.RemoveCommand(req.ID), c.Remove(req.ID)
}

func hostIn(body []byte) (string, error) {
	var req struct {
		Host string `json:"host"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", invalid{errors.New("expected {\"host\": ...}")}
	}
	req.Host = strings.TrimSpace(req.Host)
	if err := policy.ValidHost(req.Host); err != nil {
		return "", invalid{err}
	}
	return req.Host, nil
}

func writeSbxError(w http.ResponseWriter, err error) {
	var bad invalid
	switch {
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, bad.Error())
	case errors.Is(err, policy.ErrNotSignedIn):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error(), "code": "not-signed-in"})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error(), "code": "sbx"})
	}
}

// writeError answers with status and {"error": msg}: what the page shows
// when something fails.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// cache holds the result of an sbx call for a moment, and shares one call
// among requests that arrive while it runs.
type cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	done chan struct{}
	at   time.Time
	val  interface{}
	err  error
	// refreshing is set (under the cache's lock) while getStale is fetching
	// a newer answer than this one.
	refreshing bool
}

func newCache(ttl time.Duration) *cache {
	return &cache{ttl: ttl, entries: map[string]*cacheEntry{}}
}

// get returns key's value if it is younger than ttl (the cache's own, when
// ttl is zero), else makes one with fn. Callers that arrive while fn runs
// wait for that run's answer.
func (c *cache) get(key string, ttl time.Duration, fn func() (interface{}, error)) (interface{}, error) {
	if ttl == 0 {
		ttl = c.ttl
	}
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		select {
		case <-e.done:
			if time.Since(e.at) < ttl {
				c.mu.Unlock()
				return e.val, e.err
			}
		default:
			c.mu.Unlock()
			<-e.done
			return e.val, e.err
		}
	}
	e := &cacheEntry{done: make(chan struct{})}
	c.entries[key] = e
	c.mu.Unlock()

	e.val, e.err = fn()
	e.at = time.Now()
	close(e.done)
	return e.val, e.err
}

// getStale is get for what is slow to ask (the sandbox, the repository) and
// asked for by the page every couple of seconds: once there is an answer,
// however old, it's given at once, and a newer one is fetched in the
// background, by one call whatever the number of askers. A slow fetch would
// otherwise hold every poll waiting, and the page's few connections to vibe
// with them, until it looks as if vibe has gone. Only the first answer is
// waited for: there's nothing to give before it.
func (c *cache) getStale(key string, ttl time.Duration, fn func() (interface{}, error)) (interface{}, error) {
	if ttl == 0 {
		ttl = c.ttl
	}
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		select {
		case <-e.done:
			if time.Since(e.at) >= ttl && !e.refreshing {
				e.refreshing = true
				go c.refresh(key, fn)
			}
			c.mu.Unlock()
			return e.val, e.err
		default:
			c.mu.Unlock()
			<-e.done
			return e.val, e.err
		}
	}
	e := &cacheEntry{done: make(chan struct{})}
	c.entries[key] = e
	c.mu.Unlock()

	e.val, e.err = fn()
	e.at = time.Now()
	close(e.done)
	return e.val, e.err
}

// refresh replaces key's answer with a fresh one. The old entry is left as it
// was, for those already holding it.
func (c *cache) refresh(key string, fn func() (interface{}, error)) {
	val, err := fn()
	fresh := &cacheEntry{done: make(chan struct{}), at: time.Now(), val: val, err: err}
	close(fresh.done)
	c.mu.Lock()
	c.entries[key] = fresh
	c.mu.Unlock()
}

// cachedStale is c.getStale for a call that can't fail, typed.
func cachedStale[T any](c *cache, key string, ttl time.Duration, fn func() T) T {
	v, _ := c.getStale(key, ttl, func() (interface{}, error) { return fn(), nil })
	t, _ := v.(T)
	return t
}

// cached is c.get for a call that can't fail, typed.
func cached[T any](c *cache, key string, ttl time.Duration, fn func() T) T {
	v, _ := c.get(key, ttl, func() (interface{}, error) { return fn(), nil })
	t, _ := v.(T)
	return t
}

// forget drops key, so it is made afresh when next asked for.
func (c *cache) forget(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

func (c *cache) clear() {
	c.mu.Lock()
	c.entries = map[string]*cacheEntry{}
	c.mu.Unlock()
}

// versionOf is a digest of the page's files (all of them, in name order).
func versionOf(ui fs.FS) string {
	h := sha256.New()
	fs.WalkDir(ui, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(ui, path)
		if err != nil {
			return err
		}
		io.WriteString(h, path+"\x00")
		h.Write(data)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)[:8])
}
