package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"vibe/internal/cliargs"
	"vibe/internal/profilegen"
	"vibe/internal/state"
)

// identityRig is the world checkAgentIdentity asks: the sandbox's git
// author, the host's, and what it's said and done.
type identityRig struct {
	sandboxName, sandboxEmail string
	sandboxErr                error
	hostName, hostEmail       string
	answer                    bool
	setErr                    error

	sandboxAsked bool
	asked        []string
	said         []string
	set          [][2]string
}

func (r *identityRig) deps() identityDeps {
	return identityDeps{
		sandbox: func() (string, string, error) {
			r.sandboxAsked = true
			return r.sandboxName, r.sandboxEmail, r.sandboxErr
		},
		host: func() (string, string) { return r.hostName, r.hostEmail },
		set: func(name, email string) error {
			r.set = append(r.set, [2]string{name, email})
			return r.setErr
		},
		ask: func(q string) bool { r.asked = append(r.asked, q); return r.answer },
		say: func(format string, a ...interface{}) {
			r.said = append(r.said, strings.TrimSpace(fmt.Sprintf(format, a...)))
		},
	}
}

const aPushLine = "It has no GitHub token, so it can't push."

func TestASandboxWithAnAuthorIsLeftAlone(t *testing.T) {
	r := &identityRig{sandboxName: "A", sandboxEmail: "a@x", hostName: "H", hostEmail: "h@x"}
	if got := checkAgentIdentity(r.deps(), "", aPushLine); got != identityFine {
		t.Errorf("got %v", got)
	}
	if len(r.asked)+len(r.said)+len(r.set) != 0 {
		t.Errorf("asked %q, said %q, set %v", r.asked, r.said, r.set)
	}
}

func TestASandboxTheUserSaidToLeaveIsNotLookedAtOrAskedAbout(t *testing.T) {
	r := &identityRig{hostName: "H", hostEmail: "h@x"}
	if got := checkAgentIdentity(r.deps(), gitAuthorLeave, aPushLine); got != identityDeclined {
		t.Errorf("got %v", got)
	}
	if r.sandboxAsked || len(r.asked)+len(r.said) != 0 {
		t.Errorf("the sandbox was asked: %v, said %q, asked %q", r.sandboxAsked, r.said, r.asked)
	}
}

func TestWithNoAuthorTheUserIsAskedWithWhatWouldBeSetAndWhetherTheAgentCanPush(t *testing.T) {
	r := &identityRig{hostName: "Davyd McColl", hostEmail: "d@x.com", answer: true}
	if got := checkAgentIdentity(r.deps(), "", aPushLine); got != identityGiven {
		t.Errorf("got %v", got)
	}
	if len(r.asked) != 1 {
		t.Fatalf("asked %q", r.asked)
	}
	for _, want := range []string{"Davyd McColl", "d@x.com"} {
		if !strings.Contains(r.asked[0], want) {
			t.Errorf("the question lacks %q: %s", want, r.asked[0])
		}
	}
	said := strings.Join(r.said, "\n")
	for _, want := range []string{"can't commit", aPushLine} {
		if !strings.Contains(said, want) {
			t.Errorf("what was said lacks %q: %s", want, said)
		}
	}
	if len(r.set) != 1 || r.set[0] != [2]string{"Davyd McColl", "d@x.com"} {
		t.Errorf("set %v", r.set)
	}
}

func TestRefusingChangesNothingAndIsRemembered(t *testing.T) {
	r := &identityRig{hostName: "H", hostEmail: "h@x", answer: false}
	if got := checkAgentIdentity(r.deps(), "", aPushLine); got != identityDeclined {
		t.Errorf("got %v", got)
	}
	if len(r.set) != 0 {
		t.Errorf("set %v", r.set)
	}
	if !strings.Contains(strings.Join(r.said, "\n"), "git config --global user.name") {
		t.Errorf("it doesn't say how to do it later: %q", r.said)
	}
}

func TestOnlyWhatIsMissingIsSet(t *testing.T) {
	r := &identityRig{sandboxName: "Mine", hostName: "H", hostEmail: "h@x", answer: true}
	checkAgentIdentity(r.deps(), "", aPushLine)
	if len(r.set) != 1 || r.set[0] != [2]string{"", "h@x"} {
		t.Errorf("set %v: the name the sandbox has is the user's to keep", r.set)
	}
}

func TestWhenTheHostHasNoAuthorEitherThereIsNothingToOfferAndTheUserIsToldWhatToDo(t *testing.T) {
	r := &identityRig{}
	if got := checkAgentIdentity(r.deps(), "", aPushLine); got != identityUnknown {
		t.Errorf("got %v", got)
	}
	if len(r.asked)+len(r.set) != 0 {
		t.Errorf("asked %q, set %v", r.asked, r.set)
	}
	said := strings.Join(r.said, "\n")
	if !strings.Contains(said, "can't commit") || !strings.Contains(said, "git config --global user.name") {
		t.Errorf("said %q", said)
	}
}

func TestAHostAuthorThatIsNotAPlainLineIsNotOffered(t *testing.T) {
	r := &identityRig{hostName: "H\nevil", hostEmail: "h@x", answer: true}
	checkAgentIdentity(r.deps(), "", aPushLine)
	if len(r.set) != 0 {
		t.Errorf("set %v from a name with a line break", r.set)
	}
}

func TestASandboxThatCannotBeAskedIsSaidNotGuessedAt(t *testing.T) {
	r := &identityRig{sandboxErr: errors.New("boom"), hostName: "H", hostEmail: "h@x", answer: true}
	if got := checkAgentIdentity(r.deps(), "", aPushLine); got != identityUnknown {
		t.Errorf("got %v", got)
	}
	if len(r.asked)+len(r.set) != 0 || !strings.Contains(strings.Join(r.said, "\n"), "boom") {
		t.Errorf("asked %q, set %v, said %q", r.asked, r.set, r.said)
	}
}

func TestAFailureToSetItIsSaidAndNotRemembered(t *testing.T) {
	r := &identityRig{hostName: "H", hostEmail: "h@x", answer: true, setErr: errors.New("no room")}
	if got := checkAgentIdentity(r.deps(), "", aPushLine); got != identityUnknown {
		t.Errorf("got %v", got)
	}
	if !strings.Contains(strings.Join(r.said, "\n"), "no room") {
		t.Errorf("said %q", r.said)
	}
}

func TestWhatThePushLineSays(t *testing.T) {
	none := pushStatement(state.Instance{})
	if !strings.Contains(none, "can't push") {
		t.Errorf("no token: %s", none)
	}
	with := pushStatement(state.Instance{GitToken: "own", GitRules: []string{"branch-only"}})
	if !strings.Contains(with, "can push") || strings.Contains(with, "can't push") {
		t.Errorf("a token: %s", with)
	}
}

func TestLoadSettingsRefusesAnUnknownGitAuthorChoice(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), "gitAuthor: sometimes\n")
	_, err := loadSettings(lay, "p")
	if err == nil || !strings.Contains(err.Error(), "sometimes") || !strings.Contains(err.Error(), "import") || !strings.Contains(err.Error(), "leave") {
		t.Errorf("want an error naming the value and the choices, got %v", err)
	}
}

func TestTheChoiceIsKeptInItsOwnFileInTheProfileAndLeavesSettingsAlone(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), "# mine\nmemory: 8g\n")
	if err := keepGitAuthorChoice(lay, "p", gitAuthorImport); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(lay.HomePath("profiles", "p", profilegen.GitAuthorFile)); strings.TrimSpace(string(data)) != gitAuthorImport {
		t.Errorf("the file holds %q", data)
	}
	if data, _ := os.ReadFile(lay.HomePath("profiles", "p", "settings.yaml")); string(data) != "# mine\nmemory: 8g\n" {
		t.Errorf("settings.yaml was touched: %q", data)
	}
	if got, err := gitAuthorPreference(lay, "p"); err != nil || got != gitAuthorImport {
		t.Errorf("read back %q, %v", got, err)
	}
}

func TestAChoiceWrittenInSettingsBeatsTheAnswerKept(t *testing.T) {
	lay := updateLayout(t)
	keepGitAuthorChoice(lay, "p", gitAuthorImport)
	writeFile(t, lay.HomePath("settings.yaml"), "gitAuthor: leave\n")
	if got, _ := gitAuthorPreference(lay, "p"); got != gitAuthorLeave {
		t.Errorf("preference %q: what the user wrote should win over what they answered once", got)
	}
}

func TestNoChoiceAnywhereIsToAsk(t *testing.T) {
	lay := updateLayout(t)
	if got, err := gitAuthorPreference(lay, "p"); err != nil || got != "" {
		t.Errorf("preference %q, %v", got, err)
	}
}

func TestAnAnswerFileThatIsNotAChoiceIsSaidNotAsked(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "p", profilegen.GitAuthorFile), "maybe\n")
	if _, err := gitAuthorPreference(lay, "p"); err == nil {
		t.Error("an answer that isn't one was read as a choice")
	}
}

func TestAProfileThatIsNotThereIsNotMadeToKeepTheChoice(t *testing.T) {
	lay := updateLayout(t)
	if err := keepGitAuthorChoice(lay, "nope", gitAuthorLeave); err == nil {
		t.Error("a profile was made for it")
	}
	if _, err := os.Stat(lay.HomePath("profiles", "nope")); err == nil {
		t.Error("a folder was made")
	}
}

func TestAnImportChoiceGivesTheAuthorWithoutAskingAndSaysSo(t *testing.T) {
	r := &identityRig{hostName: "H", hostEmail: "h@x", answer: false}
	if got := checkAgentIdentity(r.deps(), gitAuthorImport, aPushLine); got != identityGiven {
		t.Errorf("got %v", got)
	}
	if len(r.asked) != 0 || len(r.set) != 1 || r.set[0] != [2]string{"H", "h@x"} {
		t.Errorf("asked %q, set %v", r.asked, r.set)
	}
	if !strings.Contains(strings.Join(r.said, "\n"), "H <h@x>") {
		t.Errorf("said %q", r.said)
	}
}

func TestAnImportChoiceLeavesASandboxThatHasAnAuthorAlone(t *testing.T) {
	r := &identityRig{sandboxName: "A", sandboxEmail: "a@x", hostName: "H", hostEmail: "h@x"}
	if got := checkAgentIdentity(r.deps(), gitAuthorImport, aPushLine); got != identityFine || len(r.set) != 0 {
		t.Errorf("got %v, set %v", got, r.set)
	}
}

func TestAnswerKeptInTheProfileDoesNotMakeItLookEditedByHand(t *testing.T) {
	lay := updateLayout(t)
	if err := keepGitAuthorChoice(lay, "p", gitAuthorImport); err != nil {
		t.Fatal(err)
	}
	changeLibrary(t, lay)
	if got := updated(t, lay); got.state != profileUpdated {
		t.Errorf("outcome = %+v: the user's answer was taken for a hand edit, so the profile is no longer kept up to date", got)
	}
}

func TestTheAnswerSurvivesAReComposeOfTheProfile(t *testing.T) {
	lay := updateLayout(t)
	keepGitAuthorChoice(lay, "p", gitAuthorLeave)
	if err := replaceProfile(lay, "p", "box", []string{"mine"}); err != nil {
		t.Fatal(err)
	}
	if got, err := gitAuthorPreference(lay, "p"); err != nil || got != gitAuthorLeave {
		t.Errorf("after re-composing, the choice is %q (%v)", got, err)
	}
}

func TestTheAnswerSurvivesAnAutomaticUpdateOfTheProfile(t *testing.T) {
	lay := updateLayout(t)
	keepGitAuthorChoice(lay, "p", gitAuthorImport)
	changeLibrary(t, lay)
	if got := updated(t, lay); got.state != profileUpdated {
		t.Fatalf("outcome = %+v", got)
	}
	if got, err := gitAuthorPreference(lay, "p"); err != nil || got != gitAuthorImport {
		t.Errorf("after updating, the choice is %q (%v)", got, err)
	}
}

// terminalRig is the world a terminal session's check runs in.
type terminalRig struct {
	identityRig
	pref       string
	prefErr    error
	startable  bool
	started    bool
	canAsk     bool
	force      bool
	keptChoice []string
	keepErr    error
	notes      []string
}

func newTerminalRig() *terminalRig {
	r := &terminalRig{startable: true, canAsk: true}
	r.hostName, r.hostEmail, r.answer = "H", "h@x", true
	return r
}

func (r *terminalRig) check() terminalAuthor {
	return terminalAuthor{
		preference: func() (string, error) { return r.pref, r.prefErr },
		start:      func() bool { r.started = true; return r.startable },
		deps: func(say func(string, ...interface{}), ask func(string) bool) identityDeps {
			d := r.deps()
			d.say, d.ask = say, ask
			return d
		},
		push:    func() string { return aPushLine },
		keep:    func(choice string) error { r.keptChoice = append(r.keptChoice, choice); return r.keepErr },
		say:     func(format string, a ...interface{}) { r.notes = append(r.notes, fmt.Sprintf(format, a...)) },
		force:   r.force,
		canAsk:  func() bool { return r.canAsk },
		confirm: func(q string) bool { r.asked = append(r.asked, q); return r.answer },
	}
}

func TestAtATerminalYesGivesTheAuthorAndKeepsImport(t *testing.T) {
	r := newTerminalRig()
	r.check().run()
	if len(r.set) != 1 || strings.Join(r.keptChoice, ",") != gitAuthorImport {
		t.Errorf("set %v, kept %v", r.set, r.keptChoice)
	}
}

func TestAtATerminalNoLeavesItAndKeepsLeave(t *testing.T) {
	r := newTerminalRig()
	r.answer = false
	r.check().run()
	if len(r.set) != 0 || strings.Join(r.keptChoice, ",") != gitAuthorLeave {
		t.Errorf("set %v, kept %v", r.set, r.keptChoice)
	}
}

func TestWithNoTerminalToAskOnNothingIsDoneOrKept(t *testing.T) {
	r := newTerminalRig()
	r.canAsk = false
	r.check().run()
	if len(r.asked)+len(r.set)+len(r.keptChoice) != 0 {
		t.Errorf("asked %q, set %v, kept %v", r.asked, r.set, r.keptChoice)
	}
	if !strings.Contains(strings.Join(r.notes, "\n"), "would ask") {
		t.Errorf("it said %q", r.notes)
	}
}

func TestAChoiceAlreadyMadeIsNotKeptAgain(t *testing.T) {
	r := newTerminalRig()
	r.pref = gitAuthorImport
	r.check().run()
	if len(r.set) != 1 || len(r.asked) != 0 || len(r.keptChoice) != 0 {
		t.Errorf("set %v, asked %q, kept %v", r.set, r.asked, r.keptChoice)
	}
}

func TestALeaveChoiceDoesNotEvenStartTheSandbox(t *testing.T) {
	r := newTerminalRig()
	r.pref = gitAuthorLeave
	r.check().run()
	if r.started || r.sandboxAsked {
		t.Errorf("started %v, sandbox asked %v", r.started, r.sandboxAsked)
	}
}

func TestASandboxThatWillNotStartIsSaidAndNothingElseIsDone(t *testing.T) {
	r := newTerminalRig()
	r.startable = false
	r.check().run()
	if r.sandboxAsked || len(r.asked)+len(r.keptChoice) != 0 || len(r.notes) == 0 {
		t.Errorf("sandbox asked %v, asked %q, kept %v, said %q", r.sandboxAsked, r.asked, r.keptChoice, r.notes)
	}
}

func TestAChoiceThatCannotBeReadIsSaidAndNothingElseIsDone(t *testing.T) {
	r := newTerminalRig()
	r.prefErr = errors.New("unreadable")
	r.check().run()
	if r.started || len(r.notes) != 1 || !strings.Contains(r.notes[0], "unreadable") {
		t.Errorf("started %v, said %q", r.started, r.notes)
	}
}

func TestAnAnswerThatCannotBeKeptIsSaid(t *testing.T) {
	r := newTerminalRig()
	r.keepErr = errors.New("read-only")
	r.check().run()
	if !strings.Contains(strings.Join(r.notes, "\n"), "read-only") {
		t.Errorf("said %q", r.notes)
	}
}

func TestOnlyAHeadlessSessionGetsADeskForThePage(t *testing.T) {
	lay := updateLayout(t)
	inst := state.Instance{Name: "box", Profile: "p"}
	if desk := authorDeskFor(cliargs.Args{Headless: true}, true, lay, "box", inst); desk == nil {
		t.Error("a headless session has no desk")
	}
	for name, c := range map[string]struct {
		args  cliargs.Args
		found bool
	}{"a terminal session": {cliargs.Args{}, true}, "a headless one with no record of the sandbox": {cliargs.Args{Headless: true}, false}} {
		if desk := authorDeskFor(c.args, c.found, lay, "box", inst); desk != nil {
			t.Errorf("%s has a desk", name)
		}
	}
}
