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
a toast, and the button can be pressed again. Once the stop has emptied the balloon, it closes (it would otherwise only say what it is
for), and keyboard focus goes back to its button. One for each task would need the server to stop a task by its id: not done.

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
  balloon pops up when it is back (once), unless nothing is left in it: work that started and ended out of sight pops nothing up (seen
  by hand 2026-11-20: it popped up only to say what it is for).
- **Wording:** "No output from the agent for at least N minutes" (the count is a lower bound, polled).
- **Accessibility:** the pulse stops under `prefers-reduced-motion` (a steady dot); the button's accessible name says what the dot means;
  routine messages are announced politely (`role="status"`), warnings at once (`role="alert"`).

## Tests

Page tests (jsdom), with the clock injected so the 5 s close is not waited for: each kind pops and closes (or doesn't) as above; the list
order and removal; a count update doesn't re-open; Stopping doesn't pop; the user-opened balloon stays; closing a live warning keeps the
amber dot; the empty text; coming back to the tab, and focus, re-pops only after a change; the dot's states and accessible names.
Firefox: the balloon on the page at every width, beside its button, moving nothing else.

## Also on this branch: the clipboard page's status line becomes toasts (decided with the user 2026-11-20)

The clipboard page had a line under its drop area (`#status`) for what was just done: mostly empty, it still took its height above the
list, and what it said stayed until something replaced it ("Copied to your clipboard." lingered). It was not given a balloon: the balloon
is for what is going on (work running), and these are outcomes of one action. Instead the line is gone and the page's toast says them:
- what was just done ("Copied…", "Already pasted…"): 3 s, as "Deleted." was already;
- a problem ("Couldn't add that…", "Not kept…", "The clipboard had nothing…"): 8 s, in the problem colour, announced as an alert;
- one to act on (open the page as `localhost`, not by IP address): stays until clicked away;
- "Sending N items…": while the paste is on its way, gone when it lands (not over the empty-file warning, which needs reading);
- any toast goes when clicked.
The held box (pastes waiting for the server to come back) is state, not an outcome, and stays as it is.

## Also on this branch: a Review file's header stays one line, and a comment gone to clears its own header (2026-11-20)

Seen by hand: a long path, with the reading order's label, wrapped a file's header onto two lines, and going to a comment in that file
(Ctrl+Enter, the arrows) left the comment's top under the header. Decided with the user:
- **The header stays one line.** The path is its folders and its name, each the header's own item: the folders, the label and the
  "not coloured" note share what the rest leaves and give way when there isn't enough (the folders from the left, `…vibe/name.html`,
  the others from their ends); the file's name never does (the header wraps first, on a window too narrow even for that). The whole
  path and label are in their titles, and the path is still all there to select or find. The tree shows where the file is.
- **Going to a comment allows for its own file's header**, not the first file's (which was all that was measured): a header can still
  wrap on a narrow window. Which comment the page is at counts from under the tallest header, for the same reason.
