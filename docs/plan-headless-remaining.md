# Plan: what's left for headless

Status: steps 1–3 are built, tested on a real machine, and committed (41fd67e, 2026-10-14). These steps
come next, in order. Nothing below is built: a start on step 1 was reverted to pick up afresh.

## 1. Reaching the page from another machine

A headless vibe is mostly useful from elsewhere, over SSH. The page only answers on its own port (its Host
check wants `localhost:<its port>`), and it embeds the clipboard and diffity pages by their own ports, so a
forward has to keep every port's number.

`vibe --info` gains, when the sandbox exists and a companion port is recorded:

```
page      http://localhost:10000/#<token> (being served now)
remote    from another machine, forward the page's ports, keeping their numbers:
            ssh -N -L 10000:localhost:10000 -L 5390:localhost:5390 -L 5391:localhost:5391 you@this-host
          then open the page's address there
```

- Not served: `(not being served: 'vibe --headless' here serves it)`. Served or not: `companion.Running(port, name)`.
- The ports: `inst.CompanionPort` first, then each `inst.Publish[].HostPort` (clipboard, diffity, and any
  others a profile publishes).
- The address with its token: `companionURL` (src/vibe/detach.go).
- `you@this-host`: the current user (strip a Windows `DOMAIN\`) and `os.Hostname()`. Another machine
  may know this one by another name; the line is a starting point.
- Pure helper `sshForwarding(ports, target)`, unit-tested; a `TestPrintInfo`-style test for the rows,
  including "no page yet" showing neither row.
- The headless start (detachHeadless, after "its log: …") adds: `'vibe --info <folder>' says how to reach
  the page from another machine`.
- Readme and `--help`, wording drafted then reverted with the code:
  - usage line: `vibe -a/--info [path]  # show the sandbox's settings, its companion page and how to reach it
    from another machine, and, while it runs, its uptime and memory and disk use` (and the same in main.go's usage).
  - `--info` paragraph: "Once it has had a companion page, it shows the page's address (and whether a vibe is
    serving it now), and the `ssh -L` command that reaches it from another machine: it forwards the page's port
    and the ports the page embeds (the clipboard, diffity), each to the same number, as the page only answers
    on its own port. The host name in it is this machine's own; use whatever name the other machine knows it by."
  - Headless section: "To use the page from another machine, `vibe --info` gives the `ssh -L` command that
    forwards its ports there."

To check first: whether sbx publishes ports on 127.0.0.1 or all interfaces. If all, say so: the clipboard
and diffity pages are then reachable from the network without SSH.

## 2. An agent that isn't logged in

Headless, nobody sees the agent's login prompt, so prompts from the page would just fail. Detect a
logged-out agent when the headless vibe starts (and when a turn fails for it), and say so in the log and on
the page, with what to do (`vibe` in the folder, in a terminal, to log in). Find first how a logged-out
`claude -p` fails: exit code, stream-json lines.

## 3. Starting at boot

vibe prints (never installs) a systemd user unit, or a launchd agent on macOS, running
`vibe --headless --foreground <folder>`. First check by hand that sbx works with no desktop session:
`env -i HOME=$HOME PATH=/usr/bin:/bin sbx ls`, from a unit. A wedged keyring can hang sbx's Docker sign-in
(see the shelved keyring note); the unit must not hang silently if it does.

## 4. A time limit on unattended turns (optional)

An agent with bypassed permissions and nobody watching can run for a long time. The page already says when
it has been quiet for an hour, and leaves stopping it to the user. Decide whether headless wants a limit.

## Smaller gaps

- Headless in the background on Windows (`detach_windows.go`) has never been run there.
- `vibe --headless` while a terminal session runs gets the picker: fine, as that vibe already serves the page.
- The Admin tab's new toast wording is unseen; the user feeds back as they come across it.
- The compaction notice waits on a real compaction to be seen.

Dropped: a periodic check that starts a stopped sandbox again. sbx stopping idle sandboxes is wanted, and
the page keeps its sandbox awake while it's in view.
