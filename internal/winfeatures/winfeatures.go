// Package winfeatures checks for, and turns on, the optional Windows
// features sbx's VMs need, for vibe --install. It drives dism rather than
// PowerShell's *-WindowsOptionalFeature, which can stall for minutes.
//
// dism needs admin rights for anything /Online — even just to look — so a
// vibe that isn't elevated asks Windows to run it elevated (see
// RunElevated).
package winfeatures

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Required are the features sbx needs, by dism's names for them.
var Required = []string{"HypervisorPlatform", "VirtualMachinePlatform"}

// Dism is where dism.exe lives: System32, by full path, so it's Windows'
// own whatever PATH holds.
func Dism() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "dism.exe")
	}
	return "dism.exe"
}

// InfoArgs are dism's arguments to report on one feature. /English keeps
// the output parseable whatever language Windows speaks.
func InfoArgs(feature string) []string {
	return []string{"/Online", "/English", "/Get-FeatureInfo", "/FeatureName:" + feature}
}

// EnableArgs are dism's arguments to enable features — and whatever they
// depend on (/All) — without restarting. Enabling one that's already on
// does nothing, and succeeds.
func EnableArgs(features []string) []string {
	args := []string{"/Online", "/English", "/Enable-Feature"}
	for _, f := range features {
		args = append(args, "/FeatureName:"+f)
	}
	return append(args, "/All", "/NoRestart")
}

// CommandLine is dism's command line for args, as someone would type it.
func CommandLine(args []string) string {
	return "dism " + strings.Join(args, " ")
}

// ParseState reads a feature's state ("Enabled", "Disabled", "Enable
// Pending", ...) from dism /Get-FeatureInfo's output, which has a line
// "State : Enabled". ok is false when there's no such line.
func ParseState(out string) (state string, ok bool) {
	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(line, ":")
		if found && strings.EqualFold(strings.TrimSpace(key), "State") {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

// Enabled reports whether a state means the feature is on, now.
func Enabled(state string) bool {
	return strings.EqualFold(state, "Enabled")
}

// Pending reports whether a state means the feature is on once Windows
// restarts.
func Pending(state string) bool {
	return strings.EqualFold(state, "Enable Pending")
}

// Exit codes dism, and asking Windows to elevate it, can end with.
const (
	exitRestart           = 3010 // ERROR_SUCCESS_REBOOT_REQUIRED
	exitElevationRequired = 740  // ERROR_ELEVATION_REQUIRED
	exitCancelled         = 1223 // ERROR_CANCELLED: admin rights refused
)

// Outcome reads dism /Enable-Feature's exit code: whether the features are
// on, whether Windows must restart before they work, and, when they aren't
// on, why.
func Outcome(code int) (ok, restart bool, why string) {
	switch code {
	case 0:
		return true, false, ""
	case exitRestart:
		return true, true, ""
	case exitElevationRequired:
		return false, false, "dism needs admin rights"
	case exitCancelled:
		return false, false, "admin rights weren't given"
	}
	return false, false, fmt.Sprintf("dism exited with code %d (0x%08x)", code, uint32(code))
}
