package running

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

// TestMain doubles as a stand-in for another vibe: re-run with
// RUNNING_TEST_HOLD set, the test binary claims a folder and sits on it
// until SIGTERM, the way vibe does during a session.
func TestMain(m *testing.M) {
	if home := os.Getenv("RUNNING_TEST_HOLD"); home != "" {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGTERM)
		s, err := Register(home, os.Getenv("RUNNING_TEST_TARGET"))
		if err != nil {
			os.Exit(2)
		}
		os.Stdout.WriteString("ready\n")
		<-sigs
		s.Release()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestOthersFindsLiveClaimsOnTheSameFolderOnly(t *testing.T) {
	home := t.TempDir()
	a, err := register(home, "/work/a", 101)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	b, err := register(home, "/work/b", 102)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Release()

	if got := Others(home, "/work/a"); !reflect.DeepEqual(got, []int{101}) {
		t.Errorf("Others(/work/a) = %v, want [101]", got)
	}
	if got := Others(home, "/work/c"); len(got) != 0 {
		t.Errorf("Others(/work/c) = %v, want none", got)
	}
}

func TestOthersSkipsThisProcess(t *testing.T) {
	home := t.TempDir()
	s, err := Register(home, "/work/a")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	if got := Others(home, "/work/a"); len(got) != 0 {
		t.Errorf("Others = %v, want this process left out", got)
	}
}

// A process that died without releasing leaves its file behind, unlocked:
// it must not count, and gets tidied away.
func TestOthersIgnoresAndRemovesStaleClaims(t *testing.T) {
	home := t.TempDir()
	stale := lockPath(home, 103)
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("/work/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Others(home, "/work/a"); len(got) != 0 {
		t.Errorf("Others = %v, want the stale claim ignored", got)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale claim was left behind")
	}
}

func TestReleaseDropsTheClaim(t *testing.T) {
	home := t.TempDir()
	s, err := register(home, "/work/a", 104)
	if err != nil {
		t.Fatal(err)
	}
	s.Release()
	s.Release() // twice is harmless
	if got := Others(home, "/work/a"); len(got) != 0 {
		t.Errorf("Others after Release = %v, want none", got)
	}
}

func TestStopEndsTheOtherProcessAndWaitsForIt(t *testing.T) {
	home := t.TempDir()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "RUNNING_TEST_HOLD="+home, "RUNNING_TEST_TARGET=/work/a")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	buf := make([]byte, 6)
	if _, err := out.Read(buf); err != nil {
		t.Fatalf("helper never came up: %v", err)
	}

	pid := cmd.Process.Pid
	if got := Others(home, "/work/a"); !reflect.DeepEqual(got, []int{pid}) {
		t.Fatalf("Others = %v, want [%d]", got, pid)
	}
	if err := Stop(home, pid, 10*time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := Others(home, "/work/a"); len(got) != 0 {
		t.Errorf("Others after Stop = %v, want none", got)
	}
	cmd.Wait()
}

func TestStopOfAGonePIDSucceeds(t *testing.T) {
	// Well past any real pid_max, so nothing can be signalled.
	if err := Stop(t.TempDir(), 1<<30, time.Second); err != nil {
		t.Errorf("Stop of a PID that is not running = %v, want nil", err)
	}
}
