# Profiles and sandboxes: one view of them, kept up to date by content, with the user's choices kept apart

- **Status:** accepted, built. Managing and updating 2026-10-14; the preferences file 2026-10-22/23, with its migration code removed 2026-11-10.
- **Decided:** 2026-10-14 (managing, updating) and 2026-10-22 (preferences), with the user.
- **Came from** three plans, now deleted: `plan-profile-management.md` (`git show 3de3d8d:docs/plan-profile-management.md`),
  `plan-update-sandboxes.md` (`git show 738e67b:docs/plan-update-sandboxes.md`) and `plan-user-preferences.md`
  (`git show a62d0f0:docs/plan-user-preferences.md`). They are one subject: what a profile is, what a sandbox is built from, and what
  must survive the one being rebuilt from the other.

## Context

Testing and everyday use leave trash (profiles like `foo` and `test`, sandboxes whose folders are gone, records of sandboxes sbx no
longer has), and there was no one place to see it. New vibe features reach a sandbox only when its profile is re-composed from the
library and the sandbox rebuilt from the profile, which with more than ten sandboxes is a chore a shell loop gets wrong (shared
profiles, sandboxes in use, profiles that can't be re-composed). And the user's own choices (the Claude theme, the git author answer,
the model and effort) had nowhere to live that didn't make a profile look edited by hand.

## Decision

### Managing

- **One inventory** (profiles from both layers, vibe's records, `sbx ls`, folder checks, attached vibes, freshness), gathered by shared
  code (`src/vibe/inventory.go`) that the page, `--cleanup` and `--update-sandboxes` all use, so they agree about what exists.
- **Where:** `vibe --manage` serves the page on its own, needing no sandbox or folder, on one remembered port; every companion page also
  has a **Profiles** tab. The list is driven by profiles, a group each with its sandboxes, flagged for spotting trash ("no sandboxes",
  "every folder gone", "edited by hand", "out of date", "vibe's own"); records naming a missing profile, and sandboxes sbx has that vibe
  never recorded, have groups of their own. It loads whole, under the 20-second backstop, since everything on it waits on `sbx ls`.
- **Deleting** a profile takes everything about it (folder, sandboxes, records, memories, clipboard history, conversations, tokens,
  backups), with no undo; deleting a sandbox keeps the profile. **Select likely trash** selects profiles with no sandboxes, profiles whose
  folders are all gone, and records sbx no longer has. **One confirmation** lists exactly what will go, and deleting carries on past a failure
  and ends with a summary. `vibe --cleanup` is the same checklist in a terminal.
- **Other actions:** Start, Stop, Rebuild, Update for a sandbox; Re-compose and Update for a profile. One job at a time, with progress and
  Cancel, each holding the sandbox's folder lock, so two pages (or a page and a terminal) can't act on one sandbox.
- **Guards, each disabled control saying why:** a page can't delete or rebuild its own sandbox; a sandbox with a vibe attached can't be
  deleted or rebuilt; the bundle's profiles can't be deleted.
- **A rebuild checks the sandbox's folder first**, and if it is gone, removes nothing and says what to do. And sbx is **never given the
  terminal when nobody can answer** (a batch run, the page), so a question from sbx fails at once with its message. Found when
  `--update-sandboxes` removed a sandbox whose folder was gone and `sbx create` asked "create the workspace?".

### Keeping up to date

- **Staleness is about content, not versions.** Every push to master is a release, so comparing vibe versions would rebuild everything on
  every release. A **profile** is compared with a fresh composition of the features it records (written to a temporary folder); the
  user's own files (`agent-instructions.md`, the preferences, the generated README) are left out of the comparison. A **sandbox** is
  compared by a **fingerprint** of what it was handed (the kit, the agent, the environment, the published ports and the host folders it
  mounts, which `sandboxFolders` decides in one place). Composing and kit-building must be deterministic, which a test pins.
- **Updating without asking, safely.** Composing records a hash of what it wrote (`composed-hash`). A profile that still matches it is
  unedited, so it is re-composed (backed up first); one that doesn't was edited by hand, and is **never** re-composed automatically (vibe
  warns at each session start, pointing at `vibe -C`); one with no hash is older than this vibe, so it is re-composed unless it is identical
  to a fresh composition, when it is adopted. A profile that is hand-written, copied, blank or only in the bundle is skipped, as is one that
  records a feature the library no longer has (a warning; `vibe -C` asks).
- **A rebuild carries across what vibe knows of** (memories, theme, GitHub token, timezone, conversation); what is lost is anything installed
  by hand or kept outside the workspace, so the rebuild is announced.
- **When a session starts** on a terminal, a due rebuild is announced with a countdown (Enter goes now, `s` skips this time, Ctrl-C quits
  changing nothing); with no terminal there is none. `--no-auto-update` skips the check for a run; `autoUpdate: false` turns it off.
- **`vibe --update-sandboxes`** re-composes each out-of-date guided profile once, then rebuilds each out-of-date sandbox, leaving it running
  or stopped as it was, and leaving alone one with a vibe attached. It carries on past a failure, ends with a summary, and exits non-zero if
  anything failed. It lists what it will do and asks once; `-f` goes ahead (fit for cron), and with no terminal and no `-f` it won't.

### The user's choices

- **One file, `user-preferences.yaml`,** in the profile's folder: `theme`, `gitAuthor`, `model`, `effort`, `mode`, and later ones (a new
  preference is a field, not a file). It is left out of the profile's hash, carried across a re-compose and an automatic update, written
  atomically, and changed line by line (comments and keys this vibe doesn't know survive).
- **One precedence rule for every preference:** the profile's preferences, then the profile's `settings.yaml`, then `~/.vibe/settings.yaml`,
  so the latest thing the user did through vibe wins and editing the file by hand works too. For the git author this reversed the earlier
  order.
- **`--re-create` keeps the file** (with `-f` without asking, and said once; otherwise vibe lists what is in it and asks, defaulting to keep),
  after backing the profile up to `~/.vibe/backup/<sandbox>-<date>.zip` as a re-compose does.
- **A `README.md` in each guided profile** says it is generated, that its files may be edited but a re-compose or an automatic update replaces
  them (after a backup, listed by `vibe --restore`), and that the preferences and instructions are the user's. It is excluded from the hash, is
  added when missing whenever a session starts or a sandbox is made, is never written over, and a hand-made profile gets none.

## Consequences

- A profile edited by hand is never silently overwritten; the cost is that it stops getting library updates until the user settles it.
- A user's choice never makes a profile look edited, and survives a rebuild. The cost is that anything a user puts into a composed file
  other than the preferences is lost at the next re-compose (and is in the backup).
- **The migration of earlier vibes' files was removed on 2026-11-10,** as the plan said to do two releases after it shipped (about 23 releases
  had passed). `claude-theme`, `git-author`, and `model:`/`effort:` in a profile's `settings.yaml` are no longer read or moved, and a stray old
  file (an old backup restored) counts as an edit to the profile, to be deleted. A model in `settings.yaml` is still read as a layer.
- The two waits and the sandbox-wait unification are in `2026-10-13-bounded-waits.md`; the parallel update is planned in
  `docs/plan-parallel-sandbox-update.md`.

## Not built

- **A bundled profile's choices can't be kept:** the profile is read, but nothing is written into the bundle, so the page says to make a
  profile of the user's own (`--profile`). The theme's `SaveToProfile` still writes into whatever folder it is given.
