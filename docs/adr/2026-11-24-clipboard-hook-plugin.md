# The clipboard hook is registered by a plugin, not a managed-settings drop-in

- **Status:** accepted, built 2026-11-24 on `feat/agent-permission-mode-choice` (with the user: the same organisation's managed
  settings are the likely cause of both). Not yet tried by hand (it changes `library/`: `./test-clean`, then a re-compose).
- **Decided:** 2026-11-24, with the user, after every item pasted on the clipboard page stayed "waiting for the agent".

## Context

What is pasted on the clipboard page reaches the agent with the next prompt through a `UserPromptSubmit` hook
(`~/.local/bin/clipboard-hook`). The clipboard feature's install step registered it in a managed-settings drop-in,
`/etc/claude-code/managed-settings.d/50-vibe-clipboard.json`. With Claude Code 2.1.331 it never ran: nothing was claimed, and the
agent was never told.

Checked with a stand-in clipboard server (`VIBE_CLIPBOARD_SERVER`), so nothing real was claimed:
- the hook script, run by hand, claims (`POST /api/claim`);
- Claude Code runs it given through `--settings`, and as a plugin's `hooks/hooks.json` (`--plugin-dir`; its debug log: "Hooks:
  UserPromptSubmit [/home/agent/.local/bin/clipboard-hook] (plugin vibe-clipboard@inline) finished with status 0");
- it runs nothing from the drop-in, in `-p` or in stream-json. Claude Code watches that folder, but the managed settings it applied
  were the organisation's, from its servers ("policySettings"). That file-based managed settings are ignored alongside those is the
  likely cause, not proven: the organisation's settings couldn't be taken away to see.

## Decision

- **A plugin of vibe's** registers the hook, as `vibe-band`'s hooks are: the feature ships `.claude/skills/vibe-clipboard/` (a
  `.claude-plugin/plugin.json` and `hooks/hooks.json`), which vibe moves into `~/.local/share/vibe/plugins/` and names in
  `CLAUDE_CODE_PLUGIN_DIRS`, as it does every skill and plugin a feature carries.
- **The old drop-in goes:** the install step (now `01-remove-old-clipboard-hooks`) removes it, so that where Claude Code does read
  drop-ins the hook doesn't run twice.
- Tests: the plugin's hook is registered for `UserPromptSubmit` alone, with the right command; no install step writes a drop-in, and
  one removes the old; the kit built from a profile with the feature puts the plugin in the plugins folder and names it (it fails
  with the plugin taken away).
