package usage

import (
	"math"
	"testing"
	"time"
)

// /proc/stat of a 4-core machine, cut down to what's read.
const stat = `cpu  1000 100 400 8000 200 50 50 100 0 0
cpu0 250 25 100 2000 50 12 13 25 0 0
cpu1 250 25 100 2000 50 13 12 25 0 0
cpu2 250 25 100 2000 50 12 13 25 0 0
cpu3 250 25 100 2000 50 13 12 25 0 0
intr 12345 0 0
ctxt 99999
btime 1700000000
`

const meminfo = `MemTotal:        8192000 kB
MemFree:          500000 kB
MemAvailable:    6144000 kB
Buffers:           10000 kB
`

func TestStatGivesBusyAndTotalTimeAndTheNumberOfCores(t *testing.T) {
	got, cores, err := ParseStat(stat)
	if err != nil {
		t.Fatal(err)
	}
	// busy: user nice system irq softirq steal; idle: idle iowait. Guest time
	// is already in user's.
	if want := (CPUTimes{Busy: 1000 + 100 + 400 + 50 + 50 + 100, Total: 1700 + 8200}); got != want {
		t.Errorf("times = %+v, want %+v", got, want)
	}
	if cores != 4 {
		t.Errorf("cores = %d, want 4", cores)
	}
}

func TestStatWithNoCPULineIsAnError(t *testing.T) {
	if _, _, err := ParseStat("intr 1\nctxt 2\n"); err == nil {
		t.Error("accepted output with no cpu line")
	}
	if _, _, err := ParseStat("cpu  1 2\n"); err == nil {
		t.Error("accepted a cpu line too short to hold the times")
	}
}

func TestMeminfoGivesTotalAndAvailableInBytes(t *testing.T) {
	total, available, err := ParseMeminfo(meminfo)
	if err != nil || total != 8192000*1024 || available != 6144000*1024 {
		t.Errorf("ParseMeminfo = %d, %d, %v", total, available, err)
	}
	if _, _, err := ParseMeminfo("MemTotal: 1 kB\n"); err == nil {
		t.Error("ParseMeminfo accepted output with no MemAvailable")
	}
}

func at(sec int) time.Time { return time.Unix(1700000000, 0).Add(time.Duration(sec) * time.Second) }

func TestTheFirstSampleHasNoCPUYetButHasMemory(t *testing.T) {
	var s Sampler
	r, err := s.Add(stat+meminfo, at(0))
	if err != nil {
		t.Fatal(err)
	}
	if r.HaveCPU {
		t.Errorf("a CPU figure from one sample: %+v", r)
	}
	if r.Cores != 4 || r.MemTotal != 8192000*1024 || r.MemUsed != (8192000-6144000)*1024 {
		t.Errorf("reading = %+v", r)
	}
	if !r.At.Equal(at(0)) {
		t.Errorf("At = %v", r.At)
	}
}

func cpuOnly(busy, total uint64) string {
	return statLine(busy, total-busy) + meminfo
}

func statLine(busy, idle uint64) string {
	return "cpu  " + u(busy) + " 0 0 " + u(idle) + " 0 0 0 0 0 0\ncpu0 1 0 0 1 0 0 0 0 0 0\n"
}

func u(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestCPUIsHowMuchOfTheTimeBetweenSamplesWasBusy(t *testing.T) {
	var s Sampler
	s.Add(cpuOnly(1000, 10000), at(0))
	r, err := s.Add(cpuOnly(1750, 11000), at(3)) // 750 of the next 1000 ticks
	if err != nil || !r.HaveCPU || math.Abs(r.CPU-75) > 0.001 {
		t.Errorf("CPU = %.2f (have %v, %v), want 75", r.CPU, r.HaveCPU, err)
	}
}

// Smoothed over the last three samples (two intervals), so a burst between two
// doesn't make the bar jump about, and is forgotten once it's gone from them.
func TestCPUIsSmoothedOverTheLastThreeSamples(t *testing.T) {
	var s Sampler
	s.Add(cpuOnly(0, 1000), at(0))
	for _, step := range []struct {
		busy, total uint64
		at          int
		want        float64
	}{
		{1000, 2000, 3, 100}, // the first interval: all busy
		{1000, 3000, 6, 50},  // over both: half
		{1000, 4000, 9, 0},   // the busy one has left the window
	} {
		r, err := s.Add(cpuOnly(step.busy, step.total), at(step.at))
		if err != nil || !r.HaveCPU || math.Abs(r.CPU-step.want) > 0.001 {
			t.Errorf("at %ds: CPU = %.2f (have %v, %v), want %.0f", step.at, r.CPU, r.HaveCPU, err, step.want)
		}
	}
}

// A sandbox restarted starts its counters again: that's no CPU figure yet,
// not a negative one.
func TestCountersThatWentBackwardsStartAgain(t *testing.T) {
	var s Sampler
	s.Add(cpuOnly(5000, 10000), at(0))
	r, err := s.Add(cpuOnly(10, 100), at(3))
	if err != nil || r.HaveCPU {
		t.Errorf("CPU %.2f (have %v, %v) from counters that went back", r.CPU, r.HaveCPU, err)
	}
	r, _ = s.Add(cpuOnly(60, 200), at(6))
	if !r.HaveCPU || math.Abs(r.CPU-50) > 0.001 {
		t.Errorf("CPU = %.2f (have %v), want 50 from the fresh start", r.CPU, r.HaveCPU)
	}
}

func TestNoTimePassedMeansNoNewCPUFigure(t *testing.T) {
	var s Sampler
	s.Add(cpuOnly(100, 1000), at(0))
	r, _ := s.Add(cpuOnly(100, 1000), at(3))
	if r.HaveCPU {
		t.Errorf("a figure from no elapsed ticks: %+v", r)
	}
}

func TestOutputThatCantBeReadIsAnErrorAndLeavesTheHistoryAlone(t *testing.T) {
	var s Sampler
	s.Add(cpuOnly(100, 1000), at(0))
	if _, err := s.Add("garbage", at(3)); err == nil {
		t.Fatal("accepted garbage")
	}
	r, _ := s.Add(cpuOnly(150, 1100), at(6))
	if !r.HaveCPU || math.Abs(r.CPU-50) > 0.001 {
		t.Errorf("CPU = %.2f (have %v): the garbage spoiled the history", r.CPU, r.HaveCPU)
	}
}
