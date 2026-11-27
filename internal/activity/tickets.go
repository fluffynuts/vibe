package activity

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"vibe/internal/fscopy"
)

// Tickets are the links the user gave branches to their tickets (a Trello card, a Jira or GitHub issue), by repository and branch,
// kept on the host for every vibe: copied ahead of a branch's summary, they make its timesheet entry. One that names a branch gone
// from every checkout, and unmentioned all week, is forgotten (see Desk.forgetTickets): a branch's name is more often reused months
// apart than within a week.
type Tickets struct {
	Path string
	mu   sync.Mutex
}

// maxTicketLink bounds a link kept.
const maxTicketLink = 2000

// TicketKey is how a branch's link is kept: "repo branch" (a repository's name has no spaces; a branch's can't).
func TicketKey(repo, branch string) string { return repo + " " + branch }

type ticketEntry struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	URL    string `json:"url"`
}

// All are the links kept, by TicketKey.
func (t *Tickets) All() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.read()
}

func (t *Tickets) read() map[string]string {
	out := map[string]string{}
	data, err := fscopy.ReadFile(t.Path)
	if err != nil {
		return out
	}
	var entries []ticketEntry
	if json.Unmarshal(data, &entries) != nil {
		return out
	}
	for _, e := range entries {
		out[TicketKey(e.Repo, e.Branch)] = e.URL
	}
	return out
}

func (t *Tickets) write(all map[string]string) error {
	entries := []ticketEntry{}
	for k, u := range all {
		repo, branch, _ := strings.Cut(k, " ")
		entries = append(entries, ticketEntry{Repo: repo, Branch: branch, URL: u})
	}
	sort.Slice(entries, func(i, j int) bool {
		return TicketKey(entries[i].Repo, entries[i].Branch) < TicketKey(entries[j].Repo, entries[j].Branch)
	})
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o700); err != nil {
		return err
	}
	return fscopy.WriteWholeMode(t.Path, append(data, '\n'), 0o600)
}

// CheckTicketLink says why link can't be a ticket's: it must be a web address (http or https, with a host), which the page links
// to.
func CheckTicketLink(link string) error {
	if len(link) > maxTicketLink {
		return fmt.Errorf("that link is longer than %d characters", maxTicketLink)
	}
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.ContainsAny(link, " \t\n") {
		return fmt.Errorf("%q isn't a web address (https://…)", link)
	}
	return nil
}

// Set keeps link for repo's branch; "" forgets it.
func (t *Tickets) Set(repo, branch, link string) error {
	link = strings.TrimSpace(link)
	if link != "" {
		if err := CheckTicketLink(link); err != nil {
			return err
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	all := t.read()
	if link == "" {
		delete(all, TicketKey(repo, branch))
	} else {
		all[TicketKey(repo, branch)] = link
	}
	return t.write(all)
}

// forget forgets the links gone says to.
func (t *Tickets) forget(gone func(repo, branch string) bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	all := t.read()
	changed := false
	for k := range all {
		repo, branch, _ := strings.Cut(k, " ")
		if gone(repo, branch) {
			delete(all, k)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return t.write(all)
}
