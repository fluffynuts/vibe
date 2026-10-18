package packagereq

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
