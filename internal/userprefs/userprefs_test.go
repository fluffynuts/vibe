package userprefs

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAProfileWithNoPreferencesHasNone(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil || got.Theme != "" || got.GitAuthor != "" || got.Model != "" || got.Effort != "" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestWhatIsUpdatedIsReadBack(t *testing.T) {
	dir := t.TempDir()
	if err := Update(dir, func(p *Prefs) { p.Model, p.Effort = "opus", "high" }); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, func(p *Prefs) { p.GitAuthor = "import" }); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || got.Model != "opus" || got.Effort != "high" || got.GitAuthor != "import" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestOnePreferenceChangedLeavesTheOthers(t *testing.T) {
	dir := t.TempDir()
	Update(dir, func(p *Prefs) { p.Model, p.Theme = "opus", "amber" })
	Update(dir, func(p *Prefs) { p.Model = "" })
	got, _ := Load(dir)
	if got.Model != "" || got.Theme != "amber" {
		t.Errorf("got %+v", got)
	}
}

func TestTheFileSaysWhatItIsAndHowToUndoAChoice(t *testing.T) {
	dir := t.TempDir()
	Update(dir, func(p *Prefs) { p.Model = "opus" })
	data, _ := os.ReadFile(Path(dir))
	for _, want := range []string{"# ", "delete", "model: opus"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the file lacks %q:\n%s", want, data)
		}
	}
}

func TestWhenNothingIsLeftTheFileGoes(t *testing.T) {
	dir := t.TempDir()
	Update(dir, func(p *Prefs) { p.Model = "opus" })
	Update(dir, func(p *Prefs) { p.Model = "" })
	if _, err := os.Stat(Path(dir)); err == nil {
		t.Error("an empty file was left")
	}
	if err := Update(dir, func(p *Prefs) {}); err != nil {
		t.Errorf("nothing to keep, nothing to do: %v", err)
	}
	if _, err := os.Stat(Path(dir)); err == nil {
		t.Error("a file was made for nothing")
	}
}

func TestWhatThisVibeDoesNotKnowIsKept(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(Path(dir), []byte("model: opus\nfuture: thing\nnested:\n  a: 1\n"), 0o644)
	if err := Update(dir, func(p *Prefs) { p.Effort = "low" }); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(Path(dir))
	for _, want := range []string{"future: thing", "nested:", "model: opus", "effort: low"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("lost %q:\n%s", want, data)
		}
	}
}

func TestAFileThatCannotBeReadIsSaidAndNotWrittenOver(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(Path(dir), []byte("model: [unclosed\n"), 0o644)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), File) {
		t.Errorf("Load err = %v, want one naming the file", err)
	}
	if err := Update(dir, func(p *Prefs) { p.Model = "opus" }); err == nil {
		t.Error("a file the user may have been editing was written over")
	}
	if data, _ := os.ReadFile(Path(dir)); string(data) != "model: [unclosed\n" {
		t.Errorf("the file was changed: %q", data)
	}
}

func TestAProfileThatIsNotThereIsNotMade(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	if err := Update(dir, func(p *Prefs) { p.Model = "opus" }); err == nil {
		t.Error("a folder was made for it")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the folder was made")
	}
}

func TestTheFileCanBeReadByOthersAndIsNeverSeenHalfWritten(t *testing.T) {
	dir := t.TempDir()
	Update(dir, func(p *Prefs) { p.Model = "opus"; p.Theme = "amber" })
	if info, err := os.Stat(Path(dir)); err != nil || info.Mode().Perm() != 0o644 && !windows() {
		t.Errorf("mode %v, %v", info.Mode(), err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			model := []string{"opus", "sonnet"}[i%2]
			if err := Update(dir, func(p *Prefs) { p.Model = model }); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for i := 0; i < 300; i++ {
		got, err := Load(dir)
		if err != nil || got.Model == "" || got.Theme != "amber" {
			t.Fatalf("a read saw %+v, %v", got, err)
		}
	}
	close(stop)
	wg.Wait()
}

func windows() bool { return os.PathSeparator == '\\' }

func TestWhatEarlierVibesKeptInTheirOwnFilesIsStillRead(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "claude-theme"), []byte("amber\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "git-author"), []byte("import\n"), 0o644)
	got, err := Load(dir)
	if err != nil || got.Theme != "amber" || got.GitAuthor != "import" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestTheNewFileWinsOverWhatEarlierVibesKept(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "claude-theme"), []byte("amber\n"), 0o644)
	os.WriteFile(Path(dir), []byte("theme: dark\n"), 0o644)
	if got, _ := Load(dir); got.Theme != "dark" {
		t.Errorf("theme = %q", got.Theme)
	}
}

func TestTheFirstChangeMovesTheOldFilesIntoTheNewOne(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "claude-theme"), []byte("amber\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "git-author"), []byte("leave\n"), 0o644)
	if err := Update(dir, func(p *Prefs) { p.Model = "opus" }); err != nil {
		t.Fatal(err)
	}
	for _, old := range LegacyFiles {
		if _, err := os.Stat(filepath.Join(dir, old)); err == nil {
			t.Errorf("%s was left", old)
		}
	}
	got, _ := Load(dir)
	if got.Theme != "amber" || got.GitAuthor != "leave" || got.Model != "opus" {
		t.Errorf("got %+v", got)
	}
}

func TestAnOldChoiceUndoneStaysUndone(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "git-author"), []byte("import\n"), 0o644)
	Update(dir, func(p *Prefs) { p.GitAuthor = "" })
	if got, _ := Load(dir); got.GitAuthor != "" {
		t.Errorf("the old file's answer came back: %q", got.GitAuthor)
	}
}

func TestMovingTheOldFilesWithNothingElseToKeepLeavesNoEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := Migrate(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(dir)); err == nil {
		t.Error("a file was made with nothing to say")
	}
	os.WriteFile(filepath.Join(dir, "claude-theme"), []byte("amber\n"), 0o644)
	if err := Migrate(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(dir); got.Theme != "amber" {
		t.Errorf("got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "claude-theme")); err == nil {
		t.Error("the old file was left")
	}
	if err := Migrate(dir); err != nil {
		t.Errorf("a second time: %v", err)
	}
}

func TestAFileTheUserMadePrivateStaysSoAndANewOneIsReadableByOthers(t *testing.T) {
	if windows() {
		t.Skip("no modes here")
	}
	dir := t.TempDir()
	os.WriteFile(Path(dir), []byte("model: opus\n"), 0o600)
	if err := Update(dir, func(p *Prefs) { p.Effort = "low" }); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(Path(dir)); info.Mode().Perm() != 0o600 {
		t.Errorf("a private file became %v", info.Mode().Perm())
	}
	fresh := t.TempDir()
	Update(fresh, func(p *Prefs) { p.Effort = "low" })
	if info, _ := os.Stat(Path(fresh)); info.Mode().Perm() != 0o644 {
		t.Errorf("a new file is %v", info.Mode().Perm())
	}
}

func TestACommentAndAnUnknownKeyTheUserWroteSurviveAChange(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(Path(dir), []byte("# why I use opus\nmodel: opus\nfuture: thing\n"), 0o644)
	if err := Update(dir, func(p *Prefs) { p.Effort = "low" }); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(Path(dir))
	for _, want := range []string{"# why I use opus", "future: thing", "model: opus", "effort: low"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("lost %q:\n%s", want, data)
		}
	}
}

func TestMigratingWithNothingOldToMoveLeavesTheFileExactlyAsItIs(t *testing.T) {
	dir := t.TempDir()
	text := "# mine\ntheme:   amber\nfuture: thing\n"
	os.WriteFile(Path(dir), []byte(text), 0o644)
	if err := Migrate(dir); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(Path(dir)); string(data) != text {
		t.Errorf("the file was rewritten: %q", data)
	}
}
