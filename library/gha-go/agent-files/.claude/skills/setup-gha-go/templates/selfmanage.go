package main

// --install and --upgrade for {{NAME}}, from the setup-gha-go skill.
//
//	{{NAME}} --install   copy this binary to ~/.local/bin, warning when that
//	                     folder is not on the PATH
//	{{NAME}} --upgrade   download the latest GitHub release for this platform,
//	                     check it against the release's SHA256SUMS, and replace
//	                     the running binary with it
//
// Only the first command-line argument is looked at, so this doesn't get in
// the way of the program's own flag parsing: call runSelfManage() first
// thing in main(). Nothing here needs a GitHub login or GitHub's API (which
// limits anonymous callers): the latest release's tag comes from where
// <releases>/latest redirects to, and every file is downloaded from that tag,
// so a release published mid-upgrade can't mix versions.

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Version and Build are set at build time by make.sh (-X main.Version=...
// -X main.Build=...): Version from the VERSION file, Build from CI's run
// number. A local build has no Build, so any release counts as newer.
var (
	Version = "0.0"
	Build   = ""
)

// selfManageUsage is for the program's --help text.
const selfManageUsage = `  --install    copy {{NAME}} to ~/.local/bin
  --upgrade    download and install the latest release from GitHub`

// releasesURL is where {{NAME}}'s releases live. A variable so tests can
// point it at a stand-in server.
var releasesURL = "https://github.com/{{REPO_SLUG}}/releases"

const sumsFile = "SHA256SUMS"

var httpClient = &http.Client{Timeout: 10 * time.Minute}

// runSelfManage handles --install and --upgrade, and reports whether it did:
// when true, main() should return. A failure is printed and exits non-zero.
func runSelfManage() bool {
	if len(os.Args) < 2 {
		return false
	}
	var err error
	switch os.Args[1] {
	case "--install":
		err = selfInstall()
	case "--upgrade":
		err = selfUpgrade()
	default:
		return false
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "{{NAME}}: "+err.Error())
		os.Exit(1)
	}
	return true
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "{{NAME}}.exe"
	}
	return "{{NAME}}"
}

func localBin() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("can't find your home directory: %w", err)
	}
	return filepath.Join(home, ".local", "bin"), nil
}

// ---- install ---------------------------------------------------------------

func selfInstall() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return err
	}
	dir, err := localBin()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(dir, exeName())
	if sameFile(self, dest) {
		fmt.Println("{{NAME}} is already installed at " + dest)
	} else {
		src, err := os.Open(self)
		if err != nil {
			return err
		}
		defer src.Close()
		if err := replaceFile(dest, src); err != nil {
			return err
		}
		fmt.Println("installed " + dest)
	}
	if !onPath(dir, os.Getenv("PATH")) {
		fmt.Fprintf(os.Stderr, "warning: %s is not on your PATH, so '{{NAME}}' won't be found by name.\n", dir)
		fmt.Fprintln(os.Stderr, "         add it to your PATH (e.g. in ~/.profile or ~/.bashrc):")
		fmt.Fprintf(os.Stderr, "           export PATH=\"%s:$PATH\"\n", dir)
	}
	return nil
}

func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

// onPath reports whether dir is one of the folders in a PATH value.
func onPath(dir, pathEnv string) bool {
	want := filepath.Clean(dir)
	for _, p := range filepath.SplitList(pathEnv) {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if p == want || (runtime.GOOS == "windows" && strings.EqualFold(p, want)) {
			return true
		}
	}
	return false
}

// replaceFile writes r to dest as an executable, via a temporary file beside
// it so a failed copy never leaves a half-written program. Replacing a
// running executable is fine on Unix (the rename leaves the old inode to the
// running process); Windows refuses, but lets it be renamed out of the way.
func replaceFile(dest string, r io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".{{NAME}}-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := dest + ".old"
		os.Remove(old)
		if err := os.Rename(dest, old); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(tmpName, dest)
}

// ---- upgrade ---------------------------------------------------------------

func selfUpgrade() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return err
	}
	tag, err := latestTag()
	if err != nil {
		return err
	}
	latest := strings.TrimPrefix(tag, "v")
	running := Version
	if Build != "" {
		running += "." + Build
	}
	fmt.Printf("running %s, latest release is %s\n", running, latest)
	if !isNewer(latest, running) {
		fmt.Println("already up to date")
		return nil
	}
	tmp, err := os.MkdirTemp("", "{{NAME}}-upgrade-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	asset := assetName(runtime.GOOS, runtime.GOARCH)
	zipPath, err := downloadAsset(tag, asset, tmp)
	if err != nil {
		return err
	}
	exe, err := openExe(zipPath)
	if err != nil {
		return err
	}
	defer exe.Close()
	if err := replaceFile(self, exe); err != nil {
		return fmt.Errorf("replacing %s: %w", self, err)
	}
	fmt.Printf("upgraded %s to %s\n", self, latest)
	return nil
}

// assetName is the release zip for an OS and architecture, as the workflow
// publishes them: {{NAME}}-linux-amd64.zip, {{NAME}}-macos-arm64.zip, ...
func assetName(goos, goarch string) string {
	if goos == "darwin" {
		goos = "macos"
	}
	return "{{NAME}}-" + goos + "-" + goarch + ".zip"
}

// latestTag returns the latest release's tag ("v0.1.57"), read from where the
// releases/latest page redirects to. GitHub's latest is never a prerelease.
func latestTag() (string, error) {
	noRedirect := *httpClient
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Get(releasesURL + "/latest")
	if err != nil {
		return "", fmt.Errorf("asking GitHub for the latest release: %w", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || !strings.Contains(loc, "/releases/tag/") {
		return "", fmt.Errorf("asking GitHub for the latest release: got %s, not a redirect to a release", resp.Status)
	}
	tag := path.Base(loc)
	if tag == "" || tag == "tag" {
		return "", fmt.Errorf("asking GitHub for the latest release: can't read a tag from %s", loc)
	}
	return tag, nil
}

// isNewer reports whether version latest ("0.1.57") is newer than running,
// which lacks its third part when it wasn't built by CI.
func isNewer(latest, running string) bool {
	l, r := versionParts(latest), versionParts(running)
	for i := 0; i < max(len(l), len(r)); i++ {
		lv, rv := partAt(l, i), partAt(r, i)
		if lv != rv {
			return lv > rv
		}
	}
	return false
}

func versionParts(v string) []int {
	var out []int
	for _, p := range strings.Split(strings.TrimPrefix(v, "v"), ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = -1
		}
		out = append(out, n)
	}
	return out
}

// partAt is a version's i'th part; a missing one sorts below any real one.
func partAt(p []int, i int) int {
	if i < len(p) {
		return p[i]
	}
	return -1
}

// downloadAsset fetches tag's asset into dir and checks it against the tag's
// SHA256SUMS, returning the file's path.
func downloadAsset(tag, asset, dir string) (string, error) {
	sums, err := fetchSums(tag)
	if err != nil {
		return "", err
	}
	want, ok := sums[asset]
	if !ok {
		return "", fmt.Errorf("release %s has no %s — is this platform built?", tag, asset)
	}
	dest := filepath.Join(dir, asset)
	if err := fetchFile(releasesURL+"/download/"+tag+"/"+asset, dest); err != nil {
		return "", err
	}
	got, err := sha256File(dest)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("%s doesn't match its checksum in %s (got %s, want %s) — not using it", asset, sumsFile, got, want)
	}
	return dest, nil
}

func fetchSums(tag string) (map[string]string, error) {
	resp, err := httpClient.Get(releasesURL + "/download/" + tag + "/" + sumsFile)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", sumsFile, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s for %s: %s", sumsFile, tag, resp.Status)
	}
	sums := map[string]string{}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		// sha256sum's format: "<hex>  <name>", or "<hex> *<name>" in binary mode.
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 {
			sums[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
		}
	}
	return sums, scanner.Err()
}

func fetchFile(url, dest string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	return f.Close()
}

func sha256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// zipExe is a release zip's executable, open for reading; closing it closes
// the zip too. Only that one entry is read, so nothing else in the zip is
// ever written to disk.
type zipExe struct {
	io.ReadCloser
	zip *zip.ReadCloser
}

func (z zipExe) Close() error {
	z.ReadCloser.Close()
	return z.zip.Close()
}

func openExe(zipPath string) (io.ReadCloser, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || path.Base(f.Name) != exeName() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			zr.Close()
			return nil, err
		}
		return zipExe{rc, zr}, nil
	}
	zr.Close()
	return nil, fmt.Errorf("%s has no %s in it", filepath.Base(zipPath), exeName())
}
