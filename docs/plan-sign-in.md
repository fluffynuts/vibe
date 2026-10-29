# Signing in from vibe

Planned 2026-10-29, not built. Signing in (to sbx, and to the agent) is the highest-friction point today: both need a
terminal. Aim: as painless as possible from the page, with, as in every other dialog, the plain commands shown for anyone
who would rather do it by hand. Claude only for now; Cursor later, either by generalising or by building alongside with
the same structure so the next agent can generalise (see the `cursor-support` notes).

## Two sign-ins

1. **sbx's own (Docker) sign-in.** Needed once; it may expire (not known).
2. **The agent's.** The user's Claude auth survives sandbox rebuilds without signing in again, so it is almost certainly
   in sbx's secret store, which the proxy uses on the agent's behalf (`sbx secret --help`: "the secret is never exposed
   directly"; services include `anthropic`). Not yet confirmed (see "To find out").

## What is known (sbx on the user's host, and a Windows VM)

- `sbx login` is a device flow: it prints a one-time code and `https://login.docker.com/activate?user_code=<code>`, then
  prints "Waiting for authentication..." and blocks until the user has approved in a browser. Signed in already, it prints
  `You are signed in [username: <name>]`. Non-interactive: `echo "$DOCKER_PAT" | sbx login --username <u> --password-stdin`.
- **`sbx login` is not a status check**: signed out, it starts a flow. A read-only way to tell the states apart is needed.
- `sbx secret` has `import ls rm set set-custom`; secrets are global or per sandbox (`--sandbox`).
- sbx can't run in the Windows VM the user has (no hypervisor), so Windows output has to come from their work laptop.

## Shape (to confirm once the questions are answered)

- A **Sign-in card** (Admin tab, probably) showing the sbx sign-in and the agent's sign-in, each with its status.
- Buttons run the real command on the host (vibe runs there) and stream its output to the page; for `sbx login` that
  means pulling the URL and code out and showing them as a link. Parsing tolerant: only one sample of the output so far.
- Each button has a "do it yourself" disclosure with the exact commands.
- Status: **presence is cheap** (a read-only sbx command, to find); **validity is not free to probe**, so an expired
  credential is best caught when it bites: recognise the failure in the conversation (not signed in, 401) and offer the
  sign-in then; plus a "Check now" button.
- The agent's sign-in for a **subscription** (browser login) and for an **API key** (`sbx secret set anthropic`,
  probably, with the key read from a field and passed on stdin, never on a command line or in a log).
- Order: status and manual instructions first; then the sbx login button; then the Claude sign-in.

## To find out (the user, on their host / work laptop)

1. `sbx secret ls`: is there an `anthropic` entry? Run it before and after a fresh Claude sign-in to see what changes.
2. `sbx secret set --help`, and `sbx secret set anthropic --help`: can a Claude secret be set without a terminal?
3. A read-only sbx command that fails with a "not signed in" error when signed out (try `sbx secret ls`, `sbx ls`, with
   exit codes), on a machine that has never signed in or after a sign-out.
4. Inside a signed-in sandbox: what `claude login` prints (the URL? a code to paste back?), and whether it can be driven
   without a terminal. The user offered to ssh into the Windows VM's sandbox for this once a sandbox runs on the laptop.
5. Do not assume the Claude CLI on the host: vibe exists so that no agent is on the user's machine.

## Decisions so far

- Claude only first; Cursor later, sharing the structure.
- Subscription flow wanted, API key supported too.
- This goes before `docs/plan-stop-ends-review.md`: sign-in is user friction, an interrupted review can be closed and
  reopened.
