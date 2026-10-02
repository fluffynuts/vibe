package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"vibe/internal/sbxinstall"
)

// sbxMethod is one way --install-sbx can install sbx.
type sbxMethod struct {
	label string
	// asset is the release asset it installs from, downloaded before
	// install runs; "" for a method that fetches sbx itself (Homebrew).
	asset string
	// sbx is where the sbx it installs ends up, and binDir the folder on
	// PATH that runs it.
	sbx, binDir string
	// install installs from the downloaded asset (file) of release tag —
	// reinstalling when that release is already in place.
	install func(tag, file string, reinstall bool) error
}

// doInstallSbx installs the latest stable release of Docker Sandboxes (sbx).
// Where a platform has more than one way of doing that, the user picks one
// (the first, with -f or no terminal). The release's asset for that way is
// downloaded into a temporary folder and installed from there; -f
// reinstalls even when that release is already installed.
func doInstallSbx(force bool) error {
	methods, err := sbxMethods()
	if err != nil {
		return err
	}
	m := methods[0]
	if len(methods) > 1 && !force && interactive() {
		labels := make([]string, len(methods))
		for i, m := range methods {
			labels[i] = m.label
		}
		i, ok := chooseFrom("how should Docker SBX be installed?", labels, 0)
		if !ok {
			return fmt.Errorf("aborted — sbx not installed")
		}
		m = methods[i]
	}

	if m.asset == "" {
		if err := m.install("", "", force); err != nil {
			return err
		}
		return reportSbx(m)
	}

	tag, err := sbxinstall.LatestStableTag()
	if err != nil {
		return err
	}
	installed, err := sbxinstall.Version(m.sbx)
	switch {
	case err == nil && installed == tag && !force:
		note("Docker SBX is already at the latest release (%s); -f reinstalls it", tag)
		return nil
	case err == nil && installed == tag:
		note("reinstalling Docker SBX %s", tag)
	case err == nil:
		note("upgrading Docker SBX %s to %s", installed, tag)
	default:
		note("installing Docker SBX %s", tag)
	}

	downloads, err := os.MkdirTemp("", "vibe-sbx-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(downloads)
	status := newStatusLine()
	status.show("Downloading %s", m.asset)
	file, err := sbxinstall.Download(tag, m.asset, downloads)
	if err != nil {
		status.done("Downloading %s failed", m.asset)
		return err
	}
	status.done("Downloaded %s", m.asset)

	if err := m.install(tag, file, installed == tag); err != nil {
		return err
	}
	return reportSbx(m)
}

// sbxMethods lists the ways sbx can be installed on this machine, the one
// to offer first first.
func sbxMethods() ([]sbxMethod, error) {
	if !sbxinstall.Supported(runtime.GOOS, runtime.GOARCH) {
		return nil, fmt.Errorf("sbx has no release for %s/%s — see %s", runtime.GOOS, runtime.GOARCH, sbxinstall.Releases)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	prefix := sbxinstall.Prefix(home)
	binDir := filepath.Join(prefix, "bin")
	sbx := filepath.Join(binDir, "sbx")

	switch runtime.GOOS {
	case "linux":
		return []sbxMethod{{
			label: "the release's install script, into " + displayPath(prefix),
			asset: sbxinstall.AssetName(runtime.GOARCH), sbx: sbx, binDir: binDir,
			install: func(_, file string, _ bool) error { return installSbxLinux(file, prefix) },
		}}, nil

	case "darwin":
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil && !sbxinstall.SupportedMacOS(string(out)) {
			return nil, fmt.Errorf("sbx needs macOS %d (Sonoma) or newer, and this is %s", sbxinstall.MinMacOS, strings.TrimSpace(string(out)))
		}
		methods := []sbxMethod{
			{
				label: "unpack the release into " + displayPath(prefix) + " (no admin rights needed)",
				asset: sbxinstall.DarwinTarball, sbx: sbx, binDir: binDir,
				install: func(_, file string, _ bool) error { return installSbxMacTarball(file, prefix) },
			},
			{
				label: "install Sbx.app from the release's .dmg into /Applications, linking sbx into " + displayPath(binDir),
				asset: sbxinstall.DarwinDMG, sbx: sbx, binDir: binDir,
				install: func(_, file string, _ bool) error { return installSbxMacDMG(file, binDir) },
			},
		}
		if brew, err := exec.LookPath("brew"); err == nil {
			brewBin := filepath.Dir(brew)
			if out, err := exec.Command(brew, "--prefix").Output(); err == nil {
				brewBin = filepath.Join(strings.TrimSpace(string(out)), "bin")
			}
			methods = append(methods, sbxMethod{
				label: "Homebrew: brew install --cask " + sbxinstall.BrewCask + " (brew upgrades it from then on)",
				sbx:   filepath.Join(brewBin, "sbx"), binDir: brewBin,
				install: func(_, _ string, reinstall bool) error { return installSbxBrew(brew, reinstall) },
			})
		} else {
			note("(Homebrew isn't on your PATH, so installing through it isn't offered)")
		}
		return methods, nil

	case "windows":
		user := filepath.Join(os.Getenv("LOCALAPPDATA"), "DockerSandboxes", "bin")
		machine := filepath.Join(os.Getenv("ProgramFiles"), "DockerSandboxes", "bin")
		return []sbxMethod{
			{
				label: "just for you, in " + user + " (no admin rights needed)",
				asset: sbxinstall.WindowsMSI, sbx: filepath.Join(user, "sbx.exe"), binDir: user,
				install: func(_, file string, reinstall bool) error { return installSbxMSI(file, false, reinstall) },
			},
			{
				label: "for every user, in " + machine + " (Windows asks for admin rights)",
				asset: sbxinstall.WindowsMachineMSI, sbx: filepath.Join(machine, "sbx.exe"), binDir: machine,
				install: func(_, file string, reinstall bool) error { return installSbxMSI(file, true, reinstall) },
			},
		}, nil
	}
	return nil, fmt.Errorf("--install-sbx doesn't support %s", runtime.GOOS)
}

// installSbxLinux unpacks the Linux release tarball into a temporary
// folder, and runs the install.sh it holds — patched to run its AppArmor
// steps with sudo (see sbxinstall.PatchInstaller) — to install into prefix.
func installSbxLinux(tarball, prefix string) error {
	unpacked, err := os.MkdirTemp("", "vibe-sbx-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(unpacked)
	bundle, err := sbxinstall.Untar(tarball, unpacked)
	if err != nil {
		return err
	}
	script := filepath.Join(bundle, sbxinstall.Installer)
	original, err := os.ReadFile(script)
	if err != nil {
		return fmt.Errorf("the sbx release has no %s: %w", sbxinstall.Installer, err)
	}
	patched, err := sbxinstall.PatchInstaller(string(original))
	if err != nil {
		return err
	}
	if err := os.WriteFile(script, []byte(patched), 0o755); err != nil {
		return err
	}

	if sbxinstall.NeedsSudo(bundle) {
		note("the installer adds an AppArmor profile for sbx to /etc/apparmor.d, which needs root:")
		note("you'll be asked for your password, so that just that step can run with sudo")
		sudo := exec.Command("sudo", "-v")
		sudo.Stdin, sudo.Stdout, sudo.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := sudo.Run(); err != nil {
			return fmt.Errorf("couldn't get sudo for the AppArmor step, so sbx isn't installed: %w", err)
		}
	}

	cmd := exec.Command(script)
	cmd.Env = append(os.Environ(), "PREFIX="+prefix)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sbx's %s didn't finish cleanly (see above): %w", sbxinstall.Installer, err)
	}
	return nil
}

// installSbxMacTarball unpacks the macOS release tarball — Sbx.app, and a
// bin/ of links into it — into prefix, replacing only what the tarball
// holds. It's unpacked beside prefix first, so each part swaps in with a
// rename.
func installSbxMacTarball(tarball, prefix string) error {
	if err := os.MkdirAll(filepath.Dir(prefix), 0o755); err != nil {
		return err
	}
	unpacked, err := os.MkdirTemp(filepath.Dir(prefix), ".vibe-sbx-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(unpacked)
	tops, err := sbxinstall.Unpack(tarball, unpacked)
	if err != nil {
		return err
	}
	stopSbxDaemon(filepath.Join(prefix, "bin", "sbx"))
	if err := sbxinstall.Replace(unpacked, prefix, tops); err != nil {
		return fmt.Errorf("installing into %s: %w", prefix, err)
	}
	note("installed Sbx.app into %s", displayPath(prefix))
	return nil
}

// macApplications is where installSbxMacDMG puts Sbx.app.
const macApplications = "/Applications"

// installSbxMacDMG mounts the .dmg, copies the Sbx.app it holds into
// /Applications — beside the one there, then swapped in — and links sbx (and
// llmman, as the tarball does) into binDir, so it's run from the same place
// whichever way it was installed.
func installSbxMacDMG(dmg, binDir string) error {
	mount, err := os.MkdirTemp("", "vibe-sbx-dmg-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mount)
	attach := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mount, dmg)
	if out, err := attach.CombinedOutput(); err != nil {
		return fmt.Errorf("mounting %s: %w\n%s", filepath.Base(dmg), err, out)
	}
	defer func() {
		if exec.Command("hdiutil", "detach", mount, "-quiet").Run() != nil {
			exec.Command("hdiutil", "detach", mount, "-force", "-quiet").Run()
		}
	}()

	staging, err := os.MkdirTemp(macApplications, ".vibe-sbx-")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("can't write to %s (only an admin user can) — pick the %s option instead", macApplications, displayPath(filepath.Dir(binDir)))
		}
		return err
	}
	defer os.RemoveAll(staging)
	// ditto keeps everything the app's code signature covers.
	if out, err := exec.Command("ditto", filepath.Join(mount, "Sbx.app"), filepath.Join(staging, "Sbx.app")).CombinedOutput(); err != nil {
		return fmt.Errorf("copying Sbx.app out of %s: %w\n%s", filepath.Base(dmg), err, out)
	}
	app := filepath.Join(macApplications, "Sbx.app")
	stopSbxDaemon(filepath.Join(binDir, "sbx"))
	stopSbxDaemon(filepath.Join(app, "Contents", "MacOS", "sbx"))
	if err := sbxinstall.Replace(staging, macApplications, []string{"Sbx.app"}); err != nil {
		return fmt.Errorf("installing Sbx.app into %s: %w", macApplications, err)
	}
	note("installed Sbx.app into %s", macApplications)

	for _, name := range []string{"sbx", "llmman"} {
		if err := sbxinstall.Link(filepath.Join(app, "Contents", "MacOS", name), filepath.Join(binDir, name)); err != nil {
			return fmt.Errorf("linking %s into %s: %w", name, binDir, err)
		}
	}
	note("linked sbx into %s", displayPath(binDir))
	if old := filepath.Join(filepath.Dir(binDir), "Sbx.app"); isDir(old) {
		note("  %s, from installing the tarball before, isn't used any more — you can delete it", displayPath(old))
	}
	return nil
}

// installSbxBrew installs Docker's cask with Homebrew, or upgrades it when
// it's installed already — which Homebrew skips when it's up to date, unless
// reinstall.
func installSbxBrew(brew string, reinstall bool) error {
	verb := "install"
	if exec.Command(brew, "list", "--cask", sbxinstall.BrewCask).Run() == nil {
		verb = "upgrade"
		if reinstall {
			verb = "reinstall"
		}
	}
	note("running: brew %s --cask %s", verb, sbxinstall.BrewCask)
	cmd := exec.Command(brew, verb, "--cask", sbxinstall.BrewCask)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew %s didn't finish cleanly (see above): %w", verb, err)
	}
	return nil
}

// installSbxMSI runs an sbx MSI with a progress bar and nothing to click
// through, logging to a file kept for when it fails. The machine-wide MSI
// needs an administrator, which Windows asks for itself. Reinstalling the
// release already installed repairs it instead, which is what puts every
// file back.
func installSbxMSI(msi string, machine, reinstall bool) error {
	if machine {
		note("installing for every user needs admin rights — Windows will ask for them")
	}
	log := filepath.Join(os.TempDir(), "vibe-sbx-install.log")
	op := "/i"
	if reinstall {
		op = "/fa"
	}
	cmd := exec.Command("msiexec", op, msi, "/passive", "/norestart", "/l*v", log)
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		return fmt.Errorf("running msiexec: %w", err)
	}
	ok, restart, why := sbxinstall.MSIOutcome(code)
	if !ok {
		return fmt.Errorf("installing %s: %s (msiexec's log: %s)", filepath.Base(msi), why, log)
	}
	os.Remove(log)
	if restart {
		note("Windows needs a restart to finish installing sbx")
	}
	return nil
}

// stopSbxDaemon stops the daemon of the sbx at path, if there's one there,
// before it's replaced — as Docker's own MSI and cask do.
func stopSbxDaemon(path string) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	exec.Command(path, "daemon", "stop").Run()
}

// reportSbx checks the sbx just installed: that it's what PATH runs, and
// which version it is.
func reportSbx(m sbxMethod) error {
	checkSbxOnPath(m.binDir)
	warnIfShadowed("sbx", m.sbx)
	version, err := sbxinstall.Version(m.sbx)
	if err != nil {
		return fmt.Errorf("installed sbx, but '%s version' failed: %w", m.sbx, err)
	}
	fmt.Printf("Installed Docker SBX at version: %s\n", version)
	return nil
}

// checkSbxOnPath warns when binDir isn't on PATH. On Windows the installer
// adds it to the PATH in the registry, which only terminals opened from now
// on see — so that's checked too, and what to do differs.
func checkSbxOnPath(binDir string) {
	if onPath(runtime.GOOS, os.Getenv("PATH"), binDir) {
		return
	}
	if runtime.GOOS == "windows" && onPath(runtime.GOOS, sbxinstall.PersistentPath(), binDir) {
		note("  %s is on your PATH now, but only for terminals opened from here on — open a new one to use sbx", binDir)
		return
	}
	warnIfNotOnPath(binDir)
}

// warnIfShadowed warns when running name from PATH would find something
// other than want — another copy installed elsewhere, earlier on PATH.
func warnIfShadowed(name, want string) {
	found, err := exec.LookPath(name)
	if err != nil {
		return
	}
	if sameFile(found, want) {
		return
	}
	note("  WARNING: '%s' on your PATH is %s, not the %s just installed", name, found, want)
}

func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
