// Package portalloc probes the host for a free TCP port, and lets a vibe keep one for itself while it makes up its mind.
// The port a sandbox ends up with is remembered in its instance record (see internal/state), not here — this package only
// answers "can I bind this right now", and "has another vibe said it is about to".
package portalloc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"vibe/internal/filelock"
)

// InUse reports whether something is already listening on the given port.
func InUse(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

// FindFree walks upward from start until it finds a port that is neither
// listed in taken nor being listened on, giving up above limit. A port can be
// claimed without anything listening on it — the sandbox holding it may be
// stopped, or it may have been handed out earlier in the same allocation
// pass — so taken is consulted first and InUse only confirms the rest.
func FindFree(start, limit int, taken map[int]bool) (int, error) {
	return FindFreeWhere(start, limit, taken, nil)
}

// FindFreeWhere is FindFree for a port that accept also takes (when there is an accept): it is asked last, of a port that
// is otherwise free, so that taking it (reserving it) happens only for the one that is used.
func FindFreeWhere(start, limit int, taken map[int]bool, accept func(port int) bool) (int, error) {
	for port := start; port < limit; port++ {
		if taken[port] || InUse(port) {
			continue
		}
		if accept != nil && !accept(port) {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("no free port found in %d-%d", start, limit-1)
}

// Reservations are one vibe's claims on host ports, kept in dir as a lock file for each. A sandbox's record is written when
// it has been made, minutes after its ports were chosen (and sbx binds them only when it publishes them): in between,
// nothing else shows that a port is spoken for, and vibes started together choose the same ones. A reservation is made
// as a port is chosen, and shows to other vibes at once. It is a lock, so it goes when the vibe holding it does, however it
// ends: a vibe that died leaves nothing behind to clean up.
type Reservations struct {
	dir  string
	mu   sync.Mutex
	held map[int]*os.File
}

func NewReservations(dir string) *Reservations {
	return &Reservations{dir: dir, held: map[int]*os.File{}}
}

// Reserve claims port for this vibe. It says false when another vibe holds it; one this vibe holds already is its own. An
// error is for what stops a reservation being made at all (the folder can't be made, say), when nothing is known of who else
// might want the port.
func (r *Reservations) Reserve(port int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.held[port] != nil {
		return true, nil
	}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(filepath.Join(r.dir, fmt.Sprintf("%d.lock", port)), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return false, err
	}
	if filelock.TryLock(f) != nil {
		f.Close()
		return false, nil
	}
	r.held[port] = f
	return true, nil
}

// Release gives up the reservations on ports, or all of them with none named: a sandbox's record shows what it holds once it
// is made, and one that couldn't be made has no use for them. The lock files stay (they hold nothing), since a file
// taken away under another vibe that has it open would let two lock one that is not.
func (r *Reservations) Release(ports ...int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(ports) == 0 {
		for port := range r.held {
			ports = append(ports, port)
		}
	}
	for _, port := range ports {
		if f := r.held[port]; f != nil {
			f.Close()
			delete(r.held, port)
		}
	}
}
