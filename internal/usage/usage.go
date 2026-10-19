// Package usage reads how busy a sandbox is, from /proc/stat and
// /proc/meminfo as read inside it: the companion page's CPU and memory bars.
package usage

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CPUTimes are the ticks the machine has spent, since it started, busy and
// in all.
type CPUTimes struct{ Busy, Total uint64 }

// ParseStat reads the all-CPUs line of /proc/stat, and counts the cores
// from the per-CPU lines. Busy is everything but idle and waiting on disk;
// guest time is already counted in user's.
func ParseStat(out string) (CPUTimes, int, error) {
	var times CPUTimes
	var found bool
	cores := 0
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		if fields[0] != "cpu" {
			cores++
			continue
		}
		// user nice system idle iowait irq softirq steal
		if len(fields) < 9 {
			return CPUTimes{}, 0, fmt.Errorf("the cpu line of /proc/stat is too short: %q", line)
		}
		var n [8]uint64
		for i := range n {
			v, err := strconv.ParseUint(fields[i+1], 10, 64)
			if err != nil {
				return CPUTimes{}, 0, fmt.Errorf("can't read the cpu line of /proc/stat: %q", line)
			}
			n[i] = v
		}
		idle := n[3] + n[4]
		times = CPUTimes{Busy: n[0] + n[1] + n[2] + n[5] + n[6] + n[7], Total: idle + n[0] + n[1] + n[2] + n[5] + n[6] + n[7]}
		found = true
	}
	if !found {
		return CPUTimes{}, 0, fmt.Errorf("no cpu line in /proc/stat")
	}
	return times, cores, nil
}

// ParseMeminfo reads MemTotal and MemAvailable, in bytes, from /proc/meminfo.
func ParseMeminfo(out string) (total, available uint64, err error) {
	var haveTotal, haveAvailable bool
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, haveTotal = n*1024, true
		case "MemAvailable:":
			available, haveAvailable = n*1024, true
		}
	}
	if !haveTotal || !haveAvailable {
		return 0, 0, fmt.Errorf("no MemTotal and MemAvailable in /proc/meminfo")
	}
	return total, available, nil
}

// Reading is how busy a sandbox was when it was read.
type Reading struct {
	// CPU is the percentage of the time the cores were busy, over the last
	// few samples; HaveCPU is false until there are two to tell it from.
	CPU     float64
	HaveCPU bool
	Cores   int
	// MemUsed is what the sandbox can't hand back on demand: the page cache
	// isn't in it.
	MemUsed, MemTotal uint64
	At                time.Time
}

// window is how many samples the CPU figure is taken over: two intervals.
const window = 3

// Sampler turns successive reads into readings: a CPU figure needs the
// ticks as they were a moment ago.
type Sampler struct {
	history []CPUTimes
}

// Add takes the output of reading /proc/stat and /proc/meminfo together, at
// time at, and gives the reading. Output it can't read is an error, and
// leaves what came before alone.
func (s *Sampler) Add(out string, at time.Time) (Reading, error) {
	times, cores, err := ParseStat(out)
	if err != nil {
		return Reading{}, err
	}
	total, available, err := ParseMeminfo(out)
	if err != nil {
		return Reading{}, err
	}
	r := Reading{Cores: cores, MemTotal: total, MemUsed: total - min(available, total), At: at}

	// Counters that went back mean the sandbox started again: begin afresh.
	if n := len(s.history); n > 0 && (times.Total < s.history[n-1].Total || times.Busy < s.history[n-1].Busy) {
		s.history = nil
	}
	s.history = append(s.history, times)
	if len(s.history) > window {
		s.history = s.history[len(s.history)-window:]
	}
	if len(s.history) >= 2 {
		oldest := s.history[0]
		if elapsed := times.Total - oldest.Total; elapsed > 0 {
			r.CPU, r.HaveCPU = float64(times.Busy-oldest.Busy)/float64(elapsed)*100, true
		}
	}
	return r, nil
}
