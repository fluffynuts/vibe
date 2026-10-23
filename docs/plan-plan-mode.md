# A plan mode (planned: mostly discovery, not built)

Raised 2026-10-23. People expect one, and Claude's own console has it: the agent explores and proposes before it changes anything, the user approves, and only then does it build. The user of this repo talks it through conversationally and has never used it, so this plan starts with finding out what the real thing does, and what it would be here.

## What there is

- Every turn is a fresh `claude -p … --resume <session>` (`conversationCommand`, `src/vibe/conversation.go`), run with `--permission-mode bypassPermissions` (`agentArgs`), because the agent is in a sandbox and asking its permission for each tool use would have no one to answer.
- `claude --help` lists `--permission-mode` with `plan` among its modes (seen on the claude in this sandbox).
- The page already has the pieces a plan needs: model/effort drop-downs next to Send (the choice is kept in `user-preferences.yaml`), a viewer for files in the workspace (so a plan the agent wrote can be read: `viewfile.go`), tool groups that fold the exploration into one line, and the Review tab for what the agent changed afterwards.
- Other agents (OpenCode, Codex…) have no such mode, or a different one: the Conversation tab is Claude-only for now (`docs/plan-conversation-other-agents.md`), and this must stay agent-neutral where it can.

## Discovery, before any design (a real claude, as for model/effort)

Run real turns and write the findings into this document, with the output saved as fixtures:

1. What `claude -p --permission-mode plan` does in print mode: does the agent explore read-only and finish with a plan as its answer? Does it call `ExitPlanMode`, and what does that look like in the `stream-json` output (a `tool_use` with the plan text in its input? a `tool_result` that says the user must approve)? What happens when nothing approves (print mode has nobody to ask)?
2. How a plan is *approved*: does the next turn resume the session in `plan` mode and say "go ahead", or in another mode (`acceptEdits`, `bypassPermissions`)? Does a resumed session remember it was planning? Does `--permission-mode` override what the session had?
3. What it won't do in plan mode (edits and commands that change things; what about a build or a test run, which a plan may want?), and what it does when it tries (an error the agent sees and talks about).
4. Whether the plan is written to a file (and where; if it is in `~/.claude/plans`, that is inside the sandbox: the user's page can't open it, so vibe would need to fetch it, as it fetches transcripts) or only said.
5. Cost: a planning turn followed by a building turn re-reads the conversation twice (the same cache effect the model switch warns about); is the plan carried over in a way that avoids that, and how much it costs.
6. Compaction and `/clear` interplay, and the slash commands (`/plan`?) the page lists (`updateCommands`): is there a command that does this already, which the page would just expose?

## A first design, to be corrected by what is found

- **A toggle by the model and effort drop-downs: Plan / Build.** Plan runs the turn with `--permission-mode plan` (an agent-neutral setting underneath: "read-only turns"), Build with what vibe uses now. Kept like the other choices (in `user-preferences.yaml`), with the same rule that a change applies from the next prompt and is said in the info bubble.
- **A plan is shown as one:** the turn's last message in a "Plan" card with **Approve and build** (switches to Build and sends "go ahead with this plan", or whatever the discovery says is right), **Keep planning** (stays), **Save to a file** (the file viewer already opens one in the workspace). Approving is the user's act, never the agent's.
- **Nothing hidden:** while in Plan, the page says so where the user types (the Send button's label, a tint on the prompt box), so a user doesn't wonder why the agent wouldn't act.
- **The conversational user is not made to use it.** It is a toggle, off by default; the agent's behaviour when asked "let's plan first" in Build is unchanged.
- **Mechanical safety:** Plan is a *restriction*, so it is enforced by the agent's own mode and not by trust in what the agent says; a test with a stand-in agent checks the flag is on the command line for every turn while Plan is on, including turns the page sends for other reasons (a Review submit, a stale-tidy request): those must not silently escape it.

## Pitfalls to find out, not guess

- A plan that depends on running something (to learn a version, to try a command) can't be made read-only: does the agent say so, or give a worse plan?
- The user switches to Build in mid-conversation: what the agent thinks mode it is in.
- A resumed session after a vibe restart: is the mode a per-turn flag (so it's the page's to keep) or part of the session?
- Mixed use: a plan made in Plan mode by a model the user then changes (the model switch's cost warning applies).

## Tests (once designed)

Go: the flag on the command for each turn kind while Plan is on, and absent in Build; the choice kept and read back; jsdom: the toggle, the Plan card and its buttons, the label and tint, approval sending the right turn once; and the real run from the discovery kept as a fixture the stream parser is tested against.

## Order

Discovery and a written finding first (a day): it decides whether this is a small toggle or a feature with its own flow. Nothing is built before it.
