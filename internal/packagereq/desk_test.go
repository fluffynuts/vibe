package packagereq

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake is an ecosystem that fetches nothing, and says what it was asked.
type fake struct {
	fetched []Request
	failure error
	invalid error
}

func (f *fake) Validate(Request) error { return f.invalid }
func (f *fake) Fetch(_ context.Context, r Request) (string, error) {
	f.fetched = append(f.fetched, r)
	if f.failure != nil {
		return "NuGet said: no", f.failure
	}
	return "fetched " + r.ID + " " + r.Version, nil
}

type deskRig struct {
	desk *Desk
	box  Mailbox
	eco  *fake
	log  []string
}

func newDesk(t *testing.T, policy string) *deskRig {
	t.Helper()
	box := newMailbox(t)
	rig := &deskRig{box: box, eco: &fake{}}
	rig.desk = &Desk{
		Mailbox:    box,
		Ecosystems: map[string]Ecosystem{"nuget": rig.eco},
		Policy:     func() string { return policy },
		Log:        func(line string) { rig.log = append(rig.log, line) },
	}
	return rig
}

func (r *deskRig) ask(t *testing.T, nonce, id, version string) {
	t.Helper()
	drop(t, r.box, nonce+".json", `{"ecosystem":"nuget","id":"`+id+`","version":"`+version+`","framework":"net8.0"}`)
}

func (r *deskRig) answer(t *testing.T, nonce string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.box.View(), nonce+".answer"))
	if err != nil {
		t.Fatalf("no answer for %s: %v", nonce, err)
	}
	return string(data)
}

func TestAllowFetchesAtOnceAndAnswersFetched(t *testing.T) {
	rig := newDesk(t, "allow")
	rig.ask(t, "1-a", "Corp.Lib", "1.2.3")
	rig.desk.Poll(context.Background())
	rig.desk.Wait()
	if got := rig.answer(t, "1-a"); got != "fetched\nfetched Corp.Lib 1.2.3\n" {
		t.Errorf("answer = %q", got)
	}
	if len(rig.eco.fetched) != 1 || len(rig.desk.Pending()) != 0 {
		t.Errorf("fetched %v, pending %v", rig.eco.fetched, rig.desk.Pending())
	}
	if len(rig.log) == 0 || !strings.Contains(strings.Join(rig.log, "\n"), "Corp.Lib 1.2.3") {
		t.Errorf("every fetch is said in vibe's output: %v", rig.log)
	}
}

func TestAskHoldsTheRequestForTheUserAndTellsTheSandboxItIsWaiting(t *testing.T) {
	rig := newDesk(t, "ask")
	rig.ask(t, "1-a", "Corp.Lib", "1.2.3")
	rig.desk.Poll(context.Background())
	if got := rig.answer(t, "1-a"); !strings.HasPrefix(got, "waiting\n") || !strings.Contains(got, "Admin") {
		t.Errorf("answer = %q, want waiting, and where to approve", got)
	}
	if len(rig.eco.fetched) != 0 {
		t.Error("fetched before the user approved")
	}
	pending := rig.desk.Pending()
	if len(pending) != 1 || pending[0].Request.ID != "Corp.Lib" {
		t.Errorf("pending = %+v", pending)
	}
}

func TestApprovingFetchesOnlyWhatWasApprovedAtThePolling(t *testing.T) {
	rig := newDesk(t, "ask")
	rig.ask(t, "1-a", "A.Lib", "1.0.0")
	rig.ask(t, "2-b", "B.Lib", "2.0.0")
	rig.desk.Poll(context.Background())
	var keep, take string
	for _, p := range rig.desk.Pending() {
		if p.Request.ID == "A.Lib" {
			take = p.Key
		} else {
			keep = p.Key
		}
	}
	rig.desk.Allow([]string{take})
	rig.desk.Poll(context.Background())
	rig.desk.Wait()
	if len(rig.eco.fetched) != 1 || rig.eco.fetched[0].ID != "A.Lib" {
		t.Errorf("fetched %v, want only A.Lib", rig.eco.fetched)
	}
	if !strings.HasPrefix(rig.answer(t, "1-a"), "fetched\n") || !strings.HasPrefix(rig.answer(t, "2-b"), "waiting\n") {
		t.Error("the approved is answered, the other still waits")
	}
	if p := rig.desk.Pending(); len(p) != 1 || p[0].Key != keep {
		t.Errorf("pending = %+v", p)
	}
}

func TestDenyAnswersDeniedAndFetchesNothing(t *testing.T) {
	rig := newDesk(t, "ask")
	rig.ask(t, "1-a", "A.Lib", "1.0.0")
	rig.ask(t, "2-b", "B.Lib", "2.0.0")
	rig.desk.Poll(context.Background())
	rig.desk.DenyAll()
	for _, n := range []string{"1-a", "2-b"} {
		if got := rig.answer(t, n); !strings.HasPrefix(got, "denied\n") {
			t.Errorf("%s: %q", n, got)
		}
	}
	if len(rig.eco.fetched) != 0 || len(rig.desk.Pending()) != 0 {
		t.Errorf("fetched %v, pending %v", rig.eco.fetched, rig.desk.Pending())
	}
}

func TestTheSameRequestAskedTwiceIsFetchedOnceAndBothAreAnswered(t *testing.T) {
	rig := newDesk(t, "ask")
	rig.ask(t, "1-a", "A.Lib", "1.0.0")
	rig.desk.Poll(context.Background())
	rig.ask(t, "2-a", "A.Lib", "1.0.0")
	rig.desk.Poll(context.Background())
	if p := rig.desk.Pending(); len(p) != 1 {
		t.Fatalf("pending = %+v, want the one", p)
	}
	rig.desk.Allow([]string{rig.desk.Pending()[0].Key})
	rig.desk.Poll(context.Background())
	rig.desk.Wait()
	if len(rig.eco.fetched) != 1 {
		t.Errorf("fetched %d times", len(rig.eco.fetched))
	}
	for _, n := range []string{"1-a", "2-a"} {
		if !strings.HasPrefix(rig.answer(t, n), "fetched\n") {
			t.Errorf("%s not answered fetched: %q", n, rig.answer(t, n))
		}
	}
}

func TestAFailedFetchAnswersFailedInTheEcosystemsWords(t *testing.T) {
	rig := newDesk(t, "allow")
	rig.eco.failure = errors.New("exit status 1")
	rig.ask(t, "1-a", "A.Lib", "1.0.0")
	rig.desk.Poll(context.Background())
	rig.desk.Wait()
	got := rig.answer(t, "1-a")
	if !strings.HasPrefix(got, "failed\n") || !strings.Contains(got, "NuGet said: no") {
		t.Errorf("answer = %q", got)
	}
}

func TestARequestThatIsNotValidIsAnsweredFailedAndNeverFetchedOrHeld(t *testing.T) {
	rig := newDesk(t, "allow")
	rig.eco.invalid = errors.New("version \"1.*\" is not one exact version")
	rig.ask(t, "1-a", "A.Lib", "1.*")
	drop(t, rig.box, "2-x.json", `{"ecosystem":"cargo","id":"a","version":"1"}`)
	drop(t, rig.box, "3-y.json", `not json`)
	rig.desk.Poll(context.Background())
	rig.desk.Wait()
	for n, want := range map[string]string{"1-a": "exact version", "2-x": "cargo", "3-y": "isn't a request"} {
		if got := rig.answer(t, n); !strings.HasPrefix(got, "failed\n") || !strings.Contains(got, want) {
			t.Errorf("%s: %q, want failed, saying %q", n, got, want)
		}
	}
	if len(rig.eco.fetched) != 0 || len(rig.desk.Pending()) != 0 {
		t.Errorf("fetched %v, pending %v", rig.eco.fetched, rig.desk.Pending())
	}
}

func TestAnUnavailableDeskSaysWhyToEveryRequest(t *testing.T) {
	rig := newDesk(t, "allow")
	rig.desk.Unavailable = "the sandbox's folder holds vibe's own"
	rig.ask(t, "1-a", "A.Lib", "1.0.0")
	rig.desk.Poll(context.Background())
	rig.desk.Wait()
	if got := rig.answer(t, "1-a"); !strings.HasPrefix(got, "failed\n") || !strings.Contains(got, "holds vibe's own") {
		t.Errorf("answer = %q", got)
	}
	if len(rig.eco.fetched) != 0 {
		t.Error("fetched anyway")
	}
}

func TestAHeldRequestCarriesWhatTheDeskSaysOfIt(t *testing.T) {
	rig := newDesk(t, "ask")
	rig.desk.Describe = func(r Request) string { return "an upgrade from 1.0.0 to " + r.Version }
	rig.ask(t, "1-a", "A.Lib", "2.0.0")
	rig.desk.Poll(context.Background())
	if p := rig.desk.Pending(); len(p) != 1 || p[0].Note != "an upgrade from 1.0.0 to 2.0.0" {
		t.Errorf("pending = %+v", p)
	}
}

// gate is an ecosystem whose fetches wait to be let go, saying how many are
// under way at once.
type gate struct {
	mu      sync.Mutex
	running int
	most    int
	started chan string
	release chan struct{}
}

func newGate() *gate { return &gate{started: make(chan string, 100), release: make(chan struct{})} }

func (g *gate) Validate(Request) error { return nil }
func (g *gate) Fetch(_ context.Context, r Request) (string, error) {
	g.mu.Lock()
	g.running++
	if g.running > g.most {
		g.most = g.running
	}
	g.mu.Unlock()
	g.started <- r.ID
	<-g.release
	g.mu.Lock()
	g.running--
	g.mu.Unlock()
	return "fetched " + r.ID, nil
}

func (g *gate) awaitStarted(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-g.started:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d fetches began; wanted %d", i, n)
		}
	}
}

func gatedDesk(t *testing.T, parallel int) (*Desk, *gate, Mailbox) {
	t.Helper()
	box := newMailbox(t)
	g := newGate()
	return &Desk{Mailbox: box, Ecosystems: map[string]Ecosystem{"nuget": g}, Parallel: parallel,
		Policy: func() string { return PolicyAllow }}, g, box
}

func askMany(t *testing.T, box Mailbox, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		drop(t, box, fmt.Sprintf("%d-a.json", i+1), fmt.Sprintf(`{"ecosystem":"nuget","id":"Lib%d","version":"1.0.0","framework":"net8.0"}`, i+1))
	}
}

func TestSeveralFetchesRunAtOnceButNeverMoreThanTheDeskAllows(t *testing.T) {
	desk, g, box := gatedDesk(t, 3)
	askMany(t, box, 7)
	desk.Poll(context.Background())
	g.awaitStarted(t, 3)
	select {
	case id := <-g.started:
		t.Fatalf("%s began as a fourth fetch at once", id)
	case <-time.After(50 * time.Millisecond):
	}
	// Each one finished starts the next: no Poll needed.
	for done := 0; done < 7; done++ {
		g.release <- struct{}{}
		if done+3 < 7 {
			g.awaitStarted(t, 1)
		}
	}
	desk.Wait()
	if g.most != 3 {
		t.Errorf("at most %d at once, want 3", g.most)
	}
	for i := 1; i <= 7; i++ {
		if got := answerFor(t, box, fmt.Sprintf("%d-a", i)); !strings.HasPrefix(got, "fetched\n") {
			t.Errorf("request %d: %q", i, got)
		}
	}
}

func TestPollDoesNotWaitForTheFetchesItStarts(t *testing.T) {
	desk, g, box := gatedDesk(t, 2)
	askMany(t, box, 1)
	returned := make(chan struct{})
	go func() { desk.Poll(context.Background()); close(returned) }()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Poll waited on a fetch: requests would go untaken meanwhile")
	}
	g.awaitStarted(t, 1)
	close(g.release)
	desk.Wait()
}

func TestWhatIsApprovedIsSaidAsSoonAsItIsApprovedNotWhenItsFetchBegins(t *testing.T) {
	rig := newDesk(t, "ask")
	rig.ask(t, "1-a", "A.Lib", "1.0.0")
	rig.ask(t, "2-b", "B.Lib", "2.0.0")
	rig.desk.Poll(context.Background())
	for _, p := range rig.desk.Pending() {
		if p.Approved || p.Fetching {
			t.Errorf("%+v is neither approved nor fetching yet", p)
		}
	}
	rig.desk.Allow([]string{rig.desk.Pending()[0].Key})
	approved := 0
	for _, p := range rig.desk.Pending() {
		if p.Approved {
			approved++
		}
	}
	if approved != 1 {
		t.Errorf("%d approved, want 1", approved)
	}
}

func TestWithAllowEveryRequestIsApprovedAtOnce(t *testing.T) {
	desk, g, box := gatedDesk(t, 1)
	askMany(t, box, 2)
	desk.Poll(context.Background())
	g.awaitStarted(t, 1)
	for _, p := range desk.Pending() {
		if !p.Approved {
			t.Errorf("%+v waits for approval under allow", p)
		}
	}
	close(g.release)
	desk.Wait()
}

func answerFor(t *testing.T, box Mailbox, nonce string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(box.View(), nonce+".answer"))
	if err != nil {
		t.Fatalf("no answer for %s: %v", nonce, err)
	}
	return string(data)
}

func TestDenyLeavesWhatIsAlreadyApproved(t *testing.T) {
	rig := newDesk(t, "ask")
	rig.ask(t, "1-a", "A.Lib", "1.0.0")
	rig.ask(t, "2-b", "B.Lib", "2.0.0")
	rig.desk.Poll(context.Background())
	rig.desk.Allow([]string{rig.desk.Pending()[0].Key})
	rig.desk.DenyAll()
	if pending := rig.desk.Pending(); len(pending) != 1 || !pending[0].Approved {
		t.Fatalf("pending = %+v, want the approved one still there", pending)
	}
	rig.desk.Poll(context.Background())
	rig.desk.Wait()
	if !strings.HasPrefix(rig.answer(t, "1-a"), "fetched\n") || !strings.HasPrefix(rig.answer(t, "2-b"), "denied\n") {
		t.Errorf("1-a %q, 2-b %q", rig.answer(t, "1-a"), rig.answer(t, "2-b"))
	}
}
