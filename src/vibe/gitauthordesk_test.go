package main

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// deskRig is a authorDesk's world.
type deskRig struct {
	finding    authorFinding
	preference string
	giveErr    error
	keepErr    error
	gate       chan struct{}
	entered    chan struct{}

	gave [][2]string
	kept []string
	said []string
	mu   sync.Mutex
	desk *authorDesk
}

func newDeskRig(finding authorFinding) *deskRig {
	r := &deskRig{finding: finding}
	r.desk = &authorDesk{
		assess:     func(preference string) authorFinding { r.preference = preference; return r.finding },
		preference: func() (string, error) { return r.preference, nil },
		push:       func() string { return aPushLine },
		give: func(name, email string) error {
			if r.gate != nil {
				r.entered <- struct{}{}
				<-r.gate
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			r.gave = append(r.gave, [2]string{name, email})
			return r.giveErr
		},
		keep: func(choice string) error { r.kept = append(r.kept, choice); return r.keepErr },
		say:  func(format string, a ...interface{}) { r.said = append(r.said, format) },
	}
	return r
}

var anOffer = authorFinding{Kind: authorOffer, GiveName: "H", GiveEmail: "h@x", Shown: "H <h@x>"}

func TestAMissingAuthorMakesTheDeskAskWithWhatItWouldGiveAndWhetherTheAgentCanPush(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	got := r.desk.State()
	if got.Phase != "ask" || got.Shown != "H <h@x>" || got.Push != aPushLine {
		t.Errorf("state = %+v", got)
	}
	if len(r.gave) != 0 {
		t.Errorf("gave %v before being told to", r.gave)
	}
}

func TestASandboxThatIsFineOrLeftOrUnreadableAsksNothing(t *testing.T) {
	for name, f := range map[string]authorFinding{
		"fine": {Kind: authorFine}, "left": {Kind: authorLeft}, "unknown": {Kind: authorUnknown, Why: "no sandbox"},
	} {
		r := newDeskRig(f)
		r.desk.Check()
		if got := r.desk.State(); got.Phase != "" {
			t.Errorf("%s: state = %+v", name, got)
		}
		if name == "unknown" && len(r.said) == 0 {
			t.Errorf("%s: nothing was said of why", name)
		}
	}
}

func TestAnImportPreferenceGivesTheAuthorAtOnceAndAsksNothing(t *testing.T) {
	r := newDeskRig(anOffer)
	r.preference = gitAuthorImport
	r.desk.Check()
	if got := r.desk.State(); got.Phase != "" {
		t.Errorf("state = %+v", got)
	}
	if len(r.gave) != 1 || r.gave[0] != [2]string{"H", "h@x"} {
		t.Errorf("gave %v", r.gave)
	}
	if len(r.kept) != 0 {
		t.Errorf("kept %v: the user chose this already", r.kept)
	}
}

func TestGivingKeepsTheChoiceAndEndsTheQuestion(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	if err := r.desk.Decide("give"); err != nil {
		t.Fatal(err)
	}
	if len(r.gave) != 1 || r.gave[0] != [2]string{"H", "h@x"} || strings.Join(r.kept, ",") != gitAuthorImport {
		t.Errorf("gave %v, kept %v", r.gave, r.kept)
	}
	if r.desk.State().Phase != "" {
		t.Errorf("state = %+v", r.desk.State())
	}
}

func TestAFailureToGiveIsReportedAndTheQuestionStays(t *testing.T) {
	r := newDeskRig(anOffer)
	r.giveErr = errors.New("no room")
	r.desk.Check()
	err := r.desk.Decide("give")
	if err == nil || !strings.Contains(err.Error(), "no room") {
		t.Errorf("err = %v", err)
	}
	if r.desk.State().Phase != "ask" || len(r.kept) != 0 {
		t.Errorf("state = %+v, kept %v", r.desk.State(), r.kept)
	}
}

func TestGivingThatCannotBeKeptStillGivesAndSaysSo(t *testing.T) {
	r := newDeskRig(anOffer)
	r.keepErr = errors.New("read-only")
	r.desk.Check()
	if err := r.desk.Decide("give"); err != nil {
		t.Errorf("err = %v: the agent was given its author", err)
	}
	if r.desk.State().Phase != "" || len(r.said) == 0 {
		t.Errorf("state = %+v, said %q", r.desk.State(), r.said)
	}
}

func TestLeavingKeepsTheChoiceAndGivesNothing(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	if err := r.desk.Decide("leave"); err != nil {
		t.Fatal(err)
	}
	if len(r.gave) != 0 || strings.Join(r.kept, ",") != gitAuthorLeave || r.desk.State().Phase != "" {
		t.Errorf("gave %v, kept %v, state %+v", r.gave, r.kept, r.desk.State())
	}
}

func TestALeaveThatCannotBeKeptIsReportedAndTheQuestionStays(t *testing.T) {
	r := newDeskRig(anOffer)
	r.keepErr = errors.New("read-only")
	r.desk.Check()
	if err := r.desk.Decide("leave"); err == nil {
		t.Error("no error")
	}
	if r.desk.State().Phase != "ask" {
		t.Errorf("state = %+v", r.desk.State())
	}
}

func TestLaterTurnsTheQuestionIntoAReminderThatCanBeDismissedOrReopened(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	if err := r.desk.Decide("later"); err != nil {
		t.Fatal(err)
	}
	if got := r.desk.State(); got.Phase != "reminder" || got.Shown == "" {
		t.Errorf("after later: %+v", got)
	}
	if err := r.desk.Decide("fix"); err != nil || r.desk.State().Phase != "ask" {
		t.Errorf("fix: %v, %+v", err, r.desk.State())
	}
	r.desk.Decide("later")
	if err := r.desk.Decide("dismiss"); err != nil || r.desk.State().Phase != "" {
		t.Errorf("dismiss: %v, %+v", err, r.desk.State())
	}
	if len(r.kept)+len(r.gave) != 0 {
		t.Errorf("kept %v, gave %v: neither is a decision", r.kept, r.gave)
	}
}

func TestADismissedReminderComesBackWhenTheSandboxIsStartedAgain(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	r.desk.Decide("later")
	r.desk.Decide("dismiss")
	r.desk.Check()
	if r.desk.State().Phase != "ask" {
		t.Errorf("state = %+v", r.desk.State())
	}
}

func TestAnswersThatMakeNoSenseAreRefused(t *testing.T) {
	r := newDeskRig(anOffer)
	if err := r.desk.Decide("give"); err == nil {
		t.Error("an answer to no question was taken")
	}
	r.desk.Check()
	for _, answer := range []string{"", "yes", "dismiss"} {
		if err := r.desk.Decide(answer); err == nil {
			t.Errorf("%q was taken while asking", answer)
		}
	}
	if len(r.gave)+len(r.kept) != 0 {
		t.Errorf("gave %v, kept %v", r.gave, r.kept)
	}
}

func TestTwoAnswersAtOnceGiveOnce(t *testing.T) {
	r := newDeskRig(anOffer)
	r.gate, r.entered = make(chan struct{}), make(chan struct{}, 1)
	r.desk.Check()
	first := make(chan error, 1)
	go func() { first <- r.desk.Decide("give") }()
	<-r.entered
	if err := r.desk.Decide("give"); err == nil {
		t.Error("a second answer was taken while the first was being carried out")
	}
	close(r.gate)
	if err := <-first; err != nil {
		t.Error(err)
	}
	if len(r.gave) != 1 {
		t.Errorf("gave %d times", len(r.gave))
	}
}
