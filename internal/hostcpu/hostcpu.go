// Package hostcpu works out how many CPUs a sandbox may be given, and the
// choices offered when picking: sbx gives a sandbox every CPU of the host's
// unless told otherwise, which leaves the host nothing to run vibe, the
// browser and sbx itself on while the agent builds.
package hostcpu

import "runtime"

// Total is the number of CPUs the host has. A variable so tests can stand in
// for the host.
var Total = runtime.NumCPU

// Limit is the most CPUs vibe offers a sandbox: one less than the host's, so
// the host is always left one, and at least 1.
func Limit(total int) int { return max(1, total-1) }

// Default is where the choice starts when nothing is wanted: half the host's
// CPUs, rounded down, and at least 1.
func Default(total int) int { return min(Limit(total), max(1, total/2)) }

// Options are the numbers of CPUs offered for a host with total CPUs: every
// one from 1 to Limit.
func Options(total int) []int {
	out := make([]int, Limit(total))
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// Pick finds want among the options for a host with total CPUs, returning
// them and which to start on: want when it's within them, else Default (for
// a want that isn't positive), or the most offered, for a want over what the
// host can spare, which over reports.
func Pick(total, want int) (options []int, idx int, over bool) {
	options = Options(total)
	switch {
	case want <= 0:
		return options, Default(total) - 1, false
	case want > Limit(total):
		return options, len(options) - 1, true
	}
	return options, want - 1, false
}
