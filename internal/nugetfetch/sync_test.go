package nugetfetch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncRig struct {
	s       *Sync
	cache   string
	dir     string
	mu      sync.Mutex
	fetched map[string][]Package // by framework, as asked
	result  func(fw string, pkgs []Package) (BatchResult, error)
}

func newSyncRig(t *testing.T) *syncRig {
	t.Helper()
	rig := &syncRig{cache: t.TempDir(), dir: t.TempDir(), fetched: map[string][]Package{}}
	rig.s = &Sync{
		Workspace: rig.dir,
		Cache:     rig.cache,
		Options:   func() Options { return Options{Cache: rig.cache} },
		fetch: func(ctx context.Context, fw string, pkgs []Package, opts Options) (BatchResult, error) {
			rig.mu.Lock()
			rig.fetched[fw] = append(rig.fetched[fw], pkgs...)
			rig.mu.Unlock()
			if rig.result != nil {
				return rig.result(fw, pkgs)
			}
			// What a real one does: the packages come.
			for _, p := range pkgs {
				put(t, rig.cache, p)
			}
			return BatchResult{Fetched: pkgs}, nil
		},
	}
	return rig
}

func put(t *testing.T, cache string, p Package) {
	t.Helper()
	dir := filepath.Join(cache, strings.ToLower(p.ID), strings.ToLower(p.Version))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".nupkg.metadata"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r *syncRig) project(t *testing.T, name string, targets map[string][]string) {
	t.Helper()
	writeAssets(t, filepath.Join(r.dir, name), assetsJSON(targets))
}

func TestWhatTheSandboxRestoredThatTheCacheLacksIsWhatsMissing(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Have.Pkg/1.0.0", "Lack.One/2.0.0", "Lack.Two/3.0.0"}})
	put(t, rig.cache, Package{"Have.Pkg", "1.0.0"})
	rig.s.Check()
	st := rig.s.State()
	want := []Group{{Framework: "net8.0", Packages: []Package{{"Lack.One", "2.0.0"}, {"Lack.Two", "3.0.0"}}}}
	if !reflect.DeepEqual(st.Missing, want) || st.Count != 2 {
		t.Errorf("missing %v (count %d), want %v", st.Missing, st.Count, want)
	}
	if st.Checked.IsZero() {
		t.Error("it doesn't say when it looked")
	}
}

func TestEverythingTheCacheHasIsNothingMissing(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Have.Pkg/1.0.0"}})
	put(t, rig.cache, Package{"Have.Pkg", "1.0.0"})
	rig.s.Check()
	if st := rig.s.State(); st.Count != 0 || len(st.Missing) != 0 {
		t.Errorf("missing %v", st.Missing)
	}
}

func TestPackagesInSeveralProjectsAreCountedOnce(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Shared.Pkg/1.0.0", "Only.A/1.0.0"}})
	rig.project(t, "B", map[string][]string{"net8.0": {"Shared.Pkg/1.0.0"}, "net462": {"Old.Pkg/1.0.0", "Shared.Pkg/1.0.0"}})
	rig.s.Check()
	st := rig.s.State()
	if st.Count != 3 {
		t.Errorf("count = %d, want 3 distinct packages (Shared.Pkg, Only.A, Old.Pkg)", st.Count)
	}
	if len(st.Missing) != 2 || len(st.Missing[0].Packages) != 2 {
		t.Errorf("groups = %v", st.Missing)
	}
}

func TestRunningFetchesWhatsMissingForEachFrameworkAndThenNothingIs(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Lack.One/1.0.0"}, "net462": {"Lack.Two/1.0.0"}})
	rig.s.Check()
	if err := rig.s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rig.fetched["net8.0"]) != 1 || len(rig.fetched["net462"]) != 1 {
		t.Errorf("fetched %v", rig.fetched)
	}
	st := rig.s.State()
	if st.Count != 0 || st.Last == nil || st.Last.Fetched != 2 || st.Last.Error != "" {
		t.Errorf("after: %+v last %+v", st, st.Last)
	}
}

// What came for one framework isn't fetched again for the next.
func TestAPackageInTwoFrameworksIsFetchedOnce(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Shared.Pkg/1.0.0"}, "net462": {"Shared.Pkg/1.0.0"}})
	rig.s.Check()
	rig.s.Run(context.Background())
	total := 0
	for _, pkgs := range rig.fetched {
		total += len(pkgs)
	}
	if total != 1 {
		t.Errorf("fetched %d times, want once: %v", total, rig.fetched)
	}
}

func TestWhatNoFeedHasIsSaidAndStaysMissing(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Lack.One/1.0.0", "Gone.Pkg/9.9.9"}})
	rig.result = func(fw string, pkgs []Package) (BatchResult, error) {
		put(t, rig.cache, Package{"Lack.One", "1.0.0"})
		return BatchResult{Fetched: []Package{{"Lack.One", "1.0.0"}}, Unresolved: []Package{{"Gone.Pkg", "9.9.9"}}}, nil
	}
	rig.s.Check()
	rig.s.Run(context.Background())
	st := rig.s.State()
	if st.Last == nil || !reflect.DeepEqual(st.Last.Unresolved, []Package{{"Gone.Pkg", "9.9.9"}}) || st.Last.Fetched != 1 {
		t.Errorf("last = %+v", st.Last)
	}
	if st.Count != 1 {
		t.Errorf("count = %d, want the one still missing", st.Count)
	}
}

func TestAFailedFetchIsSaidWithItsReasonAndNothingIsClaimedFetched(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Lack.One/1.0.0"}})
	rig.result = func(string, []Package) (BatchResult, error) {
		return BatchResult{Output: "error NU1301: the feed isn't reachable"}, errors.New("fetching 1 packages: exit status 1")
	}
	rig.s.Check()
	err := rig.s.Run(context.Background())
	st := rig.s.State()
	if err == nil || st.Last == nil || st.Last.Error == "" || st.Last.Fetched != 0 {
		t.Errorf("err %v, last %+v", err, st.Last)
	}
	if st.Last.Detail == "" {
		t.Error("NuGet's own words aren't kept for the page to show")
	}
	if st.Count != 1 {
		t.Errorf("count = %d", st.Count)
	}
}

func TestOneRunAtATime(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Lack.One/1.0.0"}})
	started, release := make(chan struct{}), make(chan struct{})
	rig.result = func(string, []Package) (BatchResult, error) {
		close(started)
		<-release
		return BatchResult{}, nil
	}
	rig.s.Check()
	done := make(chan error, 1)
	go func() { done <- rig.s.Run(context.Background()) }()
	<-started
	if !rig.s.State().Running {
		t.Error("a run is under way but the state doesn't say so")
	}
	if err := rig.s.Run(context.Background()); !errors.Is(err, ErrSyncRunning) {
		t.Errorf("a second run = %v, want ErrSyncRunning", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if rig.s.State().Running {
		t.Error("still running after it ended")
	}
}

func TestANothingToDoRunDoesNothing(t *testing.T) {
	rig := newSyncRig(t)
	rig.s.Check()
	if err := rig.s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rig.fetched) != 0 {
		t.Errorf("fetched %v with nothing missing", rig.fetched)
	}
}

func TestAChangedAssetsFileIsReadAgainAndAGoneOneForgotten(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"First.Pkg/1.0.0"}})
	rig.s.Check()
	if rig.s.State().Count != 1 {
		t.Fatal("the first reading missed it")
	}
	// A different file, a moment later: the restore the agent just ran.
	path := writeAssets(t, filepath.Join(rig.dir, "A"), assetsJSON(map[string][]string{"net8.0": {"First.Pkg/1.0.0", "Second.Pkg/1.0.0", "Third.Pkg/1.0.0"}}))
	later := time.Now().Add(time.Minute)
	os.Chtimes(path, later, later)
	rig.s.Check()
	if rig.s.State().Count != 3 {
		t.Errorf("count = %d after the file changed, want 3", rig.s.State().Count)
	}
	os.RemoveAll(filepath.Join(rig.dir, "A"))
	rig.s.Check()
	if rig.s.State().Count != 0 {
		t.Errorf("count = %d after the project went, want 0", rig.s.State().Count)
	}
}

func TestAnUnreadableAssetsFileIsSaidAndTheRestAreStillUsed(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "Good", map[string][]string{"net8.0": {"Lack.One/1.0.0"}})
	writeAssets(t, filepath.Join(rig.dir, "Broken"), "this isn't json")
	rig.s.Check()
	st := rig.s.State()
	if st.Count != 1 || len(st.Problems) != 1 {
		t.Errorf("count %d, problems %v", st.Count, st.Problems)
	}
}

func TestWhatTheSandboxWroteThatIsntAPackageIsCountedNotFetched(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Good.Pkg/1.0.0", `Evil"/><x/1.0.0`}})
	rig.s.Check()
	rig.s.Run(context.Background())
	st := rig.s.State()
	for _, pkgs := range rig.fetched {
		for _, p := range pkgs {
			if p.ID != "Good.Pkg" {
				t.Errorf("fetched %v", p)
			}
		}
	}
	if st.Skipped != 1 {
		t.Errorf("Skipped = %d, want the one that wasn't a package counted", st.Skipped)
	}
}

// Against the real thing: assets naming packages from a local feed, fetched by
// dotnet into the cache, as the Admin tab's button does.
func TestAnEndToEndSyncPutsWhatTheSandboxRestoredInTheHostCache(t *testing.T) {
	h := newHost(t)
	ws := t.TempDir()
	writeAssets(t, filepath.Join(ws, "App"), assetsJSON(map[string][]string{"net10.0": {"Demo.Pkg/1.0.0", "Demo.Two/1.0.0", "Demo.Three/1.0.0"}}))
	put(t, h.cache, Package{"Demo.Three", "1.0.0"}) // already there: not fetched again
	s := &Sync{Workspace: ws, Cache: h.cache, Options: h.options}
	s.Check()
	if s.State().Count != 2 {
		t.Fatalf("missing %v", s.State().Missing)
	}
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v (%+v)", err, s.State().Last)
	}
	for _, id := range []string{"demo.pkg", "demo.two"} {
		if _, err := os.Stat(filepath.Join(h.cache, id, "1.0.0", ".nupkg.metadata")); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	if st := s.State(); st.Count != 0 || st.Last.Fetched != 2 {
		t.Errorf("after: count %d, last %+v", st.Count, st.Last)
	}
}
