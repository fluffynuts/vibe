package nugetfetch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// assetsJSON is a project.assets.json as NuGet writes one, cut down: the
// packages each target framework resolved, which are what matter here.
func assetsJSON(targets map[string][]string) string {
	t := map[string]map[string]map[string]string{}
	libs := map[string]map[string]string{}
	for tfm, entries := range targets {
		t[tfm] = map[string]map[string]string{}
		for _, e := range entries {
			kind := "package"
			if strings.HasPrefix(e, "project:") {
				kind, e = "project", strings.TrimPrefix(e, "project:")
			}
			t[tfm][e] = map[string]string{"type": kind}
			libs[e] = map[string]string{"type": kind, "path": strings.ToLower(e)}
		}
	}
	out, _ := json.Marshal(map[string]interface{}{"version": 3, "targets": t, "libraries": libs})
	return string(out)
}

func writeAssets(t *testing.T, dir, content string) string {
	t.Helper()
	obj := filepath.Join(dir, "obj")
	if err := os.MkdirAll(obj, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(obj, "project.assets.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// assetsWithDownloads is assetsJSON for a project that also has packages it only downloads
// (PackageDownload: targeting packs, say), which NuGet keeps apart from what it resolved, as
// project.frameworks.<framework>.downloadDependencies with an exact version range.
func assetsWithDownloads(targets, downloads map[string][]string) string {
	var doc map[string]interface{}
	json.Unmarshal([]byte(assetsJSON(targets)), &doc)
	frameworks := map[string]map[string]interface{}{}
	for tfm, entries := range downloads {
		var deps []map[string]string
		for _, e := range entries {
			id, version, _ := strings.Cut(e, "/")
			deps = append(deps, map[string]string{"name": id, "version": "[" + version + ", " + version + "]"})
		}
		frameworks[tfm] = map[string]interface{}{"downloadDependencies": deps}
	}
	doc["project"] = map[string]interface{}{"frameworks": frameworks}
	out, _ := json.Marshal(doc)
	return string(out)
}

func TestPackagesOnlyDownloadedAreReadWithTheirExactVersions(t *testing.T) {
	path := writeAssets(t, t.TempDir(), assetsWithDownloads(
		map[string][]string{"net8.0": {"Some.Lib/1.0.0"}},
		map[string][]string{"net8.0": {"Targeting.Pack.Ref/8.0.32", "Newtonsoft.Json/12.0.1"}}))
	got, err := ReadAssets(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{{"Newtonsoft.Json", "12.0.1"}, {"Targeting.Pack.Ref", "8.0.32"}}
	if !reflect.DeepEqual(got.Downloads["net8.0"], want) {
		t.Errorf("downloads %v, want %v", got.Downloads["net8.0"], want)
	}
	if !reflect.DeepEqual(got.ByFramework["net8.0"], []Package{{"Some.Lib", "1.0.0"}}) {
		t.Errorf("what it resolved is %v: the downloads are kept apart", got.ByFramework["net8.0"])
	}
}

func TestADownloadThatIsNotOneExactVersionOrNotAPackageIsSkipped(t *testing.T) {
	doc := `{"version":3,"targets":{"net8.0":{}},"project":{"frameworks":{"net8.0":{"downloadDependencies":[` +
		`{"name":"Range.Pkg","version":"[1.0.0, 2.0.0]"},` +
		`{"name":"Open.Pkg","version":"1.0.0"},` +
		`{"name":"Bad\"/><x","version":"[1.0.0, 1.0.0]"},` +
		`{"name":"One.Version","version":"[3.0.0]"}]},` +
		`"net8.0-windows":{"downloadDependencies":[{"name":"Other.Pkg","version":"[1.0.0, 1.0.0]"}]}}}}`
	path := writeAssets(t, t.TempDir(), doc)
	got, err := ReadAssets(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Package{{"One.Version", "3.0.0"}}; !reflect.DeepEqual(got.Downloads["net8.0"], want) {
		t.Errorf("downloads %v, want %v", got.Downloads["net8.0"], want)
	}
	if len(got.Downloads) != 1 || got.Skipped != 4 {
		t.Errorf("downloads %v, skipped %d: want one framework and 4 skipped", got.Downloads, got.Skipped)
	}
}

func TestTheAssetsGiveTheExactPackagesEachFrameworkResolved(t *testing.T) {
	path := writeAssets(t, t.TempDir(), assetsJSON(map[string][]string{
		"net8.0": {"Newtonsoft.Json/13.0.3", "AWSSDK.Core/4.0.102.6", "Mono.Unix/7.1.0-final.1.21458.1"},
		"net462": {"Newtonsoft.Json/13.0.3", "Legacy.Lib/1.2.3.4"},
	}))
	got, err := ReadAssets(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]Package{
		"net8.0": {{"AWSSDK.Core", "4.0.102.6"}, {"Mono.Unix", "7.1.0-final.1.21458.1"}, {"Newtonsoft.Json", "13.0.3"}},
		"net462": {{"Legacy.Lib", "1.2.3.4"}, {"Newtonsoft.Json", "13.0.3"}},
	}
	if !reflect.DeepEqual(got.ByFramework, want) {
		t.Errorf("packages = %v\nwant %v", got.ByFramework, want)
	}
}

func TestProjectReferencesAreNotPackages(t *testing.T) {
	path := writeAssets(t, t.TempDir(), assetsJSON(map[string][]string{"net8.0": {"project:Other.Project/1.0.0", "Real.Pkg/2.0.0"}}))
	got, _ := ReadAssets(path)
	if !reflect.DeepEqual(got.ByFramework["net8.0"], []Package{{"Real.Pkg", "2.0.0"}}) {
		t.Errorf("packages = %v", got.ByFramework)
	}
}

// What the sandbox wrote isn't to be trusted: only what's a package id, an exact
// version and a framework is carried on, and the rest is counted, not used.
func TestWhatIsNotAValidIdVersionOrFrameworkIsSkippedAndCounted(t *testing.T) {
	path := writeAssets(t, t.TempDir(), assetsJSON(map[string][]string{
		"net8.0":                   {"Good.Pkg/1.0.0", `Bad"/><Evil/1.0.0`, "../escape/1.0.0", "Range.Pkg/[1.0,2.0)", "Float.Pkg/1.*", "NoVersion"},
		"net8.0/linux-x64":         {"Rid.Only/1.0.0"},
		"net8.0;evil":              {"Evil.Framework/1.0.0"},
		"../../etc":                {"Path.Framework/1.0.0"},
		".NETCoreApp,Version=v8.0": {"Long.Form/1.0.0"},
	}))
	got, err := ReadAssets(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.ByFramework, map[string][]Package{"net8.0": {{"Good.Pkg", "1.0.0"}}}) {
		t.Errorf("packages = %v, want only Good.Pkg in net8.0", got.ByFramework)
	}
	if got.Skipped < 5 {
		t.Errorf("Skipped = %d, want what wasn't taken to be counted", got.Skipped)
	}
}

func TestAnAssetsFileThatIsNotJSONOrNotAnAssetsFileIsAnError(t *testing.T) {
	for _, content := range []string{"not json", "[]", `{"targets": "nope"}`, ""} {
		if _, err := ReadAssets(writeAssets(t, t.TempDir(), content)); err == nil {
			t.Errorf("accepted %q", content)
		}
	}
}

func TestAnAssetsFileOverTheLimitIsNotRead(t *testing.T) {
	path := writeAssets(t, t.TempDir(), strings.Repeat(" ", maxAssetsFile+1))
	if _, err := ReadAssets(path); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("err = %v, want it too large", err)
	}
}

func TestAnAssetsFileThatIsALinkIsNotFollowed(t *testing.T) {
	dir := t.TempDir()
	target := writeAssets(t, filepath.Join(dir, "real"), assetsJSON(map[string][]string{"net8.0": {"A.B/1.0.0"}}))
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, err := ReadAssets(link); err == nil {
		t.Error("followed a link")
	}
}

func TestSoManyPackagesOrFrameworksIsTakenAsAbuseAndStopsAtTheLimit(t *testing.T) {
	var many []string
	for i := 0; i < maxPackages+10; i++ {
		many = append(many, "Pkg.N"+itoa(i)+"/1.0.0")
	}
	got, err := ReadAssets(writeAssets(t, t.TempDir(), assetsJSON(map[string][]string{"net8.0": many})))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.ByFramework["net8.0"]); n != maxPackages {
		t.Errorf("took %d packages, want the limit %d", n, maxPackages)
	}
	if got.Skipped < 10 {
		t.Errorf("Skipped = %d, want the ones past the limit counted", got.Skipped)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestAssetsFilesAreFoundInEveryProjectsObjFolder(t *testing.T) {
	dir := t.TempDir()
	a := writeAssets(t, filepath.Join(dir, "src", "A"), "{}")
	b := writeAssets(t, filepath.Join(dir, "B"), "{}")
	writeAssets(t, filepath.Join(dir, "node_modules", "x"), "{}")
	writeAssets(t, filepath.Join(dir, ".git", "x"), "{}")
	if err := os.WriteFile(filepath.Join(dir, "project.assets.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := FindAssets(dir)
	if len(got) != 2 {
		t.Fatalf("found %v, want just %s and %s: not outside an obj folder, nor in node_modules or .git", got, a, b)
	}
}

func TestOnlyFrameworkFoldersUnderTheLimitAreSearched(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < maxAssetsFiles+20; i++ {
		writeAssets(t, filepath.Join(dir, "p"+itoa(i)), "{}")
	}
	if got := FindAssets(dir); len(got) != maxAssetsFiles {
		t.Errorf("found %d, want the limit %d", len(got), maxAssetsFiles)
	}
}

func TestAPackageIsInTheCacheOnceItsMetadataIsThere(t *testing.T) {
	cache := t.TempDir()
	done := filepath.Join(cache, "newtonsoft.json", "13.0.3")
	partial := filepath.Join(cache, "partial.pkg", "1.0.0")
	for _, d := range []string{done, partial} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(done, ".nupkg.metadata"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(partial, "partial.pkg.1.0.0.nupkg"), []byte("half"), 0o644) // no metadata: not whole
	pkgs := []Package{{"Newtonsoft.Json", "13.0.3"}, {"Partial.Pkg", "1.0.0"}, {"Absent.Pkg", "2.0.0"}}
	got := Missing(cache, pkgs)
	if !reflect.DeepEqual(got, []Package{{"Partial.Pkg", "1.0.0"}, {"Absent.Pkg", "2.0.0"}}) {
		t.Errorf("missing = %v", got)
	}
}

func TestTheCacheIsKeptInLowerCaseWhateverTheAssetsSay(t *testing.T) {
	cache := t.TempDir()
	dir := filepath.Join(cache, "awssdk.core", "4.0.102.6-beta")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, ".nupkg.metadata"), []byte("{}"), 0o644)
	if got := Missing(cache, []Package{{"AWSSDK.Core", "4.0.102.6-Beta"}}); len(got) != 0 {
		t.Errorf("missing = %v: found by its lower-case folder", got)
	}
}

// The folders it's given are the caller's: a caller passing a slice's elements finds them as they were.
func TestReadingNamedPackagesLeavesTheFoldersItWasGivenAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.nuget.cache")
	if err := os.WriteFile(path, []byte(`{"expectedPackageFiles":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	roots := []string{"/Home/User/.nuget/packages/", `C:\Users\Me\.nuget\packages`}
	want := append([]string(nil), roots...)
	if _, err := ReadNamedElsewhere(path, roots...); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roots, want) {
		t.Errorf("the caller's folders became %v", roots)
	}
}
