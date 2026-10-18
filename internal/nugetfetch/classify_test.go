package nugetfetch

import (
	"strings"
	"testing"
)

func ref(id, version string) string {
	return `<Project><ItemGroup><PackageReference Include="` + id + `" Version="` + version + `" /></ItemGroup></Project>`
}

func classify(id, version string, committed, working map[string]string, branch string) string {
	return Classify(Request{ID: id, Version: version, Framework: "net8.0"}, ProjectFiles{Committed: committed, Working: working}, branch)
}

func TestARequestForWhatTheDefaultBranchAlreadyUsesIsMissing(t *testing.T) {
	files := map[string]string{"A.csproj": ref("Corp.Lib", "1.2.3")}
	got := classify("corp.lib", "1.2.3", files, files, "main")
	if !strings.HasPrefix(got, "missing") || !strings.Contains(got, "1.2.3") || !strings.Contains(got, "main") {
		t.Errorf("got %q", got)
	}
}

func TestARequestForAHigherVersionThanTheDefaultBranchUsesIsAnUpgrade(t *testing.T) {
	committed := map[string]string{"A.csproj": ref("Corp.Lib", "1.2.3")}
	got := classify("Corp.Lib", "1.10.0", committed, committed, "main")
	if !strings.HasPrefix(got, "upgrade") || !strings.Contains(got, "1.2.3") || !strings.Contains(got, "1.10.0") {
		t.Errorf("got %q (1.10.0 is higher than 1.2.3)", got)
	}
}

func TestARequestForALowerVersionIsADowngrade(t *testing.T) {
	committed := map[string]string{"A.csproj": ref("Corp.Lib", "2.0.0")}
	if got := classify("Corp.Lib", "1.9.9", committed, committed, "main"); !strings.HasPrefix(got, "downgrade") {
		t.Errorf("got %q", got)
	}
}

func TestAPrereleaseIsLowerThanItsRelease(t *testing.T) {
	committed := map[string]string{"A.csproj": ref("Corp.Lib", "2.0.0")}
	if got := classify("Corp.Lib", "2.0.0-beta.1", committed, committed, "main"); !strings.HasPrefix(got, "downgrade") {
		t.Errorf("got %q", got)
	}
	committed = map[string]string{"A.csproj": ref("Corp.Lib", "2.0.0-beta.2")}
	if got := classify("Corp.Lib", "2.0.0-beta.10", committed, committed, "main"); !strings.HasPrefix(got, "upgrade") {
		t.Errorf("got %q (beta.10 follows beta.2)", got)
	}
}

func TestAPackageTheDefaultBranchDoesntUseIsNewAndSaysWhetherTheBranchAddsIt(t *testing.T) {
	committed := map[string]string{"A.csproj": ref("Other", "1.0.0")}
	added := map[string]string{"A.csproj": ref("Other", "1.0.0") + ref("Corp.Lib", "1.2.3")}
	if got := classify("Corp.Lib", "1.2.3", committed, added, "main"); !strings.HasPrefix(got, "new") || !strings.Contains(got, "this branch adds it") {
		t.Errorf("got %q", got)
	}
	if got := classify("Corp.Lib", "1.2.3", committed, committed, "main"); !strings.HasPrefix(got, "new") || !strings.Contains(got, "no project file") {
		t.Errorf("got %q", got)
	}
}

func TestWithNoDefaultBranchItSaysItCantTell(t *testing.T) {
	working := map[string]string{"A.csproj": ref("Corp.Lib", "1.2.3")}
	got := classify("Corp.Lib", "1.2.3", nil, working, "")
	if strings.HasPrefix(got, "new") || strings.HasPrefix(got, "missing") || !strings.Contains(got, "default branch") {
		t.Errorf("got %q: without a default branch it can't call anything new", got)
	}
}

func TestVersionsAreReadHoweverTheProjectSpellsThem(t *testing.T) {
	files := map[string]string{
		"Directory.Packages.props": `<Project><ItemGroup><PackageVersion Include="Central.Lib" Version="3.0.0" /></ItemGroup></Project>`,
		"B.csproj": `<Project><ItemGroup>
			<PackageReference Include='Child.Lib'>
			  <Version>4.0.0</Version>
			</PackageReference>
			<PackageReference
			   Include="Multi.Line"
			   Version="5.0.0"
			/>
			<PackageReference Include="Prop.Lib" Version="$(PropVersion)" />
		</ItemGroup></Project>`,
	}
	for id, version := range map[string]string{"Central.Lib": "3.0.0", "Child.Lib": "4.0.0", "Multi.Line": "5.0.0"} {
		if got := classify(id, version, files, files, "main"); !strings.HasPrefix(got, "missing") {
			t.Errorf("%s: %q", id, got)
		}
	}
	if got := classify("Prop.Lib", "1.0.0", files, files, "main"); !strings.Contains(got, "property") {
		t.Errorf("a version from a property can't be compared: %q", got)
	}
}
