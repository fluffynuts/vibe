package kitspec

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func v2Doc() Doc {
	return Doc{
		"schemaVersion":     "2",
		"kind":              "mixin",
		"name":              "vibe",
		"displayName":       "vibe base kit",
		"description":       "shared tooling",
		"sourceURL":         "https://example.invalid/vibe",
		"permissions":       Doc{"network": Doc{"allow": []interface{}{"github.com", "*.ubuntu.com"}}},
		"environment":       Doc{"variables": Doc{"DOTNET_NOLOGO": "1", "Mixed.Case-Var": "x"}},
		"ports":             []interface{}{Doc{"container": 5390, "protocol": "tcp", "name": "clipboard"}},
		"agentInstructions": Doc{"content": "read the kit docs"},
		"setup": Doc{
			"install": []interface{}{Doc{"command": "echo hi", "user": "0", "description": "hi"}},
			"files": []interface{}{
				Doc{"path": "/home/agent/a", "mode": "0644", "onlyIfMissing": true, "description": "a", "content": "A"},
				Doc{"path": "/home/agent/b", "mode": "0755", "onlyIfMissing": false, "description": "b", "content": "B"},
			},
			"startup": []interface{}{Doc{"command": []interface{}{"/home/agent/.local/bin/on-start"}, "user": "0", "background": true, "description": "up"}},
		},
	}
}

func capability(t *testing.T, doc Doc, typ string) Doc {
	t.Helper()
	for _, c := range asList(doc["capabilities"]) {
		if asDoc(c)["type"] == typ {
			return asDoc(asDoc(c)["config"])
		}
	}
	t.Fatalf("no %s capability in %v", typ, doc["capabilities"])
	return nil
}

func TestToV3IsAMixinRequiringTheAgent(t *testing.T) {
	out, err := ToV3(v2Doc(), "claude")
	if err != nil {
		t.Fatal(err)
	}
	if out["schemaVersion"] != "3" || out["kind"] != "mixin" {
		t.Fatalf("got %v %v", out["schemaVersion"], out["kind"])
	}
	if r := asList(out["requires"]); len(r) != 1 || r[0] != "claude" {
		t.Fatalf("requires %v", out["requires"])
	}
	for _, gone := range []string{"name", "sourceURL", "permissions", "environment", "ports", "agentInstructions", "setup"} {
		if _, ok := out[gone]; ok {
			t.Errorf("%s should not be carried over as is", gone)
		}
	}
}

func TestToV3NetworkAllowCoversInstallAndRuntime(t *testing.T) {
	out, _ := ToV3(v2Doc(), "claude")
	np := capability(t, out, capNetworkPolicy)
	for _, phase := range []string{"install", "runtime"} {
		allow := asList(asDoc(np[phase])["allow"])
		if len(allow) != 2 || allow[0] != "github.com" {
			t.Errorf("%s allow = %v", phase, allow)
		}
	}
}

func TestToV3EnvironmentBecomesArgsThatSetTheVariable(t *testing.T) {
	out, _ := ToV3(v2Doc(), "claude")
	args := asDoc(out["args"])
	nologo := asDoc(args["dotnet_nologo"])
	if nologo["env"] != "DOTNET_NOLOGO" || nologo["default"] != "1" {
		t.Fatalf("got %v", nologo)
	}
	if asDoc(args["mixed.case-var"])["env"] != "Mixed.Case-Var" {
		t.Fatalf("got %v", args)
	}
}

func TestToV3PortKeepsContainerAndNameOnly(t *testing.T) {
	out, _ := ToV3(v2Doc(), "claude")
	p := capability(t, out, capPort)
	if p["container"] != 5390 || p["name"] != "clipboard" || len(p) != 2 {
		t.Fatalf("got %v", p)
	}
}

func TestToV3LifecycleFilesInvertOnlyIfMissing(t *testing.T) {
	out, _ := ToV3(v2Doc(), "claude")
	lc := capability(t, out, capLifecycle)
	files := asList(lc["files"])
	a, b := asDoc(files[0]), asDoc(files[1])
	if a["overwrite"] != false {
		t.Errorf("a: %v", a)
	}
	if _, has := b["overwrite"]; has {
		t.Errorf("b should leave overwrite to its default: %v", b)
	}
	if _, has := a["onlyIfMissing"]; has {
		t.Errorf("onlyIfMissing is not a v3 field: %v", a)
	}
	if su := asDoc(asList(lc["startup"])[0]); su["background"] != true {
		t.Errorf("startup %v", su)
	}
	if in := asDoc(asList(lc["install"])[0]); in["command"] != "echo hi" || in["user"] != "0" {
		t.Errorf("install %v", in)
	}
}

func TestToV3AgentContextCarriesTheInstructions(t *testing.T) {
	out, _ := ToV3(v2Doc(), "claude")
	if c := capability(t, out, capAgentContext); c["content"] != "read the kit docs" {
		t.Fatalf("got %v", c)
	}
}

func TestToV3RefusesWhatItCannotCarryOver(t *testing.T) {
	d := v2Doc()
	d["credentials"] = []interface{}{}
	if _, err := ToV3(d, "claude"); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("got %v", err)
	}
}

func TestMarshalV3StartsWithTheFrontendHeader(t *testing.T) {
	data, err := MarshalV3(v2Doc(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# syntax=docker/sandbox-kit:3\n") {
		t.Fatalf("header missing: %.60s", data)
	}
	var back map[string]interface{}
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if r := back["requires"].([]interface{}); r[0] != "claude" {
		t.Fatalf("default agent: %v", r)
	}
}

func TestToV3AgentContextIsOptional(t *testing.T) {
	out, _ := ToV3(v2Doc(), "claude")
	for _, c := range asList(out["capabilities"]) {
		if asDoc(c)["type"] == capAgentContext && asDoc(c)["optional"] != true {
			t.Fatalf("agent context should be optional: %v", c)
		}
	}
}

func TestToV3HooksDeclareTheEnvironment(t *testing.T) {
	out, _ := ToV3(v2Doc(), "claude")
	lc := capability(t, out, capLifecycle)
	for _, phase := range []string{"install", "startup"} {
		env := asList(asDoc(asList(lc[phase])[0])["env"])
		if len(env) != 2 || env[0] != "DOTNET_NOLOGO" || env[1] != "Mixed.Case-Var" {
			t.Errorf("%s env = %v", phase, env)
		}
	}
}

func skillsDoc(files ...Doc) Doc {
	doc := v2Doc()
	setup := asDoc(doc["setup"])
	list := asList(setup["files"])
	for _, f := range files {
		list = append(list, f)
	}
	setup["files"] = list
	return doc
}

func lifecycleFiles(t *testing.T, doc Doc) map[string]Doc {
	t.Helper()
	out := map[string]Doc{}
	for _, f := range asList(capability(t, doc, capLifecycle)["files"]) {
		out[asDoc(f)["path"].(string)] = asDoc(f)
	}
	return out
}

func TestToV3MovesSkillsOutOfTheReadOnlySkillsFolder(t *testing.T) {
	out, err := ToV3(skillsDoc(
		Doc{"path": "/home/agent/.claude/skills/vibe-band/.claude-plugin/plugin.json", "mode": "0644", "content": "{}"},
		Doc{"path": "/home/agent/.claude/skills/vibe-band/hooks/hooks.json", "mode": "0644", "content": "{}"},
		Doc{"path": "/home/agent/.claude/skills/vibe-review/SKILL.md", "mode": "0644", "content": "# review"},
	), "claude")
	if err != nil {
		t.Fatal(err)
	}
	files := lifecycleFiles(t, out)
	for path := range files {
		if strings.HasPrefix(path, "/home/agent/.claude/skills") {
			t.Errorf("%s is still under the skills folder", path)
		}
	}
	root := "/home/agent/.local/share/vibe/plugins/"
	// A plugin moves whole, with no plugin wrapped around it.
	for _, want := range []string{
		root + "vibe-band/.claude-plugin/plugin.json",
		root + "vibe-band/hooks/hooks.json",
		root + "vibe-review/skills/vibe-review/SKILL.md",
		root + "vibe-review/.claude-plugin/plugin.json",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("no %s in %v", want, files)
		}
	}
	if _, wrapped := files[root+"vibe-band/skills/vibe-band/hooks/hooks.json"]; wrapped {
		t.Error("a plugin was wrapped in another")
	}
	if !strings.Contains(files[root+"vibe-review/.claude-plugin/plugin.json"]["content"].(string), `"name": "vibe-review"`) {
		t.Error("generated manifest doesn't name the plugin")
	}
	if files[root+"vibe-review/skills/vibe-review/SKILL.md"]["content"] != "# review" {
		t.Error("the skill's content changed")
	}
}

func TestToV3NamesTheRelocatedPluginsInTheAgentsSettings(t *testing.T) {
	out, _ := ToV3(skillsDoc(
		Doc{"path": "/home/agent/.claude/skills/b/SKILL.md", "content": "b"},
		Doc{"path": "/home/agent/.claude/skills/a/SKILL.md", "content": "a"},
	), "claude")
	steps := asList(capability(t, out, capLifecycle)["install"])
	last := asDoc(steps[len(steps)-1])
	cmd := last["command"].(string)
	if last["user"] != "1000" {
		t.Errorf("user %v", last["user"])
	}
	want := "DIRS='/home/agent/.local/share/vibe/plugins/a:/home/agent/.local/share/vibe/plugins/b'"
	if !strings.Contains(cmd, want) {
		t.Errorf("script doesn't name the plugins in order:\n%s", cmd)
	}
	if strings.Contains(cmd, "${") {
		t.Error("sbx validates brace substitutions in a kit: the script mustn't have any")
	}
	if len(steps) != 2 {
		t.Errorf("the document's own install step should stay first: %v", steps)
	}
}

func TestToV3LeavesADocumentWithoutSkillsAlone(t *testing.T) {
	out, _ := ToV3(v2Doc(), "claude")
	if n := len(asList(capability(t, out, capLifecycle)["install"])); n != 1 {
		t.Errorf("%d install steps", n)
	}
}

func TestToV3RefusesALooseFileInTheSkillsFolder(t *testing.T) {
	_, err := ToV3(skillsDoc(Doc{"path": "/home/agent/.claude/skills/notes.md", "content": "x"}), "claude")
	if err == nil || !strings.Contains(err.Error(), "notes.md") {
		t.Fatalf("got %v", err)
	}
}

func TestToV3DoesNotChangeTheDocumentItWasGiven(t *testing.T) {
	doc := skillsDoc(Doc{"path": "/home/agent/.claude/skills/x/SKILL.md", "content": "x"})
	if _, err := ToV3(doc, "claude"); err != nil {
		t.Fatal(err)
	}
	last := asList(asDoc(doc["setup"])["files"])
	if got := asDoc(last[len(last)-1])["path"]; got != "/home/agent/.claude/skills/x/SKILL.md" {
		t.Errorf("path became %v", got)
	}
	if n := len(asList(asDoc(doc["setup"])["install"])); n != 1 {
		t.Errorf("install grew to %d", n)
	}
}

func settingsDoc(content string) Doc {
	return skillsDoc(Doc{"path": "/home/agent/.claude/settings.json", "mode": "0644", "onlyIfMissing": true, "content": content})
}

func lastInstallStep(t *testing.T, doc Doc) Doc {
	t.Helper()
	steps := asList(capability(t, doc, capLifecycle)["install"])
	return asDoc(steps[len(steps)-1])
}

const denyRules = `{"permissions": {"deny": ["Read(./.env)"]}}`

func TestToV3MergesVibesSettingsIntoTheOnesTheKitAlreadyMade(t *testing.T) {
	out, err := ToV3(settingsDoc(denyRules), "claude")
	if err != nil {
		t.Fatal(err)
	}
	step := lastInstallStep(t, out)
	cmd, _ := step["command"].(string)
	if !strings.Contains(cmd, "SETTINGS='"+denyRules+"'") || step["user"] != "1000" {
		t.Errorf("no merge of the deny rules as the agent: %v", step)
	}
	if strings.Contains(cmd, "${") {
		t.Error("sbx validates brace substitutions in a kit: the script mustn't have any")
	}
}

func TestToV3DoesNotMergeSettingsThatAreDeployedOverTheKits(t *testing.T) {
	doc := skillsDoc(Doc{"path": "/home/agent/.claude/settings.json", "content": denyRules})
	out, _ := ToV3(doc, "claude")
	for _, s := range asList(capability(t, out, capLifecycle)["install"]) {
		if strings.Contains(asDoc(s)["command"].(string), "SETTINGS=") {
			t.Error("merged a settings file that replaces the kit's anyway")
		}
	}
}

func TestToV3RefusesSettingsItsMergeStepCannotCarry(t *testing.T) {
	if _, err := ToV3(settingsDoc(`{"a": "it's"}`), "claude"); err == nil {
		t.Error("a quote would break out of the script's literal")
	}
}

// The merge is run as the sandbox runs it, on a settings.json like the
// kit's: what the user has is kept, the rules are added, once.
func TestTheSettingsMergeKeepsWhatIsThereAddsWhatIsNotAndIsRepeatable(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	out, err := ToV3(settingsDoc(`{"permissions": {"deny": ["Read(./.env)", "Read(**/*.pem)"], "defaultMode": "plan"}}`), "claude")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(lastInstallStep(t, out)["command"].(string), "/home/agent/.claude/settings.json", "$FILE")
	dir := t.TempDir()
	file := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(file, []byte(`{"permissions": {"defaultMode": "bypassPermissions", "deny": ["Read(**/*.pem)"]}, "theme": "x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func() {
		cmd := exec.Command("/bin/sh", "-c", script)
		cmd.Env = append(os.Environ(), "FILE="+file)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	run()
	run()
	var got struct {
		Permissions struct {
			DefaultMode string   `json:"defaultMode"`
			Deny        []string `json:"deny"`
		} `json:"permissions"`
		Theme string `json:"theme"`
	}
	data, _ := os.ReadFile(file)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if got.Permissions.DefaultMode != "bypassPermissions" || got.Theme != "x" {
		t.Errorf("what was there was changed: %s", data)
	}
	if strings.Join(got.Permissions.Deny, ",") != "Read(**/*.pem),Read(./.env)" {
		t.Errorf("deny = %v, want the pem rule once and the .env rule added", got.Permissions.Deny)
	}
}
