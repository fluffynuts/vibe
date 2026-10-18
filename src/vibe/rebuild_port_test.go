package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	agentmem "vibe/internal/memory"
	"vibe/internal/state"
)

// The page's port is remembered so its address — a bookmark, another
// machine's ssh forwards — stays the same; a rebuild mustn't forget it.
func TestARebuiltSandboxKeepsItsPagesPort(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	inst.CompanionPort = 10007
	if err := state.Save(lay.Home, inst); err != nil {
		t.Fatal(err)
	}
	loggingSbx(t, "box running\\n")

	if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
		t.Fatal(err)
	}
	after, _, err := state.Load(lay.Home, "box")
	if err != nil {
		t.Fatal(err)
	}
	if after.CompanionPort != 10007 {
		t.Errorf("page port after the rebuild: %d, want 10007", after.CompanionPort)
	}
}

// The Review tab's folder is on the host. Only two folders of it are
// mounted into the sandbox: the inbox, where vibe-review drops the agent's
// comments, and, read-only, the view it reads the review from. The review
// itself is the host's alone.
func TestASandboxIsBuiltWithItsReviewFolderMounted(t *testing.T) {
	lay := updateLayout(t)
	inst := sandboxRecord(t, lay, "box", "p").inst
	calls := loggingSbx(t, "box running\\n")
	if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(calls)
	var create string
	for _, call := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(call, "create ") {
			create = call
		}
	}
	dir := state.ReviewPath(lay.Home, "box")
	inbox, view := filepath.Join(dir, "inbox"), filepath.Join(dir, "view")
	if !strings.Contains(create, " "+inbox+" ") || !strings.Contains(create, " "+view+":ro") || !strings.Contains(create, "-e VIBE_REVIEW_DIR="+dir) || !strings.Contains(create, "-e VIBE_REVIEW_WORKSPACE="+inst.Target) {
		t.Errorf("sbx create wasn't given the inbox, and the view read-only:\n%s", create)
	}
	if strings.Contains(create, " "+dir+" ") || strings.HasSuffix(create, " "+dir) {
		t.Errorf("the whole review folder was mounted:\n%s", create)
	}
	for _, d := range []string{inbox, view} {
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			t.Errorf("%s wasn't made", d)
		}
	}
}

// createCall is the 'sbx create' a logging stand-in sbx saw.
func createCall(t *testing.T, calls string) string {
	t.Helper()
	data, _ := os.ReadFile(calls)
	for _, call := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(call, "create ") {
			return call
		}
	}
	t.Fatalf("sbx create wasn't run:\n%s", data)
	return ""
}

// Every folder of the host's a sandbox is given, and the variable that
// tells the sandbox where it is: the packages in the NuGet folder the profile
// names (read-only, when they're there), the agent's memories, the review's inbox and (read-only)
// view, and the clipboard's history.
func TestASandboxIsGivenEachOfItsFolders(t *testing.T) {
	lay := updateLayout(t)
	nuget := t.TempDir()
	packages := filepath.Join(nuget, "packages")
	if err := os.Mkdir(packages, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), "nugetDir: "+nuget+"\n"+
		"publish:\n  - name: clipboard\n    ports:\n      - 5390\n")
	inst := sandboxRecord(t, lay, "box", "p").inst
	calls := loggingSbx(t, "box running\\n")
	if err := rebuildSandbox(context.Background(), lay, cliargs.Args{Force: true}, "box", inst.Target, true, false); err != nil {
		t.Fatal(err)
	}
	create := createCall(t, calls)
	reviews := state.ReviewPath(lay.Home, "box")
	memories := agentmem.StoreFor(lay.Home, "", "box")
	clipboard := state.ClipboardPath(lay.Home, "box")
	for _, want := range []string{
		" " + packages + ":ro", "-e SBX_HOST_NUGET_PACKAGES=" + packages,
		" " + memories + " ", "-e VIBE_MEMORY_STORE=" + memories,
		" " + filepath.Join(reviews, "inbox") + " ", " " + filepath.Join(reviews, "view") + ":ro",
		"-e VIBE_REVIEW_DIR=" + reviews,
		" " + clipboard, "-e VIBE_CLIPBOARD_DIR=" + clipboard,
	} {
		if !strings.Contains(create+" ", want) {
			t.Errorf("sbx create lacks %q:\n%s", want, create)
		}
	}
}
