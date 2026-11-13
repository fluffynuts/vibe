# The network tabs: Block and Allow that work beside the rules there are, superseded log rows, a filter, a sign that the rules are being read, and polls that change nothing

- **Status:** accepted, built 2026-11-12 and 2026-11-13 on `fix/block-unblocked-host`, **not merged at the time of writing**. The first design of Block (remove every rule that
  allows the host, wildcards included) was built and tried on 2026-11-12, then replaced on 2026-11-13 by the one below after the sbx experiments in "What sbx does".
  Three parts were built earlier and merged in PR #22 (`bccd23b`): the Network log's background read (`703995c`), the rule that polls change nothing
  (`6cad1fe`, `8d02d51`), and a fix to a test (`2a1734d`).
- **Decided:** 2026-11-12 and 2026-11-13, with the user, in play-testing against a real sandbox: each decision below came from something seen on the page or from an experiment run by hand.
- **Came from:** no plan file: designed in the conversation.

## What sbx does (found by trying it, 2026-11-13, a sandbox-scoped rule unless said)

- A deny beside an allow, or an allow beside a deny, is refused **only for the identical resource in the same scope** (`conflicts with existing allow rule`). A port makes another resource.
- A **deny beats an allow always**, not by being more specific (a portless deny beat a ported allow, a ported deny a portless allow), and a sandbox's deny beats a **global** allow, exact or wildcard, without conflict.
  So a block is added beside a wildcard or a global allow and wins for its host alone; but an allow can never be added over a deny: the deny has to go.
- `rm --sandbox X` of a global rule fails (`exists in the global policy, not in sandbox X`).
- The log is a history: an entry for each (host, decision), and a blocked host shows one more for each different rule that blocked it (the rule text differs), as the screenshots of 2026-11-13 show (eight `cdn.kernel.org:443` Blocked rows, each naming another deny rule).

## Context

- **Block failed on a host a rule allowed.** It ran `sbx policy deny`, which sbx refuses beside an allow of the identical resource. The first fix removed every covering rule: wildcards too, blocking everything under them.
- **Allow failed, or did nothing, on a host a deny rule blocked.**
- **The log showed the old row of a host beside its new one**, each with a button that offered what was no longer so.
- **The Network log was not in the list when the user got there:** a newly blocked host was only read when its tab was open, on the next poll.
- **Selecting text in a card on the Admin tab lost the selection** within a couple of seconds.

## Decision

### 1. The log says which rules allow a host, and which block it

`policy.Resolve` matched each blocked entry against the allow rules to call it Unblocked; it now keeps them, for an Unblocked **and an Allowed** entry that no deny covers: `LogEntry.AllowedBy`, a list of
`RuleRef` (id, name, resources, source, scope, `Removable`, `Wider`: the rule reaches something other than this host, and `Identical`: one of its resources is exactly the host, port and all, case aside). A
still-Blocked entry that a deny covers lists those in `BlockedBy`. Inactive and suppressed rules, and the implicit fallback, are not listed, and a rule that matches by two of its resources is listed once.

### 2. Block: remove only what sbx refuses beside, add a deny when anything else still allows the host

`Client.Block(host, removing)`, behind `POST /api/deny {host, remove: [ids]}`, in one request: it reads the rules, takes the **sandbox's own** active allow rules with a resource **identical** to the host (the one
conflict sbx has), and removes them; it then adds `sbx policy deny` **unless** the removal left nothing that allows the host, or a deny already covers it. A wildcard or a global allow is **left alone**:
the deny wins over it for this host. Global rules are never touched, so the old explanation of global rules (and its "run this command yourself") is gone from Block.
The dialog lists the rules to be removed and those that stay (with "a global rule applies to every sandbox"), says what a removed multi-host rule also allowed, and gives each command, one to a line.

**What the user confirmed is what is done:** `remove` carries the ids the dialog showed; if the rules read now are not those, nothing is done (409, "the rules have changed since you were shown them"). A conflicting rule sbx says
can't be edited stops it before anything is changed (the page says so up front, with its source). A failure after a removal says what was removed and that the rest failed (`removed X, but the block could not be added, so
the host is still allowed by another rule`); a failed removal adds no deny. The page's toast says what was done (`Blocked again: the rule that allowed it is removed`).

### 3. Allow is the mirror: the deny rules that cover the host go, then the host is allowed

An allow beside a deny does nothing, so `Client.AllowRemoving(host, removing)`, behind `POST /api/allow {host, remove}`, removes the deny rules that cover the host (ids confirmed as above), then adds the allow, unless one
covers it already. A deny it **cannot remove** (one of the global policy, or one sbx says can't be edited) stops it before anything is changed: the page explains, and for a global one gives
`sbx policy rm network --id <id>` for every sandbox. A removal that also blocked other hosts says they are allowed too. A host only the default blocks is allowed as it always was.

### 3b. A log row a later entry contradicts is history

The log has an entry for each (host, decision). For the same host (port included), an entry whose time is **earlier** than that of an entry deciding the other way is `Superseded` (`markSuperseded`, in `Resolve`, also when the rules could
not be read). Allowed and Unblocked are one decision here (the host gets through). Equal times, or a time that is missing or unreadable, supersede nothing: nothing says which is later. The page draws such a row muted with its
status struck through (title: "Superseded: a later entry for this host says otherwise"), **no button**, never "Updating…", and still under its own status's filter; the counts are of the current rows, with "N superseded" beside them;
and the blocked-hosts notice leaves it out.

### 4. A sign that a list is being read, shown at once after a change

The Network log and Network rules tabs each have the existing spinner beside their filters, in a slot of its own so nothing moves. It **fades in
only after 0.2 seconds** (a CSS transition delay, no timer), so the quick read of every poll does not flicker; and **at once** (`at-once`) for the read that follows a
rule being changed, which waits for sbx for a second or two. Nothing is disabled while it shows: the user may be allowing a host.

### 4b. A row whose rule was just changed says it is updating

**From the moment** the user confirms an Allow, Block or Remove from a **row** of the Network log, that host's row says **"Updating…"** (a small spinner, no status) and its
button is disabled, and the tab's sign is on (at once, 4): sbx takes a few seconds to run the command itself, before the list can even be read again, and a page that shows
nothing for those seconds looks as though nothing was asked (found in use: the marker used to start when sbx had answered). It lasts until a read of the log that **began
after sbx finished the command** has come back; while the command is still running no read can take it off (`at` is infinity until then), or a poll in those seconds would
show the old status again. Cancelling the confirmation marks nothing. The status column has a minimum width (9em) and the updating cell does not wrap: "Updating…" with its
spinner is wider than the words a status usually is (Unblocked), and the row was twice as tall, and the column grew and moved the others, while it showed (found in use;
`firefox.test.js` measures a row's height and every column's width in a real Firefox, as the status cell is swapped). It claims nothing about the outcome: the status is derived on the server from the log and
all the rules (a deny beats an allow; a wildcard may still cover the host), and a guess in the page would be a second copy of `Resolve`, and wrong where a user trusts it most.
A read already on its way when the change was made may say the old thing, so it does **not** take the mark off (each read is numbered, and a mark records how many had begun
when it was made); a change that failed is marked too, since part of it may have been made, and shows the truth when it is read. A rule added from the Network rules
tab marks no row: the log is not being read there.
The same holds on the **Network rules** tab for a **Remove**: the rule's row says **"Removing…"** in place of its button from the moment the user confirms, until a read of the rules that began
after sbx finished has come back (`rulesReads`, `state.removing`, kept by rule id); the tab's sign stays on while any wait. Cancelling marks nothing, and a failed removal shows the rule as it is, with its button, when read.

### 5. A host seen blocked has the Network log read at once

When the page first sees a host blocked, it reads the Network log in the background whichever tab is open, once per host (a host that stops being blocked and is blocked
again counts as new). The blocked-host notice and the tab's dot are as before; what changed is that the host is already in the list when the user clicks through.

### 6. A filter on the rules tabs, like the log's

Typing in **Filter rules** (right of the add-rule form on Network rules; **not** on Filesystem rules, which is a read-only list of the one or two rules of a sandbox that may read and write everything: a filter and
a sign were put on it and taken off again, at the user's word) keeps each rule in which anything it says matches, ignoring case:
its resources, name, source, scope, protocol, status, decision. A rule is kept **whole**. An open rule stays open as the same rule (it is named by its id, or its place
in the full list). The input is **outside** the add-rule form, so Enter in it cannot try to allow a host.

### 7. Polls that bring nothing new change nothing (the whole page)

Writing `textContent` replaces the text node even with the same words, and rebuilding a list replaces its elements: a poll every two seconds did both, clearing the
selection of anyone copying text from a card. `setText` writes only when the text differs, and `drawnAs` (or a key kept per table) rebuilds a list only when what it
shows changed. The Admin tab (13 elements were rewritten on every poll), the Network log, both rules tabs, the Profiles count and the review's comments pane (which is
also redrawn by the review's diff and colours arriving) all follow it. **Mechanical:** `untouched.test.js` watches each tab's DOM with a `MutationObserver` while polls
bring nothing new and fails on any change.

### 8. A page reading the log is not read beside in the background

Reading the log is two `sbx` calls (`policy log`, `policy ls <sandbox>`), timed by the user at about **1.7 to 1.9 seconds each** (about 3.6 s a reading, at 5 to 7% CPU: nearly all
of it waiting), more with several sandboxes, and each sandbox has its own vibe. Every vibe also read the log **in the background** (at most every `blockedEvery`, 5 seconds, on
any tab) for the blocked-hosts notice, so a page on the Network log, polling, had the same two calls made again beside its own. The page's own reading (`readLog`) now says what is
blocked as a background one would, and counts as a check of it (`readingLog`, as it begins: so one is not started while it is in flight; and again as it ends), so the
background reading only runs when nothing else has read the log for `blockedEvery`. With the log tab not open, nothing changes.

**Running the two calls of one reading at once** (`ResolvedLog`) would roughly halve a reading when `sbx` lets them overlap (a timed run of both together took 2.0 s, against about
3.6 s one after the other), but a second timed run met `WARN: docker hub refresh lock held by another process: context deadline exceeded` and took 5.5 s. It is **shelved**
until the user has spoken with the `sbx` people: more `sbx` processes in flight at once may make that lock worse, for every vibe at once.

## Consequences

- Blocking or allowing a host is one confirmation, shows what is removed and what is left, and is done in one request that refuses if the rules moved.
- A wildcard is never removed to block one host under it, and a global rule is never touched by Block.
- Removing a rule from the Network rules tab says **"Removing…"** in its row from confirmation until a read that began after sbx finished has come back, as the log's rows do (4b).
- The Firefox test of the comments pane measured a card in several steps and returned a detached element when the pane redrew between them, on a slow runner (CI's macOS
  and Windows): it now measures in one step in the page, with the same assertions (`2a1734d`).

## Not built, on purpose

- **Parallel calls in `ResolvedLog`**: shelved, see 8.
- **Removing a global rule from the page.** It would change every sandbox, and `/api/remove` is always scoped to one; the command is given instead (Allow, Remove).
- **Optimistic updating** of a row (assuming the change worked and showing the result): rejected for the reasons in 4b; the row says it is updating instead.
- **Keeping the other resources of a removed multi-resource rule** (re-adding them): the dialog says what goes with it instead.

## Not verified

- Tried by the user against a real `sbx` on 2026-11-12, **of the first design of Block** (now replaced): an exact rule, a wildcard, two rules, a global rule, the spinner, the per-row "Updating…" marker through Allow and Block, the filter and layout of the Network rules tab.
- The **sbx experiments** above were run by hand by the user, with `sbx policy ls --json` and a curl from the sandbox as the receipts.
- **Not tried by hand against a real sbx, only by unit and page tests:** the new Block and Allow flows (`Client.Block`, `Client.AllowRemoving`), the "Removing…" marker on the rules tab, and the superseded rows. The tests fake sbx's answers from what the experiments showed.
- **Not tried:** a rule from an organisation policy; a rule `sbx` lists as editable that it then refuses to remove.
- One run of `TestARepositoryBuiltToExhaustMemoryCostsOnlyTheHelper` (`internal/review`) failed in a full uncached run, with the helper's last stderr line (a register dump) in
  place of "memory". It passed alone 8 times, under CPU load 12 times, and in the whole package 3 times, and nothing in this work touches that package. A likely cause,
  unconfirmed, is `helperDied` reporting "out of memory" only when stderr has a `fatal error:` or `panic:` line, which a crash in a signal handler may not print. So that a
  recurrence can be explained, the error now also says how the helper's output began and how it ended (`diedHow`), when there was no such line.
