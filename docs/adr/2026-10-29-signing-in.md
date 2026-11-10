# Signing in from vibe: sbx's sign-in, the agent's sign-in, the network policy, and getting sbx ready before anything else

- **Status:** accepted, built 2026-10-30 (`feat/sign-in`) and 2026-11-06 (`feat/signed-out-start`), both merged. What is not built is in
  `docs/plan-sign-in-next-steps.md`.
- **Decided:** 2026-10-29, with the user; refined by three real runs on fresh VMs (2026-10-30) and the user's own `sbx logout` (2026-11-06).
- **Came from:** `docs/plan-sign-in.md`, now deleted: `git show b15e0e1:docs/plan-sign-in.md` (it holds the long record of what each sbx
  command printed).

## Context

Signing in, to sbx and to the agent, was the highest-friction point, and both needed a terminal. The aim: as painless as possible from the
page, with the plain commands shown for anyone who would rather do it by hand, and the terminal as optional as the companion page. Claude
first; Cursor later, sharing the structure. vibe must not assume a Claude CLI on the host.

## Decision

**Two sign-ins.** sbx's own (Docker) sign-in, and the agent's. A Claude login made in a sandbox ends in sbx's global secret store
(`sbx secret ls` shows `anthropic`, "oauth configured"), which is why it survives rebuilds and is shared by every sandbox.

**What vibe reads, and how (read-only, tolerant of wording):**
- `sbx ls` is the sbx check: answers = signed in; "not signed in to Docker" = signed out; the policy menu, "operation cancelled by user" or
  "global network policy has not been initialized" = needs a policy; a timeout = unknown, with a hint about the system keyring (a locked
  KWallet stalled `sbx ls`). `sbx login` is **never** a probe: signed out it starts a flow.
- The agent: `SBX_CRED_ANTHROPIC_MODE` and `claude auth status` (JSON `loggedIn`, `email`, ...) as the agent. An expired credential that still
  reads logged in can't be told; that is caught when it bites (the failed turn makes the page look again), plus **Check now**.

**The Sign-in card (Admin tab).** A pill for each, who the agent is signed in as, "Do it yourself" steps, Check now. **Sign in** runs the real
command on the host as a process whose output vibe reads a line at a time (`sbxrun.Process`): `sbx login`, or `claude auth login` run as the
agent in the sandbox. The first https address and the "code is" line are shown (a link only when https), the rest under "What it printed"
(last 40 lines). The agent's flow has a password-type field whose value goes to the process's stdin as one line and is kept nowhere. One at a
time; Cancel ends it; given up on after 10 minutes; success re-checks; one already running when the page opens is picked up. Verified on a
real sbx: no terminal is needed. Other agents show "Not checked", with no button. A **notice above the conversation** says when sbx or the
agent is known to be signed out, with Open Sign-in, clearing when a check finds it signed in.

**The network policy comes first.** On a pristine sbx, other commands ask the policy question (Open / Balanced / Locked down) and block, and
`sbx policy reset` stops the daemon and asks again, so vibe never runs it. `--install` (first, not with `-f`) and `--setup` ask, with Balanced
the default, and run `sbx policy init <allow-all|balanced|deny-all>`; a second `init` is refused harmlessly ("already initialized") and is not
a failure. vibe's captured sbx calls have no stdin and a timeout, so a prompt can't hang them. The real text with no terminal is `error: global
network policy has not been initialized`, so "a policy is wanted" is that, the menu, or a clean `sbx policy ls` with no `local-policy` row, unless
`~/.vibe/sbx-policy-chosen` says vibe has already given one. vibe's own allow lists are per-sandbox rules on top of any of the three.

**sbx is made ready at every start that can ask** (a terminal, no `-f`): one `sbx ls` and, if fine, one `sbx policy ls`; when sbx waits for a
policy or a Docker sign-in, ask and give it, then look again (at most three rounds). `--install`, `--setup`, and `vibe -l`/`--info` do the
same (the last two reacting to the failure). Scripts and the detached headless child get sbx's own error. `setUpSbx` offers `sbx login` on
the terminal and checks it took.

**Headless from the start.** A favourite of `mode: headless` met a folder with no sandbox and fell back to a terminal, because creating
asks questions. Now the sandbox is made on the terminal (`createBeforeHeadless`, `--no-start`) and the headless start follows. `--headless`
typed still refuses, with what to do, when there is no sandbox.

**Signed out while a sandbox is stopped.** `sbx start` is not a command; only `sbx exec` wakes a stopped sandbox. Signed out, `sbx exec` on a
terminal runs the device flow itself; with no terminal it fails in under a second (exit 125, "not signed in to Docker"). So:
1. Waking reads sbx's output as it comes (`internal/sbxrun/wake.go`): a docker.com address with a `user_code` (`signin.DeviceLink`), or a
   failure naming `sbx login`, ends the wait with a `NotSignedInError`. **Neither carries the address**: a wake's output is what a command in
   the sandbox printed, which the agent can make, so an address could be a Docker sign-in of its own for the user to authorise (found in
   review). Output is capped (64 KB) and the process stopped at the cap.
2. A failure nothing recognises runs `sbx login` as a last resort, at most once in two minutes (an address in its output = signed out;
   none = signing in isn't the cause); its message says it was used and asks for a report of what sbx said. Accepted: the sandbox can fake a
   failure to make the host run `sbx login`; it is throttled, harmless when signed in, and its code is never shown.
3. The page says sbx is signed out (pointing at the Admin tab) and shows the banner whenever it comes back into view
   (`sbxrun.NotSignedInSeen`, cleared by whatever confirms sbx signed in: the card's check, a sandbox listed as running; passed on as
   `sbxSignedOut` in `/api/state`). Any other failure is in sbx's own words with the path of `~/.vibe/sbx-wake.log` (failed or over-5 s
   wakes, 256 KB then it starts again).

**An orphaned sandbox.** A sandbox sbx lists whose mounted folders are gone from the host is said to be unable to start, naming the folders;
on a terminal the user is asked whether to `sbx rm -f` it and make it afresh (default no); with `-f` or no terminal it is never removed, and the
command is given. `sbx start`'s own output is now kept (`sbxrun.StartExplained`).

## Consequences

- A fresh machine goes from `--install` to a working page with the terminal used only for first questions.
- The agent's sign-in can't be at setup: `claude auth login` runs inside a sandbox, and none exists then; it is once per machine.
- Matching sbx's English ("not signed in") degrades to "unknown" if reworded, the safe direction.
- A second review (2026-11-06) found the signed-out flag stayed set after signing in, so the page kept looking every 10 s; fixed as above.

## Not verified

- The card's real output on every sbx version; the last-resort path end to end (it needs a failure nothing recognises); `sbx policy ls` on
  a machine that has never been asked, beyond the text recorded above. Tried by hand 2026-11-06 and worked: `vibe -l` signed out, a page prompt
  signed out, the Admin Sign in, `sbx-wake.log` entries.
