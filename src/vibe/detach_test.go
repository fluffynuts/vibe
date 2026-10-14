package main

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestTheBackgroundVibeSaysWhereItsPageIs(t *testing.T) {
	url, err := awaitHeadlessReady(strings.NewReader("noise\n"+readyPrefix+"http://localhost:10000/#tok\n"), time.Second)
	if err != nil || url != "http://localhost:10000/#tok" {
		t.Errorf("awaitHeadlessReady = %q, %v", url, err)
	}
}

func TestABackgroundVibeThatEndsFirstIsSaidToHave(t *testing.T) {
	if _, err := awaitHeadlessReady(strings.NewReader("vibe: there's no sandbox\n"), time.Second); !errors.Is(err, errEndedEarly) {
		t.Errorf("awaitHeadlessReady = %v, want errEndedEarly", err)
	}
}

// Past the backstop, the background vibe is left to carry on rather than
// waited on for good.
func TestABackgroundVibeStillStartingIsLeftToCarryOn(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	if _, err := awaitHeadlessReady(r, 20*time.Millisecond); !errors.Is(err, errStillStarting) {
		t.Errorf("awaitHeadlessReady = %v, want errStillStarting", err)
	}
}
