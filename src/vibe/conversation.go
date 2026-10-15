package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"vibe/internal/companion"
	"vibe/internal/sbxrun"
)

// agentArgs is how the conversation tab runs claude: one prompt on its
// stdin, its progress as stream-json on stdout. There is no terminal to
// approve anything on, and the sandbox is the boundary the agent was given,
// so it runs the way sbx itself runs it.
var agentArgs = []string{"claude", "-p", "--output-format", "stream-json", "--verbose", "--permission-mode", "bypassPermissions"}

// A session id is put on the command line, so only what claude's own look
// like gets there.
var resumableSession = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// shellQuote makes s one word for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// asAgentScript runs the command it is given as its first argument as the
// sandbox's agent user, in a login shell so that claude is on its PATH:
// sbx exec may be root or may be the agent, and this does not need to know.
const asAgentScript = `if [ "$(id -u)" = 0 ]; then exec su - "$(id -un 1000 2>/dev/null || echo agent)" -c "$1"; else exec bash -lc "$1"; fi`

// conversationCommand is what sbx exec runs for one turn: claude, in the
// folder the sandbox works in, resuming session when there is one, wrapped
// (see wrapTurn) to record in pidFile where it runs.
func conversationCommand(workspace, session, pidFile string) []string {
	words := make([]string, 0, len(agentArgs)+2)
	for _, a := range agentArgs {
		words = append(words, shellQuote(a))
	}
	if resumableSession.MatchString(session) {
		words = append(words, "--resume", shellQuote(session))
	}
	inner := strings.Join(words, " ")
	if workspace != "" {
		inner = "cd " + shellQuote(workspace) + " && " + inner
	}
	return asAgent(wrapTurn(inner, pidFile))
}

// wrapTurn runs agent, a turn's command, from a shell that writes its
// process ID to pidFile and removes it as it ends. Ending the sbx exec a
// turn runs in (the page's Stop, a new prompt) doesn't end what it runs in
// the sandbox: the agent carried on, unseen, and the next prompt started a
// second one on the same conversation. With pidFile, stopTurnScript can
// stop it, and all it started. The agent isn't exec'd, so the shell stays
// its parent: what's under it is the turn's.
func wrapTurn(agent, pidFile string) string {
	return "echo $$ > " + shellQuote(pidFile) + "; trap " + shellQuote("rm -f "+shellQuote(pidFile)) + " EXIT; " + agent
}

// turnStopWithin is how long a stopped turn's processes have to end before
// they're killed.
const turnStopWithin = "50" // tenths of a second

// stopTurnScript stops the turn wrapTurn recorded in pidFile: its shell and
// everything under it (the agent, its tool calls, what it left running) are
// asked to end, and killed if they haven't within 5 seconds, which it then
// says. A turn that has already ended has no pidFile: there's nothing to do.
func stopTurnScript(pidFile string) string {
	return `f=` + shellQuote(pidFile) + `
[ -f "$f" ] || exit 0
tree() { for c in $(pgrep -P "$1"); do tree "$c"; done; echo "$1"; }
alive() { kill -0 "$1" 2>/dev/null && ! grep -q ') Z ' "/proc/$1/stat" 2>/dev/null; }
pids=$(tree "$(cat "$f")")
kill -TERM $pids 2>/dev/null
i=0
while [ "$i" -lt ` + turnStopWithin + ` ]; do
  left=
  for p in $pids; do alive "$p" && left="$left $p"; done
  [ -z "$left" ] && { rm -f "$f"; exit 0; }
  sleep 0.1
  i=$((i + 1))
done
kill -KILL $left 2>/dev/null
rm -f "$f"
echo "killed what was still running 5s after being asked to stop:$left"
`
}

// stopTurnWithin bounds reaching into the sandbox to stop a turn.
const stopTurnWithin = 20 * time.Second

// stopTurn stops the turn recorded in pidFile in sandbox name, saying so
// when it had to kill it, or couldn't reach it. A variable for tests.
var stopTurn = func(name, pidFile string) {
	ctx, cancel := context.WithTimeout(context.Background(), stopTurnWithin)
	defer cancel()
	out, err := sbxrun.ExecStdout(ctx, name, "sh", "-c", stopTurnScript(pidFile))
	if err != nil {
		note("WARNING: couldn't stop the agent's turn in sandbox '%s', so it may still be running: %v — 'sbx exec %s ps -ef' shows", name, err, name)
		return
	}
	if said := strings.TrimSpace(out); said != "" {
		note("stopping the agent's turn in sandbox '%s': %s", name, said)
	}
}

// converse is the companion page's conversation with the agent of sandbox
// name, which works in workspace (the same path inside as out). A turn
// ended early (stopped, or by the next prompt) is stopped in the sandbox
// too, before it counts as ended, so a second can't start alongside it.
func converse(name, workspace string) companion.Converser {
	return func(ctx context.Context, prompt, session string, delivered func(), onLine func([]byte)) error {
		pidFile := "/tmp/vibe-turn-" + turnID() + ".pid"
		err := sbxrun.ExecStream(ctx, name, prompt, delivered, onLine, conversationCommand(workspace, session, pidFile)...)
		if ctx.Err() != nil {
			stopTurn(name, pidFile)
		}
		return err
	}
}

// turnID names a turn's pidFile: random, so turns never share one.
func turnID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// transcripts is how the agent's own record of a session — what --resume
// reads — is got out of a sandbox and put back into one.
type transcripts struct {
	// has says whether the sandbox's agent has session's transcript.
	has func(ctx context.Context, session string) bool
	// fetch is session's transcript, as a gzipped tar of the agent's
	// ~/.claude.
	fetch func(ctx context.Context, session string) ([]byte, error)
	// put unpacks what fetch gave into the agent's ~/.claude.
	put func(ctx context.Context, tgz []byte) error
}

// transcriptTimeout bounds each trip to the sandbox for a transcript.
const transcriptTimeout = 30 * time.Second

// transcriptKept is called each time keepingTranscript is done making a
// copy (or has given up on one): it does that in the background, and tests
// wait for it before their folders are cleaned away.
var transcriptKept = func() {}

// keepingTranscript is converse, keeping a copy of the agent's transcript
// of the session in file on the host after every turn, and handing it back
// to a sandbox that doesn't have it (one rebuilt by --re-compose, say)
// before the turn that resumes it. Without that, the agent of a rebuilt
// sandbox would have no idea what the page shows it was told. Nothing here
// stops a turn: a transcript that can't be kept or put back leaves the
// agent to say it can't find the session.
func keepingTranscript(converse companion.Converser, t transcripts, file string) companion.Converser {
	var (
		mu sync.Mutex
		// checked is the sessions known to be in the sandbox already.
		checked = map[string]bool{}
		// keeping is held while a copy is made, so two never race to file.
		keeping sync.Mutex
	)
	keep := func(session string) {
		defer transcriptKept()
		keeping.Lock()
		defer keeping.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), transcriptTimeout)
		defer cancel()
		tgz, err := t.fetch(ctx, session)
		if err != nil || len(tgz) == 0 {
			return
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return
		}
		tmp := file + ".tmp"
		if err := os.WriteFile(tmp, tgz, 0o600); err == nil {
			os.Rename(tmp, file)
		}
	}
	return func(ctx context.Context, prompt, session string, delivered func(), onLine func([]byte)) error {
		if resumableSession.MatchString(session) {
			mu.Lock()
			known := checked[session]
			mu.Unlock()
			if !known {
				check, cancel := context.WithTimeout(ctx, transcriptTimeout)
				if !t.has(check, session) {
					if tgz, err := os.ReadFile(file); err == nil {
						t.put(check, tgz)
					}
				}
				cancel()
				mu.Lock()
				checked[session] = true
				mu.Unlock()
			}
		}
		latest := session
		err := converse(ctx, prompt, session, delivered, func(line []byte) {
			var head struct {
				SessionID string `json:"session_id"`
			}
			if json.Unmarshal(line, &head) == nil && resumableSession.MatchString(head.SessionID) {
				latest = head.SessionID
			}
			onLine(line)
		})
		if resumableSession.MatchString(latest) {
			mu.Lock()
			checked[latest] = true
			mu.Unlock()
			// In the background, so the page hears the turn is over now.
			go keep(latest)
		}
		return err
	}
}

// asAgent is the sbx exec arguments that run script as the sandbox's agent
// user, in a login shell.
func asAgent(script string) []string {
	return []string{"sh", "-c", asAgentScript, "converse", script}
}

// sandboxTranscripts reaches into sandbox name's ~/.claude for transcripts.
// A session id is only ever one that resumableSession passes, so it is safe
// in a script; it is quoted all the same. Paths keep their projects/ in
// front: claude's folders there start with a dash, which ls would take for
// an option.
func sandboxTranscripts(name string) transcripts {
	pattern := func(session string) string { return shellQuote(session + ".jsonl") }
	return transcripts{
		has: func(ctx context.Context, session string) bool {
			return sbxrun.ExecSilentContext(ctx, name, asAgent(
				`cd "$HOME/.claude" 2>/dev/null && ls projects/*/`+pattern(session)+` >/dev/null 2>&1`)...)
		},
		fetch: func(ctx context.Context, session string) ([]byte, error) {
			// The transcript, and the folder of whatever it refers out to,
			// as one line of base64: the last line, should a login shell
			// say anything first.
			out, err := sbxrun.ExecCaptureContext(ctx, name, asAgent(
				`cd "$HOME/.claude" && ls projects/*/`+pattern(session)+` >/dev/null 2>&1 && `+
					`find projects -mindepth 2 -maxdepth 2 \( -name `+pattern(session)+` -o -name `+shellQuote(session)+` \) -print0 | `+
					`tar --null -czf - -T - 2>/dev/null | base64 -w0`)...)
			if err != nil {
				return nil, err
			}
			lines := strings.Split(strings.TrimSpace(out), "\n")
			return base64.StdEncoding.DecodeString(strings.TrimSpace(lines[len(lines)-1]))
		},
		put: func(ctx context.Context, tgz []byte) error {
			return sbxrun.ExecInput(name, base64.StdEncoding.EncodeToString(tgz), asAgent(
				`mkdir -p "$HOME/.claude" && cd "$HOME/.claude" && base64 -d | tar -xzf -`)...)
		},
	}
}
