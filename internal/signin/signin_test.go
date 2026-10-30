package signin

import (
	"errors"
	"strings"
	"testing"
)

var failed = errors.New("exit status 1")

func TestDockerIsSignedInWhenSbxAnswers(t *testing.T) {
	if got := Docker("[]", nil, false); got.State != SignedIn {
		t.Errorf("state = %q", got.State)
	}
}

func TestDockerIsSignedOutWhenSbxSaysSo(t *testing.T) {
	got := Docker("error: not signed in to Docker\n  try: sbx login\n", failed, false)
	if got.State != SignedOut {
		t.Errorf("state = %q", got.State)
	}
}

// On a pristine sbx every command asks which network policy to start with, and gets no answer: the question is what
// it printed, or that it was cancelled.
func TestDockerNeedsAPolicyWhenSbxAsksForOne(t *testing.T) {
	for _, out := range []string{
		"\n  Choose the network policy for sandboxes\n    1. Open\n  Choice [1-3, default 2]:\n  error: operation cancelled by user\n",
		"error: operation cancelled by user",
	} {
		got := Docker(out, failed, false)
		if got.State != NeedsPolicy {
			t.Errorf("%q: state = %q", out, got.State)
		}
		if !strings.Contains(strings.Join(got.Manual, "\n"), "sbx policy init balanced") {
			t.Errorf("%q: it doesn't say how to choose one by hand: %v", out, got.Manual)
		}
	}
}

func TestDockerIsUnknownWhenSbxDoesNotAnswerAndSaysWhyItMayNot(t *testing.T) {
	got := Docker("", failed, true)
	if got.State != Unknown {
		t.Errorf("state = %q", got.State)
	}
	if !strings.Contains(got.Detail, "wallet") {
		t.Errorf("it doesn't mention the system wallet, which is what makes sbx wait: %q", got.Detail)
	}
}

func TestDockerIsUnknownForAnythingElseAndSaysWhatSbxSaid(t *testing.T) {
	got := Docker("error: the daemon fell over\nmore\n", failed, false)
	if got.State != Unknown || !strings.Contains(got.Detail, "the daemon fell over") {
		t.Errorf("got %+v", got)
	}
}

func TestDockerAlwaysSaysHowToSignInByHand(t *testing.T) {
	for _, out := range []string{"[]", "error: not signed in to Docker"} {
		err := error(nil)
		if out != "[]" {
			err = failed
		}
		if m := strings.Join(Docker(out, err, false).Manual, "\n"); !strings.Contains(m, "sbx login") {
			t.Errorf("%q: manual = %q", out, m)
		}
	}
}

const signedInStatus = `{"loggedIn": true, "authMethod": "claude.ai", "email": "me@example.com", "orgName": "Codeo", "subscriptionType": "team"}`

func TestClaudeIsSignedInAndSaysWho(t *testing.T) {
	got := Claude("none", signedInStatus, true)
	if got.State != SignedIn {
		t.Fatalf("state = %q", got.State)
	}
	for _, want := range []string{"me@example.com", "Codeo", "team"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail %q lacks %q", got.Detail, want)
		}
	}
}

func TestClaudeIsSignedOutWhenItSaysSoAndSbxGivesItNothing(t *testing.T) {
	for _, mode := range []string{"", "none"} {
		if got := Claude(mode, `{"loggedIn": false}`, true); got.State != SignedOut {
			t.Errorf("mode %q: state = %q", mode, got.State)
		}
	}
}

// With a mode other than none sbx may be handing the agent its credentials itself, which the status can't see.
func TestClaudeIsNotSaidToBeSignedOutWhenSbxSuppliesCredentials(t *testing.T) {
	got := Claude("apikey", `{"loggedIn": false}`, true)
	if got.State != Unknown || !strings.Contains(got.Detail, "apikey") {
		t.Errorf("got %+v", got)
	}
}

func TestClaudeIsUnknownWhenItCouldNotBeAskedOrAnswersOddly(t *testing.T) {
	for name, c := range map[string]struct {
		status string
		asked  bool
	}{"not asked": {"", false}, "not json": {"Not logged in", true}, "no field": {`{}`, true}} {
		if got := Claude("none", c.status, c.asked); got.State != Unknown {
			t.Errorf("%s: state = %q", name, got.State)
		}
	}
}

func TestClaudeSaysHowToSignInByHandForASubscriptionAndForAKey(t *testing.T) {
	m := strings.Join(Claude("none", `{"loggedIn": false}`, true).Manual, "\n")
	for _, want := range []string{"claude auth login", "sbx secret set anthropic"} {
		if !strings.Contains(m, want) {
			t.Errorf("manual lacks %q:\n%s", want, m)
		}
	}
}

func TestAnAgentVibeCannotCheckIsSaidSo(t *testing.T) {
	got := Other("cursor")
	if got.State != Unsupported || !strings.Contains(got.Detail, "cursor") {
		t.Errorf("got %+v", got)
	}
}
