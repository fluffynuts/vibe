package main

import (
	"runtime"

	"vibe/internal/kvmcheck"
)

// checkKVM, run by --install on Linux, warns when sbx won't be able to
// start its VMs: /dev/kvm is missing, or the user can't read and write it.
// It's advice only, and never fails the install.
func checkKVM() {
	if runtime.GOOS != "linux" {
		return
	}
	advice := kvmcheck.Advice(kvmcheck.Probe())
	if len(advice) == 0 {
		return
	}
	note("")
	for _, line := range advice {
		note("%s", line)
	}
	note("")
}
