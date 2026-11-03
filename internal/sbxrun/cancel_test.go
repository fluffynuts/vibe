package sbxrun

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"vibe/internal/sbxtest"
)

// hangingSbx puts an sbx on PATH that hangs, as a stuck sbx would.
func hangingSbx(t *testing.T) {
	sbxtest.OnPath(t, "exec sleep 30\n")
}

func TestCancellingEndsAStuckSbx(t *testing.T) {
	hangingSbx(t)
	for name, call := range map[string]func(context.Context) error{
		"stop":   func(ctx context.Context) error { return StopQuiet(ctx, "box") },
		"rm":     func(ctx context.Context) error { return Remove(ctx, "box", true, nil) },
		"create": func(ctx context.Context) error { return Create(ctx, CreateOpts{Name: "box"}) },
		"start": func(ctx context.Context) error {
			if Start(ctx, "box") {
				t.Error("start: a sandbox that never came up was reported up")
			}
			return ctx.Err()
		},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		began := time.Now()
		time.AfterFunc(100*time.Millisecond, cancel)
		err := call(ctx)
		if took := time.Since(began); took > 5*time.Second {
			t.Errorf("%s: took %s after being cancelled", name, took)
		}
		if err == nil {
			t.Errorf("%s: no error from a cancelled call", name)
		}
		cancel()
	}
}

// askingSbx puts an sbx on PATH that asks a question and waits for the
// answer, as 'sbx create' does for a workspace folder that's gone.
func askingSbx(t *testing.T) {
	sbxtest.OnPath(t, "echo 'Would you like to create it? (y/N)'\nread answer || exit 1\n[ \"$answer\" = y ]\n")
}

// With nobody to answer, a question from sbx fails the create at once,
// rather than waiting on a terminal for good.
func TestACreateWithNoInputFailsAtAQuestionInsteadOfWaiting(t *testing.T) {
	askingSbx(t)
	// A terminal nobody types into: reading it never ends.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	defer r.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = Create(ctx, CreateOpts{Name: "box", NoInput: true})
	if ctx.Err() != nil {
		t.Fatal("the create waited for an answer nobody could give")
	}
	if err == nil {
		t.Error("the create succeeded, though sbx's question went unanswered")
	}
}

// chattySbx puts an sbx on PATH that prints its arguments, as sbx create
// and rm print their progress.
func chattySbx(t *testing.T) {
	sbxtest.OnPath(t, "echo \"sbx $*\"\necho 'to stderr' >&2\n")
}

// What sbx prints goes where it's told: a page action's log rather than the
// terminal, which a session's agent is using. It's asked for plain lines,
// not progress redrawn in place.
func TestCreateAndRemoveCanSayWhatTheyDoSomewhereElse(t *testing.T) {
	chattySbx(t)
	var create, remove bytes.Buffer
	if err := Create(context.Background(), CreateOpts{Name: "box", Output: &create}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(create.String(), "sbx create --name box") || !strings.Contains(create.String(), "--no-progress") || !strings.Contains(create.String(), "to stderr") {
		t.Errorf("create's output = %q", create.String())
	}
	if err := Remove(context.Background(), "box", true, &remove); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(remove.String(), "sbx rm -f box") {
		t.Errorf("rm's output = %q", remove.String())
	}
}

// A sandbox that takes an exec and never answers (its /bin/true replaced,
// say) is not reachable: the probe has a backstop of its own, so neither
// it nor a wait made of probes waits for good.
func TestAReachabilityProbeThatNeverAnswersIsNo(t *testing.T) {
	hangingSbx(t)
	old := probeTimeout
	probeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { probeTimeout = old })
	began := time.Now()
	if Reachable(context.Background(), "box") {
		t.Error("a sandbox that never answered was reported reachable")
	}
	if WaitReachable(context.Background(), "box", 2) {
		t.Error("a sandbox that never answered was waited for and reported reachable")
	}
	if took := time.Since(began); took > 10*time.Second {
		t.Errorf("took %s", took)
	}
}

// A create that takes minutes says so on the way: a page action's log would otherwise go quiet while sbx works
// (with its progress off, it prints nothing between starting and finishing).
func TestACreateThatTakesAWhileSaysItIsStillWorkingAndStopsWhenItEnds(t *testing.T) {
	sbxtest.OnPath(t, "sleep 1\n")
	old := stillWorkingEvery
	stillWorkingEvery = 100 * time.Millisecond
	t.Cleanup(func() { stillWorkingEvery = old })
	var mu sync.Mutex
	var elapsed []time.Duration
	var ended, late bool
	err := Create(context.Background(), CreateOpts{Name: "box", Output: io.Discard, StillWorking: func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		elapsed = append(elapsed, d)
		late = late || ended
	}})
	mu.Lock()
	ended = true
	got := append([]time.Duration(nil), elapsed...)
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 {
		t.Fatalf("it said it was working %d times in a second, at every 100ms: %v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Errorf("the elapsed time didn't grow: %v", got)
		}
	}
	// Create doesn't return until its ticker has stopped, so nothing can be said after it.
	mu.Lock()
	defer mu.Unlock()
	if late || len(elapsed) != len(got) {
		t.Errorf("it said it was working after the create ended: %v", elapsed)
	}
}
