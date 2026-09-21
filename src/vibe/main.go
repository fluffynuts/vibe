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
	"strings"
	"time"

	"vibe/internal/cliargs"
	"vibe/internal/fscopy"
	"vibe/internal/homeinit"
	"vibe/internal/kitspec"
	"vibe/internal/layout"
	"vibe/internal/library"
	agentmem "vibe/internal/memory"
	"vibe/internal/pathresolve"
	"vibe/internal/portalloc"
	"vibe/internal/profilegen"
	"vibe/internal/prompt"
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
  vibe -R/--re-create [path] delete the profile too, then re-init — the
                             profile is gone, so this always re-prompts
  vibe -l/--list             list every known sandbox and its status
  vibe -i/--install          copy defaults/profiles/library, config.yaml and
                             settings.yaml into ~/.vibe, and the vibe binary
                             into ~/.local/bin, so the unpacked bundle this
                             was run from can be deleted afterward

-s, -c, -r, -R and -l resolve the sandbox name exactly as a normal run
would, so "vibe -s && vibe" restarts whatever you were working on.

When a folder has no profile yet, vibe offers to create one: a copy of an
existing profile, a blank profile to grow yourself, or a guided walk
through picking features from the library. Profiles live in profiles/<name>
next to the vibe binary. ~/.vibe (or $VIBE_HOME) overrides the bundle: a
single file or a whole profile/feature overrides one at a time, so you can
override just one library feature (say, library/mysql) without affecting
any other, and pick up new ones the bundle adds later.

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
		return fmt.Errorf("--stop, --ssh, --re-init, --re-create, --list and --install are mutually exclusive")
	}

	vibeHome := vibeHomeDir()

	if args.List {
		if args.Path != "" {
			return fmt.Errorf("--list takes no path argument")
		}
		return doList()
	}

	if args.Install {
		if args.Path != "" {
			return fmt.Errorf("--install takes no path argument")
		}
		bundleRoot, err := pathresolve.BundleRoot()
		if err != nil {
			return err
		}
		return doInstall(layout.New(vibeHome, bundleRoot), args.Force)
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
	case args.ReCreate:
		return doReCreate(lay, args, name, target)
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

// --- install ----------------------------------------------------------------

// doInstall makes vibe fully self-contained under lay.Home (~/.vibe or
// $VIBE_HOME) plus a copy of the binary on PATH, so the unpacked bundle it
// was run from — lay.Bundle — can be deleted afterward. It never
// overwrites anything already at the destination, so it's safe to re-run
// (e.g. after fetching a newer bundle release) without losing local edits;
// re-running only fills in what's missing.
func doInstall(lay layout.Layout, force bool) error {
	if lay.Home == "" {
		return fmt.Errorf("no home directory to install into, and $VIBE_HOME is not set")
	}
	if err := os.MkdirAll(lay.Home, 0o755); err != nil {
		return err
	}
	note("installing into %s", lay.Home)

	for _, rel := range []string{"config.yaml", "settings.yaml"} {
		src := lay.BundlePath(rel)
		if _, err := os.Stat(src); err != nil {
			continue // the bundle doesn't ship it — nothing to install
		}
		dst := filepath.Join(lay.Home, rel)
		if _, err := os.Stat(dst); err == nil {
			note("  %s: already present, left alone", rel)
			continue
		}
		if err := fscopy.File(src, dst); err != nil {
			return fmt.Errorf("copying %s: %w", rel, err)
		}
		note("  copied %s", rel)
	}

	for _, rel := range []string{"defaults", "profiles", "library"} {
		src := lay.BundlePath(rel)
		if info, err := os.Stat(src); err != nil || !info.IsDir() {
			continue // the bundle doesn't ship it — nothing to install
		}
		if err := fscopy.TreeMerge(src, filepath.Join(lay.Home, rel)); err != nil {
			return fmt.Errorf("copying %s/: %w", rel, err)
		}
		note("  merged %s/ (any files already there were left alone)", rel)
	}

	return installBinary(force)
}

// installBinary copies the running executable to ~/.local/bin, creating
// that directory if needed, then warns (without failing) if it isn't on
// $PATH — the freshly installed binary would otherwise be invisible to the
// shell with no explanation why.
func installBinary(force bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating vibe executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolving vibe executable path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	binDir := filepath.Join(home, ".local", "bin")
	dest := filepath.Join(binDir, filepath.Base(exe))

	if srcInfo, err := os.Stat(exe); err == nil {
		if dstInfo, err := os.Stat(dest); err == nil && os.SameFile(srcInfo, dstInfo) {
			note("  already installed at %s", dest)
			return warnIfNotOnPath(binDir)
		}
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		if !confirmDefault(force, true, fmt.Sprintf("Overwrite existing %s?", dest)) {
			note("  left %s as-is", dest)
			return warnIfNotOnPath(binDir)
		}
	}
	if err := fscopy.File(exe, dest); err != nil {
		return fmt.Errorf("copying the vibe binary to %s: %w", dest, err)
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return err
	}
	note("  copied the vibe binary to %s", dest)
	return warnIfNotOnPath(binDir)
}

// warnIfNotOnPath reports whether dir is on $PATH, warning (never failing)
// when it isn't.
func warnIfNotOnPath(dir string) error {
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(entry) == dir {
			return nil
		}
	}
	note("  WARNING: %s is not on your PATH", dir)
	note("  add it, e.g. in ~/.bashrc or ~/.zshrc:  export PATH=\"%s:$PATH\"", dir)
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
		env = append(env, "VIBE_MEMORY_STORE="+memoryStore)
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

// resolveReInitProfile returns the profile a re-init (or re-create) should
// use: the one explicitly given, else the one already recorded for an
// existing sandbox of this name, else the one a plain run would derive.
func resolveReInitProfile(vibeHome string, args cliargs.Args, name, target string) (string, error) {
	if args.Profile != "" {
		return args.Profile, nil
	}
	inst, found, err := state.Load(vibeHome, name)
	if err != nil {
		return "", err
	}
	if found {
		return inst.Profile, nil
	}
	return resolveProfileName(args, target), nil
}

func doReInit(lay layout.Layout, args cliargs.Args, name, target string) error {
	return reInit(lay, args, name, target, false)
}

// reInit is --re-init's implementation. skipSandboxConfirm is set by
// doReCreate, which already got one confirmation up front covering both
// the profile and the sandbox, so this must not ask about the sandbox a
// second time.
func reInit(lay layout.Layout, args cliargs.Args, name, target string, skipSandboxConfirm bool) error {
	vibeHome := lay.Home
	profile, err := resolveReInitProfile(vibeHome, args, name, target)
	if err != nil {
		return err
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
		if !skipSandboxConfirm && !confirmDefault(args.Force, true, fmt.Sprintf("Remove sandbox '%s' (workspace %s)?", name, target)) {
			return fmt.Errorf("aborted — sandbox left alone")
		}
		if agentmem.Supported(agent) {
			switch {
			case !agentmem.HasMemories(name):
				note("  no memories found in '%s' — nothing to preserve", name)
			case confirmDefault(args.Force, true, fmt.Sprintf("Preserve agent memories from '%s'?", name)):
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

// --- re-create --------------------------------------------------------------

// doReCreate deletes the overlay copy of the matched profile (if there is
// one — the bundle's own copy, if any, is never touched), then does
// exactly what --re-init does: with the profile gone, ensureProfile guides
// the user through creating a new one (copy, blank or guided) instead of
// silently reusing the old one, and any existing sandbox is removed and
// rebuilt from it. Both destructive steps share a single confirmation up
// front rather than asking once per step.
func doReCreate(lay layout.Layout, args cliargs.Args, name, target string) error {
	profile, err := resolveReInitProfile(lay.Home, args, name, target)
	if err != nil {
		return err
	}

	if !confirmDefault(args.Force, false,
		"This action will destroy any existing profile or sandbox for the current project. Continue?") {
		return fmt.Errorf("aborted — nothing removed")
	}

	dir := lay.HomePath("profiles", profile)
	if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		note("deleted profile '%s' from %s", profile, dir)
	} else {
		note("no overlay profile '%s' to delete", profile)
	}

	return reInit(lay, args, name, target, true)
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

// confirm asks a yes/no question on the terminal, defaulting to no. force
// answers yes without asking.
func confirm(force bool, question string) bool {
	return confirmDefault(force, false, question)
}

// confirmDefault asks a yes/no question on the terminal, answering def when
// the user just presses enter. force answers yes without asking.
func confirmDefault(force, def bool, question string) bool {
	if force {
		return true
	}
	reader, closeFn, ok := ttyReader()
	if !ok {
		note("no terminal to confirm on — re-run with -f")
		os.Exit(1)
	}
	defer closeFn()
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	fmt.Fprintf(os.Stderr, "vibe: %s [%s] ", question, hint)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	switch line {
	case "":
		return def
	case "y", "yes":
		return true
	default:
		return false
	}
}

// interactive reports whether there is a terminal to ask questions on.
func interactive() bool {
	_, closeFn, ok := ttyReader()
	closeFn()
	return ok
}

// ttyRW opens the same terminal ttyReader would, but for reading and writing
// raw bytes on one *os.File — what an interactive list picker needs to put
// the terminal into raw mode and redraw itself in place.
func ttyRW() (rw *os.File, closeFn func(), ok bool) {
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		return tty, func() { tty.Close() }, true
	}
	if !isTerminal(os.Stdin) {
		return nil, func() {}, false
	}
	return os.Stdin, func() {}, true
}

// chooseFrom asks the user to pick one of labels with an interactive,
// arrow-key-navigable list (inquirer-style). def is the 0-based index
// highlighted first. It returns the chosen index, and ok=false when the
// user quit or there is no terminal to ask on.
//
// Falling back to numbered/typed input when Select can't put the terminal
// into raw mode is deliberately not attempted: interactive() already gates
// every chooseFrom call site on there being a real terminal, and a terminal
// that can't go raw is rare enough (and unhelpful enough for scripting) not
// to be worth a second input mode.
func chooseFrom(question string, labels []string, def int) (int, bool) {
	tty, closeFn, ok := ttyRW()
	if !ok {
		return 0, false
	}
	defer closeFn()
	note("%s", question)
	return prompt.Select(tty, labels, def)
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
// no profile behind it yet, so rather than failing, offer to seed one: a
// copy of an existing profile, a blank one, or a guided walk through
// picking library features.
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
			[]string{"copy an existing profile", "create a new blank profile", "guided profile creation"}, 0)
		if !ok {
			return fmt.Errorf("aborted — no profile '%s' created", profile)
		}
		switch choice {
		case 0:
			pick, ok := chooseFrom(fmt.Sprintf("which profile should '%s' start from?", profile), existing, 0)
			if !ok {
				return fmt.Errorf("aborted — no profile '%s' created", profile)
			}
			source = existing[pick]
		case 2:
			return ensureGuidedProfile(lay, profile)
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
	return nil
}

// ensureGuidedProfile walks the user through picking which library
// features a new profile should install, lets them reorder the ones they
// picked, and writes the result as a new profile via profilegen.CreateGuided.
func ensureGuidedProfile(lay layout.Layout, profile string) error {
	names := lay.Features()
	if len(names) == 0 {
		return fmt.Errorf("no library features found — pick another option instead")
	}
	settingsPath := lay.File("settings.yaml")
	base, err := settings.Load(settingsPath)
	if err != nil {
		return err
	}
	// A defaultFeatures entry naming a feature that isn't there is a
	// mistake worth stopping for: carrying on would quietly build a
	// sandbox without the tooling it was told to always start with.
	if err := library.Validate(base.DefaultFeatures, names); err != nil {
		return fmt.Errorf("defaultFeatures in %s: %w", settingsPath, err)
	}

	features := library.List(names, lay.FeatureDir)
	labels := make([]string, len(features))
	checked := make([]bool, len(features))
	wanted := make(map[string]bool, len(base.DefaultFeatures))
	for _, name := range base.DefaultFeatures {
		wanted[name] = true
	}
	for i, f := range features {
		labels[i] = f.Label()
		checked[i] = wanted[f.Name]
	}
	if len(base.DefaultFeatures) > 0 {
		note("ticked from defaultFeatures (untick any you don't want): %s", strings.Join(base.DefaultFeatures, ", "))
	}

	idxs, ok := checklistFrom("pick the features this sandbox needs", labels, checked)
	if !ok {
		return fmt.Errorf("aborted — no profile '%s' created", profile)
	}
	chosen := make([]string, len(idxs))
	for i, idx := range idxs {
		chosen[i] = features[idx].Name
	}

	if len(chosen) > 1 {
		ordered, ok := reorderFrom("order the features — they install and start in this order", chosen)
		if !ok {
			return fmt.Errorf("aborted — no profile '%s' created", profile)
		}
		chosen = ordered
	}

	dir, err := profilegen.CreateGuided(lay, profile, chosen)
	if err != nil {
		return err
	}
	if len(chosen) > 0 {
		note("created guided profile '%s' (%s) in %s", profile, strings.Join(chosen, ", "), dir)
	} else {
		note("created guided profile '%s' (no features picked) in %s", profile, dir)
	}
	return nil
}

// checklistFrom asks the user to check zero or more of labels with an
// interactive checkbox list. checked is the initial checked state. It
// returns the checked indices, and ok=false when the user quit or there is
// no terminal to ask on.
func checklistFrom(question string, labels []string, checked []bool) ([]int, bool) {
	tty, closeFn, ok := ttyRW()
	if !ok {
		return nil, false
	}
	defer closeFn()
	note("%s", question)
	return prompt.MultiSelect(tty, labels, checked)
}

// reorderFrom asks the user to rearrange labels with an interactive,
// arrow-key-movable list. It returns the labels in their final order, and
// ok=false when the user quit or there is no terminal to ask on.
func reorderFrom(question string, labels []string) ([]string, bool) {
	tty, closeFn, ok := ttyRW()
	if !ok {
		return nil, false
	}
	defer closeFn()
	note("%s", question)
	return prompt.Reorder(tty, labels)
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
