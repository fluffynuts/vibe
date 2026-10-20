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
	// files, so it can't find the packages until they're generated again.
	Stale int
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
		for _, p := range g.Packages {
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

	// fetch is FetchBatch; tests stand in for it.
	fetch func(ctx context.Context, framework string, pkgs []Package, opts Options) (BatchResult, error)

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
	assets   Assets
}

// namedFiles is the packages a project.nuget.cache names outside the cache, kept until it changes.
type namedFiles struct {
	modified  time.Time
	size      int64
	elsewhere []Package
}

// Check looks at the workspace's assets files, and works out what's missing.
// A file that hasn't changed isn't read again.
func (s *Sync) Check() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkLocked()
}

func (s *Sync) checkLocked() {
	if s.parsed == nil {
		s.parsed = map[string]parsedAssets{}
	}
	if s.named == nil {
		s.named = map[string]namedFiles{}
	}
	elsewhere := map[Package]bool{}
	frameworks := map[string]map[Package]bool{}
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
		if !ok || !kept.modified.Equal(info.ModTime()) || kept.size != info.Size() {
			assets, err := ReadAssets(path)
			if err != nil {
				delete(s.parsed, path)
				problems = append(problems, strings.TrimPrefix(err.Error(), s.Workspace+string(os.PathSeparator)))
				continue
			}
			kept = parsedAssets{info.ModTime(), info.Size(), assets}
			s.parsed[path] = kept
		}
		skipped += kept.assets.Skipped
		for _, p := range s.namedElsewhere(filepath.Join(filepath.Dir(path), "project.nuget.cache")) {
			elsewhere[p] = true
		}
		for fw, pkgs := range kept.assets.ByFramework {
			if frameworks[fw] == nil {
				frameworks[fw] = map[Package]bool{}
			}
			for _, p := range pkgs {
				frameworks[fw][p] = true
			}
			if key := graphKey(fw, pkgs); !graphKeys[key] {
				graphKeys[key] = true
				graphs = append(graphs, Group{Framework: fw, Packages: pkgs})
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
		if lacking := Missing(s.Cache, pkgs); len(lacking) > 0 {
			missing = append(missing, Group{Framework: fw, Packages: lacking})
			for _, p := range lacking {
				distinct[p] = true
			}
		}
	}
	sort.Slice(graphs, func(i, j int) bool {
		if graphs[i].Framework != graphs[j].Framework {
			return graphs[i].Framework < graphs[j].Framework
		}
		return graphKey("", graphs[i].Packages) < graphKey("", graphs[j].Packages)
	})
	s.graphs = graphs
	s.state.Missing, s.state.Count = missing, len(distinct)
	s.state.Skipped, s.state.Problems, s.state.Stale = skipped, problems, stale
	s.state.Checked = time.Now()
}

// namedElsewhere is what the project.nuget.cache at path names outside the user's cache; a file
// that can't be read names nothing (the assets file it sits by is what reports problems).
func (s *Sync) namedElsewhere(path string) []Package {
	info, err := os.Stat(path)
	if err != nil {
		delete(s.named, path)
		return nil
	}
	if kept, ok := s.named[path]; ok && kept.modified.Equal(info.ModTime()) && kept.size == info.Size() {
		return kept.elsewhere
	}
	roots := []string{s.Cache}
	if real, err := filepath.EvalSymlinks(s.Cache); err == nil {
		roots = append(roots, real)
	}
	pkgs, err := ReadNamedElsewhere(path, roots...)
	if err != nil {
		pkgs = nil
	}
	s.named[path] = namedFiles{info.ModTime(), info.Size(), pkgs}
	return pkgs
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
		for _, p := range g.Packages {
			total[p] = true
		}
		for _, p := range Missing(s.Cache, g.Packages) {
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
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrSyncRunning
	}
	s.running = true
	s.checkLocked()
	groups := s.state.Missing
	graphs := s.graphs
	s.runGroups = groups
	s.mu.Unlock()
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
		fetch = FetchBatch
	}
	result := &SyncResult{At: time.Now()}
	unresolved := map[Package]bool{}
	var failure error
	for _, g := range graphs {
		lacking := Missing(s.Cache, g.Packages)
		if len(lacking) == 0 {
			continue
		}
		res, err := fetch(ctx, g.Framework, lacking, s.Options())
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
