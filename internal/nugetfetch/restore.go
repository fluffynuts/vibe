package nugetfetch

import (
	"fmt"
	"strings"
)

// ProjectXML is the throwaway project whose restore fetches r. It holds
// nothing but the one reference, at its exact version; r must be valid, or
// its fields could be markup.
func (r Request) ProjectXML() string {
	return fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>%s</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="%s" Version="[%s]" />
  </ItemGroup>
</Project>
`, r.Framework, r.ID, r.Version)
}

// RestoreArgs are the arguments to dotnet that restore project with nothing
// of the workspace imported: a Directory.Build.props or .targets planted in
// a folder above the project would otherwise run on the host at restore. No
// --configfile or --source: the user's own NuGet config is what reaches
// their feeds, and asking for it by hand is no kindness.
func RestoreArgs(project string) []string {
	return []string{
		"restore", project,
		"-p:ImportDirectoryBuildProps=false",
		"-p:ImportDirectoryBuildTargets=false",
		"-p:ImportDirectoryPackagesProps=false",
	}
}

// RestoreEnv is env with the packages going to cache, the host's, and
// nowhere else.
func RestoreEnv(env []string, cache string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "NUGET_PACKAGES=") || strings.HasPrefix(e, "NUGET_FALLBACK_PACKAGES=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "NUGET_PACKAGES="+cache)
}
