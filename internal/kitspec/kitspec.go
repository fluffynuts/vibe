// Package kitspec builds the generated sbx kit spec (a schemaVersion "2"
// mixin document) for a sandbox: it merges the base and profile config.yaml
// fragments, then generates the setup.install, setup.files and startup
// sections from the defaults and profile install-scripts / agent-files
// directories.
package kitspec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"vibe/internal/directive"
)

// AgentHome is where every profile's agent-files tree is rooted inside the
// sandbox. sbx always runs the coding agent as this fixed user regardless of
// which agent tool is selected.
const AgentHome = "/home/agent"

// Doc is a generic YAML document: config.yaml fragments may contain any
// fields sbx's kit schema supports, so we merge them structurally instead of
// modelling every field.
type Doc map[string]interface{}

// LoadDoc reads a config.yaml fragment. A missing file yields an empty Doc.
func LoadDoc(path string) (Doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Doc{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var d Doc
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if d == nil {
		d = Doc{}
	}
	return d, nil
}

// MergeDocs overlays override on base: maps merge recursively, slices are
// appended (base entries first), and any other value type is replaced by
// override's value.
func MergeDocs(base, override Doc) Doc {
	return mergeValue(base, override).(Doc)
}

func mergeValue(base, override interface{}) interface{} {
	switch ov := override.(type) {
	case Doc:
		bm, ok := base.(Doc)
		if !ok || bm == nil {
			bm = Doc{}
		}
		out := Doc{}
		for k, v := range bm {
			out[k] = v
		}
		for k, v := range ov {
			if existing, ok := out[k]; ok {
				out[k] = mergeValue(existing, v)
			} else {
				out[k] = v
			}
		}
		return out
	case map[string]interface{}:
		return mergeValue(base, Doc(ov))
	case []interface{}:
		bs, ok := base.([]interface{})
		if !ok {
			bs = nil
		}
		out := make([]interface{}, 0, len(bs)+len(ov))
		out = append(out, bs...)
		out = append(out, ov...)
		return out
	default:
		return override
	}
}

// leadingNumber extracts a script/file's ordering prefix, e.g. "01" from
// "01-refresh-apt-indices". Files with no leading number sort last.
var leadingNumberRE = regexp.MustCompile(`^\d+`)

func leadingNumber(name string) int {
	m := leadingNumberRE.FindString(name)
	if m == "" {
		return int(^uint(0) >> 1) // max int: no-prefix files sort last
	}
	n, _ := strconv.Atoi(m)
	return n
}

// SortedDirEntries returns dir's non-directory, non-sidecar entries in the
// order install scripts and agent files are applied in: by leading number,
// then by name. A missing directory yields no entries rather than an
// error, since not every profile or feature has one. Exported so the
// library package can plan a combined, renumbered install-scripts sequence
// using the exact same ordering rules InstallSteps applies.
func SortedDirEntries(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	files := make([]os.DirEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && !IsSidecar(e.Name()) {
			files = append(files, e)
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		ni, nj := leadingNumber(files[i].Name()), leadingNumber(files[j].Name())
		if ni != nj {
			return ni < nj
		}
		return files[i].Name() < files[j].Name()
	})
	return files, nil
}

// IsSidecar reports whether name is a ".vibe" directive sidecar file, which
// carries directives for a file that can't hold a "# vibe:" comment inline
// (e.g. JSON) and is never itself deployed.
func IsSidecar(name string) bool {
	return strings.HasSuffix(name, ".vibe")
}

// directivesFor reads the directives that apply to a file: those embedded in
// its own content, overlaid with any found in a "<name>.vibe" sidecar.
func directivesFor(path string, content []byte) (directive.Set, error) {
	d := directive.Parse(content)
	sidecar, err := os.ReadFile(path + ".vibe")
	if err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return nil, fmt.Errorf("reading %s.vibe: %w", path, err)
	}
	return d.Merge(directive.ParsePlain(sidecar)), nil
}

// InstallSteps generates the setup.install sequence: each dir's scripts run
// in their own numeric order, and dirs are concatenated in the given order
// (the defaults directory first, then the profile directory).
func InstallSteps(dirs ...string) ([]interface{}, error) {
	var steps []interface{}
	for _, dir := range dirs {
		entries, err := SortedDirEntries(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("reading install script %s: %w", path, err)
			}
			d, err := directivesFor(path, content)
			if err != nil {
				return nil, err
			}
			steps = append(steps, Doc{
				"command":     string(directive.Strip(content)),
				"user":        d.String("user", "0"),
				"description": d.Description(e.Name()),
			})
		}
	}
	return steps, nil
}

type fileSpec struct {
	relPath  string
	sourceAt string
	content  []byte
}

// FileEntries generates the setup.files sequence from one or more agent-files
// trees. Trees are applied in the given order, and a file at the same
// relative path in a later tree overrides an earlier one (so a profile can
// override a default file) — the destination is $AgentHome/<relative path>.
func FileEntries(dirs ...string) ([]interface{}, error) {
	byPath := map[string]fileSpec{}
	var order []string

	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", dir, err)
		}
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if IsSidecar(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading agent file %s: %w", path, err)
			}
			if _, seen := byPath[rel]; !seen {
				order = append(order, rel)
			}
			byPath[rel] = fileSpec{relPath: rel, sourceAt: path, content: content}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Strings(order)

	entries := make([]interface{}, 0, len(order))
	for _, rel := range order {
		spec := byPath[rel]
		d, err := directivesFor(spec.sourceAt, spec.content)
		if err != nil {
			return nil, err
		}
		entries = append(entries, Doc{
			"path":          AgentHome + "/" + rel,
			"mode":          fileMode(rel, spec.content, d),
			"onlyIfMissing": d.Bool("onlyifmissing", false),
			"description":   d.Description(filepath.Base(rel)),
			"content":       string(directive.Strip(spec.content)),
		})
	}
	return entries, nil
}

func fileMode(relPath string, content []byte, d directive.Set) string {
	if v, ok := d["mode"]; ok && v != "" {
		return normalizeMode(v)
	}
	if strings.HasPrefix(string(content), "#!") {
		return "0755"
	}
	if strings.Contains(relPath, "bin/") {
		return "0755"
	}
	return "0644"
}

func normalizeMode(v string) string {
	v = strings.TrimSpace(v)
	if len(v) == 3 {
		return "0" + v
	}
	return v
}

// OnStartRelPath is the fixed location a profile or the defaults may place an
// on-start script at, to have it run at sandbox startup.
const OnStartRelPath = ".local/bin/on-start"

// StartupSteps returns the setup.startup section, adding the on-start
// script (if setup.files ended up including one at OnStartRelPath) as a
// backgrounded root step, per spec.md. sbx's kit schema only recognizes
// startup nested under setup — there is no top-level startup field.
func StartupSteps(fileEntries []interface{}) []interface{} {
	target := AgentHome + "/" + OnStartRelPath
	for _, e := range fileEntries {
		entry, ok := e.(Doc)
		if !ok {
			continue
		}
		if entry["path"] == target {
			return []interface{}{Doc{
				"command":     []interface{}{target},
				"user":        "0",
				"background":  true,
				"description": "running startup script",
			}}
		}
	}
	return nil
}

// urlHostRE matches an http(s) URL's host, requiring at least two
// dot-separated labels so a bare hostname like "localhost" (used for
// services the sandbox reaches on its own loopback, never an external host
// needing an egress allow rule) never matches.
var urlHostRE = regexp.MustCompile(`https?://([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+)`)

// extractDomains returns every http(s) URL host found in text, excluding
// bare IPv4 addresses (which an allow rule wouldn't sensibly target either).
func extractDomains(text string) []string {
	var domains []string
	for _, m := range urlHostRE.FindAllStringSubmatch(text, -1) {
		host := m[1]
		if !isIPv4(host) {
			domains = append(domains, host)
		}
	}
	return domains
}

func isIPv4(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// discoverDomains scans every install step's command and every deployed
// file's content for http(s) URLs, so a feature's own curl/apt-repo
// targets — like the Elastic apt key fetch in
// library/elasticsearch/install-scripts/01-enable-elasticsearch-repo —
// don't also have to be hand-copied into permissions.network.allow, a
// separate list that's easy for it to drift out of sync with. It returns
// the discovered hosts in a stable, deduplicated order.
func discoverDomains(installSteps, fileEntries []interface{}) []string {
	seen := map[string]bool{}
	var domains []string
	add := func(text string) {
		for _, d := range extractDomains(text) {
			if !seen[d] {
				seen[d] = true
				domains = append(domains, d)
			}
		}
	}
	for _, s := range installSteps {
		if step, ok := s.(Doc); ok {
			if cmd, ok := step["command"].(string); ok {
				add(cmd)
			}
		}
	}
	for _, f := range fileEntries {
		if entry, ok := f.(Doc); ok {
			if content, ok := entry["content"].(string); ok {
				add(content)
			}
		}
	}
	sort.Strings(domains)
	return domains
}

// mergeDiscoveredDomains adds any of domains not already present (by exact
// string match — a wildcard entry like "*.ubuntu.com" already covering one
// isn't recognized, so both may end up listed; harmless, just slightly
// redundant) to merged's permissions.network.allow, creating that path if
// config.yaml didn't set it at all.
func mergeDiscoveredDomains(merged Doc, domains []string) {
	if len(domains) == 0 {
		return
	}
	permissions, _ := merged["permissions"].(Doc)
	if permissions == nil {
		permissions = Doc{}
	}
	network, _ := permissions["network"].(Doc)
	if network == nil {
		network = Doc{}
	}
	existing, _ := network["allow"].([]interface{})
	have := map[string]bool{}
	for _, e := range existing {
		if s, ok := e.(string); ok {
			have[s] = true
		}
	}
	allow := append([]interface{}{}, existing...)
	for _, d := range domains {
		if !have[d] {
			allow = append(allow, d)
			have[d] = true
		}
	}
	network["allow"] = allow
	permissions["network"] = network
	merged["permissions"] = permissions
}

// Build assembles the final kit spec document for a sandbox.
//
//   - baseConfig, profileConfig: config.yaml fragments (profile overrides base)
//   - defaultsDir, profileDir: the single directories the defaults and the
//     profile are each read from, containing install-scripts/ and
//     agent-files/ subdirectories (the defaults applied first)
//   - agentInstructionsProfile: contents of profile/agent-instructions.md,
//     appended to whatever agentInstructions.content the merged config set
func Build(baseConfig, profileConfig Doc, defaultsDir, profileDir string, agentInstructionsProfile string) (Doc, error) {
	merged := MergeDocs(baseConfig, profileConfig)

	installSteps, err := InstallSteps(
		filepath.Join(defaultsDir, "install-scripts"),
		filepath.Join(profileDir, "install-scripts"),
	)
	if err != nil {
		return nil, err
	}

	fileEntries, err := FileEntries(
		filepath.Join(defaultsDir, "agent-files"),
		filepath.Join(profileDir, "agent-files"),
	)
	if err != nil {
		return nil, err
	}

	setup := Doc{
		"install": installSteps,
		"files":   fileEntries,
	}
	if startup := StartupSteps(fileEntries); startup != nil {
		setup["startup"] = startup
	}
	merged["setup"] = setup

	mergeDiscoveredDomains(merged, discoverDomains(installSteps, fileEntries))

	if agentInstructionsProfile != "" {
		agentInstructions, _ := merged["agentInstructions"].(Doc)
		if agentInstructions == nil {
			agentInstructions = Doc{}
		}
		existing, _ := agentInstructions["content"].(string)
		if existing != "" {
			agentInstructions["content"] = existing + "\n" + agentInstructionsProfile
		} else {
			agentInstructions["content"] = agentInstructionsProfile
		}
		merged["agentInstructions"] = agentInstructions
	}

	return merged, nil
}

// Marshal renders the document as YAML.
func Marshal(d Doc) ([]byte, error) {
	return yaml.Marshal(map[string]interface{}(d))
}
