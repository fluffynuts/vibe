// Package clockskew tells how far this machine's clock is from the real
// time, as a web server's Date header has it. A clock that's off — most
// often a wrong timezone, which moves the UTC time the machine believes in
// by whole hours — makes the short-lived tokens Docker's sign-in hands out
// look expired (or not yet valid) the moment they arrive, and sbx then
// fails with nothing more helpful than "token has invalid claims".
package clockskew

import (
	"fmt"
	"net/http"
	"time"
)

// Reference is the server whose clock this one is checked against. A
// variable so tests can point it at a stand-in server.
var Reference = "https://github.com"

// Tolerance is how far off a clock can be before it's worth mentioning: well
// past the network's delay and the Date header's one-second resolution, and
// within the minute or so of leeway token checks usually allow.
const Tolerance = 2 * time.Minute

var client = &http.Client{Timeout: 10 * time.Second}

// Skew returns how far ahead of the reference server's clock this machine's
// is (behind, when negative), allowing for the request's round trip.
func Skew() (time.Duration, error) {
	req, err := http.NewRequest(http.MethodHead, Reference, nil)
	if err != nil {
		return 0, err
	}
	sent := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("checking the time against %s: %w", Reference, err)
	}
	resp.Body.Close()
	received := time.Now()
	date := resp.Header.Get("Date")
	if date == "" {
		return 0, fmt.Errorf("checking the time against %s: it sent no Date", Reference)
	}
	remote, err := http.ParseTime(date)
	if err != nil {
		return 0, fmt.Errorf("checking the time against %s: %w", Reference, err)
	}
	local := sent.Add(received.Sub(sent) / 2)
	return local.Sub(remote), nil
}

// Warning says what's wrong with a clock skew ahead of the real time (behind,
// when negative), or "" when it's within Tolerance.
func Warning(skew time.Duration) string {
	off := skew
	direction := "ahead of"
	if off < 0 {
		off, direction = -off, "behind"
	}
	if off <= Tolerance {
		return ""
	}
	return fmt.Sprintf("this machine's clock is about %s %s the real time — check its date, time and timezone "+
		"(signing in to Docker fails with \"token is expired\" or \"invalid claims\" while they're wrong)",
		round(off), direction)
}

// round shows d to the minute, or to the second under a minute.
func round(d time.Duration) time.Duration {
	if d < time.Minute {
		return d.Round(time.Second)
	}
	return d.Round(time.Minute)
}
