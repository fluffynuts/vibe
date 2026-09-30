package running

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestMain doubles as a stand-in for another vibe: re-run with
// RUNNING_TEST_HOLD set, the test binary claims a folder and sits on it
// until asked to stop, the way vibe does during a session — or, with
// RUNNING_TEST_STUBBORN set too, ignores the request.
func TestMain(m *testing.M) {
	if home := os.Getenv("RUNNING_TEST_HOLD"); home != "" {
		s, err := Register(home, os.Getenv("RUNNING_TEST_TARGET"))
		if err != nil {
			os.Exit(2)
		}
		os.Stdout.WriteString("ready\n")
		if os.Getenv("RUNNING_TEST_STUBBORN") != "" {
			time.Sleep(time.Hour)
		}
		<-s.StopRequested()
		s.Release()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// startHolder runs the stand-in, waiting until it has claimed /work/a.
func startHolder(t *testing.T, home string, extraEnv ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "RUNNING_TEST_HOLD="+home, "RUNNING_TEST_TARGET=/work/a")
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	buf := make([]byte, 6)
	if _, err := out.Read(buf); err != nil {
		t.Fatalf("helper never came up: %v", err)
	}
	return cmd
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
	pid := startHolder(t, home).Process.Pid
	if got := Others(home, "/work/a"); !reflect.DeepEqual(got, []int{pid}) {
		t.Fatalf("Others = %v, want [%d]", got, pid)
	}
	killed, err := Stop(home, pid, 10*time.Second)
	if err != nil || killed {
		t.Fatalf("Stop = %v, %v; want a clean stop", killed, err)
	}
	if got := Others(home, "/work/a"); len(got) != 0 {
		t.Errorf("Others after Stop = %v, want none", got)
	}
}

func TestStopKillsAProcessThatIgnoresTheRequest(t *testing.T) {
	home := t.TempDir()
	pid := startHolder(t, home, "RUNNING_TEST_STUBBORN=1").Process.Pid
	killed, err := Stop(home, pid, 500*time.Millisecond)
	if err != nil || !killed {
		t.Fatalf("Stop = %v, %v; want it killed", killed, err)
	}
	if got := Others(home, "/work/a"); len(got) != 0 {
		t.Errorf("Others after Stop = %v, want none", got)
	}
}

func TestStopOfAGoneProcessSucceeds(t *testing.T) {
	if killed, err := Stop(t.TempDir(), 1<<30, time.Second); err != nil || killed {
		t.Errorf("Stop of a vibe that is not running = %v, %v; want false, nil", killed, err)
	}
}

func TestStopRequestedFiresOnRequest(t *testing.T) {
	home := t.TempDir()
	s, err := register(home, "/work/a", 105)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	if err := os.WriteFile(stopPath(home, 105), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.StopRequested():
	case <-time.After(5 * time.Second):
		t.Fatal("stop request never noticed")
	}
}
