// Package state persists per-sandbox instance records under ~/.vibe/instances
// so vibe can resolve --stop/--ssh/--re-init/--list against the profile and
// target folder a sandbox was created with, without the user repeating them.
package state

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// PublishRecord remembers one published port so its URL stays stable across
// restarts, and so vibe can report it back on a later attach without
// re-reading the profile.
type PublishRecord struct {
	Name          string `yaml:"name"`
	ContainerPort int    `yaml:"containerPort"`
	HostPort      int    `yaml:"hostPort"`
}

// Instance records how a sandbox was created.
type Instance struct {
	Name    string          `yaml:"name"`
	Profile string          `yaml:"profile"`
	Target  string          `yaml:"target"`
	Publish []PublishRecord `yaml:"publish,omitempty"`
	// MemoryStore is the host directory mounted in for the agent's
	// memories, so they can be copied back out when a session ends.
	MemoryStore string `yaml:"memoryStore,omitempty"`
	// Timezone is the zone the sandbox keeps its local time in, put back
	// at every session start. Empty in records from before it was chosen,
	// which are put on the host's.
	Timezone string `yaml:"timezone,omitempty"`
	// Agent is the agent the sandbox was created to run, so a rebuild keeps
	// it rather than falling back to the settings' default. Empty in records
	// from before it was chosen per sandbox, which use the settings'.
	Agent string `yaml:"agent,omitempty"`
	// Memory and CPUs are what the sandbox was created with (the sizes sbx
	// takes, "12g" and a number), so a rebuild keeps them rather than
	// falling back to the settings. Empty and zero in records from before
	// they were kept, which use the settings'.
	Memory string `yaml:"memory,omitempty"`
	CPUs   int    `yaml:"cpus,omitempty"`
	// CompanionPort is the host port the companion page is served on, kept
	// so its URL (and a bookmark of it) survives restarts. Zero until the
	// page has first been served.
	CompanionPort int `yaml:"companionPort,omitempty"`
	// GitToken says where the GitHub token the agent pushes with came
	// from — "default" (the one --setup stored) or "own" (one given for
	// this sandbox) — or is empty when the sandbox has none, which is how
	// every sandbox starts. The token itself is kept apart, in
	// GitTokenPath, readable by the user alone.
	GitToken string `yaml:"gitToken,omitempty"`
	// GitRules are the rules the agent was given for pushing, besides
	// never force-pushing (see gittoken.RuleOrder).
	GitRules []string `yaml:"gitRules,omitempty"`
	// GitTokenExpires is when the token stops working, as GitHub said when
	// it was given; zero when it never does, or GitHub didn't say. vibe
	// warns as it nears, at every session start.
	GitTokenExpires time.Time `yaml:"gitTokenExpires,omitempty"`
	// KitHash fingerprints what the sandbox was built from (see
	// kitFingerprint), to tell when it's out of date. Empty in records from
	// before it was kept, which are.
	KitHash   string    `yaml:"kitHash,omitempty"`
	CreatedAt time.Time `yaml:"createdAt"`
}

// Dir returns the instances directory under the given vibe home.
func Dir(vibeHome string) string {
	return filepath.Join(vibeHome, "instances")
}

func path(vibeHome, name string) string {
	return filepath.Join(Dir(vibeHome), name+".yaml")
}

// Save writes (or overwrites) the instance record for a sandbox.
func Save(vibeHome string, inst Instance) error {
	if err := os.MkdirAll(Dir(vibeHome), 0o755); err != nil {
		return fmt.Errorf("creating instances dir: %w", err)
	}
	data, err := yaml.Marshal(inst)
	if err != nil {
		return fmt.Errorf("encoding instance record: %w", err)
	}
	if err := os.WriteFile(path(vibeHome, inst.Name), data, 0o644); err != nil {
		return fmt.Errorf("writing instance record: %w", err)
	}
	return nil
}

// Load reads a sandbox's instance record. It returns (Instance{}, false, nil)
// when no record exists.
func Load(vibeHome, name string) (Instance, bool, error) {
	data, err := os.ReadFile(path(vibeHome, name))
	if err != nil {
		if os.IsNotExist(err) {
			return Instance{}, false, nil
		}
		return Instance{}, false, fmt.Errorf("reading instance record: %w", err)
	}
	var inst Instance
	if err := yaml.Unmarshal(data, &inst); err != nil {
		return Instance{}, false, fmt.Errorf("parsing instance record: %w", err)
	}
	return inst, true, nil
}

// Remove deletes a sandbox's instance record, its companion and git
// tokens, and what its companion page keeps of the conversation, if any.
func Remove(vibeHome, name string) error {
	err := os.Remove(path(vibeHome, name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing instance record: %w", err)
	}
	err = os.Remove(tokenPath(vibeHome, name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing companion token: %w", err)
	}
	err = os.Remove(GitTokenPath(vibeHome, name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing git token: %w", err)
	}
	for _, file := range []string{ConversationPath(vibeHome, name), TranscriptPath(vibeHome, name)} {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing companion conversation: %w", err)
		}
	}
	if err := os.RemoveAll(ClipboardPath(vibeHome, name)); err != nil {
		return fmt.Errorf("removing clipboard history: %w", err)
	}
	if err := os.RemoveAll(ReviewPath(vibeHome, name)); err != nil {
		return fmt.Errorf("removing reviews: %w", err)
	}
	if err := os.RemoveAll(PackagesPath(vibeHome, name)); err != nil {
		return fmt.Errorf("removing package requests: %w", err)
	}
	return nil
}

// Move takes everything vibe keeps for sandbox oldName (its record, page
// token, GitHub token, conversation, clipboard history, reviews and package requests) to newName, for
// a sandbox that has moved folders. The record's Name follows; anything
// else in it is the caller's to change. Another sandbox's record already at
// newName is never overwritten.
func Move(vibeHome, oldName, newName string) error {
	if oldName == newName {
		return nil
	}
	if _, err := os.Stat(path(vibeHome, newName)); err == nil {
		return fmt.Errorf("vibe already has a record of a sandbox '%s'", newName)
	}
	inst, found, err := Load(vibeHome, oldName)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("vibe has no record of a sandbox '%s'", oldName)
	}
	for _, pair := range [][2]string{
		{tokenPath(vibeHome, oldName), tokenPath(vibeHome, newName)},
		{GitTokenPath(vibeHome, oldName), GitTokenPath(vibeHome, newName)},
		{ConversationPath(vibeHome, oldName), ConversationPath(vibeHome, newName)},
		{TranscriptPath(vibeHome, oldName), TranscriptPath(vibeHome, newName)},
		{ClipboardPath(vibeHome, oldName), ClipboardPath(vibeHome, newName)},
		{ReviewPath(vibeHome, oldName), ReviewPath(vibeHome, newName)},
		{PackagesPath(vibeHome, oldName), PackagesPath(vibeHome, newName)},
	} {
		if err := MoveIfThere(pair[0], pair[1]); err != nil {
			return err
		}
	}
	inst.Name = newName
	if err := Save(vibeHome, inst); err != nil {
		return err
	}
	if err := os.Remove(path(vibeHome, oldName)); err != nil {
		return fmt.Errorf("removing the old record: %w", err)
	}
	return nil
}

// MoveIfThere renames from to to, when from exists and isn't to already,
// refusing to replace anything already at to.
func MoveIfThere(from, to string) error {
	if from == to {
		return nil
	}
	if _, err := os.Lstat(from); os.IsNotExist(err) {
		return nil
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s is already there, so %s wasn't moved to it", to, from)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("moving %s: %w", from, err)
	}
	return nil
}

// ReviewPath is the folder the companion page's Review tab keeps the
// sandbox's reviews in, on the host, so they outlive a rebuild.
func ReviewPath(vibeHome, name string) string {
	return filepath.Join(vibeHome, "reviews", name)
}

// PackagesPath is the folder a sandbox's requests for packages are kept in,
// on the host: an inbox the sandbox drops them in, and a read-only view of
// the answers (see internal/packagereq).
func PackagesPath(vibeHome, name string) string {
	return filepath.Join(vibeHome, "packages", name)
}

// NugetFetchPath is the folder vibe makes the throwaway projects that fetch
// a sandbox's packages in. NuGet reads a nuget.config from it and every
// folder above it, so none of those may be one the agent can write: it is
// no sandbox's folder, and none is mounted at or above it.
func NugetFetchPath(vibeHome string) string {
	return filepath.Join(vibeHome, "nuget-fetch")
}

// ClipboardPath is the folder the sandbox's clipboard history is kept in on
// the host, mounted into the sandbox so it outlives a rebuild.
func ClipboardPath(vibeHome, name string) string {
	return filepath.Join(vibeHome, "clipboard", name)
}

// GitTokenPath is where the GitHub token a sandbox's agent pushes with is
// kept on the host, so a rebuild can hand it to the new sandbox.
func GitTokenPath(vibeHome, name string) string {
	return filepath.Join(vibeHome, "git", "sandboxes", name+".token")
}

// DefaultGitTokenPath is where the token --setup stores for sandboxes to
// use is kept.
func DefaultGitTokenPath(vibeHome string) string {
	return filepath.Join(vibeHome, "git", "default.token")
}

// ConversationPath is where the companion page keeps its conversation with
// a sandbox's agent between runs of vibe.
func ConversationPath(vibeHome, name string) string {
	return filepath.Join(vibeHome, "companion", name+".conversation.json")
}

// TranscriptPath is where the agent's own record of that conversation is
// kept on the host, so a rebuilt sandbox's agent can be handed it back.
func TranscriptPath(vibeHome, name string) string {
	return filepath.Join(vibeHome, "companion", name+".transcript.tgz")
}

func tokenPath(vibeHome, name string) string {
	return filepath.Join(vibeHome, "companion", name+".token")
}

// CompanionToken returns the secret the companion page for a sandbox asks
// for, making one the first time. It is the same every time, so a bookmark
// of the page keeps working across restarts. It lives only on the host, in
// a file only the user can read: the page can change the sandbox's network
// rules, so the sandbox itself must never learn it.
func CompanionToken(vibeHome, name string) (string, error) {
	return secretIn(tokenPath(vibeHome, name))
}

// managePath is where the Profiles page served on its own ('vibe --manage')
// keeps what it keeps: apart from the sandboxes' own, so no sandbox's name
// can collide with it.
func managePath(vibeHome, file string) string {
	return filepath.Join(vibeHome, "companion", "manage", file)
}

// ManageToken is CompanionToken for the Profiles page served on its own.
func ManageToken(vibeHome string) (string, error) {
	return secretIn(managePath(vibeHome, "token"))
}

// ManagePort is the port the Profiles page was last served on on its own,
// or 0 when it never has been.
func ManagePort(vibeHome string) int {
	data, err := os.ReadFile(managePath(vibeHome, "port"))
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return port
}

// SaveManagePort remembers the port the Profiles page is served on on its
// own, so its URL (and a bookmark of it) stays the same.
func SaveManagePort(vibeHome string, port int) error {
	file := managePath(vibeHome, "port")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(strconv.Itoa(port)+"\n"), 0o600)
}

// secretIn returns the secret kept in file, making one the first time.
func secretIn(file string) (string, error) {
	if data, err := os.ReadFile(file); err == nil {
		if token := strings.TrimSpace(string(data)); len(token) >= 32 {
			return token, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("reading companion token: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("making a companion token: %w", err)
	}
	token := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return "", fmt.Errorf("creating companion dir: %w", err)
	}
	if err := os.WriteFile(file, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("writing companion token: %w", err)
	}
	return token, nil
}

// List returns every known instance record.
func List(vibeHome string) ([]Instance, error) {
	entries, err := os.ReadDir(Dir(vibeHome))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading instances dir: %w", err)
	}
	var out []Instance
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".yaml" {
			continue
		}
		inst, ok, err := Load(vibeHome, name[:len(name)-len(".yaml")])
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, inst)
		}
	}
	return out, nil
}
