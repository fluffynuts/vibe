// Package library discovers vibe's feature library — a directory of named
// folders (mysql, rabbitmq, dotnet, ...), each providing one selectable
// feature — and composes a chosen, ordered subset of them into a profile:
// its install-scripts and agent-files, plus the config.yaml, settings.yaml
// and agent-instructions.md fragments a feature may carry.
package library

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"vibe/internal/directive"
	"vibe/internal/fscopy"
	"vibe/internal/kitspec"
)

// Feature is one selectable folder under the library.
type Feature struct {
	Name        string // the folder name, e.g. "mysql"
	Description string // "" when no "# vibe: description:" was found anywhere in it
}

// Label is what to show the user for this feature: "name: description", or
// just the name when the feature carries no description anywhere, so the
// name always identifies where the description (if any) came from.
func (f Feature) Label() string {
	if f.Description == "" {
		return f.Name
	}
	return f.Name + ": " + f.Description
}

// DirFor resolves a feature name to the single directory it should be read
// from — the overlay's copy when the user has one, else the bundle's (see
// layout.Layout.FeatureDir) — so overriding one feature never hides any
// other.
type DirFor func(feature string) string

// List describes every named feature, in the given order — normally
// layout.Layout.Features(), the sorted union of feature names across both
// layers.
func List(names []string, dirFor DirFor) []Feature {
	features := make([]Feature, len(names))
	for i, name := range names {
		features[i] = Feature{Name: name, Description: describe(dirFor(name))}
	}
	return features
}

// describe derives a feature's description from what it already documents
// itself with, so the library never needs a separate manifest to keep in
// sync: the first "# vibe: description:" found in its install-scripts (in
// their run order), else the first one found among its agent-files (by
// path), else "".
func describe(featureDir string) string {
	if d := firstScriptDescription(filepath.Join(featureDir, "install-scripts")); d != "" {
		return d
	}
	return firstAgentFileDescription(filepath.Join(featureDir, "agent-files"))
}

func firstScriptDescription(dir string) string {
	entries, err := kitspec.SortedDirEntries(dir)
	if err != nil || len(entries) == 0 {
		return ""
	}
	content, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		return ""
	}
	return directive.Parse(content).String("description", "")
}

func firstAgentFileDescription(dir string) string {
	var rels []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || kitspec.IsSidecar(d.Name()) {
			return nil
		}
		if rel, relErr := filepath.Rel(dir, path); relErr == nil {
			rels = append(rels, rel)
		}
		return nil
	})
	if len(rels) == 0 {
		return ""
	}
	sort.Strings(rels)
	content, err := os.ReadFile(filepath.Join(dir, rels[0]))
	if err != nil {
		return ""
	}
	return directive.Parse(content).String("description", "")
}

// Validate checks that every name in wanted is a feature the library
// actually has, failing on the first one that isn't. A name that is only a
// typo away from a real feature is reported with that feature suggested,
// since the usual source of this is a settings.yaml defaultFeatures entry
// written from memory.
func Validate(wanted, available []string) error {
	have := make(map[string]bool, len(available))
	for _, name := range available {
		have[name] = true
	}
	for _, name := range wanted {
		if have[name] {
			continue
		}
		if s := suggest(name, available); s != "" {
			return fmt.Errorf("no library feature '%s' — did you mean '%s'?", name, s)
		}
		if len(available) == 0 {
			return fmt.Errorf("no library feature '%s' — the library is empty", name)
		}
		return fmt.Errorf("no library feature '%s' — the library has: %s", name, strings.Join(available, ", "))
	}
	return nil
}

// suggest returns the closest of available to name, or "" when nothing is
// close enough to be worth guessing at. The allowance grows with the length
// of the name typed, so a long name may be a couple of letters out while a
// short one has to be nearly exact.
func suggest(name string, available []string) string {
	limit := len([]rune(name))/3 + 1
	best, bestDistance := "", 0
	for _, candidate := range available {
		d := distance(strings.ToLower(name), strings.ToLower(candidate))
		if d > limit {
			continue
		}
		if best == "" || d < bestDistance {
			best, bestDistance = candidate, d
		}
	}
	return best
}

// distance is the Levenshtein edit distance between a and b, counting the
// single-character insertions, deletions and substitutions a typo is made
// of. It keeps one row of the matrix rather than all of it — feature names
// are short, but there is no reason to allocate more than this needs.
func distance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

// leadingDigitsRE matches a script's ordering prefix, mirroring kitspec's
// own leading-number rule.
var leadingDigitsRE = regexp.MustCompile(`^\d+`)

// afterLeadingNumber returns what to keep of name once its ordering prefix
// is replaced: everything after the leading digits (e.g. "-install-mysql"
// from "01-install-mysql"), or "-"+name when it has no leading number.
func afterLeadingNumber(name string) string {
	m := leadingDigitsRE.FindString(name)
	if m == "" {
		return "-" + name
	}
	return name[len(m):]
}

type plannedScript struct {
	src    string
	number int
	rest   string
}

// Compose copies the given features, in order, into profileDir:
//
//   - Each feature's install-scripts are renumbered to run in the given
//     feature order — a feature's own scripts keep their own relative
//     order, only whole features are reordered — starting the next
//     feature's numbers right after the previous feature's highest.
//   - Each feature's agent-files are copied over profileDir's agent-files;
//     a later feature's file overrides an earlier one's at the same path.
//   - When any feature places files directly under agent-files/.local/bin,
//     an on-start script is generated (in the same vein as
//     library/on-start.example) to run all of them, across every feature,
//     in the given feature order.
//   - Each feature's config.yaml and settings.yaml fragments are merged
//     into the profile's own (lists append in feature order, and anything
//     the profile itself already sets wins), and its agent-instructions.md
//     is appended to the profile's. That is what lets a feature carry the
//     ports, permissions and instructions its tooling needs instead of
//     leaving them to be hand-copied into every profile that picks it.
func Compose(features []string, dirFor DirFor, profileDir string) error {
	var scripts []plannedScript
	var startupScripts []string
	config := kitspec.Doc{}
	settings := kitspec.Doc{}
	var instructions []string
	counter := 0

	for _, feature := range features {
		featureDir := dirFor(feature)

		for _, frag := range []struct {
			name string
			into *kitspec.Doc
		}{
			{"config.yaml", &config},
			{"settings.yaml", &settings},
		} {
			doc, err := kitspec.LoadDoc(filepath.Join(featureDir, frag.name))
			if err != nil {
				return err
			}
			*frag.into = kitspec.MergeDocs(*frag.into, doc)
		}

		text, err := os.ReadFile(filepath.Join(featureDir, "agent-instructions.md"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if s := strings.TrimSpace(string(text)); s != "" {
			instructions = append(instructions, s)
		}

		entries, err := kitspec.SortedDirEntries(filepath.Join(featureDir, "install-scripts"))
		if err != nil {
			return err
		}
		for _, e := range entries {
			counter++
			scripts = append(scripts, plannedScript{
				src:    filepath.Join(featureDir, "install-scripts", e.Name()),
				number: counter,
				rest:   afterLeadingNumber(e.Name()),
			})
		}

		agentDir := filepath.Join(featureDir, "agent-files")
		if info, statErr := os.Stat(agentDir); statErr == nil && info.IsDir() {
			if err := fscopy.Tree(agentDir, filepath.Join(profileDir, "agent-files")); err != nil {
				return err
			}
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}

		binEntries, err := os.ReadDir(filepath.Join(agentDir, ".local", "bin"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		var names []string
		for _, be := range binEntries {
			if !be.IsDir() {
				names = append(names, be.Name())
			}
		}
		sort.Strings(names)
		startupScripts = append(startupScripts, names...)
	}

	if len(scripts) > 0 {
		if err := writeInstallScripts(profileDir, scripts, counter); err != nil {
			return err
		}
	}
	if len(startupScripts) > 0 {
		if err := writeOnStart(profileDir, startupScripts); err != nil {
			return err
		}
	}
	for _, frag := range []struct {
		name string
		doc  kitspec.Doc
	}{
		{"config.yaml", config},
		{"settings.yaml", settings},
	} {
		if len(frag.doc) == 0 {
			continue
		}
		if err := mergeUnderProfile(filepath.Join(profileDir, frag.name), frag.doc); err != nil {
			return err
		}
	}
	if len(instructions) > 0 {
		if err := appendInstructions(filepath.Join(profileDir, "agent-instructions.md"), instructions); err != nil {
			return err
		}
	}
	return nil
}

// commentHeaderRE matches the leading comment block a generated profile file
// opens with, so merging a feature's fragment in doesn't throw away the
// explanation of what the file is.
var commentHeaderRE = regexp.MustCompile(`\A(?:[ \t]*(?:#[^\n]*)?\n)*`)

// mergeUnderProfile merges fragment into the YAML document at path, with
// whatever the profile already set winning over it — the profile's own
// name and description are not something a feature gets to replace. The
// file's leading comment block is kept; comments further down (and inside
// the fragment) are lost to the YAML round-trip, which is why the library's
// copy stays the readable one.
func mergeUnderProfile(path string, fragment kitspec.Doc) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	header := commentHeaderRE.FindString(string(data))
	own, err := kitspec.LoadDoc(path)
	if err != nil {
		return err
	}
	merged, err := kitspec.Marshal(kitspec.MergeDocs(fragment, own))
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(header), merged...), 0o644)
}

// appendInstructions adds each feature's agent-instructions.md to the
// profile's, after anything already there.
func appendInstructions(path string, instructions []string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	blocks := instructions
	if s := strings.TrimSpace(string(existing)); s != "" {
		blocks = append([]string{s}, instructions...)
	}
	return os.WriteFile(path, []byte(strings.Join(blocks, "\n\n")+"\n"), 0o644)
}

// writeInstallScripts writes the planned scripts into profileDir's
// install-scripts, using enough digits for the highest number assigned (2
// at minimum, matching the library's own convention) so filesystem
// ordering keeps matching numeric ordering how ever many scripts a guided
// profile ends up with.
func writeInstallScripts(profileDir string, scripts []plannedScript, highest int) error {
	destDir := filepath.Join(profileDir, "install-scripts")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	width := len(strconv.Itoa(highest))
	if width < 2 {
		width = 2
	}
	for _, s := range scripts {
		name := fmt.Sprintf("%0*d%s", width, s.number, s.rest)
		dest := filepath.Join(destDir, name)
		if err := fscopy.File(s.src, dest); err != nil {
			return err
		}
		sidecar := s.src + ".vibe"
		data, err := os.ReadFile(sidecar)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("reading %s: %w", sidecar, err)
		}
		if err := os.WriteFile(dest+".vibe", data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// writeOnStart generates a startup script — in the same vein as
// library/on-start.example — that runs every given agent-files/.local/bin
// script, in order, and reports which ones failed without stopping the
// rest. kitspec's StartupSteps picks this file up automatically once it is
// in place at agent-files/.local/bin/on-start.
func writeOnStart(profileDir string, scripts []string) error {
	dest := filepath.Join(profileDir, "agent-files", ".local", "bin", "on-start")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#!/bin/sh\n")
	fmt.Fprintf(&b, "# vibe: description: Run the startup scripts for: %s\n", strings.Join(scripts, ", "))
	b.WriteString("exec >>/tmp/start-services.log 2>&1\n")
	b.WriteString("echo \"=== start-services at $(date -Is) ===\"\n")
	fmt.Fprintf(&b, "for s in %s; do\n", strings.Join(scripts, " "))
	b.WriteString("  echo \"--- $s\"\n")
	b.WriteString("  /home/agent/.local/bin/$s || echo \"WARN: $s exited $?\"\n")
	b.WriteString("done\n")
	b.WriteString("echo \"=== done at $(date -Is) ===\"\n")
	return os.WriteFile(dest, []byte(b.String()), 0o644)
}
