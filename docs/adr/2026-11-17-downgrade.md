# `vibe --downgrade`: one step back for vibe or Docker SBX, and stopping what runs before sbx changes

- **Status:** accepted, built 2026-11-17 on `feat/downgrade`, merged (PR #29); stopping what runs before sbx changes, for `--upgrade` too,
  built on `fix/review-viewed-comments`, merged (PR #30). Tried by hand by the user in a Linux VM. What is not built is in
  `docs/plan-downgrade.md`.
- **Decided:** 2026-11-17, with the user (the first version, out of a wider plan raised 2026-11-04); the stop offer the same day, after an
  `--upgrade` hung restarting sbx's daemon with sandboxes running.
- **Came from:** `docs/plan-downgrade.md`, now cut down to what is not built: `git show 7b56280:docs/plan-downgrade.md` has it whole.

## Context

sbx updates broke vibe twice (sbx 0.51's kit and image change, and its `policy ls` deny fallback), and the only way back was by hand. With
a one-step way back, a new sbx (or vibe) can be tried without the risk of being unable to work.

## Decision

**A checklist like `--upgrade`'s**, with one row for vibe and one for sbx, each offering only **the release before the installed one** (sbx's
latest *stable* one before it: no RCs or nightlies). **Both rows start unticked**: going back is opt-in (the upgrade's are opt-out).

- **Finding the release before** (`selfupdate.PreviousTagAt`): the project's releases Atom feed, which needs no API and no sign-in but holds
  only the newest ten and doesn't page; only when the installed release is older than all of them, GitHub's API, a page at a time (60 calls
  an hour without sign-in, said when refused). Versions compare by number. A development build has no release before it, and says so.
- **It always asks.** `-f` is ignored (it says so), and with no terminal it changes nothing.
- **Everything picked is downloaded first**, then what runs is dealt with, then `~/.vibe` is backed up (when vibe is picked), then each is
  installed, going on past one that fails. Until the installs start nothing has changed: a failed download stops it all.
- **The backup** is of `~/.vibe` *beside* it (`~/.vibe-backup-<date>_<time>.zip`, the last three kept), so whatever goes wrong with
  `~/.vibe` doesn't take its backup with it; logs, the running sessions' files and the NuGet cache are left out. It is readable by its owner
  alone (it holds GitHub tokens), shows a spinner while it zips, and says where it is. Going back on sbx alone leaves `~/.vibe` as it is and
  makes none: sbx is the likelier to be gone back on, and a backup made for nothing would push out one that mattered.
- **sbx goes back the way vibe installed it**: the Linux release script, or the macOS tarball or `.dmg`. Installed by Homebrew (which installs
  only the cask's current release) or the Windows MSI (which won't put an older release over a newer one), it says how to go back by hand
  instead.
- **vibe goes back the way `--upgrade` goes forward**: the older release's zip, checked against its `SHA256SUMS`, runs its own `--install`
  (`--update-strategy` passed on). `--upgrade` and `--install-sbx` download and install through the same split, doing what they did.

**What runs is stopped first, once the user says so** (`clearToChange`, for `--upgrade` and `--downgrade` alike). Changing sbx restarts its
daemon, which pulls every running sandbox out from under its vibe, and a restart with sandboxes running was seen to hang at "Stopped
daemon"; going back on vibe changes the files running vibes use. So, once what was picked is downloaded, whatever `--stop-all` would stop is
listed and the user is asked to stop it all, as `--stop-all` does (vibes end, saving their agents' memories). Said no to, or with no terminal,
sbx is left as it is and vibe still goes ahead (unless a vibe is still running, for a downgrade of vibe). `--upgrade -f` stops them unasked,
but only when an sbx upgrade is actually being installed. The Admin tab's "all of them, unasked" command says so when one is waiting.

## Consequences

- `vibe --upgrade` offers the newest again whenever wanted: a downgrade isn't a hold.
- A sandbox built by the newer vibe may need `vibe -C`, and one made under the newer sbx `vibe -r`: it says so, and rebuilds nothing itself.
- sbx's `daemon restart` is kept (with or without a terminal it returned in 2-4 s in the user's tests); it is what runs after every install.

## Not built

The release cache, `COMPAT` (which sbx a vibe release works with) and skew warnings, the installer's `--release`, naming a version, more than
one step back, going back for Homebrew and MSI installs: `docs/plan-downgrade.md`.

## Not verified

macOS and Windows, including the Homebrew and MSI messages (waiting for a tester).
