package sbxrun

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vibe/internal/signin"
)

// NotSignedInError is what a sandbox that can't be woken says when sbx is signed out of Docker: sbx starts its sign-in
// flow instead, and waits for someone to confirm it in a browser.
type NotSignedInError struct {
	URL, Code string
}

func (e *NotSignedInError) Error() string {
	return fmt.Sprintf("sbx isn't signed in to Docker (sign in at %s, with the code %s, or run: sbx login)", e.URL, e.Code)
}

// WakeLog, when set, is given a note of each wake that failed or took long, with what sbx printed: what sbx does in a
// sandbox that can't be woken is only learnt from what it said.
var WakeLog func(entry string)

// slowWake is how long a wake takes before it is worth a note even when it worked.
const slowWake = 5 * time.Second

// wake runs something in the sandbox, which starts it when sbx has stopped it, and says why it couldn't: nil when it
// ran. sbx signed out doesn't fail that: it starts a sign-in flow and waits for the browser, so what it prints is read
// as it comes, and the wait is ended when the flow's address appears, with a *NotSignedInError.
func wake(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	began := time.Now()
	p, err := start(false, "exec", name, "--", "/bin/true")
	if err != nil {
		noteWake(name, began, "didn't run: "+err.Error(), "")
		return err
	}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			p.Stop()
		case <-stopped:
		}
	}()
	var said strings.Builder
	for line := range p.Lines() {
		if said.Len() < 64<<10 {
			said.WriteString(line + "\n")
		}
		if link, ok := signin.DeviceLink(said.String()); ok {
			p.Stop()
			for range p.Lines() {
			}
			<-p.Done()
			noteWake(name, began, "sbx started its sign-in flow", said.String())
			return &NotSignedInError{URL: link.URL, Code: link.Code}
		}
	}
	err = <-p.Done()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		noteWake(name, began, "failed: "+err.Error(), said.String())
	} else if time.Since(began) > slowWake {
		noteWake(name, began, "worked, slowly", said.String())
	}
	return err
}

func noteWake(name string, began time.Time, outcome, said string) {
	if WakeLog == nil {
		return
	}
	WakeLog(fmt.Sprintf("%s sbx exec %s: %s after %s\n%s", began.Format(time.RFC3339), name, outcome,
		time.Since(began).Round(time.Millisecond), strings.TrimRight(said, "\n")))
}
