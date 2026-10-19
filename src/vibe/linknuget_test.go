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
	bin    string
	calls  string
	chowns string
}

func newFakeMounting(t *testing.T, succeeds bool) fakeMounting {
	t.Helper()
	dir := t.TempDir()
	f := fakeMounting{bin: filepath.Join(dir, "bin"), calls: filepath.Join(dir, "mount-calls"), chowns: filepath.Join(dir, "chown-calls")}
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
		"chown": "#!/bin/sh\necho \"$*\" >> '" + f.chowns + "'\n",
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
	return linesOf(t, f.calls)
}

func (f fakeMounting) chowned(t *testing.T) []string {
	t.Helper()
	return linesOf(t, f.chowns)
}

func linesOf(t *testing.T, file string) []string {
	t.Helper()
	data, err := os.ReadFile(file)
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
	cmd.Env = append([]string{"PATH=" + mounting.bin + string(os.PathListSeparator) + os.Getenv("PATH"), "SBX_PERSIST_FILE=" + file, "SBX_LINK_NUGET_LOG=" + filepath.Join(t.TempDir(), "link-nuget.log"), "SBX_NUGET_LINK=" + filepath.Join(t.TempDir(), ".nuget"), "HOME=" + t.TempDir()}, env...)
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

// The other way a package is missing: the restore succeeds, even saying it is up to date, and
// the build can't find the package's files. That is no NU1101, so the skill's description has to
// say it, or the agent only finds the cure by trying things.
func TestTheSkillIsAlsoForABuildThatCantFindAPackageAfterARestoreThatWorked(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "library", "dotnet", "agent-files", ".claude", "skills", "vibe-nuget", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	front, body, _ := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---\n")
	for _, trigger := range []string{"CS0246", "MSB3030", "up to date"} {
		if !strings.Contains(front, trigger) {
			t.Errorf("the description should mention %q:\n%s", trigger, front)
		}
	}
	force, clear := strings.Index(body, "dotnet restore --force"), strings.Index(body, "assets.cache")
	if force < 0 || clear < 0 || force > clear {
		t.Errorf("the skill should say to restore with --force first (at %d), and only then to delete the assets.cache files (at %d)", force, clear)
	}
	if !strings.Contains(body, "find . -path '*/obj/*' -name '*.assets.cache' -delete") {
		t.Error("the skill doesn't give the command that deletes the stale path caches")
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
	got := runLinkNugetMounting(t, mounting, "export NUGET_FALLBACK_PACKAGES=/old\n", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_OVERLAY=1", "SBX_NUGET_UPPER_BASE="+base)
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
	got := runLinkNugetMounting(t, newFakeMounting(t, false), "export NUGET_PACKAGES=/old\nexport VIBE_NUGET_OVERLAY=1\n", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_OVERLAY=1", "SBX_NUGET_UPPER_BASE="+base)
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
	runLinkNugetMounting(t, mounting, "", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_OVERLAY=1", "SBX_NUGET_UPPER_BASE="+filepath.Join(t.TempDir(), "no", "such", "disk", "vibe-nuget"))
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
		got := runLinkNugetMounting(t, mounting, "", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_OVERLAY=1", "SBX_NUGET_UPPER_BASE="+filepath.Join(t.TempDir(), "vibe-nuget"))
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
	got := runLinkNugetMounting(t, mounting, "", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_OVERLAY=1", "SBX_NUGET_UPPER_BASE="+base, "SBX_PROC_MOUNTS="+mounts)
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
	runLinkNugetMounting(t, mounting, "", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_OVERLAY=1", "SBX_NUGET_UPPER_BASE="+filepath.Join(t.TempDir(), "vibe-nuget"), "SBX_PROC_MOUNTS="+mounts)
	if calls := mounting.mounts(t); len(calls) != 1 {
		t.Errorf("want the overlay mounted, got %q", calls)
	}
}

// link-nuget runs as root when the sandbox is set up, so the upper layer (whose
// owner is the owner of the overlay's root) has to be given to the agent
// explicitly: root's would leave NuGet unable to write a package.
func TestLinkNugetGivesTheUpperLayerToTheAgent(t *testing.T) {
	packages := t.TempDir()
	base := filepath.Join(t.TempDir(), "vibe-nuget")
	mounting := newFakeMounting(t, true)
	runLinkNugetMounting(t, mounting, "", "SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_OVERLAY=1", "SBX_NUGET_UPPER_BASE="+base)
	if got := mounting.chowned(t); len(got) != 1 || got[0] != "agent: "+base+"/upper" {
		t.Errorf("the upper layer's owner: %q", got)
	}
}

// Without being asked for (nuget: overlay) nothing is mounted over the host's cache, whatever
// the sandbox could do: the overlay breaks a restore that needs a version beside one the host
// has, so the cache stays a read-only fallback beside the sandbox's own.
func TestLinkNugetDoesNotOverlayUnlessAskedTo(t *testing.T) {
	packages := t.TempDir()
	mounting := newFakeMounting(t, true)
	got := runLinkNugetMounting(t, mounting, "export NUGET_PACKAGES=/old\nexport VIBE_NUGET_OVERLAY=1\n",
		"SBX_HOST_NUGET_PACKAGES="+packages, "SBX_NUGET_UPPER_BASE="+filepath.Join(t.TempDir(), "vibe-nuget"))
	if calls := mounting.mounts(t); len(calls) != 0 {
		t.Errorf("mounted without being asked: %q", calls)
	}
	if !strings.Contains(got, "export NUGET_FALLBACK_PACKAGES="+packages) {
		t.Errorf("no fallback:\n%s", got)
	}
	if strings.Contains(got, "export NUGET_PACKAGES=") || strings.Contains(got, "VIBE_NUGET_OVERLAY") {
		t.Errorf("what an earlier overlay set is left behind:\n%s", got)
	}
}

// A package that used to come from the host's cache (the fallback) and is gone from it is
// downloaded into the sandbox's own; the build still looks where it was, and keeps failing,
// whatever the restore says, until the SDK's cache of package paths is deleted. The agent
// has to be told: nothing else makes it look there.
func TestTheDotnetInstructionsSayWhatToDoWhenABuildCantFindAPackageAfterARestore(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "library", "dotnet", "agent-instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"CS0246", "MSB3030", "up to date", "assets.cache", "find . -path '*/obj/*' -name '*.assets.cache' -delete", "dotnet restore --force"} {
		if !strings.Contains(text, want) {
			t.Errorf("the instructions don't mention %q", want)
		}
	}
	if strings.Index(text, "dotnet restore --force") > strings.Index(text, "assets.cache") {
		t.Error("the instructions should say to try a forced restore before deleting the path caches")
	}
}

// An agent that builds before the user has dealt with what vibe is holding for them downloads its own
// copies of packages the user's cache lacks, which the user's IDE can't see. Asking first, and telling
// the user, is the agent's part of that; the instructions are always read, the skill when it matches.
func TestTheAgentIsToldToAskWhatIsWaitingBeforeItRestoresOrBuilds(t *testing.T) {
	dotnet := filepath.Join("..", "..", "library", "dotnet")
	instructions, err := os.ReadFile(filepath.Join(dotnet, "agent-instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill, err := os.ReadFile(filepath.Join(dotnet, "agent-files", ".claude", "skills", "vibe-nuget", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	front, _, _ := strings.Cut(strings.TrimPrefix(string(skill), "---\n"), "\n---\n")
	if !strings.Contains(front, "vibe-nuget status") {
		t.Errorf("the skill's description should say to run vibe-nuget status before a restore or build:\n%s", front)
	}
	for name, text := range map[string]string{"instructions": string(instructions), "skill": string(skill)} {
		for _, want := range []string{"vibe-nuget status", "Tell the user", "wait"} {
			if !strings.Contains(text, want) {
				t.Errorf("the %s don't say %q", name, want)
			}
		}
	}
	text := string(instructions)
	if strings.Index(text, "vibe-nuget status") > strings.Index(text, "## NuGet packages from private feeds") {
		t.Error("the instructions should come to asking what is waiting before the rest: it's what the agent does first")
	}
}

// A request the user didn't select, or declined, comes back denied: the agent is to tell the user and ask,
// not to retry and not to find another way, and to ask for it again only when the user says it was a slip.
func TestTheSkillSaysWhatToDoWithARequestTheUserDidNotSelectOrDeclined(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "library", "dotnet", "agent-files", ".claude", "skills", "vibe-nuget", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, body, _ := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---\n")
	i := strings.Index(body, "If it isn't fetched")
	if i < 0 {
		t.Fatal("the skill has no section for a package that isn't fetched")
	}
	// Wrapped lines are no matter to what it says.
	section := strings.Join(strings.Fields(body[i:]), " ")
	for _, want := range []string{"did not select", "slip", "declined", "ask the user", "don't ask again", "unless the user says"} {
		if !strings.Contains(section, want) {
			t.Errorf("the section on what isn't fetched lacks %q:\n%s", want, section)
		}
	}
}
