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
	Memory string `yaml:"memory,omitempty"`
	// CPUs is how many CPUs a sandbox gets; 0 (unset) means half the host's.
	// sbx itself would give a sandbox all of them.
	CPUs     int    `yaml:"cpus,omitempty"`
	BasePort int    `yaml:"basePort,omitempty"`
	Agent    string `yaml:"agent,omitempty"`
	NugetDir string `yaml:"nugetDir,omitempty"`
	// Packages is what vibe does about a package the agent asks the host
	// for: "ask" (the default) holds each request for the user's approval on
	// the companion page's Admin tab; "allow" fetches at once.
	Packages string `yaml:"packages,omitempty"`
	// Nuget is how a sandbox gets the host's NuGet packages: "fallback"
	// (the default) gives it the host's package cache read-only and
	// nothing else of NugetDir; "overlay" is that cache overlaid at its own
	// path in the sandbox, so what a restore writes names the host's path
	// (experimental: a version can't be added beside one the host has);
	// "shared" mounts all of NugetDir read-write,
	// which lets the agent plant code in packages the host builds with,
	// and read its feeds' credentials.
	Nuget      string `yaml:"nuget,omitempty"`
	MemoryRoot string `yaml:"memoryRoot,omitempty"`
	// Companion says what vibe does about the companion page it serves for
	// a running sandbox: "open" serves it and opens it in the browser,
	// "serve" serves it and only prints the URL, "off" doesn't serve it.
	// Unset means "open".
	Companion string            `yaml:"companion,omitempty"`
	Publish   []PublishEntry    `yaml:"publish,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	// DefaultFeatures are the library features a guided profile starts with
	// them ticked. They are a starting point, not a requirement: the user
	// can untick any of them while picking.
	DefaultFeatures []string `yaml:"defaultFeatures,omitempty"`
	// AutoUpdate false stops a session start bringing an out-of-date
	// profile and sandbox up to date. Unset means it does.
	AutoUpdate *bool `yaml:"autoUpdate,omitempty"`
	// DefaultBranch is the name the companion page suggests for a
	// repository's default branch when origin can't say which it is: the
	// one last set there.
	DefaultBranch string `yaml:"defaultBranch,omitempty"`
	// Mode is how the user likes vibe to start: "headless", serving the
	// companion page with no terminal, or "terminal". Unset means terminal.
	// It's the user's own, read from ~/.vibe/settings.yaml only.
	Mode string `yaml:"mode,omitempty"`
	// Model and Effort are what the conversation tab asks the agent to use
	// (see internal/modelchoice). Unset, the agent's own settings decide.
	// The page writes them into the profile's settings.yaml.
	Model  string `yaml:"model,omitempty"`
	Effort string `yaml:"effort,omitempty"`
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
	if override.CPUs != 0 {
		out.CPUs = override.CPUs
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
	if override.Packages != "" {
		out.Packages = override.Packages
	}
	if override.Nuget != "" {
		out.Nuget = override.Nuget
	}
	if override.MemoryRoot != "" {
		out.MemoryRoot = override.MemoryRoot
	}
	if override.Companion != "" {
		out.Companion = override.Companion
	}
	if override.AutoUpdate != nil {
		out.AutoUpdate = override.AutoUpdate
	}
	if override.Model != "" {
		out.Model = override.Model
	}
	if override.Effort != "" {
		out.Effort = override.Effort
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

// Unset returns the settings.yaml in data without key, keeping everything
// else in the file as it was. A file without the key is returned as it is.
func Unset(data []byte, key string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 {
		return data, nil
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("not a settings file: expected a mapping of settings")
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != key {
			continue
		}
		m.Content = append(m.Content[:i], m.Content[i+2:]...)
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
	return data, nil
}
