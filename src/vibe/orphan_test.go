package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/sbxrun"
)

func TestAMountIsMissingWhenItsFolderIsGoneWhateverItsAccessSuffix(t *testing.T) {
	present := map[string]bool{"/work": true, "/home/me/.vibe/clipboard/box": true}
	got := missingWorkspaces([]string{
		"/work",
		"/home/me/.vibe/memories/box",
		"/home/me/.vibe/clipboard/box",
		"/home/me/.vibe/reviews/box/view:ro",
		"/home/me/.vibe/reviews/box/inbox:rw",
	}, func(p string) bool { return present[p] })
	want := "/home/me/.vibe/memories/box,/home/me/.vibe/reviews/box/view,/home/me/.vibe/reviews/box/inbox"
	if strings.Join(got, ",") != want {
		t.Errorf("missing = %v", got)
	}
}

func TestAWindowsPathIsNotTakenForOneWithASuffix(t *testing.T) {
	got := missingWorkspaces([]string{`C:\Users\me\.vibe\memories\box`, `C:\Users\me\.vibe\reviews\box\view:ro`}, func(string) bool { return false })
	if len(got) != 2 || got[0] != `C:\Users\me\.vibe\memories\box` || got[1] != `C:\Users\me\.vibe\reviews\box\view` {
		t.Errorf("missing = %q", got)
	}
}

type fakeOrphan struct {
	listed    []sbxrun.Status
	listErr   error
	present   map[string]bool
	yes       bool
	asked     []string
	removed   []string
	removeErr error
	said      []string
}

func (f *fakeOrphan) check(args cliargs.Args, canAsk bool) error {
	return clearOrphanedSandboxWith(context.Background(), "box", args, orphanSteps{
		list:    func(context.Context) ([]sbxrun.Status, error) { return f.listed, f.listErr },
		exists:  func(p string) bool { return f.present[p] },
		canAsk:  canAsk,
		confirm: func(q string) bool { f.asked = append(f.asked, q); return f.yes },
		remove:  func(_ context.Context, name string) error { f.removed = append(f.removed, name); return f.removeErr },
		say:     func(line string) { f.said = append(f.said, line) },
	})
}

func orphaned() *fakeOrphan {
	return &fakeOrphan{
		listed:  []sbxrun.Status{{Name: "box", State: "stopped", Workspaces: []string{"/work", "/home/me/.vibe/memories/box"}}},
		present: map[string]bool{"/work": true},
	}
}

func TestASandboxWithAllItsFoldersIsLeftAlone(t *testing.T) {
	f := orphaned()
	f.present["/home/me/.vibe/memories/box"] = true
	if err := f.check(cliargs.Args{}, true); err != nil || len(f.asked) != 0 || len(f.removed) != 0 || len(f.said) != 0 {
		t.Errorf("err %v, asked %v, removed %v, said %v", err, f.asked, f.removed, f.said)
	}
}

func TestNoSandboxByThatNameIsNothingToClear(t *testing.T) {
	f := orphaned()
	f.listed = []sbxrun.Status{{Name: "another", Workspaces: []string{"/gone"}}}
	if err := f.check(cliargs.Args{}, true); err != nil || len(f.asked) != 0 {
		t.Errorf("err %v, asked %v", err, f.asked)
	}
}

func TestAnOrphanedSandboxIsSaidToBeAndOfferedRemovalWithTheFoldersNamed(t *testing.T) {
	f := orphaned()
	f.yes = true
	if err := f.check(cliargs.Args{}, true); err != nil {
		t.Fatal(err)
	}
	said := strings.Join(f.said, "\n")
	for _, want := range []string{"box", "/home/me/.vibe/memories/box", "can't start"} {
		if !strings.Contains(said, want) {
			t.Errorf("it doesn't say %q: %s", want, said)
		}
	}
	if len(f.asked) != 1 || !strings.Contains(f.asked[0], "Remove") {
		t.Errorf("asked %v", f.asked)
	}
	if len(f.removed) != 1 || f.removed[0] != "box" {
		t.Errorf("removed %v", f.removed)
	}
}

func TestDecliningLeavesItAndStopsWithTheCommandToRunByHand(t *testing.T) {
	f := orphaned()
	err := f.check(cliargs.Args{}, true)
	if err == nil || !strings.Contains(err.Error(), "sbx rm box") || len(f.removed) != 0 {
		t.Errorf("err %v, removed %v", err, f.removed)
	}
}

// Nothing is deleted unasked, and -f is not asking: a script is told what to run.
func TestWithNoTerminalOrWithForceItIsNeverRemovedOnlyStoppedAt(t *testing.T) {
	for name, tc := range map[string]struct {
		args   cliargs.Args
		canAsk bool
	}{"no terminal": {cliargs.Args{}, false}, "-f": {cliargs.Args{Force: true}, true}} {
		f := orphaned()
		f.yes = true
		err := f.check(tc.args, tc.canAsk)
		if err == nil || !strings.Contains(err.Error(), "sbx rm box") || len(f.removed) != 0 || len(f.asked) != 0 {
			t.Errorf("%s: err %v, removed %v, asked %v", name, err, f.removed, f.asked)
		}
	}
}

func TestARemovalThatFailsIsAnError(t *testing.T) {
	f := orphaned()
	f.yes = true
	f.removeErr = errors.New("exit status 1")
	if err := f.check(cliargs.Args{}, true); err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("err = %v", err)
	}
}

// Whether a sandbox is whole is no reason to stop a start that sbx can't be asked about.
func TestAListingThatFailsIsLeftForSbxsOwnErrorToSay(t *testing.T) {
	f := orphaned()
	f.listErr = errors.New("sbx ls: boom")
	if err := f.check(cliargs.Args{}, true); err != nil || len(f.asked) != 0 {
		t.Errorf("err %v, asked %v", err, f.asked)
	}
}
