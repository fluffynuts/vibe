package userprefs

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestUpdatesMadeAtOnceAreAllKept(t *testing.T) {
	changes := []func(*Prefs){
		func(p *Prefs) { p.Theme = "amber" },
		func(p *Prefs) { p.GitAuthor = "import" },
		func(p *Prefs) { p.Model = "opus" },
		func(p *Prefs) { p.Effort = "high" },
	}
	for round := 0; round < 200; round++ {
		dir := t.TempDir()
		start := make(chan struct{})
		var done sync.WaitGroup
		for _, change := range changes {
			done.Add(1)
			go func(change func(*Prefs)) {
				defer done.Done()
				<-start
				if err := Update(dir, change); err != nil {
					t.Error(err)
				}
			}(change)
		}
		close(start)
		done.Wait()
		got, err := Load(dir)
		if err != nil || got.Theme != "amber" || got.GitAuthor != "import" || got.Model != "opus" || got.Effort != "high" {
			t.Fatalf("round %d: got %+v, %v: an update was lost to another made at the same time", round, got, err)
		}
	}
}

func TestAnUpdateThatChangesNothingDoesNotWriteTheFile(t *testing.T) {
	dir := t.TempDir()
	if err := Update(dir, func(p *Prefs) { p.Model = "opus" }); err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(Path(dir), then, then); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, func(p *Prefs) { p.Model = "opus" }); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(Path(dir))
	if err != nil || !info.ModTime().Equal(then) {
		t.Errorf("modified %v, want it left at %v: %v", info.ModTime(), then, err)
	}
}

// Earlier vibes kept the theme and the git author answer in files of their own (claude-theme, git-author). They were read, and
// moved into the preferences, for as long as a profile might still have them; no profile of this age can. They are nobody's now:
// not read, and not removed either, as a file that is there is the user's to deal with.
func TestTheFilesEarlierVibesKeptChoicesInAreNeitherReadNorTouched(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"claude-theme": "amber\n", "git-author": "import\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := Load(dir); err != nil || got.Theme != "" || got.GitAuthor != "" {
		t.Errorf("Load = %+v, %v: an old file was read", got, err)
	}
	if err := Update(dir, func(p *Prefs) { p.Model = "sonnet" }); err != nil {
		t.Fatal(err)
	}
	got, _ := Load(dir)
	if got.Model != "sonnet" || got.Theme != "" || got.GitAuthor != "" {
		t.Errorf("after an update: %+v: an old file was moved in", got)
	}
	for _, name := range []string{"claude-theme", "git-author"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
}
