package nugetfetch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTheBatchProjectPinsEveryPackageAtItsExactVersionForOneFramework(t *testing.T) {
	xml := BatchProjectXML("net462", []Package{{"B.Lib", "2.0.0-beta.1"}, {"A.Lib", "1.2.3"}})
	for _, want := range []string{
		`<TargetFramework>net462</TargetFramework>`,
		`<PackageReference Include="A.Lib" Version="[1.2.3]" />`,
		`<PackageReference Include="B.Lib" Version="[2.0.0-beta.1]" />`,
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("project lacks %q:\n%s", want, xml)
		}
	}
	if strings.Index(xml, "A.Lib") > strings.Index(xml, "B.Lib") {
		t.Error("references aren't in order, so the same batch isn't the same project twice")
	}
}

// The batch is only to get these exact versions into the cache: a vulnerability NuGet
// would tell the project's owner of, or a direct reference lower than what a package
// wants, is no reason to fetch none of them.
func TestTheBatchProjectDoesNotAuditNorFailOnADowngrade(t *testing.T) {
	xml := BatchProjectXML("net8.0", []Package{{"A.Lib", "1.0.0"}})
	for _, want := range []string{`<NuGetAudit>false</NuGetAudit>`, `NU1605`} {
		if !strings.Contains(xml, want) {
			t.Errorf("project lacks %q:\n%s", want, xml)
		}
	}
}

func TestAnExactVersionLowerThanAPackageWantsIsStillFetched(t *testing.T) {
	h := newHost(t)
	addPackage(t, h.feed, "Demo.Lib", "1.0.0", nil)
	addPackage(t, h.feed, "Demo.Lib", "2.0.0", nil)
	addPackage(t, h.feed, "Demo.Needs", "1.0.0", map[string]string{"Demo.Lib": "2.0.0"})
	res, err := FetchBatch(context.Background(), "net10.0", []Package{{"Demo.Lib", "1.0.0"}, {"Demo.Needs", "1.0.0"}}, h.options())
	if err != nil {
		t.Fatalf("FetchBatch: %v\n%s", err, res.Output)
	}
	if len(res.Fetched) != 2 {
		t.Errorf("fetched %v", res.Fetched)
	}
}

func fetchTargets(ids ...string) []Package {
	var out []Package
	for _, id := range ids {
		out = append(out, Package{id, "1.0.0"})
	}
	return out
}

func TestABatchFetchesManyPackagesInOneRestore(t *testing.T) {
	h := newHost(t)
	res, err := FetchBatch(context.Background(), "net10.0", fetchTargets("Demo.Pkg", "Demo.Two", "Demo.Three"), h.options())
	if err != nil {
		t.Fatalf("FetchBatch: %v\n%s", err, res.Output)
	}
	for _, id := range []string{"demo.pkg", "demo.two", "demo.three"} {
		if _, err := os.Stat(filepath.Join(h.cache, id, "1.0.0", ".nupkg.metadata")); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	if len(res.Fetched) != 3 || len(res.Unresolved) != 0 {
		t.Errorf("fetched %v, unresolved %v", res.Fetched, res.Unresolved)
	}
	if left, _ := os.ReadDir(h.root); len(left) != 0 {
		t.Errorf("throwaway folder left behind: %v", left)
	}
}

// One package no feed has mustn't keep the rest from coming: NuGet names it,
// and it's set aside and said, with the rest fetched.
func TestAPackageNoFeedHasIsNamedAndTheRestStillComes(t *testing.T) {
	h := newHost(t)
	res, err := FetchBatch(context.Background(), "net10.0", fetchTargets("Demo.Pkg", "Nope.Pkg", "Demo.Two"), h.options())
	if err != nil {
		t.Fatalf("FetchBatch: %v\n%s", err, res.Output)
	}
	if !reflect.DeepEqual(res.Unresolved, []Package{{"Nope.Pkg", "1.0.0"}}) {
		t.Errorf("unresolved = %v", res.Unresolved)
	}
	for _, id := range []string{"demo.pkg", "demo.two"} {
		if _, err := os.Stat(filepath.Join(h.cache, id, "1.0.0")); err != nil {
			t.Errorf("%s wasn't fetched: %v", id, err)
		}
	}
}

// NuGet's words about a package no feed has are kept with it, for the request that asked.
func TestWhatNuGetSaidOfAPackageNoFeedHasIsKept(t *testing.T) {
	h := newHost(t)
	res, err := FetchBatch(context.Background(), "net10.0", []Package{{"Demo.Pkg", "1.0.0"}, {"Nope.Pkg", "1.0.0"}}, h.options())
	if err != nil {
		t.Fatalf("FetchBatch: %v\n%s", err, res.Output)
	}
	why := res.UnresolvedWhy[Package{"Nope.Pkg", "1.0.0"}]
	if !strings.Contains(why, "NU1101") || !strings.Contains(why, "Nope.Pkg") || strings.Contains(why, "Demo.Pkg") {
		t.Errorf("why = %q", why)
	}
}

func TestSeveralUnresolvedPackagesAreAllNamed(t *testing.T) {
	h := newHost(t)
	res, err := FetchBatch(context.Background(), "net10.0", fetchTargets("Demo.Pkg", "Nope.One", "Nope.Two"), h.options())
	if err != nil {
		t.Fatalf("FetchBatch: %v\n%s", err, res.Output)
	}
	if len(res.Unresolved) != 2 {
		t.Errorf("unresolved = %v", res.Unresolved)
	}
}

func TestABatchOfNothingTheFeedsHaveIsAllUnresolvedNotAnError(t *testing.T) {
	h := newHost(t)
	res, err := FetchBatch(context.Background(), "net10.0", fetchTargets("Nope.One"), h.options())
	if err != nil || len(res.Unresolved) != 1 || len(res.Fetched) != 0 {
		t.Errorf("fetched %v, unresolved %v, err %v", res.Fetched, res.Unresolved, err)
	}
}

func TestABatchWithAnInvalidPackageOrFrameworkRunsNothing(t *testing.T) {
	h := newHost(t)
	if _, err := FetchBatch(context.Background(), "net10.0", []Package{{`A"/><x`, "1.0.0"}}, h.options()); err == nil {
		t.Error("an invalid package id was run")
	}
	if _, err := FetchBatch(context.Background(), "net10.0;evil", fetchTargets("Demo.Pkg"), h.options()); err == nil {
		t.Error("an invalid framework was run")
	}
	if _, err := FetchBatch(context.Background(), "net10.0", nil, h.options()); err == nil {
		t.Error("an empty batch was run")
	}
	if entries, _ := os.ReadDir(h.root); len(entries) != 0 {
		t.Errorf("something was made: %v", entries)
	}
}

func TestABatchRunsNothingPlantedAboveItsFolder(t *testing.T) {
	h := newHost(t)
	marker := filepath.Join(t.TempDir(), "ran")
	plant := `<Project><Target Name="Pwn" BeforeTargets="Restore;_GenerateRestoreGraph"><Exec Command="touch ` + marker + `" /></Target></Project>`
	os.WriteFile(filepath.Join(filepath.Dir(h.root), "Directory.Build.targets"), []byte(plant), 0o644)
	if _, err := FetchBatch(context.Background(), "net10.0", fetchTargets("Demo.Pkg"), h.options()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a Directory.Build file above the batch's folder ran on the host")
	}
}

// NuGet can fail on one package for reasons it doesn't name in a way that can be
// matched (a corrupt download, here), and fetches none of the others for it. The
// package at fault is found by restoring halves, and said; the rest come.
func TestAPackageNuGetFailsOnIsFoundAndNamedAndTheRestStillComes(t *testing.T) {
	h := newHost(t)
	if err := os.WriteFile(filepath.Join(h.feed, "bad.pkg.1.0.0.nupkg"), []byte("this is not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := FetchBatch(context.Background(), "net10.0",
		[]Package{{"Demo.Pkg", "1.0.0"}, {"Bad.Pkg", "1.0.0"}, {"Demo.Two", "1.0.0"}, {"Demo.Three", "1.0.0"}}, h.options())
	if err != nil {
		t.Fatalf("FetchBatch: %v\n%s", err, res.Output)
	}
	if len(res.Fetched) != 3 {
		t.Errorf("fetched %v, want the three that are fine", res.Fetched)
	}
	if len(res.Failed) != 1 || res.Failed[0].Package != (Package{"Bad.Pkg", "1.0.0"}) || !strings.Contains(res.Failed[0].Message, "corrupt") {
		t.Errorf("failed %+v, want Bad.Pkg with NuGet's words", res.Failed)
	}
	if left, _ := os.ReadDir(h.root); len(left) != 0 {
		t.Errorf("throwaway folders left behind: %v", left)
	}
}

// restoring stands in for dotnet restore: restoreBatch is what FetchBatch runs.
func restoring(t *testing.T, fake func(xml string) (string, error)) *int {
	t.Helper()
	calls := 0
	was := restoreBatch
	restoreBatch = func(_ context.Context, xml string, _ Options) (string, error) {
		calls++
		return fake(xml)
	}
	t.Cleanup(func() { restoreBatch = was })
	return &calls
}

func manyPackages(n int) []Package {
	var pkgs []Package
	for i := 0; i < n; i++ {
		pkgs = append(pkgs, Package{fmt.Sprintf("Pkg.N%03d", i), "1.0.0"})
	}
	return pkgs
}

// A timeout isn't a package's fault, and splitting only multiplies the wait.
func TestATimedOutRestoreIsNotSplit(t *testing.T) {
	calls := restoring(t, func(string) (string, error) { return "", timedOut{time.Minute} })
	_, err := FetchBatch(context.Background(), "net8.0", manyPackages(8), Options{Cache: t.TempDir(), Timeout: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "time limit") {
		t.Errorf("err = %v, want the time limit said", err)
	}
	if *calls != 1 {
		t.Errorf("%d restores, want 1", *calls)
	}
}

func TestACancelledFetchIsNotSplit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := restoring(t, func(string) (string, error) { cancel(); return "interrupted", errors.New("signal: killed") })
	_, err := FetchBatch(ctx, "net8.0", manyPackages(8), Options{Cache: t.TempDir(), Timeout: time.Minute})
	if err == nil {
		t.Error("a cancelled fetch isn't an error")
	}
	if *calls != 1 {
		t.Errorf("%d restores, want 1", *calls)
	}
}

// What can't be told apart is bounded: not hundreds of restores, and nothing left unsaid.
func TestRestoresToFindTheFailingPackagesAreBoundedAndWhatWasNotTriedIsSaid(t *testing.T) {
	calls := restoring(t, func(string) (string, error) { return "error : something broke", errors.New("exit status 1") })
	pkgs := manyPackages(200)
	res, err := FetchBatch(context.Background(), "net8.0", pkgs, Options{Cache: t.TempDir(), Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if *calls > maxBatchRestores {
		t.Errorf("%d restores, want at most %d", *calls, maxBatchRestores)
	}
	if len(res.Failed) != len(pkgs) {
		t.Errorf("%d of %d packages accounted for", len(res.Failed), len(pkgs))
	}
	untried := 0
	for _, f := range res.Failed {
		if strings.Contains(f.Message, "not tried") {
			untried++
		}
	}
	if untried == 0 {
		t.Error("nothing says it wasn't tried")
	}
}

// With one package failing among many, finding it takes about log2 restores.
func TestOneFailingPackageAmongManyIsFoundInAFewRestores(t *testing.T) {
	bad := "Pkg.N137"
	calls := restoring(t, func(xml string) (string, error) {
		if strings.Contains(xml, `Include="`+bad+`"`) {
			return "error : " + bad + " is broken", errors.New("exit status 1")
		}
		return "", nil
	})
	res, err := FetchBatch(context.Background(), "net8.0", manyPackages(200), Options{Cache: t.TempDir(), Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Package.ID != bad {
		t.Errorf("failed %+v, want only %s", res.Failed, bad)
	}
	if *calls > 20 {
		t.Errorf("%d restores to find one in 200", *calls)
	}
}
