package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"vibe"
	"vibe/internal/fscopy"
	"vibe/internal/instructions"
	"vibe/internal/layout"
	"vibe/internal/pathresolve"
	"vibe/internal/sbxfind"
	"vibe/internal/sbxinstall"
	"vibe/internal/upgrade"
)

// doInstall makes vibe fully self-contained under lay.Home (~/.vibe or
// $VIBE_HOME) plus a copy of the binary on PATH, so the unpacked bundle it
// was run from — lay.Bundle — can be deleted afterward. It never
// overwrites anything already at the destination, so it's safe to re-run
// (e.g. after fetching a newer bundle release) without losing local edits;
// re-running only fills in what's missing.
func doInstall(lay layout.Layout, force bool, strategyValue string) error {
	if lay.Home == "" {
		return fmt.Errorf("no home directory to install into, and $VIBE_HOME is not set")
	}
	var strategy upgrade.Strategy
	if strategyValue != "" {
		var err error
		if strategy, err = upgrade.ParseStrategy(strategyValue); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(lay.Home, 0o755); err != nil {
		return err
	}
	note("installing into %s", lay.Home)
	_, statErr := os.Stat(lay.HomePath("settings.yaml"))
	firstInstall := os.IsNotExist(statErr)

	// A file changed both in ~/.vibe and in the package is settled by the
	// strategy if one was given, else by asking — and with -f, or nobody
	// to ask, it's left alone and reported below.
	var decide func(upgrade.Conflict) (upgrade.Choice, error)
	var decideRetired func(rel, mine string) (bool, error)
	var decideShadowed func(name, dir string) bool
	switch {
	case strategy.Given:
		decideShadowed = func(string, string) bool { return strategy.Otherwise == upgrade.Update }
	case !force && interactive():
		decide = decideConflict
		decideRetired = decideRetiredFile
		decideShadowed = decideShadowedFeature
	}
	// First, so the upgrade doesn't also ask about each file in a copy
	// that goes as a whole.
	retireShadowedFeatures(lay, time.Now(), decideShadowed)
	// The user's instructions for every sandbox's agent: theirs, so made
	// once, empty, and never touched again.
	if created, err := instructions.EnsureFile(lay.HomePath(instructions.File)); err != nil {
		note("  WARNING: couldn't create %s: %s", pathresolve.Tilde(lay.HomePath(instructions.File)), err)
	} else if created {
		note("  created %s, for instructions to every sandbox's agent", pathresolve.Tilde(lay.HomePath(instructions.File)))
	}
	res, err := upgrade.Run(upgrade.Options{
		Package:       lay.Bundle,
		Home:          lay.Home,
		Strategy:      strategy,
		Decide:        decide,
		DecideRetired: decideRetired,
		Log:           func(format string, a ...interface{}) { note("  "+format, a...) },
	})
	if err != nil {
		return err
	}
	if res.Identical > 0 {
		note("  %d file(s) already up to date", res.Identical)
	}

	// A first install sets up ~/.vibe/settings.yaml for this machine; a
	// later one only makes sure its memory is still something this
	// machine can give a sandbox.
	if firstInstall {
		if err := setUpSettings(lay, !force); err != nil {
			return err
		}
	} else if err := lowerMemorySetting(lay.HomePath("settings.yaml")); err != nil {
		note("  WARNING: couldn't check the memory setting in %s: %s", pathresolve.Tilde(lay.HomePath("settings.yaml")), err)
	}

	binErr := installBinary()

	if len(res.UpdatedCopies) > 0 {
		note("")
		note("these couldn't be merged automatically — merge each by hand, then delete the .updated copy:")
		for _, rel := range res.UpdatedCopies {
			mine := filepath.Join(lay.Home, strings.TrimSuffix(rel, upgrade.UpdatedSuffix))
			note("  yours: %s", pathresolve.Tilde(mine))
			note("  new:   %s", pathresolve.Tilde(mine+upgrade.UpdatedSuffix))
		}
	}
	if len(res.UndecidedRetired) > 0 {
		note("")
		note("these are no longer in the package, but you had changed them, so they were kept — delete any you don't need:")
		for _, rel := range res.UndecidedRetired {
			note("  %s", pathresolve.Tilde(filepath.Join(lay.Home, rel)))
		}
	}
	if len(res.Unresolved) > 0 {
		note("")
		note("WARNING: %d file(s) changed both in %s and upstream in the package were left as they are:",
			len(res.Unresolved), pathresolve.Tilde(lay.Home))
		for _, rel := range res.Unresolved {
			note("  %s", pathresolve.Tilde(filepath.Join(lay.Home, rel)))
		}
		note("re-run with --update-strategy to settle them: %s", upgrade.StrategyValues)
		note("  merge,keep    merge what merges; keep yours for the rest, with the new version beside it as .updated")
		note("  merge,update  merge what merges; take the package's version for the rest")
		note("  update        take the package's version of every one")
		note("  keep          keep yours (re-run with another strategy to take the changes later)")
		return fmt.Errorf("upstream changes left unmerged in %d file(s)", len(res.Unresolved))
	}
	return binErr
}

// retireShadowedFeatures deals with each feature in ~/.vibe/library with
// the name of a base feature: a copy from before that feature became a base
// one, which nothing reads any more (the base one is always used). remove,
// when there is one, says whether to remove it; otherwise, or when it says
// not to, it is moved to ~/.vibe/retired, in case it held something of the
// user's.
func retireShadowedFeatures(lay layout.Layout, now time.Time, remove func(name, dir string) bool) {
	for _, name := range lay.BaseFeatures() {
		dir := lay.HomePath("library", name)
		if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
			continue
		}
		if remove != nil && remove(name, dir) {
			if err := os.RemoveAll(dir); err != nil {
				note("  WARNING: couldn't remove %s: %s", pathresolve.Tilde(dir), err)
				continue
			}
			note("  removed: %s ('%s' is a base feature now)", pathresolve.Tilde(dir), name)
			continue
		}
		to := lay.HomePath("retired", "library", name)
		if _, err := os.Stat(to); err == nil {
			to += "-" + now.Format("2006-01-02_150405")
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			note("  WARNING: couldn't move aside %s: %s", pathresolve.Tilde(dir), err)
			continue
		}
		if err := os.Rename(dir, to); err != nil {
			note("  WARNING: couldn't move aside %s: %s", pathresolve.Tilde(dir), err)
			continue
		}
		note("  moved aside: %s → %s ('%s' is a base feature now, so nothing used this copy)", pathresolve.Tilde(dir), pathresolve.Tilde(to), name)
	}
}

// decideShadowedFeature asks what to do with a copy of a feature that is a
// base feature now.
func decideShadowedFeature(name, dir string) bool {
	note("%s is an old copy of '%s', which is a base feature now: every profile has it, and this copy is never used.", pathresolve.Tilde(dir), name)
	picked, ok := chooseFrom("What would you like to do with it?",
		[]string{"move it to " + pathresolve.Tilde(retiredPath(dir)) + ", in case it holds anything of mine", "remove it"}, 0)
	return ok && picked == 1
}

// retiredPath is where retireShadowedFeatures moves dir (a feature in
// ~/.vibe/library) aside to, for saying so.
func retiredPath(dir string) string {
	library := filepath.Dir(dir)
	return filepath.Join(filepath.Dir(library), "retired", "library", filepath.Base(dir))
}

// decideRetiredFile asks whether to remove the user's changed copy of a
// file the package no longer ships.
func decideRetiredFile(rel, mine string) (bool, error) {
	note("%s is no longer in the package, but you have changed it.", pathresolve.Tilde(mine))
	picked, ok := chooseFrom("What would you like to do?",
		[]string{"keep my version", "remove it, as the package did"}, 0)
	return ok && picked == 1, nil
}

// sbxOnPath reports whether there's an sbx to run, on PATH or where it's
// installed. A variable so tests can say either way.
var sbxOnPath = sbxfind.Available

// offerSbxInstall, run after --install, offers to install Docker SBX when
// sbx isn't on PATH, since vibe can't start a sandbox without it. With -f,
// or no terminal to ask on, it only says how.
func offerSbxInstall(force bool) error {
	if sbxOnPath() {
		return nil
	}
	if !sbxinstall.Supported(runtime.GOOS, runtime.GOARCH) {
		note("sbx was not found on your PATH — install it from %s", sbxinstall.Releases)
		return nil
	}
	if force || !interactive() {
		note("sbx was not found on your PATH — 'vibe --install-sbx' installs it")
		return nil
	}
	if !confirmDefault(false, true, "sbx was not found on your PATH — install now?") {
		note("not installing sbx — 'vibe --install-sbx' installs it later")
		return nil
	}
	return doInstallSbx(false)
}

// installBinary copies the running executable to ~/.local/bin, creating
// that directory if needed, then warns (without failing) if it isn't on
// $PATH — the freshly installed binary would otherwise be invisible to the
// shell with no explanation why. An existing one is replaced without
// asking, by a release: installing or upgrading vibe is what --install is
// for. A development build leaves an existing one alone.
func installBinary() error {
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
			return warnIfNotOnPath(binDir, "vibe")
		}
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	_, statErr := os.Stat(dest)
	replacing := statErr == nil
	// A build from source would lose the vibe installed there — most likely
	// a release — and leave 'vibe --upgrade' offering that release back.
	if replacing && vibe.Build == "" {
		note("  this is a development build (%s): leaving the vibe at %s as it is", vibe.String(), dest)
		note("  to run this build from your PATH, copy %s there yourself", exe)
		return warnIfNotOnPath(binDir, "vibe")
	}
	if err := replaceFile(exe, dest); err != nil {
		return fmt.Errorf("copying the vibe binary to %s: %w", dest, err)
	}
	if replacing {
		note("  replaced the vibe binary at %s", dest)
	} else {
		note("  copied the vibe binary to %s", dest)
	}
	return warnIfNotOnPath(binDir, "vibe")
}

// replaceFile puts a copy of src at dest, safely even while dest is running
// — another vibe session, say, which an upgrade can't expect to be closed.
// Writing into a running binary fails ("text file busy" on Linux, a sharing
// violation on Windows), so the copy is written beside dest and renamed into
// place. Windows won't rename onto a running executable either, but it will
// rename one out of the way, so the old binary is moved aside first there;
// it is removed if it can be, and otherwise left as dest+".old" to go the
// next time.
func replaceFile(src, dest string) error {
	tmp := dest + ".new"
	if err := fscopy.File(src, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err == nil {
		return nil
	}
	old := dest + ".old"
	os.Remove(old)
	if err := os.Rename(dest, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Rename(old, dest) // put the old one back
		os.Remove(tmp)
		return err
	}
	os.Remove(old)
	return nil
}

// warnIfNotOnPath makes sure dir — where cmd was just installed — is on
// PATH, warning (never failing) when it isn't. On Windows there's one place
// a user's PATH is kept, so it's added there if need be; that, like any
// installer's change to PATH, only reaches terminals opened from now on.
// Elsewhere PATH is whatever the user's shell profile makes it, so vibe
// only says what to add.
func warnIfNotOnPath(dir, cmd string) error {
	if onPath(runtime.GOOS, os.Getenv("PATH"), dir) {
		return nil
	}
	if runtime.GOOS == "windows" {
		if onPath(runtime.GOOS, sbxinstall.PersistentPath(), dir) {
			note("  %s is on your PATH, but only for terminals opened from here on — open a new one to use %s", dir, cmd)
			return nil
		}
		added, err := addToUserPath(dir)
		switch {
		case err == nil && added:
			note("  added %s to your PATH — open a new terminal to use %s", dir, cmd)
			return nil
		case err == nil:
			// Already in the user's PATH, but PersistentPath didn't see it:
			// spelled in a way only the registry check understood.
			note("  %s is on your PATH, but only for terminals opened from here on — open a new one to use %s", dir, cmd)
			return nil
		}
		note("  WARNING: %s is not on your PATH, and adding it failed: %s", dir, err)
		note("  add it for your user, e.g. in PowerShell:")
		note("    [Environment]::SetEnvironmentVariable('Path', \"$([Environment]::GetEnvironmentVariable('Path', 'User'));%s\", 'User')", dir)
		note("  then open a new terminal")
		return nil
	}
	note("  WARNING: %s is not on your PATH", dir)
	note("  add it, e.g. in ~/.bashrc or ~/.zshrc:  export PATH=\"%s:$PATH\"", dir)
	return nil
}

// addToUserPath is sbxinstall.AddToUserPath, as a variable so tests never
// touch the real registry.
var addToUserPath = sbxinstall.AddToUserPath

// onPath reports whether dir is one of the entries in pathList. Windows
// paths are case-insensitive, and its PATH entries often carry a trailing
// separator.
func onPath(goos, pathList, dir string) bool {
	dir = filepath.Clean(dir)
	for _, entry := range filepath.SplitList(pathList) {
		if entry == "" {
			continue
		}
		entry = filepath.Clean(entry)
		if entry == dir || (goos == "windows" && strings.EqualFold(entry, dir)) {
			return true
		}
	}
	return false
}
