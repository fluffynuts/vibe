# Plan: keeping sandboxes up to date

Status: agreed and built 2026-10-14 (uncommitted then). Code: `src/vibe/update.go`, `internal/profilegen/freshness.go`, `internal/prompt/countdown.go`.

## The problem

New vibe features (the clipboard bridge, diffity's hooks, ...) reach a sandbox only when its profile is
re-composed from the library and the sandbox is rebuilt from the profile. With more than ten sandboxes,
`vibe -C -f` in each folder is a chore, and a shell loop over `~/.vibe/profiles` gets the details wrong:
shared profiles, sandboxes in use, profiles that can't be re-composed.

## The outcome

vibe can tell whether a profile or a sandbox is out of date, and brings it up to date without asking:

- **When a session starts** in a folder: its profile and sandbox are brought up to date before attaching.
- **In batch**, with `vibe --update-sandboxes`: every profile and every sandbox, without starting a session.
  With `-f`, it's fit for a cron job: `vibe --upgrade -f && vibe --update-sandboxes -f`.

## Staleness is about content, not versions

Every push to master is a release, so comparing vibe versions would rebuild every sandbox on every release.
What matters is whether what a sandbox is built from has changed.

- **A profile vs the library.** vibe composes the profile afresh into a temporary folder, from the features
  it records, and compares the two. Files that are the user's rather than the composition's are left out of
  the comparison: `agent-instructions.md` and the recorded Claude theme.
- **A sandbox vs what it was built from.** When a sandbox is built, vibe records a fingerprint of what it
  handed sbx: the kit (profile + `~/.vibe/config.yaml` + `defaults/`), the agent, the environment and the
  published ports. A different fingerprint now means a rebuild is due. The Claude theme is left out: it is
  a choice, not a feature, and sandboxes sharing a profile can differ in it.

Both comparisons need composition and kit-building to be deterministic: composing the same thing twice must
give the same bytes. A test pins this.

## Updating without asking, safely

- **Profiles.** Composing records a hash of what it wrote (`composed-hash` in the profile's folder).
  - The profile still matches it: nobody edited it, so it's re-composed (backed up first, as `-C` does).
  - It doesn't match: it was edited by hand. It's never re-composed automatically; vibe warns, pointing at
    `vibe -C`, at every session start until it's settled.
  - No hash recorded (from before this): it's older than this vibe, so it's treated as out of date and
    re-composed, backed up first. Identical to a fresh composition, it's adopted instead: the hash is
    recorded and nothing else changes.
  - Not guided (hand-written, copied, blank) or only in vibe's bundle: skipped, as `-C` refuses them.
  - Recording a feature the library no longer has: skipped with a warning; `vibe -C` asks what to do.
- **Sandboxes.** A rebuild carries across what vibe knows of: agent memories, theme, GitHub token,
  timezone, the conversation. What's lost is anything installed by hand or kept outside the workspace,
  so the rebuild is announced. A sandbox with no recorded fingerprint (from before this) is out of date.

## When a session starts

On a terminal, a rebuild that's due is announced with a countdown:

    sandbox 'x' is out of date (its profile changed): rebuilding in 5s — Enter to go now, s to skip this time, Ctrl-C to quit

- **Enter:** go now. **s:** start the sandbox as it is. **Ctrl-C:** quit, changing nothing.
- With no terminal, there's no countdown: nobody is there to answer.
- `--no-auto-update` skips the check for one run; `autoUpdate: false` in `settings.yaml` turns it off.
- Re-composing a profile isn't counted down: it's backed up, and nothing in a sandbox changes until the
  rebuild, which is.

## `vibe --update-sandboxes`

1. Every guided profile in `~/.vibe/profiles` is re-composed if it's out of date (each once, however many
   sandboxes share it). Skipped ones are reported.
2. Every sandbox vibe has a record of is rebuilt if it's out of date, and left running or stopped as it was.
   - A sandbox with a vibe attached is left alone: it updates when its next session starts.
     `vibe --stop-all` first stops them all.
   - A sandbox sbx no longer has is skipped.
3. It carries on past a failure, ends with a summary of what was updated, skipped and failed, and exits
   non-zero if anything failed. It never starts a session.

No countdown, but rebuilding is destructive: what it will re-compose and rebuild is listed and asked
about once (not at all when nothing is out of date). `-f` goes ahead unasked; with no terminal, it won't
go ahead without `-f`. (Changed 2026-10-14: at first it went ahead unasked.)
