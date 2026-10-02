// Package hostmem works out how much memory a sandbox may be given: the
// host's memory, sizes in sbx's notation ("512m", "12g"), and the choices
// offered when picking one.
package hostmem

import (
	"fmt"
	"strconv"
	"strings"
)

// GiB is a binary gigabyte, the unit sbx's sizes are in.
const GiB = 1 << 30

// Step is the size the offered choices go up in.
const Step = 4 * GiB

// Total is the host's physical memory in bytes. A variable so tests can
// stand in for the host.
var Total = total

// Limit is the most memory vibe offers a sandbox: half the host's. The host
// is counted in whole GiB, rounded up, since what an OS reports is a little
// under what's installed (a 16 GB Linux machine reports about 15.5 GiB), and
// half of that wouldn't allow 8g. sbx itself allows up to 75% of the
// reported memory, which this always stays under.
func Limit(total uint64) uint64 {
	gib := (total + GiB - 1) / GiB
	return gib * GiB / 2
}

// Options are the sizes offered for a host with total bytes: every Step up
// to Limit. A host too small for even one Step is offered its Limit, in
// whole GiB where there's at least one.
func Options(total uint64) []string {
	limit := Limit(total)
	var out []string
	for size := uint64(Step); size <= limit; size += Step {
		out = append(out, Format(size))
	}
	if len(out) == 0 {
		size := limit / GiB * GiB
		if size == 0 {
			size = limit
		}
		out = append(out, Format(size))
	}
	return out
}

// Pick finds want among options, for a host with total bytes, returning the
// options to show and which one to start on. A want within the limit that
// isn't one of the steps ("6g") is added, in order; one over the limit, or
// that can't be read, starts on the largest option. over reports a want
// that was over the limit.
func Pick(total uint64, want string) (options []string, idx int, over bool) {
	options = Options(total)
	idx = len(options) - 1
	size, err := Parse(want)
	if err != nil {
		return options, idx, false
	}
	if size > Limit(total) {
		return options, idx, true
	}
	for i, o := range options {
		s, _ := Parse(o)
		if s == size {
			return options, i, false
		}
		if s > size {
			options = append(options[:i], append([]string{want}, options[i:]...)...)
			return options, i, false
		}
	}
	return append(options, want), len(options), false
}

// Parse reads a size in sbx's notation: a number and a binary unit, k, m, g
// or t ("512m", "12g"; "12G", "12gb" and "12GiB" too). A bare number is
// bytes.
func Parse(s string) (uint64, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.TrimSuffix(strings.TrimSuffix(t, "ib"), "b")
	mult := uint64(1)
	if n := len(t); n > 0 {
		switch t[n-1] {
		case 'k':
			mult = 1 << 10
		case 'm':
			mult = 1 << 20
		case 'g':
			mult = 1 << 30
		case 't':
			mult = 1 << 40
		}
		if mult != 1 {
			t = t[:n-1]
		}
	}
	n, err := strconv.ParseUint(t, 10, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("can't read %q as a memory size (like 8g or 512m)", s)
	}
	return n * mult, nil
}

// Format writes size in sbx's notation, in whole g where it divides evenly,
// else in m.
func Format(size uint64) string {
	if size%GiB == 0 {
		return strconv.FormatUint(size/GiB, 10) + "g"
	}
	return strconv.FormatUint(size/(1<<20), 10) + "m"
}
