package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"vibe/internal/companion"
	"vibe/internal/sbxrun"
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
	known := map[string]bool{"this": true, "soon": true, "sooner": true, "gone": true, "later": true, "forever": true, "none": true}
	got := gitTokensRunningOut(home, "this", now, known)
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
	if expired := expiredGitTokens(home, now, known); !reflect.DeepEqual(expired, []string{"gone", "this"}) {
		t.Errorf("expired = %v, want every expired one, this sandbox's too", expired)
	}
	var _ []companion.AdminTokenSoon = got
}

// A record whose sandbox sbx no longer knows (removed with sbx rm, lost to a reset) can't be renewed by the batch the tab names:
// it is in neither the list nor the dot, or the dot would stay on for good.
func TestTokensRunningOutLeaveOutRecordsWhoseSandboxSbxDoesntKnow(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"here", "lost"} {
		if err := state.Save(home, state.Instance{Name: name, Target: "/w/" + name, GitToken: gitTokenOwn, GitTokenExpires: now.Add(-time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	known := map[string]bool{"here": true}
	if got := gitTokensRunningOut(home, "", now, known); len(got) != 1 || got[0].Name != "here" {
		t.Errorf("running out = %+v, want here alone", got)
	}
	if got := expiredGitTokens(home, now, known); !reflect.DeepEqual(got, []string{"here"}) {
		t.Errorf("expired = %v, want here alone", got)
	}
}

// What sbx last listed is kept for the dot, which never waits on sbx: until it has answered once, the dot says nothing; a list
// grown old is asked for again in the background, once at a time, and the dot answers from what it has meanwhile.
func TestTheDotAsksSbxInTheBackgroundAndSaysNothingUntilItHasAnswered(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if err := state.Save(home, state.Instance{Name: "box", Target: "/w/box", GitToken: gitTokenOwn, GitTokenExpires: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	asked := make(chan struct{}, 10)
	k := &knownSandboxes{now: func() time.Time { return now }, list: func(context.Context) ([]sbxrun.Status, error) {
		asked <- struct{}{}
		<-release
		return []sbxrun.Status{{Name: "box"}}, nil
	}}
	done := make(chan []string)
	go func() { done <- expiredForDot(home, now, k) }()
	select {
	case got := <-done:
		if len(got) != 0 {
			t.Errorf("before sbx answered, the dot named %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the dot waited on sbx")
	}
	<-asked
	if again := expiredForDot(home, now, k); len(again) != 0 {
		t.Errorf("still waiting on sbx, the dot named %v", again)
	}
	select {
	case <-asked:
		t.Fatal("sbx was asked again while it was still answering")
	default:
	}
	close(release)
	for i := 0; i < 100 && len(expiredForDot(home, now, k)) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if got := expiredForDot(home, now, k); !reflect.DeepEqual(got, []string{"box"}) {
		t.Errorf("once sbx had answered, the dot named %v, want box", got)
	}
}
