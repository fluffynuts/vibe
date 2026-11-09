package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"vibe/internal/layout"
	"vibe/internal/library"
	"vibe/internal/profilegen"
)

// The pwsh feature's install script is run as it is, against a local stand-in for GitHub's releases:
// serving a tarball made as PowerShell's is (no folder of its own, and pwsh without its executable bit).

func pwshScriptPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "library", "pwsh", "install-scripts", "01-install-pwsh")
}

// pwshTarball is a gzipped tar of a fake PowerShell, and its checksum.
func pwshTarball(t *testing.T) (data []byte, sum string) {
	t.Helper()
	return pwshTarballOf(t, "#!/bin/sh\necho 7.6.9\n")
}

// pwshTarballOf is that, with pwsh being the script body.
func pwshTarballOf(t *testing.T, body string) (data []byte, sum string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name, body string
		mode       int64
	}{
		{"pwsh", body, 0o644},
		{"Modules/Some/Some.psd1", "@{}\n", 0o644},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(f.body))
	}
	tw.Close()
	gz.Close()
	h := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(h[:])
}

// pwshRun is one run of the install script.
type pwshRun struct {
	log, prefix, link, calls string
	requests                 int32
	exit                     error
}

// pwshApt is what apt does, for a run: the packages apt-cache lists (one "name - description" a line), and whether
// installing fails. The script's apt is never the machine's: a real one would install packages on whoever runs the tests.
type pwshApt struct {
	cache string
	fails bool
}

var defaultPwshApt = pwshApt{cache: "libicu76 - International Components for Unicode\n"}

// runPwshInstall runs the script against a server serving tarball (404 when it is nil), with the checksums
// it will accept being sums, and arch what uname says the machine is ("" for the real one).
func runPwshInstall(t *testing.T, tarball []byte, sums string, arch string, before func(prefix string)) pwshRun {
	t.Helper()
	return runPwshInstallWith(t, tarball, sums, arch, before, defaultPwshApt)
}

func runPwshInstallWith(t *testing.T, tarball []byte, sums string, arch string, before func(prefix string), apt pwshApt) pwshRun {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the script is for a Linux sandbox: GNU tar, sha256sum and date")
	}
	for _, tool := range []string{"curl", "tar", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s isn't installed: the install script needs it", tool)
		}
	}
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if tarball == nil || !strings.HasPrefix(r.URL.Path, "/v7.6.9/powershell-7.6.9-linux-") {
			http.NotFound(w, r)
			return
		}
		w.Write(tarball)
	}))
	t.Cleanup(srv.Close)

	script, err := os.ReadFile(pwshScriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	// The script holds the checksums of the real release: the ones this fake one would have are put in.
	sumLine := regexp.MustCompile(`(?m)^(SHA256_(?:X64|ARM64))=[0-9a-f]{64}$`)
	edited := sumLine.ReplaceAllString(string(script), "${1}="+sums)
	dir := t.TempDir()
	path := filepath.Join(dir, "install")
	if err := os.WriteFile(path, []byte(edited), 0o755); err != nil {
		t.Fatal(err)
	}
	run := pwshRun{prefix: filepath.Join(dir, "opt", "pwsh"), link: filepath.Join(dir, "bin", "pwsh")}
	logFile := filepath.Join(dir, "setup.log")
	if before != nil {
		before(run.prefix)
	}
	fake := t.TempDir()
	callsFile := filepath.Join(fake, "calls")
	os.WriteFile(filepath.Join(fake, "sudo"), []byte("#!/bin/sh\necho \"sudo $*\" >>\"$FAKE_CALLS\"\n[ -n \"$FAKE_APT_FAILS\" ] && exit 100\nexit 0\n"), 0o755)
	os.WriteFile(filepath.Join(fake, "apt-cache"), []byte("#!/bin/sh\nprintf '%s' \"$FAKE_APT_CACHE\"\n"), 0o755)
	if arch != "" {
		os.WriteFile(filepath.Join(fake, "uname"), []byte("#!/bin/sh\necho "+arch+"\n"), 0o755)
	}
	cmd := exec.Command("sh", path)
	cmd.Env = append(os.Environ(), "PWSH_BASE="+srv.URL, "PWSH_PREFIX="+run.prefix, "PWSH_LINK="+run.link, "PWSH_LOG="+logFile,
		"PATH="+fake+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_CALLS="+callsFile, "FAKE_APT_CACHE="+apt.cache)
	if apt.fails {
		cmd.Env = append(cmd.Env, "FAKE_APT_FAILS=1")
	}
	run.exit = cmd.Run()
	log, _ := os.ReadFile(logFile)
	run.log = string(log)
	calls, _ := os.ReadFile(callsFile)
	run.calls = string(calls)
	run.requests = atomic.LoadInt32(&requests)
	return run
}

func TestPwshIsUnpackedIntoItsOwnFolderMadeExecutableAndLinkedOntoThePath(t *testing.T) {
	tarball, sum := pwshTarball(t)
	run := runPwshInstall(t, tarball, sum, "", nil)
	if run.exit != nil {
		t.Fatalf("the script failed: %v\n%s", run.exit, run.log)
	}
	info, err := os.Stat(filepath.Join(run.prefix, "pwsh"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("pwsh in its folder: %v, %v (the tarball leaves it without the executable bit)", info, err)
	}
	if _, err := os.Stat(filepath.Join(run.prefix, "Modules", "Some", "Some.psd1")); err != nil {
		t.Errorf("the rest of the tarball isn't under the folder: %v", err)
	}
	if target, err := os.Readlink(run.link); err != nil || target != filepath.Join(run.prefix, "pwsh") {
		t.Errorf("the link: %q, %v", target, err)
	}
	out, err := exec.Command(run.link).Output()
	if err != nil || strings.TrimSpace(string(out)) != "7.6.9" {
		t.Errorf("running it through the link: %q, %v", out, err)
	}
	if !strings.Contains(run.log, "installed PowerShell 7.6.9") {
		t.Errorf("the log doesn't say it worked:\n%s", run.log)
	}
}

func TestATarballThatIsNotTheOneExpectedIsNotInstalled(t *testing.T) {
	tarball, _ := pwshTarball(t)
	run := runPwshInstall(t, tarball, strings.Repeat("0", 64), "", nil)
	if run.exit != nil {
		t.Errorf("a sandbox's setup is not to fail because of it: %v", run.exit)
	}
	if _, err := os.Stat(run.prefix); err == nil {
		t.Error("something was unpacked though its checksum was wrong")
	}
	if _, err := os.Lstat(run.link); err == nil {
		t.Error("pwsh was linked though its checksum was wrong")
	}
	if !strings.Contains(run.log, "WARN:") || !strings.Contains(run.log, "checksum") {
		t.Errorf("the log doesn't say why:\n%s", run.log)
	}
}

func TestADownloadThatFailsIsSaidAndInstallsNothing(t *testing.T) {
	_, sum := pwshTarball(t)
	run := runPwshInstall(t, nil, sum, "", nil)
	if run.exit != nil {
		t.Errorf("the script failed: %v", run.exit)
	}
	if _, err := os.Stat(run.prefix); err == nil {
		t.Error("a folder was made for nothing")
	}
	if !strings.Contains(run.log, "WARN:") || !strings.Contains(run.log, "download") {
		t.Errorf("the log doesn't say why:\n%s", run.log)
	}
}

func TestAMachineThereIsNoBuildForIsSaidAndNothingIsDownloaded(t *testing.T) {
	tarball, sum := pwshTarball(t)
	run := runPwshInstall(t, tarball, sum, "riscv64", nil)
	if run.requests != 0 {
		t.Errorf("%d downloads for a machine there is no build for", run.requests)
	}
	if !strings.Contains(run.log, "WARN:") || !strings.Contains(run.log, "riscv64") {
		t.Errorf("the log doesn't say why:\n%s", run.log)
	}
}

func TestBothTheCommonMachinesAreKnown(t *testing.T) {
	tarball, sum := pwshTarball(t)
	for _, arch := range []string{"x86_64", "aarch64", "arm64"} {
		if run := runPwshInstall(t, tarball, sum, arch, nil); run.requests != 1 || !strings.Contains(run.log, "installed PowerShell") {
			t.Errorf("%s: %d downloads, log:\n%s", arch, run.requests, run.log)
		}
	}
}

func TestInstallingAgainReplacesTheOldInstallWholeAndANewVersionLeavesNoFileOfTheOld(t *testing.T) {
	tarball, sum := pwshTarball(t)
	run := runPwshInstall(t, tarball, sum, "", func(prefix string) {
		os.MkdirAll(prefix, 0o755)
		os.WriteFile(filepath.Join(prefix, "left-from-the-old-version"), []byte("x"), 0o644)
	})
	if _, err := os.Stat(filepath.Join(run.prefix, "left-from-the-old-version")); err == nil {
		t.Error("a file of the old install is still there")
	}
	if _, err := os.Stat(filepath.Join(run.prefix, "pwsh")); err != nil {
		t.Errorf("the new one isn't there: %v", err)
	}
}

func TestAFailedInstallLeavesAnExistingOneAsItWas(t *testing.T) {
	_, sum := pwshTarball(t)
	run := runPwshInstall(t, nil, sum, "", func(prefix string) {
		os.MkdirAll(prefix, 0o755)
		os.WriteFile(filepath.Join(prefix, "pwsh"), []byte("#!/bin/sh\necho old\n"), 0o755)
	})
	if data, err := os.ReadFile(filepath.Join(run.prefix, "pwsh")); err != nil || !strings.Contains(string(data), "old") {
		t.Errorf("the working install was lost to a failed one: %q, %v", data, err)
	}
}

func TestThePwshFeatureIsOfferedWithADescriptionAndComposesWithWhatItNeeds(t *testing.T) {
	lay := layout.New(t.TempDir(), repoRoot(t))
	var found *library.Feature
	for _, f := range library.List(lay.Features(), lay.FeatureDir) {
		if f.Name == "pwsh" {
			f := f
			found = &f
		}
	}
	if found == nil || found.Description == "" {
		t.Fatalf("pwsh in the library: %+v", found)
	}
	dir, err := profilegen.CreateGuided(lay, "p", []string{"pwsh"})
	if err != nil {
		t.Fatal(err)
	}
	config, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	for _, want := range []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "POWERSHELL_TELEMETRY_OPTOUT", "POWERSHELL_UPDATECHECK"} {
		if !strings.Contains(string(config), want) {
			t.Errorf("config.yaml lacks %s:\n%s", want, config)
		}
	}
	scripts, _ := filepath.Glob(filepath.Join(dir, "install-scripts", "*-install-pwsh"))
	if len(scripts) != 1 {
		t.Errorf("install scripts for pwsh: %v", scripts)
	}
	if kit, _ := os.ReadFile(filepath.Join(dir, "kit-instructions.md")); !strings.Contains(string(kit), "pwsh") {
		t.Errorf("the agent isn't told of it:\n%s", kit)
	}
}

// leftovers are what a run leaves beside the install that isn't it: a half-unpacked one, the old one set aside.
func leftovers(prefix string) []string {
	var found []string
	for _, suffix := range []string{".staging", ".old"} {
		if _, err := os.Stat(prefix + suffix); err == nil {
			found = append(found, prefix+suffix)
		}
	}
	return found
}

func oldPwshInstalled(prefix string) {
	os.MkdirAll(prefix, 0o755)
	os.WriteFile(filepath.Join(prefix, "pwsh"), []byte("#!/bin/sh\necho old\n"), 0o755)
}

func TestAnInstallThatCantBeLinkedLeavesTheWorkingOneAsItWasAndNothingBeside(t *testing.T) {
	tarball, sum := pwshTarball(t)
	run := runPwshInstall(t, tarball, sum, "", func(prefix string) {
		oldPwshInstalled(prefix)
		// Where the link goes is a file, so the folder for it can't be made.
		os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(prefix)), "bin"), []byte("x"), 0o644)
	})
	if data, err := os.ReadFile(filepath.Join(run.prefix, "pwsh")); err != nil || !strings.Contains(string(data), "old") {
		t.Errorf("the working install was lost to one that couldn't be linked: %q, %v", data, err)
	}
	if left := leftovers(run.prefix); len(left) != 0 {
		t.Errorf("left beside the install: %v", left)
	}
	if !strings.Contains(run.log, "WARN:") {
		t.Errorf("the log doesn't say it failed:\n%s", run.log)
	}
}

func TestAnInstallThatWontRunPutsTheOldOneBackWorkingAndLinked(t *testing.T) {
	tarball, sum := pwshTarballOf(t, "#!/bin/sh\nexit 3\n")
	run := runPwshInstall(t, tarball, sum, "", oldPwshInstalled)
	if out, err := exec.Command(filepath.Join(run.prefix, "pwsh")).Output(); err != nil || strings.TrimSpace(string(out)) != "old" {
		t.Errorf("the old install isn't back: %q, %v", out, err)
	}
	if target, err := os.Readlink(run.link); err != nil || target != filepath.Join(run.prefix, "pwsh") {
		t.Errorf("the link: %q, %v", target, err)
	}
	if left := leftovers(run.prefix); len(left) != 0 {
		t.Errorf("left beside the install: %v", left)
	}
	if !strings.Contains(run.log, "WARN:") || !strings.Contains(run.log, "won't run") {
		t.Errorf("the log doesn't say why:\n%s", run.log)
	}
}

func TestAnInstallThatWontRunAndHasNothingToPutBackLeavesNoPwshToBeTakenForOne(t *testing.T) {
	tarball, sum := pwshTarballOf(t, "#!/bin/sh\nexit 3\n")
	run := runPwshInstall(t, tarball, sum, "", nil)
	if _, err := os.Stat(run.prefix); err == nil {
		t.Error("an install that won't run was left in place")
	}
	if _, err := os.Lstat(run.link); err == nil {
		t.Error("pwsh is on the path, though it won't run: the agent is told it is there when it is")
	}
	if !strings.Contains(run.log, "WARN:") || !strings.Contains(run.log, "won't run") {
		t.Errorf("the log doesn't say why:\n%s", run.log)
	}
}

func TestASuccessfulInstallLeavesNothingBesideIt(t *testing.T) {
	tarball, sum := pwshTarball(t)
	run := runPwshInstall(t, tarball, sum, "", oldPwshInstalled)
	if left := leftovers(run.prefix); len(left) != 0 {
		t.Errorf("left beside the install: %v", left)
	}
	if out, err := exec.Command(run.link).Output(); err != nil || strings.TrimSpace(string(out)) != "7.6.9" {
		t.Errorf("the new one isn't what runs: %q, %v", out, err)
	}
}

// PowerShell is .NET, which stops at start without ICU ("Couldn't find a valid ICU package"): seen in a built sandbox,
// whose log said it "won't run" and which had no pwsh. So the ICU library is installed first, whichever version the
// image's apt has (the package's name carries it: libicu76 on Debian 13).
func TestTheIcuLibraryIsInstalledBeforePwshIsRun(t *testing.T) {
	tarball, sum := pwshTarball(t)
	run := runPwshInstall(t, tarball, sum, "", nil)
	if run.exit != nil {
		t.Fatalf("the script failed: %v\n%s", run.exit, run.log)
	}
	if !strings.Contains(run.calls, "apt-get -o DPkg::Lock::Timeout=300 install -y libicu76") {
		t.Errorf("libicu76 wasn't installed; calls:\n%s", run.calls)
	}
	icu, ran := strings.Index(run.log, "libicu76"), strings.Index(run.log, "installed PowerShell")
	if icu < 0 || ran < 0 || icu > ran {
		t.Errorf("the log doesn't show ICU installed before PowerShell was run:\n%s", run.log)
	}
}

func TestTheNewestIcuPackageTheImageHasIsTheOneInstalled(t *testing.T) {
	tarball, sum := pwshTarball(t)
	run := runPwshInstallWith(t, tarball, sum, "", nil, pwshApt{cache: "libicu9 - old\nlibicu76 - new\nlibicu72 - older\n"})
	if !strings.Contains(run.calls, "install -y libicu76") || strings.Contains(run.calls, "libicu72") || strings.Contains(run.calls, "libicu9") {
		t.Errorf("not the newest only; calls:\n%s", run.calls)
	}
}

func TestAnImageWithNoIcuPackageIsSaidAndPwshIsStillInstalled(t *testing.T) {
	tarball, sum := pwshTarball(t)
	run := runPwshInstallWith(t, tarball, sum, "", nil, pwshApt{})
	if run.exit != nil || !strings.Contains(run.log, "installed PowerShell") {
		t.Errorf("the install didn't go on: %v\n%s", run.exit, run.log)
	}
	if !strings.Contains(run.log, "WARN: no ICU package") || strings.Contains(run.calls, "install -y libicu") {
		t.Errorf("not said, or something was installed; log:\n%s\ncalls:\n%s", run.log, run.calls)
	}
}

func TestAnIcuInstallThatFailsIsSaidAndPwshIsStillTried(t *testing.T) {
	tarball, sum := pwshTarball(t)
	run := runPwshInstallWith(t, tarball, sum, "", nil, pwshApt{cache: defaultPwshApt.cache, fails: true})
	if !strings.Contains(run.log, "WARN: couldn't install libicu76") || !strings.Contains(run.log, "installed PowerShell") {
		t.Errorf("not said, or the install didn't go on:\n%s", run.log)
	}
}
