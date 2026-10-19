package packagereq

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake is an ecosystem that fetches nothing, and says what it was asked.
type fake struct {
	fetched []Request
	batches [][]Request
	failure error
	invalid error
	// failFor makes the requests with these ids fail, the rest succeeding.
	failFor map[string]error
}

func (f *fake) Validate(Request) error { return f.invalid }
func (f *fake) Fetch(_ context.Context, rs []Request) []Outcome {
	f.batches = append(f.batches, append([]Request(nil), rs...))
	out := make([]Outcome, len(rs))
	for i, r := range rs {
		f.fetched = append(f.fetched, r)
		switch {
		case f.failure != nil:
			out[i] = Outcome{"NuGet said: no", f.failure}
		case f.failFor[r.ID] != nil:
			out[i] = Outcome{"NuGet said: no to " + r.ID, f.failFor[r.ID]}
		default:
			out[i] = Outcome{Message: "fetched " + r.ID + " " + r.Version}
		}
	}
	return out
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
// under way at once, and which requests each took.
type gate struct {
	mu      sync.Mutex
	running int
	most    int
	started chan []string
	release chan struct{}
}

func newGate() *gate { return &gate{started: make(chan []string, 100), release: make(chan struct{})} }

func (g *gate) Validate(Request) error { return nil }
func (g *gate) Fetch(_ context.Context, rs []Request) []Outcome {
	g.mu.Lock()
	g.running++
	if g.running > g.most {
		g.most = g.running
	}
	g.mu.Unlock()
	var ids []string
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	g.started <- ids
	<-g.release
	g.mu.Lock()
	g.running--
	g.mu.Unlock()
	out := make([]Outcome, len(rs))
	for i, r := range rs {
		out[i] = Outcome{Message: "fetched " + r.ID}
	}
	return out
}

// awaitBatch is the requests of the next fetch to begin.
func (g *gate) awaitBatch(t *testing.T) []string {
	t.Helper()
	select {
	case ids := <-g.started:
		return ids
	case <-time.After(5 * time.Second):
		t.Fatal("no fetch began")
		return nil
	}
}

func (g *gate) noBatchWithin(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case ids := <-g.started:
		t.Fatalf("%v began, with the desk at its limit", ids)
	case <-time.After(d):
	}
}

func gatedDesk(t *testing.T, parallel int) (*Desk, *gate, Mailbox) {
	t.Helper()
	box := newMailbox(t)
	g := newGate()
	return &Desk{Mailbox: box, Ecosystems: map[string]Ecosystem{"nuget": g}, Parallel: parallel,
		Policy: func() string { return PolicyAllow }}, g, box
}

func askMany(t *testing.T, box Mailbox, n int) { askRange(t, box, 1, n) }

// askRange asks for Lib<from> to Lib<to>.
func askRange(t *testing.T, box Mailbox, from, to int) {
	t.Helper()
	for i := from; i <= to; i++ {
		drop(t, box, fmt.Sprintf("%d-a.json", i), fmt.Sprintf(`{"ecosystem":"nuget","id":"Lib%d","version":"1.0.0","framework":"net8.0"}`, i))
	}
}

// What's approved together is fetched together: one restore of all of it brings the same
// packages sooner than one each, and what NuGet fails on one doesn't stop the others.
func TestRequestsApprovedTogetherAreFetchedTogether(t *testing.T) {
	desk, g, box := gatedDesk(t, 3)
	askMany(t, box, 7)
	desk.Poll(context.Background())
	if got := g.awaitBatch(t); len(got) != 7 {
		t.Errorf("first fetch took %v, want all 7", got)
	}
	g.noBatchWithin(t, 50*time.Millisecond)
	g.release <- struct{}{}
	desk.Wait()
	for i := 1; i <= 7; i++ {
		if got := answerFor(t, box, fmt.Sprintf("%d-a", i)); !strings.HasPrefix(got, "fetched\n") {
			t.Errorf("request %d: %q", i, got)
		}
	}
}

// Requests that come while a fetch is under way are fetched together as the next, alongside
// it, so the agent that is waiting isn't held behind it; never more at once than the desk allows.
func TestWhatIsApprovedWhileAFetchIsUnderWayIsFetchedTogetherAlongsideItUpToTheLimit(t *testing.T) {
	desk, g, box := gatedDesk(t, 2)
	askRange(t, box, 1, 1)
	desk.Poll(context.Background())
	if got := g.awaitBatch(t); !reflect.DeepEqual(got, []string{"Lib1"}) {
		t.Fatalf("first fetch took %v", got)
	}
	askRange(t, box, 2, 3)
	desk.Poll(context.Background())
	if got := g.awaitBatch(t); !reflect.DeepEqual(got, []string{"Lib2", "Lib3"}) {
		t.Fatalf("second fetch took %v, want what came meanwhile together", got)
	}
	askRange(t, box, 4, 5)
	desk.Poll(context.Background())
	g.noBatchWithin(t, 50*time.Millisecond)
	g.release <- struct{}{}
	if got := g.awaitBatch(t); !reflect.DeepEqual(got, []string{"Lib4", "Lib5"}) {
		t.Fatalf("third fetch took %v, want it to begin when room came", got)
	}
	g.release <- struct{}{}
	g.release <- struct{}{}
	desk.Wait()
	if g.most != 2 {
		t.Errorf("at most %d at once, want 2", g.most)
	}
}

func TestEachRequestInAFetchedTogetherSetIsAnsweredWithItsOwnOutcome(t *testing.T) {
	rig := newDesk(t, "allow")
	rig.eco.failFor = map[string]error{"B.Lib": errors.New("no feed has it")}
	rig.ask(t, "1-a", "A.Lib", "1.0.0")
	rig.ask(t, "2-b", "B.Lib", "2.0.0")
	rig.ask(t, "3-c", "C.Lib", "3.0.0")
	rig.desk.Poll(context.Background())
	rig.desk.Wait()
	if len(rig.eco.batches) != 1 || len(rig.eco.batches[0]) != 3 {
		t.Fatalf("batches %v, want one of three", rig.eco.batches)
	}
	for n, want := range map[string]string{"1-a": "fetched\nfetched A.Lib 1.0.0", "2-b": "failed\nNuGet said: no to B.Lib", "3-c": "fetched\nfetched C.Lib 3.0.0"} {
		if got := rig.answer(t, n); !strings.HasPrefix(got, want) {
			t.Errorf("%s: %q, want %q", n, got, want)
		}
	}
	if len(rig.desk.Pending()) != 0 {
		t.Errorf("pending %v", rig.desk.Pending())
	}
}

// An ecosystem that answers for fewer requests than it was given has a bug; the
// requests it has no answer for aren't left waiting for ever.
func TestARequestAnEcosystemGivesNoOutcomeForIsAnsweredFailed(t *testing.T) {
	box := newMailbox(t)
	desk := &Desk{Mailbox: box, Ecosystems: map[string]Ecosystem{"nuget": shortChanging{}}, Policy: func() string { return PolicyAllow }}
	drop(t, box, "1-a.json", `{"ecosystem":"nuget","id":"A.Lib","version":"1.0.0","framework":"net8.0"}`)
	drop(t, box, "2-b.json", `{"ecosystem":"nuget","id":"B.Lib","version":"1.0.0","framework":"net8.0"}`)
	desk.Poll(context.Background())
	desk.Wait()
	if got := answerFor(t, box, "2-b"); !strings.HasPrefix(got, "failed\n") {
		t.Errorf("answer = %q", got)
	}
	if got := answerFor(t, box, "1-a"); !strings.HasPrefix(got, "fetched\n") {
		t.Errorf("answer = %q", got)
	}
}

// shortChanging answers for the first request only.
type shortChanging struct{}

func (shortChanging) Validate(Request) error { return nil }
func (shortChanging) Fetch(_ context.Context, rs []Request) []Outcome {
	return []Outcome{{Message: "fetched " + rs[0].ID}}
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
	g.awaitBatch(t)
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
	g.awaitBatch(t)
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

// A request to say what is waiting for the user is answered at once, from what the host knows; it
// is never held for approval, and no package is fetched for it.
func TestAStatusRequestIsAnsweredAtOnceWithWhatTheHostSaysIsWaiting(t *testing.T) {
	rig := newDesk(t, "ask")
	rig.desk.Overview = func() string { return "2 package requests are waiting for the user" }
	drop(t, rig.box, "1-a.json", `{"kind":"status"}`)
	rig.desk.Poll(context.Background())
	if got := rig.answer(t, "1-a"); got != "info\n2 package requests are waiting for the user\n" {
		t.Errorf("answer = %q", got)
	}
	if len(rig.desk.Pending()) != 0 || len(rig.eco.fetched) != 0 {
		t.Errorf("pending %v, fetched %v: a status request is neither held nor fetched", rig.desk.Pending(), rig.eco.fetched)
	}
}

func TestAStatusRequestIsAnsweredFailedWhereThereIsNoWayToSay(t *testing.T) {
	rig := newDesk(t, "ask")
	drop(t, rig.box, "1-a.json", `{"kind":"status"}`)
	rig.desk.Poll(context.Background())
	if got := rig.answer(t, "1-a"); !strings.HasPrefix(got, "failed\n") {
		t.Errorf("answer = %q", got)
	}
}

func TestAnUnavailableDeskSaysWhyToAStatusRequestToo(t *testing.T) {
	rig := newDesk(t, "ask")
	rig.desk.Unavailable = "the sandbox's folder holds vibe's own"
	rig.desk.Overview = func() string { return "all well" }
	drop(t, rig.box, "1-a.json", `{"kind":"status"}`)
	rig.desk.Poll(context.Background())
	if got := rig.answer(t, "1-a"); !strings.HasPrefix(got, "failed\n") || !strings.Contains(got, "holds vibe's own") {
		t.Errorf("answer = %q", got)
	}
}

// What isn't a kind of request the host knows isn't taken for a package request, with the rest of
// it ignored.
func TestARequestOfAnUnknownKindIsRefused(t *testing.T) {
	rig := newDesk(t, "allow")
	drop(t, rig.box, "1-a.json", `{"kind":"explode","ecosystem":"nuget","id":"A.Lib","version":"1.0.0","framework":"net8.0"}`)
	rig.desk.Poll(context.Background())
	rig.desk.Wait()
	if got := rig.answer(t, "1-a"); !strings.HasPrefix(got, "failed\n") || !strings.Contains(got, "explode") {
		t.Errorf("answer = %q", got)
	}
	if len(rig.eco.fetched) != 0 {
		t.Error("fetched for a request of a kind it doesn't know")
	}
}
