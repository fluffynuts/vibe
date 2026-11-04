package main

import (
	"context"
	"strings"

	"vibe/internal/companion"
	"vibe/internal/opencodestream"
	"vibe/internal/sbxrun"
)

// opencodeArgs is how the conversation tab runs OpenCode: one process for each turn (opencode run), the prompt on
// its stdin, its progress as JSON events on stdout. It needs nothing to run commands and write files without asking:
// a turn in the sandbox does so with no permission flag (seen on opencode 1.19.5), as the sandbox is the boundary, as
// it is for claude's bypassPermissions.
var opencodeArgs = []string{"opencode", "run", "--format", "json"}

// opencodeCommand is what sbx exec runs for one turn: OpenCode, in the folder the sandbox works in, resuming session
// when there is one, and asking for model and variant (OpenCode's word for effort) when there are some, wrapped (see
// wrapTurn) to record in pidFile where it runs.
func opencodeCommand(workspace, session, pidFile, model, variant string) []string {
	words := make([]string, 0, len(opencodeArgs)+6)
	for _, a := range opencodeArgs {
		words = append(words, shellQuote(a))
	}
	if model != "" {
		words = append(words, "'-m'", shellQuote(model))
	}
	if variant != "" {
		words = append(words, "'--variant'", shellQuote(variant))
	}
	if companion.SessionID.MatchString(session) {
		words = append(words, "'-s'", shellQuote(session))
	}
	inner := strings.Join(words, " ")
	if workspace != "" {
		inner = "cd " + shellQuote(workspace) + " && " + inner
	}
	return asAgent(wrapTurn(inner, pidFile))
}

// opencodeFreeModel is the model a turn runs when none is chosen. OpenCode's own default can be one that needs a
// payment or a key (it was Grok on the first real try), which fails the turn with an API key error; this one costs
// nothing, needs no sign-in, and is on OpenCode's free list both in October and in November 2026.
const opencodeFreeModel = "opencode/big-pickle"

// opencodeStream runs a turn in the sandbox. A variable for tests.
var opencodeStream = sbxrun.ExecStream

// converseOpencode is how a prompt from the page is given to OpenCode in the sandbox: as converse does for claude,
// except that what OpenCode says is first made into what the page draws (see opencodestream).
//
// OpenCode says what went wrong as an event, and then ends with exit status 1: the event is the explanation, so a
// turn that said one isn't also failed as an exit status. A failure it said nothing about (OpenCode not being
// there at all) is the turn failing, with what the shell said.
func converseOpencode(name, workspace string, choose func() (opencodeTurn, error)) companion.Converser {
	return func(ctx context.Context, prompt, session string, delivered func(), onLine func([]byte)) error {
		// Read for each turn, so a change on the page is the next prompt's.
		turn, err := choose()
		if err != nil {
			return err
		}
		pidFile := "/tmp/vibe-turn-" + turnID() + ".pid"
		saidError := false
		err = opencodeStream(ctx, name, prompt, delivered, func(raw []byte) {
			for _, line := range opencodestream.Translate(raw) {
				if strings.Contains(string(line), `"is_error":true`) && strings.Contains(string(line), `"type":"result"`) {
					saidError = true
				}
				onLine(line)
			}
		}, opencodeCommand(workspace, session, pidFile, turn.Model, turn.Variant)...)
		if ctx.Err() != nil {
			stopTurn(name, pidFile)
		}
		if err != nil && saidError && ctx.Err() == nil {
			return nil
		}
		return err
	}
}
