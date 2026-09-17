// Package directive parses "# vibe: key: value" annotation comments that
// profile authors embed in install-scripts and agent-files to override the
// generation defaults (description, mode, onlyIfMissing, user).
package directive

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// Set holds the directives found in a single file, keyed by lowercase name.
type Set map[string]string

var linePrefixes = []string{"# vibe:", "#vibe:", "// vibe:", "//vibe:"}

// Parse scans content for "# vibe: key: value" lines anywhere in the file
// and returns them as a Set. Keys are lowercased; values are trimmed.
func Parse(content []byte) Set {
	set := Set{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		for _, prefix := range linePrefixes {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			rest := strings.TrimSpace(line[len(prefix):])
			key, value, ok := strings.Cut(rest, ":")
			if !ok {
				continue
			}
			key = strings.ToLower(strings.TrimSpace(key))
			value = strings.TrimSpace(value)
			if key != "" {
				set[key] = value
			}
			break
		}
	}
	return set
}

// Strip removes every "# vibe: ..." line from content, so generator metadata
// never leaks into a deployed script or config file.
func Strip(content []byte) []byte {
	var out bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(content))
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		isDirective := false
		for _, prefix := range linePrefixes {
			if strings.HasPrefix(trimmed, prefix) {
				isDirective = true
				break
			}
		}
		if isDirective {
			continue
		}
		if !first {
			out.WriteByte('\n')
		}
		out.WriteString(line)
		first = false
	}
	if bytes.HasSuffix(content, []byte("\n")) && out.Len() > 0 {
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// ParsePlain parses a sidecar directive file (used for content types, such as
// JSON, that can't carry a "# vibe:" comment inline): every non-blank line
// is "key: value", with no prefix required.
func ParsePlain(content []byte) Set {
	set := Set{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key != "" {
			set[key] = value
		}
	}
	return set
}

// Merge returns a new Set with other's entries overlaid on s (other wins on
// conflicts).
func (s Set) Merge(other Set) Set {
	out := Set{}
	for k, v := range s {
		out[k] = v
	}
	for k, v := range other {
		out[k] = v
	}
	return out
}

// Description returns the "description" directive, or fallback when absent.
func (s Set) Description(fallback string) string {
	if v, ok := s["description"]; ok && v != "" {
		return v
	}
	return fallback
}

// Bool returns the named directive parsed as a bool, or fallback when absent
// or unparsable.
func (s Set) Bool(key string, fallback bool) bool {
	v, ok := s[key]
	if !ok {
		return fallback
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return b
}

// String returns the named directive, or fallback when absent.
func (s Set) String(key, fallback string) string {
	if v, ok := s[key]; ok && v != "" {
		return v
	}
	return fallback
}
