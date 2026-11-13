# The network tabs: blocking a host a rule allows, a filter, a sign that the rules are being read, and polls that change nothing

- **Status:** accepted, built 2026-11-12 on `fix/block-unblocked-host`, **not merged at the time of writing**: `5b9e4e6`, `97af2f9`, `27fd1cb`, `4bb40cd`, `14b8488`.
  Three parts were built earlier the same day and merged in PR #22 (`bccd23b`): the Network log's background read (`703995c`), the rule that polls change nothing
  (`6cad1fe`, `8d02d51`), and a fix to a test (`2a1734d`).
- **Decided:** 2026-11-12, with the user, in play-testing against a real sandbox: each decision below came from something seen on the page.
- **Came from:** no plan file: designed in the conversation. (A plan for the one thing here not built, Block on a host that was never blocked, is not written; see below.)

## Context

- **Block on an "Unblocked" host failed.** A host that was blocked and is now let through by a rule is "Unblocked" in the Network log (`policy.Resolve`). Its Block
  button ran `sbx policy deny`, which `sbx` refuses: `deny … conflicts with existing allow rule <id>`.
- **A rule of the global policy cannot be removed for one sandbox**: `sbx policy rm network --sandbox X --id …` fails with `rule … exists in the global policy, not in
  sandbox "X"; run: sbx policy rm network --id …`. Found by trying it.
- **The Network log was not in the list when the user got there:** a newly blocked host was only read when its tab was open, on the next poll.
- **Selecting text in a card on the Admin tab lost the selection** within a couple of seconds.

## Decision

### 1. The log says which rules let a host through

`policy.Resolve` already matched each blocked entry against the allow rules to call it Unblocked; it now keeps them: `LogEntry.AllowedBy`, a list of `RuleRef` (id, name,
resources, source, scope, `Removable`, and `Wider`: the rule allows something other than this host, a wildcard or another host in the same rule, ports and case
ignored). Inactive and suppressed rules, and the implicit fallback, are not listed, and a rule that matches by two of its resources is listed once.

### 2. Block on an Unblocked host removes the rule(s), after saying what else goes with them

The button opens the confirmation with each rule described by what it allows and a short id (`Allows www.kernel.org:443 (rule ac481252…)`; sbx names a rule vibe made
with its own id, so a name equal to the id counts as no name), and the `sbx policy rm network --sandbox … --id … --force` command for each, one to a line
(`#confirm-command { white-space: pre-wrap }`). If a rule is wider it says so: *What is removed also allows `*.kernel.org`, which are blocked too.* Confirmed, it
removes each through the existing `/api/remove`, one after another, and stops at the first failure with sbx's own message. The host is blocked again; **no deny is
ever added** beside an allow.

### 3. What cannot be removed from here is explained, and nothing is half done

If **any** covering rule has no id, is not removable, or is **global** (its scope is not `sandbox:<this sandbox>`), **none** is removed (the host would be left
half unblocked) and a toast says why. For a global rule it gives the command that does remove it, for every sandbox: `sbx policy rm network --id <id>`. The Network rules
tab's **Remove** does the same for a global row, where it used to open a confirmation that sbx then refused; the dialog for the sandbox's own rules now reads "an
allow rule for this sandbox" (it said "a allowed rule"). A rule with many resources is named by its first three and a count.

### 4. A sign that a list is being read, shown at once after a change

The Network log, Network rules and Filesystem rules tabs each have the existing spinner beside their filters, in a slot of its own so nothing moves. It **fades in
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

### 5. A host seen blocked has the Network log read at once

When the page first sees a host blocked, it reads the Network log in the background whichever tab is open, once per host (a host that stops being blocked and is blocked
again counts as new). The blocked-host notice and the tab's dot are as before; what changed is that the host is already in the list when the user clicks through.

### 6. A filter on the rules tabs, like the log's

Typing in **Filter rules** (right of the add-rule form on Network rules; a new bar on Filesystem rules) keeps each rule in which anything it says matches, ignoring case:
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

- Blocking an unblocked host is one confirmation, with the rule it will remove shown; the host goes back to Blocked at the next read.
- Removing a wildcard to block one host blocks every host under it: said in the dialog, not avoided (a deny cannot coexist with the allow).
- The Firefox test of the comments pane measured a card in several steps and returned a detached element when the pane redrew between them, on a slow runner (CI's macOS
  and Windows): it now measures in one step in the page, with the same assertions (`2a1734d`).

## Not built, on purpose

- **Parallel calls in `ResolvedLog`**: shelved, see 8.
- **Block on an "Allowed" host** (never blocked) is as it was: `sbx policy deny`. Whether sbx refuses it when an allow rule covers it was not tried.
- **Removing a global rule from the page.** It would change every sandbox, and `/api/remove` is always scoped to one; the command is given instead.
- **Optimistic updating** of a row (assuming the change worked and showing the result): rejected for the reasons in 4b; the row says it is updating instead.

## Not verified

- Tried by the user against a real `sbx`: an exact rule (Block then Remove, host blocked again); a wildcard (the warning, Cancel changing nothing, all four hosts blocked again);
  two rules at once (both listed, the commands one to a line, both removed); a global rule (the explanation, the old failing removal replaced); Block on an Allowed host
  (the old dialog, cancelled); the spinner immediately after Allow, Block and Remove; the per-row "Updating…" marker through a whole Allow and Block (one line, the other columns still, the button disabled,
  and the real status when it ended); the filter, spinner and layout of the **Network rules** tab. The **Filesystem rules** tab's new bar was
  not looked at.
- **Not tried:** a rule `sbx` lists as editable that it then refuses to remove for a reason other than being global; a rule from an organisation policy.
- One run of `TestARepositoryBuiltToExhaustMemoryCostsOnlyTheHelper` (`internal/review`) failed in a full uncached run, with the helper's last stderr line (a register dump) in
  place of "memory". It passed alone 8 times, under CPU load 12 times, and in the whole package 3 times, and nothing in this work touches that package. A likely cause,
  unconfirmed, is `helperDied` reporting "out of memory" only when stderr has a `fatal error:` or `panic:` line, which a crash in a signal handler may not print.
