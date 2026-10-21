package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCompanionTokenIsMadeOnceAndKept(t *testing.T) {
	home := t.TempDir()
	first, err := CompanionToken(home, "vibe")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 32 {
		t.Errorf("token %q is too short to be a secret", first)
	}
	again, err := CompanionToken(home, "vibe")
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Errorf("token changed between calls: %q then %q", first, again)
	}
	other, err := CompanionToken(home, "other")
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Error("two sandboxes share a token")
	}
}

func TestCompanionTokenIsOnlyTheUsersToRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows has no unix file modes")
	}
	home := t.TempDir()
	if _, err := CompanionToken(home, "vibe"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, "companion", "vibe.token"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("token file mode %o lets others read it", perm)
	}
}

func TestRemoveDeletesTheToken(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, Instance{Name: "vibe"}); err != nil {
		t.Fatal(err)
	}
	token, err := CompanionToken(home, "vibe")
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(home, "vibe"); err != nil {
		t.Fatal(err)
	}
	fresh, err := CompanionToken(home, "vibe")
	if err != nil {
		t.Fatal(err)
	}
	if fresh == token {
		t.Error("a removed sandbox's token came back")
	}
}

func TestCompanionPortIsRemembered(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, Instance{Name: "vibe", CompanionPort: 5352}); err != nil {
		t.Fatal(err)
	}
	inst, ok, err := Load(home, "vibe")
	if err != nil || !ok {
		t.Fatalf("load: %v %v", ok, err)
	}
	if inst.CompanionPort != 5352 {
		t.Errorf("port = %d", inst.CompanionPort)
	}
}

func TestRemoveDeletesTheConversation(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, Instance{Name: "vibe"}); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{ConversationPath(home, "vibe"), TranscriptPath(home, "vibe")} {
		os.MkdirAll(filepath.Dir(file), 0o700)
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Remove(home, "vibe"); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{ConversationPath(home, "vibe"), TranscriptPath(home, "vibe")} {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Errorf("%s is still there", file)
		}
	}
}

func TestRemoveDeletesTheClipboardHistoryButNotAnotherSandboxs(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"vibe", "other"} {
		if err := Save(home, Instance{Name: name}); err != nil {
			t.Fatal(err)
		}
		item := filepath.Join(ClipboardPath(home, name), "items", "1.png")
		os.MkdirAll(filepath.Dir(item), 0o755)
		os.WriteFile(item, []byte("png"), 0o644)
	}
	if err := Remove(home, "vibe"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ClipboardPath(home, "vibe")); !os.IsNotExist(err) {
		t.Errorf("the clipboard history is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ClipboardPath(home, "other"), "items", "1.png")); err != nil {
		t.Errorf("another sandbox's history went too: %v", err)
	}
}

func TestRemoveDeletesPackageRequestsButNotAnotherSandboxs(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"vibe", "other"} {
		if err := Save(home, Instance{Name: name}); err != nil {
			t.Fatal(err)
		}
		answer := filepath.Join(PackagesPath(home, name), "view", "1.answer")
		os.MkdirAll(filepath.Dir(answer), 0o755)
		os.WriteFile(answer, []byte("fetched"), 0o644)
	}
	if err := Remove(home, "vibe"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(PackagesPath(home, "vibe")); !os.IsNotExist(err) {
		t.Errorf("the package requests are still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(PackagesPath(home, "other"), "view", "1.answer")); err != nil {
		t.Errorf("another sandbox's went too: %v", err)
	}
}

func TestTheProfilesPageKeepsItsOwnTokenAndPort(t *testing.T) {
	home := t.TempDir()
	token, err := ManageToken(home)
	if err != nil || len(token) < 32 {
		t.Fatalf("ManageToken = %q, %v", token, err)
	}
	if again, _ := ManageToken(home); again != token {
		t.Error("the token changed between calls")
	}
	if sandbox, _ := CompanionToken(home, "manage"); sandbox == token {
		t.Error("a sandbox called 'manage' shares the Profiles page's token")
	}
	if ManagePort(home) != 0 {
		t.Error("a port was remembered before one was saved")
	}
	if err := SaveManagePort(home, 10007); err != nil || ManagePort(home) != 10007 {
		t.Errorf("ManagePort = %d after saving 10007 (%v)", ManagePort(home), err)
	}
}

func TestRemoveDeletesTheKeptNetworkRulesButNotAnotherSandboxs(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"vibe", "other"} {
		if err := Save(home, Instance{Name: name}); err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Dir(RulesPath(home, name)), 0o700)
		os.WriteFile(RulesPath(home, name), []byte(`{"rules": []}`), 0o600)
	}
	if err := Remove(home, "vibe"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(RulesPath(home, "vibe")); !os.IsNotExist(err) {
		t.Errorf("the kept rules are still there: %v", err)
	}
	if _, err := os.Stat(RulesPath(home, "other")); err != nil {
		t.Errorf("another sandbox's went too: %v", err)
	}
}

func TestMoveTakesTheKeptNetworkRulesAlong(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, Instance{Name: "old"}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(RulesPath(home, "old")), 0o700)
	os.WriteFile(RulesPath(home, "old"), []byte(`{"rules": []}`), 0o600)
	if err := Move(home, "old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(RulesPath(home, "new")); err != nil {
		t.Errorf("not moved: %v", err)
	}
	if _, err := os.Stat(RulesPath(home, "old")); !os.IsNotExist(err) {
		t.Errorf("still at the old name: %v", err)
	}
}
