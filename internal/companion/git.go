package companion

import (
	"context"
	"time"

	"vibe/internal/review"
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
// repository (or it can't be read). It's read with go-git, which runs
// nothing the repository's config names: the sandbox can write that.
func readGit(dir string) *GitInfo {
	if dir == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := review.StatusOf(ctx, dir)
	if err != nil {
		return nil
	}
	info := GitInfo(*st)
	return &info
}
