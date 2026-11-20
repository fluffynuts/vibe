# The Conversation tab's status in a balloon from an info button (planned 2026-11-18; being built on `feat/status-balloon`)

## Why

The status line above the conversation (`convo-status`) can run to three lines and more: "The agent has answered, and has something
running in the background: "Build and run the full suite with NExpect 2.0.132". You can send a prompt meanwhile: the work carries on." It
takes screen space the conversation needs. The aim: keep the user informed with as little noise, and as little of the screen, as possible.

## Decided with the user (2026-11-18)

**An info button of its own**, just left of "New conversation" (never the model/effort one: unrelated concerns). The status line's
messages move into a **balloon** that opens from it, as the model/effort bubble does, with a small ✕ top-right.

**Which messages:** the status line's texts only. The banners with buttons (sign-in, stale, git author, sync) and the network and packages
banners stay as they are. The "Stopped… Stop them too / Leave them running" offer after a stop stays in the conversation.

**Stopping background work from the balloon** (added 2026-11-20, while hand-testing: between turns the page had no way to stop it, the
Stop button showing only while the agent answers): while tasks are listed, the balloon has **Stop it** / **Stop them all**, which stops
them all as "Stop them too" does (`/api/conversation/stop` with `tasks`). It says **Stopping…** until the tasks have gone; a failure is
a toast, and the button can be pressed again. One for each task would need the server to stop a task by its id: not done.

**Two kinds of message:**

| Kind | Which | The balloon | The button |
|---|---|---|---|
| **Resolves itself** | background work, compacting | pops up, closes itself after 5 s; still listed when the button is clicked | a blue dot that **pulses** while background work runs |
| **Needs the user** | "no output for at least N minutes", "has not stopped yet — Force stop" | pops up and **stays** until it is no longer true, or the user closes it | a steady dot in the warning colour (amber), no pulse |

- **One running list**: every current message, warnings first, background tasks one to a line; each leaves the list when it is no longer
  true (the background work has finished, output has come again).
- **When it pops up:** when the list gains something: a new task, a task finishing while others run, a new warning. A warning's minute
  count updating ("at least 5 minutes", then 6) changes the text in place and does not re-open it. "Stopping the agent…" (an answer to the
  user's own click) is listed but never pops it.
- **Opened by the user** (clicking the button), it stays until closed: the button again, the ✕, or Escape.
- **A warning closed while still true** leaves the amber dot on the button until it clears. A warning that clears by itself leaves the
  list, and closes the balloon if it was all there was.
- **Amber outranks the blue pulse** when both kinds are current. (Today they can't be: both warnings are while the agent answers,
  and its background work is listed once it has answered. The rule stands for when they can.)
- **With nothing current**, the button has no dot, and opening it says: "Work the agent is doing in the background will appear here."
- **Coming back:** if the list changed while the Conversation tab wasn't showing, or the browser tab or window didn't have focus, the
  balloon pops up when it is back (once).
- **Wording:** "No output from the agent for at least N minutes" (the count is a lower bound, polled).
- **Accessibility:** the pulse stops under `prefers-reduced-motion` (a steady dot); the button's accessible name says what the dot means;
  routine messages are announced politely (`role="status"`), warnings at once (`role="alert"`).

## Tests

Page tests (jsdom), with the clock injected so the 5 s close is not waited for: each kind pops and closes (or doesn't) as above; the list
order and removal; a count update doesn't re-open; Stopping doesn't pop; the user-opened balloon stays; closing a live warning keeps the
amber dot; the empty text; coming back to the tab, and focus, re-pops only after a change; the dot's states and accessible names.
Firefox: the balloon on the page at every width, beside its button, moving nothing else.
