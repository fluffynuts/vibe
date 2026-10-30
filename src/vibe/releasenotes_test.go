package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A push straight to master has no pull request, so the release's notes are headed by its last commit's message. The jq filter
// that makes them is in the release workflow, which only runs on GitHub: this runs the same filter on sample commits.
func commitNotesFilter(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "build.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`commit_filter='([^']*)'`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("the release workflow has no commit_filter")
	}
	return m[1]
}

func notesFor(t *testing.T, message string) string {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("no jq")
	}
	quoted, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	input := `{"commit":{"message":` + string(quoted) + `}}`
	cmd := exec.Command("jq", "-r", commitNotesFilter(t))
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("jq: %v: %s", err, out)
	}
	return string(out)
}

func TestTheNotesOfAPushWithNoPullRequestAreTheLastCommitsMessage(t *testing.T) {
	got := notesFor(t, ":sparkles: an API key option\n\nIt is kept in sbx.\nIt applies to every sandbox.\n")
	if !strings.HasPrefix(got, "## :sparkles: an API key option\n\nIt is kept in sbx.\nIt applies to every sandbox.\n") {
		t.Errorf("notes:\n%s", got)
	}
	if !strings.Contains(got, "---") {
		t.Errorf("no rule under the message, which the install notes follow:\n%s", got)
	}
}

func TestACommitWithOnlyASubjectHeadsTheNotesWithIt(t *testing.T) {
	if got := notesFor(t, "just a subject"); !strings.HasPrefix(got, "## just a subject\n") {
		t.Errorf("notes:\n%s", got)
	}
}

func TestTheCoAuthorLineIsNotInTheNotes(t *testing.T) {
	got := notesFor(t, "a change\n\nwhat it does\n\nCo-Authored-By: Someone <x@example.com>\n")
	if strings.Contains(got, "Co-Authored-By") || !strings.Contains(got, "what it does") {
		t.Errorf("notes:\n%s", got)
	}
}

func TestAMessageNeverBecomesPartOfAScript(t *testing.T) {
	got := notesFor(t, "a $(touch /tmp/pwned) `id` \"quoted\" change\n\n'single' and $HOME\n")
	if !strings.Contains(got, "$(touch /tmp/pwned) `id` \"quoted\"") || !strings.Contains(got, "$HOME") {
		t.Errorf("the message was changed on the way:\n%s", got)
	}
}
