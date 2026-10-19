package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
