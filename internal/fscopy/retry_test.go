package fscopy

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var errBusy = errors.New("being used by another process")

// busyFor makes errBusy the in-use error, and keeps the waits instead of
// waiting; restore puts them back.
func busyFor(t *testing.T) (waits *[]time.Duration) {
	t.Helper()
	waits = new([]time.Duration)
	wasInUse, wasPause := inUse, pause
	inUse = func(err error) bool { return errors.Is(err, errBusy) }
	pause = func(d time.Duration) { *waits = append(*waits, d) }
	t.Cleanup(func() { inUse, pause = wasInUse, wasPause })
	return waits
}

func TestAnOperationThatSucceedsIsNotWaitedFor(t *testing.T) {
	waits := busyFor(t)
	calls := 0
	if err := retryInUse("f", func() error { calls++; return nil }); err != nil || calls != 1 || len(*waits) != 0 {
		t.Errorf("err %v, calls %d, waits %v", err, calls, *waits)
	}
}

func TestAnOperationThatFindsTheFileInUseIsTriedAgainAfterAWait(t *testing.T) {
	waits := busyFor(t)
	calls := 0
	err := retryInUse("f", func() error {
		calls++
		if calls <= 2 {
			return errBusy
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err %v after %d calls", err, calls)
	}
	if len(*waits) != 2 || (*waits)[1] <= (*waits)[0] {
		t.Errorf("waited %v: two waits, the second longer", *waits)
	}
}

func TestAFileThatStaysInUseIsGivenUpOnAfterAShortWhileWithTheErrorItGave(t *testing.T) {
	waits := busyFor(t)
	calls := 0
	err := retryInUse("f", func() error { calls++; return errBusy })
	if !errors.Is(err, errBusy) {
		t.Errorf("err = %v, want the error it kept giving", err)
	}
	if calls != len(inUseWaits)+1 || len(*waits) != len(inUseWaits) {
		t.Errorf("%d calls and %d waits for %d allowed", calls, len(*waits), len(inUseWaits))
	}
	var total time.Duration
	for _, w := range *waits {
		total += w
	}
	if total < time.Second || total > 2*time.Second {
		t.Errorf("it would wait %s before giving up: long enough for a slow virus scanner (a second or more), short enough that a page action doesn't hang on a file (under two)", total)
	}
}

func TestAnyOtherErrorIsNotRetried(t *testing.T) {
	waits := busyFor(t)
	calls := 0
	other := errors.New("no such file")
	if err := retryInUse("f", func() error { calls++; return other }); !errors.Is(err, other) || calls != 1 || len(*waits) != 0 {
		t.Errorf("err %v, calls %d, waits %v", err, calls, *waits)
	}
}

func TestAFileWrittenWholeIsTriedAgainWhenTheReplacingFindsItInUse(t *testing.T) {
	busyFor(t)
	path := filepath.Join(t.TempDir(), "settings.yaml")
	os.WriteFile(path, []byte("old"), 0o644)
	failures := 2
	was := rename
	rename = func(from, to string) error {
		if failures > 0 {
			failures--
			return errBusy
		}
		return was(from, to)
	}
	defer func() { rename = was }()
	if err := WriteWholeMode(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Errorf("the file holds %q", data)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("left beside it: %v", entries)
	}
}

func TestAFileThatCannotBeReplacedStaysAsItWasWithNothingLeftBeside(t *testing.T) {
	busyFor(t)
	path := filepath.Join(t.TempDir(), "settings.yaml")
	os.WriteFile(path, []byte("old"), 0o644)
	was := rename
	rename = func(string, string) error { return errBusy }
	defer func() { rename = was }()
	if err := WriteWholeMode(path, []byte("new"), 0o644); !errors.Is(err, errBusy) {
		t.Errorf("err = %v, want the error to be said", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "old" {
		t.Errorf("the file holds %q", data)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("left beside it: %v", entries)
	}
}

func TestAFileReadIsTriedAgainWhenItIsInUse(t *testing.T) {
	busyFor(t)
	path := filepath.Join(t.TempDir(), "settings.yaml")
	os.WriteFile(path, []byte("held"), 0o644)
	failures := 2
	was := readFile
	readFile = func(p string) ([]byte, error) {
		if failures > 0 {
			failures--
			return nil, errBusy
		}
		return was(p)
	}
	defer func() { readFile = was }()
	if data, err := ReadFile(path); err != nil || string(data) != "held" {
		t.Errorf("%q, %v", data, err)
	}
}

func TestAFileThatIsNotThereIsNotWaitedFor(t *testing.T) {
	waits := busyFor(t)
	if _, err := ReadFile(filepath.Join(t.TempDir(), "nope")); !os.IsNotExist(err) || len(*waits) != 0 {
		t.Errorf("err %v, waits %v", err, *waits)
	}
}

// On Windows a file a reader has open can't be replaced: the first try fails,
// the write waits (the wait is the signal that it did), and goes through once
// the reader lets go. Where files are replaced under their readers it goes
// through at once and never waits. No race and no sleeping: the reader lets go
// when the writer says it is waiting.
func TestAFileAReaderHasOpenIsReplacedOnceTheReaderLetsGo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	waited := false
	wasPause := pause
	pause = func(time.Duration) {
		waited = true
		reader.Close()
	}
	defer func() { pause = wasPause }()
	if err := WriteWholeMode(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("the write failed with the reader still there: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Errorf("the file holds %q", data)
	}
	if runtime.GOOS == "windows" && !waited {
		t.Error("Windows replaced a file a reader had open without a wait: this test is not seeing the behaviour it is for")
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("left beside it: %v", entries)
	}
}

func TestAFileInUseForLongerThanHalfASecondIsSaidOnceAndAShortWaitIsNot(t *testing.T) {
	busyFor(t)
	var said []string
	was := OnLongWait
	OnLongWait = func(path string) { said = append(said, path) }
	defer func() { OnLongWait = was }()

	calls := 0
	retryInUse("quick.yaml", func() error {
		calls++
		if calls <= 4 {
			return errBusy
		}
		return nil
	})
	if len(said) != 0 {
		t.Errorf("a wait of 37ms was said: %v", said)
	}

	retryInUse("slow.yaml", func() error { return errBusy })
	if len(said) != 1 || said[0] != "slow.yaml" {
		t.Errorf("said %v, want slow.yaml once", said)
	}

	calls = 0
	retryInUse("freed.yaml", func() error {
		calls++
		if calls <= 8 {
			return errBusy
		}
		return nil
	})
	if len(said) != 2 || said[1] != "freed.yaml" {
		t.Errorf("a file freed after more than half a second wasn't said: %v", said)
	}
}

func TestTheWaitsRoughlyDoubleSoTheLastIsTheLongest(t *testing.T) {
	for i := 1; i < len(inUseWaits); i++ {
		if inUseWaits[i] <= inUseWaits[i-1] || inUseWaits[i] > 3*inUseWaits[i-1] {
			t.Errorf("wait %d (%s) after %s: not a back-off that roughly doubles", i, inUseWaits[i], inUseWaits[i-1])
		}
	}
}
