package companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeReader stands in for reading the sandbox's load: it answers with what's
// in next, after it's let go (at once when release is nil).
type fakeReader struct {
	calls   atomic.Int32
	release chan struct{}
	mu      sync.Mutex
	reading UsageReading
	err     error
}

func (f *fakeReader) read(ctx context.Context) (UsageReading, error) {
	f.calls.Add(1)
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return UsageReading{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reading, f.err
}

func (f *fakeReader) set(r UsageReading, err error) {
	f.mu.Lock()
	f.reading, f.err = r, err
	f.mu.Unlock()
}

func pct(v float64) *float64 { return &v }

var t0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

type usageRig struct {
	s      *Server
	reader *fakeReader
	now    time.Time
	mu     sync.Mutex
}

func newUsageRig(t *testing.T, reader *fakeReader) *usageRig {
	t.Helper()
	rig := &usageRig{reader: reader, now: t0}
	rig.s = New(Config{Sandbox: "box", Token: token, Port: testPort, Usage: reader.read})
	rig.s.usageNow = func() time.Time { rig.mu.Lock(); defer rig.mu.Unlock(); return rig.now }
	return rig
}

func (r *usageRig) advance(d time.Duration) { r.mu.Lock(); r.now = r.now.Add(d); r.mu.Unlock() }

type usageBody struct {
	State      string   `json:"state"`
	CPU        *float64 `json:"cpu"`
	Cores      int      `json:"cores"`
	CoresSet   int      `json:"coresSetting"`
	MemUsed    uint64   `json:"memUsed"`
	MemTotal   uint64   `json:"memTotal"`
	MemSetting string   `json:"memSetting"`
	AgeMs      int64    `json:"ageMs"`
	Error      string   `json:"error"`
}

func (r *usageRig) get(t *testing.T) usageBody {
	t.Helper()
	rec := do(t, r.s, req{path: "/api/usage", headers: authed(nil)})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/usage: %d %s", rec.Code, rec.Body)
	}
	var b usageBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

// untilState polls the endpoint until it says want: a reading is taken in the
// background, so when it lands is when it lands.
func (r *usageRig) untilState(t *testing.T, want string) usageBody {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b := r.get(t)
		if b.State == want {
			return b
		}
		if time.Now().After(deadline) {
			t.Fatalf("still %q, wanted %q", b.State, want)
		}
		time.Sleep(time.Millisecond)
	}
}

var busyReading = UsageReading{CPU: pct(87.5), Cores: 12, CPUSetting: 12, MemUsed: 5 << 30, MemTotal: 8 << 30, MemSetting: "8g"}

func TestAPageWithNoWayToReadTheSandboxHasNoUsage(t *testing.T) {
	s := New(Config{Sandbox: "box", Token: token, Port: testPort})
	if rec := do(t, s, req{path: "/api/usage", headers: authed(nil)}); rec.Code != http.StatusNotFound {
		t.Errorf("status %d", rec.Code)
	}
}

func TestTheStateSaysWhetherThereIsUsageToShow(t *testing.T) {
	has := func(s *Server) bool {
		rec := do(t, s, req{path: "/api/state", headers: authed(nil)})
		var st struct {
			Usage bool `json:"usage"`
		}
		json.Unmarshal(rec.Body.Bytes(), &st)
		return st.Usage
	}
	if has(New(Config{Sandbox: "box", Token: token, Port: testPort})) {
		t.Error("usage offered with no way to read it")
	}
	if !has(newUsageRig(t, &fakeReader{}).s) {
		t.Error("usage not offered")
	}
}

// The first poll isn't kept waiting for the first reading, however slow the
// sandbox is to give it: it's told there's none yet, and one is started.
func TestTheFirstPollIsAnsweredAtOnceNotWhenTheSandboxAnswers(t *testing.T) {
	reader := &fakeReader{release: make(chan struct{})}
	rig := newUsageRig(t, reader)
	answered := make(chan usageBody, 1)
	go func() { answered <- rig.get(t) }()
	select {
	case b := <-answered:
		if b.State != "unknown" {
			t.Errorf("state %q, want unknown before there's a reading", b.State)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the poll waited on the sandbox")
	}
	reader.set(busyReading, nil)
	close(reader.release)
	b := rig.untilState(t, "ok")
	if b.CPU == nil || *b.CPU != 87.5 || b.Cores != 12 || b.CoresSet != 12 || b.MemUsed != 5<<30 || b.MemTotal != 8<<30 || b.MemSetting != "8g" {
		t.Errorf("reading = %+v", b)
	}
}

func TestPollsAreServedFromWhatWasReadAndReadingIsNotMoreOftenThanEveryFewSeconds(t *testing.T) {
	reader := &fakeReader{reading: busyReading}
	rig := newUsageRig(t, reader)
	rig.get(t)
	rig.untilState(t, "ok")
	for i := 0; i < 20; i++ {
		rig.get(t)
	}
	if n := reader.calls.Load(); n != 1 {
		t.Fatalf("the sandbox was read %d times for 21 polls in an instant, want 1", n)
	}
	rig.advance(usageEvery + time.Millisecond)
	rig.get(t) // asks for another; served what there is meanwhile
	deadline := time.Now().Add(10 * time.Second)
	for reader.calls.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("a reading was never taken again")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestOneReadingIsUnderWayAtATimeHoweverManyPollsComeWhileItIs(t *testing.T) {
	reader := &fakeReader{release: make(chan struct{}), reading: busyReading}
	rig := newUsageRig(t, reader)
	for i := 0; i < 10; i++ {
		rig.get(t)
		// Well past the interval each time: it's the reading still under way
		// that holds the next off, not how soon it was asked.
		rig.advance(2 * usageEvery)
	}
	close(reader.release)
	rig.untilState(t, "ok")
	if n := reader.calls.Load(); n != 1 {
		t.Errorf("read %d times, want 1", n)
	}
}

func TestHowOldTheReadingIsIsSaidSoThePageCanTellWhenItIsStale(t *testing.T) {
	reader := &fakeReader{reading: UsageReading{CPU: pct(10), Cores: 4, MemTotal: 1 << 30, At: t0}}
	rig := newUsageRig(t, reader)
	rig.get(t)
	b := rig.untilState(t, "ok")
	if b.AgeMs != 0 {
		t.Errorf("age %dms just after, want 0", b.AgeMs)
	}
	rig.advance(7 * time.Second)
	reader.set(UsageReading{}, errors.New("the sandbox didn't answer"))
	rig.get(t)
	deadline := time.Now().Add(10 * time.Second)
	for {
		b = rig.get(t)
		if b.Error != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the failure was never said")
		}
		time.Sleep(time.Millisecond)
	}
	if b.State != "ok" || b.CPU == nil || *b.CPU != 10 || b.AgeMs != 7000 || b.Error != "the sandbox didn't answer" {
		t.Errorf("after a failed read: %+v: want the old reading, its age, and why it's old", b)
	}
}

func TestAStoppedSandboxIsSaidToBeStoppedWithNoFigures(t *testing.T) {
	reader := &fakeReader{}
	reader.set(UsageReading{}, ErrNotRunning)
	rig := newUsageRig(t, reader)
	rig.get(t)
	b := rig.untilState(t, "stopped")
	if b.CPU != nil || b.MemTotal != 0 || b.Error != "" {
		t.Errorf("stopped but %+v", b)
	}
}

func TestASandboxNeverReadSaysWhyAndCarriesOnTrying(t *testing.T) {
	reader := &fakeReader{}
	reader.set(UsageReading{}, errors.New("exec failed"))
	rig := newUsageRig(t, reader)
	rig.get(t)
	b := rig.untilState(t, "unreachable")
	if b.Error != "exec failed" {
		t.Errorf("error %q", b.Error)
	}
	reader.set(busyReading, nil)
	rig.advance(usageEvery + time.Millisecond)
	rig.get(t)
	rig.untilState(t, "ok")
}

func TestAReadingThatIsNeverAnsweredIsGivenUpOn(t *testing.T) {
	old := usageReadWithin
	usageReadWithin = 30 * time.Millisecond
	t.Cleanup(func() { usageReadWithin = old })
	reader := &fakeReader{release: make(chan struct{})}
	rig := newUsageRig(t, reader)
	rig.get(t)
	rig.untilState(t, "unreachable")
	close(reader.release)
}
