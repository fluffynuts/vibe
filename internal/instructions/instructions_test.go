package instructions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func TestEnsureFileCreatesAnEmptyOneAndLeavesAnExistingOneAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deeper", File)
	if created, err := EnsureFile(path); err != nil || !created {
		t.Fatalf("first: created %v, %v", created, err)
	}
	if got, ok := read(t, path); !ok || got != "" {
		t.Errorf("created %q, %v", got, ok)
	}
	os.WriteFile(path, []byte("mine"), 0o644)
	if created, err := EnsureFile(path); err != nil || created {
		t.Errorf("again: created %v, %v", created, err)
	}
	if got, _ := read(t, path); got != "mine" {
		t.Errorf("an existing file became %q", got)
	}
}

func TestAGuidedProfilesKitTextMovesOutOfTheWayOnce(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, File), []byte("## Diffity\nuse it"), 0o644)
	moved, err := MigrateProfile(dir, true)
	if err != nil || !moved {
		t.Fatalf("moved %v, %v", moved, err)
	}
	if got, _ := read(t, filepath.Join(dir, KitFile)); got != "## Diffity\nuse it" {
		t.Errorf("kit file = %q", got)
	}
	if got, ok := read(t, filepath.Join(dir, File)); !ok || got != "" {
		t.Errorf("the user's file = %q, %v; want a blank one", got, ok)
	}

	// Once moved, the user's own words are never taken for the kits'.
	os.WriteFile(filepath.Join(dir, File), []byte("mine"), 0o644)
	if moved, err := MigrateProfile(dir, true); err != nil || moved {
		t.Errorf("again: moved %v, %v", moved, err)
	}
	if got, _ := read(t, filepath.Join(dir, File)); got != "mine" {
		t.Errorf("the user's file became %q", got)
	}
}

func TestAHandMadeProfilesInstructionsAreItsAuthors(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, File), []byte("written by hand"), 0o644)
	if moved, err := MigrateProfile(dir, false); err != nil || moved {
		t.Fatalf("moved %v, %v", moved, err)
	}
	if got, _ := read(t, filepath.Join(dir, File)); got != "written by hand" {
		t.Errorf("file = %q", got)
	}
	if _, ok := read(t, filepath.Join(dir, KitFile)); ok {
		t.Error("a kit file was made")
	}
}

func TestAProfileWithNoInstructionsGetsABlankFile(t *testing.T) {
	for _, guided := range []bool{true, false} {
		dir := t.TempDir()
		if moved, err := MigrateProfile(dir, guided); err != nil || moved {
			t.Errorf("guided %v: moved %v, %v", guided, moved, err)
		}
		if got, ok := read(t, filepath.Join(dir, File)); !ok || got != "" {
			t.Errorf("guided %v: file = %q, %v", guided, got, ok)
		}
	}
}

func TestMergeIsUniversalThenProjectAndLeavesOutWhatIsBlank(t *testing.T) {
	both := Merge("Use British spelling.\r\n", "  This project uses Go.\n\n")
	want := "Universal instructions\n----------------------\n\nUse British spelling.\n\n" +
		"Project-specific instructions\n-----------------------------\n\nThis project uses Go.\n"
	if both != want {
		t.Errorf("both:\n%q\nwant\n%q", both, want)
	}
	if got := Merge(" \n", "Go."); strings.Contains(got, "Universal") || !strings.Contains(got, "Go.") {
		t.Errorf("blank universal: %q", got)
	}
	if got := Merge("British.", ""); strings.Contains(got, "Project-specific") {
		t.Errorf("blank project: %q", got)
	}
	if got := Merge("\r\n", " "); got != "" {
		t.Errorf("both blank: %q", got)
	}
}

func TestVibesBlockIsAddedReplacedAndRemovedLeavingTheRestAlone(t *testing.T) {
	theirs := "# Memory\n\n- the user likes tabs\n"
	added := WithBlock(theirs, "@/home/agent/.claude/vibe-instructions.md")
	if !strings.HasPrefix(added, theirs) || !strings.Contains(added, BeginMarker+"\n@/home/agent/.claude/vibe-instructions.md\n"+EndMarker) {
		t.Fatalf("added:\n%s", added)
	}
	// Claude adds a memory after vibe's part; vibe's part changes.
	later := added + "\n- remember: run make check\n"
	replaced := WithBlock(later, "inline text")
	for _, want := range []string{"# Memory", "- the user likes tabs", "- remember: run make check", BeginMarker + "\ninline text\n" + EndMarker} {
		if !strings.Contains(replaced, want) {
			t.Errorf("replaced is missing %q:\n%s", want, replaced)
		}
	}
	if strings.Count(replaced, BeginMarker) != 1 || strings.Contains(replaced, "vibe-instructions.md") {
		t.Errorf("the old part wasn't replaced:\n%s", replaced)
	}
	removed := WithBlock(replaced, "")
	if strings.Contains(removed, BeginMarker) || strings.Contains(removed, "inline text") {
		t.Errorf("removed:\n%s", removed)
	}
	if !strings.Contains(removed, "- the user likes tabs") || !strings.Contains(removed, "- remember: run make check") {
		t.Errorf("removing vibe's part took the rest with it:\n%s", removed)
	}
	if got := WithBlock("", ""); got != "" {
		t.Errorf("nothing, nothing: %q", got)
	}
	if got := WithBlock(WithBlock("", "x"), ""); got != "" {
		t.Errorf("a file that was only vibe's part, emptied: %q", got)
	}
}

func TestAgentsThatImportGetOneLineTheOthersTheText(t *testing.T) {
	if got := Agents["claude"].Block("/home/agent", "text"); got != "@/home/agent/.claude/vibe-instructions.md" {
		t.Errorf("claude: %q", got)
	}
	if got := Agents["codex"].Block("/home/agent", "text"); got != "text" {
		t.Errorf("codex: %q", got)
	}
	if got := Agents["claude"].Block("/home/agent", " \n"); got != "" {
		t.Errorf("nothing to give: %q", got)
	}
}

func TestOptimisePromptGivesTheOtherFileAsContextOnly(t *testing.T) {
	p := OptimisePrompt(false, "Use Go.\nUse British spelling.", "Use British spelling.")
	for _, want := range []string{"universal instructions, below, already say", "For context only", "Use British spelling.", "The file to rewrite:\n<document>\nUse Go."} {
		if !strings.Contains(p, want) {
			t.Errorf("project prompt lacks %q:\n%s", want, p)
		}
	}
	if u := OptimisePrompt(true, "Be brief.", ""); strings.Contains(u, "For context only") || !strings.Contains(u, "keep it general") {
		t.Errorf("universal prompt with no project file:\n%s", u)
	}
}

func TestOnlyOneTaggedDocumentIsTakenFromTheReply(t *testing.T) {
	doc, err := ExtractDocument("Sure! Here it is:\r\n<document>\n# Rules\n\n```sh\nmake check\n```\n</document>\nHope that helps.")
	if err != nil || doc != "# Rules\n\n```sh\nmake check\n```\n" {
		t.Errorf("doc %q, %v", doc, err)
	}
	for _, bad := range []string{"no tags at all", "<document>a</document> <document>b</document>", "</document>backwards<document>", "<document>  </document>"} {
		if _, err := ExtractDocument(bad); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
}
