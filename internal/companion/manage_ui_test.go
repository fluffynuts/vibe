package companion

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestTheProfilesTabsTicks runs the page's own tests for ui/manage.js with
// node, so `go test` covers them; without node, it says so and skips.
func TestTheProfilesTabsTicks(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node isn't installed: the Profiles tab's tests need it")
	}
	out, err := exec.Command(node, "--test", filepath.Join("uitest", "manage.test.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("node --test uitest/manage.test.js failed: %v\n%s", err, out)
	}
}
