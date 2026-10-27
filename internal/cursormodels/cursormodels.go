// Package cursormodels reads the list of models Cursor's agent prints
// (cursor-agent --list-models) as families with efforts, as the page's two
// drop-downs show them. Cursor lists every combination as an id of its own
// (gpt-5.6-sol-high-fast): the effort and whether it is the fast variant are
// part of the id, so they are taken out of it, and put back to find the id a
// choice means. See docs/plan-cursor.md and docs/samples/cursor/list-models.txt.
package cursormodels

import (
	"fmt"
	"regexp"
	"strings"
)

// Entry is one line of the list.
type Entry struct {
	ID, Name string
	// Current says Cursor marked it as the one in use now.
	Current bool
}

var (
	entryLine = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]*) - (.+)$`)
	ansi      = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")
	marker    = regexp.MustCompile(`\s*\(((?:current|default)(?:, *(?:current|default))*)\)$`)
)

// Parse reads the list. A list with no model in it is an error: it is a
// changed cursor-agent, or one that said something else (not signed in), and
// that is said, not read as having no models.
func Parse(text string) ([]Entry, error) {
	var entries []Entry
	for _, line := range strings.Split(ansi.ReplaceAllString(text, ""), "\n") {
		m := entryLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		// Some names end in zero-width characters.
		name := strings.TrimSpace(strings.NewReplacer("\u200b", "", "\ufeff", "").Replace(m[2]))
		current := false
		if mark := marker.FindStringSubmatch(name); mark != nil {
			current = strings.Contains(mark[1], "current")
			name = strings.TrimSpace(strings.TrimSuffix(name, mark[0]))
		}
		entries = append(entries, Entry{ID: m[1], Name: name, Current: current})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("cursor-agent's list has no model in it: %.200q", strings.TrimSpace(text))
	}
	return entries, nil
}

// Efforts, in the order they are offered.
var efforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// effortWords are what Cursor's names call them.
var effortWords = map[string]string{
	"none": "None", "minimal": "Minimal", "low": "Low", "medium": "Medium", "high": "High", "xhigh": "Extra High", "max": "Max",
}

func isEffort(token string) bool {
	_, ok := effortWords[token]
	return ok
}

// split takes an id apart: its family, its effort ("" when it has none), and
// whether it is the fast variant. The effort is the last word of the id, before
// "fast" and before "thinking" when that comes last ("-high-thinking"; in
// "-thinking-high" it is part of the family), with "-extra-high" for xhigh;
// anything else stays in the family.
func split(id string) (family, effort string, fast bool) {
	tokens := strings.Split(id, "-")
	if n := len(tokens); n > 1 && tokens[n-1] == "fast" {
		tokens, fast = tokens[:n-1], true
	}
	thinking := false
	if n := len(tokens); n > 1 && tokens[n-1] == "thinking" {
		tokens, thinking = tokens[:n-1], true
	}
	if n := len(tokens); n > 2 && tokens[n-2] == "extra" && tokens[n-1] == "high" {
		// What some ids call xhigh.
		tokens, effort = tokens[:n-2], "xhigh"
	} else if n > 1 && isEffort(tokens[n-1]) {
		tokens, effort = tokens[:n-1], tokens[n-1]
	}
	if thinking {
		tokens = append(tokens, "thinking")
	}
	return strings.Join(tokens, "-"), effort, fast
}

// Variant is one id of a family.
type Variant struct {
	ID     string
	Effort string
	Fast   bool
}

// Value is the variant as the page keeps it: the effort and "fast" run
// together, as an effort is kept as lower-case letters ("highfast", "fast"),
// and "" for the family's plain id.
func (v Variant) Value() string {
	if v.Fast {
		return v.Effort + "fast"
	}
	return v.Effort
}

// Family is a model with its efforts: what the list's ids are after the effort
// and "fast" are taken out.
type Family struct {
	Key  string
	Name string
	// Variants are in the order they are offered: by effort, plain before
	// fast.
	Variants []Variant
}

// Catalogue is the list, as families.
type Catalogue struct {
	Families []Family
	// Current is the name of the family Cursor says is in use now, "" if it
	// didn't say.
	Current string
}

// Build makes families of entries, in the order each first appears.
func Build(entries []Entry) Catalogue {
	var c Catalogue
	index := map[string]int{}
	for _, e := range entries {
		key, effort, fast := split(e.ID)
		i, seen := index[key]
		if !seen {
			index[key] = len(c.Families)
			i = len(c.Families)
			c.Families = append(c.Families, Family{Key: key})
		}
		f := &c.Families[i]
		f.Variants = append(f.Variants, Variant{ID: e.ID, Effort: effort, Fast: fast})
		if f.Name == "" || (effort == "" && !fast) {
			f.Name = familyName(e.Name, effort, fast)
		}
		if e.Current {
			c.Current = f.Name
		}
	}
	for i := range c.Families {
		f := &c.Families[i]
		rank := func(v Variant) int {
			for r, e := range append([]string{""}, efforts...) {
				if e == v.Effort {
					return r
				}
			}
			return len(efforts) + 1
		}
		sortVariants(f.Variants, rank)
	}
	return c
}

func sortVariants(vs []Variant, rank func(Variant) int) {
	for i := 1; i < len(vs); i++ {
		for j := i; j > 0; j-- {
			a, b := vs[j-1], vs[j]
			if rank(a) < rank(b) || (rank(a) == rank(b) && !a.Fast && b.Fast) {
				break
			}
			vs[j-1], vs[j] = vs[j], vs[j-1]
		}
	}
}

// familyName is name without the words that say its effort and "Fast": what
// the family is called. A name that doesn't have them is left as it is.
func familyName(name, effort string, fast bool) string {
	drop := func(word string) {
		re := regexp.MustCompile(`(?i)(^|\s)` + regexp.QuoteMeta(word) + `(\s|$)`)
		name = re.ReplaceAllString(name, " ")
	}
	if fast {
		drop("Fast")
	}
	if effort != "" {
		drop(effortWords[effort])
	}
	return strings.Join(strings.Fields(name), " ")
}

func (c Catalogue) family(key string) (Family, bool) {
	for _, f := range c.Families {
		if f.Key == key {
			return f, true
		}
	}
	return Family{}, false
}

// Efforts are the values the family offers besides its default, in order.
func (c Catalogue) Efforts(family string) []string {
	f, _ := c.family(family)
	var out []string
	for _, v := range f.Variants {
		if v.Value() != "" {
			out = append(out, v.Value())
		}
	}
	return out
}

// defaultEffort is the effort a family with no plain id is run on when none is
// chosen: a middling one, the first of these it has.
var defaultEfforts = []string{"high", "medium"}

// ID is the id of family at the effort value ("" is its default), or false when
// Cursor has no such model. A family with no plain id has no default of
// Cursor's: it is run on high effort, or medium, or else its first.
func (c Catalogue) ID(family, value string) (string, bool) {
	f, ok := c.family(family)
	if !ok {
		return "", false
	}
	for _, v := range f.Variants {
		if v.Value() == value {
			return v.ID, true
		}
	}
	if value != "" {
		return "", false
	}
	for _, want := range append(defaultEfforts, "") {
		for _, v := range f.Variants {
			if !v.Fast && v.Effort == want {
				return v.ID, true
			}
		}
	}
	return f.Variants[0].ID, true
}

// Split is the family and effort value an id is, whether or not it is in the
// list.
func Split(id string) (family, value string) {
	family, effort, fast := split(id)
	return family, Variant{Effort: effort, Fast: fast}.Value()
}

// Has says id is one of the list's.
func (c Catalogue) Has(id string) bool {
	for _, f := range c.Families {
		for _, v := range f.Variants {
			if v.ID == id {
				return true
			}
		}
	}
	return false
}

// EffortLabel is how an effort value is shown: "High effort", "High effort ·
// Fast", "Fast".
func EffortLabel(value string) string {
	fast := strings.HasSuffix(value, "fast")
	effort := strings.TrimSuffix(value, "fast")
	var parts []string
	if effort != "" {
		word := effortWords[effort]
		if word == "" {
			word = effort
		}
		if effort == "none" {
			word = "No"
		}
		parts = append(parts, word+" effort")
	}
	if fast {
		parts = append(parts, "Fast")
	}
	return strings.Join(parts, " · ")
}

// AllEfforts are the effort values any family offers, in the order they're
// offered.
func (c Catalogue) AllEfforts() []string {
	seen := map[string]bool{}
	for _, f := range c.Families {
		for _, v := range f.Variants {
			if v.Value() != "" {
				seen[v.Value()] = true
			}
		}
	}
	var out []string
	for _, e := range efforts {
		for _, value := range []string{e, e + "fast"} {
			if seen[value] {
				out = append(out, value)
				delete(seen, value)
			}
		}
	}
	if seen["fast"] {
		out = append([]string{"fast"}, out...)
	}
	return out
}
