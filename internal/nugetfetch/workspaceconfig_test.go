package nugetfetch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wsConfig = `<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <add key="corp" value="https://nuget.corp.example/v3/index.json" />
    <add key="gh" value="https://nuget.pkg.github.com/corp/index.json" />
  </packageSources>
</configuration>`

func newWorkspace(t *testing.T, config string) (WorkspaceConfig, string) {
	t.Helper()
	dir := t.TempDir()
	if config != "" {
		if err := os.WriteFile(filepath.Join(dir, "nuget.config"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return WorkspaceConfig{Dir: dir, Approvals: filepath.Join(t.TempDir(), "approved.json")}, dir
}

func TestAWorkspaceWithNoConfigHasNothingToApprove(t *testing.T) {
	w, _ := newWorkspace(t, "")
	info, err := w.Look()
	if err != nil || info != nil {
		t.Errorf("info %+v, err %v", info, err)
	}
	if w.ApprovedContent() != nil {
		t.Error("content with nothing there")
	}
}

func TestAWorkspaceConfigIsShownWithItsSourcesAndIsUnapprovedUntilApproved(t *testing.T) {
	w, _ := newWorkspace(t, wsConfig)
	info, err := w.Look()
	if err != nil || info == nil {
		t.Fatalf("info %+v, err %v", info, err)
	}
	if info.Approved || len(info.Sources) != 2 || info.Sources[0].Name != "corp" || info.Sources[1].URL != "https://nuget.pkg.github.com/corp/index.json" {
		t.Errorf("info = %+v", info)
	}
	if info.Text != wsConfig {
		t.Errorf("the user reads the whole file before approving it: %q", info.Text)
	}
	if w.ApprovedContent() != nil {
		t.Error("used before it was approved")
	}
	if err := w.Approve(info.Hash); err != nil {
		t.Fatal(err)
	}
	if after, _ := w.Look(); !after.Approved {
		t.Error("not approved after approving")
	}
	if string(w.ApprovedContent()) != wsConfig {
		t.Errorf("approved content = %q", w.ApprovedContent())
	}
}

func TestAnApprovalIsForThatContentAndAnyChangeAsksAgain(t *testing.T) {
	w, dir := newWorkspace(t, wsConfig)
	info, _ := w.Look()
	if err := w.Approve(info.Hash); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(wsConfig, "nuget.corp.example", "evil.example", 1)
	if err := os.WriteFile(filepath.Join(dir, "nuget.config"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	after, _ := w.Look()
	if after.Approved || w.ApprovedContent() != nil {
		t.Errorf("a changed config is used on the old approval: %+v", after)
	}
	if err := w.Approve(info.Hash); err == nil {
		t.Error("approved content that's no longer what the user saw")
	}
	if still, _ := w.Look(); still.Approved {
		t.Error("approving the old hash approved the new content")
	}
}

func TestTheConfigIsFoundHoweverItsNameIsCased(t *testing.T) {
	for _, name := range []string{"NuGet.Config", "NuGet.config", "NUGET.CONFIG"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(wsConfig), 0o644); err != nil {
			t.Fatal(err)
		}
		w := WorkspaceConfig{Dir: dir, Approvals: filepath.Join(t.TempDir(), "a.json")}
		if info, err := w.Look(); err != nil || info == nil {
			t.Errorf("%s: %+v, %v", name, info, err)
		}
	}
}

func TestAConfigThatRedefinesASourceTheUserHasIsWarnedAbout(t *testing.T) {
	w, _ := newWorkspace(t, wsConfig)
	host := filepath.Join(t.TempDir(), "NuGet.Config")
	hostConfig := `<configuration><packageSources>
		<add key="corp" value="https://real.corp.example/index.json" />
		<add key="gh" value="https://nuget.pkg.github.com/corp/index.json" />
	</packageSources></configuration>`
	if err := os.WriteFile(host, []byte(hostConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	w.HostConfigs = []string{host, filepath.Join(t.TempDir(), "absent.config")}
	info, err := w.Look()
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Redefines) != 1 || !strings.Contains(info.Redefines[0], "corp") || !strings.Contains(info.Redefines[0], "real.corp.example") {
		t.Errorf("Redefines = %v: only 'corp' points somewhere new", info.Redefines)
	}
}

func TestAConfigThatIsNotXMLIsShownAsItIsWithNoSources(t *testing.T) {
	w, _ := newWorkspace(t, "not xml at all")
	info, err := w.Look()
	if err != nil || info == nil || len(info.Sources) != 0 || info.Problem == "" {
		t.Errorf("info %+v, err %v", info, err)
	}
}

func TestAConfigTooLargeToReadIsNotUsed(t *testing.T) {
	w, _ := newWorkspace(t, strings.Repeat("x", maxWorkspaceConfig+1))
	info, _ := w.Look()
	if info == nil || info.Approved || info.Problem == "" || info.Hash != "" {
		t.Errorf("info = %+v", info)
	}
}
