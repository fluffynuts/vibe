package main

import (
	"fmt"
	"net"
	"testing"

	"vibe/internal/portalloc"
	"vibe/internal/settings"
	"vibe/internal/state"
)

// freeBase returns a port B where B..B+span-1 are all bindable right now, so
// a test can reason about what the allocator picks without depending on which
// ports this host happens to have busy.
func freeBase(t *testing.T, span int) int {
	t.Helper()
	for base := 24000; base < 40000; base += span {
		ok := true
		for p := base; p < base+span; p++ {
			if portalloc.InUse(p) {
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

func publishOne(ports ...int) settings.Settings {
	return settings.Settings{Publish: []settings.PublishEntry{{Name: "dashboard", Ports: ports, UrlEnv: "URL"}}}
}

func hostPorts(mappings []publishMapping) []int {
	out := make([]int, 0, len(mappings))
	for _, m := range mappings {
		out = append(out, m.hostPort)
	}
	return out
}

// allocatePublishFor is allocatePublish with its reservations given up when the test ends: a lock held on a file in the
// test's home would stop Windows removing it.
func allocatePublishFor(t *testing.T, home, name string, merged settings.Settings) ([]publishMapping, error) {
	t.Helper()
	mappings, err := allocatePublish(home, name, merged)
	t.Cleanup(func() { releasePublishReservations(home, mappings) })
	return mappings, err
}

func saveInstance(t *testing.T, home, name string, recs ...state.PublishRecord) {
	t.Helper()
	if err := state.Save(home, state.Instance{Name: name, Profile: "p", Publish: recs}); err != nil {
		t.Fatal(err)
	}
}

// A name with no record takes the container port itself when it is free.
func TestAllocatePublishFreshTakesContainerPort(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 2)

	got, err := allocatePublishFor(t, home, "crit", publishOne(base))
	if err != nil {
		t.Fatalf("allocatePublish: %v", err)
	}
	if want := []int{base}; !equal(hostPorts(got), want) {
		t.Errorf("host ports = %v, want %v", hostPorts(got), want)
	}
	if got[0].urlEnv != "URL" {
		t.Errorf("urlEnv = %q, want URL on the entry's first port", got[0].urlEnv)
	}
}

// A sandbox re-claims the host port its own record remembers, even though
// that port is not the container port and nothing is listening on it. This is
// what keeps a URL stable across a --re-init.
func TestAllocatePublishReclaimsOwnRecordedPort(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 4)
	saveInstance(t, home, "crit", state.PublishRecord{Name: "dashboard", ContainerPort: base, HostPort: base + 2})

	got, err := allocatePublishFor(t, home, "crit", publishOne(base))
	if err != nil {
		t.Fatalf("allocatePublish: %v", err)
	}
	if want := []int{base + 2}; !equal(hostPorts(got), want) {
		t.Errorf("host ports = %v, want %v", hostPorts(got), want)
	}
}

// The regression this consolidation exists for: another sandbox's record
// claims the port, nothing is listening on it because that sandbox is
// stopped, and the new sandbox must still route around it.
func TestAllocatePublishSkipsPortClaimedByAnotherStoppedSandbox(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 4)
	saveInstance(t, home, "valleyrunner", state.PublishRecord{Name: "dashboard", ContainerPort: base, HostPort: base})

	got, err := allocatePublishFor(t, home, "crit", publishOne(base))
	if err != nil {
		t.Fatalf("allocatePublish: %v", err)
	}
	if hostPorts(got)[0] == base {
		t.Fatalf("host port = %d, want anything but valleyrunner's claim", base)
	}
	if want := []int{base + 1}; !equal(hostPorts(got), want) {
		t.Errorf("host ports = %v, want %v", hostPorts(got), want)
	}
}

// Two container ports allocated in one pass must not land on the same host
// port. Nothing is listening on the first pick yet, so only the in-pass
// bookkeeping can prevent the collision.
func TestAllocatePublishNoDuplicateWithinOnePass(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 4)
	saveInstance(t, home, "valleyrunner", state.PublishRecord{Name: "dashboard", ContainerPort: base, HostPort: base})

	// base is claimed, so the first container port slides to base+1 — which
	// is exactly what the second container port would otherwise take.
	got, err := allocatePublishFor(t, home, "crit", publishOne(base, base+1))
	if err != nil {
		t.Fatalf("allocatePublish: %v", err)
	}
	ports := hostPorts(got)
	if ports[0] == ports[1] {
		t.Fatalf("both container ports got host port %d", ports[0])
	}
	if want := []int{base + 1, base + 2}; !equal(ports, want) {
		t.Errorf("host ports = %v, want %v", ports, want)
	}
}

// A live listener is still honoured, including over the sandbox's own record.
func TestAllocatePublishSkipsLiveListener(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 4)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", base))
	if err != nil {
		t.Skipf("could not bind %d: %v", base, err)
	}
	defer ln.Close()

	saveInstance(t, home, "crit", state.PublishRecord{Name: "dashboard", ContainerPort: base, HostPort: base})

	got, err := allocatePublishFor(t, home, "crit", publishOne(base))
	if err != nil {
		t.Fatalf("allocatePublish: %v", err)
	}
	if want := []int{base + 1}; !equal(hostPorts(got), want) {
		t.Errorf("host ports = %v, want %v", hostPorts(got), want)
	}
}

// Only the first port of an entry carries the URL env var.
func TestAllocatePublishUrlEnvOnFirstPortOnly(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 3)

	got, err := allocatePublishFor(t, home, "crit", publishOne(base, base+1))
	if err != nil {
		t.Fatalf("allocatePublish: %v", err)
	}
	if got[0].urlEnv != "URL" || got[1].urlEnv != "" {
		t.Errorf("urlEnv = %q, %q; want URL, \"\"", got[0].urlEnv, got[1].urlEnv)
	}
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The race this exists for: vibes started together choose their ports before any of them has a record to show for it, and
// sbx binds a published port only once it is made, minutes on. Another vibe has chosen the port, and says so by reserving it:
// no record, nothing listening.
func TestAllocatePublishSkipsAPortAnotherVibeIsAboutToUse(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 4)
	other := portalloc.NewReservations(publishReservationsDir(home))
	defer other.Release()
	if ok, err := other.Reserve(base); err != nil || !ok {
		t.Fatalf("reserve: %v, %v", ok, err)
	}

	got, err := allocatePublishFor(t, home, "crit", publishOne(base))
	if err != nil {
		t.Fatal(err)
	}
	if hostPorts(got)[0] == base {
		t.Errorf("host port = %d, which another vibe is about to use", base)
	}
}

func TestTwoVibesAllocatingTogetherGetDifferentPorts(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 4)
	// The second is a vibe of its own: not this process's reservations.
	otherVibe := portalloc.NewReservations(publishReservationsDir(home))
	defer otherVibe.Release()

	first, err := allocatePublishFor(t, home, "one", publishOne(base))
	if err != nil {
		t.Fatal(err)
	}
	second, err := allocateWith(otherVibe, home, "two", publishOne(base))
	if err != nil {
		t.Fatal(err)
	}
	if hostPorts(first)[0] == hostPorts(second)[0] {
		t.Errorf("both were given %d", hostPorts(first)[0])
	}
}

// A vibe that remakes its own sandbox is not kept from the port it holds.
func TestAllocatePublishAgainInTheSameVibeKeepsItsOwnPorts(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 4)
	first, _ := allocatePublishFor(t, home, "crit", publishOne(base))
	second, err := allocatePublishFor(t, home, "crit", publishOne(base))
	if err != nil || hostPorts(first)[0] != hostPorts(second)[0] {
		t.Errorf("first %v, then %v, %v", hostPorts(first), hostPorts(second), err)
	}
}

func TestReleasedPublishPortsCanBeChosenByAnotherVibe(t *testing.T) {
	home := t.TempDir()
	base := freeBase(t, 4)
	mappings, _ := allocatePublish(home, "one", publishOne(base))
	releasePublishReservations(home, mappings)
	other := portalloc.NewReservations(publishReservationsDir(home))
	defer other.Release()
	if ok, _ := other.Reserve(base); !ok {
		t.Error("the port is still held after its sandbox was made (or failed to be)")
	}
}
