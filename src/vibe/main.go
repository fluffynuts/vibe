// Command vibe creates, starts and attaches to docker sbx sandboxes for
// contained agentic coding, driven by profiles bundled alongside the binary.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"vibe"
	"vibe/internal/cliargs"
	"vibe/internal/fscopy"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/prompt"
	"vibe/internal/review"
	"vibe/internal/sbxfind"
	"vibe/internal/sbxinstall"
	"vibe/internal/sbxrun"
	"vibe/internal/termtitle"
)

const usage = `vibe — open (creating if needed) a sandbox for a project folder.

  vibe                       use $PWD, profile derived from its folder name
  vibe /path/to/code         use an explicit folder
  vibe -n/--name custom .    override the derived sandbox name
  vibe -p/--profile foo      use profile "foo" instead of the derived one
  vibe -s/--stop [path]      end any vibe attached to the sandbox for path
                             (default: $PWD), saving its agent's memories,
                             and stop the sandbox; with -f, no sandbox there
                             isn't an error
  vibe --cancel [path]       cancel what the companion page for path has under
                             way (the agent's turn, an Admin action), leaving
                             the sandbox running
  vibe -S/--stop-all         stop every running sandbox, and every sleeping
                             one a vibe is attached to, in parallel, ending
                             those vibes first (asks first; -f stops them
                             without asking)
  vibe -c/--ssh [path]       ssh into the sandbox for path
  vibe -r/--re-init [path]   remove and recreate the sandbox from its profile
  vibe -r -f/--force         ...without prompting
  vibe -f                    ...and, for an unknown profile, create a blank
                             one instead of asking
  vibe -R/--re-create [path] delete the profile too, then re-init — the
                             profile is gone, so this always re-prompts
  vibe -C/--re-compose [path] rebuild a guided profile from the library
                             features it was composed from (pick which to
                             keep, add or drop), picking up whatever they
                             have gained since, then re-init
  vibe --restore [path]      pick one of the last three backups of the
                             profile (taken before each re-compose) to roll
                             back to, then offer to re-compose from it;
                             -f restores the newest, unasked
  vibe --migrate FROM TO     move folder FROM to TO (which mustn't exist
                             yet), its sandbox with it: rebuilt there, under
                             the name TO gives it, keeping its agent's
                             memories, conversation, GitHub token and
                             clipboard history; asks first (-f doesn't)
  vibe -l/--list             list every sandbox: state, owner, name
  vibe -a/--info [path]      show the sandbox's settings (memory, agent,
                             features), its companion page and how to
                             reach it from another machine, and, while it
                             runs, its uptime and how much memory and disk
                             it is using
  vibe -H/--headless [path]  start the sandbox for path (one that exists:
                             run vibe there once first) and serve its
                             companion page, with no terminal attached, from
                             a vibe in the background: prints the page's
                             address and the log's (~/.vibe/logs), and gives
                             the terminal back; run again, it just points at
                             the one running. 'vibe --stop' ends it and stops
                             the sandbox
  vibe --headless --foreground
                             ...staying in the foreground (for tmux, nohup or
                             a systemd unit) until Ctrl-C, which leaves the
                             sandbox running
  vibe --restart [path]      end the vibe already running for path (with a
                             terminal or headless, saving its agent's
                             memories), leaving its sandbox running, and take
                             over, in the mode the one it ended was in: to
                             pick up a new vibe. Asks first when that would
                             cut off an agent's turn or another terminal's
                             session (-f: doesn't)
  vibe --restart --terminal  ...taking over with a terminal (to get one on a
                             headless sandbox); --headless takes over
                             headless the same way
  vibe --terminal [path]     start with a terminal attached, which is what
                             plain vibe does unless 'mode: headless' is in
                             settings.yaml (a profile's, or ~/.vibe's)
  vibe -r/-R/-C --headless   rebuild (or re-create, or re-compose), then go
                             headless, instead of attaching a terminal
  vibe -M/--manage           serve the Profiles page on its own, until Ctrl-C:
                             every profile and its sandboxes, to clean up as
                             --cleanup does (every companion page has it as
                             a tab too)
  vibe -x/--cleanup          pick profiles and sandboxes to delete from a
                             checklist grouped by profile, with what's likely
                             trash selected; a profile goes with all its
                             sandboxes and everything vibe keeps for them.
                             Lists exactly what will go, and asks once
  vibe -d/--delete [path]    pick from a checklist whether to delete the
                             sandbox for path (selected), its profile
                             (deselected), or both; -f deletes both unasked
  vibe -U/--upgrade          check GitHub for newer releases of vibe and
                             Docker SBX, pick which to upgrade (all are
                             selected), and install them; -f upgrades them all
                             without asking (-f and -u are passed on to
                             vibe's --install)
  vibe -I/--install-sbx      download the latest stable Docker Sandboxes (sbx)
                             release and install it: on Linux into
                             ~/.docker/sbx; on macOS (Apple Silicon) and
                             Windows, asking how (-f takes the first way,
                             and reinstalls when already up to date)
  vibe --update-sandboxes    bring every profile and sandbox up to date with
                             the library: re-compose the profiles it has
                             moved on from (unless edited by hand), and
                             rebuild the sandboxes built from what has since
                             changed, leaving each running or stopped as it
                             was. Sandboxes with a vibe attached are left
                             for their next session. Lists what it will do
                             and asks first; -f goes ahead unasked. Starts
                             no session, so with -f it suits cron:
                             vibe --upgrade -f && vibe --update-sandboxes -f
  vibe --no-auto-update      ...start the sandbox as it is, even when it's
                             out of date: by default, starting a session
                             brings its profile and sandbox up to date
                             first ("autoUpdate: false" in settings.yaml
                             turns that off)
  vibe -N/--no-companion     ...without serving the companion page this run
  vibe --no-start            ...doing all the setup (create, -r, -R, -C...) but
                             stopping short of starting the sandbox and
                             companion; prints "type vibe to get started"
                             unless -f is given (for scripts and cron)
  vibe --setup               go through the questions --install asks again
                             (default memory, agent and guided-profile
                             features), saving the answers in
                             ~/.vibe/settings.yaml, and offer to store a
                             default GitHub token for sandboxes to push with
  vibe --git-token [path]    give the sandbox for path a GitHub token to
                             push with, renew or change it, or take it away
  vibe --git-token-env VAR   ...a new sandbox (or --git-token) the token in
                             $VAR, unasked
  vibe --git-rules R[,R]     ...and these rules for pushing, unasked:
                             feature-branch, no-default-branch
  vibe --no-git-token        ...no token for a new sandbox, unasked; with
                             --git-token, take the sandbox's away
  vibe -v/--version          print the version and the commit it was built from
  vibe -i/--install          copy defaults/profiles/library, config.yaml and
                             settings.yaml into ~/.vibe, and the vibe binary
                             into ~/.local/bin, so the unpacked bundle this
                             was run from can be deleted afterward. Run from
                             a newer release, it upgrades: new files are
                             copied, files you never edited are updated, and
                             ones changed on both sides are merged or asked
                             about
  vibe -i -u/--update-strategy S
                             settle files changed on both sides without
                             asking: keep, update, merge,keep or merge,update

Every run that opens a sandbox also serves a companion page on this machine
(localhost, only): tabs for a conversation with the agent (with the sandbox's
clipboard page beside it), network log, network rules and filesystem rules, so none of that needs the sbx TUI. Its URL is
printed, and it opens in your browser. -N/--no-companion skips it for one run;
"companion: open|serve|off" in settings.yaml sets the default (serve: print
the URL, don't open it).

An agent in a vibe sandbox has no git credentials unless you give it some,
so you commit and push — and read the code — yourself. To let it push while
it works through a plan, a new sandbox asks whether to give it a GitHub token
(no is the default): the default one --setup stored, or one just for the
folder's repository, which vibe opens GitHub's token page to make. The token
is checked with GitHub, handed to sbx as the sandbox's own secret (it never
enters the sandbox), and the agent is told never to force-push, plus any
rules you pick (only work in a feature branch; never push the default
branch).

-s, -c, -a, -r, -R, -C, -d and --git-token resolve the sandbox name exactly as a normal run
would, so "vibe -s && vibe" restarts whatever you were working on. -l and -x
work on every sandbox at once and take no path.

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
	// Started as the helper reading a repository (see review.isolated), it
	// does only that.
	review.ServeHelperIfAsked()
	prompt.EnableVT(os.Stdout)
	prompt.EnableVT(os.Stderr)
	sbxfind.Warn = func(msg string) { note("%s", msg) }
	fscopy.OnLongWait = func(path string) {
		note("waiting for %s: another program (a virus scanner, say) has it open", pathresolve.Tilde(path))
	}
	err := run(os.Args[1:])
	termtitle.Restore()
	session.Release()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vibe: %s\n", err)
		os.Exit(1)
	}
}

func note(format string, a ...interface{}) {
	line := fmt.Sprintf(format, a...)
	noteAs(line, line)
}

// noteAs is note for a line that is shown differently from what is passed on
// (to the page's log, or held until the session ends): line is the plain text,
// shown is what the terminal gets, with its styling (see askText).
func noteAs(line, shown string) {
	teeNote(line)
	if holdNote(line) {
		return
	}
	fmt.Fprintf(os.Stderr, "vibe: %s\n", shown)
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
	if args.Version {
		fmt.Println(vibe.String())
		return nil
	}
	if args.UpdateStrategy != "" && !args.Install && !args.Upgrade {
		return fmt.Errorf("--update-strategy only applies to --install and --upgrade")
	}
	if args.ExclusiveActions() > 1 {
		return fmt.Errorf("--migrate, --stop, --stop-all, --ssh, --re-init, --re-create, --re-compose, --restore, --list, --install, --upgrade, --install-sbx, --cleanup, --delete, --info, --setup, --git-token, --update-sandboxes, --manage and --headless are mutually exclusive (--headless goes with --re-init, --re-create and --re-compose alone: the rebuild, then headless)")
	}

	if args.Terminal && args.ExclusiveActions() > 0 && !args.Rebuilds() {
		return fmt.Errorf("--terminal only goes with starting vibe: on its own, with --restart, or with a rebuild (--re-init, --re-create, --re-compose)")
	}

	if (args.GitTokenEnv != "" || args.NoGitToken || args.GitRules != "") && args.ExclusiveActions() > 0 && !args.GitToken {
		return fmt.Errorf("--git-token-env, --no-git-token and --git-rules only apply to a new sandbox, or with --git-token")
	}

	vibeHome := vibeHomeDir()

	if args.List {
		if args.Path != "" {
			return fmt.Errorf("--list takes no path argument")
		}
		return doList(vibeHome)
	}

	if args.Upgrade {
		if args.Path != "" {
			return fmt.Errorf("--upgrade takes no path argument")
		}
		return doUpgrade(args.Force, args.UpdateStrategy)
	}

	if args.InstallSbx {
		if args.Path != "" {
			return fmt.Errorf("--install-sbx takes no path argument")
		}
		return doInstallSbx(args.Force)
	}

	if args.Setup {
		if args.Path != "" {
			return fmt.Errorf("--setup takes no path argument")
		}
		return doSetup(vibeHome, args.Force)
	}

	if args.Install {
		if args.Path != "" {
			return fmt.Errorf("--install takes no path argument")
		}
		bundleRoot, err := pathresolve.BundleRoot()
		if err != nil {
			return err
		}
		lay := layout.New(vibeHome, bundleRoot)
		if err := doInstall(lay, args.Force, args.UpdateStrategy); err != nil {
			return err
		}
		ensureWindowsFeatures(lay, args.Force)
		checkKVM()
		if err := offerSbxInstall(args.Force); err != nil {
			return err
		}
		reportOutOfDate(lay)
		return nil
	}

	if !sbxrun.Available() {
		hint := "Install it from " + sbxinstall.Releases + ",\n"
		if sbxinstall.Supported(runtime.GOOS, runtime.GOARCH) {
			hint = "Install it with 'vibe --install-sbx', or from " + sbxinstall.Releases + ",\n"
		}
		return fmt.Errorf("sbx wasn't found on PATH, or where it's usually installed — vibe needs Docker Sandboxes.\n%s"+
			"then make sure its bin directory is on PATH (e.g. ${HOME}/.docker/sbx/bin).", hint)
	}
	sbxfind.Look()

	// Like cleanup, this is about sandboxes sbx holds, not a folder.
	if args.StopAll {
		if args.Path != "" {
			return fmt.Errorf("--stop-all takes no path argument")
		}
		return doStopAll(vibeHome, args.Force)
	}

	bundleRoot, err := pathresolve.BundleRoot()
	if err != nil {
		return err
	}
	lay := layout.New(vibeHome, bundleRoot)
	if err := initHome(lay, args.Force); err != nil {
		return err
	}

	if args.Cleanup {
		if args.Path != "" {
			return fmt.Errorf("--cleanup takes no path argument")
		}
		return doCleanup(lay)
	}

	if args.Manage {
		if args.Path != "" {
			return fmt.Errorf("--manage takes no path argument")
		}
		return doManage(lay)
	}

	if args.UpdateSandboxes {
		if args.Path != "" {
			return fmt.Errorf("--update-sandboxes takes no path argument")
		}
		if err := vmCheck(); err != nil {
			return err
		}
		return doUpdateSandboxes(lay, args.Force)
	}

	// Windows PowerShell hands "~\project" over as it is, and vibe shows
	// folders that way itself, so the "~" is vibe's to expand.
	target := pathresolve.ExpandHome(args.Path)
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

	if startsAVM(args) {
		if err := vmCheck(); err != nil {
			return err
		}
	}
	switch {
	case args.Migrate:
		return doMigrate(lay, args, name, target, pathresolve.ExpandHome(args.MigrateTo))
	case args.Stop:
		return doStop(vibeHome, name, target, args.Force)
	case args.Cancel:
		return doCancel(vibeHome, target)
	case args.Ssh:
		return doSsh(name, target)
	case args.Info:
		return doInfo(lay, args, name, target)
	case args.GitToken:
		return doGitToken(vibeHome, args, name, target)
	case args.Restore:
		again, err := doRestore(lay, args, name, target)
		if err != nil || !again {
			return err
		}
		// On from here as `--restart --re-compose`: what is running here is
		// ended first, and what starts after takes its mode.
		args.ReCompose, args.Restart = true, true
	}

	// Before anything is ended or asked: a re-compose that can't be done says
	// so first, and doesn't stop a vibe or a sandbox on the way to saying it.
	if args.ReCompose {
		profile, err := resolveReInitProfile(lay.Home, args, name, target)
		if err != nil {
			return err
		}
		if err := checkReComposable(lay, profile); err != nil {
			return err
		}
	}

	ended := modeNone
	if args.Restart && !detachedChild() {
		var err error
		if ended, err = restartFolder(vibeHome, name, target, args.Force); err != nil {
			return err
		}
	}
	if !detachedChild() {
		headless, createFirst, err := startMode(lay, args, name, target, ended)
		if err != nil {
			return err
		}
		args.Headless = headless
		if createFirst {
			if err := createBeforeHeadless(lay, args, name, target); err != nil {
				return err
			}
		}
	}
	if args.Foreground && !args.Headless {
		return fmt.Errorf("--foreground only goes with --headless (or with the mode setting in settings.yaml at headless)")
	}
	if args.Headless && !args.Foreground && !detachedChild() {
		if args.Rebuilds() {
			return rebuildThenHeadless(lay, args, name, target)
		}
		return startHeadlessInBackground(lay, args, name, target)
	}

	attaching := !args.ReInit && !args.ReCreate && !args.ReCompose && !args.Delete && !args.Headless
	destroying := args.ReInit || args.ReCreate || args.ReCompose || args.Delete
	if err := guardFolder(vibeHome, target, args.Force, attaching, destroying); err != nil {
		return err
	}
	if !args.Delete {
		termtitle.Set("VIBE: " + pathresolve.Tilde(target))
	}

	if rebuild := rebuildAction(args); rebuild != nil {
		return rebuild(lay, args, name, target)
	}
	switch {
	case args.Delete:
		return doDelete(lay, args, name, target)
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

func resolveProfileName(args cliargs.Args, target string) string {
	if args.Profile != "" {
		return args.Profile
	}
	if args.Name != "" {
		return args.Name
	}
	return pathresolve.DeriveName(target)
}
