package companion

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// getTestDeps is what to run to get what the page's tests need (jsdom, and the Firefox that the pinned Playwright runs):
// the same target in each of the three build scripts. The tests that need either fail, saying so, and never skip: a
// skipped test passes, and a green run that didn't run them went unnoticed.
const getTestDeps = "run `make test-deps` (`./make.sh test-deps`, or `./make.ps1 test-deps` on Windows)"

// nodeMissing is what a page test says when node isn't installed (jsdom wants 22.22, 24.15 or 26 and later).
func nodeMissing(whose string) string {
	return "node isn't installed, and " + whose + " tests need it (22.22 or later): install it, then " + getTestDeps
}

// staleDeps says, of a uitest folder, which of the dependencies its package.json pins aren't installed at that version
// (jsdom and Playwright are pinned exactly). node_modules follows no branch, so one that pins another version than the
// one before it leaves the tests running against the wrong one. Nothing is returned when all are as pinned.
func staleDeps(dir string) []string {
	var pinned struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err == nil {
		err = json.Unmarshal(data, &pinned)
	}
	if err != nil {
		return []string{fmt.Sprintf("%s's package.json can't be read (%v)", dir, err)}
	}
	var stale []string
	for name, want := range pinned.DevDependencies {
		var installed struct {
			Version string `json:"version"`
		}
		data, err := os.ReadFile(filepath.Join(dir, "node_modules", name, "package.json"))
		if err == nil {
			err = json.Unmarshal(data, &installed)
		}
		switch {
		case err != nil:
			stale = append(stale, fmt.Sprintf("%s (not installed, pinned %s)", name, want))
		case installed.Version != want:
			stale = append(stale, fmt.Sprintf("%s (installed %s, pinned %s)", name, installed.Version, want))
		}
	}
	sort.Strings(stale)
	return stale
}
