// Command vibe creates, starts and attaches to docker sbx sandboxes for
// contained agentic coding, driven by profiles bundled alongside the binary.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"vibe"
	"vibe/internal/cliargs"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/prompt"
	"vibe/internal/sbxinstall"
	"vibe/internal/sbxrun"
	"vibe/internal/termtitle"
)

const usage = `vibe — open (creating if needed) a sandbox for a project folder.

  vibe                       use $PWD, profile derived from its folder name
  vibe /path/to/code         use an explicit folder
  vibe -n/--name custom .    override the derived sandbox name
  vibe -p/--profile foo      use profile "foo" instead of the derived one
  vibe -s/--stop [path]      stop the sandbox for path (default: $PWD); with
                             -f, no sandbox there isn't an error
  vibe --cancel [path]       cancel what the companion page for path has under
                             way (the agent's turn, an Admin action), leaving
                             the sandbox running
  vibe -S/--stop-all         stop every running sandbox, in parallel (asks
                             first; -f stops them without asking)
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
  vibe -l/--list             list every known sandbox and its status
  vibe -a/--info [path]      show the sandbox's settings (memory, agent,
                             features) and, while it runs, its uptime and
                             how much memory and disk it is using
  vibe -x/--cleanup          pick sandboxes from a checklist and delete them;
                             the profiles they were built from are kept
  vibe -d/--delete [path]    pick from a checklist whether to delete the
                             sandbox for path (ticked), its profile
                             (unticked), or both; -f deletes both unasked
  vibe -U/--upgrade          check GitHub for newer releases of vibe and
                             Docker SBX, pick which to upgrade (all are
                             ticked), and install them; -f upgrades them all
                             without asking (-f and -u are passed on to
                             vibe's --install)
  vibe -I/--install-sbx      download the latest stable Docker Sandboxes (sbx)
                             release and install it: on Linux into
                             ~/.docker/sbx; on macOS (Apple Silicon) and
                             Windows, asking how (-f takes the first way,
                             and reinstalls when already up to date)
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
	prompt.EnableVT(os.Stdout)
	prompt.EnableVT(os.Stderr)
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
	teeNote(line)
	if holdNote(line) {
		return
	}
	fmt.Fprintf(os.Stderr, "vibe: %s\n", line)
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
		return fmt.Errorf("--stop, --stop-all, --ssh, --re-init, --re-create, --re-compose, --restore, --list, --install, --upgrade, --install-sbx, --cleanup, --delete, --info, --setup and --git-token are mutually exclusive")
	}

	if (args.GitTokenEnv != "" || args.NoGitToken || args.GitRules != "") && args.ExclusiveActions() > 0 && !args.GitToken {
		return fmt.Errorf("--git-token-env, --no-git-token and --git-rules only apply to a new sandbox, or with --git-token")
	}

	vibeHome := vibeHomeDir()

	if args.List {
		if args.Path != "" {
			return fmt.Errorf("--list takes no path argument")
		}
		return doList()
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
		return offerSbxInstall(args.Force)
	}

	if !sbxrun.Available() {
		hint := "Install it from " + sbxinstall.Releases + ",\n"
		if sbxinstall.Supported(runtime.GOOS, runtime.GOARCH) {
			hint = "Install it with 'vibe --install-sbx', or from " + sbxinstall.Releases + ",\n"
		}
		return fmt.Errorf("'sbx' is not on PATH — vibe needs Docker Sandboxes.\n%s"+
			"then make sure its bin directory is on PATH (e.g. ${HOME}/.docker/sbx/bin).", hint)
	}

	// Before the bundle is resolved and ~/.vibe is seeded: cleanup is about
	// sandboxes sbx holds, and needs neither a profile nor a target folder.
	if args.Cleanup {
		if args.Path != "" {
			return fmt.Errorf("--cleanup takes no path argument")
		}
		return doCleanup(vibeHome)
	}

	// Like cleanup, this is about sandboxes sbx holds, not a folder.
	if args.StopAll {
		if args.Path != "" {
			return fmt.Errorf("--stop-all takes no path argument")
		}
		return doStopAll(args.Force)
	}

	bundleRoot, err := pathresolve.BundleRoot()
	if err != nil {
		return err
	}
	lay := layout.New(vibeHome, bundleRoot)
	if err := initHome(lay, args.Force); err != nil {
		return err
	}

	// Windows PowerShell hands "~\project" over as it is, and vibe shows
	// folders that way itself, so the "~" is vibe's to expand.
	target := expandHome(args.Path)
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
		return doRestore(lay, args, name, target)
	}

	if err := guardFolder(vibeHome, target, args.Force); err != nil {
		return err
	}
	if !args.Delete {
		termtitle.Set("VIBE: " + displayPath(target))
	}

	switch {
	case args.ReInit:
		return doReInit(lay, args, name, target)
	case args.ReCreate:
		return doReCreate(lay, args, name, target)
	case args.ReCompose:
		return doReCompose(lay, args, name, target)
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

// expandHome expands a leading "~", "~/" or "~\" in a path: a settings.yaml
// value, or a folder given on the command line.
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
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
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

// displayPath shortens a path under the home directory to ~/..., the way
// the user thinks of it.
func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return path
}
