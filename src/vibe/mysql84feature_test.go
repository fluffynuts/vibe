package main

import (
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
	"testing"
	"time"

	"vibe/internal/layout"
	"vibe/internal/library"
	"vibe/internal/profilegen"
)

// The mysql-8.4 feature's scripts are run as they are: the install against a stand-in for Oracle's download
// server (serving a small tarball made as Oracle's are), the start with a stand-in sudo that records what it is
// asked. The real run, against the real download and a real mysqld, is a different kind of test.

func mysql84Script(t *testing.T, parts ...string) string {
	t.Helper()
	return filepath.Join(append([]string{repoRoot(t), "library", "mysql-8.4"}, parts...)...)
}

func mysql84Tools(t *testing.T, dir string, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s on this machine", tool)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakeMysqlTarball is a .tar.xz laid out as Oracle's is (one folder, with bin and lib/private in it), whose mysqld
// says what mysqldSays, and its SHA-256.
func fakeMysqlTarball(t *testing.T, mysqldBody string) (data []byte, sum string) {
	t.Helper()
	work := t.TempDir()
	root := filepath.Join(work, "mysql-8.4.13-linux-glibc2.28-x86_64-minimal")
	writeExecutable(t, filepath.Join(root, "bin", "mysqld"), mysqldBody)
	writeExecutable(t, filepath.Join(root, "bin", "mysql"), "echo mysql\n")
	if err := os.MkdirAll(filepath.Join(root, "lib", "private"), 0o755); err != nil {
		t.Fatal(err)
	}
	tarball := filepath.Join(work, "out.tar.xz")
	if out, err := exec.Command("tar", "-cJf", tarball, "-C", work, filepath.Base(root)).CombinedOutput(); err != nil {
		t.Skipf("can't make a .tar.xz here: %v\n%s", err, out)
	}
	data, err := os.ReadFile(tarball)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	return data, hex.EncodeToString(h[:])
}

type mysql84Install struct {
	prefix, links, log string
	requests           []string
	exit               error
}

// runMysql84Install runs the install script against a stand-in server that serves what serve says for each path
// (and 404 for the rest), with the script's pinned checksums replaced by sums (the fake tarball's, or not).
func runMysql84Install(t *testing.T, serve map[string][]byte, sums string, arch string, prefixHasMysql bool) mysql84Install {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the script is for a Linux sandbox")
	}
	var run mysql84Install
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		run.requests = append(run.requests, r.URL.Path)
		body, ok := serve[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()

	script, err := os.ReadFile(mysql84Script(t, "install-scripts", "01-install-mysql-8.4"))
	if err != nil {
		t.Fatal(err)
	}
	sumLine := regexp.MustCompile(`(?m)^(SHA256_(?:X64|ARM64))=[0-9a-f]{64}$`)
	dir := t.TempDir()
	edited := filepath.Join(dir, "install")
	if err := os.WriteFile(edited, []byte(sumLine.ReplaceAllString(string(script), "${1}="+sums)), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	mysql84Tools(t, bin, "date", "mktemp", "curl", "sha256sum", "cut", "rm", "mkdir", "tar", "xz", "ls", "head", "mv", "basename", "ln", "cat")
	writeExecutable(t, filepath.Join(bin, "uname"), "echo "+arch+"\n")
	calls := filepath.Join(dir, "calls")
	for _, name := range []string{"sudo", "install", "useradd"} {
		writeExecutable(t, filepath.Join(bin, name), "echo \""+name+" $*\" >>"+calls+"\n")
	}
	writeExecutable(t, filepath.Join(bin, "getent"), "exit 0\n")
	bundle := filepath.Join(dir, "certs.crt")
	os.WriteFile(bundle, []byte("certs"), 0o644)

	run.prefix, run.links, run.log = filepath.Join(dir, "opt", "mysql-8.4"), filepath.Join(dir, "links"), filepath.Join(dir, "log")
	if prefixHasMysql {
		writeExecutable(t, filepath.Join(run.prefix, "bin", "mysqld"), "echo old mysqld\n")
	}
	cmd := exec.Command("/bin/sh", edited)
	cmd.Env = []string{"PATH=" + bin, "MYSQL_BASE=" + srv.URL, "MYSQL_PREFIX=" + run.prefix, "MYSQL_LINKS=" + run.links,
		"MYSQL_DATADIR=" + filepath.Join(dir, "data"), "MYSQL_INSTALL_LOG=" + run.log, "MYSQL_CA_BUNDLE=" + bundle}
	run.exit = cmd.Run()
	logged, _ := os.ReadFile(run.log)
	run.log = string(logged)
	return run
}

const mysqldOK = "echo \"mysqld  Ver 8.4.13 for Linux on x86_64 (MySQL Community Server - GPL)\"\n"

const x64Asset = "mysql-8.4.13-linux-glibc2.28-x86_64-minimal.tar.xz"

func TestMysql84IsDownloadedCheckedUnpackedAndLinked(t *testing.T) {
	data, sum := fakeMysqlTarball(t, mysqldOK)
	run := runMysql84Install(t, map[string][]byte{"/Downloads/MySQL-8.4/" + x64Asset: data}, sum, "x86_64", false)
	if run.exit != nil {
		t.Fatalf("the script failed: %v\n%s", run.exit, run.log)
	}
	if !strings.Contains(run.log, "installed ") || !strings.Contains(run.log, "Ver 8.4.13") || strings.Contains(run.log, "WARN") {
		t.Errorf("the log doesn't say it was installed cleanly:\n%s", run.log)
	}
	if _, err := os.Stat(filepath.Join(run.prefix, "bin", "mysqld")); err != nil {
		t.Errorf("mysqld isn't in the prefix: %v", err)
	}
	for _, program := range []string{"mysqld", "mysql"} {
		target, err := os.Readlink(filepath.Join(run.links, program))
		if err != nil || target != filepath.Join(run.prefix, "bin", program) {
			t.Errorf("%s isn't linked to the prefix's: %q, %v", program, target, err)
		}
	}
	if _, err := os.Stat(run.prefix + ".staging"); !os.IsNotExist(err) {
		t.Errorf("the staging folder is left behind: %v", err)
	}
}

func TestMysql84IsTakenFromTheArchiveOnceTheReleaseHasMovedThere(t *testing.T) {
	data, sum := fakeMysqlTarball(t, mysqldOK)
	run := runMysql84Install(t, map[string][]byte{"/archives/mysql-8.4/" + x64Asset: data}, sum, "x86_64", false)
	if run.exit != nil || !strings.Contains(run.log, "installed ") {
		t.Fatalf("not installed from the archive: %v\n%s\nrequests: %v", run.exit, run.log, run.requests)
	}
	if len(run.requests) < 2 || !strings.HasPrefix(run.requests[0], "/Downloads/") {
		t.Errorf("the release's own folder should be tried first: %v", run.requests)
	}
}

func TestMysql84OnArmGetsTheFullBuildNotTheMinimalOne(t *testing.T) {
	data, sum := fakeMysqlTarball(t, mysqldOK)
	const armAsset = "mysql-8.4.13-linux-glibc2.28-aarch64.tar.xz"
	run := runMysql84Install(t, map[string][]byte{"/Downloads/MySQL-8.4/" + armAsset: data}, sum, "aarch64", false)
	if run.exit != nil || !strings.Contains(run.log, "installed ") {
		t.Fatalf("not installed on ARM: %v\n%s\nrequests: %v", run.exit, run.log, run.requests)
	}
	if len(run.requests) == 0 || !strings.HasSuffix(run.requests[0], armAsset) {
		t.Errorf("asked for %v, want %s", run.requests, armAsset)
	}
}

func TestMysql84WithTheWrongChecksumIsNotInstalled(t *testing.T) {
	data, _ := fakeMysqlTarball(t, mysqldOK)
	run := runMysql84Install(t, map[string][]byte{"/Downloads/MySQL-8.4/" + x64Asset: data}, strings.Repeat("0", 64), "x86_64", false)
	if !strings.Contains(run.log, "checksum") || !strings.Contains(run.log, "not installed") {
		t.Errorf("the log doesn't say why:\n%s", run.log)
	}
	if _, err := os.Stat(run.prefix); !os.IsNotExist(err) {
		t.Errorf("something was installed although its checksum was wrong: %v", err)
	}
}

func TestMysql84ThatCannotBeDownloadedIsSaid(t *testing.T) {
	_, sum := fakeMysqlTarball(t, mysqldOK)
	run := runMysql84Install(t, map[string][]byte{}, sum, "x86_64", false)
	if !strings.Contains(run.log, "WARN: the MySQL download failed") {
		t.Errorf("the log doesn't say the download failed:\n%s", run.log)
	}
}

func TestAMysqlThatWontRunDoesNotReplaceOneThatDoes(t *testing.T) {
	data, sum := fakeMysqlTarball(t, "exit 1\n")
	run := runMysql84Install(t, map[string][]byte{"/Downloads/MySQL-8.4/" + x64Asset: data}, sum, "x86_64", true)
	if !strings.Contains(run.log, "won't run") || !strings.Contains(run.log, "is back") {
		t.Errorf("the log doesn't say the old one was put back:\n%s", run.log)
	}
	got, err := os.ReadFile(filepath.Join(run.prefix, "bin", "mysqld"))
	if err != nil || !strings.Contains(string(got), "old mysqld") {
		t.Errorf("the mysqld that was there isn't back: %q, %v", got, err)
	}
}

type mysql84Start struct {
	calls, log, datadir string
	exit                error
}

type startState struct {
	listening   bool
	data        string // "data" (a MySQL data folder), "empty", or "other" (files that aren't MySQL's)
	stateFails  bool   // reading the folder as root fails
	initFails   bool
	wantStarted bool
}

func runMysql84Start(t *testing.T, st startState) mysql84Start {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the script is for a Linux sandbox")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	mysql84Tools(t, bin, "date", "awk", "ls", "find", "sh", "sleep")
	calls := filepath.Join(dir, "calls")
	writeExecutable(t, filepath.Join(bin, "sudo"), `echo "$@" >>`+calls+`
case "$1" in
  sh) [ -n "$FAKE_STATE_FAILS" ] && exit 1; exec "$@" ;;
  find) exec "$@" ;;
  -u) case "$*" in *--initialize-insecure*) [ -n "$FAKE_INIT_FAILS" ] && exit 1 ;; esac ;;
esac
exit 0
`)
	prefix := filepath.Join(dir, "opt")
	writeExecutable(t, filepath.Join(prefix, "bin", "mysqld"), "exit 0\n")
	writeExecutable(t, filepath.Join(prefix, "bin", "mysqladmin"), "exit 0\n")
	datadir := filepath.Join(dir, "data")
	os.MkdirAll(datadir, 0o755)
	switch st.data {
	case "data":
		os.MkdirAll(filepath.Join(datadir, "mysql"), 0o755)
		os.WriteFile(filepath.Join(datadir, "ibdata1"), []byte("the user's data"), 0o644)
	case "other":
		os.WriteFile(filepath.Join(datadir, "someone-elses-file"), []byte("x"), 0o644)
	}
	table := "  sl  local_address rem_address   st\n"
	if st.listening {
		table += "   0: 0100007F:0CEA 00000000:0000 0A 00000000:00000000 00:00000000 00000000   100        0 1 1\n"
	}
	net := filepath.Join(dir, "tcp")
	os.WriteFile(net, []byte(table), 0o644)

	run := mysql84Start{datadir: datadir}
	cmd := exec.Command("/bin/sh", mysql84Script(t, "agent-files", ".local", "bin", "start-mysql-8.4"))
	cmd.Env = []string{"PATH=" + bin, "MYSQL_START_LOG=" + filepath.Join(dir, "log"), "MYSQL_NET_TCP=" + net,
		"MYSQL_PREFIX=" + prefix, "MYSQL_DATADIR=" + datadir, "MYSQL_RUNDIR=" + filepath.Join(dir, "run")}
	if st.stateFails {
		cmd.Env = append(cmd.Env, "FAKE_STATE_FAILS=1")
	}
	if st.initFails {
		cmd.Env = append(cmd.Env, "FAKE_INIT_FAILS=1")
	}
	run.exit = cmd.Run()
	if st.wantStarted {
		// The server is started as a background job, so it may be asked for just after the script ends.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if got, _ := os.ReadFile(calls); strings.Contains(string(got), "nohup") {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	got, _ := os.ReadFile(calls)
	run.calls = string(got)
	logged, _ := os.ReadFile(filepath.Join(dir, "log"))
	run.log = string(logged)
	return run
}

func TestStartMysql84LeavesDataThatIsThereAloneAndStartsTheServerWithNoOptionFile(t *testing.T) {
	run := runMysql84Start(t, startState{data: "data", wantStarted: true})
	if run.exit != nil {
		t.Fatalf("%v\n%s", run.exit, run.log)
	}
	if strings.Contains(run.calls, "--initialize") || strings.Contains(run.calls, "-delete") {
		t.Errorf("existing data was initialised over or cleared: %s", run.calls)
	}
	if got, err := os.ReadFile(filepath.Join(run.datadir, "ibdata1")); err != nil || string(got) != "the user's data" {
		t.Errorf("the data was changed: %q, %v", got, err)
	}
	if !strings.Contains(run.calls, "-u mysql setsid nohup") || !strings.Contains(run.calls, "mysqld --no-defaults --basedir=") ||
		!strings.Contains(run.calls, "--bind-address=127.0.0.1") {
		t.Errorf("the server wasn't started as the mysql user, with no option file, on loopback: %s", run.calls)
	}
}

func TestStartMysql84InitialisesAnEmptyFolderBeforeStarting(t *testing.T) {
	run := runMysql84Start(t, startState{data: "empty", wantStarted: true})
	if run.exit != nil {
		t.Fatalf("%v\n%s", run.exit, run.log)
	}
	init, start := strings.Index(run.calls, "--no-defaults --initialize-insecure"), strings.Index(run.calls, "nohup")
	if init < 0 || start < 0 || init > start {
		t.Errorf("expected the folder initialised, then the server started: %s", run.calls)
	}
}

func TestStartMysql84AnInitialiseThatFailsClearsOnlyTheEmptyFolderItWasGivenAndSaysSo(t *testing.T) {
	run := runMysql84Start(t, startState{data: "empty", initFails: true})
	if run.exit == nil || !strings.Contains(run.log, "could not initialise") {
		t.Errorf("a failed initialise wasn't reported: %v\n%s", run.exit, run.log)
	}
	if strings.Contains(run.calls, "nohup") {
		t.Errorf("the server was started over a failed initialise: %s", run.calls)
	}
	if !strings.Contains(run.calls, "find "+run.datadir+" -mindepth 1 -delete") {
		t.Errorf("what the failed initialise left wasn't cleared, so every later start would fail: %s", run.calls)
	}
}

func TestStartMysql84NeverTouchesAFolderWithOtherFilesInIt(t *testing.T) {
	run := runMysql84Start(t, startState{data: "other"})
	if run.exit == nil || !strings.Contains(run.log, "not touching it") {
		t.Errorf("it didn't refuse: %v\n%s", run.exit, run.log)
	}
	if strings.Contains(run.calls, "--initialize") || strings.Contains(run.calls, "-delete") || strings.Contains(run.calls, "nohup") {
		t.Errorf("it went on: %s", run.calls)
	}
	if _, err := os.Stat(filepath.Join(run.datadir, "someone-elses-file")); err != nil {
		t.Errorf("a file in the folder was removed: %v", err)
	}
}

// The data folder is closed to the agent, who may be the one running the script: if what is in it can't be read,
// it must not be taken to be empty (which once led to its being initialised over, and cleared).
func TestStartMysql84NeverTakesAFolderItCannotReadForEmpty(t *testing.T) {
	run := runMysql84Start(t, startState{data: "data", stateFails: true})
	if run.exit == nil || !strings.Contains(run.log, "could not tell") {
		t.Errorf("it didn't refuse: %v\n%s", run.exit, run.log)
	}
	if strings.Contains(run.calls, "--initialize") || strings.Contains(run.calls, "-delete") || strings.Contains(run.calls, "nohup") {
		t.Errorf("it went on: %s", run.calls)
	}
	if got, err := os.ReadFile(filepath.Join(run.datadir, "ibdata1")); err != nil || string(got) != "the user's data" {
		t.Errorf("the data was changed: %q, %v", got, err)
	}
}

func TestStartMysql84LeavesAServerThatIsAlreadyListening(t *testing.T) {
	run := runMysql84Start(t, startState{data: "data", listening: true})
	if run.exit != nil || strings.Contains(run.calls, "nohup") || strings.Contains(run.calls, "--initialize") {
		t.Errorf("started a second server: %v\n%s", run.exit, run.calls)
	}
}

// The series is in the feature's name, so that a later one is a sibling: a name with a dot in it has to be offered,
// composed, and carry its environment and instructions into a profile.
func TestTheMysql84FeatureIsOfferedAndComposesIntoAProfile(t *testing.T) {
	lay := layout.New(t.TempDir(), repoRoot(t))
	var found *library.Feature
	for _, f := range library.List(lay.Features(), lay.FeatureDir) {
		if f.Name == "mysql-8.4" {
			f := f
			found = &f
		}
		if f.Name == "mysql" {
			t.Errorf("a feature called plain mysql is still offered: it would be taken for Oracle's MySQL: %+v", f)
		}
	}
	if found == nil || !strings.Contains(found.Description, "8.4") {
		t.Fatalf("mysql-8.4 in the library: %+v", found)
	}
	dir, err := profilegen.CreateGuided(lay, "p", []string{"mysql-8.4"})
	if err != nil {
		t.Fatal(err)
	}
	config, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if !strings.Contains(string(config), "MYSQL_UNIX_PORT: /run/mysqld/mysqld.sock") || !strings.Contains(string(config), "mysql-8.4") {
		t.Errorf("config.yaml lacks the socket variable or the feature's name:\n%s", config)
	}
	instructions, _ := os.ReadFile(filepath.Join(dir, "kit-instructions.md"))
	if !strings.Contains(string(instructions), "MySQL 8.4") {
		t.Errorf("the profile's kit-instructions.md lacks the feature's:\n%s", instructions)
	}
}
