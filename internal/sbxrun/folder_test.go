package sbxrun

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vibe/internal/sbxtest"
)

func TestWindowsMountCandidatesCoverTheUsualSpellings(t *testing.T) {
	got := WindowsMountCandidates(`C:\Users\Quickemu\code\yafs`)
	want := []string{
		"C:/Users/Quickemu/code/yafs",
		"/c/Users/Quickemu/code/yafs",
		"/mnt/c/Users/Quickemu/code/yafs",
		"/run/desktop/mnt/host/c/Users/Quickemu/code/yafs",
		"/host_mnt/c/Users/Quickemu/code/yafs",
		"/Users/Quickemu/code/yafs",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestOffWindowsTheFolderIsTheSameInsideAndAsksNothing(t *testing.T) {
	sbxtest.OnPath(t, `echo "sbx was asked" >&2; exit 1`)
	got, err := FolderInSandbox(context.Background(), "box", "/home/me/code/yafs")
	if err != nil || got != "/home/me/code/yafs" {
		t.Fatalf("want the folder itself, got %q, %v", got, err)
	}
}

// playWindows makes FolderInSandbox act as on Windows, with a stand-in sbx that runs what it's asked to exec on this machine,
// under root (a folder standing for the sandbox's /), counting its calls in calls.
func playWindows(t *testing.T) (root, calls string) {
	t.Helper()
	old := hostOS
	hostOS = "windows"
	t.Cleanup(func() { hostOS = old; folders.Clear() })
	dir := sbxtest.OnPath(t, `echo x >> "$(dirname "$0")/calls"
while [ "$1" != "--" ]; do shift; done; shift
# The sandbox's paths are under ROOT: each argument after the script's own name that starts with / is moved there.
sh=$1; flag=$2; script=$3; name=$4; shift 4
for a do set -- "$@" "$ROOT$a"; shift; done
out=$("$sh" "$flag" "$script" "$name" "$@"); status=$?
[ -n "$out" ] && printf '%s\n' "$out" | sed "s#^$ROOT##"
exit $status`)
	root = t.TempDir()
	t.Setenv("ROOT", root)
	return root, filepath.Join(dir, "calls")
}

func TestOnWindowsTheFolderIsFoundWhereSbxMountedItOnceAndKept(t *testing.T) {
	root, calls := playWindows(t)
	if err := os.MkdirAll(filepath.Join(root, "c", "Users", "Quickemu", "code", "yafs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, err := FolderInSandbox(context.Background(), "box", `C:\Users\Quickemu\code\yafs`)
		if err != nil || got != "/c/Users/Quickemu/code/yafs" {
			t.Fatalf("want /c/Users/Quickemu/code/yafs, got %q, %v", got, err)
		}
	}
	data, _ := os.ReadFile(calls)
	if n := strings.Count(string(data), "x"); n != 1 {
		t.Fatalf("want sbx asked once, the answer kept, got %d asks", n)
	}
}

func TestOnWindowsAFolderNotInTheSandboxIsAnErrorNotAGuess(t *testing.T) {
	playWindows(t)
	_, err := FolderInSandbox(context.Background(), "box", `C:\Users\Quickemu\code\yafs`)
	if err == nil || !strings.Contains(err.Error(), "/c/Users/Quickemu/code/yafs") {
		t.Fatalf("want an error naming where it looked, got %v", err)
	}
}
