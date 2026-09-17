// Package library discovers vibe's feature library — a directory of named
// folders (mysql, rabbitmq, dotnet, ...), each providing one selectable
// feature — and composes a chosen, ordered subset of them into a profile's
// install-scripts and agent-files.
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

// List returns every feature in dir (a library root), sorted by name. A
// missing directory yields no features rather than an error, since a
// bundle need not ship a library at all.
func List(dir string) ([]Feature, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var features []Feature
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		featureDir := filepath.Join(dir, e.Name())
		features = append(features, Feature{
			Name:        e.Name(),
			Description: describe(featureDir),
		})
	}
	sort.Slice(features, func(i, j int) bool { return features[i].Name < features[j].Name })
	return features, nil
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
func Compose(libraryDir string, features []string, profileDir string) error {
	var scripts []plannedScript
	var startupScripts []string
	counter := 0

	for _, feature := range features {
		featureDir := filepath.Join(libraryDir, feature)

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
	return nil
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
