package nugetfetch

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
