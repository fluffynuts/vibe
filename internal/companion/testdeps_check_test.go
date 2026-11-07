package companion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uitestFolder makes a folder like uitest: package.json pinning wanted, and node_modules holding installed (a package
// whose version is "" is left out of node_modules).
func uitestFolder(t *testing.T, wanted, installed map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	var pins []string
	for name, version := range wanted {
		pins = append(pins, `"`+name+`": "`+version+`"`)
	}
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "package.json"), `{"devDependencies": {`+strings.Join(pins, ",")+`}}`)
	for name, version := range installed {
		write(filepath.Join(dir, "node_modules", name, "package.json"), `{"name": "`+name+`", "version": "`+version+`"}`)
	}
	return dir
}

// node_modules follows no branch: after switching to one that pins jsdom 24 with 30 installed, the page tests failed with
// "virtualConsole.sendTo is not a function", which says nothing of the cause.
func TestDependenciesInstalledAtOtherVersionsThanPinnedAreSaid(t *testing.T) {
	dir := uitestFolder(t,
		map[string]string{"jsdom": "24.1.3", "playwright": "1.64.1", "left-out": "1.0.0"},
		map[string]string{"jsdom": "30.1.3", "playwright": "1.64.1"})
	got := strings.Join(staleDeps(dir), "; ")
	for _, want := range []string{"jsdom (installed 30.1.3, pinned 24.1.3)", "left-out (not installed, pinned 1.0.0)"} {
		if !strings.Contains(got, want) {
			t.Errorf("staleDeps = %q, which doesn't say %q", got, want)
		}
	}
	if strings.Contains(got, "playwright") {
		t.Errorf("staleDeps = %q, which blames playwright, installed as pinned", got)
	}
}

func TestDependenciesInstalledAsPinnedAreNotStale(t *testing.T) {
	dir := uitestFolder(t, map[string]string{"jsdom": "30.1.3"}, map[string]string{"jsdom": "30.1.3"})
	if got := staleDeps(dir); len(got) != 0 {
		t.Errorf("staleDeps = %v for what is installed as pinned", got)
	}
}

func TestAFolderWithNoPackageJSONHasNothingPinnedToBeStaleAgainst(t *testing.T) {
	if got := staleDeps(t.TempDir()); len(got) != 1 || !strings.Contains(got[0], "package.json") {
		t.Errorf("staleDeps = %v, want one line about the missing package.json", got)
	}
}
