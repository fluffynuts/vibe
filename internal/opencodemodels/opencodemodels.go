// Package opencodemodels reads OpenCode's list of models (opencode models --verbose) into what the page offers:
// each model with its name, the efforts (OpenCode's "variants") it takes, whether it is free, and whether it can
// use tools. See docs/plan-conversation-other-agents.md and docs/samples/opencode/models-verbose.txt.
package opencodemodels

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Entry is one model as OpenCode lists it.
type Entry struct {
	// Key is what -m takes: provider/id.
	Key      string
	Provider string
	Name     string
	// Free says it costs nothing per token: neither the input nor the output has a price.
	Free bool
	// Tools says the model can use tools, which an agent's work needs.
	Tools    bool
	Active   bool
	Variants []string
}

// header is the line before each model's JSON: provider/id.
var header = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/\S+$`)

// Parse reads OpenCode's verbose list: for each model its key on a line, then its description as JSON. A text with
// no model in it (an error OpenCode printed, nothing at all) is an error: the page would otherwise show an agent
// with no models to choose from.
func Parse(text string) ([]Entry, error) {
	lines := strings.Split(text, "\n")
	var entries []Entry
	for i := 0; i < len(lines); i++ {
		key := strings.TrimSpace(lines[i])
		if key == "" {
			continue
		}
		if !header.MatchString(key) {
			// Something else (an error, a notice) before or among the models.
			if len(entries) == 0 {
				return nil, fmt.Errorf("OpenCode's list of models has no model in it: %.200q", strings.TrimSpace(text))
			}
			continue
		}
		start := i + 1
		for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
			start++
		}
		end := start
		for end < len(lines) && !header.MatchString(strings.TrimSpace(lines[end])) {
			end++
		}
		var m struct {
			ID         string `json:"id"`
			ProviderID string `json:"providerID"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Cost       struct {
				Input  float64 `json:"input"`
				Output float64 `json:"output"`
			} `json:"cost"`
			Capabilities struct {
				Toolcall bool `json:"toolcall"`
			} `json:"capabilities"`
			Variants map[string]json.RawMessage `json:"variants"`
		}
		body := strings.Join(lines[start:end], "\n")
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			return nil, fmt.Errorf("OpenCode's description of %s isn't JSON: %w", key, err)
		}
		provider := m.ProviderID
		if provider == "" {
			provider, _, _ = strings.Cut(key, "/")
		}
		entry := Entry{
			Key: key, Provider: provider, Name: m.Name,
			Free:     m.Cost.Input == 0 && m.Cost.Output == 0,
			Tools:    m.Capabilities.Toolcall,
			Active:   m.Status == "" || m.Status == "active",
			Variants: variantsInOrder(body),
		}
		if entry.Name == "" {
			entry.Name = m.ID
		}
		entries = append(entries, entry)
		i = end - 1
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("OpenCode's list of models has no model in it: %.200q", strings.TrimSpace(text))
	}
	return entries, nil
}

// variantsInOrder is the names under "variants" in the order OpenCode printed them (from lowest effort to highest),
// which a map would lose.
func variantsInOrder(body string) []string {
	dec := json.NewDecoder(strings.NewReader(body))
	var names []string
	// Walk the tokens to the "variants" key at the top level, then read its keys.
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return names
		}
		switch v := tok.(type) {
		case json.Delim:
			if v == '{' || v == '[' {
				depth++
			} else {
				depth--
			}
		case string:
			if depth == 1 && v == "variants" {
				open, err := dec.Token()
				if err != nil || open != json.Delim('{') {
					return names
				}
				for dec.More() {
					key, err := dec.Token()
					if err != nil {
						return names
					}
					names = append(names, key.(string))
					var skip json.RawMessage
					if dec.Decode(&skip) != nil {
						return names
					}
				}
				return names
			} else if depth == 1 {
				// A key at the top level: skip its value, which isn't "variants".
				var skip json.RawMessage
				if dec.Decode(&skip) != nil {
					return names
				}
			}
		}
	}
}

// Catalogue is the models the page offers: the active ones that can use tools, in the order OpenCode listed them.
type Catalogue struct {
	entries []Entry
}

// Build is the catalogue of entries.
func Build(entries []Entry) Catalogue {
	var c Catalogue
	for _, e := range entries {
		if e.Tools && e.Active {
			c.entries = append(c.entries, e)
		}
	}
	return c
}

func (c Catalogue) find(key string) (Entry, bool) {
	for _, e := range c.entries {
		if e.Key == key {
			return e, true
		}
	}
	return Entry{}, false
}

// Keys are the models, as -m takes them.
func (c Catalogue) Keys() []string {
	keys := make([]string, 0, len(c.entries))
	for _, e := range c.entries {
		keys = append(keys, e.Key)
	}
	return keys
}

// Has says OpenCode lists the model, and it can be offered.
func (c Catalogue) Has(key string) bool { _, ok := c.find(key); return ok }

// Name is what OpenCode calls the model.
func (c Catalogue) Name(key string) string { e, _ := c.find(key); return e.Name }

// Free says the model costs nothing per token.
func (c Catalogue) Free(key string) bool { e, _ := c.find(key); return e.Free }

// Provider is who the model is from.
func (c Catalogue) Provider(key string) string { e, _ := c.find(key); return e.Provider }

// Efforts are the efforts the model takes: an empty list, not none, for one that takes none.
func (c Catalogue) Efforts(key string) []string {
	e, _ := c.find(key)
	return append([]string{}, e.Variants...)
}

// AllEfforts is every effort any model takes, lowest first as they were met.
func (c Catalogue) AllEfforts() []string {
	var all []string
	seen := map[string]bool{}
	for _, e := range c.entries {
		for _, v := range e.Variants {
			if !seen[v] {
				seen[v] = true
				all = append(all, v)
			}
		}
	}
	return all
}

// Providers are the providers the models are from, in the order they first appear.
func (c Catalogue) Providers() []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range c.entries {
		if !seen[e.Provider] {
			seen[e.Provider] = true
			out = append(out, e.Provider)
		}
	}
	return out
}

// efforts are what the page calls an effort where it isn't just its name in title case.
var effortNames = map[string]string{"xhigh": "Extra high"}

// EffortLabel is what the page calls an effort.
func EffortLabel(effort string) string {
	if name, ok := effortNames[effort]; ok {
		return name
	}
	if effort == "" {
		return effort
	}
	return strings.ToUpper(effort[:1]) + effort[1:]
}
