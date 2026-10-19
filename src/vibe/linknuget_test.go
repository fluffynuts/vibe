package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeMounting is a bin folder holding a sudo that runs what it's given and a
// mount that records its arguments in calls (one line each) and succeeds when
// succeeds, so no test mounts anything for real.
type fakeMounting struct {
	bin   string
	calls string
}

func newFakeMounting(t *testing.T, succeeds bool) fakeMounting {
	t.Helper()
	dir := t.TempDir()
	f := fakeMounting{bin: filepath.Join(dir, "bin"), calls: filepath.Join(dir, "mount-calls")}
	if err := os.MkdirAll(f.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	status := "1"
	if succeeds {
		status = "0"
	}
	scripts := map[string]string{
		"sudo":  "#!/bin/sh\nexec \"$@\"\n",
		"mount": "#!/bin/sh\necho \"$*\" >> '" + f.calls + "'\nexit " + status + "\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(f.bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f fakeMounting) mounts(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(f.calls)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// runLinkNuget runs the dotnet feature's link-nuget as the sandbox would,
// against a persistent env file of its own, with no mount that works, and
// returns that file's content.
func runLinkNuget(t *testing.T, persisted string, env ...string) string {
	t.Helper()
	return runLinkNugetMounting(t, newFakeMounting(t, false), persisted, env...)
}

func runLinkNugetMounting(t *testing.T, mounting fakeMounting, persisted string, env ...string) string {
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
	cmd.Env = append([]string{"PATH=" + mounting.bin + string(os.PathListSeparator) + os.Getenv("PATH"), "SBX_PERSIST_FILE=" + file, "SBX_NUGET_LINK=" + filepath.Join(t.TempDir(), ".nuget"), "HOME=" + t.TempDir()}, env...)
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
	got := runLinkNuget(t, "export NUGET_FALLBACK_PACKAGES=/x\nexport RestoreIgnoreFailedSources=true\nexport VIBE_NUGET_OVERLAY=1\n", "SBX_HOST_NUGET="+nuget)
	if !strings.Contains(got, "export NUGET_PACKAGES="+nuget+"/packages") {
		t.Errorf("shared should pin NUGET_PACKAGES:\n%s", got)
	}
	if strings.Contains(got, "NUGET_FALLBACK_PACKAGES") || strings.Contains(got, "RestoreIgnoreFailedSources") || strings.Contains(got, "VIBE_NUGET_OVERLAY") {
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

// With the host's cache overlaid at its own path (the host's packages below,
// read-only; what the sandbox writes above, in the sandbox), restore bakes
// the host's path into obj/project.assets.json and the *.nuget.g.props files,
// so the host's IDE resolves what the sandbox restored. The upper layer must
// not be on the sandbox's own root, itself an overlay, which can't hold one.
func TestLinkNugetOverlaysTheHostsPackagesAtTheirOwnPath(t *testing.T) {
	packages := t.TempDir()
	base := filepath.Join(t.TempDir(), "vibe-nuget")
	mounting := newFakeMounting(t, true)
	got := runLinkNugetMounting(t, mounting, "export NUGET_FALLBACK_PACKAGES=/old\n", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_UPPER_BASE="+base)
	calls := mounting.mounts(t)
	if len(calls) != 1 {
		t.Fatalf("want one mount, got %q", calls)
	}
	for _, want := range []string{"-t overlay", "lowerdir=" + packages, "upperdir=" + base + "/upper", "workdir=" + base + "/work", " " + packages} {
		if !strings.Contains(calls[0], want) {
			t.Errorf("mount %q lacks %q", calls[0], want)
		}
	}
	for _, want := range []string{"export NUGET_PACKAGES=" + packages, "export VIBE_NUGET_OVERLAY=1", "export RestoreIgnoreFailedSources=true"} {
		if !strings.Contains(got, want) {
			t.Errorf("persistent file lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "NUGET_FALLBACK_PACKAGES") {
		t.Errorf("with the overlay the cache is the one place, not a fallback:\n%s", got)
	}
	if info, err := os.Stat(filepath.Join(base, "upper")); err != nil || !info.IsDir() {
		t.Errorf("the upper layer wasn't made: %v", err)
	}
}

func TestLinkNugetKeepsTheFallbackWhenTheOverlayCantBeMounted(t *testing.T) {
	packages := t.TempDir()
	base := filepath.Join(t.TempDir(), "vibe-nuget")
	got := runLinkNugetMounting(t, newFakeMounting(t, false), "export NUGET_PACKAGES=/old\nexport VIBE_NUGET_OVERLAY=1\n", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_UPPER_BASE="+base)
	if !strings.Contains(got, "export NUGET_FALLBACK_PACKAGES="+packages) {
		t.Errorf("no fallback:\n%s", got)
	}
	if strings.Contains(got, "export NUGET_PACKAGES=") || strings.Contains(got, "VIBE_NUGET_OVERLAY") {
		t.Errorf("what the overlay set is left behind:\n%s", got)
	}
}

func TestLinkNugetMountsNothingWhereThereIsNoDiskForTheUpperLayer(t *testing.T) {
	packages := t.TempDir()
	mounting := newFakeMounting(t, true)
	runLinkNugetMounting(t, mounting, "", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_UPPER_BASE="+filepath.Join(t.TempDir(), "no", "such", "disk", "vibe-nuget"))
	if calls := mounting.mounts(t); len(calls) != 0 {
		t.Errorf("mounted anyway: %q", calls)
	}
}

// overlayfs options are split on commas and colons, so a path holding either
// (a Windows host's, C:\Users\...) can't be one.
func TestLinkNugetDoesNotOverlayAPathOverlayfsCantTake(t *testing.T) {
	for _, name := range []string{"has,comma", "has:colon"} {
		packages := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(packages, 0o755); err != nil {
			t.Fatal(err)
		}
		mounting := newFakeMounting(t, true)
		got := runLinkNugetMounting(t, mounting, "", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_UPPER_BASE="+filepath.Join(t.TempDir(), "vibe-nuget"))
		if calls := mounting.mounts(t); len(calls) != 0 {
			t.Errorf("%s: mounted anyway: %q", name, calls)
		}
		if !strings.Contains(got, "NUGET_FALLBACK_PACKAGES") {
			t.Errorf("%s: no fallback:\n%s", name, got)
		}
	}
}

func TestLinkNugetDoesNotOverlayTwice(t *testing.T) {
	packages := t.TempDir()
	base := filepath.Join(t.TempDir(), "vibe-nuget")
	mounts := filepath.Join(t.TempDir(), "mounts")
	if err := os.WriteFile(mounts, []byte("overlay "+packages+" overlay rw,relatime,lowerdir="+packages+",upperdir="+base+"/upper,workdir="+base+"/work 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mounting := newFakeMounting(t, true)
	got := runLinkNugetMounting(t, mounting, "", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_UPPER_BASE="+base, "SBX_PROC_MOUNTS="+mounts)
	if calls := mounting.mounts(t); len(calls) != 0 {
		t.Errorf("mounted over a mount: %q", calls)
	}
	if !strings.Contains(got, "export NUGET_PACKAGES="+packages) {
		t.Errorf("an overlay already there is still the cache:\n%s", got)
	}
}

// A folder bound from a sandbox's root, itself an overlay, is listed as one.
func TestLinkNugetOverlaysOverAnOverlayThatIsntItsOwn(t *testing.T) {
	packages := t.TempDir()
	mounts := filepath.Join(t.TempDir(), "mounts")
	if err := os.WriteFile(mounts, []byte("overlay "+packages+" overlay ro,relatime,lowerdir=/a,upperdir=/b,workdir=/c 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mounting := newFakeMounting(t, true)
	runLinkNugetMounting(t, mounting, "", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_UPPER_BASE="+filepath.Join(t.TempDir(), "vibe-nuget"), "SBX_PROC_MOUNTS="+mounts)
	if calls := mounting.mounts(t); len(calls) != 1 {
		t.Errorf("want the overlay mounted, got %q", calls)
	}
}
