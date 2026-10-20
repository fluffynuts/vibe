package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"vibe/internal/packagereq"
)

type askResult struct {
	out  string
	code int
}

// runVibeNuget runs the sandbox's vibe-nuget with args, in a mailbox the
// test plays the host of: respond is given each request taken and answers
// for it.
func runVibeNuget(t *testing.T, mailbox packagereq.Mailbox, respond func(packagereq.Taken), args ...string) askResult {
	t.Helper()
	return runVibeNugetIn(t, "", mailbox, respond, args...)
}

// runVibeNugetIn is runVibeNuget run in the folder dir ("" for where the
// test is).
func runVibeNugetIn(t *testing.T, dir string, mailbox packagereq.Mailbox, respond func(packagereq.Taken), args ...string) askResult {
	t.Helper()
	return runVibeNugetEnv(t, dir, nil, mailbox, respond, args...)
}

// runVibeNugetEnv is runVibeNugetIn with env set in the script's environment.
func runVibeNugetEnv(t *testing.T, dir string, env []string, mailbox packagereq.Mailbox, respond func(packagereq.Taken), args ...string) askResult {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "library", "dotnet", "agent-files", ".local", "bin", "vibe-nuget"))
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, taken := range mailbox.Take() {
				respond(taken)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "VIBE_PACKAGES_DIR=" + mailbox.Dir, "HOME=" + os.TempDir()}, env...)
	out, err := cmd.CombinedOutput()
	close(stop)
	<-done
	res := askResult{out: string(out)}
	if exit, ok := err.(*exec.ExitError); ok {
		res.code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return res
}

func sandboxMailbox(t *testing.T) packagereq.Mailbox {
	t.Helper()
	m := packagereq.Mailbox{Dir: t.TempDir()}
	for _, d := range []string{m.Inbox(), m.View()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func TestVibeNugetSaysWhatTheHostFetched(t *testing.T) {
	m := sandboxMailbox(t)
	var asked packagereq.Request
	res := runVibeNuget(t, m, func(tk packagereq.Taken) {
		asked = tk.Request
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Fetched, Message: "fetched Corp.Lib 1.2.3"})
	}, "fetch", "Corp.Lib", "1.2.3", "--framework", "net8.0")
	if res.code != 0 || !strings.Contains(res.out, "fetched Corp.Lib 1.2.3") {
		t.Errorf("exit %d:\n%s", res.code, res.out)
	}
	want := packagereq.Request{Ecosystem: "nuget", ID: "Corp.Lib", Version: "1.2.3", Framework: "net8.0"}
	if asked != want {
		t.Errorf("the host was asked %+v, want %+v", asked, want)
	}
}

func TestVibeNugetFailsWithTheHostsReasonWhenItIsNotFetched(t *testing.T) {
	for _, status := range []packagereq.Status{packagereq.Failed, packagereq.Denied} {
		m := sandboxMailbox(t)
		res := runVibeNuget(t, m, func(tk packagereq.Taken) {
			m.Reply(tk.Nonce, packagereq.Answer{Status: status, Message: "because reasons"})
		}, "fetch", "Corp.Lib", "1.2.3", "--framework", "net8.0")
		if res.code != 1 || !strings.Contains(res.out, "because reasons") {
			t.Errorf("%s: exit %d:\n%s", status, res.code, res.out)
		}
	}
}

func TestVibeNugetSaysItIsWaitingForTheUserAndKeepsWaiting(t *testing.T) {
	m := sandboxMailbox(t)
	res := runVibeNuget(t, m, func(tk packagereq.Taken) {
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Waiting, Message: "waiting for you to approve it on the Admin tab"})
		time.Sleep(1500 * time.Millisecond)
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Fetched, Message: "fetched"})
	}, "fetch", "Corp.Lib", "1.2.3", "--framework", "net8.0")
	if res.code != 0 || !strings.Contains(res.out, "waiting for you to approve it on the Admin tab") || !strings.HasSuffix(strings.TrimSpace(res.out), "fetched") {
		t.Errorf("exit %d:\n%s", res.code, res.out)
	}
}

func TestVibeNugetRefusesWhatIsNotAnIdVersionOrFrameworkAndAsksNothing(t *testing.T) {
	cases := [][]string{
		{"fetch", `A"/><x`, "1.0.0", "--framework", "net8.0"},
		{"fetch", "../A", "1.0.0", "--framework", "net8.0"},
		{"fetch", "A", "[1.0,2.0)", "--framework", "net8.0"},
		{"fetch", "A", "1.0.0", "--framework", "net8.0;evil"},
		{"fetch", "A"},
		{"restore", "A", "1.0.0"},
	}
	for _, args := range cases {
		m := sandboxMailbox(t)
		asked := false
		res := runVibeNuget(t, m, func(packagereq.Taken) { asked = true }, args...)
		if res.code != 2 || asked {
			t.Errorf("%v: exit %d, asked %v:\n%s", args, res.code, asked, res.out)
		}
	}
}

func TestVibeNugetSaysWhatToDoWithNoPlaceToAsk(t *testing.T) {
	script, _ := filepath.Abs(filepath.Join("..", "..", "library", "dotnet", "agent-files", ".local", "bin", "vibe-nuget"))
	cmd := exec.Command("sh", script, "fetch", "A", "1.0.0", "--framework", "net8.0")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "vibe -r") {
		t.Errorf("want it to say to rebuild: %v\n%s", err, out)
	}
}

func project(t *testing.T, dir, name, frameworks string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, name), `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup>`+frameworks+`</PropertyGroup></Project>`)
}

func askedFramework(t *testing.T, dir string) (asked packagereq.Request, res askResult) {
	t.Helper()
	m := sandboxMailbox(t)
	res = runVibeNugetIn(t, dir, m, func(tk packagereq.Taken) {
		asked = tk.Request
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Fetched, Message: "fetched"})
	}, "fetch", "Corp.Lib", "1.2.3")
	return asked, res
}

// In a folder with a solution (or several projects) msbuild can't say which
// project's framework is meant. When every project's files agree, that's it.
func TestVibeNugetTakesTheFrameworkAllTheProjectsAgreeOn(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, "src/A/A.csproj", "<TargetFramework>net8.0</TargetFramework>")
	project(t, dir, "src/B/B.csproj", "<TargetFramework>net8.0</TargetFramework>")
	project(t, dir, "tests/T/T.csproj", "<TargetFrameworks>net8.0</TargetFrameworks>")
	project(t, dir, "src/A/obj/Stale.csproj", "<TargetFramework>net6.0</TargetFramework>")
	asked, res := askedFramework(t, dir)
	if res.code != 0 || asked.Framework != "net8.0" {
		t.Errorf("exit %d, asked %+v:\n%s", res.code, asked, res.out)
	}
}

func TestVibeNugetSaysWhichFrameworksTheProjectsUseWhenTheyDisagree(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, "A/A.csproj", "<TargetFramework>net8.0</TargetFramework>")
	project(t, dir, "B/B.csproj", "<TargetFrameworks>net8.0;netstandard2.0</TargetFrameworks>")
	asked, res := askedFramework(t, dir)
	if res.code != 2 || asked.ID != "" {
		t.Errorf("exit %d, asked %+v", res.code, asked)
	}
	for _, want := range []string{"net8.0", "netstandard2.0", "--framework"} {
		if !strings.Contains(res.out, want) {
			t.Errorf("the message should say %q:\n%s", want, res.out)
		}
	}
}

func TestVibeNugetWontGuessAFrameworkFromAProperty(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, "A/A.csproj", "<TargetFramework>net8.0</TargetFramework>")
	project(t, dir, "B/B.csproj", "<TargetFramework>$(Tfm)</TargetFramework>")
	asked, res := askedFramework(t, dir)
	if res.code != 2 || asked.ID != "" || !strings.Contains(res.out, "--framework") {
		t.Errorf("exit %d, asked %+v:\n%s", res.code, asked, res.out)
	}
}

func TestVibeNugetWithNoProjectsHereSaysToGiveTheFramework(t *testing.T) {
	asked, res := askedFramework(t, t.TempDir())
	if res.code != 2 || asked.ID != "" || !strings.Contains(res.out, "--framework") {
		t.Errorf("exit %d, asked %+v:\n%s", res.code, asked, res.out)
	}
}

// The sandbox's overlay of the host's cache remembers a package it looked for
// and didn't find, and keeps saying so after the host has fetched it; the
// kernel forgets when it drops its dentry caches.
func TestVibeNugetMakesAnOverlaidCacheSeeWhatTheHostFetched(t *testing.T) {
	fetched := func(m packagereq.Mailbox) func(packagereq.Taken) {
		return func(tk packagereq.Taken) {
			m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Fetched, Message: "fetched Corp.Lib 1.2.3"})
		}
	}
	refused := func(m packagereq.Mailbox) func(packagereq.Taken) {
		return func(tk packagereq.Taken) {
			m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Denied, Message: "the user said no"})
		}
	}
	cases := []struct {
		name    string
		overlay bool
		answer  func(packagereq.Mailbox) func(packagereq.Taken)
		dropped bool
	}{
		{"fetched, overlaid", true, fetched, true},
		{"fetched, not overlaid", false, fetched, false},
		{"refused, overlaid", true, refused, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := sandboxMailbox(t)
			drop := filepath.Join(t.TempDir(), "drop_caches")
			env := []string{"PATH=" + newFakeMounting(t, true).bin + string(os.PathListSeparator) + os.Getenv("PATH"), "VIBE_DROP_CACHES=" + drop}
			if c.overlay {
				env = append(env, "VIBE_NUGET_OVERLAY=1")
			}
			runVibeNugetEnv(t, "", env, m, c.answer(m), "fetch", "Corp.Lib", "1.2.3", "--framework", "net8.0")
			data, err := os.ReadFile(drop)
			if c.dropped {
				if err != nil || strings.TrimSpace(string(data)) != "2" {
					t.Errorf("want 2 written to the drop_caches file, got %q (%v)", data, err)
				}
			} else if err == nil {
				t.Errorf("dropped the caches: %q", data)
			}
		})
	}
}

// Before it builds, the agent can ask what the user has waiting: vibe-nuget status prints what the
// host says, at once, so it can tell the user before downloading its own copies of packages.
func TestVibeNugetStatusSaysWhatTheHostSaysIsWaiting(t *testing.T) {
	m := sandboxMailbox(t)
	var asked packagereq.Request
	res := runVibeNuget(t, m, func(tk packagereq.Taken) {
		asked = tk.Request
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Info, Message: "2 package requests are waiting for the user"})
	}, "status")
	if res.code != 0 || !strings.Contains(res.out, "2 package requests are waiting for the user") {
		t.Errorf("exit %d:\n%s", res.code, res.out)
	}
	if asked.Kind != packagereq.KindStatus || asked.ID != "" || asked.Ecosystem != "" {
		t.Errorf("the host was asked %+v, want only the status", asked)
	}
}

func TestVibeNugetStatusFailsWhenTheHostCannotSay(t *testing.T) {
	m := sandboxMailbox(t)
	res := runVibeNuget(t, m, func(tk packagereq.Taken) {
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Failed, Message: "the host has nothing to say"})
	}, "status")
	if res.code != 1 || !strings.Contains(res.out, "the host has nothing to say") {
		t.Errorf("exit %d:\n%s", res.code, res.out)
	}
}

func TestVibeNugetStatusTakesNothingElseAndAsksNothing(t *testing.T) {
	m := sandboxMailbox(t)
	res := runVibeNuget(t, m, func(packagereq.Taken) { t.Error("asked the host") }, "status", "Corp.Lib")
	if res.code != 2 || !strings.Contains(res.out, "usage") {
		t.Errorf("exit %d:\n%s", res.code, res.out)
	}
}

// --- tidy: pointing the generated files at the user's cache, once it has what the sandbox downloaded -----------

// tidyRig is a workspace whose project.nuget.cache names packages in the sandbox's own cache, a host cache,
// and what's in each. The script runs in the sandbox, a Linux guest, never on a Windows host.
type tidyRig struct {
	ws, own, host string
}

func newTidyRig(t *testing.T) tidyRig {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("vibe-nuget tidy runs in the Linux sandbox")
	}
	r := tidyRig{ws: t.TempDir(), own: filepath.Join(t.TempDir(), "own"), host: filepath.Join(t.TempDir(), "host")}
	for _, d := range []string{r.own, r.host} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r tidyRig) put(t *testing.T, cache, id, version string) {
	t.Helper()
	dir := filepath.Join(cache, id, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".nupkg.metadata", id + "." + version + ".nupkg.sha512"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (r tidyRig) has(cache, id, version string) bool {
	_, err := os.Stat(filepath.Join(cache, id, version))
	return err == nil
}

// project writes a project's obj files: its project.nuget.cache naming files at the given paths, as given.
func (r tidyRig) project(t *testing.T, name string, expected ...string) {
	t.Helper()
	obj := filepath.Join(r.ws, name, "obj")
	if err := os.MkdirAll(filepath.Join(obj, "Debug", "net8.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	var quoted []string
	for _, e := range expected {
		quoted = append(quoted, `"`+e+`"`)
	}
	projectFile := filepath.Join(r.ws, name, name+".csproj")
	if err := os.WriteFile(projectFile, []byte("<Project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"version":2,"projectFilePath":"` + projectFile + `","expectedPackageFiles":[` + strings.Join(quoted, ",") + `],"logs":[]}`
	for file, content := range map[string]string{
		"project.nuget.cache": body, "project.assets.json": "{}", filepath.Join("Debug", "net8.0", name+".assets.cache"): "stale",
	} {
		if err := os.WriteFile(filepath.Join(obj, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (r tidyRig) sha(cache, id, version string) string {
	return filepath.Join(cache, id, version, id+"."+version+".nupkg.sha512")
}

func (r tidyRig) assetsCache(name string) string {
	return filepath.Join(r.ws, name, "obj", "Debug", "net8.0", name+".assets.cache")
}

func (r tidyRig) run(t *testing.T, args ...string) askResult {
	t.Helper()
	return runVibeNugetEnv(t, r.ws, []string{"NUGET_PACKAGES=" + r.own, "NUGET_FALLBACK_PACKAGES=" + r.host}, sandboxMailbox(t),
		func(packagereq.Taken) { t.Error("tidy asks the host nothing") }, args...)
}

// What the sandbox downloaded and the user's cache now has too is dropped from the sandbox's, so a restore
// finds it in the user's first; and what the path caches held is gone with it.
func TestTidyRemovesTheSandboxsCopiesTheUsersCacheHasToo(t *testing.T) {
	r := newTidyRig(t)
	r.put(t, r.own, "a.lib", "1.0.0") // named, and the host has it: tidied
	r.put(t, r.host, "a.lib", "1.0.0")
	r.put(t, r.own, "b.lib", "2.0.0") // named, the host lacks it: kept
	r.put(t, r.own, "c.lib", "3.0.0") // the host has it, but no project names it: kept
	r.put(t, r.host, "c.lib", "3.0.0")
	r.project(t, "App", r.sha(r.own, "a.lib", "1.0.0"), r.sha(r.own, "b.lib", "2.0.0"))
	res := r.run(t, "tidy")
	if res.code != 0 {
		t.Fatalf("exit %d:\n%s", res.code, res.out)
	}
	if r.has(r.own, "a.lib", "1.0.0") || !r.has(r.own, "b.lib", "2.0.0") || !r.has(r.own, "c.lib", "3.0.0") {
		t.Errorf("a.lib gone: %v; b.lib kept: %v; c.lib kept: %v\n%s", !r.has(r.own, "a.lib", "1.0.0"), r.has(r.own, "b.lib", "2.0.0"), r.has(r.own, "c.lib", "3.0.0"), res.out)
	}
	if !r.has(r.host, "a.lib", "1.0.0") {
		t.Error("the user's cache was touched")
	}
	if _, err := os.Stat(r.assetsCache("App")); err == nil {
		t.Error("the path cache was left, still naming what was removed")
	}
	if _, err := os.Stat(filepath.Join(r.ws, "App", "obj", "project.assets.json")); err != nil {
		t.Error("what restore wrote was deleted: only the derived path caches go")
	}
	for _, want := range []string{"a.lib 1.0.0", "dotnet restore", "copy of 1 package the user's cache has too"} {
		if !strings.Contains(res.out, want) {
			t.Errorf("the output lacks %q:\n%s", want, res.out)
		}
	}
	if strings.Contains(res.out, "1 packages") {
		t.Errorf("one package is not packages:\n%s", res.out)
	}
	if strings.Contains(res.out, "b.lib") {
		t.Errorf("it says it did something about b.lib:\n%s", res.out)
	}
}

func TestTidyWithNothingToDoSaysSoAndDeletesNothing(t *testing.T) {
	r := newTidyRig(t)
	r.put(t, r.own, "b.lib", "2.0.0")
	r.project(t, "App", r.sha(r.own, "b.lib", "2.0.0"))
	res := r.run(t, "tidy")
	if res.code != 0 || !strings.Contains(res.out, "nothing to tidy") {
		t.Errorf("exit %d:\n%s", res.code, res.out)
	}
	if _, err := os.Stat(r.assetsCache("App")); err != nil {
		t.Error("the path cache was deleted with nothing tidied")
	}
	if !r.has(r.own, "b.lib", "2.0.0") {
		t.Error("a copy the user's cache lacks was removed")
	}
}

// Said twice, it does nothing the second time.
func TestTidyAgainHasNothingLeftToDo(t *testing.T) {
	r := newTidyRig(t)
	r.put(t, r.own, "a.lib", "1.0.0")
	r.put(t, r.host, "a.lib", "1.0.0")
	r.project(t, "App", r.sha(r.own, "a.lib", "1.0.0"))
	r.run(t, "tidy")
	if res := r.run(t, "tidy"); res.code != 0 || !strings.Contains(res.out, "nothing to tidy") {
		t.Errorf("the second time: exit %d:\n%s", res.code, res.out)
	}
}

// What a project.nuget.cache names is data the agent can write: only a copy in exactly the sandbox's own cache, which
// the file names there, is removed.
func TestTidyRemovesNothingOutsideTheSandboxsOwnCache(t *testing.T) {
	r := newTidyRig(t)
	outside := filepath.Join(t.TempDir(), "elsewhere")
	r.put(t, outside, "d.lib", "1.0.0") // named, but in another folder than the sandbox's own cache
	r.put(t, r.own, "d.lib", "1.0.0")   // so this copy of it is not what the file names
	r.put(t, r.host, "d.lib", "1.0.0")
	r.put(t, r.host, "e.lib", "1.0.0") // named in the user's cache itself
	r.put(t, r.own, "e.lib", "1.0.0")
	r.project(t, "App", r.sha(outside, "d.lib", "1.0.0"), r.sha(r.host, "e.lib", "1.0.0"))
	res := r.run(t, "tidy")
	if !r.has(outside, "d.lib", "1.0.0") || !r.has(r.own, "d.lib", "1.0.0") || !r.has(r.host, "e.lib", "1.0.0") || !r.has(r.own, "e.lib", "1.0.0") {
		t.Errorf("something not named in the sandbox's own cache was removed:\n%s", res.out)
	}
	if !strings.Contains(res.out, "nothing to tidy") {
		t.Errorf("%s", res.out)
	}
}

// A folder next to the cache, reached with .. in the file's path, is not the cache's to lose. (Both caches exist,
// and so does what the .. leads to on each side, or the path would not get as far as the check.)
func TestTidyDoesNotFollowADotDotOutOfTheCache(t *testing.T) {
	r := newTidyRig(t)
	victim := filepath.Join(filepath.Dir(r.own), "1.0.0") // what own/../1.0.0 is
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(filepath.Dir(r.host), "1.0.0"), 0o755); err != nil { // what host/../1.0.0 is
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(r.host), "1.0.0", ".nupkg.metadata"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Written as is: filepath.Join would tidy the .. away.
	r.project(t, "App", r.own+"/../1.0.0/x.nupkg.sha512")
	res := r.run(t, "tidy")
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("a folder beside the cache was removed through a ..:\n%s", res.out)
	}
}

// Names NuGet makes are lowercase and plain; anything else isn't taken for one, whatever is on disk.
func TestTidyLeavesWhatHasAnUnusualName(t *testing.T) {
	r := newTidyRig(t)
	r.put(t, r.own, "we ird", "1.0.0")
	r.put(t, r.host, "we ird", "1.0.0")
	r.put(t, r.own, "Mixed.Case", "1.0.0")
	r.put(t, r.host, "Mixed.Case", "1.0.0")
	r.project(t, "App", r.sha(r.own, "we ird", "1.0.0"), r.sha(r.own, "Mixed.Case", "1.0.0"))
	res := r.run(t, "tidy")
	if !r.has(r.own, "we ird", "1.0.0") || !r.has(r.own, "Mixed.Case", "1.0.0") {
		t.Errorf("a copy with an unusual name was removed:\n%s", res.out)
	}
}

func TestTidyNeedsTheUsersCacheToTidyAgainst(t *testing.T) {
	r := newTidyRig(t)
	res := runVibeNugetEnv(t, r.ws, []string{"NUGET_PACKAGES=" + r.own, "NUGET_FALLBACK_PACKAGES="}, sandboxMailbox(t), func(packagereq.Taken) {}, "tidy")
	if res.code != 1 || !strings.Contains(res.out, "NUGET_FALLBACK_PACKAGES") {
		t.Errorf("exit %d:\n%s", res.code, res.out)
	}
}

func TestTidyTakesNothingElse(t *testing.T) {
	r := newTidyRig(t)
	if res := r.run(t, "tidy", "a.lib"); res.code != 2 || !strings.Contains(res.out, "usage") {
		t.Errorf("exit %d:\n%s", res.code, res.out)
	}
}

// The agent runs status first, so that's where it's told: the host's answer, then what it can do in the sandbox.
func TestStatusSaysToTidyWhenTheUserHasWhatTheSandboxDownloaded(t *testing.T) {
	r := newTidyRig(t)
	r.put(t, r.own, "a.lib", "1.0.0")
	r.put(t, r.host, "a.lib", "1.0.0")
	r.put(t, r.own, "a.two", "1.0.0")
	r.put(t, r.host, "a.two", "1.0.0")
	r.project(t, "App", r.sha(r.own, "a.lib", "1.0.0"), r.sha(r.own, "a.two", "1.0.0"))
	m := sandboxMailbox(t)
	res := runVibeNugetEnv(t, r.ws, []string{"NUGET_PACKAGES=" + r.own, "NUGET_FALLBACK_PACKAGES=" + r.host}, m, func(tk packagereq.Taken) {
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Info, Message: "Nothing is waiting for the user."})
	}, "status")
	if res.code != 0 || !strings.Contains(res.out, "Nothing is waiting for the user.") {
		t.Fatalf("exit %d:\n%s", res.code, res.out)
	}
	// An agent that doesn't know what a command deletes waits to be told to run it: so it's said what, and that it's expected.
	for _, want := range []string{"2 of them", "vibe-nuget tidy", "dotnet restore", "only drops your own copies of exactly those packages",
		"derived path caches", "no need to ask the user first"} {
		if !strings.Contains(res.out, want) {
			t.Errorf("status lacks %q:\n%s", want, res.out)
		}
	}
	if !r.has(r.own, "a.lib", "1.0.0") {
		t.Error("status removed something: only tidy does")
	}
}

func TestStatusSaysNothingOfTidyWhenThereIsNothingToTidy(t *testing.T) {
	r := newTidyRig(t)
	r.put(t, r.own, "b.lib", "2.0.0")
	r.project(t, "App", r.sha(r.own, "b.lib", "2.0.0"))
	m := sandboxMailbox(t)
	res := runVibeNugetEnv(t, r.ws, []string{"NUGET_PACKAGES=" + r.own, "NUGET_FALLBACK_PACKAGES=" + r.host}, m, func(tk packagereq.Taken) {
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Info, Message: "Nothing is waiting for the user."})
	}, "status")
	if strings.Contains(res.out, "tidy") {
		t.Errorf("status mentions tidy with nothing to tidy:\n%s", res.out)
	}
}

// --- projects the solution doesn't cover -----------------------------------------------------------------------

// projectFile is the project file as the script says it: by the path it really has (a temp folder can be reached by
// a link, as on macOS).
func (r tidyRig) projectFile(name string) string {
	dir, err := filepath.EvalSymlinks(filepath.Join(r.ws, name))
	if err != nil {
		return filepath.Join(r.ws, name, name+".csproj")
	}
	return filepath.Join(dir, name+".csproj")
}

// A restore of the solution covers the projects in it: one outside it keeps naming the copies tidy removed, and
// nothing restores it unless the agent is told which it is. Tidy doesn't list the projects (a solution's can be dozens,
// and its restore covers nearly all): it points at status, which after that restore names only those left.
func TestTidyPointsAtStatusForTheProjectsAndDoesNotListThem(t *testing.T) {
	r := newTidyRig(t)
	r.put(t, r.own, "a.lib", "1.0.0")
	r.put(t, r.host, "a.lib", "1.0.0")
	for i := 0; i < 31; i++ {
		r.project(t, fmt.Sprintf("P%d", i), r.sha(r.own, "a.lib", "1.0.0"))
	}
	res := r.run(t, "tidy")
	if strings.Contains(res.out, "dotnet restore '") || strings.Contains(res.out, "P7") {
		t.Errorf("tidy lists projects:\n%s", res.out)
	}
	for _, want := range []string{"vibe-nuget status", "outside the solution"} {
		if !strings.Contains(res.out, want) {
			t.Errorf("tidy lacks %q:\n%s", want, res.out)
		}
	}
}

// What is left after the restore is what status names.
func TestStatusAfterATidyAndARestoreOfTheSolutionNamesOnlyTheProjectLeft(t *testing.T) {
	r := newTidyRig(t)
	r.put(t, r.own, "a.lib", "1.0.0")
	r.put(t, r.host, "a.lib", "1.0.0")
	r.project(t, "InSolution", r.sha(r.own, "a.lib", "1.0.0"))
	r.project(t, "Orphan", r.sha(r.own, "a.lib", "1.0.0"))
	r.run(t, "tidy")
	r.project(t, "InSolution", r.sha(r.host, "a.lib", "1.0.0")) // restoring the solution rewrote its file
	m := sandboxMailbox(t)
	res := runVibeNugetEnv(t, r.ws, []string{"NUGET_PACKAGES=" + r.own, "NUGET_FALLBACK_PACKAGES=" + r.host}, m, func(tk packagereq.Taken) {
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Info, Message: "Nothing is waiting for the user."})
	}, "status")
	if !strings.Contains(res.out, "dotnet restore '"+r.projectFile("Orphan")+"'") || strings.Contains(res.out, "InSolution") {
		t.Errorf("status names:\n%s", res.out)
	}
}

// Status is where the agent looks after restoring: a project still naming copies that are gone says it is not done.
func TestStatusNamesTheProjectsStillNamingCopiesThatAreGone(t *testing.T) {
	r := newTidyRig(t)
	r.project(t, "Orphan", r.sha(r.own, "gone.lib", "1.0.0")) // named in the sandbox's cache, and not there
	r.put(t, r.own, "here.lib", "1.0.0")
	r.project(t, "Fine", r.sha(r.own, "here.lib", "1.0.0"))
	m := sandboxMailbox(t)
	res := runVibeNugetEnv(t, r.ws, []string{"NUGET_PACKAGES=" + r.own, "NUGET_FALLBACK_PACKAGES=" + r.host}, m, func(tk packagereq.Taken) {
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Info, Message: "Nothing is waiting for the user."})
	}, "status")
	if want := "dotnet restore '" + r.projectFile("Orphan") + "'"; !strings.Contains(res.out, want) {
		t.Errorf("status doesn't say to %s:\n%s", want, res.out)
	}
	if strings.Contains(res.out, "Fine") {
		t.Errorf("a project whose copies are all there is listed:\n%s", res.out)
	}
	if !strings.Contains(res.out, "outside the solution") {
		t.Errorf("status doesn't say why a solution's restore may not have covered it:\n%s", res.out)
	}
}

func TestStatusSaysNothingOfProjectsWhenNoneNameCopiesThatAreGone(t *testing.T) {
	r := newTidyRig(t)
	r.put(t, r.own, "here.lib", "1.0.0")
	r.project(t, "Fine", r.sha(r.own, "here.lib", "1.0.0"))
	m := sandboxMailbox(t)
	res := runVibeNugetEnv(t, r.ws, []string{"NUGET_PACKAGES=" + r.own, "NUGET_FALLBACK_PACKAGES=" + r.host}, m, func(tk packagereq.Taken) {
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Info, Message: "Nothing is waiting for the user."})
	}, "status")
	if strings.Contains(res.out, "dotnet restore") {
		t.Errorf("status tells the agent to restore with nothing to restore:\n%s", res.out)
	}
}

// The project file a cache names is data the agent can write, and what is printed is a command it is told to run.
func TestAProjectFileThatIsNotAPlainOneInTheWorkspaceIsNeverSaidToRestore(t *testing.T) {
	for name, path := range map[string]string{
		"outside the workspace": "$OUT/x.csproj",
		"a command in it":       "$WS/x$(touch pwned).csproj",
		"a quote in it":         "$WS/x'y.csproj",
		"a semicolon":           "$WS/x;y.csproj",
		"a way back out":        "$WS/../elsewhere.csproj",
		"not a project":         "$WS/App/App.txt",
		"a line break":          "$WS/x\ny.csproj",
	} {
		t.Run(name, func(t *testing.T) {
			r := newTidyRig(t)
			r.project(t, "App", r.sha(r.own, "gone.lib", "1.0.0"))
			cache := filepath.Join(r.ws, "App", "obj", "project.nuget.cache")
			data, err := os.ReadFile(cache)
			if err != nil {
				t.Fatal(err)
			}
			path = strings.ReplaceAll(strings.ReplaceAll(path, "$WS", r.ws), "$OUT", t.TempDir())
			// It exists, so that only what it is called keeps it from being named.
			if err := os.WriteFile(path, []byte("<Project/>"), 0o644); err != nil {
				t.Fatal(err)
			}
			// The cache names the project as the rig wrote it, which may be by a link.
			written := filepath.Join(r.ws, "App", "App.csproj")
			if !strings.Contains(string(data), written) {
				t.Fatalf("the cache doesn't name %s:\n%s", written, data)
			}
			data = []byte(strings.Replace(string(data), written, path, 1))
			if err := os.WriteFile(cache, data, 0o644); err != nil {
				t.Fatal(err)
			}
			m := sandboxMailbox(t)
			res := runVibeNugetEnv(t, r.ws, []string{"NUGET_PACKAGES=" + r.own, "NUGET_FALLBACK_PACKAGES=" + r.host}, m, func(tk packagereq.Taken) {
				m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Info, Message: "Nothing is waiting for the user."})
			}, "status")
			if strings.Contains(res.out, "dotnet restore") {
				t.Errorf("a command was suggested from %q:\n%s", path, res.out)
			}
		})
	}
}

// A workspace can be reached by a link (the user's folder, in the sandbox; /var on macOS), while the shell's own
// idea of where it is, and what dotnet writes into the cache file, are the real path or the linked one: the project
// is in the workspace either way, and is said by its real path.
func TestAProjectInAWorkspaceReachedByALinkIsStillNamed(t *testing.T) {
	r := newTidyRig(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(r.ws, link); err != nil {
		t.Skip("can't make a link here: " + err.Error())
	}
	linked := r
	linked.ws = link
	linked.project(t, "Orphan", linked.sha(linked.own, "gone.lib", "1.0.0")) // the cache names the project by the link
	m := sandboxMailbox(t)
	res := runVibeNugetEnv(t, link, []string{"NUGET_PACKAGES=" + r.own, "NUGET_FALLBACK_PACKAGES=" + r.host}, m, func(tk packagereq.Taken) {
		m.Reply(tk.Nonce, packagereq.Answer{Status: packagereq.Info, Message: "Nothing is waiting for the user."})
	}, "status")
	real, err := filepath.EvalSymlinks(r.ws)
	if err != nil {
		t.Fatal(err)
	}
	if want := "dotnet restore '" + filepath.Join(real, "Orphan", "Orphan.csproj") + "'"; !strings.Contains(res.out, want) {
		t.Errorf("status doesn't say to %s:\n%s", want, res.out)
	}
}
