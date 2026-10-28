package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

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

// isUnavailableModel says err is Cursor's refusal of the model it is set to,
// which isn't one the plan can run: said on stderr alone. It is recognised by
// what stays when the wording changes (it has, on 2026-10-28; see
// docs/samples/cursor/): an ActionRequiredError saying a model is unavailable
// and pointing at Auto, whatever the plan is called. Run again on Auto on a
// false positive, a turn could be repeated, so it takes all three.
func isUnavailableModel(err error) bool {
	said := strings.ToLower(err.Error())
	return strings.Contains(said, "actionrequirederror") &&
		strings.Contains(said, "unavailable") &&
		autoWord.MatchString(said)
}

// autoWord is Auto as a word, not the start of "automatically".
var autoWord = regexp.MustCompile(`\bauto\b`)

// What the page says when a turn is run again on Auto: that the choice is
// set to Auto too, since the plan can't run another and the selector would
// otherwise go on showing one, each prompt first refused; or why it isn't.
const (
	cursorFellBackToAuto = "Cursor can't run the model it was set to on this plan, so this turn ran on Auto, and the model is now set to Auto."
	cursorFellBackKept   = "Cursor can't run the model it was set to on this plan, so this turn ran on Auto. The model couldn't be set to Auto (%v), so prompts will go on being tried on the other first."
)

// What the clipboard page holds for the agent is handed over, for Claude, by a hook that Claude Code
// runs before each prompt. Cursor has none that vibe knows to add context, so vibe does it: before a
// turn it asks the sandbox's clipboard hook for the text it would have given, once, and puts it in
// front of the prompt (it is claimed as it's handed over, so a second asking would find nothing).
const (
	pastedStart = "@@vibe-pasted@@"
	pastedEnd   = "@@vibe-pasted-end@@"
	// claimPastedWithin bounds asking for it: a turn waits on that.
	claimPastedWithin = 10 * time.Second
)

// claimPastedScript runs the clipboard hook in its plain-text mode between markers, as a login shell
// may say something of its own first. A sandbox with no hook (a profile without the clipboard kit)
// says nothing, and that is no error.
const claimPastedScript = `printf '%s\n' '` + pastedStart + `'; ` +
	`if [ -x "$HOME/.local/bin/clipboard-hook" ]; then "$HOME/.local/bin/clipboard-hook" --text; fi; ` +
	`printf '\n%s\n' '` + pastedEnd + `'`

// pastedIn is what claimPastedScript printed between its markers, trimmed of the newlines around it:
// "" when nothing was.
func pastedIn(out string) string {
	start := strings.Index(out, pastedStart)
	if start < 0 {
		return ""
	}
	rest := out[start+len(pastedStart):]
	end := strings.Index(rest, pastedEnd)
	if end < 0 {
		return ""
	}
	return strings.Trim(rest[:end], "\r\n")
}

// itemsIn is how many items the clipboard hook's text lists: its lines that begin "- image", "- file" or
// "- text", outside the code fences that quote a pasted text (which may have such lines of its own).
func itemsIn(text string) int {
	n, fence := 0, ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case fence != "":
			if line == fence {
				fence = ""
			}
		case strings.HasPrefix(line, "```") && strings.Trim(line, "`") == "":
			fence = line
		case strings.HasPrefix(line, "- image ") || strings.HasPrefix(line, "- file ") || strings.HasPrefix(line, "- text "):
			n++
		}
	}
	return n
}

// items is n as "1 item" or "2 items".
func items(n int) string {
	if n == 1 {
		return "1 item"
	}
	return fmt.Sprintf("%d items", n)
}

// pastedFor is what the user pasted on the clipboard page for sandbox name's agent since its last
// turn, as text to put in front of the prompt, claimed as it is given. A variable for tests.
var pastedFor = func(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, claimPastedWithin)
	defer cancel()
	out, err := sbxrun.ExecStdout(ctx, name, asAgent(claimPastedScript)...)
	if err != nil {
		return "", err
	}
	return pastedIn(out), nil
}

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
// and for the page's selector; plan learns that the plan only runs Auto (the
// page shows the other models as not available), and forgets it when a model
// other than Auto runs.
func converseCursor(name, workspace string, log *turnLog, choose func() (string, error), setAuto func() error, plan *cursorPlan) companion.Converser {
	return func(ctx context.Context, prompt, session string, delivered func(), onLine func([]byte)) error {
		model, err := choose()
		if err != nil {
			return err
		}
		// Not stopping the turn if it can't be asked: what was pasted waits, unclaimed, for the next.
		pasted, err := pastedFor(ctx, name)
		clipboard := "nothing pasted"
		switch {
		case err != nil:
			note("couldn't ask sandbox '%s' what was pasted on its clipboard page, so it is not sent with this prompt: %v", name, err)
			clipboard = fmt.Sprintf("couldn't be asked (%v)", err)
		case pasted != "":
			prompt = pasted + "\n\n" + prompt
			clipboard = items(itemsIn(pasted))
			// The page marks them sent as well, but the conversation is where the user is looking.
			onLine(vibeNote("Sent with this prompt: " + items(itemsIn(pasted)) + " from the clipboard page."))
		}
		run := func(model string) error {
			pidFile := "/tmp/vibe-turn-" + turnID() + ".pid"
			log.begin("sandbox " + name + ", folder " + workspace + ", resuming '" + session + "', model '" + model + "', clipboard: " + clipboard)
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
		if err == nil && model != "" && model != cursorAuto {
			// A model other than Auto ran: the plan runs more than Auto.
			if forgotten := plan.forget(); forgotten != nil {
				note("couldn't take away the memory that Cursor's plan only runs Auto: %v", forgotten)
			}
		}
		if err != nil && model != cursorAuto && ctx.Err() == nil && isUnavailableModel(err) {
			if learned := plan.learned(err.Error(), model); learned != nil {
				note("couldn't remember that Cursor's plan only runs Auto: %v", learned)
			}
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

// maxCursorTranscriptsKB is how much of Cursor's transcripts are kept with a
// session: past it they are left out (the session is kept all the same) rather
// than make every turn copy more than a sandbox ought to hold.
const maxCursorTranscriptsKB = 10 * 1024

// cursorFetchScript writes, as one line of base64, a gzipped tar of what Cursor
// keeps of session, relative to ~/.cursor: the session itself, which --resume
// loads, and the transcripts of every chat (projects/<folder>/agent-transcripts)
// unless they come to more than maxKB. The agent searches those for what it
// can't tell from the conversation (after it was summarised, say), and for
// earlier chats, so all of them are worth having after a rebuild. It writes
// nothing for a session the sandbox doesn't have. A session id is only ever
// one that companion.SessionID passes, so it is safe in a script; it is quoted
// all the same.
func cursorFetchScript(session string, maxKB int) string {
	return `cd "$HOME/.cursor" && ls -d chats/*/` + shellQuote(session) + ` >/dev/null 2>&1 && { ` +
		`find chats -mindepth 2 -maxdepth 2 -name ` + shellQuote(session) + ` -print0; ` +
		`kb=$(du -sk projects/*/agent-transcripts 2>/dev/null | awk '{s+=$1} END {print s+0}'); ` +
		`[ "$kb" -le ` + strconv.Itoa(maxKB) + ` ] && find projects -mindepth 2 -maxdepth 2 -name agent-transcripts -print0; ` +
		`} | tar --null -czf - -T - 2>/dev/null | base64 -w0`
}

// cursorPutScript unpacks what cursorFetchScript wrote (on its stdin) into
// ~/.cursor, leaving alone what is there already: a chat had in the sandbox's
// own terminal since it was rebuilt is newer than the kept copy.
func cursorPutScript() string {
	return `mkdir -p "$HOME/.cursor" && cd "$HOME/.cursor" && base64 -d | tar --skip-old-files -xzf -`
}

// sandboxCursorSessions reaches into sandbox name's ~/.cursor for sessions,
// which Cursor keeps under chats/<a hash of the folder>/<session>/.
func sandboxCursorSessions(name string) transcripts {
	return transcripts{
		lost: cursorSessionLost,
		has: func(ctx context.Context, session string) bool {
			return sbxrun.ExecSilent(ctx, name, asAgent(
				`cd "$HOME/.cursor" 2>/dev/null && ls -d chats/*/`+shellQuote(session)+` >/dev/null 2>&1`)...)
		},
		fetch: func(ctx context.Context, session string) ([]byte, error) {
			out, err := sbxrun.ExecStdout(ctx, name, asAgent(cursorFetchScript(session, maxCursorTranscriptsKB))...)
			if err != nil {
				return nil, err
			}
			lines := strings.Split(strings.TrimSpace(out), "\n")
			return base64.StdEncoding.DecodeString(strings.TrimSpace(lines[len(lines)-1]))
		},
		put: func(ctx context.Context, tgz []byte) error {
			return sbxrun.ExecInput(name, base64.StdEncoding.EncodeToString(tgz), asAgent(cursorPutScript())...)
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
