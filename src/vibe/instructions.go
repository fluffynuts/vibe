package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"context"
	"os"
	"path"
	"path/filepath"
	"time"

	"vibe/internal/companion"
	"vibe/internal/instructions"
	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/sbxrun"
	"vibe/internal/state"
	"vibe/internal/termtitle"
	"vibe/internal/upgrade"
)

// --- the user's instructions, in the sandbox ---------------------------------

// instructionsFor is the merged text of the user's instructions for the
// sandbox name — the universal file, then its profile's — and the agent it
// runs ("" for none known).
func instructionsFor(lay layout.Layout, name string) (merged, agent string, err error) {
	inst, found, err := state.Load(lay.Home, name)
	if err != nil || !found {
		return "", "", err
	}
	agent = inst.Agent
	if agent == "" {
		if merged, err := loadSettings(lay, inst.Profile); err == nil {
			agent = agentOf(merged)
		}
	}
	read := func(path string) string {
		data, _ := os.ReadFile(path)
		return string(data)
	}
	universal := ""
	if p := lay.HomePath(instructions.File); p != "" {
		universal = read(p)
	}
	project := read(filepath.Join(lay.ProfileDir(inst.Profile), instructions.File))
	return instructions.Merge(universal, project), agent, nil
}

// sandboxFileWrite writes stdin to the file named by $1 in the sandbox,
// replacing it whole (so the agent never reads half of it), and leaves it
// the agent user's when run as root. No braced expansions: see kitspec.
const sandboxFileWrite = `f=$1
d=$(dirname "$f")
mkdir -p "$d" && cat > "$f.vibe-tmp" && mv "$f.vibe-tmp" "$f" || exit 1
if [ "$(id -u)" = 0 ]; then
  u=$(id -un 1000 2>/dev/null || echo agent)
  chown "$u:" "$d" "$f" 2>/dev/null || true
fi`

// applyInstructions gives the sandbox's agent the user's instructions: the
// merged text in its own file (for an agent that follows imports), and
// vibe's marked part of the agent's global instructions file pointing at it
// (or holding the text, for one that doesn't). Nothing outside that part is
// touched. It says whether the sandbox's agent takes these at all; it must
// be running.
func applyInstructions(lay layout.Layout, name string) (supported bool, err error) {
	merged, agent, err := instructionsFor(lay, name)
	if err != nil {
		return false, err
	}
	spec, ok := instructions.Agents[agent]
	if !ok {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	home := kitspec.AgentHome
	global := path.Join(home, spec.Dir, spec.Global)
	mergedPath := path.Join(home, spec.Dir, instructions.MergedFile)

	if spec.Imports {
		if merged == "" {
			if _, err := sbxrun.ExecStdout(ctx, name, "rm", "-f", mergedPath); err != nil {
				return true, err
			}
		} else if err := sbxrun.ExecInput(name, merged, "sh", "-c", sandboxFileWrite, "sh", mergedPath); err != nil {
			return true, err
		}
	}
	// What's there now, read as file content: stdout alone.
	current, err := sbxrun.ExecStdout(ctx, name, "sh", "-c", `if [ -f "$1" ]; then cat "$1"; fi`, "sh", global)
	if err != nil {
		return true, err
	}
	updated := instructions.WithBlock(current, spec.Block(home, merged))
	switch {
	case updated == current:
		return true, nil
	case updated == "":
		// The file was only ever vibe's part.
		_, err = sbxrun.ExecStdout(ctx, name, "rm", "-f", global)
		return true, err
	}
	return true, sbxrun.ExecInput(name, updated, "sh", "-c", sandboxFileWrite, "sh", global)
}

// --- the Admin tab's editors -------------------------------------------------

// instructionsFile is where one of the user's instructions files is, for
// the sandbox the tab is for, and whether the tab may write it.
func (a *companionAdmin) instructionsFile(which string) (file companion.AdminInstructionsFile, profile string) {
	file.Which = which
	if which == "universal" {
		file.Label = "For every sandbox"
		file.Path = a.lay.HomePath(instructions.File)
		file.Editable = file.Path != ""
		return file, ""
	}
	file.Label = "For this profile's sandboxes"
	if inst, found, err := state.Load(a.lay.Home, a.name); err == nil && found {
		profile = inst.Profile
	}
	file.Label = fmt.Sprintf("For profile '%s'", profile)
	dir := a.lay.HomePath("profiles", profile)
	if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
		file.Path, file.Editable = filepath.Join(dir, instructions.File), true
		return file, profile
	}
	// Writing it would make a folder in ~/.vibe for a profile vibe ships,
	// which would hide the shipped one.
	file.Path = filepath.Join(a.lay.ProfileDir(profile), instructions.File)
	file.Note = fmt.Sprintf("Profile '%s' ships with vibe: copy it into %s to give it instructions of its own.",
		profile, displayPath(a.lay.HomePath("profiles")))
	return file, profile
}

// textVersion identifies a file's text as read, for noticing it changed.
func textVersion(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func readText(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

func (a *companionAdmin) Instructions() companion.AdminInstructions {
	out := companion.AdminInstructions{}
	for _, which := range []string{"universal", "project"} {
		file, _ := a.instructionsFile(which)
		file.Text = readText(file.Path)
		file.Version = textVersion(file.Text)
		file.Path = displayPath(file.Path)
		out.Files = append(out.Files, file)
	}
	_, agent, _ := instructionsFor(a.lay, a.name)
	spec, known := instructions.Agents[agent]
	out.Agent, out.Applies, out.CanOptimise = agent, known, known && spec.OneShot != nil
	out.Running = sbxrun.Reachable(a.name)
	return out
}

func (a *companionAdmin) SaveInstructions(save companion.AdminSave) (companion.AdminSaved, error) {
	file, profile := a.instructionsFile(save.Which)
	if !file.Editable {
		return companion.AdminSaved{}, errors.New(file.Note)
	}
	disk := readText(file.Path)
	text := save.Text
	if textVersion(disk) != save.Version {
		conflict := companion.AdminSaved{Conflict: true, DiskText: disk, DiskVersion: textVersion(disk)}
		merged, canMerge, why := upgrade.MergeText([]byte(save.Base), []byte(save.Text), []byte(disk))
		conflict.CanMerge, conflict.Why = canMerge, why
		switch {
		case save.Resolve == "overwrite":
		case save.Resolve == "merge" && canMerge:
			text = string(merged)
		default:
			return conflict, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(file.Path), 0o755); err != nil {
		return companion.AdminSaved{}, err
	}
	if err := os.WriteFile(file.Path, []byte(text), 0o644); err != nil {
		return companion.AdminSaved{}, err
	}
	saved := companion.AdminSaved{Saved: true, Text: text, Version: textVersion(text)}
	saved.Updated, saved.NotRunning, saved.Failed = a.pushInstructions(profile)
	return saved, nil
}

// pushInstructions gives the instructions to every running sandbox they
// reach, now, rather than at its next start: all of vibe's sandboxes, or
// only those on profile when it isn't "".
func (a *companionAdmin) pushInstructions(profile string) (updated, notRunning []string, failed map[string]string) {
	instances, err := state.List(a.lay.Home)
	if err != nil {
		return nil, nil, map[string]string{a.name: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), statusWithin)
	defer cancel()
	list := a.listSandboxes
	if list == nil {
		list = sbxrun.ListContext
	}
	statuses, err := list(ctx)
	if err != nil {
		// Saved all the same: each sandbox gets it when it next starts.
		failed = map[string]string{}
		why := fmt.Sprintf("sbx didn't say which sandboxes are running (%v); it gets the change when it next starts", err)
		if ctx.Err() != nil {
			why = fmt.Sprintf("sbx didn't answer within %s; it gets the change when it next starts", statusWithin)
		}
		for _, inst := range instances {
			if profile == "" || inst.Profile == profile {
				failed[inst.Name] = why
			}
		}
		return nil, nil, failed
	}
	running := map[string]bool{}
	for _, st := range statuses {
		running[st.Name] = st.Running
	}
	for _, inst := range instances {
		if profile != "" && inst.Profile != profile {
			continue
		}
		if !running[inst.Name] {
			notRunning = append(notRunning, inst.Name)
			continue
		}
		supported, err := applyInstructions(a.lay, inst.Name)
		switch {
		case err != nil:
			if failed == nil {
				failed = map[string]string{}
			}
			failed[inst.Name] = err.Error()
		case supported:
			updated = append(updated, inst.Name)
		}
	}
	return updated, notRunning, failed
}

func (a *companionAdmin) Optimise(ctx context.Context, which, text string) (string, error) {
	_, agent, err := instructionsFor(a.lay, a.name)
	if err != nil {
		return "", err
	}
	spec := instructions.Agents[agent]
	if spec.OneShot == nil {
		return "", fmt.Errorf("optimising isn't available for %s yet", agent)
	}
	if !sbxrun.Reachable(a.name) {
		return "", errors.New("the sandbox isn't running: start it to optimise")
	}
	other := "project"
	if which == "project" {
		other = "universal"
	}
	otherFile, _ := a.instructionsFile(other)
	prompt := instructions.OptimisePrompt(which == "universal", text, readText(otherFile.Path))

	// The terminal's own screen belongs to the agent: say so in its title.
	title := "VIBE: " + displayPath(a.target)
	termtitle.Set(title + " — optimising instructions…")
	defer termtitle.Set(title)
	noteIfFree("asking the agent to optimise the %s instructions…", which)

	words := make([]string, len(spec.OneShot))
	for i, w := range spec.OneShot {
		words[i] = shellQuote(w)
	}
	// Somewhere neutral, not the workspace: it has no tools, and needn't
	// pick up the project's own instructions.
	reply, err := sbxrun.ExecInputStdout(ctx, a.name, prompt, asAgent("cd /tmp && exec "+strings.Join(words, " "))...)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("the agent couldn't be asked: %w", err)
	}
	return instructions.ExtractDocument(reply)
}
