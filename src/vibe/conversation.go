package main

import (
	"context"
	"regexp"
	"strings"

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
// folder the sandbox works in, resuming session when there is one.
func conversationCommand(workspace, session string) []string {
	words := make([]string, 0, len(agentArgs)+2)
	for _, a := range agentArgs {
		words = append(words, shellQuote(a))
	}
	if resumableSession.MatchString(session) {
		words = append(words, "--resume", shellQuote(session))
	}
	inner := "exec " + strings.Join(words, " ")
	if workspace != "" {
		inner = "cd " + shellQuote(workspace) + " && " + inner
	}
	return []string{"sh", "-c", asAgentScript, "converse", inner}
}

// converse is the companion page's conversation with the agent of sandbox
// name, which works in workspace (the same path inside as out).
func converse(name, workspace string) companion.Converser {
	return func(ctx context.Context, prompt, session string, delivered func(), onLine func([]byte)) error {
		return sbxrun.ExecStream(ctx, name, prompt, delivered, onLine, conversationCommand(workspace, session)...)
	}
}
