// Package sbxinstall installs Docker Sandboxes (sbx) from its latest stable
// GitHub release, for vibe --install-sbx. Each platform's release differs:
//
//   - Linux: a tarball holding sbx and an install.sh that copies it into
//     ~/.docker/sbx, which is patched before it runs (see PatchInstaller).
//   - macOS (Apple Silicon only): a tarball of Sbx.app and a bin/ of links
//     into it, a .dmg holding just Sbx.app, and a Homebrew cask of the .dmg.
//   - Windows (x64 only): an MSI installing for the user, and one
//     installing for every user.
//
// Like selfupdate, nothing here needs a GitHub login or GitHub's API.
package sbxinstall

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"vibe/internal/selfupdate"
)

// Releases is where sbx's releases live. A variable so tests can point it at
// a stand-in server.
var Releases = "https://github.com/docker/sbx-releases/releases"

// Installer is the install script inside the release tarball's folder.
const Installer = "install.sh"

// stableTag matches a release proper ("v0.46.0"), not an RC
// ("v0.47.0-rc2"), a nightly or a dev build.
var stableTag = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// Stable reports whether tag names a stable release.
func Stable(tag string) bool {
	return stableTag.MatchString(tag)
}

// LatestStableTag returns the latest stable release's tag. GitHub's latest
// release is never one marked as a prerelease, which every sbx RC, nightly
// and dev build is; the tag is checked too, so one marked wrongly is refused
// rather than installed.
func LatestStableTag() (string, error) {
	tag, err := selfupdate.LatestTagAt(Releases)
	if err != nil {
		return "", err
	}
	if !Stable(tag) {
		return "", fmt.Errorf("the latest sbx release, %s, isn't a stable release (vX.Y.Z) — not installing it; see %s", tag, Releases)
	}
	return tag, nil
}

// The macOS and Windows release assets.
const (
	DarwinTarball     = "DockerSandboxes-darwin.tar.gz"
	DarwinDMG         = "DockerSandboxes-darwin.dmg"
	WindowsMSI        = "DockerSandboxes.msi"        // for the user, in %LOCALAPPDATA%
	WindowsMachineMSI = "DockerSandboxesMachine.msi" // for every user, in Program Files
)

// BrewCask is Docker's Homebrew cask of sbx's .dmg.
const BrewCask = "docker/tap/sbx"

// Supported reports whether sbx's releases have a build vibe can install for
// an OS and architecture: macOS's are Apple Silicon only, and Windows' x64
// only.
func Supported(goos, goarch string) bool {
	switch goos {
	case "linux":
		return goarch == "amd64" || goarch == "arm64"
	case "darwin":
		return goarch == "arm64"
	case "windows":
		return goarch == "amd64"
	}
	return false
}

// MinMacOS is the oldest macOS sbx runs on: 14, Sonoma — what Docker's
// cask requires.
const MinMacOS = 14

// SupportedMacOS reports whether a macOS version, as sw_vers
// -productVersion prints it ("14.5"), is new enough for sbx. One that can't
// be read is given the benefit of the doubt.
func SupportedMacOS(version string) bool {
	major, err := strconv.Atoi(strings.SplitN(strings.TrimSpace(version), ".", 2)[0])
	return err != nil || major >= MinMacOS
}

// AssetName is the Linux release tarball for an architecture:
// DockerSandboxes-linux-amd64.tar.gz or DockerSandboxes-linux-arm64.tar.gz.
func AssetName(goarch string) string {
	return "DockerSandboxes-linux-" + goarch + ".tar.gz"
}

// Download fetches tag's asset into dir, returning the downloaded file's
// path. sbx's releases publish no checksums file, so there's nothing to
// check it against beyond it coming over HTTPS from GitHub.
func Download(tag, asset, dir string) (string, error) {
	dest := filepath.Join(dir, asset)
	if err := selfupdate.Fetch(Releases+"/download/"+tag+"/"+asset, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// Untar unpacks a .tar.gz into dir, keeping file modes, and returns the one
// top-level folder it holds (docker-sbx).
func Untar(tarGz, dir string) (string, error) {
	tops, err := Unpack(tarGz, dir)
	if err != nil {
		return "", err
	}
	if len(tops) != 1 {
		return "", fmt.Errorf("%s holds more than one top-level entry (%s)", tarGz, strings.Join(tops, ", "))
	}
	return filepath.Join(dir, tops[0]), nil
}

// Unpack unpacks a .tar.gz into dir, keeping file modes and symbolic links,
// and returns the names of the entries at its top level, in the order they
// first appear. Nothing in it may land outside dir: not a "../" in a name,
// nor a link pointing out.
func Unpack(tarGz, dir string) ([]string, error) {
	f, err := os.Open(tarGz)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", tarGz, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var tops []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", tarGz, err)
		}
		name := filepath.FromSlash(strings.TrimPrefix(h.Name, "./"))
		if name == "" || name == "." {
			continue
		}
		target := filepath.Join(dir, name)
		if !within(dir, target) {
			return nil, fmt.Errorf("%s: entry %q would land outside %s", tarGz, h.Name, dir)
		}
		if top := strings.SplitN(filepath.ToSlash(name), "/", 2)[0]; !contains(tops, top) {
			tops = append(tops, top)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if err := extract(tr, target, os.FileMode(h.Mode).Perm()); err != nil {
				return nil, err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(h.Linkname) || !within(dir, filepath.Join(filepath.Dir(target), filepath.FromSlash(h.Linkname))) {
				return nil, fmt.Errorf("%s: link %q points outside %s", tarGz, h.Name, dir)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, err
			}
			if err := os.Symlink(h.Linkname, target); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("%s: entry %q isn't a file, folder or link", tarGz, h.Name)
		}
	}
	if len(tops) == 0 {
		return nil, errors.New(tarGz + " is empty")
	}
	return tops, nil
}

// within reports whether path is dir or somewhere under it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Replace moves each of names from the folder from into the folder to,
// replacing whatever to already has by that name. Each one is swapped in
// with renames, so from must be on the same filesystem as to; an entry that
// can't be moved in is put back as it was.
func Replace(from, to string, names []string) error {
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	for _, name := range names {
		src, dst := filepath.Join(from, name), filepath.Join(to, name)
		old := ""
		if _, err := os.Lstat(dst); err == nil {
			old = dst + ".vibe-old"
			if err := os.RemoveAll(old); err != nil {
				return err
			}
			if err := os.Rename(dst, old); err != nil {
				return err
			}
		}
		if err := os.Rename(src, dst); err != nil {
			if old != "" {
				os.Rename(old, dst)
			}
			return err
		}
		if old != "" {
			if err := os.RemoveAll(old); err != nil {
				return err
			}
		}
	}
	return nil
}

// Link makes link a symbolic link to target, replacing whatever link was.
func Link(target, link string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, link)
}

// MSIOutcome reads msiexec's exit code: whether the install went through,
// whether Windows needs a restart to finish it, and, when it didn't go
// through, why.
func MSIOutcome(code int) (ok, restart bool, why string) {
	switch code {
	case 0:
		return true, false, ""
	case 3010:
		return true, true, ""
	case 1641:
		return true, true, "" // and Windows is restarting now
	case 1602:
		return false, false, "the install was cancelled"
	case 1618:
		return false, false, "another install is already running — wait for it to finish, then try again"
	case 1625, 1925:
		return false, false, "Windows didn't allow the install — it needs an administrator"
	case 1638:
		return false, false, "another version of Docker Sandboxes is already installed"
	}
	return false, false, fmt.Sprintf("msiexec exited with code %d", code)
}

func extract(r io.Reader, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(target, mode) // past the umask, so 0755 stays 0755
}

// apparmorStep matches the installer's two lines that need root: copying
// the AppArmor profile into /etc/apparmor.d, and loading it.
var apparmorStep = regexp.MustCompile(`(?m)^([ \t]*)((?:install[ \t].*\$\{apparmor_dir\}|apparmor_parser[ \t]).*)$`)

// PatchInstaller makes sbx's install.sh run its AppArmor steps with sudo.
// Run as the user, the script can't write to /etc/apparmor.d; run whole
// under sudo, it installs sbx into root's home instead of the user's. So
// only those steps are elevated.
//
// A script that mentions AppArmor but has none of the lines this knows how
// to patch has changed shape since this was written, and is refused rather
// than run as-is.
func PatchInstaller(script string) (string, error) {
	if apparmorStep.MatchString(script) {
		return apparmorStep.ReplaceAllString(script, "${1}sudo ${2}"), nil
	}
	if strings.Contains(script, "apparmor") && !strings.Contains(script, "sudo apparmor_parser") {
		return "", errors.New("sbx's " + Installer + " has changed: can't find its AppArmor steps to run them with sudo")
	}
	return script, nil
}

// Paths the installer checks before its AppArmor steps. Variables so tests
// can stand in for them.
var (
	apparmorFS  = "/sys/kernel/security/apparmor"
	apparmorDir = "/etc/apparmor.d"
)

// NeedsSudo reports whether the installer in bundle will run its AppArmor
// steps — and so ask for a password — mirroring the script's own check:
// the profile is bundled, AppArmor is active, and apparmor_parser is there.
func NeedsSudo(bundle string) bool {
	if _, err := os.Stat(filepath.Join(bundle, "apparmor-profile")); err != nil {
		return false
	}
	for _, dir := range []string{apparmorFS, apparmorDir} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return false
		}
	}
	_, err := exec.LookPath("apparmor_parser")
	return err == nil
}

// Prefix is where the installer puts sbx for a user: <home>/.docker/sbx,
// with the binary in its bin folder.
func Prefix(home string) string {
	return filepath.Join(home, ".docker", "sbx")
}

// Version runs sbx at path with "version", and returns the version it
// reports ("v0.46.0").
func Version(path string) (string, error) {
	out, err := exec.Command(path, "version").Output()
	if err != nil {
		return "", err
	}
	return ParseVersion(string(out)), nil
}

// ParseVersion reads the version from sbx version's output, which is
// "sbx version: v0.46.0 <commit>". Output in any other shape is returned
// as-is, trimmed.
func ParseVersion(out string) string {
	out = strings.TrimSpace(out)
	rest, ok := strings.CutPrefix(out, "sbx version:")
	if !ok {
		return out
	}
	if fields := strings.Fields(rest); len(fields) > 0 {
		return fields[0]
	}
	return out
}
