package nugetfetch

import (
	"archive/zip"
	"bytes"
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

// A restore of hundreds of packages is one process saying nothing until it ends,
// so how far it has got is read from the cache: how many of the packages the run
// is for have come.
func TestWhileFetchingItSaysHowManyOfThePackagesHaveComeSoFar(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Pkg.A/1.0.0", "Pkg.B/1.0.0", "Pkg.C/1.0.0"}})
	landed, proceed := make(chan struct{}), make(chan struct{})
	rig.result = func(_ string, pkgs []Package) (BatchResult, error) {
		put(t, rig.cache, pkgs[0])
		close(landed)
		<-proceed
		for _, p := range pkgs[1:] {
			put(t, rig.cache, p)
		}
		return BatchResult{Fetched: pkgs}, nil
	}
	rig.s.Check()
	if rig.s.State().Progress != nil {
		t.Errorf("progress %+v before any run", *rig.s.State().Progress)
	}
	done := make(chan error, 1)
	go func() { done <- rig.s.Run(context.Background()) }()
	<-landed
	if got := rig.s.State().Progress; got == nil || *got != (Progress{Done: 1, Total: 3}) {
		t.Errorf("progress = %+v, want 1 of 3", got)
	}
	close(proceed)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := rig.s.State().Progress; got != nil {
		t.Errorf("progress %+v after the run ended", *got)
	}
}

// What two frameworks share is fetched once, so it's counted once.
func TestAPackageInTwoFrameworksIsOneOfTheTotal(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{
		"net462": {"Shared.Pkg/1.0.0", "Old.Only/1.0.0"},
		"net8.0": {"Shared.Pkg/1.0.0", "New.Only/1.0.0"},
	})
	started, release := make(chan struct{}), make(chan struct{})
	rig.result = func(string, []Package) (BatchResult, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return BatchResult{}, nil
	}
	rig.s.Check()
	done := make(chan error, 1)
	go func() { done <- rig.s.Run(context.Background()) }()
	<-started
	if got := rig.s.State().Progress; got == nil || got.Total != 3 || got.Done != 0 {
		t.Errorf("progress = %+v, want 0 of 3", got)
	}
	close(release)
	<-done
}

// addPackage puts a package with no files, only dependencies, in the feed: what
// a restore needs of it is its identity and what it depends on.
func addPackage(t *testing.T, feed, id, version string, deps map[string]string) {
	t.Helper()
	var group strings.Builder
	for dep, from := range deps {
		group.WriteString(`<dependency id="` + dep + `" version="` + from + `"/>`)
	}
	nuspec := `<?xml version="1.0"?><package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd"><metadata><id>` + id +
		`</id><version>` + version + `</version><authors>t</authors><description>t</description><dependencies><group targetFramework="net10.0">` +
		group.String() + `</group></dependencies></metadata></package>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(id + ".nuspec")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(nuspec)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(feed, strings.ToLower(id)+"."+version+".nupkg"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Each project's restore is consistent with itself, but what two projects resolved
// need not be consistent together: one has Demo.Lib at 1.0.0, another has Demo.Needs,
// which wants Demo.Lib from 2.0.0 (and so has it). Asked for all at once, that is a
// downgrade NuGet refuses (NU1605) and nothing comes; each project's own is fetched
// on its own, so it does.
func TestWhatTwoProjectsResolvedDifferentlyIsStillFetched(t *testing.T) {
	h := newHost(t)
	addPackage(t, h.feed, "Demo.Lib", "1.0.0", nil)
	addPackage(t, h.feed, "Demo.Lib", "2.0.0", nil)
	addPackage(t, h.feed, "Demo.Needs", "1.0.0", map[string]string{"Demo.Lib": "2.0.0"})
	ws := t.TempDir()
	writeAssets(t, filepath.Join(ws, "A"), assetsJSON(map[string][]string{"net10.0": {"Demo.Lib/1.0.0"}}))
	writeAssets(t, filepath.Join(ws, "B"), assetsJSON(map[string][]string{"net10.0": {"Demo.Needs/1.0.0", "Demo.Lib/2.0.0"}}))
	s := &Sync{Workspace: ws, Cache: h.cache, Options: h.options}
	s.Check()
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v\n%+v", err, s.State().Last)
	}
	for _, want := range []string{"demo.lib/1.0.0", "demo.lib/2.0.0", "demo.needs/1.0.0"} {
		if _, err := os.Stat(filepath.Join(h.cache, filepath.FromSlash(want), ".nupkg.metadata")); err != nil {
			t.Errorf("%s: %v", want, err)
		}
	}
	if st := s.State(); st.Count != 0 {
		t.Errorf("still missing %v", st.Missing)
	}
}

// A set that can't be fetched mustn't stop the others being tried.
func TestAFailedSetDoesNotStopTheOthers(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Bad.Pkg/1.0.0"}})
	rig.project(t, "B", map[string][]string{"net8.0": {"Good.Pkg/1.0.0"}})
	rig.result = func(_ string, pkgs []Package) (BatchResult, error) {
		if pkgs[0].ID == "Bad.Pkg" {
			return BatchResult{Output: "error : the feed fell over"}, errors.New("fetching 1 packages: exit status 1")
		}
		for _, p := range pkgs {
			put(t, rig.cache, p)
		}
		return BatchResult{Fetched: pkgs}, nil
	}
	rig.s.Check()
	if err := rig.s.Run(context.Background()); err == nil {
		t.Error("a set failed and the run doesn't say so")
	}
	st := rig.s.State()
	if st.Last == nil || st.Last.Fetched != 1 || st.Last.Error == "" || st.Last.Detail == "" {
		t.Errorf("last %+v, want the one that came counted and the failure said", st.Last)
	}
	if st.Count != 1 {
		t.Errorf("count = %d, want only Bad.Pkg still missing", st.Count)
	}
}

// Packages NuGet failed on are said, each with its words, and the run isn't an error:
// the rest came.
func TestPackagesNuGetFailedOnAreSaidWithTheirWords(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"Bad.Pkg/1.0.0", "Good.Pkg/1.0.0"}})
	rig.result = func(_ string, pkgs []Package) (BatchResult, error) {
		put(t, rig.cache, Package{"Good.Pkg", "1.0.0"})
		return BatchResult{Fetched: []Package{{"Good.Pkg", "1.0.0"}}, Failed: []Failure{{Package{"Bad.Pkg", "1.0.0"}, "Central Directory corrupt"}}}, nil
	}
	rig.s.Check()
	if err := rig.s.Run(context.Background()); err != nil {
		t.Errorf("Run = %v: the rest came", err)
	}
	want := []Failure{{Package{"Bad.Pkg", "1.0.0"}, "Central Directory corrupt"}}
	if st := rig.s.State(); st.Last == nil || !reflect.DeepEqual(st.Last.Failed, want) || st.Last.Fetched != 1 {
		t.Errorf("last %+v, want failed %v", st.Last, want)
	}
}

func TestACancelledRunDoesNotGoOnToTheOtherSets(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"One.Pkg/1.0.0"}})
	rig.project(t, "B", map[string][]string{"net8.0": {"Two.Pkg/1.0.0"}})
	ctx, cancel := context.WithCancel(context.Background())
	asked := 0
	rig.result = func(string, []Package) (BatchResult, error) {
		asked++
		cancel()
		return BatchResult{}, errors.New("fetching 1 packages: signal: killed")
	}
	rig.s.Check()
	rig.s.Run(ctx)
	if asked != 1 {
		t.Errorf("%d sets asked for, want the run to stop at the cancel", asked)
	}
}

func TestWhenSeveralSetsFailItIsTheFirstThatIsSaid(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"A.First/1.0.0"}})
	rig.project(t, "B", map[string][]string{"net8.0": {"B.Second/1.0.0"}})
	rig.result = func(_ string, pkgs []Package) (BatchResult, error) {
		return BatchResult{Output: "output for " + pkgs[0].ID}, errors.New("failed for " + pkgs[0].ID)
	}
	rig.s.Check()
	rig.s.Run(context.Background())
	last := rig.s.State().Last
	if last == nil || !strings.Contains(last.Error, "A.First") || !strings.Contains(last.Detail, "A.First") || strings.Contains(last.Error, "B.Second") {
		t.Errorf("last %+v, want the first set's failure", last)
	}
}

// What a fetch could bring: the missing, less what the last run found no feed to have, and none while a run is on.
func TestWhatAFetchCouldBringIsWhatIsMissingLessWhatNoFeedHas(t *testing.T) {
	missing := []Group{{Framework: "net8.0", Packages: []Package{{"A", "1.0.0"}, {"B", "1.0.0"}, {"Gone", "9.0.0"}}}, {Framework: "net462", Packages: []Package{{"A", "1.0.0"}}}}
	st := SyncState{Missing: missing, Count: 3}
	if got := st.Actionable(); got != 3 {
		t.Errorf("Actionable = %d, want the 3 different packages", got)
	}
	st.Last = &SyncResult{Unresolved: []Package{{"Gone", "9.0.0"}}}
	if got := st.Actionable(); got != 2 {
		t.Errorf("Actionable = %d, want 2 once one is known to be nowhere", got)
	}
	st.Running = true
	if got := st.Actionable(); got != 0 {
		t.Errorf("Actionable = %d while a run is on, want 0", got)
	}
}

// namesFiles writes a project's project.nuget.cache, naming the given files as the packages' files.
func (r *syncRig) namesFiles(t *testing.T, project string, files ...string) {
	t.Helper()
	var quoted []string
	for _, f := range files {
		quoted = append(quoted, `"`+filepath.ToSlash(f)+`"`)
	}
	body := `{"version":2,"expectedPackageFiles":[` + strings.Join(quoted, ",") + `],"logs":[]}`
	obj := filepath.Join(r.dir, project, "obj")
	if err := os.MkdirAll(obj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(obj, "project.nuget.cache"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha512In(root string, p Package) string {
	id, version := strings.ToLower(p.ID), strings.ToLower(p.Version)
	return filepath.Join(root, id, version, id+"."+version+".nupkg.sha512")
}

func TestPackagesTheGeneratedFilesNameElsewhereThatTheCacheHasAreStale(t *testing.T) {
	rig := newSyncRig(t)
	sandbox := t.TempDir()
	rig.project(t, "A", map[string][]string{"net8.0": {"In.Theirs/1.0.0", "In.Mine/1.0.0", "Not.Yet/1.0.0"}})
	for _, p := range []Package{{"In.Theirs", "1.0.0"}, {"In.Mine", "1.0.0"}} {
		put(t, rig.cache, p)
	}
	rig.namesFiles(t, "A", sha512In(rig.cache, Package{"In.Theirs", "1.0.0"}), sha512In(sandbox, Package{"In.Mine", "1.0.0"}), sha512In(sandbox, Package{"Not.Yet", "1.0.0"}))
	rig.s.Check()
	st := rig.s.State()
	if st.Stale != 1 {
		t.Errorf("%d stale, want 1: the one named in the sandbox's cache that theirs has (not the one theirs lacks, nor the one named in theirs)", st.Stale)
	}
}

func TestOnceTheFilesNameTheCachePackagesAreNoLongerStale(t *testing.T) {
	rig := newSyncRig(t)
	sandbox := t.TempDir()
	p := Package{"In.Mine", "1.0.0"}
	put(t, rig.cache, p)
	rig.project(t, "A", map[string][]string{"net8.0": {"In.Mine/1.0.0"}})
	rig.namesFiles(t, "A", sha512In(sandbox, p))
	rig.s.Check()
	if rig.s.State().Stale != 1 {
		t.Fatal("not stale to begin with")
	}
	rig.namesFiles(t, "A", sha512In(rig.cache, p))
	rig.s.Check()
	if st := rig.s.State(); st.Stale != 0 {
		t.Errorf("still %d stale after the files name the cache", st.Stale)
	}
}

func TestAPackageTwoProjectsNameIsCountedOnce(t *testing.T) {
	rig := newSyncRig(t)
	sandbox := t.TempDir()
	p := Package{"In.Mine", "1.0.0"}
	put(t, rig.cache, p)
	rig.project(t, "A", map[string][]string{"net8.0": {"In.Mine/1.0.0"}})
	rig.project(t, "B", map[string][]string{"net8.0": {"In.Mine/1.0.0"}})
	rig.namesFiles(t, "A", sha512In(sandbox, p))
	rig.namesFiles(t, "B", sha512In(sandbox, p))
	rig.s.Check()
	if st := rig.s.State(); st.Stale != 1 {
		t.Errorf("%d stale, want 1", st.Stale)
	}
}

func TestFilesThatArentAPackagesAreNeverTakenForOne(t *testing.T) {
	rig := newSyncRig(t)
	put(t, rig.cache, Package{"In.Mine", "1.0.0"})
	rig.project(t, "A", map[string][]string{"net8.0": {"In.Mine/1.0.0"}})
	rig.namesFiles(t, "A", "/etc/passwd", "../../x/.nupkg.sha512", "/a/in.mine/../1.0.0/x.nupkg.sha512", "/a/in.mine/1.0.0/other.txt", "/a/b c/1.0.0/b c.1.0.0.nupkg.sha512", "/a/x;y/1.0.0/x;y.1.0.0.nupkg.sha512", "")
	rig.s.Check()
	if st := rig.s.State(); st.Stale != 0 {
		t.Errorf("%d stale from files that name no package", st.Stale)
	}
}

func TestACacheFileThatCantBeReadIsNotStaleNorFatal(t *testing.T) {
	rig := newSyncRig(t)
	rig.project(t, "A", map[string][]string{"net8.0": {"In.Mine/1.0.0"}})
	obj := filepath.Join(rig.dir, "A", "obj")
	if err := os.WriteFile(filepath.Join(obj, "project.nuget.cache"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	rig.s.Check()
	if st := rig.s.State(); st.Stale != 0 || st.Count != 1 {
		t.Errorf("stale %d, missing %d: want 0 and 1", st.Stale, st.Count)
	}
}
