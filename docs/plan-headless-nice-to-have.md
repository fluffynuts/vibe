# Headless: the nice-to-haves (planned, not built; for when there is time)

Left over from the plan "what's left for headless", whose built parts (reaching the page from another machine; noticing a logged-out agent)
are in `docs/adr/2026-10-14-headless.md`. Nothing here is built, and none of it is urgent. The text is as it was written, trimmed to what
remains, and the wrong status line of the old plan ("nothing below is built") is gone with it.

## 1. Starting at boot

vibe prints (never installs) a systemd user unit, or a launchd agent on macOS, running `vibe --headless --foreground <folder>`. Only
`--foreground` exists today (for tmux, `nohup` or a unit to call); nothing prints a unit. First check by hand that sbx works with no desktop
session: `env -i HOME=$HOME PATH=/usr/bin:/bin sbx ls`, from a unit. A wedged keyring can hang sbx's Docker sign-in; the unit must not hang
silently if it does.

## 2. A time limit on unattended turns (optional, undecided)

An agent with bypassed permissions and nobody watching can run for a long time. The page already says when it has been quiet for an hour, and
leaves stopping it to the user (`docs/adr/2026-10-13-bounded-waits.md`). Decide whether headless wants a limit.

## 3. Forwarding without the ssh line (not designed)

Asked for 2026-10-15: once someone can reach a vibe on the host from another machine, launching a sandbox from the Profiles page there should
make the same forwards happen for them, so remote use is transparent rather than a command to copy.

What the user said (review of the `--info` change): "when launching a profile from the profiles page, the same ssh magic is automatically
done for the user, ie make this process transparent once the user has access to an instance on the host machine."

To settle when it's designed:

- Who makes the forwards. The page runs on the host; the forwards have to be made from the remote machine, so something there has to do it:
  the user's existing ssh session (`ssh -O forward` over a ControlMaster connection adds forwards to it), or a small helper the user runs
  once. A page in a browser can't open ssh tunnels itself.
- Which ports: the launched sandbox's page, clipboard and diffity, as `--info` lists them, each keeping its number. A port already taken on
  the remote machine needs saying so, not a silent miss.
- What the page offers meanwhile: its link to the new sandbox's page only works remotely once forwarded.

## 4. Smaller gaps

- Headless in the background on Windows (`detach_windows.go`) has never been run there.
- The Admin tab's new toast wording is unseen; the user feeds back as they come across it.
- The compaction notice waits on a real compaction to be seen.
