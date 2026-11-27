package activity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTheRulesFilesAreTheDaysAndTheWeeksWrittenFromTheirDefaultsTheFirstTime(t *testing.T) {
	dir := t.TempDir()
	s := summariser(t, dir, nil, "vibe")
	files := s.RulesFiles()
	if len(files) != 2 || files[0].Which != "day" || files[1].Which != "week" {
		t.Fatalf("want the day's then the week's, got %+v", files)
	}
	if files[0].Text != DefaultRules || files[0].Default != DefaultRules || files[1].Text != DefaultWeekRules || files[1].Default != DefaultWeekRules {
		t.Fatal("want each written from, and told, its default")
	}
	if files[0].Path != filepath.Join(dir, "summary-rules.md") {
		t.Fatalf("want its path, got %q", files[0].Path)
	}
	if _, err := os.Stat(files[1].Path); err != nil {
		t.Fatalf("want the week's written for the user: %v", err)
	}
}

func TestSavingRulesKeepsThemUnlessTheFileChangedSinceItWasRead(t *testing.T) {
	s := summariser(t, t.TempDir(), nil, "vibe")
	week := s.RulesFiles()[1]
	got, err := s.SaveRules("week", "Five bullets at most.\n", week.Text)
	if err != nil || got.Text != "Five bullets at most.\n" {
		t.Fatalf("want it kept, got %+v, %v", got, err)
	}
	if data, _ := os.ReadFile(week.Path); string(data) != "Five bullets at most.\n" {
		t.Fatalf("want the file written, got %q", data)
	}
	if _, err := s.SaveRules("week", "mine", week.Text); !errors.Is(err, ErrRulesChanged) {
		t.Fatalf("want a save over a version since changed refused, got %v", err)
	}
	if _, err := s.SaveRules("month", "x", ""); err == nil {
		t.Fatal("want rules that aren't the day's or the week's refused")
	}
}
