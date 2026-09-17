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
