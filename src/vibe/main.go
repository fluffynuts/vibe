// Command vibe creates, starts and attaches to docker sbx sandboxes for
// contained agentic coding, driven by profiles bundled alongside the binary.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/homeinit"
	"vibe/internal/kitspec"
	"vibe/internal/layout"
	agentmem "vibe/internal/memory"
	"vibe/internal/pathresolve"
	"vibe/internal/portalloc"
	"vibe/internal/profilegen"
	"vibe/internal/sbxrun"
	"vibe/internal/settings"
	"vibe/internal/state"
)

const usage = `vibe — open (creating if needed) a sandbox for a project folder.

  vibe                       use $PWD, profile derived from its folder name
  vibe /path/to/code         use an explicit folder
  vibe -n/--name custom .    override the derived sandbox name
  vibe -p/--profile foo      use profile "foo" instead of the derived one
  vibe -s/--stop [path]      stop the sandbox for path (default: $PWD)
  vibe -c/--ssh [path]       ssh into the sandbox for path
  vibe -r/--re-init [path]   remove and recreate the sandbox from its profile
  vibe -r -f/--force         ...without prompting
  vibe -f                    ...and, for an unknown profile, create a blank
                             one instead of asking
  vibe -l/--list             list every known sandbox and its status

-s, -c, -r and -l resolve the sandbox name exactly as a normal run would, so
"vibe -s && vibe" restarts whatever you were working on.

When a folder has no profile yet, vibe offers to create one: a copy of an
existing profile, or a blank profile to grow yourself. Profiles live in
profiles/<name> next to the vibe binary.

-c requires 'sbx setup ssh' to have been run once on this machine.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "vibe: %s\n", err)
		os.Exit(1)
	}
}

func note(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "vibe: "+format+"\n", a...)
}

func run(argv []string) error {
	args, err := cliargs.Parse(argv)
	if err != nil {
		return err
	}
	if args.Help {
		fmt.Print(usage)
		return nil
	}
	if args.ExclusiveActions() > 1 {
		return fmt.Errorf("--stop, --ssh, --re-init and --list are mutually exclusive")
	}

	vibeHome := vibeHomeDir()

	if args.List {
		if args.Path != "" {
			return fmt.Errorf("--list takes no path argument")
		}
		return doList()
	}

	if !sbxrun.Available() {
		return fmt.Errorf("'sbx' is not on PATH — vibe needs Docker Sandboxes.\n" +
			"Install it from https://github.com/docker/sbx-releases/releases,\n" +
			"then make sure its bin directory is on PATH (e.g. ${HOME}/.docker/sbx/bin).")
	}

	bundleRoot, err := pathresolve.BundleRoot()
	if err != nil {
		return err
	}
	lay := layout.New(vibeHome, bundleRoot)
	if err := initHome(lay, args.Force); err != nil {
		return err
	}

	target := args.Path
	if target == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		target = wd
	}
	info, statErr := os.Stat(target)
	if statErr != nil || !info.IsDir() {
		return fmt.Errorf("not a directory: %s", target)
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}

	name := args.Name
	if name == "" {
		name = pathresolve.DeriveName(target)
	}

	switch {
	case args.Stop:
		return doStop(name, target)
	case args.Ssh:
		return doSsh(name, target)
	case args.ReInit:
		return doReInit(lay, args, name, target)
	default:
		return doCreateOrAttach(lay, args, name, target)
	}
}

func vibeHomeDir() string {
	if v := os.Getenv("VIBE_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".vibe"
	}
	return filepath.Join(home, ".vibe")
}

// expandHome expands a leading "~" or "~/" in a settings.yaml path value.
func expandHome(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func resolveProfileName(args cliargs.Args, target string) string {
	if args.Profile != "" {
		return args.Profile
	}
	if args.Name != "" {
		return args.Name
	}
	return pathresolve.DeriveName(target)
}

// --- stop / ssh -------------------------------------------------------

func doStop(name, target string) error {
	if !sbxrun.Exists(name) {
		return fmt.Errorf("no sandbox named '%s' for %s — nothing to stop", name, target)
	}
	note("stopping sandbox '%s'", name)
	code, err := sbxrun.Stop(name)
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}

func doSsh(name, target string) error {
	if !sbxrun.Exists(name) {
		return fmt.Errorf("no sandbox named '%s' for %s — run vibe first", name, target)
	}
	host := name + ".sbx"
	if !sbxrun.SshConfigured(host) {
		return fmt.Errorf("no ssh configuration for %s\n"+
			"(checked ~/.ssh/config and everything it Includes, via 'ssh -G')\n"+
			"Run 'sbx setup ssh' once on this machine.", host)
	}
	note("connecting to %s", host)
	code, err := sbxrun.Ssh(host)
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}

// --- list ---------------------------------------------------------------

func doList() error {
	statuses, err := sbxrun.List()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		note("no sandboxes found")
		return nil
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	for _, s := range statuses {
		text := "not running"
		if s.Running {
			text = "running"
		}
		fmt.Printf("%-16s%s\n", s.Name, text)
	}
	return nil
}

// --- kit loading ----------------------------------------------------------

func loadKit(lay layout.Layout, profile string) (kitspec.Doc, settings.Settings, error) {
	if !lay.ProfileExists(profile) {
		return nil, settings.Settings{}, fmt.Errorf(
			"unknown profile '%s' — expected %s/config.yaml", profile, lay.ProfileDir(profile))
	}
	profileDir := lay.ProfileDir(profile)

	baseSettings, err := settings.Load(lay.File("settings.yaml"))
	if err != nil {
		return nil, settings.Settings{}, err
	}
	profileSettings, err := settings.Load(filepath.Join(profileDir, "settings.yaml"))
	if err != nil {
		return nil, settings.Settings{}, err
	}
	merged := settings.Merge(baseSettings, profileSettings)
	merged.NugetDir = expandHome(merged.NugetDir)
	merged.MemoryRoot = expandHome(merged.MemoryRoot)

	baseConfig, err := kitspec.LoadDoc(lay.File("config.yaml"))
	if err != nil {
		return nil, settings.Settings{}, err
	}
	profileConfig, err := kitspec.LoadDoc(filepath.Join(profileDir, "config.yaml"))
	if err != nil {
		return nil, settings.Settings{}, err
	}

	var agentInstructions string
	if data, err := os.ReadFile(filepath.Join(profileDir, "agent-instructions.md")); err == nil {
		agentInstructions = string(data)
	} else if !os.IsNotExist(err) {
		return nil, settings.Settings{}, err
	}

	doc, err := kitspec.Build(baseConfig, profileConfig, lay.DefaultsDir(), profileDir, agentInstructions)
	if err != nil {
		return nil, settings.Settings{}, err
	}
	return doc, merged, nil
}

func writeKit(doc kitspec.Doc, name string) (string, error) {
	dir, err := os.MkdirTemp("", "vibe-kit-"+name+"-")
	if err != nil {
		return "", err
	}
	data, err := kitspec.Marshal(doc)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.yaml"), data, 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// --- publish port allocation ----------------------------------------------

type publishMapping struct {
	name          string
	containerPort int
	hostPort      int
	urlEnv        string
}

// portSearchSpan is how far above a container port allocatePublish will look
// for a free host port before giving up.
const portSearchSpan = 60

// allocatePublish assigns a host port to every container port the merged
// settings publish. The instance records are the only source of truth for who
// holds what: a sandbox re-claims the ports its own record remembers, so its
// URL survives a re-init, and every port another record claims is excluded
// even when nothing is listening on it — that sandbox may simply be stopped.
// Ports handed out earlier in this same pass are excluded on the same
// grounds, since the sandbox that will bind them does not exist yet.
func allocatePublish(vibeHome, name string, merged settings.Settings) ([]publishMapping, error) {
	instances, err := state.List(vibeHome)
	if err != nil {
		return nil, err
	}
	taken := map[int]bool{}
	remembered := map[int]int{}
	for _, inst := range instances {
		for _, rec := range inst.Publish {
			if inst.Name == name {
				remembered[rec.ContainerPort] = rec.HostPort
				continue
			}
			taken[rec.HostPort] = true
		}
	}

	var mappings []publishMapping
	for _, entry := range merged.Publish {
		for i, cp := range entry.Ports {
			hostPort := remembered[cp]
			if hostPort == 0 || taken[hostPort] || portalloc.InUse(hostPort) {
				hostPort, err = portalloc.FindFree(cp, cp+portSearchSpan, taken)
				if err != nil {
					return nil, err
				}
			}
			taken[hostPort] = true
			urlEnv := ""
			if i == 0 {
				urlEnv = entry.UrlEnv
			}
			mappings = append(mappings, publishMapping{name: entry.Name, containerPort: cp, hostPort: hostPort, urlEnv: urlEnv})
		}
	}
	return mappings, nil
}

// --- create / attach --------------------------------------------------

func doCreateOrAttach(lay layout.Layout, args cliargs.Args, name, target string) error {
	vibeHome := lay.Home
	if sbxrun.Exists(name) {
		note("attaching to existing sandbox '%s'", name)
		inst, found, err := state.Load(vibeHome, name)
		if err != nil {
			return err
		}
		if found {
			if !sbxrun.Reachable(name) {
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
		return finish(vibeHome, name)
	}

	profile := resolveProfileName(args, target)
	if err := ensureProfile(lay, profile, args.Force); err != nil {
		return err
	}
	doc, merged, err := loadKit(lay, profile)
	if err != nil {
		return err
	}
	if err := createSandbox(vibeHome, name, target, profile, doc, merged, false, ""); err != nil {
		return err
	}
	return finish(vibeHome, name)
}

func createSandbox(vibeHome, name, target, profile string, doc kitspec.Doc, merged settings.Settings, restoreAfterCreate bool, memoryStore string) error {
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

	note("creating sandbox '%s'", name)
	note("  workspace: %s", target)
	note("  memory:    %s", memSize)

	var mounts []string
	var env []string

	if merged.NugetDir != "" {
		if info, err := os.Stat(merged.NugetDir); err == nil && info.IsDir() {
			mounts = append(mounts, merged.NugetDir)
			env = append(env, "SBX_HOST_NUGET="+merged.NugetDir)
			note("  nuget:     %s (shared with host)", merged.NugetDir)
		} else {
			note("  nuget:     %s not found on host — not mounting", merged.NugetDir)
		}
	}

	if agentmem.Supported(agent) {
		root := merged.MemoryRoot
		if root == "" {
			root = filepath.Join(vibeHome, "memories")
		}
		if memoryStore == "" {
			memoryStore = agentmem.StoreFor(root, name)
		}
		if err := os.MkdirAll(memoryStore, 0o755); err != nil {
			return err
		}
		mounts = append(mounts, memoryStore)
		env = append(env, "SBX_CC_MEMORY_STORE="+memoryStore)
		note("  memories:  %s", memoryStore)
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

	envKeys := make([]string, 0, len(merged.Env))
	for k := range merged.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		env = append(env, k+"="+merged.Env[k])
	}

	if err := sbxrun.Create(sbxrun.CreateOpts{
		Name:    name,
		KitDir:  kitDir,
		Memory:  memSize,
		Publish: publishArgs,
		Env:     env,
		Agent:   agent,
		Target:  target,
		Mounts:  mounts,
	}); err != nil {
		return err
	}

	if restoreAfterCreate {
		if err := agentmem.Restore(name, memoryStore); err != nil {
			note("WARNING: %s", err)
		}
	}

	if err := state.Save(vibeHome, state.Instance{
		Name: name, Profile: profile, Target: target, Publish: publishRecords, CreatedAt: time.Now(),
	}); err != nil {
		return err
	}
	return nil
}

// --- re-init --------------------------------------------------------------

func doReInit(lay layout.Layout, args cliargs.Args, name, target string) error {
	vibeHome := lay.Home
	var profile string
	inst, found, err := state.Load(vibeHome, name)
	if err != nil {
		return err
	}
	switch {
	case args.Profile != "":
		profile = args.Profile
	case found:
		profile = inst.Profile
	default:
		profile = resolveProfileName(args, target)
	}

	if err := ensureProfile(lay, profile, args.Force); err != nil {
		return err
	}
	doc, merged, err := loadKit(lay, profile)
	if err != nil {
		return err
	}
	agent := merged.Agent
	if agent == "" {
		agent = "claude"
	}

	restoreAfterCreate := false
	var memoryStore string

	if sbxrun.Exists(name) {
		if !confirm(args.Force, fmt.Sprintf("Remove sandbox '%s' (workspace %s)?", name, target)) {
			return fmt.Errorf("aborted — sandbox left alone")
		}
		if agentmem.Supported(agent) {
			if confirm(args.Force, fmt.Sprintf("Preserve agent memories from '%s'?", name)) {
				root := merged.MemoryRoot
				if root == "" {
					root = filepath.Join(vibeHome, "memories")
				}
				memoryStore = agentmem.StoreFor(root, name)
				ok, err := agentmem.Backup(name, memoryStore)
				if err != nil {
					note("  WARNING: %s", err)
				} else if !ok {
					note("  no memories found in '%s' — nothing to preserve", name)
				} else {
					note("  memories saved to %s", memoryStore)
					restoreAfterCreate = true
				}
			}
		}
		note("removing sandbox '%s'", name)
		if err := sbxrun.Remove(name, true); err != nil {
			return err
		}
		// The instance record deliberately outlives the sandbox: it carries
		// the published ports this name already owns, which is what lets the
		// rebuild below re-claim them and keep the URL stable. createSandbox
		// overwrites it once the new sandbox is up.
	} else {
		note("no existing sandbox '%s' — nothing to remove", name)
	}

	note("rebuilding '%s' from profile '%s'", name, profile)
	if err := createSandbox(vibeHome, name, target, profile, doc, merged, restoreAfterCreate, memoryStore); err != nil {
		return err
	}
	return finish(vibeHome, name)
}

// ttyReader opens whatever this process can ask a question on: /dev/tty so a
// redirected stdin can't silently answer for the user, falling back (e.g. on
// Windows) to stdin when that is itself a terminal. The returned close
// function must be called once the answer has been read; ok is false when
// there is no terminal at all.
func ttyReader() (reader *bufio.Reader, closeFn func(), ok bool) {
	if tty, err := os.Open("/dev/tty"); err == nil {
		return bufio.NewReader(tty), func() { tty.Close() }, true
	}
	if !isTerminal(os.Stdin) {
		return nil, func() {}, false
	}
	return bufio.NewReader(os.Stdin), func() {}, true
}

// isTerminal reports whether f looks like a terminal we can ask a question
// on. Without a cgo/x-term isatty, a character device is the best signal the
// standard library offers — minus /dev/null, which is a character device
// that answers every read with EOF and is what a daemonised run gets.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(info, null) {
		return false
	}
	return true
}

// confirm asks a yes/no question on the terminal. force answers yes.
func confirm(force bool, prompt string) bool {
	if force {
		return true
	}
	reader, closeFn, ok := ttyReader()
	if !ok {
		note("no terminal to confirm on — re-run with -f")
		os.Exit(1)
	}
	defer closeFn()
	fmt.Fprintf(os.Stderr, "vibe: %s [y/N] ", prompt)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// interactive reports whether there is a terminal to ask questions on.
func interactive() bool {
	_, closeFn, ok := ttyReader()
	closeFn()
	return ok
}

// chooseFrom asks the user to pick one of labels, by number or by its exact
// text. An empty answer takes def (a 0-based index); "q" quits. It returns
// the chosen index, and ok=false when the user quit.
func chooseFrom(question string, labels []string, def int) (int, bool) {
	reader, closeFn, ok := ttyReader()
	if !ok {
		return 0, false
	}
	defer closeFn()
	for {
		note("%s", question)
		for i, label := range labels {
			marker := " "
			if i == def {
				marker = "*"
			}
			fmt.Fprintf(os.Stderr, "     %s %d) %s\n", marker, i+1, label)
		}
		fmt.Fprintf(os.Stderr, "       q) quit\n")
		fmt.Fprintf(os.Stderr, "vibe: choice [%d] ", def+1)
		line, err := reader.ReadString('\n')
		answer := strings.TrimSpace(line)
		switch {
		case answer == "":
			if err != nil { // EOF with nothing typed: don't loop forever
				return 0, false
			}
			return def, true
		case strings.EqualFold(answer, "q"), strings.EqualFold(answer, "quit"):
			return 0, false
		}
		if n, convErr := strconv.Atoi(answer); convErr == nil {
			if n >= 1 && n <= len(labels) {
				return n - 1, true
			}
		}
		for i, label := range labels {
			if strings.EqualFold(answer, label) {
				return i, true
			}
		}
		note("'%s' is not one of the choices", answer)
		if err != nil {
			return 0, false
		}
	}
}

// initHome seeds the user's ~/.vibe overlay on first run: it is where the
// master copy of vibe's configuration is meant to live, so the bundle's base
// config is copied there to be edited, and the bundled profiles are offered
// one at a time for the user to take their own copy of.
func initHome(lay layout.Layout, force bool) error {
	if !homeinit.Needed(lay) {
		return nil
	}
	note("first run — setting up %s", lay.Home)

	var ask func(string) bool
	switch {
	case force || interactive():
		ask = func(profile string) bool {
			return confirm(force, fmt.Sprintf("copy profile '%s' into %s?",
				profile, filepath.Join(lay.Home, "profiles")))
		}
	default:
		note("  no terminal to ask on — leaving the bundled profiles where they are")
	}

	res, err := homeinit.Run(lay, ask)
	if err != nil {
		return err
	}
	if len(res.Files) > 0 {
		note("  copied %s", strings.Join(res.Files, ", "))
	}
	for _, profile := range res.Profiles {
		note("  copied profile '%s'", profile)
	}
	note("  %s now overrides the bundle at %s — edit it, not the bundle", lay.Home, lay.Bundle)
	return nil
}

// ensureProfile makes sure the profile a sandbox is about to be built from
// exists. A folder vibe hasn't seen before derives a profile name that has
// no profile behind it yet, so rather than failing, offer to seed one:
// either a copy of an existing profile or a blank one.
func ensureProfile(lay layout.Layout, profile string, force bool) error {
	if lay.ProfileExists(profile) {
		return nil
	}
	if err := profilegen.Validate(profile); err != nil {
		return err
	}
	existing := lay.Profiles()
	note("no profile '%s' yet in %s", profile, filepath.Dir(lay.NewProfileDir(profile)))

	source := ""
	switch {
	case len(existing) == 0:
		note("there is no profile to copy — creating a blank profile '%s'", profile)
	case force:
		note("--force given — creating a blank profile '%s'", profile)
	default:
		if !interactive() {
			return fmt.Errorf("no terminal to ask on — re-run with -p <profile> to use an existing one (%s), "+
				"with -f to create a blank profile, or write %s by hand",
				strings.Join(existing, ", "),
				filepath.Join(lay.NewProfileDir(profile), "config.yaml"))
		}
		choice, ok := chooseFrom("create it how?",
			[]string{"copy an existing profile", "create a new blank profile"}, 0)
		if !ok {
			return fmt.Errorf("aborted — no profile '%s' created", profile)
		}
		if choice == 0 {
			pick, ok := chooseFrom(fmt.Sprintf("which profile should '%s' start from?", profile), existing, 0)
			if !ok {
				return fmt.Errorf("aborted — no profile '%s' created", profile)
			}
			source = existing[pick]
		}
	}

	var dir string
	var err error
	if source != "" {
		dir, err = profilegen.CopyFrom(lay, source, profile)
	} else {
		dir, err = profilegen.CreateBlank(lay, profile)
	}
	if err != nil {
		return err
	}
	if source != "" {
		note("created profile '%s' as a copy of '%s' in %s", profile, source, dir)
	} else {
		note("created blank profile '%s' in %s", profile, dir)
	}
	note("edit it and re-run with -r to rebuild the sandbox from the changes")
	return nil
}

// --- finishing the foreground session --------------------------------------

// finish nudges the on-start script (idempotent — safe on every attach, and
// needed because setup.startup does not fire on a sandbox's very first boot,
// before the launcher is on disk) then hands off to `sbx run` in the
// foreground.
func finish(vibeHome, name string) error {
	go nudgeOnStart(name)
	code, err := sbxrun.Run(name)
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}

func nudgeOnStart(name string) {
	logPath := filepath.Join(os.TempDir(), "vibe-nudge.log")
	logf := func(format string, a ...interface{}) {
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "[%s] %s: "+format+"\n",
			append([]interface{}{time.Now().Format(time.RFC3339), name}, a...)...)
	}
	if !sbxrun.WaitReachable(name, 180) {
		logf("gave up waiting for reachability")
		return
	}
	if err := sbxrun.ExecDetached(name, "/home/agent/.local/bin/on-start"); err != nil {
		logf("on-start nudge failed: %v", err)
		return
	}
	logf("on-start nudge dispatched")
}

func reportPortHolder(port int) {
	note("  processes holding %d:", port)
	if out, err := exec.Command("ss", "-tlnp", fmt.Sprintf("sport = :%d", port)).CombinedOutput(); err == nil {
		fmt.Fprintln(os.Stderr, string(out))
	}
	if _, err := exec.LookPath("fuser"); err == nil {
		if out, err := exec.Command("fuser", "-v", fmt.Sprintf("%d/tcp", port)).CombinedOutput(); err == nil {
			fmt.Fprintln(os.Stderr, string(out))
		}
	}
}
