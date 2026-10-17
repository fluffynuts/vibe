package main

import (
	"fmt"
	"os"

	"vibe/internal/sbxrun"
)

func doSsh(name, target string) error {
	if exists, err := sbxrun.Exists(name); err != nil {
		return err
	} else if !exists {
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
