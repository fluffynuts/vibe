package claudetheme

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"vibe/internal/kitspec"
)

func kit(files ...kitspec.Doc) kitspec.Doc {
	list := make([]interface{}, len(files))
	for i, f := range files {
		list[i] = f
	}
	return kitspec.Doc{"setup": kitspec.Doc{"files": list, "install": []interface{}{}}}
}

func file(p, content string) kitspec.Doc {
	return kitspec.Doc{"path": p, "content": content, "onlyIfMissing": true}
}

func TestAvailableListsTheDefaultsThemesOnly(t *testing.T) {
	defaults := t.TempDir()
	themes := filepath.Join(defaults, "agent-files", ".claude", "themes")
	for _, name := range []string{"red.json", "amber.json", "notes.txt", "Bad Name.json"} {
		writeTestFile(t, filepath.Join(themes, name), "{}")
	}
	writeTestFile(t, filepath.Join(themes, "nested", "green.json"), "{}")

	if got, want := Available(defaults), []string{"amber", "red"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Available = %v, want %v", got, want)
	}
	if got, want := Choices(Available(defaults)), []string{"default", "amber", "red"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Choices = %v, want %v", got, want)
	}
	if got := Available(t.TempDir()); len(got) != 0 {
		t.Errorf("Available(defaults with no themes) = %v, want none", got)
	}
}

// The bundle's own defaults are what seed ~/.vibe/defaults.
func TestAvailableFindsTheBundledThemes(t *testing.T) {
	root, _ := filepath.Abs("../..")
	if got, want := Available(filepath.Join(root, "defaults")), []string{"amber", "blue", "cyan", "green", "orange", "red", "yellow"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Available(bundled defaults) = %v, want %v", got, want)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	known := []string{"amber", "red"}
	for _, tt := range []struct {
		in, want string
		ok       bool
	}{
		{"", "default", true}, {"default", "default", true}, {"amber", "amber", true},
		{"light", "light", true}, {"purple", "default", false},
	} {
		got, ok := Resolve(tt.in, known)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Resolve(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSettingValuesRoundTrip(t *testing.T) {
	for choice, value := range map[string]string{"default": "", "amber": "custom:amber", "light": "light"} {
		if got := settingValue(choice); got != value {
			t.Errorf("settingValue(%q) = %q, want %q", choice, got, value)
		}
		if got := choiceFromSetting(value); got != choice {
			t.Errorf("choiceFromSetting(%q) = %q, want %q", value, got, choice)
		}
	}
}

func settingsOf(t *testing.T, doc kitspec.Doc) map[string]interface{} {
	t.Helper()
	for _, f := range files(doc) {
		if f["path"] == settingsPath {
			var s map[string]interface{}
			if err := json.Unmarshal([]byte(f["content"].(string)), &s); err != nil {
				t.Fatalf("settings.json content: %v", err)
			}
			return s
		}
	}
	return nil
}

func TestApplyMergesIntoTheKitsSettings(t *testing.T) {
	doc := kit(file(settingsPath, `{"permissions":{"deny":["Read(./.env)"]}}`))
	doc, err := Apply(doc, "amber")
	if err != nil {
		t.Fatal(err)
	}
	s := settingsOf(t, doc)
	if s["theme"] != "custom:amber" {
		t.Errorf("theme = %v, want custom:amber", s["theme"])
	}
	if _, kept := s["permissions"]; !kept {
		t.Error("the rest of settings.json was lost")
	}
	install := doc["setup"].(kitspec.Doc)["install"].([]interface{})
	if len(install) != 1 || !strings.Contains(install[0].(kitspec.Doc)["command"].(string), "THEME='custom:amber'") {
		t.Errorf("install steps = %v, want one setting custom:amber", install)
	}
	if strings.Contains(install[0].(kitspec.Doc)["command"].(string), "${") {
		t.Error("the install script has a braced expansion, which sbx rejects")
	}
}

func TestApplyAddsSettingsWhenTheKitHasNone(t *testing.T) {
	doc, err := Apply(kit(), "red")
	if err != nil {
		t.Fatal(err)
	}
	if s := settingsOf(t, doc); s == nil || s["theme"] != "custom:red" {
		t.Errorf("settings = %v, want a new settings.json with custom:red", s)
	}
	// ...but the default needs no file of its own.
	doc, _ = Apply(kit(), "default")
	if s := settingsOf(t, doc); s != nil {
		t.Errorf("the default theme added a settings.json: %v", s)
	}
}

func TestApplyDefaultRemovesATheme(t *testing.T) {
	doc, _ := Apply(kit(file(settingsPath, `{"theme":"custom:red","x":1}`)), "default")
	if s := settingsOf(t, doc); s["theme"] != nil || s["x"] == nil {
		t.Errorf("settings = %v, want the theme gone and the rest kept", s)
	}
}

func TestApplyRejectsNamesThatNeedQuoting(t *testing.T) {
	if _, err := Apply(kit(), "red'; rm -rf /"); err == nil {
		t.Error("Apply accepted a theme name that would break its script")
	}
}

// The generated step, run against a settings.json that is already there,
// merges the theme in and leaves the rest alone.
func TestInstallScriptMergesIntoAnExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the install script is a shell script")
	}
	for _, tool := range []string{"node", "python3"} {
		if _, err := exec.LookPath(tool); err != nil {
			continue
		}
		t.Run(tool, func(t *testing.T) {
			dir := t.TempDir()
			f := filepath.Join(dir, "settings.json")
			os.WriteFile(f, []byte(`{"defaultMode":"bypassPermissions","theme":"dark"}`), 0o644)
			script := strings.Replace(installScript("custom:green"), "F="+settingsPath, "F="+f, 1)
			if tool == "python3" { // take node away, so the fall-back is what runs
				script = strings.Replace(script, "command -v node", "false", 1)
			}
			if out, err := exec.Command("sh", "-c", script).CombinedOutput(); err != nil {
				t.Fatalf("script: %v: %s", err, out)
			}
			var s map[string]interface{}
			data, _ := os.ReadFile(f)
			if err := json.Unmarshal(data, &s); err != nil {
				t.Fatal(err)
			}
			if s["theme"] != "custom:green" || s["defaultMode"] != "bypassPermissions" {
				t.Errorf("settings after the script = %v", s)
			}
		})
	}
}

func TestProfileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := FromProfile(dir); ok {
		t.Error("a profile with no recorded theme reported one")
	}
	if err := SaveToProfile(dir, "amber"); err != nil {
		t.Fatal(err)
	}
	if got, ok := FromProfile(dir); !ok || got != "amber" {
		t.Errorf("FromProfile = %q, %v; want amber", got, ok)
	}
}

// fakeSandbox puts an `sbx` on PATH whose exec runs the command here, with
// /home/agent standing for the returned directory.
func fakeSandbox(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake sbx is a shell script")
	}
	root := t.TempDir()
	bin := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = exec ] || exit 1\nshift 3\n" +
		"for a; do shift; set -- \"$@\" \"$(printf %s \"$a\" | sed \"s#^/home/agent#" + root + "#\")\"; done\n" +
		"exec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "sbx"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return root
}

func TestFromSandboxReadsWhateverTheAgentIsSetTo(t *testing.T) {
	home := fakeSandbox(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	legacy := filepath.Join(home, ".claude.json")
	os.MkdirAll(filepath.Dir(settings), 0o755)

	check := func(label, want string) {
		t.Helper()
		if got, ok := FromSandbox("any"); !ok || got != want {
			t.Errorf("%s: FromSandbox = %q, %v; want %q", label, got, ok, want)
		}
	}
	check("no settings at all", "default")
	os.WriteFile(legacy, []byte(`{"theme":"light"}`), 0o644)
	check("only the old global config", "light")
	os.WriteFile(settings, []byte(`{"defaultMode":"x"}`), 0o644)
	check("settings without a theme", "light")
	os.WriteFile(settings, []byte(`{"theme":"custom:amber"}`), 0o644)
	check("settings with a custom theme", "amber")

	os.WriteFile(settings, []byte(`not json`), 0o644)
	if _, ok := FromSandbox("any"); ok {
		t.Error("an unreadable settings.json was taken as an answer")
	}
}

func files(doc kitspec.Doc) []kitspec.Doc {
	setup, _ := doc["setup"].(kitspec.Doc)
	list, _ := setup["files"].([]interface{})
	var out []kitspec.Doc
	for _, e := range list {
		if entry, ok := e.(kitspec.Doc); ok {
			out = append(out, entry)
		}
	}
	return out
}
