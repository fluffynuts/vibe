package activity

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

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
//
// HEAD's reflog is the timeline: it says which branch HEAD was on (its checkouts) and what was done there, and it keeps
// a branch's work after the branch is deleted, which deletes the branch's own reflog. A branch's reflog adds only what
// HEAD's hasn't (a branch moved while HEAD was elsewhere).
func FromHistory(h review.GitHistory, folder string) ([]Event, []Bullet) {
	repo := RepoName(h.Origin, folder)
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
	// byHead are the moves HEAD's reflog recorded, by commit and time: a branch's reflog has them too.
	byHead := map[string]bool{}
	moveKey := func(e review.ReflogEntry) string { return e.New + "@" + e.At.UTC().Format(time.RFC3339) }
	current := currentAtStart(entries, h.Current)
	onto := ""
	// record is what one move of branch tells: an event, and a bullet for what the user did.
	record := func(e review.ReflogEntry, branch string) {
		event(Event{At: e.At, Folder: folder, Repo: repo, Branch: branch})
		bullet := func(text string, at time.Time) {
			bullets = append(bullets, Bullet{At: at, Repo: repo, Branch: branch, Text: text})
		}
		switch {
		case rebaseFinish.MatchString(e.Message):
			target := onto
			if target == "" {
				target = "a new base"
			}
			bullet("rebased onto "+branchName(target), e.At)
			onto = ""
		case mergeEntry.MatchString(e.Message):
			bullet("merged "+branchName(mergeEntry.FindStringSubmatch(e.Message)[1]), e.At)
		case strings.HasPrefix(e.Message, "commit") || strings.HasPrefix(e.Message, "cherry-pick") || strings.HasPrefix(e.Message, "revert"):
			if strings.HasPrefix(e.Message, "commit (amend)") {
				if i, ok := bulletOf[e.Old]; ok {
					dropped[i] = true
				}
			}
			c, ok := h.Commits[e.New]
			if !ok || !byUser(c, h.UserEmail) {
				return
			}
			text := c.Subject
			if m := mergeSubject.FindStringSubmatch(c.Subject); c.Parents > 1 && m != nil {
				text = "merged " + branchName(m[1])
			}
			bulletOf[e.New] = len(bullets)
			bullet(text, c.AuthorTime)
		}
	}
	for _, e := range entries {
		if e.Ref != "HEAD" {
			continue
		}
		byHead[moveKey(e)] = true
		switch m := checkout.FindStringSubmatch(e.Message); {
		case rebaseStart.MatchString(e.Message):
			onto = rebaseStart.FindStringSubmatch(e.Message)[2]
		case m != nil:
			current = ""
			if isBranch(m[1]) {
				current = m[1]
				event(Event{At: e.At, Folder: folder, Repo: repo, Branch: current})
			}
		case strings.HasPrefix(e.Message, "rebase"):
			// The steps of a rebase are on a detached HEAD: its finish, returning to the branch, is the one event.
			if b, ok := strings.CutPrefix(rebaseReturn.ReplaceAllString(e.Message, ""), "refs/heads/"); ok && rebaseReturn.MatchString(e.Message) {
				record(e, b)
			}
		case current != "":
			record(e, current)
		}
	}
	for _, e := range entries {
		branch, ok := strings.CutPrefix(e.Ref, "refs/heads/")
		if !ok || byHead[moveKey(e)] {
			continue
		}
		if rebaseFinish.MatchString(e.Message) && byHeadRebase(entries, e) {
			continue
		}
		record(e, branch)
	}
	kept := bullets[:0]
	for i, b := range bullets {
		if !dropped[i] {
			kept = append(kept, b)
		}
	}
	return events, kept
}

var rebaseReturn = regexp.MustCompile(`^rebase( -i)? \(finish\): returning to `)

// byHeadRebase says HEAD's reflog has the rebase a branch's "rebase (finish)" entry is of: their times are the same.
func byHeadRebase(entries []review.ReflogEntry, e review.ReflogEntry) bool {
	for _, h := range entries {
		if h.Ref == "HEAD" && rebaseReturn.MatchString(h.Message) && h.At.Equal(e.At) {
			return true
		}
	}
	return false
}

// currentAtStart is the branch HEAD was on before the first of entries: the one its first checkout moved from, else the
// one it is on now (no checkout since).
func currentAtStart(entries []review.ReflogEntry, now string) string {
	for _, e := range entries {
		if e.Ref != "HEAD" {
			continue
		}
		if m := checkoutFrom.FindStringSubmatch(e.Message); m != nil {
			if isBranch(m[1]) {
				return m[1]
			}
			return ""
		}
	}
	return now
}

var checkoutFrom = regexp.MustCompile(`^checkout: moving from (.+) to .+$`)

// isBranch says a name a checkout moved to is a branch's, not a commit's (a detached HEAD).
func isBranch(name string) bool {
	return !hexName.MatchString(name) && !strings.ContainsAny(name, "~^") && name != "HEAD"
}

var hexName = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// branchName is how a branch is named in a bullet: master, for refs/heads/master, refs/remotes/origin/master and
// origin/master. Another remote's prefix can't be told from a branch's own folder (feat/x), so is kept.
func branchName(ref string) string {
	name := strings.TrimPrefix(ref, "refs/heads/")
	if rest, ok := strings.CutPrefix(name, "refs/remotes/"); ok {
		if _, branch, ok := strings.Cut(rest, "/"); ok {
			name = branch
		}
	}
	return strings.TrimPrefix(name, "origin/")
}

func byUser(c review.ReflogCommit, email string) bool {
	return email == "" || strings.EqualFold(c.AuthorEmail, email)
}
