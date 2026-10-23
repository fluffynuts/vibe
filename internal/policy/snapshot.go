package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"vibe/internal/fscopy"
)

// Snapshot is a sandbox's own network rules as they were at Taken, kept on
// the host: sbx forgets them with the sandbox, so one made again (rebuilt,
// or removed with sbx and made anew) gets them back from here.
type Snapshot struct {
	Taken time.Time   `json:"taken"`
	Rules []SavedRule `json:"rules"`
}

// SavedRule is one of a sandbox's own rules: Allowed or Blocked, and what
// it's for.
type SavedRule struct {
	Decision  string   `json:"decision"`
	Resources []string `json:"resources"`
}

// Own is the rules in rules that are sandbox's own: scoped to it, and
// editable. Global rules apply to every sandbox anyway, and its kit's
// (which sbx says aren't editable) come back with its profile.
func Own(rules Rules, sandbox string) []SavedRule {
	own := []SavedRule{}
	for _, r := range rules.Rules {
		if r.Scope == "sandbox:"+sandbox && r.Removable {
			own = append(own, SavedRule{Decision: r.Decision, Resources: r.Resources})
		}
	}
	return own
}

// SnapshotTo keeps the sandbox's own network rules in file, as they are
// now, replacing what was kept: a rule removed since stays gone. Rules that
// can't be read leave file as it was.
func (c *Client) SnapshotTo(file string, now time.Time) (Snapshot, error) {
	rules, err := c.NetworkRules()
	if err != nil {
		return Snapshot{}, fmt.Errorf("the sandbox's network rules couldn't be read, so the ones kept in %s were left as they were: %w", file, err)
	}
	snap := Snapshot{Taken: now.UTC(), Rules: Own(rules, c.Sandbox)}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return Snapshot{}, err
	}
	if err := fscopy.WriteWhole(file, data); err != nil {
		return Snapshot{}, fmt.Errorf("the sandbox's network rules couldn't be kept in %s: %w", file, err)
	}
	return snap, nil
}

// LoadSnapshot is the snapshot kept in file, and whether there is one.
func LoadSnapshot(file string) (Snapshot, bool, error) {
	data, err := fscopy.ReadFile(file)
	if os.IsNotExist(err) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, false, fmt.Errorf("the network rules kept in %s can't be read: %w", file, err)
	}
	return snap, true, nil
}

// Restore gives the sandbox snap's rules: what it put back ("allowed
// example.com"), and, for each it couldn't, why and the sbx command that
// would.
func (c *Client) Restore(snap Snapshot) (restored []string, failed []error) {
	for _, r := range snap.Rules {
		command, do, done := c.AllowCommand, c.Allow, "allowed"
		if r.Decision == Blocked {
			command, do, done = c.DenyCommand, c.Deny, "blocked"
		}
		for _, host := range r.Resources {
			if err := do(host); err != nil {
				failed = append(failed, fmt.Errorf("%s %s wasn't put back (%v): to have it back, run: sbx %s", strings.TrimSuffix(done, "ed"), host, err, strings.Join(command(host), " ")))
				continue
			}
			restored = append(restored, done+" "+host)
		}
	}
	return restored, failed
}
