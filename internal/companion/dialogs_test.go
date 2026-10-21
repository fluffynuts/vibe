package companion

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// browserDialog finds the browser's own alert, confirm and prompt, which the
// page never uses: it asks in its own dialogs, so every question looks and
// behaves the same, and a browser can't suppress one (it does, in the frame
// the clipboard page is shown in).
var browserDialog = regexp.MustCompile(`(^|[^A-Za-z0-9_$.])(window\.)?(alert|confirm|prompt)\s*\(`)

func TestThePageAsksInItsOwnDialogsNeverTheBrowsers(t *testing.T) {
	files, err := filepath.Glob("ui/*")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "../../library/base/clipboard-bridge/agent-files/.local/share/vibe/clipboard-page.html")
	checked := 0
	for _, file := range files {
		if ext := filepath.Ext(file); ext != ".js" && ext != ".html" {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		checked++
		for i, line := range strings.Split(string(content), "\n") {
			if code := strings.TrimSpace(line); strings.HasPrefix(code, "//") || strings.HasPrefix(code, "*") || strings.HasPrefix(code, "/*") {
				continue
			}
			if browserDialog.MatchString(line) {
				t.Errorf("%s:%d uses a browser dialog; ask in the page's own dialog instead: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
	if checked < 5 {
		t.Fatalf("only %d page files checked; has the page moved?", checked)
	}
}
