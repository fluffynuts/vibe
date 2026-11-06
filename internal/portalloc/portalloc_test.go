package portalloc

import (
	"fmt"
	"net"
	"testing"
)

func TestFindFreeSkipsTaken(t *testing.T) {
	start := freeRun(t, 4)
	taken := map[int]bool{start: true, start + 1: true}

	got, err := FindFree(start, start+4, taken)
	if err != nil {
		t.Fatalf("FindFree: %v", err)
	}
	if got != start+2 {
		t.Errorf("FindFree = %d, want %d", got, start+2)
	}
}

func TestFindFreeSkipsListener(t *testing.T) {
	start := freeRun(t, 4)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", start))
	if err != nil {
		t.Skipf("could not bind %d: %v", start, err)
	}
	defer ln.Close()

	got, err := FindFree(start, start+4, nil)
	if err != nil {
		t.Fatalf("FindFree: %v", err)
	}
	if got != start+1 {
		t.Errorf("FindFree = %d, want %d", got, start+1)
	}
}

// The search is bounded: limit is exclusive, so a window whose every port is
// taken reports failure rather than running away up the port range.
func TestFindFreeExhausted(t *testing.T) {
	start := freeRun(t, 2)
	taken := map[int]bool{start: true, start + 1: true}

	if _, err := FindFree(start, start+2, taken); err == nil {
		t.Fatal("expected an error when every port in the window is taken")
	}
}

func freeRun(t *testing.T, span int) int {
	t.Helper()
	for base := 41000; base < 55000; base += span {
		ok := true
		for p := base; p < base+span; p++ {
			if InUse(p) {
				ok = false
				break
			}
		}
		if ok {
			return base
		}
	}
	t.Fatalf("no run of %d free ports found", span)
	return 0
}

// Two vibes creating sandboxes at once each pick ports before either has a record or a listener to show for it: a claim
// that outlives the choosing, and goes when the vibe holding it does, is what keeps them apart. Two Reservations on one
// folder stand for two vibes (a lock is the open file's, so they conflict as two processes would).
func TestAPortAnotherVibeHasReservedIsNotReservedAgain(t *testing.T) {
	dir := t.TempDir()
	one, two := NewReservations(dir), NewReservations(dir)
	defer one.Release()
	defer two.Release()
	port := freeRun(t, 1)
	if ok, err := one.Reserve(port); err != nil || !ok {
		t.Fatalf("first reserve: %v, %v", ok, err)
	}
	if ok, err := two.Reserve(port); err != nil || ok {
		t.Errorf("a port another vibe holds was reserved again: %v, %v", ok, err)
	}
}

func TestAPortAVibeHasReservedIsItsOwnToReserveAgain(t *testing.T) {
	r := NewReservations(t.TempDir())
	defer r.Release()
	port := freeRun(t, 1)
	r.Reserve(port)
	if ok, _ := r.Reserve(port); !ok {
		t.Error("a vibe was refused a port it holds: a rebuild would never get its own port back")
	}
}

func TestAReleasedPortCanBeReservedByAnotherVibe(t *testing.T) {
	dir := t.TempDir()
	one, two := NewReservations(dir), NewReservations(dir)
	defer two.Release()
	port := freeRun(t, 1)
	one.Reserve(port)
	one.Release(port)
	if ok, _ := two.Reserve(port); !ok {
		t.Error("a released port was still held")
	}
}

func TestReleasingOnePortLeavesTheOthersHeld(t *testing.T) {
	dir := t.TempDir()
	one, two := NewReservations(dir), NewReservations(dir)
	defer one.Release()
	defer two.Release()
	start := freeRun(t, 2)
	one.Reserve(start)
	one.Reserve(start + 1)
	one.Release(start)
	if ok, _ := two.Reserve(start + 1); ok {
		t.Error("releasing one port released another")
	}
}

func TestFindFreeWhereSkipsWhatTheAcceptanceRefuses(t *testing.T) {
	start := freeRun(t, 3)
	got, err := FindFreeWhere(start, start+3, nil, func(port int) bool { return port != start })
	if err != nil || got != start+1 {
		t.Errorf("FindFreeWhere = %d, %v, want %d", got, err, start+1)
	}
}
