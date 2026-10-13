package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibe/internal/kitspec"
	"vibe/internal/layout"
)

func themedKit(themes ...string) kitspec.Doc {
	var files []interface{}
	for _, name := range themes {
		files = append(files, kitspec.Doc{"path": kitspec.AgentHome + "/.claude/themes/" + name + ".json", "content": "{}"})
	}
	files = append(files, kitspec.Doc{"path": kitspec.AgentHome + "/.claude/settings.json", "content": "{}", "onlyIfMissing": true})
	return kitspec.Doc{"setup": kitspec.Doc{"files": files, "install": []interface{}{}}}
}

func themeIn(t *testing.T, doc kitspec.Doc) string {
	t.Helper()
	for _, f := range doc["setup"].(kitspec.Doc)["files"].([]interface{}) {
		entry := f.(kitspec.Doc)
		if entry["path"] == kitspec.AgentHome+"/.claude/settings.json" {
			var s map[string]interface{}
			if err := json.Unmarshal([]byte(entry["content"].(string)), &s); err != nil {
				t.Fatal(err)
			}
			theme, _ := s["theme"].(string)
			return theme
		}
	}
	t.Fatal("no settings.json in the kit")
	return ""
}

// With no terminal (as in every test), nothing is asked: the sandbox being
// replaced wins, then the profile's record, then the default — and whatever
// is settled on is what the profile records next. What counts as a theme is
// what ~/.vibe/defaults holds, not what the kit happens to carry.
func TestSetClaudeThemePrefersTheSandboxThenTheProfile(t *testing.T) {
	for _, tc := range []struct {
		name, recorded, fromSandbox, want, setting string
	}{
		{"nothing known", "", "", "default", ""},
		{"profile only", "amber", "", "amber", "custom:amber"},
		{"sandbox beats profile", "amber", "red", "red", "custom:red"},
		{"sandbox on a built-in theme", "", "light", "light", "light"},
		{"theme no longer shipped", "purple", "", "default", ""},
		{"theme only in the kit, not the defaults", "cyan", "", "default", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lay := layout.New(t.TempDir(), t.TempDir())
			for _, name := range []string{"amber", "green", "red"} {
				writeFile(t, lay.HomePath("defaults", "agent-files", ".claude", "themes", name+".json"), "{}")
			}
			profileDir := lay.HomePath("profiles", "demo")
			writeFile(t, filepath.Join(profileDir, "config.yaml"), "name: demo\n")
			if tc.recorded != "" {
				writeFile(t, filepath.Join(profileDir, "claude-theme"), tc.recorded+"\n")
			}

			doc, err := setClaudeTheme(lay, "demo", themedKit("amber", "cyan", "green", "red"), tc.fromSandbox, true, false)
			if err != nil {
				t.Fatal(err)
			}
			if got := themeIn(t, doc); got != tc.setting {
				t.Errorf("settings.json theme = %q, want %q", got, tc.setting)
			}
			data, err := os.ReadFile(filepath.Join(profileDir, "claude-theme"))
			if err != nil || strings.TrimSpace(string(data)) != tc.want {
				t.Errorf("profile records %q (%v), want %q", data, err, tc.want)
			}
		})
	}
}
