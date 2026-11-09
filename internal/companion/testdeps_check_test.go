package companion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uitestFolder makes a folder like uitest: package.json pinning wanted, and node_modules holding installed (a package not
// listed in installed is not installed).
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

// The node versions the page tests' messages name are the ones in the engines of uitest/package.json, whichever jsdom the branch
// pins (22.22, 24.15 and 26 for jsdom 30, which is not the one on master): read from the file, so that they cannot differ from it.
func TestTheNodeRangeTheMessagesNameIsTheOneThePackageAsksFor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"engines": {"node": "^22.22.2 || ^24.15.0 || >=26"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := nodeRangeIn(dir); !strings.Contains(got, "^22.22.2 || ^24.15.0 || >=26") {
		t.Errorf("the range is %q, which doesn't carry the engines", got)
	}
	data, err := os.ReadFile(filepath.Join("uitest", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil || pkg.Engines.Node == "" {
		t.Fatalf("uitest/package.json has no engines.node to name: %v", err)
	}
	if !strings.Contains(nodeMissing("the page's"), pkg.Engines.Node) {
		t.Errorf("nodeMissing doesn't name the engines %q: %s", pkg.Engines.Node, nodeMissing("the page's"))
	}
}

func TestWithNoEnginesToReadTheMessageSaysWhereToLookInstead(t *testing.T) {
	for _, dir := range []string{t.TempDir(), func() string {
		d := t.TempDir()
		os.WriteFile(filepath.Join(d, "package.json"), []byte(`{"name": "x"}`), 0o644)
		return d
	}()} {
		if got := nodeRangeIn(dir); !strings.Contains(got, "engines of uitest/package.json") || strings.Contains(got, "node ") {
			t.Errorf("the range is %q, which names versions that weren't read", got)
		}
	}
}

// staleDeps (testdeps_test.go) compares what is installed with what package.json pins, which only means "stale" while every pin is a
// plain version. The pins are ours (devDependencies, installed by make test-deps and CI), so they are kept exact, and this says when
// one isn't.
func TestEveryPinInUitestPackageJSONIsAPlainVersion(t *testing.T) {
	if problems := inexactPins("uitest"); len(problems) != 0 {
		t.Errorf("uitest/package.json pins what staleDeps can't compare with what is installed:\n%s", strings.Join(problems, "\n"))
	}
}

func TestAPinThatIsNotAPlainVersionIsNamedWithWhyItMatters(t *testing.T) {
	dir := t.TempDir()
	pins := `{"devDependencies": {"jsdom": "^30.1", "playwright": "1.64.1", "tagged": "latest", "local": "file:../x", "tilde": "~1.2.3",
		"gte": ">=1", "wild": "1.x", "pre": "1.2.3-beta.1", "two": "1.2"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pins), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(inexactPins(dir), "\n")
	for _, bad := range []string{`jsdom ("^30.1")`, `tagged ("latest")`, `local ("file:../x")`, `tilde ("~1.2.3")`, `gte (">=1")`, `wild ("1.x")`, `two ("1.2")`} {
		if !strings.Contains(got, bad) {
			t.Errorf("%s isn't named:\n%s", bad, got)
		}
	}
	for _, fine := range []string{"playwright", "pre ("} {
		if strings.Contains(got, fine) {
			t.Errorf("%s was refused, but is a plain version:\n%s", fine, got)
		}
	}
	if !strings.Contains(got, "plain version") || !strings.Contains(got, "fail for ever") {
		t.Errorf("it doesn't say what a plain version is, or why one is wanted:\n%s", got)
	}
}

func TestPinsThatCantBeReadAreSaidNotPassed(t *testing.T) {
	if got := inexactPins(t.TempDir()); len(got) != 1 || !strings.Contains(got[0], "package.json") {
		t.Errorf("inexactPins = %v, want one line about the package.json that can't be read", got)
	}
}
