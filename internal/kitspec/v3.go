package kitspec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// V3Header is the first line of a v3 kit: it names the BuildKit frontend sbx
// builds a source-form kit with.
const V3Header = "# syntax=docker/sandbox-kit:3\n"

// Capability types a v3 kit's behaviour is declared with.
const (
	capNetworkPolicy = "com.docker.sandbox/network-policy@1"
	capLifecycle     = "com.docker.sandbox/lifecycle@1"
	capPort          = "com.docker.sandbox/port@1"
	capAgentContext  = "com.docker.sandbox/agent-context@2"
)

// v2Keys are the top-level fields of the document vibe builds (the v2 shape
// its config.yaml fragments are written in) that ToV3 knows how to carry over.
var v2Keys = map[string]bool{
	"schemaVersion":     true,
	"kind":              true,
	"name":              true,
	"displayName":       true,
	"description":       true,
	"sourceURL":         true,
	"permissions":       true,
	"environment":       true,
	"ports":             true,
	"agentInstructions": true,
	"setup":             true,
}

func asDoc(v interface{}) Doc {
	switch m := v.(type) {
	case Doc:
		return m
	case map[string]interface{}:
		return Doc(m)
	}
	return nil
}

func asList(v interface{}) []interface{} {
	l, _ := v.([]interface{})
	return l
}

// argNameRE is what a v3 kit argument's name may be; anything else in an
// environment variable's name becomes an underscore.
var argNameRE = regexp.MustCompile(`[^a-z0-9_.-]`)

func argName(env string) string {
	n := argNameRE.ReplaceAllString(strings.ToLower(env), "_")
	if n == "" || n[0] < 'a' || n[0] > 'z' {
		n = "v" + n
	}
	return n
}

// ToV3 renders the document Build produced (the v2 kit shape, which is what
// the config.yaml fragments, the claude theme and the library all work in)
// as a schemaVersion "3" mixin for agent, which sbx's v3 claude requires: it
// refuses to compose a v3 agent with a v1/v2 kit. A field it doesn't know how
// to carry over is an error rather than silently dropped.
func ToV3(doc Doc, agent string) (Doc, error) {
	for k := range doc {
		if !v2Keys[k] {
			return nil, fmt.Errorf("kit field %q has no schemaVersion 3 equivalent in vibe yet", k)
		}
	}
	if agent == "" {
		agent = "claude"
	}
	out := Doc{
		"schemaVersion": "3",
		"kind":          "mixin",
		"requires":      []interface{}{agent},
	}
	for _, k := range []string{"displayName", "description"} {
		if v, ok := doc[k]; ok {
			out[k] = v
		}
	}

	setup, err := relocateSkills(asDoc(doc["setup"]))
	if err != nil {
		return nil, err
	}
	setup, err = mergeSettingsIntoTheKits(setup)
	if err != nil {
		return nil, err
	}

	var capabilities []interface{}

	// permissions.network.allow: the install steps run before the agent does
	// and need the same hosts, so the list covers both phases.
	if network := asDoc(asDoc(doc["permissions"])["network"]); network != nil {
		if allow := asList(network["allow"]); len(allow) > 0 {
			capabilities = append(capabilities, Doc{
				"type": capNetworkPolicy,
				"config": Doc{
					"install": Doc{"allow": allow},
					"runtime": Doc{"allow": allow},
				},
			})
		}
	}

	for _, p := range asList(doc["ports"]) {
		port := asDoc(p)
		if port == nil {
			continue
		}
		// The port capability is written here with no protocol, which is how a
		// tcp port is published: a port of another protocol would be published
		// as tcp, unsaid, so it is refused.
		if proto, _ := port["protocol"].(string); proto != "" && !strings.EqualFold(proto, "tcp") {
			return nil, fmt.Errorf("port %v is %s: vibe only carries tcp ports into a schemaVersion 3 kit so far", port["container"], proto)
		}
		config := Doc{"container": port["container"]}
		if name, ok := port["name"]; ok {
			config["name"] = name
		}
		capabilities = append(capabilities, Doc{"type": capPort, "config": config})
	}

	if content, _ := asDoc(doc["agentInstructions"])["content"].(string); content != "" {
		// Optional, as sbx's own claude kit declares its agent context: a
		// runtime without agent-context@2 (sbx before v0.51) skips it rather
		// than refusing the whole sandbox.
		capabilities = append(capabilities, Doc{
			"type":     capAgentContext,
			"optional": true,
			"config":   Doc{"content": content},
		})
	}

	if lifecycle := lifecycleConfig(setup, envNames(doc)); len(lifecycle) > 0 {
		capabilities = append(capabilities, Doc{"type": capLifecycle, "config": lifecycle})
	}
	if len(capabilities) > 0 {
		out["capabilities"] = capabilities
	}

	// environment.variables become arguments that set the variable, since a
	// v3 kit has no environment block of its own.
	if vars := asDoc(asDoc(doc["environment"])["variables"]); len(vars) > 0 {
		names := envNames(doc)
		args := Doc{}
		for _, k := range names {
			name := argName(k)
			if _, taken := args[name]; taken {
				return nil, fmt.Errorf("environment variables %q collide as kit arguments (%q)", k, name)
			}
			args[name] = Doc{
				"env":         k,
				"default":     fmt.Sprint(vars[k]),
				"description": "sets " + k,
			}
		}
		out["args"] = args
	}
	return out, nil
}

// envNames is the sorted names of the document's environment.variables.
func envNames(doc Doc) []string {
	vars := asDoc(asDoc(doc["environment"])["variables"])
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// withEnv gives a hook the env list v3 needs it to declare: a v3 hook sees
// only the variables it names, where a v2 one saw the whole environment.
func withEnv(step Doc, env []string) Doc {
	if len(env) == 0 {
		return step
	}
	list := make([]interface{}, 0, len(env))
	for _, e := range env {
		list = append(list, e)
	}
	step["env"] = list
	return step
}

// lifecycleConfig maps setup.install, setup.files and setup.startup to the
// fields of a v3 lifecycle capability; env names the variables every hook
// may see.
func lifecycleConfig(setup Doc, env []string) Doc {
	if setup == nil {
		return nil
	}
	config := Doc{}
	if install := asList(setup["install"]); len(install) > 0 {
		steps := make([]interface{}, 0, len(install))
		for _, s := range install {
			steps = append(steps, withEnv(copyFields(asDoc(s), "command", "user", "description"), env))
		}
		config["install"] = steps
	}
	if files := asList(setup["files"]); len(files) > 0 {
		entries := make([]interface{}, 0, len(files))
		for _, f := range files {
			src := asDoc(f)
			entry := copyFields(src, "path", "mode", "description", "content")
			// "only if missing" is "don't overwrite": v3 states it the other way round.
			if only, _ := src["onlyIfMissing"].(bool); only {
				entry["overwrite"] = false
			}
			entries = append(entries, entry)
		}
		config["files"] = entries
	}
	if startup := asList(setup["startup"]); len(startup) > 0 {
		steps := make([]interface{}, 0, len(startup))
		for _, s := range startup {
			steps = append(steps, withEnv(copyFields(asDoc(s), "command", "user", "background", "description"), env))
		}
		config["startup"] = steps
	}
	return config
}

func copyFields(src Doc, keys ...string) Doc {
	out := Doc{}
	for _, k := range keys {
		if v, ok := src[k]; ok {
			out[k] = v
		}
	}
	return out
}

// MarshalV3 renders the document as a v3 kit's spec.yaml, frontend header
// first.
func MarshalV3(doc Doc, agent string) ([]byte, error) {
	v3, err := ToV3(doc, agent)
	if err != nil {
		return nil, err
	}
	body, err := yaml.Marshal(map[string]interface{}(v3))
	if err != nil {
		return nil, err
	}
	return append([]byte(V3Header), body...), nil
}

const (
	// skillsRoot is where Claude Code looks for a user's skills. sbx's own claude
	// kit mounts a read-only folder of the host's skills there, so nothing a kit
	// writes under it can be applied.
	skillsRoot = AgentHome + "/.claude/skills/"
	// pluginsRoot is where those skills go instead: each as a Claude Code
	// plugin, which the agent is told to load from there.
	pluginsRoot = AgentHome + "/.local/share/vibe/plugins"
	// settingsPath is where the agent's user settings are, and so where its
	// plugin folders are named (the env block is how CLAUDE_CODE_PLUGIN_DIRS
	// reaches a session no flag can be given to).
	settingsPath = AgentHome + "/.claude/settings.json"
)

var pluginNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// relocateSkills returns setup with every file the document puts under
// ~/.claude/skills/<name>/ placed in a plugin folder of its own instead,
// plus an install step that has the agent load those folders. A <name> that
// is already a plugin (it has a .claude-plugin/plugin.json) is moved whole;
// a plain skill is given a generated plugin to live in, as skills/<name>/. setup
// itself is left as it was.
func relocateSkills(setup Doc) (Doc, error) {
	files := asList(setup["files"])
	hasManifest := map[string]bool{}
	for _, f := range files {
		name, inner, ok, err := skillFile(asDoc(f))
		if err != nil {
			return nil, err
		}
		if ok && inner == ".claude-plugin/plugin.json" {
			hasManifest[name] = true
		}
	}

	moved := make([]interface{}, 0, len(files))
	plugins := map[string]bool{}
	for _, f := range files {
		entry := asDoc(f)
		name, inner, ok, _ := skillFile(entry)
		if !ok {
			moved = append(moved, f)
			continue
		}
		plugins[name] = true
		dest := pluginsRoot + "/" + name + "/"
		if !hasManifest[name] {
			dest += "skills/" + name + "/"
		}
		relocated := Doc{}
		for k, v := range entry {
			relocated[k] = v
		}
		relocated["path"] = dest + inner
		moved = append(moved, relocated)
	}
	if len(plugins) == 0 {
		return setup, nil
	}

	names := make([]string, 0, len(plugins))
	for name := range plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	dirs := make([]string, 0, len(names))
	for _, name := range names {
		dirs = append(dirs, pluginsRoot+"/"+name)
		if hasManifest[name] {
			continue
		}
		manifest, _ := json.MarshalIndent(map[string]string{
			"name":        name,
			"version":     "0.1.0",
			"description": "vibe's " + name + " skill",
		}, "", "  ")
		moved = append(moved, Doc{
			"path":        pluginsRoot + "/" + name + "/.claude-plugin/plugin.json",
			"mode":        "0644",
			"description": "plugin.json for the " + name + " skill",
			"content":     string(manifest) + "\n",
		})
	}

	out := Doc{}
	for k, v := range setup {
		out[k] = v
	}
	out["files"] = moved
	out["install"] = append(append([]interface{}{}, asList(setup["install"])...), Doc{
		"command":     pluginDirsScript(strings.Join(dirs, ":")),
		"user":        "1000",
		"description": "Have Claude Code load vibe's plugins and skills",
	})
	return out, nil
}

// skillFile says whether entry is a file under ~/.claude/skills/, and if so which
// skill and where in it.
func skillFile(entry Doc) (name, inner string, ok bool, err error) {
	path, _ := entry["path"].(string)
	if !strings.HasPrefix(path, skillsRoot) {
		return "", "", false, nil
	}
	name, inner, found := strings.Cut(strings.TrimPrefix(path, skillsRoot), "/")
	if !found || inner == "" {
		return "", "", false, fmt.Errorf("%s sits directly in the skills folder, which sbx keeps read-only: put it in a folder of its own", path)
	}
	if !pluginNameRE.MatchString(name) {
		return "", "", false, fmt.Errorf("skill folder name %q is not one a plugin can have", name)
	}
	return name, inner, true, nil
}

// pluginDirsScript is the install step that adds dirs, a colon-separated list, to
// CLAUDE_CODE_PLUGIN_DIRS in the agent's user settings, where its sessions
// read it from: the folders already named there are kept, and none is named
// twice. It runs after the claude kit has made the settings file, as the
// claude theme step does, and says in the setup log if it can't.
func pluginDirsScript(dirs string) string {
	return `#!/bin/sh
# Generated by vibe: sbx keeps ~/.claude/skills read-only, so vibe's skills and
# plugins live in plugin folders of their own, named here.
F=` + settingsPath + `
DIRS='` + dirs + `'
if [ ! -f "$F" ]; then
  echo "WARN: no $F to name vibe's plugins in — they will not load" >>/tmp/sbx-setup.log
  exit 0
fi
[ -f /etc/sandbox-persistent.sh ] && . /etc/sandbox-persistent.sh >/dev/null 2>&1
if command -v node >/dev/null 2>&1; then
  node -e 'const fs = require("fs"); const [f, d] = process.argv.slice(1);
const s = JSON.parse(fs.readFileSync(f, "utf8"));
const have = ((s.env || {}).CLAUDE_CODE_PLUGIN_DIRS || "").split(":").filter(Boolean);
const add = d.split(":").filter((x) => !have.includes(x));
s.env = Object.assign({}, s.env, { CLAUDE_CODE_PLUGIN_DIRS: have.concat(add).join(":") });
fs.writeFileSync(f, JSON.stringify(s, null, 2) + "\n");' "$F" "$DIRS" && exit 0
fi
if command -v python3 >/dev/null 2>&1; then
  python3 -c 'import json, sys
f, d = sys.argv[1], sys.argv[2]
s = json.load(open(f))
env = s.setdefault("env", {})
have = [x for x in env.get("CLAUDE_CODE_PLUGIN_DIRS", "").split(":") if x]
env["CLAUDE_CODE_PLUGIN_DIRS"] = ":".join(have + [x for x in d.split(":") if x not in have])
open(f, "w").write(json.dumps(s, indent=2) + "\n")' "$F" "$DIRS" && exit 0
fi
echo "WARN: could not name vibe's plugins in $F — they will not load" >>/tmp/sbx-setup.log
true
`
}

// mergeSettingsIntoTheKits returns setup with an install step that merges the
// content of its ~/.claude/settings.json file into the settings.json that is
// already there. The file is deployed only if missing, and sbx's claude kit
// makes the settings file first, so without the step vibe's deny rules would
// never reach the agent. setup itself is left as it was.
func mergeSettingsIntoTheKits(setup Doc) (Doc, error) {
	for _, f := range asList(setup["files"]) {
		entry := asDoc(f)
		if entry["path"] != settingsPath || entry["onlyIfMissing"] != true {
			continue
		}
		content, _ := entry["content"].(string)
		if strings.Contains(content, "'") || strings.Contains(content, "${") {
			return nil, fmt.Errorf("%s holds a quote or a brace substitution, which its merge step can't carry", settingsPath)
		}
		out := Doc{}
		for k, v := range setup {
			out[k] = v
		}
		out["install"] = append(append([]interface{}{}, asList(setup["install"])...), Doc{
			"command":     settingsMergeScript(strings.TrimSpace(content)),
			"user":        "1000",
			"description": "Merge vibe's settings (such as its deny rules) into the agent's settings.json",
		})
		return out, nil
	}
	return setup, nil
}

// settingsMergeScript is the install step that merges the JSON settings into
// the agent's settings.json: objects are merged, arrays are added to without
// repeating what is there, and a value the file already has is kept. Run
// again, it changes nothing.
func settingsMergeScript(settings string) string {
	return `#!/bin/sh
# Generated by vibe: its settings, merged into the settings.json sbx's claude
# kit has already made, which vibe's own copy (deployed only if missing) never
# replaces.
F=` + settingsPath + `
SETTINGS='` + settings + `'
if [ ! -f "$F" ]; then
  echo "WARN: no $F to merge vibe's settings into" >>/tmp/sbx-setup.log
  exit 0
fi
[ -f /etc/sandbox-persistent.sh ] && . /etc/sandbox-persistent.sh >/dev/null 2>&1
if command -v python3 >/dev/null 2>&1; then
  python3 -c 'import json, sys
def merge(have, add):
    if isinstance(have, list) and isinstance(add, list):
        return have + [x for x in add if x not in have]
    if isinstance(have, dict) and isinstance(add, dict):
        for k, v in add.items():
            have[k] = merge(have[k], v) if k in have else v
    return have
f = sys.argv[1]
s = merge(json.load(open(f)), json.loads(sys.argv[2]))
open(f, "w").write(json.dumps(s, indent=2) + "\n")' "$F" "$SETTINGS" && exit 0
fi
echo "WARN: could not merge vibe's settings into $F — its deny rules do not apply" >>/tmp/sbx-setup.log
true
`
}
