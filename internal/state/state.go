// Package state persists per-sandbox instance records under ~/.vibe/instances
// so vibe can resolve --stop/--ssh/--re-init/--list against the profile and
// target folder a sandbox was created with, without the user repeating them.
package state

import (
	"fmt"
	"os"
	"path/filepath"
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
	Name      string          `yaml:"name"`
	Profile   string          `yaml:"profile"`
	Target    string          `yaml:"target"`
	Publish   []PublishRecord `yaml:"publish,omitempty"`
	CreatedAt time.Time       `yaml:"createdAt"`
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

// Remove deletes a sandbox's instance record, if any.
func Remove(vibeHome, name string) error {
	err := os.Remove(path(vibeHome, name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing instance record: %w", err)
	}
	return nil
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
