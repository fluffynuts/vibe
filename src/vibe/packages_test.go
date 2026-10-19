package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vibe/internal/layout"
	"vibe/internal/nugetfetch"
	"vibe/internal/packagereq"
	"vibe/internal/settings"
	"vibe/internal/state"
)

func TestLoadSettingsRefusesAnUnknownPackagesPolicy(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), "packages: sometimes\n")
	_, err := loadSettings(lay, "p")
	if err == nil || !strings.Contains(err.Error(), "sometimes") || !strings.Contains(err.Error(), "ask") || !strings.Contains(err.Error(), "allow") {
		t.Errorf("want an error naming the value and the choices, got %v", err)
	}
}

func TestPackagesAreAskedBeforeFetchingUnlessTheUserSaysAllow(t *testing.T) {
	for policy, want := range map[string]string{"": packagereq.PolicyAsk, "ask": packagereq.PolicyAsk, "allow": packagereq.PolicyAllow} {
		if got := packagesPolicy(settings.Settings{Packages: policy}); got != want {
			t.Errorf("packages %q: policy %q, want %q", policy, got, want)
		}
	}
}

// NuGet on the host reads its config from the user's folders, and a
// nuget.config from every folder above the one it restores in: if the
// agent can write any of those, it can redirect the host's fetch.
func TestNoFetchesWhenTheAgentsFolderHoldsWhatNuGetReadsOnTheHost(t *testing.T) {
	home := t.TempDir()
	vibeHome := filepath.Join(home, ".vibe")
	nuget := filepath.Join(home, ".nuget")
	merged := settings.Settings{NugetDir: nuget}
	for name, target := range map[string]string{
		"the user's home":  home,
		"vibe's own":       vibeHome,
		"nuget's":          nuget,
		"above vibe's own": filepath.Dir(home),
	} {
		if why := packagesUnavailable(target, vibeHome, merged); why == "" || !strings.Contains(why, "start vibe in a project folder") {
			t.Errorf("a sandbox in %s: %q, want it refused, saying what to do", name, why)
		}
	}
	project := filepath.Join(home, "code", "project")
	if why := packagesUnavailable(project, vibeHome, merged); why != "" {
		t.Errorf("an ordinary project folder: %q", why)
	}
}

func TestAFolderTheAgentCanWriteIsComparedWithItsLinksFollowed(t *testing.T) {
	home := t.TempDir()
	vibeHome := filepath.Join(home, ".vibe")
	if err := os.MkdirAll(vibeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if why := packagesUnavailable(alias, vibeHome, settings.Settings{}); why == "" {
		t.Error("a link to the folder holding vibe's own passed as a project")
	}
}

// What a request is to the project is worked out from the project's own
// files, on the repository's default branch (set here, as with no origin).
func TestARequestIsDescribedFromTheProjectOnTheDefaultBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	project := func(version string) string {
		return `<Project><ItemGroup><PackageReference Include="Corp.Lib" Version="` + version + `" /></ItemGroup></Project>`
	}
	run("init", "-q", "-b", "main")
	run("config", "vibe.defaultBranch", "main")
	writeFile(t, filepath.Join(dir, "App.csproj"), project("1.0.0"))
	run("add", "-A")
	run("commit", "-q", "-m", "start")
	run("checkout", "-q", "-b", "feature")
	writeFile(t, filepath.Join(dir, "App.csproj"), project("2.0.0"))

	got := describeRequest(dir, packagereq.Request{Ecosystem: "nuget", ID: "Corp.Lib", Version: "2.0.0", Framework: "net8.0"})
	if !strings.HasPrefix(got, "upgrade") || !strings.Contains(got, "1.0.0") {
		t.Errorf("got %q", got)
	}
	got = describeRequest(dir, packagereq.Request{Ecosystem: "nuget", ID: "Other.Lib", Version: "1.0.0", Framework: "net8.0"})
	if !strings.HasPrefix(got, "new") {
		t.Errorf("got %q", got)
	}
}

// What the sandbox restores into its own cache is fetched into the host's, for
// the user's IDE: from the sandbox's folder, into the packages folder the
// sandbox is given read-only.
func TestTheSyncReadsTheSandboxsFolderAndFillsTheHostsPackages(t *testing.T) {
	nuget := hostNuget(t)
	lay := updateLayout(t)
	target := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	merged := settings.Settings{NugetDir: nuget}
	sync := packageSyncFor(lay, "box", state.Instance{Name: "box", Profile: "p", Target: target}, merged)
	if sync == nil {
		t.Fatal("no sync for a sandbox that's given the host's packages")
	}
	if sync.Workspace != target || sync.Cache != filepath.Join(nuget, "packages") {
		t.Errorf("workspace %q, cache %q", sync.Workspace, sync.Cache)
	}
	opts := sync.Options()
	if opts.Root != state.NugetFetchPath(lay.Home) || opts.Cache != sync.Cache || opts.Timeout < 10*time.Minute {
		t.Errorf("options = %+v: fetches go in vibe's own folder, to the same cache, with time for a whole solution", opts)
	}
}

func TestThereIsNoSyncWhereTheSandboxIsntGivenTheHostsPackages(t *testing.T) {
	lay := updateLayout(t)
	inst := state.Instance{Name: "box", Profile: "p", Target: t.TempDir()}
	for name, merged := range map[string]settings.Settings{
		"no nuget":      {},
		"shared":        {NugetDir: hostNuget(t), Nuget: "shared"},
		"no host cache": {NugetDir: filepath.Join(t.TempDir(), "nope")},
	} {
		if packageSyncFor(lay, "box", inst, merged) != nil {
			t.Errorf("%s: a sync for it", name)
		}
	}
}

// If the agent could change what a fetch does, nothing is fetched for it.
func TestThereIsNoSyncWhenTheSandboxsFolderHoldsWhatNuGetReadsOnTheHost(t *testing.T) {
	home := t.TempDir()
	lay := layout.New(filepath.Join(home, ".vibe"), t.TempDir())
	nuget := filepath.Join(home, ".nuget")
	os.MkdirAll(filepath.Join(nuget, "packages"), 0o755)
	if packageSyncFor(lay, "box", state.Instance{Name: "box", Target: home}, settings.Settings{NugetDir: nuget}) != nil {
		t.Error("a sync for a sandbox in the user's home folder")
	}
}

func TestSyncingRunsWithoutAskingOnlyWhenPackagesAreAllowed(t *testing.T) {
	lay := updateLayout(t)
	for setting, want := range map[string]bool{"": false, "packages: ask\n": false, "packages: allow\n": true} {
		writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), setting)
		if got := packageSyncAuto(lay, "p")(); got != want {
			t.Errorf("settings %q: auto = %v, want %v", setting, got, want)
		}
	}
}

// What the agent is told when it asks what's waiting for the user, before it builds: said so it
// can pass it on, and wait, instead of downloading its own copies of what the user's cache lacks.
func TestWhatIsWaitingIsSaidToTheAgentInTheTermsItCanActOn(t *testing.T) {
	t.Run("nothing", func(t *testing.T) {
		got := overviewText(nil, 0, false)
		if !strings.Contains(got, "Nothing is waiting") || strings.Contains(got, "Tell the user") {
			t.Errorf("%q", got)
		}
	})
	t.Run("requests to approve", func(t *testing.T) {
		got := overviewText([]string{"Corp.Lib 1.2.3", "Corp.Other 2.0.0"}, 0, false)
		for _, want := range []string{"2 package requests", "Corp.Lib 1.2.3", "Corp.Other 2.0.0", "Admin tab", "Tell the user", "wait"} {
			if !strings.Contains(got, want) {
				t.Errorf("lacks %q: %q", want, got)
			}
		}
	})
	t.Run("one request is a request", func(t *testing.T) {
		if got := overviewText([]string{"Corp.Lib 1.2.3"}, 0, false); !strings.Contains(got, "1 package request ") {
			t.Errorf("%q", got)
		}
	})
	t.Run("packages the user's IDE can't see", func(t *testing.T) {
		got := overviewText(nil, 405, false)
		for _, want := range []string{"405 packages", "IDE can't see", "download your own copies", "Fetch them", "Tell the user", "vibe-nuget status again"} {
			if !strings.Contains(got, want) {
				t.Errorf("lacks %q: %q", want, got)
			}
		}
	})
	t.Run("a fetch under way", func(t *testing.T) {
		got := overviewText(nil, 3, true)
		if !strings.Contains(got, "being fetched") || !strings.Contains(got, "vibe-nuget status again") || strings.Contains(got, "Fetch them") {
			t.Errorf("%q", got)
		}
	})
	t.Run("both are said", func(t *testing.T) {
		got := overviewText([]string{"Corp.Lib 1.2.3"}, 2, false)
		if !strings.Contains(got, "Corp.Lib") || !strings.Contains(got, "2 packages") {
			t.Errorf("%q", got)
		}
	})
	t.Run("a long list of requests is cut short, and says so", func(t *testing.T) {
		var many []string
		for i := 0; i < 12; i++ {
			many = append(many, fmt.Sprintf("Corp.Lib%02d 1.0.0", i))
		}
		got := overviewText(many, 0, false)
		if !strings.Contains(got, "12 package requests") || !strings.Contains(got, "Corp.Lib00") || strings.Contains(got, "Corp.Lib11") || !strings.Contains(got, "and 7 more") {
			t.Errorf("%q", got)
		}
	})
}

type quietEcosystem struct{}

func (quietEcosystem) Validate(packagereq.Request) error { return nil }
func (quietEcosystem) Fetch(_ context.Context, rs []packagereq.Request) []packagereq.Outcome {
	return make([]packagereq.Outcome, len(rs))
}

// The overview is of now: what the desk holds undecided, and what the workspace says the cache lacks
// as the agent asks, not as of the last scan.
func TestTheOverviewIsOfWhatIsWaitingNow(t *testing.T) {
	box := packagereq.Mailbox{Dir: t.TempDir()}
	if err := os.MkdirAll(box.Inbox(), 0o755); err != nil {
		t.Fatal(err)
	}
	desk := &packagereq.Desk{Mailbox: box, Ecosystems: map[string]packagereq.Ecosystem{"nuget": quietEcosystem{}}, Policy: func() string { return packagereq.PolicyAsk }}
	if err := os.WriteFile(filepath.Join(box.Inbox(), "1-a.json"), []byte(`{"ecosystem":"nuget","id":"Corp.Lib","version":"1.2.3","framework":"net8.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	desk.Poll(context.Background())

	ws, cache := t.TempDir(), t.TempDir()
	sync := &nugetfetch.Sync{Workspace: ws, Cache: cache, Options: func() nugetfetch.Options { return nugetfetch.Options{Cache: cache} }}
	overview := packagesOverview(desk, sync)

	// Written after the sync was made and never checked: asking looks.
	obj := filepath.Join(ws, "App", "obj")
	if err := os.MkdirAll(obj, 0o755); err != nil {
		t.Fatal(err)
	}
	assets := `{"version":3,"targets":{"net8.0":{"Dapper/2.1.86":{"type":"package"}}},"libraries":{"Dapper/2.1.86":{"type":"package","path":"dapper/2.1.86"}}}`
	if err := os.WriteFile(filepath.Join(obj, "project.assets.json"), []byte(assets), 0o644); err != nil {
		t.Fatal(err)
	}
	got := overview()
	if !strings.Contains(got, "Corp.Lib 1.2.3") || !strings.Contains(got, "1 package") || !strings.Contains(got, "IDE can't see") {
		t.Errorf("overview = %q", got)
	}

	// Approved, it's no longer waiting for the user; and once the cache has the package, nothing is.
	desk.Decide([]string{desk.Pending()[0].Key}, nil)
	dapper := filepath.Join(cache, "dapper", "2.1.86")
	if err := os.MkdirAll(dapper, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dapper, ".nupkg.metadata"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := overview(); !strings.Contains(got, "Nothing is waiting") {
		t.Errorf("overview = %q, want nothing waiting", got)
	}
}

func TestTheOverviewWithNoSyncSaysOnlyWhatTheDeskHolds(t *testing.T) {
	box := packagereq.Mailbox{Dir: t.TempDir()}
	desk := &packagereq.Desk{Mailbox: box, Ecosystems: map[string]packagereq.Ecosystem{"nuget": quietEcosystem{}}, Policy: func() string { return packagereq.PolicyAsk }}
	if got := packagesOverview(desk, nil)(); !strings.Contains(got, "Nothing is waiting") {
		t.Errorf("overview = %q", got)
	}
}
