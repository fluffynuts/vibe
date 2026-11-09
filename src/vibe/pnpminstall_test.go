package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The pnpm feature's install step is run as it is, with stand-ins for sudo, node, npm and corepack: what it asks
// them for, and what it says when one fails, without the network. That the shim it makes follows packageManager is
// corepack's behaviour, not the step's.
type pnpmStand struct {
	haveNode, haveNpm, haveCorepack bool
	npmFails, enableFails           bool
	notRoot                         bool
}

func runPnpmInstall(t *testing.T, stand pnpmStand) (calls, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the install step is for a Linux sandbox: it needs /bin/sh")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	globalBin := filepath.Join(dir, "global", "bin")
	os.MkdirAll(bin, 0o755)
	os.MkdirAll(globalBin, 0o755)
	callsFile := filepath.Join(dir, "calls")
	logFile := filepath.Join(dir, "log")
	corepack := "#!/bin/sh\necho \"corepack $*\" >>" + callsFile + "\n"
	if stand.enableFails {
		corepack += "exit 1\n"
	}
	npm := "#!/bin/sh\necho \"npm $*\" >>" + callsFile + "\necho \"npm-cache [$npm_config_cache]\" >>" + callsFile + "\n" +
		"case \"$1\" in\n  prefix) echo " + filepath.Join(dir, "global") + "; exit 0 ;;\nesac\n"
	if stand.npmFails {
		npm += "exit 1\n"
	} else {
		npm += "cp " + filepath.Join(dir, "corepack-stand") + " " + filepath.Join(globalBin, "corepack") + "\n"
	}
	writeExecutable(t, filepath.Join(dir, "corepack-stand"), corepack)
	if stand.haveCorepack {
		writeExecutable(t, filepath.Join(bin, "corepack"), corepack)
	}
	if stand.haveNode {
		writeExecutable(t, filepath.Join(bin, "node"), "")
	}
	if stand.haveNpm {
		writeExecutable(t, filepath.Join(bin, "npm"), npm)
	}
	installs := ""
	if !stand.haveNpm {
		installs = "case \"$*\" in *'install -y npm'*) cp " + filepath.Join(dir, "npm-stand") + " " + filepath.Join(bin, "npm") + " ;; esac\n"
		writeExecutable(t, filepath.Join(dir, "npm-stand"), npm)
	}
	// Root, as a build runs it, unless the test says otherwise.
	uid := "0"
	if stand.notRoot {
		uid = "1000"
	}
	writeExecutable(t, filepath.Join(bin, "id"), "echo "+uid+"\n")
	writeExecutable(t, filepath.Join(bin, "sudo"), "echo \"$*\" >>"+callsFile+"\n"+installs)
	mysql84Tools(t, bin, "date", "cp", "chmod")
	cmd := exec.Command("/bin/sh", filepath.Join(repoRoot(t), "library", "pnpm", "install-scripts", "01-install-pnpm"))
	cmd.Env = []string{"PATH=" + bin, "PNPM_LOG=" + logFile, "PNPM_SHIM_FOLDER=" + filepath.Join(dir, "shims")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install step failed: %v\n%s", err, out)
	}
	recorded, _ := os.ReadFile(callsFile)
	logged, _ := os.ReadFile(logFile)
	return string(recorded), string(logged)
}

func TestThePnpmInstallStepInstallsCorepackAndEnablesPnpmWithoutNamingAVersion(t *testing.T) {
	calls, log := runPnpmInstall(t, pnpmStand{haveNode: true, haveNpm: true})
	if !strings.Contains(calls, "npm install -g corepack\n") {
		t.Errorf("corepack wasn't installed from npm: %q", calls)
	}
	if !strings.Contains(calls, "corepack enable --install-directory") || !strings.HasSuffix(strings.TrimSpace(calls), "pnpm") {
		t.Errorf("corepack wasn't asked to enable pnpm: %q", calls)
	}
	if strings.Contains(calls, "pnpm@") {
		t.Errorf("a pnpm version was named, which is the project's to say: %q", calls)
	}
	if strings.Contains(log, "WARN") {
		t.Errorf("warned although it all worked:\n%s", log)
	}
}

func TestThePnpmInstallStepUsesACorepackThatIsThere(t *testing.T) {
	calls, _ := runPnpmInstall(t, pnpmStand{haveNode: true, haveNpm: true, haveCorepack: true})
	if strings.Contains(calls, "npm install") {
		t.Errorf("installed corepack over one that is there: %q", calls)
	}
	if !strings.Contains(calls, "corepack enable") {
		t.Errorf("pnpm wasn't enabled: %q", calls)
	}
}

func TestThePnpmInstallStepInstallsNpmWhenTheImageHasNone(t *testing.T) {
	calls, _ := runPnpmInstall(t, pnpmStand{haveNode: true})
	if !strings.Contains(calls, "install -y npm") || !strings.Contains(calls, "corepack enable") {
		t.Errorf("npm wasn't installed and used: %q", calls)
	}
}

func TestThePnpmInstallStepSaysWhenNpmCannotInstallCorepack(t *testing.T) {
	calls, log := runPnpmInstall(t, pnpmStand{haveNode: true, haveNpm: true, npmFails: true})
	if !strings.Contains(log, "WARN: npm couldn't install corepack: pnpm not installed") {
		t.Errorf("log: %q", log)
	}
	if strings.Contains(calls, "corepack enable") {
		t.Errorf("enabled pnpm without corepack: %q", calls)
	}
}

func TestThePnpmInstallStepSaysWhenCorepackCannotEnablePnpm(t *testing.T) {
	_, log := runPnpmInstall(t, pnpmStand{haveNode: true, haveNpm: true, haveCorepack: true, enableFails: true})
	if !strings.Contains(log, "WARN: corepack couldn't put pnpm in") {
		t.Errorf("log: %q", log)
	}
}

func TestThePnpmInstallStepSaysWhenThereIsNoNode(t *testing.T) {
	_, log := runPnpmInstall(t, pnpmStand{})
	if !strings.Contains(log, "WARN: node and npm aren't installed") {
		t.Errorf("log: %q", log)
	}
}

// The step runs as root with the agent's home: npm with its default cache would fill ~/.npm with root's files, and the agent's
// own npm install then fails with EACCES (seen in a re-composed sandbox: this step ran after the Playwright one had given
// the cache back to the agent, and took it again). Root's npm has a cache in a folder only root can reach.
func TestThePnpmInstallStepKeepsRootsNpmCacheInARootOnlyFolder(t *testing.T) {
	calls, _ := runPnpmInstall(t, pnpmStand{haveNode: true, haveNpm: true})
	if !strings.Contains(calls, "npm install -g corepack\n") {
		t.Fatalf("npm wasn't run to install corepack: %q", calls)
	}
	if strings.Count(calls, "npm-cache [/root/.cache/npm]") != 2 || strings.Contains(calls, "npm-cache []") {
		t.Errorf("npm wasn't run, each time, with /root/.cache/npm as its cache: %q", calls)
	}
}

func TestThePnpmInstallStepLeavesNpmsCacheAloneWhenItIsNotRoot(t *testing.T) {
	calls, _ := runPnpmInstall(t, pnpmStand{haveNode: true, haveNpm: true, notRoot: true})
	if strings.Contains(calls, "/root/") || !strings.Contains(calls, "npm-cache []") {
		t.Errorf("npm was given a cache of the step's choosing when it isn't root: %q", calls)
	}
}
