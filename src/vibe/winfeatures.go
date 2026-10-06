package main

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"vibe/internal/layout"
	"vibe/internal/winfeatures"
)

// windowsFeaturesMarker, under ~/.vibe, records that the Windows features
// sbx needs were found on, so later --installs don't ask for admin rights
// again just to look.
const windowsFeaturesMarker = ".windows-features-enabled"

// ensureWindowsFeatures, run by --install on Windows, makes sure the
// optional features sbx's VMs need (winfeatures.Required) are on, enabling
// whichever aren't with dism, and warns when Windows must restart before
// they work. Nothing here fails the install: when the features can't be
// checked or enabled, it says how to do it by hand.
func ensureWindowsFeatures(lay layout.Layout, force bool) {
	if runtime.GOOS != "windows" {
		return
	}
	marker := lay.HomePath(windowsFeaturesMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	features := strings.Join(winfeatures.Required, " and ")
	var code int
	var err error
	if winfeatures.Elevated() {
		code, err = enableMissingFeatures()
	} else {
		note("sbx needs the Windows features %s; checking them with dism needs admin rights", features)
		if !force && !interactive() {
			manualFeaturesHint()
			return
		}
		if !confirmDefault(force, true, "check (and, if needed, enable) them now? Windows will ask for admin rights") {
			manualFeaturesHint()
			return
		}
		note("running dism as administrator — this can take a minute or two")
		code, err = winfeatures.RunElevated(winfeatures.Dism(), winfeatures.EnableArgs(winfeatures.Required))
	}
	if err != nil {
		note("WARNING: couldn't check the Windows features %s: %s", features, err)
		manualFeaturesHint()
		return
	}
	ok, restart, why := winfeatures.Outcome(code)
	switch {
	case !ok:
		note("WARNING: couldn't enable the Windows features %s: %s", features, why)
		manualFeaturesHint()
	case restart:
		note("")
		note("WARNING: the Windows features %s are enabled, but Windows must restart before they work.", features)
		note("WARNING: restart Windows before using vibe or sbx.")
		note("")
	default:
		note("the Windows features %s are enabled", features)
		os.WriteFile(marker, nil, 0o644)
	}
}

// enableMissingFeatures, with admin rights already, asks dism for each
// required feature's state, and enables those that are off. It returns
// dism's exit code as winfeatures.Outcome reads it: 3010 when any feature
// is only on once Windows restarts.
func enableMissingFeatures() (int, error) {
	var missing []string
	pending := false
	for _, f := range winfeatures.Required {
		out, err := exec.Command(winfeatures.Dism(), winfeatures.InfoArgs(f)...).Output()
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return exit.ExitCode(), nil
			}
			return 0, err
		}
		state, ok := winfeatures.ParseState(string(out))
		switch {
		case !ok:
			return 0, errors.New("couldn't read " + f + "'s state from dism's output")
		case winfeatures.Pending(state):
			pending = true
		case !winfeatures.Enabled(state):
			missing = append(missing, f)
		}
	}
	if len(missing) == 0 {
		if pending {
			return 3010, nil
		}
		return 0, nil
	}
	note("enabling the Windows feature(s) %s with dism — this can take a minute or two", strings.Join(missing, " and "))
	cmd := exec.Command(winfeatures.Dism(), winfeatures.EnableArgs(missing)...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	case err != nil:
		return 0, err
	}
	if pending {
		return 3010, nil
	}
	return 0, nil
}

// manualFeaturesHint says how to enable the features by hand.
func manualFeaturesHint() {
	note("to enable them yourself, run this in a terminal opened as administrator, then restart Windows:")
	note("  %s", winfeatures.CommandLine(winfeatures.EnableArgs(winfeatures.Required)))
}
