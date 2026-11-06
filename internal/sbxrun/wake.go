package sbxrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"vibe/internal/signin"
)

// NotSignedInError is what a sandbox that can't be woken says when sbx is signed out of Docker. URL and Code are the
// sign-in flow's address and code, and are only ever set from what the host's own `sbx login` printed: what a wake prints is
// what a command run in the sandbox printed, which is the agent's to make, and a script in place of /bin/true could start a
// Docker device login of its own and print its address for a user to follow and confirm. A wake that finds the flow's address
// in that output takes it for a sign that sbx is signed out, and gives it to nobody.
type NotSignedInError struct {
	URL, Code string
	// LastResort says nothing else recognised that sbx was signed out, and `sbx login` had to be asked: it is said, so that
	// someone who sees it can say what sbx printed, and the message that nothing recognised can be learnt.
	LastResort bool

	said string
	took time.Duration
	how  string
}

func (e *NotSignedInError) Error() string {
	if e.URL == "" {
		return "sbx isn't signed in to Docker (run: sbx login)" + e.LastResortNote()
	}
	return fmt.Sprintf("sbx isn't signed in to Docker (sign in at %s, with the code %s, or run: sbx login)", e.URL, e.Code) + e.LastResortNote()
}

// LastResortNote says, for the end of a message, that it took the last resort to find out (see LastResort): empty otherwise.
func (e *NotSignedInError) LastResortNote() string {
	if !e.LastResort {
		return ""
	}
	return " (vibe didn't recognise what sbx said, and found this out by asking sbx login as a last resort: please report what sbx said, which is in ~/.vibe/sbx-wake.log)"
}

// Hint is where the sign-in flow sbx started waits, for a message: empty when there isn't one to give.
func (e *NotSignedInError) Hint() string {
	if e.URL == "" {
		return ""
	}
	return fmt.Sprintf(", or open %s (code %s)", e.URL, e.Code)
}

func (e *NotSignedInError) note() (outcome, said string, took time.Duration) {
	return e.how, e.said, e.took
}

// wakeError is a wake that failed: what sbx said when it did, which is the reason, or how the command ended when it said
// nothing. What it said is the agent's to make (see NotSignedInError), so it is shown without control characters, and short.
type wakeError struct {
	said string
	err  error
	took time.Duration
}

// maxShown is the most of what a wake printed that goes into a message: it is shown on a page, and the log has the rest.
const maxShown = 300

func (e *wakeError) Error() string {
	if said := strings.TrimSpace(printable(e.said)); said != "" {
		return shortened(said, maxShown)
	}
	return e.err.Error()
}

func (e *wakeError) Unwrap() error { return e.err }

func (e *wakeError) note() (outcome, said string, took time.Duration) {
	return "failed: " + e.err.Error(), e.said, e.took
}

// printable is s without its control characters, bar newlines and tabs: what a wake printed is shown in a terminal (the
// log, with cat) and escape sequences in it would act on it.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}

// shortened is s cut to n characters, with an ellipsis where it was.
func shortened(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// pointsToSbxLogin says what sbx printed sends the user to `sbx login`. With no terminal to answer on, that is how it says
// it is signed out; the command is looked for and not the words around it, which sbx may translate.
func pointsToSbxLogin(said string) bool { return strings.Contains(said, "sbx login") }

// WakeLog, when set, is given a note of each wake that failed or took long, with what sbx printed: what sbx does in a
// sandbox that can't be woken is only learnt from what it said.
var WakeLog func(entry string)

// slowWake is how long a wake takes before it is worth a note even when it worked.
const slowWake = 5 * time.Second

// maxWakeOutput is the most of what a command prints that is read: a command that prints more than that is stopped, since
// what it prints is the agent's to make and would otherwise be read for as long as the probe's own limit.
const maxWakeOutput = 64 << 10

var errPrintedTooMuch = errors.New("it printed more than 64 KB")

// readUntilDeviceLink reads what p prints, a line at a time, until it ends, or prints the sign-in flow's address (which
// sbx prints, with its code, on one line, so each line is looked at once), or prints more than maxWakeOutput: the last two
// stop it. It is stopped when ctx ends too. ended is how it ended (nil when it finished well).
func readUntilDeviceLink(ctx context.Context, p *Process) (said string, link signin.Link, found bool, ended error) {
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			p.Stop()
		case <-finished:
		}
	}()
	stop := func() {
		p.Stop()
		for range p.Lines() {
		}
		<-p.Done()
	}
	var out strings.Builder
	for line := range p.Lines() {
		out.WriteString(line + "\n")
		if link, ok := signin.DeviceLink(line); ok {
			stop()
			return out.String(), link, true, nil
		}
		if out.Len() > maxWakeOutput {
			stop()
			return out.String(), signin.Link{}, false, errPrintedTooMuch
		}
	}
	return out.String(), signin.Link{}, false, <-p.Done()
}

// wake runs something in the sandbox, which starts it when sbx has stopped it, and says why it couldn't: nil when it
// ran. sbx signed out doesn't fail that on a terminal: it starts a sign-in flow and waits for the browser, so what it prints
// is read as it comes, and the wait is ended when the flow's address appears, with a *NotSignedInError (which doesn't carry
// the address: see there). With no terminal it fails at once, pointing to `sbx login`, which is taken the same way.
// Failures aren't noted here: a caller that asks again and again notes some of them (see failedWakes).
func wake(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	began := time.Now()
	p, err := start(false, "exec", name, "--", "/bin/true")
	if err != nil {
		return &wakeError{err: err, took: time.Since(began)}
	}
	said, _, found, ended := readUntilDeviceLink(ctx, p)
	took := time.Since(began)
	if found {
		signedOutSeen.Store(true)
		return &NotSignedInError{said: said, took: took, how: "sbx started its sign-in flow"}
	}
	if ctx.Err() != nil {
		ended = ctx.Err()
	}
	if ended == nil {
		signedOutSeen.Store(false)
		if took > slowWake {
			noteWake(name, "worked, slowly", said, took)
		}
		return nil
	}
	if ctx.Err() == nil && pointsToSbxLogin(said) {
		signedOutSeen.Store(true)
		return &NotSignedInError{said: said, took: took, how: "failed, pointing to sbx login: " + ended.Error()}
	}
	return &wakeError{said: said, err: ended, took: took}
}

// failedWakes notes the failures of a run of wakes: the first and the last, and not each in between, so that a boot that
// takes minutes (a wake a second) doesn't fill the log, and push out what it is kept for.
type failedWakes struct {
	name        string
	first, last error
}

func (f *failedWakes) saw(err error) {
	if f.first == nil {
		f.first = err
		noteFailedWake(f.name, err)
	}
	f.last = err
}

func (f *failedWakes) finish() {
	if f.last != nil && f.last != f.first {
		noteFailedWake(f.name, f.last)
	}
}

func isNotSignedIn(err error) bool {
	var signedOut *NotSignedInError
	return errors.As(err, &signedOut)
}

func noteFailedWake(name string, err error) {
	if n, ok := err.(interface {
		note() (outcome, said string, took time.Duration)
	}); ok {
		outcome, said, took := n.note()
		noteWake(name, outcome, said, took)
	}
}

func noteWake(name, outcome, said string, took time.Duration) {
	if WakeLog == nil {
		return
	}
	WakeLog(fmt.Sprintf("%s sbx exec %s: %s after %s\n%s", time.Now().Add(-took).Format(time.RFC3339), name, outcome,
		took.Round(time.Millisecond), strings.TrimRight(printable(said), "\n")))
}

// signedOutSeen is whether the last try to wake a sandbox found sbx signed out: it is the page's to be told, so that it can
// show its banner when it is brought back into view, which is a reaction to a failure that happened anyway and not a probe.
var signedOutSeen atomic.Bool

// NotSignedInSeen says the last wake found sbx signed out, and nothing has confirmed it signed in since.
func NotSignedInSeen() bool { return signedOutSeen.Load() }

// NoteSignedIn says sbx has been confirmed signed in, by something other than a wake (sbx answering `sbx ls`, a sandbox it
// lists as running): what a wake found of it being signed out is no longer so. Without it the flag stays until a wake works,
// which with a sandbox that is running may be never, and a page that is told of it looks at the sign-in every few seconds.
func NoteSignedIn() { signedOutSeen.Store(false) }

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
// leaves err as it was when it can't tell. It is the host's own sbx that prints the address, and so the one place the
// address is given to the user.
//
// The sandbox can cause it to run. What a failed wake looks like is partly the agent's to make (the command run in the sandbox
// can print what it likes, and fail), and the failure is all that is known of why, so nothing tells a failure of sbx's from one
// the agent faked: a /bin/true that prints more than maxWakeOutput, or anything unrecognised, and fails, has the host run
// `sbx login`. That is accepted: it is at most once in signInCheckEvery, harmless when signed in (it says so and ends), and when
// signed out leaves a sign-in code to expire, which nobody is given.
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
	began := time.Now()
	p, startErr := start(false, "login")
	if startErr != nil {
		return err
	}
	said, link, found, ended := readUntilDeviceLink(ctx, p)
	took := time.Since(began)
	switch {
	case found:
		noteWake("(sign-in check)", "sbx login started its flow", said, took)
		signedOutSeen.Store(true)
		return &NotSignedInError{URL: link.URL, Code: link.Code, LastResort: true}
	case ended != nil || ctx.Err() != nil:
		noteWake("(sign-in check)", "sbx login didn't finish well", said, took)
		return fmt.Errorf("%w (vibe asked sbx login as a last resort, and it gave no answer: please report this, with ~/.vibe/sbx-wake.log)", err)
	}
	noteWake("(sign-in check)", "sbx login finished without a flow: signed in", said, took)
	return fmt.Errorf("%w (vibe asked sbx login as a last resort: it finishes without asking for anything, so signing in isn't the cause; please report this, with ~/.vibe/sbx-wake.log)", err)
}
