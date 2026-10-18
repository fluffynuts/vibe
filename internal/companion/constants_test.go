package companion

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// jsConst is the value of `const name = ...` in the script at path: a
// number, or numbers multiplied.
func jsConst(t *testing.T, path, name string) int64 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s*const ` + name + ` = ([0-9* ]+);?\s*$`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatalf("%s has no const %s made of numbers", path, name)
	}
	value := int64(1)
	for _, factor := range strings.Split(m[1], "*") {
		n, err := strconv.ParseInt(strings.TrimSpace(factor), 10, 64)
		if err != nil {
			t.Fatalf("%s's %s: %v", path, name, err)
		}
		value *= n
	}
	return value
}

// What the page and the sandbox's scripts must agree with vibe on is said
// in each language; this keeps them saying the same.
func TestTheScriptsAgreeWithVibe(t *testing.T) {
	app := filepath.Join("ui", "app.js")
	clipboard := filepath.Join("..", "..", "library", "base", "clipboard-bridge", "agent-files", ".local", "share", "vibe", "clipboard-server.js")
	ms := func(d time.Duration) int64 { return d.Milliseconds() }

	if got := jsConst(t, app, "AWAKE_MS"); got != ms(workingAwakeEvery) {
		t.Errorf("the page keeps the sandbox awake every %d ms, vibe while the agent works every %d ms", got, ms(workingAwakeEvery))
	}
	if got := jsConst(t, clipboard, "MAX_BYTES"); got != maxPaste {
		t.Errorf("the clipboard page takes %d bytes, vibe passes on %d", got, int64(maxPaste))
	}
	// The page's backstops are past vibe's own.
	for _, name := range []string{"FRESH_OPTIONS_MS", "FRESH_FETCH_MS", "FRESH_START_MS"} {
		if got := jsConst(t, app, name); got <= ms(fetchWithin) {
			t.Errorf("the page gives up on %s after %d ms, before vibe's fetch would (%d ms)", name, got, ms(fetchWithin))
		}
	}
}
