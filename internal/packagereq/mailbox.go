// Package packagereq is how a sandbox asks the host for a package it can't
// get itself (see docs/plan-nuget-feeds.md): the agent's command drops a
// request in an inbox folder, and the host answers in a read-only view
// folder, each a folder of the host's mounted into the sandbox. What's in
// the inbox is the agent's to make, so it's read only as a small plain file,
// and nothing the agent wrote decides where an answer goes.
package packagereq

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"vibe/internal/plainfile"
)

const (
	// maxRequest is the most of a request that's read.
	maxRequest = 4096
	// maxTakeAtOnce keeps a flood of requests from being taken in one go.
	maxTakeAtOnce = 100
)

// Request is what the agent asks for.
type Request struct {
	Ecosystem string `json:"ecosystem"`
	ID        string `json:"id"`
	Version   string `json:"version"`
	Framework string `json:"framework,omitempty"`
}

// Status is how a request stands.
type Status string

const (
	Fetched Status = "fetched"
	Failed  Status = "failed"
	Denied  Status = "denied"
	// Waiting is for a request that's with the user, to approve or not.
	Waiting Status = "waiting"
)

// Answer is what the host says of a request: the sandbox's command prints
// the message.
type Answer struct {
	Status  Status
	Message string
}

// Taken is one request out of the inbox, or why it couldn't be.
type Taken struct {
	// Nonce names the request, and where its answer goes: the file's
	// name, which the command made, never anything it wrote inside. Empty
	// when the name isn't one an answer can safely be written to.
	Nonce   string
	Request Request
	Err     error
}

// noncePattern is what a request file is named: the time, then something to
// keep two apart. It sorts oldest first, and can't be a path.
var noncePattern = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// Mailbox is a sandbox's package requests, kept in Dir on the host.
type Mailbox struct{ Dir string }

// Inbox is where the agent's command drops requests (read-write in the sandbox).
func (m Mailbox) Inbox() string { return filepath.Join(m.Dir, "inbox") }

// View is where answers are written (read-only in the sandbox).
func (m Mailbox) View() string { return filepath.Join(m.Dir, "view") }

// Take takes the requests out of the inbox, oldest first. One that can't be
// read is in the result with why, and is taken out all the same: left
// there it would be taken again, forever.
func (m Mailbox) Take() []Taken {
	entries, err := os.ReadDir(m.Inbox())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) > maxTakeAtOnce {
		names = names[:maxTakeAtOnce]
	}
	var taken []Taken
	for _, name := range names {
		path := filepath.Join(m.Inbox(), name)
		t := Taken{Nonce: strings.TrimSuffix(name, ".json")}
		if !noncePattern.MatchString(t.Nonce) {
			t.Nonce = ""
			t.Err = fmt.Errorf("%q isn't named as a request is", name)
		} else if data, err := plainfile.Read(path, maxRequest); err != nil {
			t.Err = fmt.Errorf("%s couldn't be read: %w", name, err)
		} else if err := json.Unmarshal(data, &t.Request); err != nil {
			t.Err = fmt.Errorf("%s isn't a request: %w", name, err)
		}
		taken = append(taken, t)
		_ = os.Remove(path)
	}
	return taken
}

// Reply writes the answer to the request named nonce. The sandbox reads
// the answer by its name, so it's written whole elsewhere and renamed.
func (m Mailbox) Reply(nonce string, a Answer) error {
	if !noncePattern.MatchString(nonce) {
		return fmt.Errorf("%q isn't a request's name: not answering it", nonce)
	}
	if err := os.MkdirAll(m.View(), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.View(), ".answer-")
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintf(tmp, "%s\n%s\n", a.Status, a.Message)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return werr
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(m.View(), nonce+".answer"))
}
