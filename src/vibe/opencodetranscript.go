package main

import (
	"context"
	"encoding/base64"
	"strings"

	"vibe/internal/sbxrun"
)

// opencodeDataDir is where OpenCode keeps its sessions, in a sandbox's shell.
const opencodeDataDir = `"${XDG_DATA_HOME:-$HOME/.local/share}/opencode"`

// opencodeHasScript succeeds when OpenCode's database holds session. Its
// ids are stored as plain text, so a search finds them without needing
// sqlite in the sandbox; session is always one companion.SessionID passes.
func opencodeHasScript(session string) string {
	return `cd ` + opencodeDataDir + ` 2>/dev/null && grep -aqF -e ` + shellQuote(session) + ` opencode.db opencode.db-wal 2>/dev/null`
}

// opencodeFetchScript prints OpenCode's whole database (with its -shm and
// -wal files, which hold what it hasn't folded in yet) as one line of base64.
const opencodeFetchScript = `cd ` + opencodeDataDir + ` && tar -czf - opencode.db* 2>/dev/null | base64 -w0`

// opencodePutScript unpacks what opencodeFetchScript made, from stdin.
const opencodePutScript = `mkdir -p ` + opencodeDataDir + ` && cd ` + opencodeDataDir + ` && base64 -d | tar -xzf -`

// sandboxOpencodeSessions reaches into sandbox name's OpenCode database for
// the conversation, so a rebuilt sandbox's OpenCode can resume it. It is the
// whole database that is kept: OpenCode has no way to take one session out,
// and a rebuilt sandbox starts without sessions of its own.
func sandboxOpencodeSessions(name string) transcripts {
	return transcripts{
		has: func(ctx context.Context, session string) bool {
			return sbxrun.ExecSilent(ctx, name, asAgent(opencodeHasScript(session))...)
		},
		fetch: func(ctx context.Context, _ string) ([]byte, error) {
			out, err := sbxrun.ExecStdout(ctx, name, asAgent(opencodeFetchScript)...)
			if err != nil {
				return nil, err
			}
			lines := strings.Split(strings.TrimSpace(out), "\n")
			return base64.StdEncoding.DecodeString(strings.TrimSpace(lines[len(lines)-1]))
		},
		put: func(ctx context.Context, tgz []byte) error {
			return sbxrun.ExecInput(name, base64.StdEncoding.EncodeToString(tgz), asAgent(opencodePutScript)...)
		},
	}
}
