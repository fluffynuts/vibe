package review

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

// maxReflog is the most of a reflog read: its end, which is its newest. A week of even busy work is far less.
const maxReflog = 4 << 20

// maxReflogCommits bounds the commits looked up for a reflog's entries.
const maxReflogCommits = 5000

// ReflogEntry is one line of a reflog: Ref moved from Old to New at At, for the reason Message ("commit: fix the
// thing", "checkout: moving from master to feat/x").
type ReflogEntry struct {
	Ref     string    `json:"ref"`
	Old     string    `json:"old"`
	New     string    `json:"new"`
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

// ReflogCommit is what the activity page needs of a commit a reflog names.
type ReflogCommit struct {
	AuthorEmail string    `json:"authorEmail"`
	AuthorTime  time.Time `json:"authorTime"`
	Subject     string    `json:"subject"`
	Parents     int       `json:"parents"`
}

// GitHistory is what git recorded in a repository since a time: its reflogs (HEAD's, and each local branch's), the
// commits they name, who the user is there, and the repository's origin.
type GitHistory struct {
	// Repository is false for a folder in no repository, which has no history: nothing else is set.
	Repository bool   `json:"repository"`
	Origin     string `json:"origin"`
	UserEmail  string `json:"userEmail"`
	// Branches are the local branches there are; RemoteBranches, origin's as this repository last fetched them (refs/remotes/origin).
	Branches       []string `json:"branches"`
	RemoteBranches []string `json:"remoteBranches"`
	// Current is the branch HEAD is on now, "" when it is detached: what HEAD's reflog was on before its first checkout
	// in it.
	Current string                  `json:"current"`
	Entries []ReflogEntry           `json:"entries"`
	Commits map[string]ReflogCommit `json:"commits"`
}

// History is what git recorded in the repository the folder dir is in, since since (see GitHistory).
func History(ctx context.Context, dir string, since time.Time) (GitHistory, error) {
	var out GitHistory
	return out, isolated(ctx, "history", helperArgs{Dir: dir, Ref: since.UTC().Format(time.RFC3339)}, &out)
}

func historyOf(dir, sinceText string) (GitHistory, error) {
	since, err := time.Parse(time.RFC3339, sinceText)
	if err != nil {
		return GitHistory{}, fmt.Errorf("since %q isn't a time: %w", sinceText, err)
	}
	r, err := openRepo(dir)
	if errors.Is(err, errNotRepository) {
		return GitHistory{}, nil
	}
	if err != nil {
		return GitHistory{}, err
	}
	defer r.close()
	h := GitHistory{Repository: true, Commits: map[string]ReflogCommit{}, UserEmail: r.setting("user", "email"), Current: r.branch()}
	if remote, err := r.git.Remote("origin"); err == nil && len(remote.Config().URLs) > 0 {
		h.Origin = remote.Config().URLs[0]
	}
	if data, err := readTail(r.gitDir, "logs/HEAD", maxReflog); err == nil {
		h.Entries = append(h.Entries, parseReflog("HEAD", data, since)...)
	}
	heads := path.Join("logs", "refs", "heads")
	fs.WalkDir(r.common.FS(), heads, func(name string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		branch := strings.TrimPrefix(name, heads+"/")
		if data, err := readTail(r.common, name, maxReflog); err == nil {
			h.Entries = append(h.Entries, parseReflog("refs/heads/"+branch, data, since)...)
		}
		return nil
	})
	if refs, err := r.git.References(); err == nil {
		refs.ForEach(func(ref *plumbing.Reference) error {
			switch name := ref.Name(); {
			case name.IsBranch():
				h.Branches = append(h.Branches, name.Short())
			case name.IsRemote() && strings.HasPrefix(name.String(), "refs/remotes/origin/") && name.String() != "refs/remotes/origin/HEAD":
				h.RemoteBranches = append(h.RemoteBranches, strings.TrimPrefix(name.String(), "refs/remotes/origin/"))
			}
			return nil
		})
	}
	for _, e := range h.Entries {
		if len(h.Commits) >= maxReflogCommits {
			break
		}
		// HEAD's too: a deleted branch's commits are only in HEAD's reflog.
		if _, done := h.Commits[e.New]; done {
			continue
		}
		c, err := r.git.CommitObject(plumbing.NewHash(e.New))
		if err != nil {
			continue
		}
		subject, _, _ := strings.Cut(c.Message, "\n")
		h.Commits[e.New] = ReflogCommit{AuthorEmail: c.Author.Email, AuthorTime: c.Author.When, Subject: strings.TrimSpace(subject), Parents: c.NumParents()}
	}
	return h, nil
}

// readTail reads the end of the plain file name in root, up to limit bytes, from the start of a line: nothing outside
// root, and not a FIFO.
func readTail(root *os.Root, name string, limit int64) ([]byte, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|nonBlocking, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("it isn't a plain file")
	}
	from := info.Size() - limit
	if from < 0 {
		from = 0
	}
	data, err := io.ReadAll(io.NewSectionReader(f, from, limit))
	if err != nil {
		return nil, err
	}
	if from > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return data, nil
}

// parseReflog is the entries of ref's reflog, data, from since on. A line is
// "<old> <new> <name> <<email>> <unix time> <zone>\t<message>"; one that isn't is skipped.
func parseReflog(ref string, data []byte, since time.Time) []ReflogEntry {
	var out []ReflogEntry
	for _, line := range strings.Split(string(data), "\n") {
		head, message, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(head)
		if len(fields) < 4 {
			continue
		}
		secs, err := strconv.ParseInt(fields[len(fields)-2], 10, 64)
		if err != nil {
			continue
		}
		at := time.Unix(secs, 0)
		if at.Before(since) {
			continue
		}
		out = append(out, ReflogEntry{Ref: ref, Old: fields[0], New: fields[1], At: at, Message: message})
	}
	return out
}

// Ahead is how many commits HEAD has that a commit hasn't; Known is false when the repository hasn't that commit.
type Ahead struct {
	Count int  `json:"count"`
	Known bool `json:"known"`
}

// AheadOf is how many commits the HEAD of the folder dir has that sha (GitHub's copy of its branch, say) hasn't.
func AheadOf(ctx context.Context, dir, sha string) (Ahead, error) {
	var out Ahead
	return out, isolated(ctx, "aheadOf", helperArgs{Dir: dir, Ref: sha}, &out)
}

func aheadOf(dir, sha string) (Ahead, error) {
	if !plumbing.IsHash(sha) {
		return Ahead{}, fmt.Errorf("%q isn't a commit's hash", sha)
	}
	r, err := openRepo(dir)
	if err != nil {
		return Ahead{}, err
	}
	defer r.close()
	head, err := r.head()
	if err != nil || head == nil {
		return Ahead{}, err
	}
	other, err := r.git.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return Ahead{}, nil
	}
	ahead, _ := aheadBehind(head, other)
	return Ahead{Count: ahead, Known: true}, nil
}
