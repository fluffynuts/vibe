package nugetfetch

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	// Error says why a run failed; Detail is what NuGet said.
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
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
	Last    *SyncResult
	// Skipped is what the assets files named that wasn't a package id and an
	// exact version (not fetched); Problems are files that couldn't be read.
	Skipped  int
	Problems []string
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
	state   SyncState
	running bool
}

// parsedAssets is a file read, kept until it changes.
type parsedAssets struct {
	modified time.Time
	size     int64
	assets   Assets
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
	frameworks := map[string]map[Package]bool{}
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
		for fw, pkgs := range kept.assets.ByFramework {
			if frameworks[fw] == nil {
				frameworks[fw] = map[Package]bool{}
			}
			for _, p := range pkgs {
				frameworks[fw][p] = true
			}
		}
	}
	for path := range s.parsed {
		if !seen[path] {
			delete(s.parsed, path)
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
	s.state.Missing, s.state.Count = missing, len(distinct)
	s.state.Skipped, s.state.Problems = skipped, problems
	s.state.Checked = time.Now()
}

// State is where Sync stands.
func (s *Sync) State() SyncState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	st.Running = s.running
	return st
}

// Run fetches what's missing, a framework at a time (what one brought isn't
// fetched again for the next), and says how it went in State's Last. One run
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
	for _, g := range groups {
		lacking := Missing(s.Cache, g.Packages)
		if len(lacking) == 0 {
			continue
		}
		res, err := fetch(ctx, g.Framework, lacking, s.Options())
		result.Fetched += len(res.Fetched)
		for _, p := range res.Unresolved {
			unresolved[p] = true
		}
		if err != nil {
			result.Error = fmt.Sprintf("fetching for %s: %s", g.Framework, err)
			result.Detail = reportable(res.Output)
			failure = err
			break
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
