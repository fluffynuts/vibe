package sbxrun

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"vibe/internal/signin"
)

// NotSignedInError is what a sandbox that can't be woken says when sbx is signed out of Docker: sbx starts its sign-in
// flow instead, and waits for someone to confirm it in a browser.
type NotSignedInError struct {
	URL, Code string
}

func (e *NotSignedInError) Error() string {
	if e.URL == "" {
		return "sbx isn't signed in to Docker (run: sbx login)"
	}
	return fmt.Sprintf("sbx isn't signed in to Docker (sign in at %s, with the code %s, or run: sbx login)", e.URL, e.Code)
}

// Hint is where the sign-in flow sbx started waits, for a message: empty when sbx didn't start one.
func (e *NotSignedInError) Hint() string {
	if e.URL == "" {
		return ""
	}
	return fmt.Sprintf(", or open %s (code %s)", e.URL, e.Code)
}

// wakeError is a wake that failed: what sbx said when it did, which is the reason, or how the command ended when it said
// nothing.
type wakeError struct {
	said string
	err  error
}

func (e *wakeError) Error() string {
	if said := strings.TrimSpace(e.said); said != "" {
		return said
	}
	return e.err.Error()
}

func (e *wakeError) Unwrap() error { return e.err }

// pointsToSbxLogin says what sbx printed sends the user to `sbx login`. With no terminal to answer on, that is how it says
// it is signed out; the command is looked for and not the words around it, which sbx may translate.
func pointsToSbxLogin(said string) bool { return strings.Contains(said, "sbx login") }

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
		if ctx.Err() == nil && pointsToSbxLogin(said.String()) {
			return &NotSignedInError{}
		}
		return &wakeError{said: said.String(), err: err}
	}
	if time.Since(began) > slowWake {
		noteWake(name, began, "worked, slowly", said.String())
	}
	return nil
}

func noteWake(name string, began time.Time, outcome, said string) {
	if WakeLog == nil {
		return
	}
	WakeLog(fmt.Sprintf("%s sbx exec %s: %s after %s\n%s", began.Format(time.RFC3339), name, outcome,
		time.Since(began).Round(time.Millisecond), strings.TrimRight(said, "\n")))
}

var (
	signInCheckMu   sync.Mutex
	lastSignInCheck time.Time
)

const (
	// signInCheckEvery is how seldom sbx login is asked: signed out, each ask leaves a sign-in code waiting to expire, and
	// a page that is brought into view again and again would leave one each time.
	signInCheckEvery = 2 * time.Minute
	// signInCheckWithin bounds one ask.
	signInCheckWithin = 30 * time.Second
)

// explainWithSignIn is the last resort for a sandbox that couldn't be woken for a reason nothing recognised: sbx login,
// which says without words: signed out, it prints its flow's address (and waits, so it is stopped there); signed in, it
// finishes without one. So the wording of sbx's messages doesn't matter. It runs at most once in signInCheckEvery, and
// leaves err as it was when it can't tell.
func explainWithSignIn(ctx context.Context, err error) error {
	signInCheckMu.Lock()
	if !lastSignInCheck.IsZero() && time.Since(lastSignInCheck) < signInCheckEvery {
		signInCheckMu.Unlock()
		return err
	}
	lastSignInCheck = time.Now()
	signInCheckMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, signInCheckWithin)
	defer cancel()
	p, startErr := start(false, "login")
	if startErr != nil {
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
			noteWake("(sign-in check)", time.Now(), "sbx login started its flow", said.String())
			return &NotSignedInError{URL: link.URL, Code: link.Code}
		}
	}
	if ended := <-p.Done(); ended != nil || ctx.Err() != nil {
		noteWake("(sign-in check)", time.Now(), "sbx login didn't finish well", said.String())
		return err
	}
	noteWake("(sign-in check)", time.Now(), "sbx login finished without a flow: signed in", said.String())
	return fmt.Errorf("%w (sbx login finishes without asking for anything, so signing in isn't the cause)", err)
}
