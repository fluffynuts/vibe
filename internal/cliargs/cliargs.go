// Package cliargs parses vibe's command line: every option has both a long
// and short form.
package cliargs

import (
	"fmt"
	"strings"
)

// Args holds the parsed command line.
type Args struct {
	Name     string
	Profile  string
	Path     string
	Stop     bool
	Ssh      bool
	ReInit   bool
	ReCreate bool
	Force    bool
	List     bool
	Install  bool
	Help     bool
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
		case arg == "-s" || arg == "--stop":
			a.Stop = true
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
		case arg == "-f" || arg == "--force":
			a.Force = true
			i++
		case arg == "-l" || arg == "--list":
			a.List = true
			i++
		case arg == "-i" || arg == "--install":
			a.Install = true
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
// (stop/ssh/re-init/re-create/list/install) are set.
func (a Args) ExclusiveActions() int {
	n := 0
	for _, b := range []bool{a.Stop, a.Ssh, a.ReInit, a.ReCreate, a.List, a.Install} {
		if b {
			n++
		}
	}
	return n
}
