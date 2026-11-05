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

// ownRunners are the page tests with a Go test of their own (and Firefox's, which needs a browser): every other
// uitest/*.test.js is run by TestThePageItself, so a new one runs without anyone remembering to list it.
var ownRunners = map[string]bool{
	"review.test.js": true, "emoji.test.js": true, "markdown.test.js": true, "manage.test.js": true, "firefox.test.js": true, "commentspane.firefox.test.js": true,
}

// TestThePageItself runs the tests of the page as a whole (every uitest/*.test.js but ownRunners), in jsdom.
// jsdom is installed with `npm ci` in uitest: without it they're skipped here, saying how to get it, but never in
// CI, where skipping would pass them unseen.
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
	files, err := filepath.Glob(filepath.Join("uitest", "*.test.js"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no page tests found in uitest: %v", err)
	}
	present := map[string]bool{}
	var run []string
	for _, file := range files {
		name := filepath.Base(file)
		present[name] = true
		if !ownRunners[name] {
			run = append(run, file)
		}
	}
	for name := range ownRunners {
		if !present[name] {
			t.Errorf("ownRunners names %s, which is not in uitest: remove it", name)
		}
	}
	// One node, which runs the files side by side: the page tests are many, and each is quick.
	out, err := exec.Command(node, append([]string{"--test"}, run...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("node --test failed: %v\n%s", err, out)
	}
}
