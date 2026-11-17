package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"vibe"
	"vibe/internal/backup"
	"vibe/internal/pathresolve"
	"vibe/internal/running"
	"vibe/internal/sbxinstall"
	"vibe/internal/selfupdate"
	"vibe/internal/upgrade"
)

// doDowngrade is --downgrade: the way back from a vibe or sbx release that
// broke something. It offers, in a checklist like --upgrade's, the release
// before the installed one of each (sbx's latest stable one before it), with
// nothing ticked: going back is opt-in. What is picked is installed the way
// --upgrade installs, once ~/.vibe is backed up beside itself.
//
// It always asks, so with no terminal it changes nothing (-f doesn't skip
// the question); and it refuses while any vibe session runs, as sbx under a
// running sandbox, or vibe's files under a running vibe, would be pulled
// out from under it. --update-strategy is passed on to vibe's --install.
func doDowngrade(vibeHome, strategyValue string) error {
	if strategyValue != "" {
		if _, err := upgrade.ParseStrategy(strategyValue); err != nil {
			return err
		}
	}
	if !interactive() {
		return errors.New("--downgrade asks what to go back on, and there is no terminal to ask on: nothing changed")
	}
	if err := refuseWhileVibesRun(vibeHome); err != nil {
		return err
	}

	var found []pendingUpgrade
	var unchecked []string
	status := newStatusLine()

	status.show("Looking for the vibe release before this one")
	if d, summary, err := checkVibeDowngrade(strategyValue); err != nil {
		status.done("Looking for the vibe release before this one failed: %s", err)
		unchecked = append(unchecked, "vibe")
	} else {
		status.done("%s", summary)
		if d != nil {
			found = append(found, *d)
		}
	}

	status.show("Looking for the Docker SBX release before this one")
	if d, summary, err := checkSbxDowngrade(); err != nil {
		status.done("Looking for the Docker SBX release before this one failed: %s", err)
		unchecked = append(unchecked, "sbx")
	} else {
		status.done("%s", summary)
		if d != nil {
			found = append(found, *d)
		}
	}

	if len(found) == 0 {
		if len(unchecked) > 0 {
			return fmt.Errorf("couldn't find the release before this %s (see above): nothing changed", strings.Join(unchecked, " or "))
		}
		fmt.Println("nothing to go back to: nothing changed")
		return nil
	}

	chosen, err := chooseDowngrades(found)
	if err != nil {
		return err
	}
	return applyDowngrades(vibeHome, chosen, time.Now())
}

// otherVibes lists the vibe sessions running, and the work other vibes hold
// a folder for. A variable so tests can stand in for them.
var otherVibes = running.Anywhere

// refuseWhileVibesRun says which vibes are running, when any are.
func refuseWhileVibesRun(vibeHome string) error {
	claims := otherVibes(vibeHome)
	if len(claims) == 0 {
		return nil
	}
	var lines []string
	for _, c := range claims {
		what := c.Folder
		if c.Work != "" {
			what += " (" + c.Work + ")"
		}
		lines = append(lines, fmt.Sprintf("  %s — vibe pid %d", what, c.PID))
	}
	return fmt.Errorf("vibe is running for:\n%s\nend it first ('vibe --stop' in its folder, or 'vibe --stop-all'), then run --downgrade again: nothing changed",
		strings.Join(lines, "\n"))
}

// pickDowngrades asks which of labels to go back on, ticked as checked says,
// returning the indices picked; ok is false when the user quit. A variable
// so tests can stand in for the user.
var pickDowngrades = func(labels []string, checked []bool) ([]int, bool) {
	return checklistNoConfirmFrom("pick what to go back to the release before (nothing is picked to start with)", labels, checked)
}

// chooseDowngrades asks which of found to install, none ticked to start.
func chooseDowngrades(found []pendingUpgrade) ([]pendingUpgrade, error) {
	labels := make([]string, len(found))
	for i, d := range found {
		labels[i] = d.label
	}
	idxs, ok := pickDowngrades(labels, make([]bool, len(found)))
	if !ok {
		return nil, errors.New("aborted — nothing changed")
	}
	if len(idxs) == 0 {
		return nil, errors.New("nothing selected — nothing changed")
	}
	chosen := make([]pendingUpgrade, len(idxs))
	for i, idx := range idxs {
		chosen[i] = found[idx]
	}
	return chosen, nil
}

// backupLeavesOut is what of ~/.vibe its backup leaves out: what is only of
// the moment (which vibes are running, logs) or a cache.
var backupLeavesOut = []string{"running", "logs", "nuget-fetch"}

// applyDowngrades downloads every one of chosen, then, when one of them
// changes ~/.vibe (going back on vibe; sbx leaves it alone), backs ~/.vibe up
// beside itself, then installs each, going on past one that fails. Until
// the installs start nothing has changed, so a download that fails stops
// them all; so does a vibe session started while the user chose or the
// downloads ran (sbx's take a while), as one running before did; and so does
// a backup that can't be made.
func applyDowngrades(vibeHome string, chosen []pendingUpgrade, now time.Time) error {
	downloads, err := os.MkdirTemp("", "vibe-downgrade-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(downloads)
	installs := make([]func() error, len(chosen))
	for i, d := range chosen {
		dir := filepath.Join(downloads, strconv.Itoa(i))
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
		if installs[i], err = d.download(dir); err != nil {
			return fmt.Errorf("%s: %w — nothing changed", d.label, err)
		}
	}

	if err := refuseWhileVibesRun(vibeHome); err != nil {
		return err
	}
	if slices.ContainsFunc(chosen, func(d pendingUpgrade) bool { return d.changesVibeHome }) {
		if err := backUpVibeHome(vibeHome, now); err != nil {
			return err
		}
	}

	var errs []error
	for _, install := range installs {
		if err := install(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	note("'vibe --upgrade' offers the newest releases again, whenever you want them")
	return nil
}

// backUpVibeHome zips ~/.vibe beside itself, saying where, while a spinner
// turns. With no ~/.vibe there is nothing to back up.
func backUpVibeHome(vibeHome string, now time.Time) error {
	if _, err := os.Stat(vibeHome); err == nil {
		// Beside ~/.vibe, not in it: whatever goes wrong with ~/.vibe mustn't
		// take its backup with it.
		var zip string
		err := spinning("Backing up "+pathresolve.Tilde(vibeHome)+": this can take a while", func() (err error) {
			zip, err = backup.CreateLeavingOut(filepath.Dir(vibeHome), filepath.Base(vibeHome)+"-backup", vibeHome, now, backupLeavesOut)
			return err
		})
		if err != nil {
			return fmt.Errorf("couldn't back up %s first: %w — nothing changed", pathresolve.Tilde(vibeHome), err)
		}
		note("backed up %s to %s (the last %d backups are kept)", pathresolve.Tilde(vibeHome), pathresolve.Tilde(zip), backup.Keep)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("couldn't back up %s first: %w — nothing changed", pathresolve.Tilde(vibeHome), err)
	}
	return nil
}

// checkVibeDowngrade finds the vibe release before this one, returning how
// to go back to it (nil when there's none) and a line saying what was found.
func checkVibeDowngrade(strategyValue string) (*pendingUpgrade, string, error) {
	installed := vibe.FullVersion()
	if vibe.Build == "" {
		return nil, fmt.Sprintf("vibe %s is a development build, not a release: there is no release before it to go back to", installed), nil
	}
	tag, err := selfupdate.PreviousTag(installed)
	if err != nil {
		return nil, "", err
	}
	if tag == "" {
		return nil, fmt.Sprintf("vibe %s is the oldest release: there is none before it to go back to", installed), nil
	}
	previous := strings.TrimPrefix(tag, "v")
	return &pendingUpgrade{
		label:           fmt.Sprintf("vibe %s → %s", installed, previous),
		changesVibeHome: true,
		download: func(dir string) (func() error, error) {
			install, err := downloadVibe(tag, dir, false, strategyValue)
			if err != nil {
				return nil, err
			}
			return func() error {
				note("going back from vibe %s to %s", installed, previous)
				if err := install(); err != nil {
					return err
				}
				note("a sandbox built by vibe %s may need rebuilding by this one: 'vibe -C' in its folder", installed)
				return nil
			}, nil
		},
	}, fmt.Sprintf("vibe %s is the release before yours (%s)", previous, installed), nil
}

// checkSbxDowngrade finds the stable Docker SBX release before the one
// installed, returning how to go back to it — the way it was installed —
// (nil when there's none, or vibe can't) and a line saying what was found.
func checkSbxDowngrade() (*pendingUpgrade, string, error) {
	if !sbxinstall.Supported(runtime.GOOS, runtime.GOARCH) {
		return nil, fmt.Sprintf("Docker SBX has no release for %s/%s to go back to", runtime.GOOS, runtime.GOARCH), nil
	}
	methods, err := sbxMethods()
	if err != nil {
		return nil, "", err
	}
	m, ok := installedSbxMethod(methods)
	if !ok {
		if p, err := exec.LookPath("sbx"); err == nil {
			return nil, fmt.Sprintf("Docker SBX at %s wasn't installed by vibe --install-sbx — go back the way it was installed", p), nil
		}
		return nil, "Docker SBX isn't installed — vibe --install-sbx installs it", nil
	}
	installed, err := sbxinstall.Version(m.sbx)
	if err != nil {
		return nil, "", fmt.Errorf("'%s version' failed: %w", m.sbx, err)
	}
	tag, err := sbxinstall.PreviousStableTag(installed)
	if err != nil {
		return nil, "", err
	}
	if tag == "" {
		return nil, fmt.Sprintf("Docker SBX %s is the oldest stable release: there is none before it to go back to", installed), nil
	}
	if m.goBackByHand != nil {
		return nil, m.goBackByHand(tag), nil
	}
	return &pendingUpgrade{
		label: fmt.Sprintf("Docker SBX %s → %s", installed, tag),
		download: func(dir string) (func() error, error) {
			install, err := downloadSbxRelease(m, tag, dir, false)
			if err != nil {
				return nil, err
			}
			return func() error {
				note("going back from Docker SBX %s to %s", installed, tag)
				if err := install(); err != nil {
					return err
				}
				note("a sandbox made under Docker SBX %s may not start under %s: 'vibe -r' in its folder rebuilds it", installed, tag)
				return nil
			}, nil
		},
	}, fmt.Sprintf("Docker SBX %s is the stable release before yours (%s)", tag, installed), nil
}
