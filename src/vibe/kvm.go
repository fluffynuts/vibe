package main

import (
	"runtime"

	"vibe/internal/cliargs"
	"vibe/internal/companion"
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

// vmCheck says why sbx can't start a VM on this machine, with what to do
// about it, or nil. Anything that starts one asks first, before any
// question or change: sbx would fail anyway, part-way through, and with
// less to go on. A variable for tests.
var vmCheck = func() error { return kvmcheck.Err(kvmcheck.Probe()) }

// startsAVM says whether what args ask for can start a sandbox's VM:
// creating, attaching, rebuilding, or reaching into one (which starts a
// stopped one).
func startsAVM(args cliargs.Args) bool {
	return !args.Stop && !args.Cancel && !args.Info && !args.TurnLog && !args.Delete
}

// pageActionStartsAVM says whether one of the page's actions, of kind, can
// start a sandbox's VM. The Admin tab's and the Profiles tab's kinds share
// names where they mean the same.
func pageActionStartsAVM(kind string) bool {
	switch kind {
	// ManageStart and ManageRecompose are AdminStart and AdminReCompose.
	case companion.AdminStart, companion.AdminReInit, companion.AdminReCompose, companion.AdminRestore,
		companion.ManageRebuild, companion.ManageUpdate:
		return true
	}
	return false
}
