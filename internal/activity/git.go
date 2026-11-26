package activity

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"vibe/internal/review"
)

// RepoName is the name a repository is grouped by: its name on origin (yumbi-web, for every checkout of it), else the
// folder's.
func RepoName(origin, folder string) string {
	name := strings.TrimSuffix(strings.TrimRight(origin, "/"), ".git")
	if i := strings.LastIndexAny(name, "/:\\"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		return filepath.Base(folder)
	}
	return name
}

var (
	rebaseStart  = regexp.MustCompile(`^rebase( -i)? \(start\): checkout (.+)$`)
	rebaseFinish = regexp.MustCompile(`^rebase( -i)? \(finish\): `)
	checkout     = regexp.MustCompile(`^checkout: moving from .+ to (.+)$`)
	mergeEntry   = regexp.MustCompile(`^merge ([^:]+):`)
	mergeSubject = regexp.MustCompile(`^Merge (?:remote-tracking )?branch '([^']+)'`)
)

// FromHistory is what git recorded in the folder: each change to a branch (a commit, merge, rebase, reset, checkout)
// as an event, and the user's own commits, merges and rebases as bullets. A rebase is one event, whatever it picked.
func FromHistory(h review.GitHistory, folder string) ([]Event, []Bullet) {
	repo := RepoName(h.Origin, folder)
	branches := map[string]bool{}
	for _, b := range h.Branches {
		branches[b] = true
	}
	entries := append([]review.ReflogEntry(nil), h.Entries...)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.Before(entries[j].At) })
	var events []Event
	seen := map[Event]bool{}
	event := func(at Event) {
		if !seen[at] {
			seen[at] = true
			events = append(events, at)
		}
	}
	var bullets []Bullet
	// bulletOf is the bullet each commit gave, so an amended one can be taken back.
	bulletOf := map[string]int{}
	dropped := map[int]bool{}
	onto := ""
	for _, e := range entries {
		if e.Ref == "HEAD" {
			if m := rebaseStart.FindStringSubmatch(e.Message); m != nil {
				onto = m[2]
			} else if m := checkout.FindStringSubmatch(e.Message); m != nil && branches[m[1]] {
				event(Event{At: e.At, Folder: folder, Repo: repo, Branch: m[1]})
			}
			continue
		}
		branch, ok := strings.CutPrefix(e.Ref, "refs/heads/")
		if !ok {
			continue
		}
		event(Event{At: e.At, Folder: folder, Repo: repo, Branch: branch})
		bullet := func(text string, at Bullet) {
			at.Repo, at.Branch, at.Text = repo, branch, text
			bullets = append(bullets, at)
		}
		switch {
		case rebaseFinish.MatchString(e.Message):
			target := onto
			if target == "" {
				target = "a new base"
			}
			bullet("rebased onto "+strings.TrimPrefix(target, "origin/"), Bullet{At: e.At})
			onto = ""
		case mergeEntry.MatchString(e.Message):
			bullet("merged "+strings.TrimPrefix(mergeEntry.FindStringSubmatch(e.Message)[1], "origin/"), Bullet{At: e.At})
		case strings.HasPrefix(e.Message, "commit") || strings.HasPrefix(e.Message, "cherry-pick") || strings.HasPrefix(e.Message, "revert"):
			if strings.HasPrefix(e.Message, "commit (amend)") {
				if i, ok := bulletOf[e.Old]; ok {
					dropped[i] = true
				}
			}
			c, ok := h.Commits[e.New]
			if !ok || !byUser(c, h.UserEmail) {
				continue
			}
			text := c.Subject
			if m := mergeSubject.FindStringSubmatch(c.Subject); c.Parents > 1 && m != nil {
				text = "merged " + strings.TrimPrefix(m[1], "origin/")
			}
			bulletOf[e.New] = len(bullets)
			bullet(text, Bullet{At: c.AuthorTime})
		}
	}
	kept := bullets[:0]
	for i, b := range bullets {
		if !dropped[i] {
			kept = append(kept, b)
		}
	}
	return events, kept
}

func byUser(c review.ReflogCommit, email string) bool {
	return email == "" || strings.EqualFold(c.AuthorEmail, email)
}
