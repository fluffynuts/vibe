package companion

import (
	"sort"
	"sync"
	"time"

	"vibe/internal/policy"
)

// blockedEvery is how often the network log is read for what's blocked,
// however many pages ask: each reading is two sbx calls, and the page asks
// every couple of seconds.
const blockedEvery = 5 * time.Second

// maxBlockedHosts bounds what the state says, as a sandbox can reach for
// as many hosts as it likes.
const maxBlockedHosts = 200

// blockedCache keeps what the network log last said was blocked, read in the
// background: sbx can be slow, and the state is asked for every couple of
// seconds, so it never waits on it.
type blockedCache struct {
	mu         sync.Mutex
	refreshing bool
	checked    time.Time
	hosts      []string
	known      bool
}

// blockedHosts is the hosts the log says are blocked (not those a rule now
// allows), sorted, and whether that's known yet: it isn't until the first
// reading is back, which isn't the same as there being none.
func (s *Server) blockedHosts() (hosts []string, known bool) {
	if s.cfg.Policy == nil {
		return nil, false
	}
	b := &s.blocked
	now := s.clock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.refreshing && (b.checked.IsZero() || now.Sub(b.checked) >= blockedEvery) {
		b.refreshing = true
		b.checked = now
		go s.readBlocked()
	}
	return b.hosts, b.known
}

func (s *Server) readBlocked() {
	log, err := s.cfg.Policy.ResolvedLog()
	b := &s.blocked
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refreshing = false
	if err != nil {
		// What was known stands: "nothing is blocked" because sbx hiccuped
		// would drop the page's dot, and raise it again after.
		return
	}
	b.hosts, b.known = blockedIn(log), true
}

// blockedIn is the hosts a resolved log says are blocked: once each, sorted, and not more than maxBlockedHosts.
func blockedIn(log policy.Log) []string {
	seen := map[string]bool{}
	hosts := []string{}
	for _, e := range log.Entries {
		if e.Decision == policy.Blocked && !seen[e.Host] {
			seen[e.Host] = true
			hosts = append(hosts, e.Host)
		}
	}
	sort.Strings(hosts)
	if len(hosts) > maxBlockedHosts {
		hosts = hosts[:maxBlockedHosts]
	}
	return hosts
}

// readingLog is what the network log's own reading calls as it begins: that is a reading of what the background one is for,
// so it isn't made beside it. A reading is two sbx calls, each a couple of seconds, and every vibe makes them: a page on the
// Network log, polling, used to have the same two calls made again in the background.
func (s *Server) readingLog() {
	b := &s.blocked
	b.mu.Lock()
	b.checked = s.clock()
	b.mu.Unlock()
}

// readLog is the network log checked against the rules, as the page's reading of it asks sbx: what it finds is what the state
// says is blocked, as a reading in the background would have found.
func (s *Server) readLog() (interface{}, error) {
	s.readingLog()
	log, err := s.cfg.Policy.ResolvedLog()
	if err != nil {
		return log, err
	}
	b := &s.blocked
	b.mu.Lock()
	b.checked = s.clock()
	b.hosts, b.known = blockedIn(log), true
	b.mu.Unlock()
	return log, nil
}
