# The Conversation tab's status in a balloon from an info button, with Stop for background work

- **Status:** accepted, built 2026-11-18 to 2026-11-20 on `feat/status-balloon`. Tried by hand by the user throughout (2026-11-20): the pop
  and self-close, opening it, the pulse, Stop and the close after it, work started from the Review tab, coming back to the tab and to the
  window.
- **Decided:** 2026-11-18, with the user, in three rounds; Stop in the balloon, and not popping up empty on coming back, added 2026-11-20
  while trying it; the review's fixes (no redraw on an unchanged poll, Force stop) the same day.
- **Came from:** `docs/plan-status-balloon.md`, now deleted: `git show 332c1f8:docs/plan-status-balloon.md`.

## Context

The status line above the conversation (`convo-status`) could run to three lines and more ("The agent has answered, and has something
running in the background: "…". You can send a prompt meanwhile: the work carries on."), taking screen space the conversation needs. The aim:
keep the user informed with as little noise, and as little of the screen, as possible.

## Decision

**An info button of its own**, just left of New conversation (not the model/effort one: unrelated concerns). (Since 2026-11-24 both sit at the tab bar's end, on the Conversation tab alone, and the balloon hangs from the panel's top
right below them: see `2026-11-24-conversation-tab-height.md`.) The status line's messages
move into a **balloon** that opens from it, with a ✕ top-right. Only the status line's texts: the banners with buttons (sign-in, stale, git
author, sync), the network and packages banners, and the "Stopped… Stop them too / Leave them running" offer in the conversation stay.

| Kind | Which | The balloon | The button |
|---|---|---|---|
| **Resolves itself** | background work, compacting | pops up, closes itself after 5 s; still listed when the button is clicked | a dot that **pulses** between blue and grey (as the favicon's: the same size throughout) while background work runs |
| **Needs the user** | "no output for at least N minutes", "has not stopped yet — Force stop" | pops up and **stays** until it is no longer true, or is closed | a steady amber dot |

- **One running list:** warnings first, then background tasks one to a line; each leaves when it is no longer true.
- **It pops up** when the list gains something (a new task, a task finishing while others run, a new warning). A warning's minute count
  updating changes the text in place, without re-opening it; "Stopping the agent…" (the answer to the user's own click) is listed, never
  popped. **Opened by the user**, it stays until closed (the button, the ✕, Escape), or until what it listed has all gone: then it
  closes, however it was opened, focus inside it going back to its button (amended 2026-11-23: left open, it said only what it is for).
- **A warning closed while still true** leaves the amber dot; one that clears by itself leaves the list, and closes the balloon if it was
  all there was. Amber outranks the pulse.
- **With nothing current**, the button has no dot, and opening it says "Work the agent is doing in the background will appear here."
- **Coming back** to the tab, or to the window, pops up (once) what changed while it couldn't be seen, unless nothing is left: work that
  started and ended out of sight pops nothing up (it popped up only to say what the balloon is for).
- **Stop for background work in the balloon** (**Stop it** / **Stop them all**): between turns the page had no Stop button, and so no
  way to stop it. It stops them all (`/api/conversation/stop` with `tasks`), says **Stopping…** until they have gone, and then closes the
  balloon if nothing else is in it, focus going back to its button. One that hasn't taken effect after 8 s (`FORCE_STOP_AFTER_MS`, the turn's
  own) offers **Force stop**, which ends the agent and all it started. A failure is a toast, and the button can be pressed again.
- **Drawn only when what it says changes:** the conversation is polled every second or two, and writing the balloon afresh each time took
  keyboard focus off its Stop button and had a screen reader say it again (found in review).
- **Accessibility:** the pulse stops under `prefers-reduced-motion`; the button's accessible name says what the dot means; routine messages
  are announced politely (`role="status"`), warnings at once (`role="alert"`).

## Consequences

- The conversation gains the lines the status took; the state is a dot until it's news.
- Stopping one task of several isn't possible: the server stops them all (it would need to stop a task by its id).
- Today the two kinds can't both be current (both warnings are while the agent answers, and background work is listed once it has
  answered); the precedence stands for when they can.

## Not verified

With a screen reader; in Safari and Chromium-based browsers by hand (Firefox placement is in the Firefox tests); the reduced-motion steady
dot; Force stop for background work against a task that really ignores being stopped (page tests only).
