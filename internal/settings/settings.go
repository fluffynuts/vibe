// Package settings loads and merges vibe's settings.yaml files: one at the
// bundle root (base) and optionally one per profile (override).
package settings

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// PublishEntry names a service and the container ports it exposes. When
// UrlEnv is set, vibe exports it as an environment variable inside the
// sandbox holding "http://localhost:<host port>" for the entry's first port
// — useful for a profile to surface a stable URL for a dashboard whose host
// port is only known once one is allocated at creation time.
type PublishEntry struct {
	Name   string `yaml:"name"`
	Ports  []int  `yaml:"ports"`
	UrlEnv string `yaml:"urlEnv,omitempty"`
}

// Settings is the merged configuration used to drive sandbox creation.
// Zero values mean "unset" for every scalar field.
type Settings struct {
	Memory     string            `yaml:"memory,omitempty"`
	BasePort   int               `yaml:"basePort,omitempty"`
	Agent      string            `yaml:"agent,omitempty"`
	NugetDir   string            `yaml:"nugetDir,omitempty"`
	MemoryRoot string            `yaml:"memoryRoot,omitempty"`
	Publish    []PublishEntry    `yaml:"publish,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"`
}

// Load reads a settings.yaml file. A missing file yields a zero Settings and
// no error, since only the bundle root's settings.yaml is required.
func Load(path string) (Settings, error) {
	var s Settings
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parsing %s: %w", path, err)
	}
	return s, nil
}

// Merge overlays override on top of base: scalars are replaced when the
// override sets a non-zero value, Publish entries are appended, and Env maps
// are merged key-by-key with override winning on conflicts.
func Merge(base, override Settings) Settings {
	out := base

	if override.Memory != "" {
		out.Memory = override.Memory
	}
	if override.BasePort != 0 {
		out.BasePort = override.BasePort
	}
	if override.Agent != "" {
		out.Agent = override.Agent
	}
	if override.NugetDir != "" {
		out.NugetDir = override.NugetDir
	}
	if override.MemoryRoot != "" {
		out.MemoryRoot = override.MemoryRoot
	}

	out.Publish = append(append([]PublishEntry{}, base.Publish...), override.Publish...)

	if len(base.Env) > 0 || len(override.Env) > 0 {
		out.Env = make(map[string]string, len(base.Env)+len(override.Env))
		for k, v := range base.Env {
			out.Env[k] = v
		}
		for k, v := range override.Env {
			out.Env[k] = v
		}
	}

	return out
}
