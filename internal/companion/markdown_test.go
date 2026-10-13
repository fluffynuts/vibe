package companion

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestThePagesMarkdownReader runs the page's own tests for ui/markdown.js
// with node, so `go test` covers them; without node, it says so and skips.
func TestThePagesMarkdownReader(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node isn't installed: the page's Markdown tests need it")
	}
	out, err := exec.Command(node, "--test", filepath.Join("uitest", "markdown.test.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("node --test uitest/markdown.test.js failed: %v\n%s", err, out)
	}
}
