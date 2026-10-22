package main

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// deskRig is a authorDesk's world.
type deskRig struct {
	finding authorFinding
	// pref is what the settings say; assessedWith is what Check passed to assess.
	pref         string
	assessedWith []string
	// assessHook, when set, is what assess answers with.
	assessHook func() authorFinding
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
		assess: func(preference string) authorFinding {
			r.assessedWith = append(r.assessedWith, preference)
			if r.assessHook != nil {
				return r.assessHook()
			}
			return r.finding
		},
		preference: func() (string, error) { return r.pref, nil },
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
		kept: func(choice string) string { return "kept: " + choice },
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
	r.pref = gitAuthorImport
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
	if _, err := r.desk.Decide("give"); err != nil {
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
	_, err := r.desk.Decide("give")
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
	if _, err := r.desk.Decide("give"); err != nil {
		t.Errorf("err = %v: the agent was given its author", err)
	}
	if r.desk.State().Phase != "" || len(r.said) == 0 {
		t.Errorf("state = %+v, said %q", r.desk.State(), r.said)
	}
}

func TestLeavingKeepsTheChoiceAndGivesNothing(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	if _, err := r.desk.Decide("leave"); err != nil {
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
	if _, err := r.desk.Decide("leave"); err == nil {
		t.Error("no error")
	}
	if r.desk.State().Phase != "ask" {
		t.Errorf("state = %+v", r.desk.State())
	}
}

func TestLaterTurnsTheQuestionIntoAReminderThatCanBeDismissedOrReopened(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	if _, err := r.desk.Decide("later"); err != nil {
		t.Fatal(err)
	}
	if got := r.desk.State(); got.Phase != "reminder" || got.Shown == "" {
		t.Errorf("after later: %+v", got)
	}
	if _, err := r.desk.Decide("fix"); err != nil || r.desk.State().Phase != "ask" {
		t.Errorf("fix: %v, %+v", err, r.desk.State())
	}
	r.desk.Decide("later")
	if _, err := r.desk.Decide("dismiss"); err != nil || r.desk.State().Phase != "" {
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
	if _, err := r.desk.Decide("give"); err == nil {
		t.Error("an answer to no question was taken")
	}
	r.desk.Check()
	for _, answer := range []string{"", "yes", "dismiss"} {
		if _, err := r.desk.Decide(answer); err == nil {
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
	go func() { first <- decideErr(r.desk, "give") }()
	<-r.entered
	if _, err := r.desk.Decide("give"); err == nil {
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

func TestCheckPassesTheSettingToAssess(t *testing.T) {
	r := newDeskRig(authorFinding{Kind: authorFine})
	r.pref = gitAuthorImport
	r.desk.Check()
	if len(r.assessedWith) != 1 || r.assessedWith[0] != gitAuthorImport {
		t.Errorf("assess was given %q", r.assessedWith)
	}
}

// slowCheck runs desk.Check with assess held until the returned release is
// called, and gives done, closed when the Check has ended. It returns once the
// Check is inside assess.
func slowCheck(r *deskRig, finding authorFinding) (release func(), done <-chan struct{}) {
	entered, gate, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	first := true
	var mu sync.Mutex
	r.assessHook = func() authorFinding {
		mu.Lock()
		mine := first
		first = false
		mu.Unlock()
		if mine {
			close(entered)
			<-gate
			return finding
		}
		return r.finding
	}
	go func() { r.desk.Check(); close(finished) }()
	<-entered
	return func() { close(gate) }, finished
}

func TestACheckThatEndsAfterAnAnswerDoesNotAskAgain(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	release, done := slowCheck(r, anOffer)
	if _, err := r.desk.Decide("give"); err != nil {
		t.Fatal(err)
	}
	release()
	<-done
	if got := r.desk.State(); got.Phase != "" {
		t.Errorf("state = %+v: a question already answered was asked again", got)
	}
}

func TestACheckThatEndsAfterLaterDoesNotTurnTheReminderBackIntoTheQuestion(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	release, done := slowCheck(r, anOffer)
	r.desk.Decide("later")
	release()
	<-done
	if got := r.desk.State(); got.Phase != "reminder" {
		t.Errorf("state = %+v", got)
	}
}

func TestAnOlderCheckThatEndsAfterANewerOneIsIgnored(t *testing.T) {
	r := newDeskRig(anOffer)
	release, done := slowCheck(r, anOffer)
	// The newer one finds the author given in the meantime.
	r.finding = authorFinding{Kind: authorFine}
	r.desk.Check()
	release()
	<-done
	if got := r.desk.State(); got.Phase != "" {
		t.Errorf("state = %+v: what the older Check saw replaced what the newer did", got)
	}
}

func TestACheckThatSawTheAuthorMissingWhileItWasBeingGivenDoesNotAskAfter(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	r.gate, r.entered = make(chan struct{}), make(chan struct{}, 1)
	answered := make(chan error, 1)
	go func() { answered <- decideErr(r.desk, "give") }()
	<-r.entered // inside give: the sandbox still has no author
	release, done := slowCheck(r, anOffer)
	close(r.gate)
	if err := <-answered; err != nil {
		t.Fatal(err)
	}
	release()
	<-done
	if got := r.desk.State(); got.Phase != "" {
		t.Errorf("state = %+v", got)
	}
}

func TestACheckThatEndsWhileAnAnswerIsBeingCarriedOutDoesNotDisturbIt(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	r.gate, r.entered = make(chan struct{}), make(chan struct{}, 1)
	answered := make(chan error, 1)
	go func() { answered <- decideErr(r.desk, "give") }()
	<-r.entered
	r.desk.Check() // sees the author missing, as give hasn't finished
	close(r.gate)
	if err := <-answered; err != nil {
		t.Fatal(err)
	}
	if got := r.desk.State(); got.Phase != "" {
		t.Errorf("state = %+v", got)
	}
}

func decideErr(d *authorDesk, answer string) error {
	_, err := d.Decide(answer)
	return err
}

func TestAnAnswerThatIsKeptSaysSoAndHowToUndoItToTheCaller(t *testing.T) {
	for answer, want := range map[string]string{"give": "kept: import", "leave": "kept: leave"} {
		r := newDeskRig(anOffer)
		r.desk.Check()
		got, err := r.desk.Decide(answer)
		if err != nil || got != want {
			t.Errorf("%s: %q, %v; want %q", answer, got, err, want)
		}
	}
}

func TestNothingIsSaidOfKeepingWhenNothingWasKept(t *testing.T) {
	r := newDeskRig(anOffer)
	r.desk.Check()
	for _, answer := range []string{"later", "fix", "later", "dismiss"} {
		if got, err := r.desk.Decide(answer); err != nil || got != "" {
			t.Errorf("%s: %q, %v", answer, got, err)
		}
	}
	r = newDeskRig(anOffer)
	r.keepErr = errors.New("read-only")
	r.desk.Check()
	if got, _ := r.desk.Decide("give"); got != "" {
		t.Errorf("a choice that couldn't be kept was said to be: %q", got)
	}
	r = newDeskRig(anOffer)
	r.giveErr = errors.New("no room")
	r.desk.Check()
	if got, _ := r.desk.Decide("give"); got != "" {
		t.Errorf("a failure was said to have kept: %q", got)
	}
}
