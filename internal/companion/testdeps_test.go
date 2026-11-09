package companion

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// getTestDeps is what to run to get what the page's tests need (jsdom, and the Firefox that the pinned Playwright runs):
// the same target in each of the three build scripts. The tests that need either fail, saying so, and never skip: a
// skipped test passes, and a green run that didn't run them went unnoticed.
const getTestDeps = "run `make test-deps` (`./make.sh test-deps`, or `./make.ps1 test-deps` on Windows)"

// nodeRangeIn is the node versions the page's tests run on, as the engines of dir's package.json say (what the pinned jsdom asks
// for): read from there, not written here, so that it is right on every branch, whichever jsdom it pins.
func nodeRangeIn(dir string) string {
	var pkg struct {
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err == nil {
		err = json.Unmarshal(data, &pkg)
	}
	if err != nil || pkg.Engines.Node == "" {
		return "the versions in the engines of uitest/package.json"
	}
	return "node " + pkg.Engines.Node + ", the engines of uitest/package.json"
}

// nodeMissing is what a page test says when node isn't installed.
func nodeMissing(whose string) string {
	return "node isn't installed, and " + whose + " tests need it (" + nodeRangeIn("uitest") + "): install it, then " + getTestDeps
}

// plainVersion is a pin staleDeps can compare with an installed version: major.minor.patch, with a prerelease tag if it has one.
var plainVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// inexactPins says, of a uitest folder, which of the dependencies its package.json pins aren't a plain version, and why that
// matters. Nothing is returned when all are.
func inexactPins(dir string) []string {
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
	var problems []string
	for name, pin := range pinned.DevDependencies {
		if !plainVersion.MatchString(pin) {
			problems = append(problems, fmt.Sprintf("%s (%q) is not a plain version (major.minor.patch): the check for what is installed compares it with the version installed, so a range or a tag would fail for ever", name, pin))
		}
	}
	sort.Strings(problems)
	return problems
}

// staleDeps says, of a uitest folder, which of the dependencies its package.json pins aren't installed at that version
// (jsdom and Playwright are pinned exactly). node_modules follows no branch, so one that pins another version than the
// one before it leaves the tests running against the wrong one. Nothing is returned when all are as pinned.
//
// "Installed at the pinned version" means the pin is a plain version: a range (^30.1) would fail for ever, as 30.1.3 is not
// "^30.1". They are plain because the pins are ours (devDependencies, installed by make test-deps and CI, no one else's range to
// honour), and inexactPins, which a test runs over uitest/package.json, says when one isn't. Comparing with the lockfile instead
// would need each package manager's own; ranges would need a semver library.
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
