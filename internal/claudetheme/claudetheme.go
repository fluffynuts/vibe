// Package claudetheme handles which Claude Code colour theme a sandbox's
// agent starts with: the themes a kit ships, the choice recorded for a
// profile, reading the choice back out of a sandbox, and setting it in the
// kit so the agent's first launch already uses it.
//
// Claude Code keeps the theme in ~/.claude/settings.json as "theme": either
// a built-in theme's name, or "custom:<name>" for a theme file in
// ~/.claude/themes. No theme at all is Claude's own default.
package claudetheme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"vibe/internal/kitspec"
	"vibe/internal/sbxrun"
)

// Default is the choice that leaves the theme unset, so Claude uses its own.
const Default = "default"

// ProfileFile is where, in a profile's directory, the theme last chosen for
// its sandbox is kept: one line, "default" or a theme name.
const ProfileFile = "claude-theme"

const (
	settingsPath = kitspec.AgentHome + "/.claude/settings.json"
	// legacyConfigPath is where Claude Code kept the theme before it moved
	// to settings.json; still read, never written.
	legacyConfigPath = kitspec.AgentHome + "/.claude.json"
	customPrefix     = "custom:"
)

// builtIn are Claude Code's own themes. A sandbox found using one keeps it,
// though the picker only offers the default and the kit's own themes.
var builtIn = map[string]bool{
	"dark": true, "light": true, "dark-daltonized": true, "light-daltonized": true,
	"dark-ansi": true, "light-ansi": true,
}

// validName is what a theme's name may look like — it ends up in a shell
// script and a JSON file, so nothing that needs quoting.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Available returns, sorted, the names of the custom themes in a defaults
// directory: every agent-files/.claude/themes/<name>.json in it. The
// defaults are the source of truth for which themes are offered — pass
// Layout.DefaultsDir(), which is ~/.vibe/defaults whenever that exists —
// and, being deployed into every sandbox, each one offered is there to use.
// A theme a profile adds is deployed too, and /theme can switch to it, but
// it isn't offered when creating a sandbox.
func Available(defaultsDir string) []string {
	entries, err := os.ReadDir(filepath.Join(defaultsDir, "agent-files", ".claude", "themes"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" || !validName.MatchString(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Choices is the picker's list: the default first, then the known themes.
func Choices(known []string) []string {
	return append([]string{Default}, known...)
}

// Resolve checks a choice — from a sandbox or a profile — against what the
// kit can honour. A custom theme the kit no longer ships falls back to the
// default, and ok says so.
func Resolve(choice string, known []string) (resolved string, ok bool) {
	switch {
	case choice == "" || choice == Default:
		return Default, true
	case builtIn[choice]:
		return choice, true
	}
	for _, k := range known {
		if k == choice {
			return choice, true
		}
	}
	return Default, false
}

// settingValue is the settings.json "theme" value for a choice; empty for
// the default, which is left unset.
func settingValue(choice string) string {
	switch {
	case choice == "" || choice == Default:
		return ""
	case builtIn[choice]:
		return choice
	}
	return customPrefix + choice
}

// choiceFromSetting is the choice a settings.json "theme" value stands for.
func choiceFromSetting(value string) string {
	if value == "" {
		return Default
	}
	return strings.TrimPrefix(value, customPrefix)
}

// FromSandbox reads the theme a running sandbox's agent is set to, however
// it was set — at creation, or by /theme since. ok is false when the
// sandbox can't be asked, or its settings can't be read.
func FromSandbox(name string) (choice string, ok bool) {
	if value, found, ok := themeIn(name, settingsPath); !ok {
		return "", false
	} else if found {
		return choiceFromSetting(value), true
	}
	if value, found, _ := themeIn(name, legacyConfigPath); found {
		return choiceFromSetting(value), true
	}
	return Default, true
}

// themeIn reads the "theme" key of a JSON file in the sandbox. A file that
// isn't there reads as having no theme; one that can't be read or parsed
// is not ok.
func themeIn(name, file string) (value string, found, ok bool) {
	if !sbxrun.ExecSilent(name, "test", "-e", file) {
		return "", false, sbxrun.Reachable(name)
	}
	out, err := sbxrun.ExecCapture(name, "cat", file)
	if err != nil {
		return "", false, false
	}
	var doc map[string]interface{}
	if json.Unmarshal([]byte(out), &doc) != nil {
		return "", false, false
	}
	value, found = doc["theme"].(string)
	return value, found, true
}

// FromProfile reads the choice recorded in a profile's directory.
func FromProfile(profileDir string) (choice string, ok bool) {
	data, err := os.ReadFile(filepath.Join(profileDir, ProfileFile))
	if err != nil {
		return "", false
	}
	choice = strings.TrimSpace(string(data))
	return choice, choice != ""
}

// SaveToProfile records a choice in a profile's directory, for the next
// re-init to fall back on when the sandbox itself can't be asked.
func SaveToProfile(profileDir, choice string) error {
	return os.WriteFile(filepath.Join(profileDir, ProfileFile), []byte(choice+"\n"), 0o644)
}

// Apply sets the theme the agent starts with, in the kit, two ways — because
// which file the agent finds depends on whether ~/.claude/settings.json
// exists before the kit's own copy is deployed, which it is only if missing:
//
//   - the kit's settings.json gets the theme merged in (or a settings.json
//     holding just the theme is added), for the usual case of a fresh home;
//   - an install step, which runs before the kit's files are deployed,
//     merges the theme into a settings.json that is already there.
func Apply(doc kitspec.Doc, choice string) (kitspec.Doc, error) {
	if choice != Default && !builtIn[choice] && !validName.MatchString(choice) {
		return nil, fmt.Errorf("not a theme name: %q", choice)
	}
	value := settingValue(choice)
	setup, _ := doc["setup"].(kitspec.Doc)
	if setup == nil {
		setup = kitspec.Doc{}
		doc["setup"] = setup
	}

	fileList, _ := setup["files"].([]interface{})
	found := false
	for _, e := range fileList {
		entry, ok := e.(kitspec.Doc)
		if !ok || entry["path"] != settingsPath {
			continue
		}
		found = true
		content, _ := entry["content"].(string)
		merged, err := withTheme(content, value)
		if err != nil {
			return nil, fmt.Errorf("setting the theme in %s: %w", settingsPath, err)
		}
		entry["content"] = merged
	}
	if !found && value != "" {
		content, _ := withTheme("", value)
		fileList = append(fileList, kitspec.Doc{
			"path":          settingsPath,
			"mode":          "0644",
			"onlyIfMissing": true,
			"description":   "Claude Code settings: the theme chosen for this sandbox",
			"content":       content,
		})
		setup["files"] = fileList
	}

	install, _ := setup["install"].([]interface{})
	setup["install"] = append(install, kitspec.Doc{
		"command":     installScript(value),
		"user":        "1000",
		"description": "Set the Claude theme in an existing settings.json",
	})
	return doc, nil
}

// withTheme returns settings JSON content with "theme" set to value, or
// removed when value is empty. Empty content counts as an empty object.
func withTheme(content, value string) (string, error) {
	settings := map[string]interface{}{}
	if strings.TrimSpace(content) != "" {
		if err := json.Unmarshal([]byte(content), &settings); err != nil {
			return "", err
		}
	}
	if value == "" {
		delete(settings, "theme")
	} else {
		settings["theme"] = value
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// installScript merges value into an existing settings.json, with whichever
// of node and python3 the sandbox has; a missing file is left for the
// kit's own copy. No braced shell expansions: sbx validates every
// brace-form substitution in a kit's content against its own placeholders.
func installScript(value string) string {
	return `#!/bin/sh
# Generated by vibe: the Claude theme chosen for this sandbox, merged into a
# settings.json that exists before vibe's own copy (deployed only if
# missing) would be. Empty means Claude's default theme.
F=` + settingsPath + `
THEME='` + value + `'
[ -f "$F" ] || exit 0
[ -f /etc/sandbox-persistent.sh ] && . /etc/sandbox-persistent.sh >/dev/null 2>&1
if command -v node >/dev/null 2>&1; then
  node -e 'const fs = require("fs"); const [f, t] = process.argv.slice(1);
const s = JSON.parse(fs.readFileSync(f, "utf8"));
if (t) s.theme = t; else delete s.theme;
fs.writeFileSync(f, JSON.stringify(s, null, 2) + "\n");' "$F" "$THEME" && exit 0
fi
if command -v python3 >/dev/null 2>&1; then
  python3 -c 'import json, sys
f, t = sys.argv[1], sys.argv[2]
s = json.load(open(f))
if t:
    s["theme"] = t
else:
    s.pop("theme", None)
open(f, "w").write(json.dumps(s, indent=2) + "\n")' "$F" "$THEME" && exit 0
fi
echo "WARN: could not set the Claude theme in $F" >>/tmp/sbx-setup.log
true
`
}
