package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/gittoken"
)

// onWindows stands in for finding the sandbox's folder as on Windows, where sbx mounts C:\w at /c/w: each caller must give
// the sandbox what the lookup found, never the host's path (which a Linux sandbox has no folder at).
func onWindows(t *testing.T, fails error) {
	t.Helper()
	old := folderInSandbox
	t.Cleanup(func() { folderInSandbox = old })
	folderInSandbox = func(_ context.Context, _, folder string) (string, error) {
		if fails != nil {
			return "", fails
		}
		if folder == `C:\w` {
			return "/c/w", nil
		}
		return folder, nil
	}
}

func usesTheSandboxsFolder(t *testing.T, what, cmd string) {
	t.Helper()
	if !strings.Contains(cmd, "/c/w") || strings.Contains(cmd, `C:\w`) {
		t.Errorf("%s ran %q: want the sandbox's /c/w, not the host's C:\\w", what, cmd)
	}
}

func TestTheConversationsAgentWorksInTheSandboxsFolder(t *testing.T) {
	onWindows(t, nil)
	var args []string
	wasStart, wasEnd := startSession, endLeftover
	t.Cleanup(func() { startSession, endLeftover = wasStart, wasEnd })
	startSession = func(_ context.Context, _ string, _ func([]byte), a ...string) (agentSession, error) {
		args = a
		return newFakeSession(), nil
	}
	endLeftover = func(string, string) error { return nil }
	if _, err := startAgent("box", `C:\w`, func() (turnChoice, error) { return turnChoice{}, nil })(context.Background(), "", func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	usesTheSandboxsFolder(t, "the agent", strings.Join(args, " "))
}

func TestAnAgentWhoseFolderCantBeFoundIsNotStarted(t *testing.T) {
	onWindows(t, errors.New("couldn't find C:\\w inside the sandbox"))
	started := false
	wasStart, wasEnd := startSession, endLeftover
	t.Cleanup(func() { startSession, endLeftover = wasStart, wasEnd })
	startSession = func(context.Context, string, func([]byte), ...string) (agentSession, error) {
		started = true
		return newFakeSession(), nil
	}
	endLeftover = func(string, string) error { return nil }
	_, err := startAgent("box", `C:\w`, func() (turnChoice, error) { return turnChoice{}, nil })(context.Background(), "", func([]byte) {})
	if err == nil || !strings.Contains(err.Error(), "couldn't find") || started {
		t.Fatalf("want the lookup's error and no agent started, got %v (started %v)", err, started)
	}
}

func TestGitInTheSandboxRunsInTheSandboxsFolder(t *testing.T) {
	onWindows(t, nil)
	asked := runsInSandbox(t, nil)
	if _, err := sandboxGitFor("box", `C:\w`)(context.Background(), "status"); err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 1 {
		t.Fatalf("want one exec, got %q", *asked)
	}
	usesTheSandboxsFolder(t, "git", strings.Join((*asked)[0], " "))
}

func TestTheModelListIsAskedInTheSandboxsFolder(t *testing.T) {
	onWindows(t, nil)
	asked := runsInSandbox(t, errors.New("stop after the first ask"))
	choice := sandboxChoice("box", `C:\w`, profileChoice{}, filepath.Join(t.TempDir(), "options.json"))
	choice.Options(context.Background())
	if len(*asked) == 0 {
		t.Fatal("want claude asked")
	}
	usesTheSandboxsFolder(t, "the model list", strings.Join((*asked)[0], " "))
}

func TestThePushCheckRunsInTheSandboxsFolder(t *testing.T) {
	onWindows(t, nil)
	sandboxesUp(t, map[string]bool{"box": true}, true)
	asked := runsInSandbox(t, nil)
	checkPushFromInside("box", `C:\w`, &gittoken.Repo{Owner: "o", Name: "r"})
	if len(*asked) != 1 {
		t.Fatalf("want one exec, got %q", *asked)
	}
	usesTheSandboxsFolder(t, "the push check", strings.Join((*asked)[0], " "))
}
