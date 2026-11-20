package main

import (
	"sort"
	"time"

	"vibe/internal/companion"
	"vibe/internal/gittoken"
	"vibe/internal/state"
)

// gitTokensRunningOut are the other sandboxes whose GitHub token has expired or expires within gittoken.WarnWithin, from vibe's
// records alone (GitHub isn't asked): expired first, then the soonest. except, the sandbox the Admin tab is for, is left out: its
// own token has a box of its own. A record that can't be read is passed over: this is a reminder, not a check.
func gitTokensRunningOut(vibeHome, except string, now time.Time) []companion.AdminTokenSoon {
	records, err := state.List(vibeHome)
	if err != nil {
		return nil
	}
	var soon []state.Instance
	for _, r := range records {
		if r.Name != except && r.GitToken != "" && gittoken.Expiring(r.GitTokenExpires, now) {
			soon = append(soon, r)
		}
	}
	sort.SliceStable(soon, func(i, j int) bool { return soon[i].GitTokenExpires.Before(soon[j].GitTokenExpires) })
	out := make([]companion.AdminTokenSoon, len(soon))
	for i, r := range soon {
		out[i] = companion.AdminTokenSoon{Name: r.Name, Expiry: gittoken.DescribeExpiry(r.GitTokenExpires, now),
			Expired: gittoken.Expired(r.GitTokenExpires, now)}
	}
	return out
}

// expiredGitTokens are the sandboxes, of all vibe has records of, whose GitHub token has expired: what the Admin tab's dot is for
// (a token only expiring soon gets no dot, or it would be there for days before each expiry).
func expiredGitTokens(vibeHome string, now time.Time) []string {
	records, err := state.List(vibeHome)
	if err != nil {
		return nil
	}
	var names []string
	for _, r := range records {
		if r.GitToken != "" && gittoken.Expired(r.GitTokenExpires, now) {
			names = append(names, r.Name)
		}
	}
	sort.Strings(names)
	return names
}
