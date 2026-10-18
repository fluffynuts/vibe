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
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "VIBE_PACKAGES_DIR=" + mailbox.Dir}
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
