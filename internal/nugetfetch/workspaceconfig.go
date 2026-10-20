package nugetfetch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"vibe/internal/fscopy"
	"vibe/internal/plainfile"
)

// maxWorkspaceConfig is the most of a nuget.config that's read.
const maxWorkspaceConfig = 256 << 10

// maxApprovals is how many approvals are kept: the latest.
const maxApprovals = 50

// approvalsMu is held while approvals are read, added to and written back.
var approvalsMu sync.Mutex

// WorkspaceConfig is the nuget.config in the sandbox's folder. Many
// repositories name their private feeds there, but the agent can write it:
// a source it redefines under the name of one of the user's is sent the
// user's stored credentials for that name. So it's used for a fetch only
// once the user has approved it, as the file reads when they did.
type WorkspaceConfig struct {
	// Dir is the sandbox's folder.
	Dir string
	// Approvals is the file the hashes of approved contents are kept in
	// (on the host, where the agent can't write).
	Approvals string
	// HostConfigs are the user's own NuGet config files, to warn of a
	// source the workspace's config would point somewhere else.
	HostConfigs []string
}

// Source is a package source a config names.
type Source struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ConfigInfo is a workspace config, for the user to approve.
type ConfigInfo struct {
	File string `json:"file"`
	// Hash stands for the content: an approval is of that, and any change
	// asks again. Empty when the file can't be read as one.
	Hash string `json:"hash,omitempty"`
	// Text is the whole file, for the user to read: sources aren't all a
	// config does.
	Text    string   `json:"text,omitempty"`
	Sources []Source `json:"sources"`
	// Redefines are the user's own sources this config gives another
	// address, each said in words.
	Redefines []string `json:"redefines,omitempty"`
	Approved  bool     `json:"approved"`
	// Problem is why the file can't be approved, when it can't.
	Problem string `json:"problem,omitempty"`
}

func (w WorkspaceConfig) find() (string, error) {
	entries, err := os.ReadDir(w.Dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Type().IsRegular() && strings.EqualFold(e.Name(), "nuget.config") {
			return filepath.Join(w.Dir, e.Name()), nil
		}
	}
	return "", nil
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// read is the config's content and its hash; empty when there's none to
// read, with why if there's a file.
func (w WorkspaceConfig) read() (path string, data []byte, problem string, err error) {
	path, err = w.find()
	if err != nil || path == "" {
		return "", nil, "", err
	}
	data, err = plainfile.Read(path, maxWorkspaceConfig)
	var tooLarge plainfile.TooLarge
	switch {
	case errors.As(err, &tooLarge):
		return path, nil, fmt.Sprintf("it's too large (over %d KB), so it isn't used", maxWorkspaceConfig>>10), nil
	case err != nil:
		return path, nil, "it can't be read: " + err.Error(), nil
	}
	return path, data, "", nil
}

// Look is the workspace's config as it is now, or nil when it has none.
func (w WorkspaceConfig) Look() (*ConfigInfo, error) {
	path, data, problem, err := w.read()
	if err != nil || path == "" {
		return nil, err
	}
	info := &ConfigInfo{File: filepath.Base(path), Sources: []Source{}, Problem: problem}
	if data == nil {
		return info, nil
	}
	info.Hash = hashOf(data)
	info.Text = string(data)
	sources, err := parseSources(data)
	if err != nil {
		info.Problem = "it isn't a NuGet config that can be read (" + err.Error() + "): shown as it is"
	}
	if sources != nil {
		info.Sources = sources
	}
	info.Redefines = w.redefines(info.Sources)
	info.Approved = w.approved(info.Hash)
	return info, nil
}

// ApprovedContent is the config as the user approved it, which is what it
// is now; nil when there's none, or it isn't (or has changed since).
func (w WorkspaceConfig) ApprovedContent() []byte {
	_, data, _, err := w.read()
	if err != nil || data == nil || !w.approved(hashOf(data)) {
		return nil
	}
	return data
}

// Approve approves the config whose content has hash: the one the user
// saw. If it has changed since, it isn't approved, and why is said.
func (w WorkspaceConfig) Approve(hash string) error {
	_, data, problem, err := w.read()
	switch {
	case err != nil:
		return err
	case data == nil && problem != "":
		return errors.New(problem)
	case data == nil:
		return errors.New("the folder has no nuget.config now")
	case hashOf(data) != hash:
		return errors.New("the nuget.config has changed since you looked at it: look again, then approve")
	}
	// Whoever else approves (another tab, another sandbox's page) adds to the same file, and each
	// reads it, adds to it and writes it back: one at a time, or an approval is lost.
	approvalsMu.Lock()
	defer approvalsMu.Unlock()
	list := w.approvedList()
	if contains(list, hash) {
		return nil
	}
	list = append(list, hash)
	if len(list) > maxApprovals {
		list = list[len(list)-maxApprovals:]
	}
	out, err := json.Marshal(map[string][]string{"approved": list})
	if err != nil {
		return err
	}
	return fscopy.WriteWhole(w.Approvals, out)
}

func (w WorkspaceConfig) approvedList() []string {
	data, err := os.ReadFile(w.Approvals)
	if err != nil {
		return nil
	}
	var file struct {
		Approved []string `json:"approved"`
	}
	if json.Unmarshal(data, &file) != nil {
		return nil
	}
	return file.Approved
}

func (w WorkspaceConfig) approved(hash string) bool {
	return hash != "" && contains(w.approvedList(), hash)
}

// parseSources reads the package sources a NuGet config names. Whatever
// else it holds is for the user to read.
func parseSources(data []byte) ([]Source, error) {
	var doc struct {
		Sources struct {
			Add []struct {
				Key   string `xml:"key,attr"`
				Value string `xml:"value,attr"`
			} `xml:"add"`
		} `xml:"packageSources"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	sources := []Source{}
	for _, a := range doc.Sources.Add {
		sources = append(sources, Source{Name: a.Key, URL: a.Value})
	}
	return sources, nil
}

// redefines says, for each source the user's own config has under a name
// this one gives another address, what changes.
func (w WorkspaceConfig) redefines(sources []Source) []string {
	theirs := map[string]string{}
	for _, file := range w.HostConfigs {
		data, err := plainfile.Read(file, maxWorkspaceConfig)
		if err != nil {
			continue
		}
		if mine, err := parseSources(data); err == nil {
			for _, s := range mine {
				theirs[strings.ToLower(s.Name)] = s.URL
			}
		}
	}
	var out []string
	for _, s := range sources {
		if was, ok := theirs[strings.ToLower(s.Name)]; ok && was != s.URL {
			out = append(out, fmt.Sprintf("it gives your source %q the address %s, not %s: the credentials you keep for %q would be sent there", s.Name, s.URL, was, s.Name))
		}
	}
	return out
}
