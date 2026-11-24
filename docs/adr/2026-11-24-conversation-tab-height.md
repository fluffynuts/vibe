# The Conversation tab uses the height under the tab bar

- **Status:** accepted, built 2026-11-24 on `feat/agent-permission-mode-choice` (with the user: it changes the clipboard page too,
  which needs a sandbox rebuild, and this branch already needs one for the clipboard hook). Not yet tried by hand.
- **Decided:** 2026-11-24, with the user: a bar above the conversation held only the status button and New conversation, and one
  above the clipboard only a "Clipboard" heading and "Open on its own" (never used): height the conversation and the clipboard's
  history want.

## Decision

- **The status button and New conversation** move to the tab bar's end, left of the agent's light and the CPU/memory meters,
  shown on the Conversation tab alone. The tab bar scrolls sideways when narrow, and its end now sticks to the right edge, so they
  and the meters stay in reach while the tabs scroll under them.
- **The status balloon** stays in the Conversation panel, hanging from its top right, below them: inside the tab bar, which
  scrolls, it would be cut off. Everything the balloon says and does is unchanged (`2026-11-18-status-balloon.md`).
- **The bar above the conversation goes.** The sign-in and packages notices stay where they were: shown only when there is
  something to say, and then the room is used.
- **The clipboard's bar goes:** the heading (the frame says what it is), and Open on its own, which becomes a small icon in the
  clipboard page's drop area, shown only when framed (it opens the page in a tab of its own). The "not answering just now" note
  becomes a slim banner over the top of the frame, shown only while the page isn't answering.

## Tests

Page tests: the buttons in the tab bar, on the Conversation tab alone; the balloon opening from its button and closing with
Escape back to it; no bar above the conversation, no heading or Open on its own above the clipboard; the banner while the page
isn't answering. Clipboard page: the icon when framed, opening it on its own, and not on its own. Firefox, at 1500, 1000 and
420 px: the buttons in the tab bar and in reach, the conversation (and the clipboard, side by side) starting under the tab bar,
the balloon below it, under its button, on the page, moving nothing.
