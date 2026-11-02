package nugetfetch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Group is the packages one target framework resolved that the host's cache
// doesn't have.
type Group struct {
	Framework string    `json:"framework"`
	Packages  []Package `json:"packages"`
	// Downloads are the packages the restore only downloaded (a targeting pack, say), which are
	// fetched as downloads, not references.
	Downloads []Package `json:"downloads,omitempty"`
}

// SyncResult is how the last run went.
type SyncResult struct {
	At time.Time `json:"at"`
	// Fetched is how many packages came.
	Fetched int `json:"fetched"`
	// Unresolved are the packages no feed has.
	Unresolved []Package `json:"unresolved,omitempty"`
	// Failed are packages NuGet failed on, each with its words: the rest came.
	Failed []Failure `json:"failed,omitempty"`
	// Error says why a run couldn't fetch a set at all (it couldn't be run, ran
	// out of time, was cancelled); Detail is what NuGet said.
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Progress is how far a run has got: Done of its Total packages are in the cache.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// SyncState is where Sync stands.
type SyncState struct {
	// Missing is what the sandbox's restores resolved that the cache lacks,
	// by framework; Count is how many different packages that is.
	Missing []Group
	Count   int
	// Checked is when the workspace was last looked at.
	Checked time.Time
	Running bool
	// Progress is set while Running.
	Progress *Progress
	Last     *SyncResult
	// Skipped is what the assets files named that wasn't a package id and an
	// exact version (not fetched); Problems are files that couldn't be read.
	Skipped  int
	Problems []string
	// Stale is how many packages the user's cache has that the sandbox's generated files
	// (project.nuget.cache) still name elsewhere: the sandbox's own copies. The IDE reads those
	// files, so it can't find the packages until they're generated again. StaleProjects are the
	// projects whose files do, by folder in the workspace: one outside the solution that was restored
	// stays so until it is restored itself.
	Stale         int
	StaleProjects []string
}

// Actionable is how many of the missing packages a fetch could bring: not those the last run found
// no feed to have, and none while a run is on (they're on their way).
func (st SyncState) Actionable() int {
	if st.Running {
		return 0
	}
	nowhere := map[Package]bool{}
	if st.Last != nil {
		for _, p := range st.Last.Unresolved {
			nowhere[p] = true
		}
	}
	distinct := map[Package]bool{}
	for _, g := range st.Missing {
		for _, p := range append(append([]Package(nil), g.Packages...), g.Downloads...) {
			if !nowhere[p] {
				distinct[p] = true
			}
		}
	}
	return len(distinct)
}

// ErrSyncRunning is what Run says when one is under way.
var ErrSyncRunning = errors.New("packages are already being fetched")

// Sync keeps the host's package cache holding what the sandbox's restores
// resolved. The sandbox keeps its own packages apart (it can't write the
// host's cache, which is what stops it planting code in a package the host
// builds with), so an IDE on the host can't find what it restored. Sync reads
// the project.assets.json files the restores left in the workspace, as data,
// and has the host fetch what's missing, from the user's own feeds, exactly as
// it fetches a package the agent asks for.
type Sync struct {
	Workspace string
	Cache     string
	// Options are where and how to fetch, asked for each run.
	Options func() Options

	// now is the time, for a test to set.
	now func() time.Time

	// fetch is FetchBatch; tests stand in for it.
	fetch func(ctx context.Context, framework string, pkgs, downloads []Package, opts Options) (BatchResult, error)

	mu      sync.Mutex
	parsed  map[string]parsedAssets
	named   map[string]namedFiles
	state   SyncState
	running bool
	// graphs are what each project's restore resolved, a framework at a time:
	// every one consistent with itself, which a mix of them need not be.
	graphs []Group
	// runGroups is what the run under way, or the last, is fetching; State
	// only reads it while one is under way.
	runGroups []Group
}

// parsedAssets is a file read, kept until it changes.
type parsedAssets struct {
	modified time.Time
	size     int64
	// read is when it was read (see unchanged).
	read   time.Time
	assets Assets
}

// namedFiles is the packages a project.nuget.cache names outside the cache, kept until it changes.
type namedFiles struct {
	modified  time.Time
	size      int64
	read      time.Time
	elsewhere []Package
}

// racyWindow is how soon after a file's time a look at it can't be trusted to tell it was written again. A file
// system's clock ticks (milliseconds on some, two seconds on FAT), and a file written twice within a tick has the same
// time both times, and the same size if what was written is as long: so what was read within a window of its time is
// read again, as git does of "racy" entries. What was left alone for longer is trusted, which is what keeps the look
// at a big workspace cheap.
const racyWindow = 2 * time.Second

// unchanged says a file read at read, when it was modified and of size, is what is there now, from info: its size and
// time are the same, and it had been left alone long enough for a write after the read to have changed them. A time
// ahead of the clock (a skewed one) is never old enough.
func unchanged(modified time.Time, size int64, read time.Time, info os.FileInfo) bool {
	return modified.Equal(info.ModTime()) && size == info.Size() && read.Sub(modified) >= racyWindow
}

// Check looks at the workspace's assets files, and works out what's missing.
// A file that hasn't changed isn't read again.
func (s *Sync) Check() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkLocked()
}

// CheckWithin is Check, unless the last look was less than age ago: asked for as often as an
// agent builds, a look at the whole workspace need not be made each time.
func (s *Sync) CheckWithin(age time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.Checked.IsZero() && s.clock().Sub(s.state.Checked) < age {
		return
	}
	s.checkLocked()
}

func (s *Sync) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Sync) checkLocked() {
	if s.parsed == nil {
		s.parsed = map[string]parsedAssets{}
	}
	if s.named == nil {
		s.named = map[string]namedFiles{}
	}
	elsewhere := map[Package]bool{}
	staleProjects := map[string]bool{}
	frameworks := map[string]map[Package]bool{}
	downloads := map[string]map[Package]bool{}
	var graphs []Group
	graphKeys := map[string]bool{}
	seen := map[string]bool{}
	var problems []string
	skipped := 0
	for _, path := range FindAssets(s.Workspace) {
		seen[path] = true
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		kept, ok := s.parsed[path]
		if !ok || !unchanged(kept.modified, kept.size, kept.read, info) {
			read := s.clock()
			assets, err := ReadAssets(path)
			if err != nil {
				delete(s.parsed, path)
				problems = append(problems, strings.TrimPrefix(err.Error(), s.Workspace+string(os.PathSeparator)))
				continue
			}
			kept = parsedAssets{info.ModTime(), info.Size(), read, assets}
			s.parsed[path] = kept
		}
		skipped += kept.assets.Skipped
		for _, p := range s.namedElsewhere(filepath.Join(filepath.Dir(path), "project.nuget.cache")) {
			elsewhere[p] = true
			if len(Missing(s.Cache, []Package{p})) == 0 {
				staleProjects[s.projectName(path)] = true
			}
		}
		for fw := range unionKeys(kept.assets.ByFramework, kept.assets.Downloads) {
			pkgs, only := kept.assets.ByFramework[fw], kept.assets.Downloads[fw]
			if frameworks[fw] == nil {
				frameworks[fw] = map[Package]bool{}
				downloads[fw] = map[Package]bool{}
			}
			for _, p := range pkgs {
				frameworks[fw][p] = true
			}
			for _, p := range only {
				downloads[fw][p] = true
			}
			if key := graphKey(fw, pkgs) + "|downloads:" + graphKey("", only); !graphKeys[key] {
				graphKeys[key] = true
				graphs = append(graphs, Group{Framework: fw, Packages: pkgs, Downloads: only})
			}
		}
	}
	for path := range s.parsed {
		if !seen[path] {
			delete(s.parsed, path)
			delete(s.named, filepath.Join(filepath.Dir(path), "project.nuget.cache"))
		}
	}
	stale := 0
	for p := range elsewhere {
		if len(Missing(s.Cache, []Package{p})) == 0 {
			stale++
		}
	}

	names := make([]string, 0, len(frameworks))
	for fw := range frameworks {
		names = append(names, fw)
	}
	sort.Strings(names)
	var missing []Group
	distinct := map[Package]bool{}
	for _, fw := range names {
		var pkgs []Package
		for p := range frameworks[fw] {
			pkgs = append(pkgs, p)
		}
		sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ID < pkgs[j].ID })
		var only []Package
		for p := range downloads[fw] {
			if !frameworks[fw][p] {
				only = append(only, p)
			}
		}
		sort.Slice(only, func(i, j int) bool { return only[i].ID < only[j].ID })
		lacking, lackingDownloads := Missing(s.Cache, pkgs), Missing(s.Cache, only)
		if len(lacking) > 0 || len(lackingDownloads) > 0 {
			missing = append(missing, Group{Framework: fw, Packages: lacking, Downloads: lackingDownloads})
			for _, p := range append(append([]Package(nil), lacking...), lackingDownloads...) {
				distinct[p] = true
			}
		}
	}
	sort.Slice(graphs, func(i, j int) bool {
		if graphs[i].Framework != graphs[j].Framework {
			return graphs[i].Framework < graphs[j].Framework
		}
		return graphKey("", graphs[i].Packages)+graphKey("", graphs[i].Downloads) < graphKey("", graphs[j].Packages)+graphKey("", graphs[j].Downloads)
	})
	s.graphs = graphs
	s.state.Missing, s.state.Count = missing, len(distinct)
	s.state.Skipped, s.state.Problems, s.state.Stale = skipped, problems, stale
	s.state.StaleProjects = sortedKeys(staleProjects)
	s.state.Checked = s.clock()
}

// projectName is the folder of the project whose project.assets.json is at path, within the workspace.
func (s *Sync) projectName(assets string) string {
	dir := filepath.Dir(filepath.Dir(assets))
	rel, err := filepath.Rel(s.Workspace, dir)
	if err != nil || rel == "." {
		return filepath.Base(dir)
	}
	return filepath.ToSlash(rel)
}

func sortedKeys(set map[string]bool) []string {
	var keys []string
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// namedElsewhere is what the project.nuget.cache at path names outside the user's cache; a file
// that can't be read names nothing (the assets file it sits by is what reports problems).
func (s *Sync) namedElsewhere(path string) []Package {
	info, err := os.Stat(path)
	if err != nil {
		delete(s.named, path)
		return nil
	}
	if kept, ok := s.named[path]; ok && unchanged(kept.modified, kept.size, kept.read, info) {
		return kept.elsewhere
	}
	read := s.clock()
	roots := []string{s.Cache}
	if real, err := filepath.EvalSymlinks(s.Cache); err == nil {
		roots = append(roots, real)
	}
	pkgs, err := ReadNamedElsewhere(path, roots...)
	if err != nil {
		pkgs = nil
	}
	s.named[path] = namedFiles{info.ModTime(), info.Size(), read, pkgs}
	return pkgs
}

// unionKeys is the frameworks either map has.
func unionKeys(a, b map[string][]Package) map[string]bool {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return keys
}

// graphKey says whether two graphs are the same one.
func graphKey(framework string, pkgs []Package) string {
	names := make([]string, len(pkgs))
	for i, p := range pkgs {
		names[i] = p.ID + "/" + p.Version
	}
	sort.Strings(names)
	return framework + "|" + strings.Join(names, ",")
}

// State is where Sync stands.
func (s *Sync) State() SyncState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	st.Running = s.running
	if s.running {
		st.Progress = s.progressLocked()
	}
	return st
}

// progressLocked counts, in the cache, how many of the packages the run is for
// have come: a restore of hundreds says nothing until it ends, but each package
// shows in the cache when it's done. A package two frameworks share is one.
func (s *Sync) progressLocked() *Progress {
	total := map[Package]bool{}
	lacking := map[Package]bool{}
	for _, g := range s.runGroups {
		every := append(append([]Package(nil), g.Packages...), g.Downloads...)
		for _, p := range every {
			total[p] = true
		}
		for _, p := range Missing(s.Cache, every) {
			lacking[p] = true
		}
	}
	return &Progress{Done: len(total) - len(lacking), Total: len(total)}
}

// Run fetches what's missing, a project's resolved set at a time (what one
// brought isn't fetched again for the next), and says how it went in State's
// Last. Each set is consistent with itself; a mix of two projects' need not be
// (one with a package at 1.0, another with one that wants it from 2.0), and
// NuGet won't restore that. One run
// at a time. A package no feed has doesn't fail it: it's said, and stays
// missing.
func (s *Sync) Run(ctx context.Context) error {
	groups, graphs, err := s.begin()
	if err != nil {
		return err
	}
	return s.run(ctx, groups, graphs)
}

// Start is Run in the background, once it is under way: by the time it returns the run is
// running (State says so), or ErrSyncRunning says one already was. What the run comes to is on
// the channel, when it ends.
func (s *Sync) Start(ctx context.Context) (<-chan error, error) {
	groups, graphs, err := s.begin()
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- s.run(ctx, groups, graphs) }()
	return done, nil
}

// begin takes the right to run, looks at the workspace, and says what there is to fetch.
func (s *Sync) begin() (groups, graphs []Group, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil, nil, ErrSyncRunning
	}
	s.running = true
	s.checkLocked()
	s.runGroups = s.state.Missing
	return s.state.Missing, s.graphs, nil
}

// run fetches groups, a graph at a time, and ends the run begun.
func (s *Sync) run(ctx context.Context, groups, graphs []Group) error {
	defer func() {
		s.mu.Lock()
		s.running = false
		s.checkLocked()
		s.mu.Unlock()
	}()
	if len(groups) == 0 {
		return nil
	}

	fetch := s.fetch
	if fetch == nil {
		fetch = FetchBatchWith
	}
	result := &SyncResult{At: time.Now()}
	unresolved := map[Package]bool{}
	var failure error
	for _, g := range graphs {
		lacking, lackingDownloads := Missing(s.Cache, g.Packages), Missing(s.Cache, g.Downloads)
		if len(lacking) == 0 && len(lackingDownloads) == 0 {
			continue
		}
		res, err := fetch(ctx, g.Framework, lacking, lackingDownloads, s.Options())
		result.Fetched += len(res.Fetched)
		result.Failed = append(result.Failed, res.Failed...)
		for _, p := range res.Unresolved {
			unresolved[p] = true
		}
		if err != nil {
			// One set not fetching mustn't keep the others from it; the first
			// failure is the one said.
			if failure == nil {
				result.Error = fmt.Sprintf("fetching for %s: %s", g.Framework, err)
				result.Detail = reportable(res.Output)
				failure = err
			}
			if ctx.Err() != nil {
				break
			}
		}
	}
	for p := range unresolved {
		result.Unresolved = append(result.Unresolved, p)
	}
	sort.Slice(result.Unresolved, func(i, j int) bool { return result.Unresolved[i].ID < result.Unresolved[j].ID })
	s.mu.Lock()
	s.state.Last = result
	s.mu.Unlock()
	return failure
}
