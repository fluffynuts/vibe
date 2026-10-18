package nugetfetch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

func packDemo(t *testing.T, h host) {
	t.Helper()
	src := t.TempDir()
	must(t, os.WriteFile(filepath.Join(src, "Demo.Pkg.csproj"), []byte(
		`<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net10.0</TargetFramework><PackageId>Demo.Pkg</PackageId><Version>1.0.0</Version></PropertyGroup></Project>`), 0o644))
	must(t, os.WriteFile(filepath.Join(src, "C.cs"), []byte("class C {}"), 0o644))
	cmd := exec.Command("dotnet", "pack", "-o", h.feed)
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "HOME="+h.home, "NUGET_PACKAGES="+filepath.Join(t.TempDir(), "p"),
		"DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("packing the demo package: %v\n%s", err, out)
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
