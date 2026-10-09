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
	// CompanionPort is the host port the companion page is served on, kept
	// so its URL (and a bookmark of it) survives restarts. Zero until the
	// page has first been served.
	CompanionPort int       `yaml:"companionPort,omitempty"`
	CreatedAt     time.Time `yaml:"createdAt"`
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

// Remove deletes a sandbox's instance record, and its companion token, if
// any.
func Remove(vibeHome, name string) error {
	err := os.Remove(path(vibeHome, name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing instance record: %w", err)
	}
	err = os.Remove(tokenPath(vibeHome, name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing companion token: %w", err)
	}
	return nil
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
	file := tokenPath(vibeHome, name)
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
