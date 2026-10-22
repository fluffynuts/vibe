package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

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

func TestTheChoiceIsKeptInTheProfileAmongWhatItAlreadyHas(t *testing.T) {
	lay := updateLayout(t)
	writeFile(t, lay.HomePath("profiles", "p", "config.yaml"), "name: p\n")
	writeFile(t, lay.HomePath("profiles", "p", "settings.yaml"), "# mine\nmemory: 8g\n")
	if err := keepGitAuthorChoice(lay, "p", gitAuthorImport); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(lay.HomePath("profiles", "p", "settings.yaml"))
	if !strings.Contains(string(data), "gitAuthor: import") || !strings.Contains(string(data), "memory: 8g") || !strings.Contains(string(data), "# mine") {
		t.Errorf("settings.yaml = %q", data)
	}
	merged, err := loadSettings(lay, "p")
	if err != nil || merged.GitAuthor != gitAuthorImport {
		t.Errorf("read back %q, %v", merged.GitAuthor, err)
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
