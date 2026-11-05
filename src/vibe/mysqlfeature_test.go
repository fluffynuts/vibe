package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The mysql feature's scripts are run as they are, with stubs for sudo (which records what it is asked) and
// mysqld: on the image (Debian) the server is MariaDB, which has no mysql-server package and whose mysqld
// has no --daemonize.

func mysqlStubs(t *testing.T) (dir, calls string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the scripts are for a Linux sandbox: they need GNU tools and /proc")
	}
	dir = t.TempDir()
	calls = filepath.Join(dir, "calls")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("sudo", "echo \"$@\" >>"+calls+"\n")
	write("mysqld", "")
	for _, tool := range []string{"date", "awk"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s on this machine", tool)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return dir, calls
}

func TestTheMysqlStepInstallsThePackageTheImagesAptHas(t *testing.T) {
	dir, calls := mysqlStubs(t)
	cmd := exec.Command("/bin/sh", filepath.Join(repoRoot(t), "library", "mysql", "install-scripts", "01-install-mysql"))
	cmd.Env = []string{"PATH=" + dir, "MYSQL_INSTALL_LOG=" + filepath.Join(dir, "log")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, _ := os.ReadFile(calls)
	if want := "install -y default-mysql-server file"; !strings.Contains(string(got), want) {
		t.Errorf("sudo was asked %q, want %q (mysql-server isn't in the image's apt)", got, want)
	}
	if strings.Contains(string(got), " mysql-server") {
		t.Errorf("sudo was asked for mysql-server: %q", got)
	}
}

// startMysql runs start-mysql with listening saying whether something already listens on 3306, and returns
// what sudo was asked, once mysqld's start (a background job) has been asked for when it should be.
func startMysql(t *testing.T, listening bool) string {
	t.Helper()
	dir, calls := mysqlStubs(t)
	table := "  sl  local_address rem_address   st\n"
	if listening {
		table += "   0: 0100007F:0CEA 00000000:0000 0A 00000000:00000000 00:00000000 00000000   100        0 1 1\n"
	}
	net := filepath.Join(dir, "tcp")
	if err := os.WriteFile(net, []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", filepath.Join(repoRoot(t), "library", "mysql", "agent-files", ".local", "bin", "start-mysql"))
	cmd.Env = []string{"PATH=" + dir, "MYSQL_START_LOG=" + filepath.Join(dir, "log"), "MYSQL_NET_TCP=" + net}
	if out, err := cmd.CombinedOutput(); err != nil {
		logged, _ := os.ReadFile(filepath.Join(dir, "log"))
		t.Fatalf("%v\n%s\n%s", err, out, logged)
	}
	if !listening {
		// The server is started as a background job, so it may be asked for just after the script ends.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if got, _ := os.ReadFile(calls); strings.Contains(string(got), "nohup mysqld") {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	got, _ := os.ReadFile(calls)
	return string(got)
}

func TestStartMysqlStartsTheServerInTheBackgroundWithoutDaemonize(t *testing.T) {
	got := startMysql(t, false)
	if !strings.Contains(got, "-u mysql setsid nohup mysqld --pid-file=/var/run/mysqld/mysqld.pid --socket=/var/run/mysqld/mysqld.sock") {
		t.Errorf("mysqld wasn't started as the mysql user, in the background: %q", got)
	}
	if strings.Contains(got, "--daemonize") {
		t.Errorf("mysqld was started with --daemonize, which MariaDB's doesn't take: %q", got)
	}
	if !strings.Contains(got, "mysqladmin --socket=/var/run/mysqld/mysqld.sock ping") {
		t.Errorf("it didn't wait for the server to answer: %q", got)
	}
}

func TestStartMysqlLeavesAServerThatIsAlreadyListening(t *testing.T) {
	if got := startMysql(t, true); strings.Contains(got, "mysqld --") || strings.Contains(got, "setsid") {
		t.Errorf("started a second server: %q", got)
	}
}
