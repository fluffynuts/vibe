// Package settings loads and merges vibe's settings.yaml files: one at the
// bundle root (base) and optionally one per profile (override).
package settings

import (
	"bytes"
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
	// DefaultFeatures are the library features a guided profile starts with
	// them ticked. They are a starting point, not a requirement: the user
	// can untick any of them while picking.
	DefaultFeatures []string `yaml:"defaultFeatures,omitempty"`
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
	if s, err = Parse(data); err != nil {
		return s, fmt.Errorf("parsing %s: %w", path, err)
	}
	return s, nil
}

// Parse reads the settings in a settings.yaml's content.
func Parse(data []byte) (Settings, error) {
	var s Settings
	err := yaml.Unmarshal(data, &s)
	return s, err
}

// Merge overlays override on top of base: scalars are replaced when the
// override sets a non-zero value, Publish entries are appended, Env maps
// are merged key-by-key with override winning on conflicts, and a non-empty
// DefaultFeatures replaces the one below it.
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

	// unlike Publish, a list of default features replaces rather than adds
	// to the one below it: "start from these" is not something two layers
	// can both be half-right about.
	if len(override.DefaultFeatures) > 0 {
		out.DefaultFeatures = override.DefaultFeatures
	}

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

// Set returns the settings.yaml in data with key set to value, keeping
// everything else in the file — other settings, and the comments — as it
// was. A key the file doesn't have is added at the end; an empty data is a
// file with nothing in it yet.
func Set(data []byte, key string, value interface{}) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("not a settings file: expected a mapping of settings")
	}
	var v yaml.Node
	if err := v.Encode(value); err != nil {
		return nil, err
	}
	m := doc.Content[0]
	found := false
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != key {
			continue
		}
		old := m.Content[i+1]
		v.HeadComment, v.LineComment, v.FootComment = old.HeadComment, old.LineComment, old.FootComment
		m.Content[i+1] = &v
		found = true
		break
	}
	if !found {
		m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &v)
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
