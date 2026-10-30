// Package signin reads what sbx and the agent say about being signed in, for the page's Sign-in card. It reads only: it
// signs nobody in, and runs nothing itself, so what it is given is all it knows.
package signin

import (
	"encoding/json"
	"fmt"
	"strings"
)

// State is whether something is signed in. Unknown is for when it couldn't be told, which is said in Detail: it is
// never made into "signed out" on a guess.
type State string

const (
	SignedIn    State = "signed-in"
	SignedOut   State = "signed-out"
	Unknown     State = "unknown"
	NeedsPolicy State = "needs-policy"
	Unsupported State = "unsupported"
)

// Part is the sign-in of one thing (Docker for sbx, or the agent): its state, who or why in Detail, and the commands
// to put it right by hand, for anyone who would rather not have vibe do it.
type Part struct {
	State  State    `json:"state"`
	Detail string   `json:"detail,omitempty"`
	Manual []string `json:"manual"`
}

// Status is both, as the page shows them: Agent is the agent's name, Docker is sbx's sign-in, AgentPart the agent's.
type Status struct {
	Agent     string `json:"agent"`
	Docker    Part   `json:"docker"`
	AgentPart Part   `json:"agentSignIn"`
}

var dockerManual = []string{
	"In a terminal on this machine, run: sbx login",
	"Open the address it prints, enter the code, and wait for it to say you are signed in.",
}

var policyManual = []string{
	"sbx asks which network policy to start with before it will answer anything. Choose one in a terminal on this machine: sbx policy init balanced",
	"The others are allow-all (no restrictions) and deny-all (nothing but what you allow). It can't be changed afterwards without sbx policy reset, which clears every sandbox's rules.",
}

// Docker reads what `sbx ls` printed (and its error, and whether it was given up on after too long): that it answers is
// being signed in; it says when it isn't.
func Docker(out string, err error, timedOut bool) Part {
	part := Part{Manual: dockerManual}
	lower := strings.ToLower(out)
	switch {
	case err == nil:
		part.State = SignedIn
	case timedOut:
		part.State = Unknown
		part.Detail = "sbx didn't answer in time. Its waiting is often for the system wallet (keyring) to be unlocked: unlock it, and check again."
	case strings.Contains(lower, "not signed in"):
		part.State = SignedOut
	case strings.Contains(lower, "choose the network policy") || strings.Contains(lower, "operation cancelled by user") ||
		strings.Contains(lower, "network policy has not been initialized"):
		part.State = NeedsPolicy
		part.Detail = "sbx hasn't been told which network policy to use."
		part.Manual = policyManual
	default:
		part.State = Unknown
		part.Detail = "sbx said: " + firstLine(out)
	}
	return part
}

// claudeManual is the two ways of signing Claude in: a subscription (a browser, then a code pasted back) and an API
// key (kept by sbx, which hands it to the agent without the sandbox holding it).
var claudeManual = []string{
	"With a subscription: in a terminal, in this sandbox's folder, run vibe -c (it needs 'sbx setup ssh' once), then claude auth login. Open the address it prints, sign in, and paste the code the page shows back into the terminal.",
	`With an API key, for every sandbox: echo "$ANTHROPIC_API_KEY" | sbx secret set anthropic`,
}

// Claude reads the sandbox's SBX_CRED_ANTHROPIC_MODE (mode) and what `claude auth status` printed (status), asked says
// whether the sandbox could be asked at all.
func Claude(mode, status string, asked bool) Part {
	part := Part{Manual: claudeManual}
	if !asked {
		part.State = Unknown
		part.Detail = "the sandbox couldn't be asked (is it running?)"
		return part
	}
	var s struct {
		LoggedIn         *bool  `json:"loggedIn"`
		Email            string `json:"email"`
		OrgName          string `json:"orgName"`
		SubscriptionType string `json:"subscriptionType"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(status)), &s) != nil || s.LoggedIn == nil {
		part.State = Unknown
		part.Detail = "claude auth status said something vibe doesn't know: " + firstLine(status)
		return part
	}
	switch {
	case *s.LoggedIn:
		part.State = SignedIn
		part.Detail = who(s.Email, s.OrgName, s.SubscriptionType)
	case mode != "" && mode != "none":
		part.State = Unknown
		part.Detail = fmt.Sprintf("sbx supplies this agent's credentials itself (mode %s), which claude can't see", mode)
	default:
		part.State = SignedOut
	}
	return part
}

// who is "email · org (plan)", leaving out what isn't there.
func who(email, org, plan string) string {
	if org != "" && plan != "" {
		org += " (" + plan + ")"
	} else if plan != "" {
		org = plan
	}
	var parts []string
	for _, p := range []string{email, org} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// Other is the part for an agent whose sign-in vibe can't read yet.
func Other(agent string) Part {
	return Part{State: Unsupported, Detail: "vibe can't check " + agent + "'s sign-in yet", Manual: []string{}}
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return "nothing"
}
