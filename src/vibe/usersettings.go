package main

import (
	"bytes"
	"fmt"
	"os"
	"slices"

	"vibe/internal/hostmem"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/prompt"
	"vibe/internal/sbxrun"
	"vibe/internal/settings"
	"vibe/internal/state"
	"vibe/internal/timezone"
)

// fallbackAgents are offered when sbx can't be asked which agents it runs —
// as on a first --install, which can come before --install-sbx. The list is
// sbx v0.46.0's.
var fallbackAgents = []string{"claude", "codex", "copilot", "cursor", "devin", "docker-agent", "droid", "gemini", "kiro", "opencode", "shell"}

// setUpSettings writes ~/.vibe/settings.yaml for this machine, from the
// copy just put there (the package's). With ask and a terminal, it asks how
// much memory sandboxes get, which agent they run, and which features a
// guided profile starts with ticked, starting on the package's choices;
// otherwise it keeps those, with the memory lowered to what this machine
// can give. Everything else in the file is left as it is.
func setUpSettings(lay layout.Layout, ask bool) error {
	path := lay.HomePath("settings.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // the package ships none, so there's nothing to set up
	}
	if err != nil {
		return err
	}
	current, err := settings.Parse(data)
	if err != nil {
		note("  WARNING: can't set up %s, which doesn't parse: %s", displayPath(path), err)
		return nil
	}
	ask = ask && interactive()
	if ask {
		note("setting up %s: the defaults every sandbox starts from", displayPath(path))
	}

	values := []struct {
		key   string
		value interface{}
	}{
		{"memory", pickMemory(current.Memory, ask, "how much memory should each sandbox get? (you're asked again for each new sandbox)")},
		{"agent", pickAgent(current.Agent, ask, "which agent should sandboxes run by default?")},
	}
	if ask {
		features, err := pickDefaultFeatures(lay, current.DefaultFeatures)
		if err != nil {
			return err
		}
		values = append(values, struct {
			key   string
			value interface{}
		}{"defaultFeatures", features})
	}

	changed := data
	for _, v := range values {
		if changed, err = settings.Set(changed, v.key, v.value); err != nil {
			return fmt.Errorf("updating %s: %w", path, err)
		}
	}
	if bytes.Equal(changed, data) {
		return nil
	}
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		return err
	}
	note("  saved your choices in %s", displayPath(path))
	return nil
}

// doSetup is --setup: it asks again the questions a first --install asks,
// so the defaults in ~/.vibe/settings.yaml can be changed. It has nothing to
// ask on without a terminal, which -f would only make more so.
func doSetup(vibeHome string, force bool) error {
	if force || !interactive() {
		return fmt.Errorf("--setup asks questions, so it needs a terminal (and no -f)")
	}
	bundleRoot, err := pathresolve.BundleRoot()
	if err != nil {
		return err
	}
	lay := layout.New(vibeHome, bundleRoot)
	if _, err := os.Stat(lay.HomePath("settings.yaml")); os.IsNotExist(err) {
		return fmt.Errorf("%s doesn't exist yet — run 'vibe --install' first", displayPath(lay.HomePath("settings.yaml")))
	}
	if err := setUpSettings(lay, true); err != nil {
		return err
	}
	setUpDefaultGitToken(vibeHome)
	return nil
}

// lowerMemorySetting lowers the memory setting in the settings.yaml at path
// to what this machine can give a sandbox, when it's more than that.
func lowerMemorySetting(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	current, err := settings.Parse(data)
	if err != nil || current.Memory == "" {
		return err
	}
	memory := pickMemory(current.Memory, false, "")
	if memory == current.Memory {
		return nil
	}
	if data, err = settings.Set(data, "memory", memory); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	note("  saved memory: %s in %s", memory, displayPath(path))
	return nil
}

// pickMemory settles how much memory a sandbox gets, starting from want:
// with ask, the user picks from 4g steps up to half this machine's memory
// (see hostmem.Options); without, want is kept unless it's more than that,
// when it's lowered to the largest step. A machine whose memory can't be
// read keeps want as it is.
func pickMemory(want string, ask bool, question string) string {
	total, err := hostmem.Total()
	if err != nil {
		note("  WARNING: can't tell how much memory this machine has (%s) — using memory: %s", err, want)
		return want
	}
	options, idx, over := hostmem.Pick(total, want)
	host := hostmem.Format(2 * hostmem.Limit(total))
	switch {
	case over:
		note("  memory: %s is more than half this machine's %s — lowering it to %s", want, host, options[idx])
	case want != "" && options[idx] != want:
		note("  memory: can't read %q as a size — using %s", want, options[idx])
	}
	if !ask {
		return options[idx]
	}
	i, ok := chooseFrom(fmt.Sprintf("%s (this machine has %s)", question, host), options, idx)
	if !ok {
		note("  keeping memory: %s", options[idx])
		return options[idx]
	}
	return options[i]
}

// pickAgent asks which agent a sandbox runs, from the ones sbx lists,
// starting on current (claude when unset). Without ask, current is kept.
func pickAgent(current string, ask bool, question string) string {
	if current == "" {
		current = "claude"
	}
	if !ask {
		return current
	}
	agents, err := sbxrun.Agents()
	if err != nil {
		agents = fallbackAgents
	}
	idx := slices.Index(agents, current)
	if idx < 0 {
		agents = append([]string{current}, agents...)
		idx = 0
	}
	i, ok := chooseFrom(question, agents, idx)
	if !ok {
		note("  keeping agent: %s", current)
		return current
	}
	return agents[i]
}

// pickTimezone asks which timezone a sandbox should keep its time in, from
// tzdata's list, starting on current. Without ask, current is kept.
func pickTimezone(current string, ask bool, question string) string {
	if !ask {
		return current
	}
	zones := timezone.Zones(current)
	tty, closeFn, ok := ttyRW()
	if !ok {
		return current
	}
	defer closeFn()
	note("%s (this machine's is %s)", question, timezone.Host())
	i, ok := prompt.Filter(tty, zones, slices.Index(zones, current))
	if !ok {
		note("  keeping timezone: %s", current)
		return current
	}
	return zones[i]
}

// recorded is what field picks from the record of sandbox name, or def when
// there's no record or it's empty there.
func recorded(vibeHome, name, def string, field func(state.Instance) string) string {
	if inst, found, err := state.Load(vibeHome, name); err == nil && found {
		if v := field(inst); v != "" {
			return v
		}
	}
	return def
}

// recordedAgent is the agent the sandbox name was created to run, or def
// when no record says.
func recordedAgent(vibeHome, name, def string) string {
	return recorded(vibeHome, name, def, func(i state.Instance) string { return i.Agent })
}

// recordedTimezone is the timezone sandbox name was created with, or the
// host's when there's no record of one.
func recordedTimezone(vibeHome, name string) string {
	return recorded(vibeHome, name, timezone.Host(), func(i state.Instance) string { return i.Timezone })
}

// preferredDefaultBranch is the name the companion page suggests for a
// repository's default branch, from the settings.yaml at path: the one last
// set. A file that can't be read suggests nothing; setting one says why.
func preferredDefaultBranch(path string) func() string {
	return func() string {
		s, _ := settings.Load(path)
		return s.DefaultBranch
	}
}

// rememberDefaultBranch keeps the name just set for a repository's default
// branch in the settings.yaml at path, to suggest next time, leaving the
// rest of the file as it was.
func rememberDefaultBranch(path string) func(name string) error {
	return func(name string) error {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if data, err = settings.Set(data, "defaultBranch", name); err != nil {
			return fmt.Errorf("updating %s: %w", displayPath(path), err)
		}
		return os.WriteFile(path, data, 0o644)
	}
}
