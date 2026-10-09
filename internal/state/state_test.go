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
