package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"vibe/internal/companion"
	"vibe/internal/fscopy"
	"vibe/internal/sbxrun"
)

// agentArgs is how the conversation tab runs claude: one process for as long
// as it has work, prompts (and vibe's requests: a stop, a model) written to
// its stdin as stream-json, its progress as stream-json on stdout. Its
// --permission-mode is added by conversationCommand. Bypass is allowed whatever the mode, so the page can switch a running agent
// into it: Claude Code refuses that switch to an agent started without (bypass_not_launched).
var agentArgs = []string{"claude", "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--allow-dangerously-skip-permissions"}

// defaultPermissionMode is how the agent runs unless the profile chooses otherwise: the sandbox is the boundary the agent was
// given, and there is no terminal to approve anything on. An organisation's managed settings can disable bypass
// (disableBypassPermissionsMode); Claude Code then runs in its settings' defaultMode without a word, which the page says, offering
// Auto (allowed under that policy, and it wins over a Manual defaultMode).
const defaultPermissionMode = companion.DefaultPermissionMode

// effectiveMode is the mode the agent is asked for when mode is chosen ("" for nothing chosen).
func effectiveMode(mode string) string {
	if mode == "" {
		return defaultPermissionMode
	}
	return mode
}

// shellQuote makes s one word for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// asAgentScript runs the command it is given as its first argument as the
// sandbox's agent user, in a login shell so that claude is on its PATH:
// sbx exec may be root or may be the agent, and this does not need to know.
const asAgentScript = `if [ "$(id -u)" = 0 ]; then exec su - "$(id -un 1000 2>/dev/null || echo agent)" -c "$1"; else exec bash -lc "$1"; fi`

// turnChoice is the model and effort a turn is asked to use: what the user
// chose on the page, empty where they left the agent's own default.
type turnChoice struct {
	Model, Effort string
	// PermissionMode is the agent's --permission-mode, as the profile's preferences or settings choose it; "" is
	// defaultPermissionMode.
	PermissionMode string
}

// conversationCommand is what sbx exec runs for one turn: claude, in the
// folder the sandbox works in, with the model and effort chosen, resuming
// session when there is one, wrapped
// (see wrapTurn) to record in pidFile where it runs.
func conversationCommand(workspace, session, pidFile string, choice turnChoice) []string {
	words := make([]string, 0, len(agentArgs)+6)
	for _, a := range agentArgs {
		words = append(words, shellQuote(a))
	}
	words = append(words, shellQuote("--permission-mode"), shellQuote(effectiveMode(choice.PermissionMode)))
	if choice.Model != "" {
		words = append(words, "--model", shellQuote(choice.Model))
	}
	if choice.Effort != "" {
		words = append(words, "--effort", shellQuote(choice.Effort))
	}
	if companion.SessionID.MatchString(session) {
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
	// A stop that got here first left pidFile+".stop": the agent never starts.
	// It is looked for after the pid is recorded, and stopTurnScript leaves it
	// before it looks for the pid, so one of them always sees the other.
	pid, stop := shellQuote(pidFile), shellQuote(pidFile+".stop")
	return "echo $$ > " + pid + "; trap " + shellQuote("rm -f "+shellQuote(pidFile)+" "+shellQuote(pidFile+".stop")) + " EXIT; [ -e " + stop + " ] && exit 0; " + agent
}

// turnStopWithin is how long a stopped turn's processes have to end before
// they're killed.
const turnStopWithin = "50" // tenths of a second

// stopTurnScript stops the turn wrapTurn recorded in pidFile: its shell and
// everything under it (the agent, its tool calls, what it left running) are
// asked to end, and killed if they haven't within 5 seconds, which it then
// says. A turn that has already ended has no pidFile: there's nothing to do,
// but the stop is left for a turn that has yet to record itself (the stop got
// to the sandbox before its shell ran) to find, so that it never starts.
func stopTurnScript(pidFile string) string {
	return `f=` + shellQuote(pidFile) + `
touch "$f.stop"
[ -f "$f" ] || exit 0
tree() { for c in $(pgrep -P "$1"); do tree "$c"; done; echo "$1"; }
alive() { kill -0 "$1" 2>/dev/null && ! grep -q ') Z ' "/proc/$1/stat" 2>/dev/null; }
pids=$(tree "$(cat "$f")")
kill -TERM $pids 2>/dev/null
i=0
while [ "$i" -lt ` + turnStopWithin + ` ]; do
  left=
  for p in $pids; do alive "$p" && left="$left $p"; done
  [ -z "$left" ] && { rm -f "$f" "$f.stop"; exit 0; }
  sleep 0.1
  i=$((i + 1))
done
kill -KILL $left 2>/dev/null
rm -f "$f" "$f.stop"
echo "killed what was still running 5s after being asked to stop:$left"
`
}

// stopTurnWithin bounds reaching into the sandbox to stop a turn.
const stopTurnWithin = 20 * time.Second

// stopTurn stops the turn recorded in pidFile in sandbox name, saying so
// when it had to kill it. An error says it couldn't be confirmed that the
// agent has ended. A variable for tests.
var stopTurn = func(name, pidFile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), stopTurnWithin)
	defer cancel()
	out, err := sbxrun.ExecStdout(ctx, name, "sh", "-c", stopTurnScript(pidFile))
	if err != nil {
		return fmt.Errorf("couldn't stop the agent's turn in sandbox '%s', so it may still be running: %w — 'sbx exec %s ps -ef' shows", name, err, name)
	}
	if said := strings.TrimSpace(out); said != "" {
		note("stopping the agent's turn in sandbox '%s': %s", name, said)
	}
	return nil
}

// conversationPidFile is where the agent's wrapper records its process (see
// wrapTurn): one for the conversation (a sandbox has one), not one per turn,
// so that a vibe started after one that died finds the agent it left (it
// runs on, unseen, its output going nowhere) and ends it.
const conversationPidFile = "/tmp/vibe-conversation.pid"

// agentSession is a command in the sandbox whose input stays open (see
// sbxrun.Session).
type agentSession interface {
	WriteLine(line []byte) error
	CloseInput() error
	Done() <-chan struct{}
	Err() error
}

// startSession starts args in sandbox name with its input kept open; a
// variable for tests.
var startSession = func(ctx context.Context, name string, onLine func([]byte), args ...string) (agentSession, error) {
	return sbxrun.ExecSession(ctx, name, onLine, args...)
}

// endLeftover ends an agent a vibe that died left running in sandbox name,
// if any, and clears the way for the next: the stop it leaves behind would
// keep the next agent from starting (see wrapTurn). A variable for tests.
var endLeftover = func(name, pidFile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), stopTurnWithin)
	defer cancel()
	script := `sh -c "$1"; rm -f "$2"`
	out, err := sbxrun.ExecStdout(ctx, name, "sh", "-c", script, "endleftover", stopTurnScript(pidFile), pidFile+".stop")
	if err != nil {
		return fmt.Errorf("couldn't end the agent a previous vibe left running in sandbox '%s': %w", name, err)
	}
	if said := strings.TrimSpace(out); said != "" {
		note("ending the agent a previous vibe left running in sandbox '%s': %s", name, said)
	}
	return nil
}

// startAgent is the companion page's way of starting the agent of sandbox
// name, which works in workspace (where the sandbox has it: see
// sbxrun.FolderInSandbox), with the
// model and effort chosen for it: what a previous vibe left running is ended
// first, or two would answer.
func startAgent(name, workspace string, choose func() (turnChoice, error)) companion.StartAgent {
	return func(ctx context.Context, session string, onLine func([]byte)) (companion.Agent, error) {
		choice, err := choose()
		if err != nil {
			return nil, err
		}
		if err := endLeftover(name, conversationPidFile); err != nil {
			return nil, err
		}
		inside, err := sbxrun.FolderInSandbox(ctx, name, workspace)
		if err != nil {
			return nil, err
		}
		// Not ctx: that bounds the start, and the agent outlives it.
		sess, err := startSession(context.Background(), name, onLine, conversationCommand(inside, session, conversationPidFile, choice)...)
		if err != nil {
			return nil, err
		}
		applied := choice
		applied.PermissionMode = effectiveMode(choice.PermissionMode)
		return &claudeAgent{name: name, pidFile: conversationPidFile, sess: sess, choose: choose, applied: applied}, nil
	}
}

// claudeAgent is claude in the sandbox, with a stream-json input: what the
// page asks of it is written there.
type claudeAgent struct {
	name, pidFile string
	sess          agentSession
	// choose is the model, effort and permission mode the user chose, read
	// for each prompt; applied is what the agent was last told (its mode as
	// asked for: "" never).
	choose  func() (turnChoice, error)
	mu      sync.Mutex
	applied turnChoice
	sent    int
}

func (a *claudeAgent) write(v interface{}) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return a.sess.WriteLine(line)
}

// control sends the agent a control request. mu is held.
func (a *claudeAgent) control(request map[string]interface{}) error {
	a.sent++
	return a.write(map[string]interface{}{"type": "control_request", "request_id": fmt.Sprintf("vibe-%d", a.sent), "request": request})
}

// Prompt gives the agent text, after the model and effort chosen since it
// was last told them: a choice that can't be used sends nothing.
func (a *claudeAgent) Prompt(text, id string) error {
	choice, err := a.choose()
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.apply(choice); err != nil {
		return err
	}
	return a.write(map[string]interface{}{"type": "user", "uuid": id, "message": map[string]interface{}{"role": "user", "content": text}})
}

// apply tells the agent the model, effort and permission mode chosen, where they changed.
// The agent's own default model can't be asked for by name: a choice back to
// it holds from the next agent on. mu is held.
func (a *claudeAgent) apply(choice turnChoice) error {
	if choice.Model != a.applied.Model && choice.Model != "" {
		if err := a.control(map[string]interface{}{"subtype": "set_model", "model": choice.Model}); err != nil {
			return err
		}
		a.applied.Model = choice.Model
	}
	if choice.Effort != a.applied.Effort {
		level := choice.Effort
		if level == "" {
			level = "auto"
		}
		if err := a.control(map[string]interface{}{"subtype": "apply_flag_settings", "settings": map[string]interface{}{"effortLevel": level}}); err != nil {
			return err
		}
		a.applied.Effort = choice.Effort
	}
	if mode := effectiveMode(choice.PermissionMode); mode != a.applied.PermissionMode {
		a.sent++
		// Named so the page knows the answer: a mode the settings disable (bypass, by an organisation's policy) is refused there.
		request := map[string]interface{}{"subtype": "set_permission_mode", "mode": mode}
		if err := a.write(map[string]interface{}{"type": "control_request", "request_id": companion.ModeRequestID(a.sent), "request": request}); err != nil {
			return err
		}
		a.applied.PermissionMode = mode
	}
	return nil
}

// AskedPermissionMode is the mode the agent was last asked for: the one it started in, or the last change since. What it is in
// is what it says (see companion.ModeAsker).
func (a *claudeAgent) AskedPermissionMode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applied.PermissionMode
}

// Interrupt stops the turn under way, sparing what runs in the background
// (shell commands and background agents alike): stopping that is the user's
// choice, made after (StopTask).
func (a *claudeAgent) Interrupt() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.control(map[string]interface{}{"subtype": "interrupt", "spare_background_tasks": true})
}

// StopTask stops one thing the agent has running in the background.
func (a *claudeAgent) StopTask(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.control(map[string]interface{}{"subtype": "stop_task", "task_id": id})
}

// End closes the agent's input: it ends once it has nothing left to do.
func (a *claudeAgent) End() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sess.CloseInput()
}

// agentEndsWithin is how long a killed agent's exec has to end.
const agentEndsWithin = 10 * time.Second

// Kill ends the agent and everything under it, in the sandbox, at once.
func (a *claudeAgent) Kill() error {
	if err := stopTurn(a.name, a.pidFile); err != nil {
		return fmt.Errorf("%w: %v", companion.ErrStopUnconfirmed, err)
	}
	select {
	case <-a.sess.Done():
	case <-time.After(agentEndsWithin):
	}
	return nil
}

func (a *claudeAgent) Done() <-chan struct{} { return a.sess.Done() }
func (a *claudeAgent) Err() error            { return a.sess.Err() }

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

// keepingTranscript is startAgent, keeping a copy of the agent's transcript
// of the session in file on the host after every answer, and handing it back
// to a sandbox that doesn't have it (one rebuilt by --re-compose, say) before
// an agent resumes it. Without that, the agent of a rebuilt sandbox would
// have no idea what the page shows it was told. Nothing here stops the
// agent: a transcript that can't be kept or put back leaves it to say it
// can't find the session.
func keepingTranscript(start companion.StartAgent, t transcripts, file string) companion.StartAgent {
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
		fscopy.WriteWhole(file, tgz)
	}
	return func(ctx context.Context, session string, onLine func([]byte)) (companion.Agent, error) {
		if companion.SessionID.MatchString(session) {
			mu.Lock()
			known := checked[session]
			mu.Unlock()
			if !known {
				check, cancel := context.WithTimeout(ctx, transcriptTimeout)
				if !t.has(check, session) {
					if tgz, err := fscopy.ReadFile(file); err == nil {
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
		return start(ctx, session, func(line []byte) {
			var head struct {
				Type      string `json:"type"`
				SessionID string `json:"session_id"`
			}
			json.Unmarshal(line, &head)
			if companion.SessionID.MatchString(head.SessionID) {
				latest = head.SessionID
				mu.Lock()
				checked[latest] = true
				mu.Unlock()
			}
			onLine(line)
			if head.Type == "result" && companion.SessionID.MatchString(latest) {
				// In the background, so the page hears the answer now.
				go keep(latest)
			}
		})
	}
}

// asAgent is the sbx exec arguments that run script as the sandbox's agent
// user, in a login shell.
func asAgent(script string) []string {
	return []string{"sh", "-c", asAgentScript, "converse", script}
}

// sandboxTranscripts reaches into sandbox name's ~/.claude for transcripts.
// A session id is only ever one that companion.SessionID passes, so it is safe
// in a script; it is quoted all the same. Paths keep their projects/ in
// front: claude's folders there start with a dash, which ls would take for
// an option.
func sandboxTranscripts(name string) transcripts {
	pattern := func(session string) string { return shellQuote(session + ".jsonl") }
	return transcripts{
		has: func(ctx context.Context, session string) bool {
			return sbxrun.ExecSilent(ctx, name, asAgent(
				`cd "$HOME/.claude" 2>/dev/null && ls projects/*/`+pattern(session)+` >/dev/null 2>&1`)...)
		},
		fetch: func(ctx context.Context, session string) ([]byte, error) {
			// The transcript, and the folder of whatever it refers out to,
			// as one line of base64: the last line, should a login shell
			// say anything first.
			out, err := sbxrun.ExecStdout(ctx, name, asAgent(
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
