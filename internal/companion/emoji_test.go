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
		t.Skip("node isn't installed: the page's emoji tests need it")
	}
	out, err := exec.Command(node, "--test", filepath.Join("uitest", "emoji.test.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("node --test uitest/emoji.test.js failed: %v\n%s", err, out)
	}
}
