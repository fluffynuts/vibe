package main

import (
	"reflect"
	"testing"
	"time"

	"vibe/internal/companion"
	"vibe/internal/state"
)

// Other sandboxes' tokens near expiry, from vibe's records alone (GitHub isn't asked): expired first, then the soonest; the
// sandbox the tab is for is left out (its own token has its own box), as are tokens with time to spare, or no expiry, or none.
func TestTokensRunningOutAreOtherSandboxesExpiredOrWithinThreeDaysSoonestFirst(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, inst := range []state.Instance{
		{Name: "this", GitToken: gitTokenOwn, GitTokenExpires: now.Add(-time.Hour)},
		{Name: "soon", GitToken: gitTokenOwn, GitTokenExpires: now.Add(2 * 24 * time.Hour)},
		{Name: "sooner", GitToken: gitTokenDefault, GitTokenExpires: now.Add(24 * time.Hour)},
		{Name: "gone", GitToken: gitTokenOwn, GitTokenExpires: now.Add(-48 * time.Hour)},
		{Name: "later", GitToken: gitTokenOwn, GitTokenExpires: now.Add(10 * 24 * time.Hour)},
		{Name: "forever", GitToken: gitTokenOwn},
		{Name: "none", GitTokenExpires: now.Add(-time.Hour)},
	} {
		inst.Target = "/w/" + inst.Name
		if err := state.Save(home, inst); err != nil {
			t.Fatal(err)
		}
	}
	got := gitTokensRunningOut(home, "this", now)
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	if !reflect.DeepEqual(names, []string{"gone", "sooner", "soon"}) {
		t.Fatalf("running out = %v, want gone, sooner, soon", names)
	}
	if !got[0].Expired || got[1].Expired || got[0].Expiry == "" || got[2].Expiry == "" {
		t.Errorf("running out = %+v", got)
	}
	if expired := expiredGitTokens(home, now); !reflect.DeepEqual(expired, []string{"gone", "this"}) {
		t.Errorf("expired = %v, want every expired one, this sandbox's too", expired)
	}
	var _ []companion.AdminTokenSoon = got
}
