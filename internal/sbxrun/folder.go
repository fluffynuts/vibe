package sbxrun

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// hostOS is runtime.GOOS, as a variable so tests can play at being Windows.
var hostOS = runtime.GOOS

// folders keeps where each sandbox's folder was found inside it, by sandbox and host folder: a sandbox's mounts are fixed
// when it is made.
var folders sync.Map

// findFolderScript prints the first of its arguments that is a folder, and fails (exit 3) when none is.
const findFolderScript = `for p do if [ -d "$p" ]; then printf '%s\n' "$p"; exit 0; fi; done; exit 3`

// FolderInSandbox is where folder, a folder on this machine that sandbox name was made with, is inside the sandbox, for a
// command run there. On Linux and macOS a mount keeps its host path, so it is folder itself. A Windows path (C:\Users\...)
// can't be one in a Linux sandbox, and sbx doesn't document what it becomes (it was /c/Users/...): the likely spellings are
// tried in one exec, the first that is a folder there taken, and kept for the sandbox.
func FolderInSandbox(ctx context.Context, name, folder string) (string, error) {
	if hostOS != "windows" || folder == "" {
		return folder, nil
	}
	key := name + "\x00" + folder
	if found, ok := folders.Load(key); ok {
		return found.(string), nil
	}
	candidates := WindowsMountCandidates(folder)
	out, err := ExecStdout(ctx, name, append([]string{"sh", "-c", findFolderScript, "find"}, candidates...)...)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("couldn't find %s inside the sandbox (looked at %s): %w", folder, strings.Join(candidates, ", "), err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := strings.TrimSpace(lines[len(lines)-1])
	folders.Store(key, found)
	return found, nil
}

// WindowsMountCandidates lists the paths a Windows host folder might be mounted at inside a Linux sandbox, likeliest first:
// the host path with forward slashes, and the /c/..., /mnt/c/... and drive-less forms Docker's tools use.
func WindowsMountCandidates(folder string) []string {
	slashed := strings.ReplaceAll(folder, `\`, "/")
	drive, rest := "", slashed
	if len(slashed) >= 2 && slashed[1] == ':' {
		drive, rest = strings.ToLower(slashed[:1]), slashed[2:]
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	candidates := []string{slashed}
	if drive != "" {
		candidates = append(candidates,
			"/"+drive+rest,
			"/mnt/"+drive+rest,
			"/run/desktop/mnt/host/"+drive+rest,
			"/host_mnt/"+drive+rest,
			rest)
	}
	return candidates
}
