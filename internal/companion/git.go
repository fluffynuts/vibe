package companion

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// GitInfo is what the page's header shows of the workspace's git status,
// as the vibe-band infobar in the sandbox does.
type GitInfo struct {
	Branch     string `json:"branch"`
	Staged     int    `json:"staged"`
	Modified   int    `json:"modified"`
	Untracked  int    `json:"untracked"`
	Conflicted int    `json:"conflicted"`
	Ahead      int    `json:"ahead"`
	Behind     int    `json:"behind"`
	Stashed    int    `json:"stashed"`
}

// readGit is the workspace's git status, or nil when dir isn't in a
// repository (or git isn't there to ask).
func readGit(dir string) *GitInfo {
	if dir == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "--no-optional-locks",
		"status", "--porcelain=v2", "--branch", "--show-stash").Output()
	if err != nil {
		return nil
	}
	info := parseGitStatus(string(out))
	return &info
}

// parseGitStatus reads `git status --porcelain=v2 --branch --show-stash`.
func parseGitStatus(out string) GitInfo {
	var info GitInfo
	oid := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			oid = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.head "):
			info.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			fields := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(fields) == 2 {
				info.Ahead, _ = strconv.Atoi(strings.TrimPrefix(fields[0], "+"))
				info.Behind, _ = strconv.Atoi(strings.TrimPrefix(fields[1], "-"))
			}
		case strings.HasPrefix(line, "# stash "):
			info.Stashed, _ = strconv.Atoi(strings.TrimPrefix(line, "# stash "))
		case strings.HasPrefix(line, "? "):
			info.Untracked++
		case strings.HasPrefix(line, "u "):
			info.Conflicted++
		case (strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 ")) && len(line) >= 4:
			if line[2] != '.' {
				info.Staged++
			}
			if line[3] != '.' {
				info.Modified++
			}
		}
	}
	if info.Branch == "(detached)" {
		if oid == "(initial)" || len(oid) < 7 {
			info.Branch = "detached"
		} else {
			info.Branch = "@" + oid[:7]
		}
	}
	return info
}

// shortenPath puts ~ for the user's home at the front of path.
func shortenPath(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
