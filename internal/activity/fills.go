package activity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vibe/internal/fscopy"
)

// Fills keeps the fill (a colour, then a colour and pattern) each repository is drawn with in the week's charts: the page's
// palette, by number. A repository keeps its fill while it is worked on in the days kept, so a repository is drawn the same in
// every week the page can show; one not seen since is freed for the next new one. Vibes share the file: two giving out fills at
// once read the same file and give the same repository the same fill; a clash between different repositories is put right by the
// next look, which finds the fill taken.
type Fills struct {
	Path string
}

type fill struct {
	Slot int    `json:"slot"`
	Seen string `json:"seen"`
}

// Assign gives each of repos its fill: the one it has, or else the lowest free, the longest of the week first. days are the
// week's dates (Monday first, as the repositories' Days); a repository's fill is kept as seen on the last of them it had time.
// Fills of repositories not seen on or after first (the first day kept) are freed. A file that can't be read as fills is begun
// afresh: the fills are given out, and the error says so.
func (f Fills) Assign(repos []RepoWeek, days []string, first time.Time) (map[string]int, error) {
	kept := map[string]fill{}
	data, err := fscopy.ReadFile(f.Path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var unreadable error
	if err == nil {
		if jerr := json.Unmarshal(data, &kept); jerr != nil {
			kept = map[string]fill{}
			unreadable = fmt.Errorf("the charts' colours, kept in %s, couldn't be read (%v), so they were given out afresh", f.Path, jerr)
		}
	}
	from := first.Format(time.DateOnly)
	changed := false
	for repo, k := range kept {
		if k.Seen < from {
			delete(kept, repo)
			changed = true
		}
	}
	taken := map[int]bool{}
	for _, k := range kept {
		taken[k.Slot] = true
	}
	out := map[string]int{}
	for _, r := range repos {
		seen := ""
		for i, h := range r.Days {
			if h > 0 && i < len(days) {
				seen = days[i]
			}
		}
		k, ok := kept[r.Repo]
		if !ok {
			k.Slot = 0
			for taken[k.Slot] {
				k.Slot++
			}
			taken[k.Slot] = true
			changed = true
		}
		if seen > k.Seen {
			k.Seen = seen
			changed = true
		}
		kept[r.Repo] = k
		out[r.Repo] = k.Slot
	}
	if !changed && unreadable == nil {
		return out, nil
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return nil, err
	}
	data, err = json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := fscopy.WriteWholeMode(f.Path, data, 0o600); err != nil {
		return out, errors.Join(unreadable, err)
	}
	return out, unreadable
}
