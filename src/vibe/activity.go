package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"vibe/internal/activity"
	"vibe/internal/companion"
	"vibe/internal/fscopy"
	"vibe/internal/instructions"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/review"
	"vibe/internal/sbxrun"
	"vibe/internal/settings"
	"vibe/internal/state"
)

// activityLog is where vibe keeps the marks of work it saw, for the Activity tab.
func activityLog(lay layout.Layout) activity.Log {
	return activity.Log{Dir: lay.HomePath("activity")}
}

// activityTabFor is the Activity tab's: every folder vibe has opened (the sandboxes it has now, and those in the log),
// read on the host with the review helper, never with git (see review.History).
func activityTabFor(lay layout.Layout, name, agent string) *companion.ActivityTab {
	path := lay.HomePath("settings.yaml")
	tickets := &activity.Tickets{Path: lay.HomePath("activity", "tickets.json")}
	return &companion.ActivityTab{
		Summaries: summariserFor(lay, name, agent),
		Tickets:   tickets,
		Desk: &activity.Desk{
			Tickets: tickets,
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
		Fills: &activity.Fills{Path: lay.HomePath("activity", "fills.json")},
		Rounding: func() string {
			s, _ := settings.Load(path)
			return s.ActivityRounding
		},
		SetRounding: func(step string) error { return setSetting(path, "activityRounding", step) },
		ColourBlind: func() bool {
			s, _ := settings.Load(path)
			return s.ActivityColourBlind
		},
		SetColourBlind: func(on bool) error { return setSetting(path, "activityColourBlind", on) },
	}
}

// setSetting keeps key's value in the settings file at path, leaving the rest of it as the user wrote it.
func setSetting(path, key string, value interface{}) error {
	data, err := fscopy.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if data, err = settings.Set(data, key, value); err != nil {
		return fmt.Errorf("updating %s: %w", pathresolve.Tilde(path), err)
	}
	return fscopy.WriteWholeMode(path, data, 0o644)
}

// summariserFor makes the branches' summaries with the agent of sandbox name, when it is Claude, the one agent with a tester
// (see TESTERS_WANTED.md); a page for no sandbox only shows those other pages made.
func summariserFor(lay layout.Layout, name, agent string) *activity.Summariser {
	s := &activity.Summariser{Dir: lay.HomePath("activity", "summaries"), Holder: name, Rules: lay.HomePath("activity", "summary-rules.md"),
		Problem: func(why string) { note("activity: %s", why) }}
	switch {
	case name == "":
		s.Unavailable = "Commit summaries are made on a sandbox's page: open the Activity tab there to have these summarised."
	case agent != "claude":
		if agent == "" {
			agent = "this sandbox's agent"
		}
		s.Unavailable = "Commit summaries are only shown for Claude agents. If you'd like " + agent + " to be supported, please ask for it with Request a feature, in the power menu."
	default:
		s.Ask = func(ctx context.Context, prompt string) (string, error) { return askOnce(ctx, name, prompt) }
	}
	return s
}

// summaryModel is the model summaries are asked of: a few lines from commit subjects need no more.
const summaryModel = "haiku"

// askOnce asks sandbox name's Claude prompt once, with no tools, in a neutral folder (as Optimise does), with summaryModel. The
// sandbox is started if it is stopped: the page keeps its own sandbox awake while it is in view anyway.
func askOnce(ctx context.Context, name, prompt string) (string, error) {
	if !sbxrun.Reachable(ctx, name) {
		return "", errors.New("the sandbox " + name + " isn't running, and couldn't be started")
	}
	reply, err := sbxrun.ExecInputStdout(ctx, name, prompt, asAgent(oneShotCommand(append(append([]string(nil), instructions.Agents["claude"].OneShot...), "--model", summaryModel)...))...)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("the agent took too long to summarise, and was stopped: %w", ctx.Err())
		}
		return "", fmt.Errorf("the agent couldn't be asked: %w", err)
	}
	return reply, nil
}

// oneShotEnv marks a claude vibe runs for its own ends (a summary, Optimise, the model list), not the user's: the clipboard hook
// (library/base/clipboard-bridge) leaves what the user pasted for their next prompt. A summary once took a pasted screenshot,
// which was marked sent and never reached the user's message.
const oneShotEnv = "VIBE_CLIPBOARD_HOOK=off"

// oneShotCommand is the shell command that runs words, an agent's one-shot ask (instructions.Agent.OneShot, the prompt on its
// stdin), marked as vibe's own, from somewhere neutral (not the workspace, whose instructions it needn't pick up).
func oneShotCommand(words ...string) string {
	words = append([]string(nil), words...)
	for i, w := range words {
		words[i] = shellQuote(w)
	}
	return "cd /tmp && exec env " + oneShotEnv + " " + strings.Join(words, " ")
}

// activityRecorderFor keeps the marks of the sandbox name's page, on its folder.
func activityRecorderFor(lay layout.Layout, name, folder string) func(kind, branch, prompt string) {
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
