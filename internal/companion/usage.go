package companion

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// ErrNotRunning is what reading the sandbox's load says when it isn't
// running: there's no load to read, and it isn't woken to find out.
var ErrNotRunning = errors.New("the sandbox isn't running")

// UsageReading is how busy the sandbox was when it was read, for the page's
// CPU and memory bars.
type UsageReading struct {
	// CPU is the percentage of the time the cores were busy; nil until
	// there are two samples to tell it from.
	CPU *float64
	// Cores is what the sandbox reports having; CPUSetting what it was given
	// (0 when not recorded).
	Cores, CPUSetting int
	MemUsed, MemTotal uint64
	// MemSetting is the memory it was given, as sbx takes it ("8g"); "" when
	// not recorded.
	MemSetting string
	// At is when it was read; zero means just now.
	At time.Time
}

// usageEvery is how often the sandbox is read, however many pages ask: each
// reading is a round trip into it.
const usageEvery = 3 * time.Second

// usageReadWithin bounds one reading. A variable for tests.
var usageReadWithin = 20 * time.Second

// usageCache keeps the last reading of the sandbox, which is taken in the
// background: a sandbox with every CPU busy is slow to answer, and the page
// asks every couple of seconds.
type usageCache struct {
	mu         sync.Mutex
	refreshing bool
	checked    time.Time // when a reading was last begun
	last       *UsageReading
	lastErr    error
	stopped    bool
}

func (s *Server) usageHandler(w http.ResponseWriter, r *http.Request) {
	now := s.usageNow()
	u := &s.usage
	u.mu.Lock()
	if !u.refreshing && (u.checked.IsZero() || now.Sub(u.checked) >= usageEvery) {
		u.refreshing = true
		u.checked = now
		go s.readUsage()
	}
	body := u.body(now)
	u.mu.Unlock()
	writeJSON(w, http.StatusOK, body)
}

// readUsage takes a reading, and keeps it.
func (s *Server) readUsage() {
	ctx, cancel := context.WithTimeout(context.Background(), usageReadWithin)
	defer cancel()
	reading, err := s.cfg.Usage(ctx)
	if reading.At.IsZero() {
		reading.At = s.usageNow()
	}
	u := &s.usage
	u.mu.Lock()
	defer u.mu.Unlock()
	u.refreshing = false
	switch {
	case errors.Is(err, ErrNotRunning):
		u.stopped, u.last, u.lastErr = true, nil, nil
	case err != nil:
		u.stopped, u.lastErr = false, err
	default:
		u.stopped, u.last, u.lastErr = false, &reading, nil
	}
}

// body is what the page is told: "unknown" before the first reading,
// "stopped" for a sandbox that isn't running, "unreachable" for one that
// never answered, else "ok" with the last reading, how old it is, and why
// it's old when the last attempt failed. Called with the lock held.
func (u *usageCache) body(now time.Time) map[string]interface{} {
	switch {
	case u.stopped:
		return map[string]interface{}{"state": "stopped"}
	case u.last == nil && u.lastErr != nil:
		return map[string]interface{}{"state": "unreachable", "error": u.lastErr.Error()}
	case u.last == nil:
		return map[string]interface{}{"state": "unknown"}
	}
	r := u.last
	body := map[string]interface{}{
		"state": "ok", "cpu": r.CPU, "cores": r.Cores, "coresSetting": r.CPUSetting,
		"memUsed": r.MemUsed, "memTotal": r.MemTotal, "memSetting": r.MemSetting,
		"ageMs": now.Sub(r.At).Milliseconds(),
	}
	if u.lastErr != nil {
		body["error"] = u.lastErr.Error()
	}
	return body
}
