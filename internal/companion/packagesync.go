package companion

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"vibe/internal/nugetfetch"
)

// PackageSync keeps the host's package cache holding what the sandbox's
// restores resolved, so an IDE on the host can find it (see
// nugetfetch.Sync).
type PackageSync interface {
	// Check looks at the workspace for what's missing; it's called every so
	// often, on one goroutine.
	Check()
	State() nugetfetch.SyncState
	// Run fetches what's missing; ErrSyncRunning when a run is under way.
	Run(ctx context.Context) error
}

// packageSyncEvery is how often the workspace is looked at. A variable for
// tests.
var packageSyncEvery = 10 * time.Second

// actionableMissing is what a fetch could bring (see nugetfetch.SyncState.Actionable), even while
// one is under way: the poll asks it when none is.
func actionableMissing(st nugetfetch.SyncState) int {
	st.Running = false
	return st.Actionable()
}

// fixableNow is actionableMissing, but none while a fetch is under way: what
// the Admin tab's dot and its card's highlight are both drawn for.
func fixableNow(st nugetfetch.SyncState) int { return st.Actionable() }

// packagesToFetch is fixableNow for the sandbox's sync, 0 for a sandbox with none.
func (s *Server) packagesToFetch() int {
	if s.cfg.PackageSync == nil {
		return 0
	}
	return fixableNow(s.cfg.PackageSync.State())
}

// packagesStale is how many packages the user's cache has that the sandbox's generated files
// still name in its own, once there's nothing left to fetch: until the files are generated again
// the IDE can't find them, though the cache has them. Fetching comes first, so none before it.
func (s *Server) packagesStale() int {
	if s.cfg.PackageSync == nil {
		return 0
	}
	st := s.cfg.PackageSync.State()
	if st.Running || st.Actionable() > 0 {
		return 0
	}
	return st.Stale
}

// packagesStaleProjects is the projects packagesStale is about, when it says any.
func (s *Server) packagesStaleProjects() []string {
	if s.packagesStale() == 0 {
		return []string{}
	}
	if projects := s.cfg.PackageSync.State().StaleProjects; projects != nil {
		return projects
	}
	return []string{}
}

// packageSyncWaiting says, for the Admin tab's dot, how many packages the
// sandbox restored that the user's IDE can't see, when fetching them would
// help; "" when none, or while they're being fetched.
func (s *Server) packageSyncWaiting() string {
	if s.cfg.PackageSync == nil {
		return ""
	}
	switch n := fixableNow(s.cfg.PackageSync.State()); n {
	case 0:
		return ""
	case 1:
		return "1 package the sandbox restored isn't in your package cache, so your IDE can't see it — fetch it on the Admin tab"
	default:
		return fmt.Sprintf("%d packages the sandbox restored aren't in your package cache, so your IDE can't see them — fetch them on the Admin tab", n)
	}
}

// pollPackageSync looks at the workspace every so often, until done is
// closed; and, when fetches are allowed without asking, fetches what's
// missing.
func (s *Server) pollPackageSync(done <-chan struct{}, every time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			s.cfg.PackageSync.Check()
			if s.cfg.PackageSyncAuto != nil && s.cfg.PackageSyncAuto() {
				if st := s.cfg.PackageSync.State(); !st.Running && actionableMissing(st) > 0 {
					if err := s.cfg.PackageSync.Run(ctx); err != nil && !errors.Is(err, nugetfetch.ErrSyncRunning) && s.cfg.Log != nil {
						s.cfg.Log("fetching the packages the sandbox restored failed: " + err.Error())
					}
				}
			}
		}
	}
}

// packageSyncCheck looks at the workspace now, and says what it found: for the page to ask when the
// user presses Send, before the agent is, at its own pace, ahead of the next scan.
func (s *Server) packageSyncCheck(w http.ResponseWriter, r *http.Request) {
	s.cfg.PackageSync.Check()
	s.packageSyncState(w, r)
}

func (s *Server) packageSyncState(w http.ResponseWriter, r *http.Request) {
	st := s.cfg.PackageSync.State()
	groups := st.Missing
	if groups == nil {
		groups = []nugetfetch.Group{}
	}
	body := map[string]interface{}{
		"auto":  s.cfg.PackageSyncAuto != nil && s.cfg.PackageSyncAuto(),
		"count": st.Count, "actionable": fixableNow(st), "missing": groups, "running": st.Running, "skipped": st.Skipped,
		"problems": st.Problems, "last": st.Last, "stale": st.Stale, "staleProjects": nonNil(st.StaleProjects),
	}
	if !st.Checked.IsZero() {
		body["checked"] = st.Checked
	}
	if st.Progress != nil {
		body["progress"] = st.Progress
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) packageSyncRun(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PackageSync.State().Running {
		writeError(w, http.StatusConflict, ErrPackageSyncRunning.Error())
		return
	}
	go func() {
		if err := s.cfg.PackageSync.Run(s.lifeCtx); err != nil && !errors.Is(err, nugetfetch.ErrSyncRunning) && s.cfg.Log != nil {
			s.cfg.Log("fetching the packages the sandbox restored failed: " + err.Error())
		}
	}()
	// A moment for it to say it's running, so the page's next answer does.
	for i := 0; i < 50 && !s.cfg.PackageSync.State().Running; i++ {
		time.Sleep(2 * time.Millisecond)
	}
	s.packageSyncState(w, r)
}

// ErrPackageSyncRunning is what asking to fetch while a fetch is under way
// is told.
var ErrPackageSyncRunning = errors.New("the packages are already being fetched")

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
