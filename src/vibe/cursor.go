package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"vibe/internal/companion"
	"vibe/internal/cursorstream"
	"vibe/internal/sbxrun"
)

// cursorArgs is how the conversation tab runs Cursor's agent: the prompt on
// its stdin, its progress as stream-json on stdout, one process for each
// turn (Cursor has no way to keep one and feed it prompts). --force is what
// lets it run commands and write files without asking, and --trust skips
// the question whether to trust the folder: there is no terminal to answer
// either, and the sandbox is the boundary, as it is for claude's
// bypassPermissions.
var cursorArgs = []string{"--trust", "--force", "-p", "--output-format", "stream-json"}

// cursorPrelude finds Cursor's agent, which the installer links under both
// cursor-agent and agent: which one a sandbox's image puts on its PATH isn't
// known. Neither being there is said in so many words (exit 127, as a shell
// would), not as the shell's "agent: command not found", which doesn't say
// there were two names, or what to look at.
const cursorPrelude = `bin=cursor-agent; command -v cursor-agent >/dev/null 2>&1 || bin=agent; ` +
	`command -v "$bin" >/dev/null 2>&1 || { echo "Cursor's agent isn't installed in this sandbox: neither cursor-agent nor agent is on its PATH. 'vibe --info' says which agent the sandbox was made for." >&2; exit 127; }; `

// cursorCommand is what sbx exec runs for one turn: Cursor's agent, in the
// folder the sandbox works in, resuming session when there is one, and
// asking for model when there is one (see converseCursor: normally none),
// wrapped (see wrapTurn) to record in pidFile where it runs.
func cursorCommand(workspace, session, pidFile, model string) []string {
	words := make([]string, 0, len(cursorArgs)+4)
	if model != "" {
		words = append(words, "--model", shellQuote(model))
	}
	for _, a := range cursorArgs {
		words = append(words, shellQuote(a))
	}
	if companion.SessionID.MatchString(session) {
		words = append(words, "--resume", shellQuote(session))
	}
	inner := cursorPrelude + `"$bin" ` + strings.Join(words, " ")
	if workspace != "" {
		inner = "cd " + shellQuote(workspace) + " && " + inner
	}
	return asAgent(wrapTurn(inner, pidFile))
}

// streamTurn runs a turn in the sandbox. A variable for tests.
var streamTurn = sbxrun.ExecStream

// unavailableModel is what Cursor says on stderr when the model it is set
// to isn't one the plan can run (a free plan runs only Auto): the only place
// it says so.
const unavailableModel = "Named models unavailable"

// What the page says when a turn is run again on Auto: that the choice is
// set to Auto too, since the plan can't run another and the selector would
// otherwise go on showing one, each prompt first refused; or why it isn't.
const (
	cursorFellBackToAuto = "Cursor can't run the model it was set to on this plan (a free plan can only use Auto), so this turn ran on Auto, and the model is now set to Auto."
	cursorFellBackKept   = "Cursor can't run the model it was set to on this plan (a free plan can only use Auto), so this turn ran on Auto. The model couldn't be set to Auto (%v), so prompts will go on being tried on the other first."
)

// converseCursor is the companion page's conversation with the Cursor agent
// of sandbox name, which works in workspace. Like converse (which it stops
// a turn the way of), except that what Cursor says is first made into what
// the page draws (see cursorstream), and kept in log.
//
// choose gives the model a turn asks for, "" for none: then Cursor runs the
// one it was last set to, which is the user's own. Naming one changes it for
// their terminal too, which the page says. A model the plan can't run (a free
// plan runs only Auto) is the turn failing, said on stderr alone: the turn is
// said to have failed so, and run again, once, on Auto: the plan can't be read
// beforehand when Cursor is given an API key, as a sandbox's is. Nothing
// else is run again. setAuto makes Auto the choice then, for the next turn
// and for the page's selector.
func converseCursor(name, workspace string, log *turnLog, choose func() (string, error), setAuto func() error) companion.Converser {
	return func(ctx context.Context, prompt, session string, delivered func(), onLine func([]byte)) error {
		model, err := choose()
		if err != nil {
			return err
		}
		run := func(model string) error {
			pidFile := "/tmp/vibe-turn-" + turnID() + ".pid"
			log.begin("sandbox " + name + ", folder " + workspace + ", resuming '" + session + "', model '" + model + "'")
			err := streamTurn(ctx, name, prompt, delivered, func(raw []byte) {
				log.line(raw)
				for _, line := range cursorstream.Translate(raw) {
					onLine(line)
				}
			}, cursorCommand(workspace, session, pidFile, model)...)
			stopped := ctx.Err() != nil
			if stopped {
				stopTurn(name, pidFile)
			}
			log.end(err, stopped)
			return err
		}
		err = run(model)
		if err != nil && model != cursorAuto && ctx.Err() == nil && strings.Contains(err.Error(), unavailableModel) {
			if kept := setAuto(); kept != nil {
				onLine(vibeNote(fmt.Sprintf(cursorFellBackKept, kept)))
			} else {
				onLine(vibeNoteOfChoice(cursorFellBackToAuto))
			}
			err = run(cursorAuto)
		}
		return err
	}
}

// cursorAuto is the model every plan can run.
const cursorAuto = "auto"

// cursorSessionLost is what the page says when a conversation is carried on
// where Cursor has no saved session for it: asked to resume one it doesn't
// have, Cursor doesn't say so: it starts a new one, and answers as though
// nothing were missing. It can still find the earlier turns if its own
// transcripts (~/.cursor/projects/<folder>/agent-transcripts) are there, as
// it searches them when asked about an earlier chat: a separate store from the
// session, which vibe doesn't restore.
const cursorSessionLost = "Cursor has no saved session for this conversation in this sandbox (it may have been rebuilt), so it starts afresh. It may still find parts of the conversation by searching its own earlier chat transcripts, if they are still there."

// sandboxCursorSessions reaches into sandbox name's ~/.cursor for sessions,
// which Cursor keeps under chats/<a hash of the folder>/<session>/. A session
// id is only ever one that companion.SessionID passes, so it is safe in a
// script; it is quoted all the same.
func sandboxCursorSessions(name string) transcripts {
	pattern := func(session string) string { return shellQuote(session) }
	return transcripts{
		lost: cursorSessionLost,
		has: func(ctx context.Context, session string) bool {
			return sbxrun.ExecSilent(ctx, name, asAgent(
				`cd "$HOME/.cursor" 2>/dev/null && ls -d chats/*/`+pattern(session)+` >/dev/null 2>&1`)...)
		},
		fetch: func(ctx context.Context, session string) ([]byte, error) {
			out, err := sbxrun.ExecStdout(ctx, name, asAgent(
				`cd "$HOME/.cursor" && ls -d chats/*/`+pattern(session)+` >/dev/null 2>&1 && `+
					`find chats -mindepth 2 -maxdepth 2 -name `+pattern(session)+` -print0 | `+
					`tar --null -czf - -T - 2>/dev/null | base64 -w0`)...)
			if err != nil {
				return nil, err
			}
			lines := strings.Split(strings.TrimSpace(out), "\n")
			return base64.StdEncoding.DecodeString(strings.TrimSpace(lines[len(lines)-1]))
		},
		put: func(ctx context.Context, tgz []byte) error {
			return sbxrun.ExecInput(name, base64.StdEncoding.EncodeToString(tgz), asAgent(
				`mkdir -p "$HOME/.cursor" && cd "$HOME/.cursor" && base64 -d | tar -xzf -`)...)
		},
	}
}

// vibeNote is a line for the page's conversation that is vibe's own, not the
// agent's: it is drawn as a note.
func vibeNote(text string) []byte {
	line, _ := json.Marshal(map[string]string{"type": "vibe_note", "text": text})
	return line
}

// vibeNoteOfChoice is a vibeNote saying the model chosen was changed, which
// has the page show the choice as it is now (see the page's rereadChoice): the
// note must not say what the drop-down doesn't.
func vibeNoteOfChoice(text string) []byte {
	line, _ := json.Marshal(map[string]string{"type": "vibe_note", "text": text, "refresh": "choice"})
	return line
}
