package companion

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestTheReviewTabsPlacing runs the page's own tests for ui/review.js with
// node, so `go test` covers them; without node, it says so and skips.
func TestTheReviewTabsPlacing(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node isn't installed: the Review tab's tests need it")
	}
	out, err := exec.Command(node, "--test", filepath.Join("uitest", "review.test.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("node --test uitest/review.test.js failed: %v\n%s", err, out)
	}
}
