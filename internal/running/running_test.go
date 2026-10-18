package running

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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
		// Winding down slowly, as a vibe waking its sandbox to save its
		// agent's memories does, but saying how it's getting on.
		if steps := os.Getenv("RUNNING_TEST_STEPS"); steps != "" {
			for _, step := range strings.Split(steps, ",") {
				s.Report(step)
				time.Sleep(time.Second)
			}
		}
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
	killed, err := Stop(home, pid, 10*time.Second, nil)
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
	killed, err := Stop(home, pid, 500*time.Millisecond, nil)
	if err != nil || !killed {
		t.Fatalf("Stop = %v, %v; want it killed", killed, err)
	}
	if got := Others(home, "/work/a"); len(got) != 0 {
		t.Errorf("Others after Stop = %v, want none", got)
	}
}

// A vibe that reports each step as it winds down is given as long as it
// keeps going: only silence for the whole of quiet gets it killed. Each
// step is passed on, so whoever asked can say what it's doing.
func TestStopWaitsOnAVibeThatReportsProgressAndPassesItOn(t *testing.T) {
	home := t.TempDir()
	pid := startHolder(t, home, "RUNNING_TEST_STEPS=waking,saving,stopping,done").Process.Pid
	var seen []string
	start := time.Now()
	killed, err := Stop(home, pid, 3*time.Second, func(step string) { seen = append(seen, step) })
	if err != nil || killed {
		t.Fatalf("Stop = %v, %v after %s; want a clean stop", killed, err, time.Since(start))
	}
	if got := strings.Join(seen, ","); got != "waking,saving,stopping,done" {
		t.Errorf("progress = %q, want each step once, in order", got)
	}
}

func TestStopOfAGoneProcessSucceeds(t *testing.T) {
	if killed, err := Stop(t.TempDir(), 1<<30, time.Second, nil); err != nil || killed {
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

func TestACancelRequestIsPassedOnAndAnswered(t *testing.T) {
	home := t.TempDir()
	s, err := register(home, "/work/a", os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	go func() {
		<-s.CancelRequested()
		s.AnswerCancel("cancelled the agent's turn")
	}()
	answer, err := AskToCancel(home, os.Getpid(), 5*time.Second)
	if err != nil || answer != "cancelled the agent's turn" {
		t.Errorf("answer %q, %v", answer, err)
	}
}

func TestAVibeThatNeverAnswersIsSaidToBeStuck(t *testing.T) {
	home := t.TempDir()
	s, err := register(home, "/work/a", os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	_, err = AskToCancel(home, os.Getpid(), 300*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "may be stuck") {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(cancelPath(home, os.Getpid())); !os.IsNotExist(err) {
		t.Errorf("the unanswered request was left behind: %v", err)
	}
}

// A process that has a session can claim other folders too (the Profiles
// page rebuilding another sandbox), each seen by other vibes, without
// touching its session's claim.
func TestAHoldClaimsAnotherFolderAlongsideTheSession(t *testing.T) {
	home := t.TempDir()
	s, err := register(home, "/work/a", 4242)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	h, err := hold(home, "/work/b", "rebuilding sandbox 'b'", 4242)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	if got := Others(home, "/work/b"); len(got) != 1 || got[0] != 4242 {
		t.Errorf("Others(/work/b) = %v, want the hold seen", got)
	}
	if got := Others(home, "/work/a"); len(got) != 1 || got[0] != 4242 {
		t.Errorf("Others(/work/a) = %v, want the session still seen", got)
	}
	h.Release()
	if got := Others(home, "/work/b"); len(got) != 0 {
		t.Errorf("Others(/work/b) = %v after release", got)
	}
	if got := Others(home, "/work/a"); len(got) != 1 {
		t.Errorf("Others(/work/a) = %v, want the session untouched by the hold's release", got)
	}
}

// A claim that can't be taken mustn't spoil the one that holds the file.
func TestAFailedClaimLeavesTheHolderAlone(t *testing.T) {
	home := t.TempDir()
	s, err := register(home, "/work/a", 4243)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	if _, err := register(home, "/work/b", 4243); err == nil {
		t.Fatal("a second claim on the same file was taken")
	}
	if got := Others(home, "/work/a"); len(got) != 1 {
		t.Errorf("Others(/work/a) = %v, want the first claim still seen", got)
	}
}

// A hold says what it's for, so a vibe finding the folder taken can say why.
func TestClaimsSayWhatAHoldIsFor(t *testing.T) {
	home := t.TempDir()
	s, err := register(home, "/work/a", 4244)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	h, err := hold(home, "/work/a", "rebuilding sandbox 'a'", 4245)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()
	want := []Claim{{PID: 4244}, {PID: 4245, Work: "rebuilding sandbox 'a'"}}
	if got := Claims(home, "/work/a"); !reflect.DeepEqual(got, want) {
		t.Errorf("Claims = %+v, want %+v", got, want)
	}
	if got := Others(home, "/work/a"); !reflect.DeepEqual(got, []int{4244, 4245}) {
		t.Errorf("Others = %v", got)
	}
}

func TestAHeadlessSessionSaysSo(t *testing.T) {
	home := t.TempDir()
	s, err := register(home, "/work/a", 4246)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	if got := Claims(home, "/work/a"); len(got) != 1 || got[0].Headless {
		t.Fatalf("before: Claims = %+v", got)
	}
	if err := s.MarkHeadless("/work/a"); err != nil {
		t.Fatal(err)
	}
	if got := Claims(home, "/work/a"); !reflect.DeepEqual(got, []Claim{{PID: 4246, Headless: true}}) {
		t.Errorf("after: Claims = %+v", got)
	}
}
