// Package cliargs parses vibe's command line: every option has both a long
// and short form.
package cliargs

import (
	"fmt"
	"strings"
)

// Args holds the parsed command line.
type Args struct {
	Name       string
	Profile    string
	Path       string
	Stop       bool
	StopAll    bool
	Ssh        bool
	ReInit     bool
	ReCreate   bool
	ReCompose  bool
	Restore    bool
	Force      bool
	List       bool
	Install    bool
	Upgrade    bool
	InstallSbx bool
	Cleanup    bool
	Delete     bool
	Info       bool
	Setup      bool
	// NoCompanion is -N: don't serve the companion page this run.
	NoCompanion bool
	// NoStart is --no-start: do all the setup, but stop short of starting
	// the sandbox and companion.
	NoStart bool
	// GitToken is --git-token: give the sandbox for the folder a GitHub
	// token to push with, change the one it has, or take it away.
	GitToken bool
	// GitTokenEnv is --git-token-env: the environment variable holding the
	// token to give a sandbox, so it needn't be typed (or be in argv, where
	// anyone on the machine could read it).
	GitTokenEnv string
	// NoGitToken is --no-git-token: no token for a new sandbox, without
	// asking; with --git-token, take the sandbox's away.
	NoGitToken bool
	// GitRules is --git-rules: the rules the agent is given for pushing,
	// comma-separated and unparsed, for when there's no one to ask.
	GitRules string
	Help     bool
	Version  bool
	// UpdateStrategy is --install's --update-strategy value, unparsed.
	UpdateStrategy string
}

// Parse parses argv (excluding the program name).
func Parse(argv []string) (Args, error) {
	var a Args
	i := 0
	for i < len(argv) {
		arg := argv[i]
		switch {
		case arg == "-h" || arg == "--help":
			a.Help = true
			i++
		case arg == "-v" || arg == "--version":
			a.Version = true
			i++
		case arg == "-s" || arg == "--stop":
			a.Stop = true
			i++
		case arg == "-S" || arg == "--stop-all":
			a.StopAll = true
			i++
		case arg == "-c" || arg == "--ssh":
			a.Ssh = true
			i++
		case arg == "-r" || arg == "--re-init" || arg == "--reinit":
			a.ReInit = true
			i++
		case arg == "-R" || arg == "--re-create" || arg == "--recreate":
			a.ReCreate = true
			i++
		case arg == "-C" || arg == "--re-compose" || arg == "--recompose":
			a.ReCompose = true
			i++
		case arg == "--restore":
			a.Restore = true
			i++
		case arg == "-f" || arg == "--force":
			a.Force = true
			i++
		case arg == "-l" || arg == "--list":
			a.List = true
			i++
		case arg == "-i" || arg == "--install":
			a.Install = true
			i++
		case arg == "-U" || arg == "--upgrade":
			a.Upgrade = true
			i++
		case arg == "-I" || arg == "--install-sbx":
			a.InstallSbx = true
			i++
		case arg == "-x" || arg == "--cleanup":
			a.Cleanup = true
			i++
		case arg == "-d" || arg == "--delete":
			a.Delete = true
			i++
		case arg == "-a" || arg == "--info":
			a.Info = true
			i++
		case arg == "--setup":
			a.Setup = true
			i++
		case arg == "-N" || arg == "--no-companion":
			a.NoCompanion = true
			i++
		case arg == "--no-start":
			a.NoStart = true
			i++
		case arg == "--git-token":
			a.GitToken = true
			i++
		case arg == "--no-git-token":
			a.NoGitToken = true
			i++
		case arg == "--git-token-env":
			v, n, err := valueArg(argv, i)
			if err != nil {
				return a, err
			}
			a.GitTokenEnv = v
			i += n
		case strings.HasPrefix(arg, "--git-token-env="):
			a.GitTokenEnv = strings.TrimPrefix(arg, "--git-token-env=")
			i++
		case arg == "--git-rules":
			v, n, err := valueArg(argv, i)
			if err != nil {
				return a, err
			}
			a.GitRules = v
			i += n
		case strings.HasPrefix(arg, "--git-rules="):
			a.GitRules = strings.TrimPrefix(arg, "--git-rules=")
			i++
		case arg == "-n" || arg == "--name":
			v, n, err := valueArg(argv, i)
			if err != nil {
				return a, err
			}
			a.Name = v
			i += n
		case strings.HasPrefix(arg, "--name="):
			a.Name = strings.TrimPrefix(arg, "--name=")
			i++
		case strings.HasPrefix(arg, "-n") && len(arg) > 2:
			a.Name = arg[2:]
			i++
		case arg == "-u" || arg == "--update-strategy":
			v, n, err := valueArg(argv, i)
			if err != nil {
				return a, err
			}
			a.UpdateStrategy = v
			i += n
		case strings.HasPrefix(arg, "--update-strategy="):
			a.UpdateStrategy = strings.TrimPrefix(arg, "--update-strategy=")
			i++
		case strings.HasPrefix(arg, "-u") && len(arg) > 2:
			a.UpdateStrategy = arg[2:]
			i++
		case arg == "-p" || arg == "--profile":
			v, n, err := valueArg(argv, i)
			if err != nil {
				return a, err
			}
			a.Profile = v
			i += n
		case strings.HasPrefix(arg, "--profile="):
			a.Profile = strings.TrimPrefix(arg, "--profile=")
			i++
		case strings.HasPrefix(arg, "-p") && len(arg) > 2:
			a.Profile = arg[2:]
			i++
		case arg == "--":
			i++
			for ; i < len(argv); i++ {
				if err := setPath(&a, argv[i]); err != nil {
					return a, err
				}
			}
		case strings.HasPrefix(arg, "-") && arg != "-":
			return a, fmt.Errorf("unknown option: %s", arg)
		default:
			if err := setPath(&a, arg); err != nil {
				return a, err
			}
			i++
		}
	}
	return a, nil
}

func valueArg(argv []string, i int) (string, int, error) {
	if i+1 >= len(argv) {
		return "", 0, fmt.Errorf("option %s requires an argument", argv[i])
	}
	return argv[i+1], 2, nil
}

func setPath(a *Args, v string) error {
	if a.Path != "" {
		return fmt.Errorf("expected at most one path argument")
	}
	a.Path = v
	return nil
}

// ExclusiveActions counts how many of the mutually-exclusive action flags
// (stop/stop-all/ssh/re-init/re-create/re-compose/restore/list/install/upgrade/install-sbx/setup/
// cleanup/delete/info/git-token) are set.
func (a Args) ExclusiveActions() int {
	n := 0
	for _, b := range []bool{a.Stop, a.StopAll, a.Ssh, a.ReInit, a.ReCreate, a.ReCompose, a.Restore, a.List, a.Install, a.Upgrade, a.InstallSbx, a.Cleanup, a.Delete, a.Info, a.Setup, a.GitToken} {
		if b {
			n++
		}
	}
	return n
}
