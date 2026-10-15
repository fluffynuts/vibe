package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMovingASandboxsRecordsTakesEverythingVibeKeepsForIt(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, Instance{Name: "old", Target: "/code/old"}); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		tokenPath(home, "old"):                                      "page token",
		GitTokenPath(home, "old"):                                   "git token",
		ConversationPath(home, "old"):                               "{}",
		TranscriptPath(home, "old"):                                 "tgz",
		filepath.Join(ClipboardPath(home, "old"), "items", "1.png"): "png",
		filepath.Join(ReviewPath(home, "old"), "review.json"):       "{}",
	}
	for file, content := range files {
		os.MkdirAll(filepath.Dir(file), 0o700)
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := Move(home, "old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := Load(home, "old"); found {
		t.Error("the old record is still there")
	}
	if inst, found, _ := Load(home, "new"); !found || inst.Name != "new" {
		t.Errorf("the new record: %+v, found %v", inst, found)
	}
	for _, moved := range []string{
		tokenPath(home, "new"), GitTokenPath(home, "new"), ConversationPath(home, "new"), TranscriptPath(home, "new"),
		filepath.Join(ClipboardPath(home, "new"), "items", "1.png"),
		filepath.Join(ReviewPath(home, "new"), "review.json"),
	} {
		if _, err := os.Stat(moved); err != nil {
			t.Errorf("not moved: %s", moved)
		}
	}
	for old := range files {
		if _, err := os.Stat(old); err == nil {
			t.Errorf("still there: %s", old)
		}
	}
}

func TestMovingASandboxWithoutEverythingMovesWhatItHas(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, Instance{Name: "old", Target: "/code/old"}); err != nil {
		t.Fatal(err)
	}
	if err := Move(home, "old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := Load(home, "new"); !found {
		t.Error("the record wasn't moved")
	}
}

func TestMovingOntoAnotherSandboxsRecordsIsRefused(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"old", "new"} {
		if err := Save(home, Instance{Name: name, Target: "/code/" + name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := Move(home, "old", "new"); err == nil {
		t.Error("moved over another sandbox's record")
	}
	if _, found, _ := Load(home, "old"); !found {
		t.Error("the refused move lost the old record")
	}
}

func TestRemovingASandboxsRecordsTakesItsReviewsToo(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, Instance{Name: "box", Target: "/code/box"}); err != nil {
		t.Fatal(err)
	}
	review := filepath.Join(ReviewPath(home, "box"), "review.json")
	os.MkdirAll(filepath.Dir(review), 0o700)
	os.WriteFile(review, []byte("{}"), 0o600)
	if err := Remove(home, "box"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ReviewPath(home, "box")); err == nil {
		t.Error("its reviews are still there")
	}
}
