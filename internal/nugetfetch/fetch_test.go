package nugetfetch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"vibe/internal/packagereq"
)

// These restore for real, so they need dotnet, as the git tests need git.
func requireDotnet(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet not installed")
	}
}

type host struct {
	home, feed, cache, root string
}

// newHost is a host with a NuGet config naming one local feed, holding
// Demo.Pkg 1.0.0, and nothing else: no nuget.org, so no network.
func newHost(t *testing.T) host {
	t.Helper()
	requireDotnet(t)
	dir := t.TempDir()
	h := host{
		home:  filepath.Join(dir, "home"),
		feed:  filepath.Join(dir, "feed"),
		cache: filepath.Join(dir, "home", ".nuget", "packages"),
		root:  filepath.Join(dir, "home", ".vibe", "nuget-fetch"),
	}
	for _, d := range []string{h.feed, filepath.Join(h.home, ".nuget", "NuGet"), h.root} {
		must(t, os.MkdirAll(d, 0o755))
	}
	must(t, os.WriteFile(filepath.Join(h.home, ".nuget", "NuGet", "NuGet.Config"), []byte(
		`<configuration><packageSources><clear/><add key="local" value="`+h.feed+`"/></packageSources></configuration>`), 0o644))
	packDemo(t, h)
	return h
}

// demoPackages is where the test packages are packed, once for the whole run:
// packing is slow, and every test wants the same ones.
var demoPackages struct {
	once sync.Once
	dir  string
	err  error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if demoPackages.dir != "" {
		os.RemoveAll(demoPackages.dir)
	}
	os.Exit(code)
}

func packDemoOnce() (string, error) {
	demoPackages.once.Do(func() {
		dir, err := os.MkdirTemp("", "nugetfetch-demo-")
		if err != nil {
			demoPackages.err = err
			return
		}
		demoPackages.dir = dir
		home := filepath.Join(dir, "home")
		if demoPackages.err = os.MkdirAll(home, 0o755); demoPackages.err != nil {
			return
		}
		for _, id := range []string{"Demo.Pkg", "Demo.Two", "Demo.Three"} {
			src := filepath.Join(dir, "src", id)
			if demoPackages.err = os.MkdirAll(src, 0o755); demoPackages.err != nil {
				return
			}
			project := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework><PackageId>` + id + `</PackageId><Version>1.0.0</Version></PropertyGroup></Project>`
			if demoPackages.err = os.WriteFile(filepath.Join(src, id+".csproj"), []byte(project), 0o644); demoPackages.err != nil {
				return
			}
			if demoPackages.err = os.WriteFile(filepath.Join(src, "C.cs"), []byte("class C {}"), 0o644); demoPackages.err != nil {
				return
			}
			cmd := exec.Command("dotnet", "pack", "-o", filepath.Join(dir, "feed"))
			cmd.Dir = src
			cmd.Env = append(os.Environ(), "HOME="+home, "NUGET_PACKAGES="+filepath.Join(dir, "cache"),
				"DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1")
			if out, err := cmd.CombinedOutput(); err != nil {
				demoPackages.err = fmt.Errorf("packing %s: %v\n%s", id, err, out)
				return
			}
		}
	})
	return filepath.Join(demoPackages.dir, "feed"), demoPackages.err
}

func packDemo(t *testing.T, h host) {
	t.Helper()
	feed, err := packDemoOnce()
	if err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(feed)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(feed, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		must(t, os.WriteFile(filepath.Join(h.feed, f.Name()), data, 0o644))
	}
}

func (h host) options() Options {
	return Options{
		Root:    h.root,
		Cache:   h.cache,
		Env:     append(os.Environ(), "HOME="+h.home, "DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1"),
		Timeout: 2 * time.Minute,
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestFetchPutsThePackageInTheHostCacheAndCleansUp(t *testing.T) {
	h := newHost(t)
	res, err := Fetch(context.Background(), Request{ID: "Demo.Pkg", Version: "1.0.0", Framework: "net10.0"}, h.options())
	if err != nil {
		t.Fatalf("Fetch: %v\n%s", err, res.Output)
	}
	if _, err := os.Stat(filepath.Join(h.cache, "demo.pkg", "1.0.0")); err != nil {
		t.Errorf("package not in the cache: %v", err)
	}
	if !slices.Contains(res.Fetched, "demo.pkg 1.0.0") {
		t.Errorf("Fetched = %v, want demo.pkg 1.0.0", res.Fetched)
	}
	if left, _ := os.ReadDir(h.root); len(left) != 0 {
		t.Errorf("throwaway folder left behind: %v", left)
	}
}

func TestFetchSaysWhatNuGetSaidWhenThePackageIsNowhere(t *testing.T) {
	h := newHost(t)
	res, err := Fetch(context.Background(), Request{ID: "Nope.Pkg", Version: "1.0.0", Framework: "net10.0"}, h.options())
	if err == nil {
		t.Fatal("fetching a package no feed has should fail")
	}
	if !strings.Contains(res.Output, "NU1101") {
		t.Errorf("output should carry NuGet's own message: %s", res.Output)
	}
}

func TestFetchRefusesAnInvalidRequestBeforeRunningAnything(t *testing.T) {
	h := newHost(t)
	_, err := Fetch(context.Background(), Request{ID: "../x", Version: "1.0.0", Framework: "net10.0"}, h.options())
	if err == nil {
		t.Fatal("invalid request accepted")
	}
}

func TestFetchRunsNothingPlantedAboveItsFolder(t *testing.T) {
	h := newHost(t)
	marker := filepath.Join(t.TempDir(), "ran")
	plant := `<Project><Target Name="Pwn" BeforeTargets="Restore;_GenerateRestoreGraph"><Exec Command="touch ` + marker + `" /></Target></Project>`
	for _, name := range []string{"Directory.Build.targets", "Directory.Build.props"} {
		must(t, os.WriteFile(filepath.Join(filepath.Dir(h.root), name), []byte(plant), 0o644))
	}
	res, err := Fetch(context.Background(), Request{ID: "Demo.Pkg", Version: "1.0.0", Framework: "net10.0"}, h.options())
	if err != nil {
		t.Fatalf("Fetch: %v\n%s", err, res.Output)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a Directory.Build file above the fetch folder ran on the host")
	}
}

func TestFetchIgnoresANuGetConfigThatIsNotTheUsers(t *testing.T) {
	h := newHost(t)
	// In the workspace, which is nowhere above the fetch folder.
	workspace := t.TempDir()
	must(t, os.WriteFile(filepath.Join(workspace, "nuget.config"), []byte(
		`<configuration><packageSources><clear/><add key="local" value="https://evil.invalid/"/></packageSources></configuration>`), 0o644))
	if _, err := Fetch(context.Background(), Request{ID: "Demo.Pkg", Version: "1.0.0", Framework: "net10.0"}, h.options()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestFetchLayersAnApprovedWorkspaceConfigOverTheUsers(t *testing.T) {
	h := newHost(t)
	// The user's config no longer names the feed; only the approved one does.
	must(t, os.WriteFile(filepath.Join(h.home, ".nuget", "NuGet", "NuGet.Config"), []byte(
		`<configuration><packageSources><clear/></packageSources></configuration>`), 0o644))
	req := Request{ID: "Demo.Pkg", Version: "1.0.0", Framework: "net10.0"}
	if _, err := Fetch(context.Background(), req, h.options()); err == nil {
		t.Fatal("without the approved config the feed is unknown, so this should fail")
	}
	opts := h.options()
	opts.ApprovedConfig = []byte(`<configuration><packageSources><add key="ws" value="` + h.feed + `"/></packageSources></configuration>`)
	if res, err := Fetch(context.Background(), req, opts); err != nil {
		t.Fatalf("Fetch with the approved config: %v\n%s", err, res.Output)
	}
}

func TestFetchStopsAtItsTimeLimitAndSaysSo(t *testing.T) {
	h := newHost(t)
	opts := h.options()
	opts.Timeout = time.Nanosecond
	_, err := Fetch(context.Background(), Request{ID: "Demo.Pkg", Version: "1.0.0", Framework: "net10.0"}, opts)
	if err == nil || !strings.Contains(err.Error(), "time limit") {
		t.Errorf("want a time-limit error, got %v", err)
	}
}

func TestAnApprovedRequestIsFetchedThroughTheDeskIntoTheHostCache(t *testing.T) {
	h := newHost(t)
	box := packagereq.Mailbox{Dir: t.TempDir()}
	must(t, os.MkdirAll(box.Inbox(), 0o755))
	var said []string
	desk := &packagereq.Desk{
		Mailbox:    box,
		Ecosystems: map[string]packagereq.Ecosystem{"nuget": Ecosystem{Options: func(packagereq.Request) Options { return h.options() }}},
		Policy:     func() string { return packagereq.PolicyAsk },
		Log:        func(line string) { said = append(said, line) },
	}
	must(t, os.WriteFile(filepath.Join(box.Inbox(), "1-a.json"),
		[]byte(`{"ecosystem":"nuget","id":"Demo.Pkg","version":"1.0.0","framework":"net10.0"}`), 0o644))

	desk.Poll(context.Background())
	if _, err := os.Stat(filepath.Join(h.cache, "demo.pkg")); err == nil {
		t.Fatal("fetched before the user approved")
	}
	pending := desk.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending = %+v", pending)
	}
	desk.Decide([]string{pending[0].Key}, nil)
	desk.Poll(context.Background())
	desk.Wait()

	if _, err := os.Stat(filepath.Join(h.cache, "demo.pkg", "1.0.0")); err != nil {
		t.Errorf("not in the host cache: %v", err)
	}
	answer, err := os.ReadFile(filepath.Join(box.View(), "1-a.answer"))
	if err != nil || !strings.HasPrefix(string(answer), "fetched\n") || !strings.Contains(string(answer), "demo.pkg 1.0.0") {
		t.Errorf("answer = %q, %v", answer, err)
	}
}

func TestAnUnknownPackageIsAnsweredFailedWithNuGetsWords(t *testing.T) {
	h := newHost(t)
	box := packagereq.Mailbox{Dir: t.TempDir()}
	must(t, os.MkdirAll(box.Inbox(), 0o755))
	desk := &packagereq.Desk{
		Mailbox:    box,
		Ecosystems: map[string]packagereq.Ecosystem{"nuget": Ecosystem{Options: func(packagereq.Request) Options { return h.options() }}},
		Policy:     func() string { return packagereq.PolicyAllow },
	}
	must(t, os.WriteFile(filepath.Join(box.Inbox(), "1-a.json"),
		[]byte(`{"ecosystem":"nuget","id":"Nope.Pkg","version":"1.0.0","framework":"net10.0"}`), 0o644))
	desk.Poll(context.Background())
	desk.Wait()
	answer, _ := os.ReadFile(filepath.Join(box.View(), "1-a.answer"))
	if !strings.HasPrefix(string(answer), "failed\n") || !strings.Contains(string(answer), "NU1101") {
		t.Errorf("answer = %q", answer)
	}
}

func TestOnlyNuGetRequestsAreValid(t *testing.T) {
	e := Ecosystem{}
	if err := e.Validate(packagereq.Request{Ecosystem: "npm", ID: "a", Version: "1.0.0", Framework: "net8.0"}); err == nil {
		t.Error("an npm request was valid for NuGet")
	}
	if err := e.Validate(packagereq.Request{Ecosystem: "nuget", ID: "a", Version: "1.*", Framework: "net8.0"}); err == nil {
		t.Error("a floating version was valid")
	}
}

func TestAFailureCarriesTheHintForWhatTheUserCouldDo(t *testing.T) {
	h := newHost(t)
	e := Ecosystem{Options: func(packagereq.Request) Options { return h.options() }, Hint: func() string { return "approve the nuget.config" }}
	out := e.Fetch(context.Background(), []packagereq.Request{{Ecosystem: "nuget", ID: "Nope.Pkg", Version: "1.0.0", Framework: "net10.0"}})
	if len(out) != 1 || out[0].Err == nil || !strings.Contains(out[0].Message, "NU1101") || !strings.HasSuffix(out[0].Message, "approve the nuget.config") {
		t.Errorf("out = %+v", out)
	}
}

// A fresh machine has no ~/.vibe/nuget-fetch yet: the fetch makes it.
func TestFetchMakesItsRootWhenItIsNotThereYet(t *testing.T) {
	h := newHost(t)
	opts := h.options()
	opts.Root = filepath.Join(t.TempDir(), "not", "yet", "nuget-fetch")
	res, err := Fetch(context.Background(), Request{ID: "Demo.Pkg", Version: "1.0.0", Framework: "net10.0"}, opts)
	if err != nil {
		t.Fatalf("Fetch: %v\n%s", err, res.Output)
	}
	if _, err := os.Stat(filepath.Join(h.cache, "demo.pkg", "1.0.0")); err != nil {
		t.Errorf("package not in the cache: %v", err)
	}
	if left, _ := os.ReadDir(opts.Root); len(left) != 0 {
		t.Errorf("throwaway folder left behind: %v", left)
	}
}

// Fetches run together (the desk runs several at once), into one cache: NuGet
// locks per package, so each lands whole, even the same package asked twice.
func TestFetchesRunTogetherIntoOneCacheAllLandWhole(t *testing.T) {
	h := newHost(t)
	var wg sync.WaitGroup
	errs := make(chan string, 6)
	for _, id := range []string{"Demo.Pkg", "Demo.Two", "Demo.Three", "Demo.Pkg", "Demo.Two", "Demo.Three"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := Fetch(context.Background(), Request{ID: id, Version: "1.0.0", Framework: "net10.0"}, h.options())
			if err != nil {
				errs <- id + ": " + err.Error() + "\n" + res.Output
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	for _, id := range []string{"demo.pkg", "demo.two", "demo.three"} {
		for _, file := range []string{id + ".1.0.0.nupkg", id + ".nuspec"} {
			if _, err := os.Stat(filepath.Join(h.cache, id, "1.0.0", file)); err != nil {
				t.Errorf("%s: %v", id, err)
			}
		}
	}
	if left, _ := os.ReadDir(h.root); len(left) != 0 {
		t.Errorf("throwaway folders left behind: %v", left)
	}
}

func nuget(id, version, framework string) packagereq.Request {
	return packagereq.Request{Ecosystem: "nuget", ID: id, Version: version, Framework: framework}
}

// countingRestores counts the restores FetchBatch runs, by the framework of the project.
func countingRestores(t *testing.T) map[string]int {
	t.Helper()
	by := map[string]int{}
	was := restoreBatch
	restoreBatch = func(ctx context.Context, xml string, opts Options) (string, error) {
		for _, fw := range []string{"net8.0", "net10.0"} {
			if strings.Contains(xml, "<TargetFramework>"+fw+"</TargetFramework>") {
				by[fw]++
			}
		}
		return was(ctx, xml, opts)
	}
	t.Cleanup(func() { restoreBatch = was })
	return by
}

// What is approved together is fetched in one restore, and each request is answered for itself.
func TestRequestsForOneFrameworkAreFetchedInOneRestoreAndEachAnswered(t *testing.T) {
	h := newHost(t)
	restores := countingRestores(t)
	e := Ecosystem{Options: func(packagereq.Request) Options { return h.options() }}
	out := e.Fetch(context.Background(), []packagereq.Request{nuget("Demo.Pkg", "1.0.0", "net10.0"), nuget("Demo.Two", "1.0.0", "net10.0"), nuget("Demo.Three", "1.0.0", "net10.0")})
	if len(out) != 3 {
		t.Fatalf("%d outcomes for 3 requests", len(out))
	}
	for i, want := range []string{"demo.pkg 1.0.0", "demo.two 1.0.0", "demo.three 1.0.0"} {
		if out[i].Err != nil || !strings.Contains(out[i].Message, "fetched "+want) {
			t.Errorf("outcome %d = %+v, want fetched %s", i, out[i], want)
		}
	}
	if restores["net10.0"] != 1 {
		t.Errorf("%d restores, want 1", restores["net10.0"])
	}
}

// A package no feed has fails its own request only, in NuGet's words, with the hint.
func TestAPackageNoFeedHasFailsItsOwnRequestOnly(t *testing.T) {
	h := newHost(t)
	e := Ecosystem{Options: func(packagereq.Request) Options { return h.options() }, Hint: func() string { return "approve the nuget.config" }}
	out := e.Fetch(context.Background(), []packagereq.Request{nuget("Demo.Pkg", "1.0.0", "net10.0"), nuget("Nope.Pkg", "1.0.0", "net10.0"), nuget("Demo.Two", "1.0.0", "net10.0")})
	if out[0].Err != nil || out[2].Err != nil {
		t.Errorf("the others should come: %+v / %+v", out[0], out[2])
	}
	if out[1].Err == nil || !strings.Contains(out[1].Message, "NU1101") || !strings.Contains(out[1].Message, "Nope.Pkg") || !strings.HasSuffix(out[1].Message, "approve the nuget.config") {
		t.Errorf("Nope.Pkg: %+v", out[1])
	}
}

// What NuGet fails on without naming it is found and fails only its own request.
func TestACorruptPackageFailsItsOwnRequestOnly(t *testing.T) {
	h := newHost(t)
	must(t, os.WriteFile(filepath.Join(h.feed, "bad.pkg.1.0.0.nupkg"), []byte("this is not a zip"), 0o644))
	e := Ecosystem{Options: func(packagereq.Request) Options { return h.options() }}
	out := e.Fetch(context.Background(), []packagereq.Request{nuget("Demo.Pkg", "1.0.0", "net10.0"), nuget("Bad.Pkg", "1.0.0", "net10.0")})
	if out[0].Err != nil || !strings.Contains(out[0].Message, "demo.pkg 1.0.0") {
		t.Errorf("Demo.Pkg: %+v", out[0])
	}
	if out[1].Err == nil || !strings.Contains(out[1].Message, "corrupt") {
		t.Errorf("Bad.Pkg: %+v", out[1])
	}
}

func TestARequestForWhatTheCacheHasAlreadyIsSaidSo(t *testing.T) {
	h := newHost(t)
	put(t, h.cache, Package{"Demo.Pkg", "1.0.0"})
	restoring(t, func(xml string) (string, error) { return "", nil })
	e := Ecosystem{Options: func(packagereq.Request) Options { return h.options() }}
	out := e.Fetch(context.Background(), []packagereq.Request{nuget("Demo.Pkg", "1.0.0", "net10.0")})
	if out[0].Err != nil || !strings.Contains(out[0].Message, "already") {
		t.Errorf("outcome = %+v", out[0])
	}
}

// Frameworks are restored separately, and each outcome is where its request was.
func TestRequestsForSeveralFrameworksAreRestoredByFrameworkAndAnsweredInOrder(t *testing.T) {
	cache := t.TempDir()
	by := map[string]int{}
	restoring(t, func(xml string) (string, error) {
		for _, fw := range []string{"net8.0", "net10.0"} {
			if strings.Contains(xml, "<TargetFramework>"+fw+"</TargetFramework>") {
				by[fw]++
			}
		}
		for _, id := range []string{"A.Lib", "B.Lib", "C.Lib"} {
			if strings.Contains(xml, `Include="`+id+`"`) {
				put(t, cache, Package{id, "1.0.0"})
			}
		}
		return "", nil
	})
	e := Ecosystem{Options: func(packagereq.Request) Options { return Options{Cache: cache, Timeout: time.Minute} }}
	out := e.Fetch(context.Background(), []packagereq.Request{nuget("A.Lib", "1.0.0", "net8.0"), nuget("B.Lib", "1.0.0", "net10.0"), nuget("C.Lib", "1.0.0", "net8.0")})
	if by["net8.0"] != 1 || by["net10.0"] != 1 {
		t.Errorf("restores by framework %v, want one each", by)
	}
	for i, want := range []string{"a.lib", "b.lib", "c.lib"} {
		if out[i].Err != nil || !strings.Contains(out[i].Message, want) {
			t.Errorf("outcome %d = %+v, want %s", i, out[i], want)
		}
	}
}

// Nothing is claimed that isn't so: a restore that said nothing of a package and left it
// out of the cache is a failure of that request.
func TestARequestWhosePackageIsNotInTheCacheAfterAllIsFailed(t *testing.T) {
	restoring(t, func(xml string) (string, error) { return "", nil })
	e := Ecosystem{Options: func(packagereq.Request) Options { return Options{Cache: t.TempDir(), Timeout: time.Minute} }}
	out := e.Fetch(context.Background(), []packagereq.Request{nuget("Ghost.Pkg", "1.0.0", "net8.0")})
	if out[0].Err == nil || !strings.Contains(out[0].Message, "isn't in the host's package cache") {
		t.Errorf("outcome = %+v", out[0])
	}
}

func TestAFetchThatCouldNotRunFailsEveryRequestInItsFramework(t *testing.T) {
	restoring(t, func(string) (string, error) { return "", timedOut{time.Minute} })
	e := Ecosystem{Options: func(packagereq.Request) Options { return Options{Cache: t.TempDir(), Timeout: time.Minute} }}
	out := e.Fetch(context.Background(), []packagereq.Request{nuget("A.Lib", "1.0.0", "net8.0"), nuget("B.Lib", "1.0.0", "net8.0")})
	for i, o := range out {
		if o.Err == nil || !strings.Contains(o.Message, "time limit") {
			t.Errorf("outcome %d = %+v", i, o)
		}
	}
}
