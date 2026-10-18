package nugetfetch

import (
	"slices"
	"strings"
	"testing"
)

var demo = Request{ID: "Demo.Pkg", Version: "1.2.3", Framework: "net8.0"}

func TestProjectPinsTheExactVersionAndFramework(t *testing.T) {
	p := demo.ProjectXML()
	for _, want := range []string{
		`<TargetFramework>net8.0</TargetFramework>`,
		`<PackageReference Include="Demo.Pkg" Version="[1.2.3]" />`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("project lacks %q:\n%s", want, p)
		}
	}
}

func TestRestoreArgsTurnOffEveryImportTheWorkspaceCouldPlant(t *testing.T) {
	args := RestoreArgs("/work/fetch.csproj")
	for _, want := range []string{
		"-p:ImportDirectoryBuildProps=false",
		"-p:ImportDirectoryBuildTargets=false",
		"-p:ImportDirectoryPackagesProps=false",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("args lack %s: %v", want, args)
		}
	}
	if args[0] != "restore" || args[1] != "/work/fetch.csproj" {
		t.Errorf("args should start with restore and the project: %v", args)
	}
}

func TestRestoreArgsLeaveTheUsersNuGetConfigAlone(t *testing.T) {
	for _, a := range RestoreArgs("/p.csproj") {
		if strings.Contains(a, "configfile") || strings.Contains(a, "--source") {
			t.Errorf("%q would replace the user's own config", a)
		}
	}
}

func TestRestoreEnvPutsPackagesInTheHostCacheOnly(t *testing.T) {
	env := RestoreEnv([]string{"PATH=/bin", "NUGET_PACKAGES=/elsewhere", "NUGET_FALLBACK_PACKAGES=/x", "HOME=/h"}, "/host/.nuget/packages")
	if !slices.Contains(env, "NUGET_PACKAGES=/host/.nuget/packages") {
		t.Errorf("cache not set: %v", env)
	}
	for _, e := range env {
		if strings.HasPrefix(e, "NUGET_PACKAGES=/elsewhere") || strings.HasPrefix(e, "NUGET_FALLBACK_PACKAGES=") {
			t.Errorf("inherited %q would send packages somewhere else", e)
		}
	}
	if !slices.Contains(env, "PATH=/bin") || !slices.Contains(env, "HOME=/h") {
		t.Errorf("the rest of the environment is kept: %v", env)
	}
}
