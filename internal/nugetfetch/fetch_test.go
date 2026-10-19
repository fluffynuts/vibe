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
	desk.Allow([]string{pending[0].Key})
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
	msg, err := e.Fetch(context.Background(), packagereq.Request{Ecosystem: "nuget", ID: "Nope.Pkg", Version: "1.0.0", Framework: "net10.0"})
	if err == nil || !strings.Contains(msg, "NU1101") || !strings.HasSuffix(msg, "approve the nuget.config") {
		t.Errorf("msg = %q, err = %v", msg, err)
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
