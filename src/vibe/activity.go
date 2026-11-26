package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"vibe/internal/activity"
	"vibe/internal/companion"
	"vibe/internal/fscopy"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/review"
	"vibe/internal/settings"
	"vibe/internal/state"
)

// activityLog is where vibe keeps the marks of work it saw, for the Activity tab.
func activityLog(lay layout.Layout) activity.Log {
	return activity.Log{Dir: lay.HomePath("activity")}
}

// activityTabFor is the Activity tab's: every folder vibe has opened (the sandboxes it has now, and those in the log),
// read on the host with the review helper, never with git (see review.History).
func activityTabFor(lay layout.Layout) *companion.ActivityTab {
	path := lay.HomePath("settings.yaml")
	return &companion.ActivityTab{
		Desk: &activity.Desk{
			Log:     activityLog(lay),
			Folders: func() []string { return workspacesOnRecord(lay.Home) },
			History: review.History,
			Status:  review.StatusOf,
			RemoteHead: func(ctx context.Context, web, branch string) (string, bool, error) {
				return ghRemoteHead(ctx, web, branch)
			},
			AheadOf: func(ctx context.Context, dir, sha string) (int, bool, error) {
				a, err := review.AheadOf(ctx, dir, sha)
				return a.Count, a.Known, err
			},
		},
		Rounding: func() string {
			s, _ := settings.Load(path)
			return s.ActivityRounding
		},
		SetRounding: func(step string) error {
			data, err := fscopy.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if data, err = settings.Set(data, "activityRounding", step); err != nil {
				return fmt.Errorf("updating %s: %w", pathresolve.Tilde(path), err)
			}
			return fscopy.WriteWholeMode(path, data, 0o644)
		},
	}
}

// activityRecorderFor keeps the marks of the sandbox name's page, on its folder.
func activityRecorderFor(lay layout.Layout, name, folder string) func(kind, branch string) {
	if folder == "" {
		return nil
	}
	r := &activity.Recorder{Log: activityLog(lay), Sandbox: name, Folder: folder, Origin: review.Origin,
		Problem: func(why string) { note("activity: %s", why) }}
	return r.Mark
}

// workspacesOnRecord are the host folders of the sandboxes vibe has records of.
func workspacesOnRecord(vibeHome string) []string {
	insts, err := state.List(vibeHome)
	if err != nil {
		return nil
	}
	var out []string
	for _, inst := range insts {
		if inst.Target != "" {
			out = append(out, inst.Target)
		}
	}
	return out
}

// ghRemoteHead is the commit branch is at in repo (https://github.com/owner/name) on GitHub, asked of the host's gh in a
// folder of its own (see ghBranchPullRequest); found false when GitHub has no such branch, activity.ErrNoGitHub without gh.
func ghRemoteHead(ctx context.Context, repo, branch string) (sha string, found bool, err error) {
	gh, err := lookGh()
	if err != nil {
		return "", false, activity.ErrNoGitHub
	}
	dir, err := os.MkdirTemp("", "vibe-gh-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(dir)
	owner, name, _ := strings.Cut(strings.TrimPrefix(repo, "https://github.com/"), "/")
	out, err := runGh(ctx, gh, dir, "api", "graphql", "-f", "query="+remoteHeadQuery, "-f", "owner="+owner, "-f", "name="+name, "-f", "ref=refs/heads/"+branch)
	if err != nil {
		return "", false, err
	}
	var said struct {
		Data struct {
			Repository struct {
				Ref *struct {
					Target struct {
						Oid string `json:"oid"`
					} `json:"target"`
				} `json:"ref"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &said); err != nil {
		return "", false, fmt.Errorf("gh said something unexpected: %q", strings.TrimSpace(string(out)))
	}
	if said.Data.Repository.Ref == nil {
		return "", false, nil
	}
	return said.Data.Repository.Ref.Target.Oid, true, nil
}

const remoteHeadQuery = `query($owner: String!, $name: String!, $ref: String!) {
  repository(owner: $owner, name: $name) { ref(qualifiedName: $ref) { target { oid } } }
}`
