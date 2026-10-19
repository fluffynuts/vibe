package companion

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"vibe/internal/policy"
)

// blockedRig is a page for a sandbox whose network log says what log does,
// with a clock the test moves.
type blockedRig struct {
	s   *Server
	sbx *fakeSbx
	mu  sync.Mutex
	now time.Time
}

func newBlockedRig(t *testing.T, log string) *blockedRig {
	t.Helper()
	sbx := &fakeSbx{out: map[string]string{"log": log, "ls-network": `{"rules": []}`}}
	rig := &blockedRig{sbx: sbx, now: t0}
	rig.s = New(Config{Sandbox: "box", Token: token, Port: testPort, Policy: &policy.Client{Sandbox: "box", Run: sbx.run}})
	rig.s.clock = func() time.Time { rig.mu.Lock(); defer rig.mu.Unlock(); return rig.now }
	return rig
}

func (r *blockedRig) advance(d time.Duration) { r.mu.Lock(); r.now = r.now.Add(d); r.mu.Unlock() }

func (r *blockedRig) logCalls() int {
	n := 0
	for _, c := range r.sbx.called() {
		if len(c) > 1 && c[1] == "log" {
			n++
		}
	}
	return n
}

// hosts is what the state says is blocked: nil before it's been read.
func (r *blockedRig) hosts(t *testing.T) (hosts []string, known bool) {
	t.Helper()
	rec := do(t, r.s, req{path: "/api/state", headers: authed(nil)})
	var st struct {
		Blocked *[]string `json:"blockedHosts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Blocked == nil {
		return nil, false
	}
	return *st.Blocked, true
}

func (r *blockedRig) untilKnown(t *testing.T) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if hosts, known := r.hosts(t); known {
			return hosts
		}
		if time.Now().After(deadline) {
			t.Fatal("the blocked hosts were never read")
		}
		time.Sleep(time.Millisecond)
	}
}

const logOf = `{"blocked_hosts": [
  {"host":"evil.example","vm_name":"box","count_since":2,"last_seen":"2026-10-19T12:00:00Z"},
  {"host":"b.example","vm_name":"box","count_since":1,"last_seen":"2026-10-19T12:00:01Z"},
  {"host":"evil.example","vm_name":"box","count_since":1,"last_seen":"2026-10-19T12:00:02Z"}],
 "allowed_hosts": [{"host":"github.com","vm_name":"box","count_since":9,"last_seen":"2026-10-19T12:00:03Z"}]}`

func TestTheStateSaysWhichHostsAreBlockedNotWhichAreAllowed(t *testing.T) {
	rig := newBlockedRig(t, logOf)
	got := rig.untilKnown(t)
	if strings.Join(got, ",") != "b.example,evil.example" {
		t.Errorf("blocked = %v, want b.example and evil.example, once each, in order", got)
	}
}

// Until it's been read there's no answer, which isn't the same as an answer of
// none: the page must not take it for "nothing is blocked".
func TestBeforeTheFirstReadingNothingIsSaidOfWhatIsBlockedAndNothingWaits(t *testing.T) {
	rig := newBlockedRig(t, logOf)
	began := time.Now()
	if _, known := rig.hosts(t); known {
		t.Error("said what's blocked before it was read")
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("the first state waited %s on the log", took)
	}
}

func TestANoneBlockedLogSaysSoAsAnEmptyListNotNothing(t *testing.T) {
	rig := newBlockedRig(t, `{"allowed_hosts": [{"host":"github.com","vm_name":"box","count_since":9,"last_seen":"2026-10-19T12:00:03Z"}]}`)
	got := rig.untilKnown(t)
	if got == nil || len(got) != 0 {
		t.Errorf("blocked = %#v, want an empty list", got)
	}
}

// What a rule now allows has been dealt with: it's marked unblocked, and isn't
// news.
func TestAHostARuleNowAllowsIsNotCountedAsBlocked(t *testing.T) {
	rig := newBlockedRig(t, logOf)
	rig.sbx.out["ls-network"] = `{"rules": [{"id":"r-1","resources":["b.example"],"decision":"allow","origin":"local","editable":true}]}`
	got := rig.untilKnown(t)
	if strings.Join(got, ",") != "evil.example" {
		t.Errorf("blocked = %v, want only evil.example: b.example is allowed now", got)
	}
}

func TestTheLogIsReadInTheBackgroundAndNoMoreOftenThanEveryFewSeconds(t *testing.T) {
	rig := newBlockedRig(t, logOf)
	rig.untilKnown(t)
	for i := 0; i < 20; i++ {
		rig.hosts(t)
	}
	if n := rig.logCalls(); n != 1 {
		t.Fatalf("the log was read %d times for 21 states in an instant, want 1", n)
	}
	rig.advance(blockedEvery + time.Millisecond)
	rig.hosts(t)
	deadline := time.Now().Add(10 * time.Second)
	for rig.logCalls() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the log was never read again")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestWhatWasBlockedIsReplacedAsTheLogChanges(t *testing.T) {
	rig := newBlockedRig(t, logOf)
	rig.untilKnown(t)
	rig.sbx.out["log"] = `{"blocked_hosts": [{"host":"new.example","vm_name":"box","count_since":1,"last_seen":"2026-10-19T12:05:00Z"}]}`
	rig.advance(blockedEvery + time.Millisecond)
	rig.hosts(t)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got, _ := rig.hosts(t); strings.Join(got, ",") == "new.example" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the new log was never taken in")
		}
		time.Sleep(time.Millisecond)
	}
}

// A log that can't be read leaves what was known: a page told "nothing is
// blocked" because sbx hiccuped would drop the dot, and raise it again after.
func TestALogThatCantBeReadLeavesWhatWasKnown(t *testing.T) {
	rig := newBlockedRig(t, logOf)
	rig.untilKnown(t)
	rig.sbx.mu.Lock()
	rig.sbx.err = errors.New("sbx: the daemon isn't running")
	rig.sbx.mu.Unlock()
	rig.advance(blockedEvery + time.Millisecond)
	rig.hosts(t)
	// Until the failed read has ended: it began, and nothing is under way.
	deadline := time.Now().Add(10 * time.Second)
	for {
		rig.s.blocked.mu.Lock()
		done := !rig.s.blocked.refreshing
		rig.s.blocked.mu.Unlock()
		if done && rig.logCalls() >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second read never ended")
		}
		time.Sleep(time.Millisecond)
	}
	if got, known := rig.hosts(t); !known || strings.Join(got, ",") != "b.example,evil.example" {
		t.Errorf("after a failed read: %v (known %v), want what was known", got, known)
	}
}

func TestAPageWithNoPolicyHasNoBlockedHostsToSay(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort})
	rec := do(t, s, req{path: "/api/state", headers: authed(nil)})
	if strings.Contains(rec.Body.String(), `"blockedHosts":["`) {
		t.Errorf("body = %s", rec.Body)
	}
}
