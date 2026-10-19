package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runLinkNuget runs the dotnet feature's link-nuget as the sandbox would,
// against a persistent env file of its own, and returns that file's content.
func runLinkNuget(t *testing.T, persisted string, env ...string) string {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "library", "dotnet", "agent-files", ".local", "bin", "link-nuget"))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "persistent.sh")
	if err := os.WriteFile(file, []byte(persisted), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "SBX_PERSIST_FILE=" + file, "SBX_NUGET_LINK=" + filepath.Join(t.TempDir(), ".nuget"), "HOME=" + t.TempDir()}, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("link-nuget: %v\n%s", err, out)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLinkNugetMakesTheHostsPackagesAReadOnlyFallback(t *testing.T) {
	packages := t.TempDir()
	got := runLinkNuget(t, "export NUGET_PACKAGES=/old/shared/packages\nexport KEEP=1\n", "SBX_HOST_NUGET_PACKAGES="+packages)
	for _, want := range []string{"export NUGET_FALLBACK_PACKAGES=" + packages, "export RestoreIgnoreFailedSources=true", "export KEEP=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("persistent file lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "export NUGET_PACKAGES=") {
		t.Errorf("a leftover NUGET_PACKAGES would send packages to the read-only cache:\n%s", got)
	}
}

func TestLinkNugetIsIdempotent(t *testing.T) {
	packages := t.TempDir()
	once := runLinkNuget(t, "", "SBX_HOST_NUGET_PACKAGES="+packages)
	twice := runLinkNuget(t, once, "SBX_HOST_NUGET_PACKAGES="+packages)
	if once != twice {
		t.Errorf("running again changed the file:\n%s\n---\n%s", once, twice)
	}
}

func TestLinkNugetSharedKeepsPinningNugetPackagesToTheHost(t *testing.T) {
	nuget := t.TempDir()
	got := runLinkNuget(t, "export NUGET_FALLBACK_PACKAGES=/x\nexport RestoreIgnoreFailedSources=true\n", "SBX_HOST_NUGET="+nuget)
	if !strings.Contains(got, "export NUGET_PACKAGES="+nuget+"/packages") {
		t.Errorf("shared should pin NUGET_PACKAGES:\n%s", got)
	}
	if strings.Contains(got, "NUGET_FALLBACK_PACKAGES") || strings.Contains(got, "RestoreIgnoreFailedSources") {
		t.Errorf("a switch from fallback leaves its settings behind:\n%s", got)
	}
}

func TestLinkNugetWithNothingMountedChangesNothing(t *testing.T) {
	if got := runLinkNuget(t, "export KEEP=1\n"); got != "export KEEP=1\n" {
		t.Errorf("changed the file: %q", got)
	}
}

// Claude lists each skill's name and description in the agent's context, and
// reads the rest only when the description fits what it's doing: the kit's
// instructions alone weren't found by an agent whose restore failed with
// NU1101. The description is what makes it reach for vibe-nuget then.
func TestTheDotnetFeatureHasASkillForAPackageARestoreCantFind(t *testing.T) {
	path := filepath.Join("..", "..", "library", "dotnet", "agent-files", ".claude", "skills", "vibe-nuget", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no skill for vibe-nuget: %v", err)
	}
	front, _, ok := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---\n")
	if !ok || !strings.HasPrefix(string(data), "---\n") {
		t.Fatalf("the skill has no frontmatter:\n%s", data)
	}
	if !strings.Contains(front, "name: vibe-nuget\n") {
		t.Errorf("frontmatter lacks the name:\n%s", front)
	}
	for _, trigger := range []string{"NU1101", "NU1102", "private", "vibe-nuget fetch"} {
		if !strings.Contains(front, trigger) {
			t.Errorf("the description should mention %q, so the agent matches it to its failure:\n%s", trigger, front)
		}
	}
}
