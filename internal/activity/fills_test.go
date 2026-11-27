package activity

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func repoWeek(repo string, days ...float64) RepoWeek {
	r := RepoWeek{Repo: repo, Days: make([]float64, 7)}
	copy(r.Days, days)
	for _, h := range days {
		r.Hours += h
	}
	return r
}

var week23 = []string{"2026-11-23", "2026-11-24", "2026-11-25", "2026-11-26", "2026-11-27", "2026-11-28", "2026-11-29"}

func TestEachRepositoryGetsTheLowestFreeFillTheLongestFirstAndKeepsIt(t *testing.T) {
	f := Fills{Path: filepath.Join(t.TempDir(), "fills.json")}
	first := time.Date(2026, 11, 13, 0, 0, 0, 0, time.Local)
	got, err := f.Assign([]RepoWeek{repoWeek("vibe", 5), repoWeek("yumbi", 2)}, week23, first)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{"vibe": 0, "yumbi": 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	got, _ = f.Assign([]RepoWeek{repoWeek("NExpect", 9), repoWeek("yumbi", 1)}, week23, first)
	if want := map[string]int{"NExpect": 2, "yumbi": 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("want yumbi's kept and NExpect the next free, however long, got %v", got)
	}
}

func TestAFillIsFreedOnceItsRepositoryHasntBeenSeenInTheDaysKept(t *testing.T) {
	f := Fills{Path: filepath.Join(t.TempDir(), "fills.json")}
	week09 := []string{"2026-11-09", "2026-11-10", "2026-11-11", "2026-11-12", "2026-11-13", "2026-11-14", "2026-11-15"}
	if _, err := f.Assign([]RepoWeek{repoWeek("old", 1), repoWeek("vibe", 0, 0, 0, 0, 0, 2)}, week09, time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	// Later: the first day kept is the 14th; "old" was last seen on the 9th, vibe on the 14th.
	got, err := f.Assign([]RepoWeek{repoWeek("new", 3)}, week23, time.Date(2026, 11, 14, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if got["new"] != 0 {
		t.Fatalf("want old's fill, freed, given to new, got %v", got)
	}
	got, _ = f.Assign([]RepoWeek{repoWeek("vibe", 1)}, week23, time.Date(2026, 11, 14, 0, 0, 0, 0, time.Local))
	if got["vibe"] != 1 {
		t.Fatalf("want vibe, seen on the first day kept, to keep its fill, got %v", got)
	}
}

func TestAFillsFileThatCantBeReadIsBegunAfreshAndSaidSo(t *testing.T) {
	f := Fills{Path: filepath.Join(t.TempDir(), "fills.json")}
	os.WriteFile(f.Path, []byte("{not json"), 0o600)
	got, err := f.Assign([]RepoWeek{repoWeek("vibe", 1)}, week23, time.Date(2026, 11, 14, 0, 0, 0, 0, time.Local))
	if err == nil || !strings.Contains(err.Error(), "given out afresh") || got["vibe"] != 0 {
		t.Fatalf("want vibe given the first fill and the trouble said, got %v, %v", got, err)
	}
	if _, err := f.Assign([]RepoWeek{repoWeek("vibe", 1)}, week23, time.Date(2026, 11, 14, 0, 0, 0, 0, time.Local)); err != nil {
		t.Fatalf("want the file written afresh, readable next time, got %v", err)
	}
}
