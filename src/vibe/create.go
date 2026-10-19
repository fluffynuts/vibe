package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"vibe/internal/claudetheme"
	"vibe/internal/cliargs"
	"vibe/internal/gittoken"
	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/library"
	agentmem "vibe/internal/memory"
	"vibe/internal/pathresolve"
	"vibe/internal/portalloc"
	"vibe/internal/profilegen"
	"vibe/internal/sbxrun"
	"vibe/internal/settings"
	"vibe/internal/state"
	"vibe/internal/timezone"
)

func doCreateOrAttach(lay layout.Layout, args cliargs.Args, name, target string) error {
	vibeHome := lay.Home
	exists, err := sbxrun.Exists(name)
	if err != nil {
		return err
	}
	if exists {
		if args.NoStart {
			note("sandbox '%s' already exists — nothing to set up", name)
			return finish(lay, args, name)
		}
		if err := autoUpdate(lay, args, name, target); err != nil {
			return err
		}
		if args.Headless {
			note("starting existing sandbox '%s', with no terminal attached", name)
		} else {
			note("attaching to existing sandbox '%s'", name)
		}
		if args.GitTokenEnv != "" || args.NoGitToken || args.GitRules != "" {
			note("  (it exists, so --git-token-env, --no-git-token and --git-rules don't apply — 'vibe --git-token' changes its token)")
		}
		inst, found, err := state.Load(vibeHome, name)
		if err != nil {
			return err
		}
		if found {
			if !sbxrun.Reachable(context.Background(), name) {
				for _, rec := range inst.Publish {
					if portalloc.InUse(rec.HostPort) {
						reportPortHolder(rec.HostPort)
						return fmt.Errorf("host port %d is in use, and sandbox '%s' is stopped.\n"+
							"sbx would prompt for an alternative port and lose your stable URL.\n"+
							"free the port, then re-run", rec.HostPort, name)
					}
				}
			}
			for _, rec := range inst.Publish {
				note("  %s: http://localhost:%d", rec.Name, rec.HostPort)
			}
		}
		return finish(lay, args, name)
	}

	if args.Headless {
		return noSandboxForHeadless(name, target)
	}
	profile := resolveProfileName(args, target)
	// The agent first: which features a new profile is offered depends on it.
	defaultAgent, err := defaultAgentFor(lay, profile)
	if err != nil {
		return err
	}
	agent := pickAgent(defaultAgent, !args.Force && interactive(), fmt.Sprintf("which agent should sandbox '%s' run?", name))
	if err := ensureProfile(lay, profile, args.Force, agent); err != nil {
		return err
	}
	settleFeatures(lay, profile)
	noteMissingBase(lay, profile)
	doc, merged, err := loadKit(lay, profile)
	if err != nil {
		return err
	}
	merged.Agent = agent
	warnUnsupportedFeatures(lay, profile, agent)
	if agentOf(merged) == "claude" {
		if doc, err = setClaudeTheme(lay, profile, doc, "", true, args.Force); err != nil {
			return err
		}
	} else {
		kitspec.DropClaudeFiles(doc)
	}
	git, err := pickGitAccess(vibeHome, args, name, target)
	if err != nil {
		return err
	}
	if err := createSandbox(context.Background(), lay, name, target, profile, doc, merged, !args.Force, timezone.Host(), !args.Force, false, "", git, !args.Force); err != nil {
		return err
	}
	return finish(lay, args, name)
}

// defaultAgentFor is the agent a new sandbox on profile is offered first:
// the profile's own setting when the profile exists, else the user's. A
// profile not made yet isn't read from, so a bad name reads nothing.
func defaultAgentFor(lay layout.Layout, profile string) (string, error) {
	if lay.ProfileExists(profile) {
		merged, err := loadSettings(lay, profile)
		return agentOf(merged), err
	}
	user, err := settings.Load(lay.File("settings.yaml"))
	return agentOf(user), err
}

// warnUnsupportedFeatures warns about features the profile was composed
// from that don't work with agent. The profile is used as it is — it may be
// shared with sandboxes running another agent — but the user should know
// what this sandbox has that its agent can't use.
func warnUnsupportedFeatures(lay layout.Layout, profile, agent string) {
	dir := lay.ProfileDir(profile)
	for _, f := range library.Unsupported(profilegen.ComposedFrom(dir), lay.FeatureDir, agent) {
		note("WARNING: profile '%s' has %s", profile, whyUnsupported(f, agent))
		note("  it is installed anyway; 'vibe --re-compose' can take it out of the profile")
	}
}

func agentOf(merged settings.Settings) string {
	if merged.Agent == "" {
		return "claude"
	}
	return merged.Agent
}

// setClaudeTheme settles which Claude theme a sandbox about to be created
// starts with, sets it in doc, and records it in the profile's directory.
//
// The preference comes from the sandbox being replaced (fromSandbox, read
// before it was removed — the truest answer, since /theme may have changed
// it since), else from what the profile last recorded. ask says to ask
// anyway, starting on that preference: a brand new sandbox always asks,
// while a rebuild asks only when nothing is known. -f, or no terminal,
// takes the preference (or the default) without asking.
func setClaudeTheme(lay layout.Layout, profile string, doc kitspec.Doc, fromSandbox string, ask, force bool) (kitspec.Doc, error) {
	// ~/.vibe/defaults (or, without one, the bundle's) is the source of
	// truth for which themes are offered.
	known := claudetheme.Available(lay.DefaultsDir())
	profileDir := lay.ProfileDir(profile)

	preferred, source := fromSandbox, "the sandbox being replaced"
	if preferred == "" {
		if c, ok := claudetheme.FromProfile(profileDir); ok {
			preferred, source = c, "profile '"+profile+"'"
		}
	}
	choice := claudetheme.Default
	if preferred != "" {
		resolved, ok := claudetheme.Resolve(preferred, known)
		if !ok {
			note("  claude theme '%s' (from %s) isn't among the themes in %s — using the default",
				preferred, source, pathresolve.Tilde(filepath.Join(lay.DefaultsDir(), "agent-files", ".claude", "themes")))
		}
		choice = resolved
	}

	choices := claudetheme.Choices(known)
	if slices.Index(choices, choice) < 0 {
		// A built-in theme the sandbox was switched to: offer to keep it.
		choices = append([]string{choices[0], choice}, choices[1:]...)
	}
	switch {
	case (ask || preferred == "") && len(choices) > 1 && !force && interactive():
		picked, ok := chooseFrom("select claude theme", choices, slices.Index(choices, choice))
		if !ok {
			return nil, fmt.Errorf("aborted — no theme chosen")
		}
		choice = choices[picked]
	case preferred != "":
		note("  claude theme: %s (from %s)", choice, source)
	}

	doc, err := claudetheme.Apply(doc, choice)
	if err != nil {
		return nil, err
	}
	if err := claudetheme.SaveToProfile(profileDir, choice); err != nil {
		note("  WARNING: could not record the claude theme in %s: %s", profileDir, err)
	}
	return doc, nil
}

// askResources offers a choice of how much memory and how many CPUs the
// sandbox gets, starting on the memory and cpus settings; without it, those
// settings are used as long as this machine can give them. git, when not nil, is the GitHub token the agent may
// push with. sbxMayAsk lets sbx ask on the terminal; without it, a question
// from sbx fails the create, for when nobody is there to answer.
func createSandbox(ctx context.Context, lay layout.Layout, name, target, profile string, doc kitspec.Doc, merged settings.Settings, askResources bool, zone string, askZone, restoreAfterCreate bool, memoryStore string, git *gitAccess, sbxMayAsk bool) error {
	vibeHome := lay.Home
	// Every sandbox's agent is told where to look for its rules for
	// pushing, since a token can be given to a sandbox after it's made.
	kitspec.AppendAgentInstructions(doc, gittoken.KitInstructions)
	kitDir, err := writeKit(doc, name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(kitDir)

	agent := merged.Agent
	if agent == "" {
		agent = "claude"
	}
	memSize := merged.Memory
	if memSize == "" {
		memSize = "12g"
	}
	memSize = pickMemory(memSize, askResources && interactive(), fmt.Sprintf("how much memory should sandbox '%s' get?", name))
	cpus := pickCPUs(merged.CPUs, askResources && interactive(), fmt.Sprintf("how many CPUs should sandbox '%s' get?", name))
	zone = pickTimezone(zone, askZone && interactive(), fmt.Sprintf("which timezone should sandbox '%s' keep its time in?", name))

	note("creating sandbox '%s'", name)
	note("  workspace: %s", target)
	note("  memory:    %s", memSize)
	note("  cpus:      %d", cpus)
	note("  timezone:  %s", zone)

	if agentmem.Supported(agent) && memoryStore == "" {
		memoryStore = agentmem.StoreFor(vibeHome, merged.MemoryRoot, name)
	}
	if merged.NugetDir != "" {
		if info, err := os.Stat(merged.NugetDir); err != nil || !info.IsDir() {
			note("  nuget:     %s not found on host — not mounting", merged.NugetDir)
		}
	}
	var mounts, env []string
	for _, f := range sandboxFolders(vibeHome, name, target, merged, agent, memoryStore) {
		if f.made {
			if err := os.MkdirAll(f.path, 0o755); err != nil {
				return err
			}
		}
		if f.note != "" {
			note("  %s", f.note)
		}
		mounts = append(mounts, f.mount())
		env = append(env, f.env...)
	}

	publishMappings, err := allocatePublish(vibeHome, name, merged)
	if err != nil {
		return err
	}

	var publishArgs []string
	var publishRecords []state.PublishRecord
	for _, m := range publishMappings {
		publishArgs = append(publishArgs, fmt.Sprintf("%d:%d", m.hostPort, m.containerPort))
		publishRecords = append(publishRecords, state.PublishRecord{Name: m.name, ContainerPort: m.containerPort, HostPort: m.hostPort})
		if m.urlEnv != "" {
			env = append(env, fmt.Sprintf("%s=http://localhost:%d", m.urlEnv, m.hostPort))
		}
		note("  %s: http://localhost:%d", m.name, m.hostPort)
	}

	// Claude Code retitles the terminal after the conversation; that would
	// replace vibe's title, which says which project the tab is for. Before
	// the profile's env, which can turn it back on.
	if agent == "claude" {
		env = append(env, "CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1")
	}

	envKeys := make([]string, 0, len(merged.Env))
	for k := range merged.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		env = append(env, k+"="+merged.Env[k])
	}

	if err := sbxrun.Create(ctx, sbxrun.CreateOpts{
		Name:    name,
		KitDir:  kitDir,
		Memory:  memSize,
		CPUs:    cpus,
		Publish: publishArgs,
		Env:     env,
		Agent:   agent,
		Target:  target,
		Mounts:  mounts,
		NoInput: !sbxMayAsk,
		Output:  actionOutput(),
	}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		warnIfClockIsOff()
		return err
	}

	// Before the agent starts, so it starts on the sandbox's local time;
	// on-start puts it back at every later session.
	if err := timezone.Apply(name, zone); err != nil {
		note("WARNING: could not set the sandbox's timezone: %s", err)
	}

	if restoreAfterCreate {
		if err := agentmem.Restore(name, memoryStore); err != nil {
			note("WARNING: %s", err)
		}
	}

	inst := state.Instance{
		Name: name, Profile: profile, Target: target, Publish: publishRecords, MemoryStore: memoryStore, Timezone: zone, Agent: agent, CreatedAt: time.Now(),
	}
	// A rebuild keeps the page on the port it had, as it keeps the published
	// ports: its address (a bookmark, another machine's ssh forwards) stays.
	if was, found, err := state.Load(vibeHome, name); err == nil && found {
		inst.CompanionPort = was.CompanionPort
	}
	if inst.KitHash, err = kitFingerprint(lay, inst); err != nil {
		note("WARNING: couldn't record what sandbox '%s' was built from, so it will be taken as out of date: %s", name, err)
	}
	if git = giveGitAccess(vibeHome, name, git, repoOf(target)); git != nil {
		inst.GitToken, inst.GitRules, inst.GitTokenExpires = git.source, git.rules, git.expires
	}
	if err := state.Save(vibeHome, inst); err != nil {
		return err
	}
	return nil
}
