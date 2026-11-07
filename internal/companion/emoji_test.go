package companion

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestThePagesEmojiNames runs the page's own tests for ui/emoji.js with node, so `go test` covers them; without node, it
// says so and skips.
func TestThePagesEmojiNames(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(nodeMissing("the page's emoji"))
	}
	out, err := exec.Command(node, "--test", filepath.Join("uitest", "emoji.test.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("node --test uitest/emoji.test.js failed: %v\n%s", err, out)
	}
}
