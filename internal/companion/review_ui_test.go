package companion

import (
	"os"
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

// TestThePageItself runs the tests of the page as a whole (uitest/page.test.js),
// and of the clipboard bridge's page beside it (uitest/clipboard-page.test.js),
// in jsdom. jsdom is installed with `npm ci` in uitest: without it they're
// skipped here, saying how to get it, but never in CI, where skipping would
// pass them unseen.
func TestThePageItself(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node isn't installed: the page's tests need it")
	}
	if _, err := os.Stat(filepath.Join("uitest", "node_modules", "jsdom")); err != nil {
		const how = "the page's tests need jsdom: run `npm ci` in internal/companion/uitest"
		if os.Getenv("CI") != "" {
			t.Fatal(how)
		}
		t.Skip(how)
	}
	for _, file := range []string{"page.test.js", "packages.test.js", "polling.test.js", "usage.test.js", "blocked.test.js", "light.test.js", "choice.test.js", "notes.test.js", "sync.test.js", "sendcheck.test.js", "stale.test.js", "search.test.js", "wantsyou.test.js", "clipboard-page.test.js", "fileview.test.js", "toolgroups.test.js", "gitauthor.test.js"} {
		out, err := exec.Command(node, "--test", filepath.Join("uitest", file)).CombinedOutput()
		if err != nil {
			t.Fatalf("node --test uitest/%s failed: %v\n%s", file, err, out)
		}
	}
}
