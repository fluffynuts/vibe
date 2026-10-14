package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"vibe"
	"vibe/internal/prompt"
	"vibe/internal/sbxinstall"
	"vibe/internal/selfupdate"
	"vibe/internal/sidebyside"
	"vibe/internal/upgrade"
)

// pendingUpgrade is something --upgrade found a newer release of, and how to
// install it.
type pendingUpgrade struct {
	label string
	run   func() error
}

// doUpgrade checks GitHub for newer releases of vibe and of Docker SBX,
// offers whichever there are in a checklist (all ticked), and installs the
// ones picked. With -f, or no terminal to ask on, it installs them all; -f
// and --update-strategy are passed on to the --install that upgrades vibe.
func doUpgrade(force bool, strategyValue string) error {
	if strategyValue != "" {
		if _, err := upgrade.ParseStrategy(strategyValue); err != nil {
			return err // before downloading anything
		}
	}
	var found []pendingUpgrade
	var unchecked []string
	status := newStatusLine()

	status.show("Checking for a newer vibe")
	if u, summary, err := checkVibeUpgrade(force, strategyValue); err != nil {
		status.done("Checking for a newer vibe failed: %s", err)
		unchecked = append(unchecked, "vibe")
	} else {
		status.done("%s", summary)
		if u != nil {
			found = append(found, *u)
		}
	}

	status.show("Checking for a newer Docker SBX")
	if u, summary, err := checkSbxUpgrade(); err != nil {
		status.done("Checking for a newer Docker SBX failed: %s", err)
		unchecked = append(unchecked, "sbx")
	} else {
		status.done("%s", summary)
		if u != nil {
			found = append(found, *u)
		}
	}

	if len(found) == 0 {
		if len(unchecked) > 0 {
			return fmt.Errorf("couldn't check for a newer %s (see above)", strings.Join(unchecked, " or "))
		}
		fmt.Println("vibe and sbx are up to date")
		return nil
	}

	chosen, err := chooseUpgrades(found, force)
	if err != nil {
		return err
	}
	var errs []error
	for _, u := range chosen {
		if err := u.run(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// availableUpgrades checks, quietly, for newer releases of vibe and of
// Docker SBX, returning the labels of those found. A check that fails finds
// nothing: this only ever offers a hint, so it never complains.
func availableUpgrades() []string {
	var labels []string
	// A build with no build number was made from source, not released: any
	// release of the same version counts as newer than it (which is right
	// for an explicit --upgrade), so suggesting one would be noise to anyone
	// building vibe themselves — very likely of the commit just released.
	if vibe.Build != "" {
		if u, _, err := checkVibeUpgrade(false, ""); err == nil && u != nil {
			labels = append(labels, u.label)
		}
	}
	if u, _, err := checkSbxUpgrade(); err == nil && u != nil {
		labels = append(labels, u.label)
	}
	return labels
}

// checkUpgradesInBackground starts availableUpgrades while a session runs,
// returning a function to call once it has ended, which says what's newer
// and recommends --upgrade. It says nothing when there's nothing newer, or
// the check hasn't finished: a session isn't kept waiting on GitHub.
func checkUpgradesInBackground() (report func()) {
	found := make(chan []string, 1)
	go func() { found <- availableUpgrades() }()
	return func() {
		select {
		case labels := <-found:
			reportUpgrades(os.Stderr, labels)
		default:
		}
	}
}

// reportUpgrades tells w of the upgrades labels lists, if any.
func reportUpgrades(w io.Writer, labels []string) {
	if len(labels) == 0 {
		return
	}
	fmt.Fprintln(w, "vibe: newer releases are available:")
	for _, l := range labels {
		fmt.Fprintf(w, "vibe:   %s\n", l)
	}
	it_or_them := "it"
	if len(labels) > 0 {
		it_or_them = "them"
	}
	fmt.Fprintln(w, "vibe: run 'vibe --upgrade' to install "+it_or_them)
}

// pickUpgrades asks which of labels to upgrade, all ticked to start with,
// returning the indices picked; ok is false when the user quit. A variable
// so tests can stand in for the user.
var pickUpgrades = func(labels []string) ([]int, bool) {
	checked := make([]bool, len(labels))
	for i := range checked {
		checked[i] = true
	}
	// Each item is the upgrade itself, done as soon as it's picked: a
	// "continue?" after that would only repeat the list.
	return checklistNoConfirmFrom("pick what to upgrade", labels, checked)
}

// chooseUpgrades settles which of found to install: all of them with -f
// or no terminal to ask on, else the ones the user picks.
func chooseUpgrades(found []pendingUpgrade, force bool) ([]pendingUpgrade, error) {
	if force || !interactive() {
		return found, nil
	}
	labels := make([]string, len(found))
	for i, u := range found {
		labels[i] = u.label
	}
	idxs, ok := pickUpgrades(labels)
	if !ok {
		return nil, fmt.Errorf("aborted — nothing upgraded")
	}
	if len(idxs) == 0 {
		return nil, errors.New("nothing selected to update")
	}
	chosen := make([]pendingUpgrade, len(idxs))
	for i, idx := range idxs {
		chosen[i] = found[idx]
	}
	return chosen, nil
}

// checkVibeUpgrade checks for a newer release of vibe, returning how to
// upgrade to it (nil when there's none) and a line saying what was found.
func checkVibeUpgrade(force bool, strategyValue string) (*pendingUpgrade, string, error) {
	tag, err := selfupdate.LatestTag()
	if err != nil {
		return nil, "", err
	}
	latest := strings.TrimPrefix(tag, "v")
	running := vibe.FullVersion()
	if !selfupdate.Newer(latest, running) {
		return nil, fmt.Sprintf("vibe %s is the latest", running), nil
	}
	label, summary := fmt.Sprintf("vibe %s → %s", running, latest), fmt.Sprintf("vibe %s is available (you have %s)", latest, running)
	// A build from source has no build number, so every release of its
	// version sorts after it: it isn't older, just not a release.
	if vibe.Build == "" {
		label = fmt.Sprintf("vibe %s (a development build) → %s", running, latest)
		summary = fmt.Sprintf("vibe %s is the latest release (you have a development build of %s)", latest, running)
	}
	return &pendingUpgrade{
		label: label,
		run: func() error {
			note("upgrading vibe %s to %s", running, latest)
			return upgradeVibe(tag, force, strategyValue)
		},
	}, summary, nil
}

// checkSbxUpgrade checks for a newer stable release of Docker SBX than the
// one installed, returning how to upgrade to it — the same way it was
// installed — (nil when there's none, or vibe can't) and a line saying
// what was found.
func checkSbxUpgrade() (*pendingUpgrade, string, error) {
	if !sbxinstall.Supported(runtime.GOOS, runtime.GOARCH) {
		return nil, fmt.Sprintf("Docker SBX has no release for %s/%s to upgrade to", runtime.GOOS, runtime.GOARCH), nil
	}
	methods, err := sbxMethods()
	if err != nil {
		return nil, "", err
	}
	m, ok := installedSbxMethod(methods)
	if !ok {
		if p, err := exec.LookPath("sbx"); err == nil {
			return nil, fmt.Sprintf("Docker SBX at %s wasn't installed by vibe --install-sbx — upgrade it the way it was installed", p), nil
		}
		return nil, "Docker SBX isn't installed — vibe --install-sbx installs it", nil
	}
	installed, err := sbxinstall.Version(m.sbx)
	if err != nil {
		return nil, "", fmt.Errorf("'%s version' failed: %w", m.sbx, err)
	}
	tag, err := sbxinstall.LatestStableTag()
	if err != nil {
		return nil, "", err
	}
	if !selfupdate.Newer(tag, installed) {
		return nil, fmt.Sprintf("Docker SBX %s is the latest", installed), nil
	}
	return &pendingUpgrade{
		label: fmt.Sprintf("Docker SBX %s → %s", installed, tag),
		run: func() error {
			note("upgrading Docker SBX %s to %s", installed, tag)
			if m.asset == "" {
				if err := m.install("", "", false); err != nil {
					return err
				}
				return reportSbx(m)
			}
			return installSbxRelease(m, tag, false)
		},
	}, fmt.Sprintf("Docker SBX %s is available (you have %s)", tag, installed), nil
}

// upgradeVibe fetches release tag for this machine from GitHub into a
// temporary folder, checks it against the release's checksums, unpacks it
// into another, and runs --install from there — which replaces this vibe
// and upgrades ~/.vibe. -f and --update-strategy are passed on to that
// --install.
func upgradeVibe(tag string, force bool, strategyValue string) error {
	downloads, err := os.MkdirTemp("", "vibe-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(downloads)
	asset := selfupdate.AssetName(runtime.GOOS, runtime.GOARCH)
	status := newStatusLine()
	status.show("Downloading %s", asset)
	zipPath, err := selfupdate.Download(tag, asset, downloads)
	if err != nil {
		status.done("Downloading %s failed", asset)
		return err
	}
	status.done("Downloaded %s and checked it against %s", asset, selfupdate.SumsFile)

	unpacked, err := os.MkdirTemp("", "vibe-upgrade-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(unpacked)
	bundle, err := selfupdate.Unzip(zipPath, unpacked)
	if err != nil {
		return err
	}
	exe := filepath.Join(bundle, "vibe")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}

	args := []string{"--install"}
	if force {
		args = append(args, "--force")
	}
	if strategyValue != "" {
		args = append(args, "--update-strategy", strategyValue)
	}
	note("running %s from the new release", strings.Join(args, " "))
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the new release's --install didn't finish cleanly (see above): %w", err)
	}
	return nil
}

// decideConflict asks the user to settle a file both they and the package
// have changed. It first finds out whether the two merge (upgrade has
// already tried), and offers what that allows: with a clean merge, their
// file beside the merged result; without one, their file beside the
// package's. Taking the package's version wholesale shows those two side by
// side first, to confirm.
func decideConflict(c upgrade.Conflict) (upgrade.Choice, error) {
	path := displayPath(c.Mine)
	for {
		if c.CanMerge {
			note("%s already exists and is different from the package source.", path)
			note("Your changes and the package's merge cleanly — here is your version beside the merged result:")
			showSideBySide("your version", "merged result", c.MineText, c.Merged)
			picked, ok := chooseFrom("What would you like to do?",
				[]string{"keep my version", "use the merged version", "overwrite my version with the updated version"}, 0)
			switch {
			case !ok:
				return 0, fmt.Errorf("aborted — %s and any files after it left as they were", path)
			case picked == 0:
				return upgrade.KeepMine, nil
			case picked == 1:
				return upgrade.TakeMerged, nil
			}
			note("Your version beside the updated package version, which would replace it:")
			showSideBySide("your version", "updated package version", c.MineText, c.TheirsText)
			yes, ok := chooseFrom("Overwrite your version with the updated version?", []string{"yes", "no"}, 1)
			if ok && yes == 0 {
				return upgrade.TakeTheirs, nil
			}
			continue // back to the choice above
		}

		note("%s already exists and is different from the package source,", path)
		note("and can't be merged automatically: %s.", c.Why)
		showSideBySide("your version", "updated package version", c.MineText, c.TheirsText)
		picked, ok := chooseFrom("What would you like to do?",
			[]string{"keep my version", "overwrite my version with the updated version"}, 0)
		switch {
		case !ok:
			return 0, fmt.Errorf("aborted — %s and any files after it left as they were", path)
		case picked == 1:
			return upgrade.TakeTheirs, nil
		default:
			return upgrade.KeepMine, nil
		}
	}
}

// showSideBySide prints two versions of a file side by side, as wide as the
// terminal allows.
func showSideBySide(leftName, rightName string, left, right []byte) {
	color := prompt.IsTerminal(os.Stderr)
	fmt.Fprint(os.Stderr, sidebyside.Render(leftName, rightName, string(left), string(right),
		prompt.Width(os.Stderr, 120), color))
}
