# The conversation's agent runs in auto mode, unless settings.yaml chooses another

- **Status:** accepted, built 2026-11-24 on `fix/agent-permission-mode` (a hotfix).
- **Decided:** 2026-11-24, with the user, after the agent in two sandboxes refused nearly every command (a write to `/tmp`, a compound
  command, a `for` loop with a variable) with "needs approval", though vibe started it with `--permission-mode bypassPermissions`.

## Context

The organisation's remote managed settings (fetched by Claude Code from Anthropic at each start, cached in the sandbox's
`~/.claude/remote-settings.json`) had `"disableBypassPermissionsMode": "disable"`, first seen on 2026-11-24: the cached file was first
written at 07:44 that day (the sandbox had been re-composed overnight), and the day before the same Claude Code (2.1.330) still ran
the agent in bypass. When the organisation set it isn't known; it was lifted at 10:06 the same day (the cached file was rewritten
without it). With bypass disabled, Claude Code
doesn't refuse `--permission-mode bypassPermissions` or say so: it falls back to the settings' `defaultMode`. That was Manual once the
user had picked it in `/status` (which writes `defaultMode` into `~/.claude/settings.json`), and in `-p` there is nobody to approve
anything. The session's transcript records the mode each turn actually ran in (`"permissionMode"`): `bypassPermissions` until that
morning, then `default`, then `auto` once the user set Auto, all with vibe's bypass flag.

What was checked (a throwaway `CLAUDE_CONFIG_DIR` with `defaultMode: default`; the mode read from the stream-json init event):

- Claude Code 2.1.305, 2.1.318 and 2.1.331 start in `default` for `--permission-mode bypassPermissions`, even with `defaultMode:
  bypassPermissions` as well; 2.1.291 still starts in `bypassPermissions`.
- `--permission-mode auto` starts in `auto` over a Manual `defaultMode`, fresh and resumed (`--resume`, as vibe runs it), from 2.1.305
  on; in 2.1.291 auto mode didn't work at all (it started in `default` even with `defaultMode: auto`).

## Decision

- **`auto` by default** (`defaultPermissionMode` in `src/vibe/conversation.go`): allowed under that policy, it wins over a Manual
  `defaultMode`, and nothing waits on an answer nobody can give. Its classifier refuses an action now and then that bypass would have
  run; the agent then says so.
- **`permissionMode`** chooses another: in the profile's `user-preferences.yaml`, over its `settings.yaml`, over `~/.vibe/settings.yaml`,
  as `model` and `effort` are (a generated profile's `settings.yaml` is what composing wrote: editing it stops its automatic updates),
  for an organisation that allows bypass, or to try it again. It is read when the agent next starts; a value Claude Code doesn't take is
  refused before anything runs, saying which file to fix.

## Not built

- The page doesn't choose the mode (as it does the model and effort), and doesn't say when the agent runs in another mode than was asked
  for: the stream-json init event says which mode it is in, so it could.
