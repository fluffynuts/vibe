# Plan: managing profiles and sandboxes

Status: agreed and built 2026-10-14, a commit per step (see "Order of work").

## The problem

Testing and everyday use leave trash behind: profiles like `foo` and `test`, sandboxes whose folders are
gone, records of sandboxes sbx no longer has. There's no one place to see what exists, how it hangs together,
and to clear out what isn't wanted. `--update-sandboxes` tripped over exactly this (see "First: the missing
folder bug").

## First: the missing folder bug

`--update-sandboxes` rebuilt sandbox `test`, whose folder `/tmp/test` was gone. It removed the sandbox, then
`sbx create` asked "The selected workspace does not exist. Would you like to create it?", because vibe hands it
the terminal's input (`internal/sbxrun/sbxrun.go`, `CreateContext`). Answering no left no sandbox at all.

- **Check the folder before touching anything.** Every rebuild (`--update-sandboxes`, the update at session
  start, `-r`, `-C`, the Admin tab) first checks the sandbox's folder exists. If it doesn't, nothing is removed:
  it's skipped, saying why and what to do ("'vibe --cleanup' deletes it; if the folder moved, 'vibe' in its new
  place makes a new one").
- **sbx never asks when nobody can answer.** In `--update-sandboxes` and the page's actions, `sbx create` gets
  no input, so a question from sbx fails at once with its message instead of stopping the run. Interactive
  `vibe` keeps the terminal.
- **Tests first:** a sandbox whose folder is gone is skipped and never removed; a stand-in sbx that asks a
  question fails the batch run rather than hanging it.

## Where it lives

- **`vibe --manage`** serves the page on its own, needing no sandbox or folder, until Ctrl-C. It keeps one
  port, remembered as the companion's is, so a bookmark keeps working.
- **A Profiles tab** on every companion page shows the same view.

## The view, driven by profiles

One group per profile, its sandboxes under it.

- **Profile line:** name, features, and flags for spotting trash: "no sandboxes", "every folder gone",
  "edited by hand", "out of date", "vibe's own" (bundled: can't be deleted).
- **Sandbox line:** status (running, stopped, gone from sbx), whether a vibe is attached, its folder (flagged
  when missing), whether it's out of date, when it was created.
- **Records naming a profile that no longer exists:** a group of their own.
- **Sandboxes sbx has that vibe never recorded:** a separate section at the bottom. Deletable; nothing else.

The list loads in stages, showing what it has while slower sbx answers arrive, under the Admin tab's 20-second
backstop.

## Deleting

- **A profile, ticked:** everything about it goes: the profile folder, all its sandboxes, their records, agent
  memories, clipboard history, conversations, GitHub tokens, and the profile's backups. No undo.
- **A sandbox, ticked:** the sandbox and its own data (record, memories, clipboard history, conversation, GitHub
  token). The profile stays.
- **Select likely trash** ticks profiles with no sandboxes, profiles whose every folder is gone, and records whose
  sandbox sbx no longer has. The user unticks what to keep.
- **One confirmation** lists exactly what will go, by name and path, before anything is deleted. Deleting
  carries on past a failure and ends with a summary.

## Other actions

- **Sandbox:** Start, Stop, Rebuild, Update.
- **Profile:** Re-compose (then rebuild its sandboxes, since they share it), Update.
- One job at a time, with progress and Cancel, as the Admin tab's actions. Each takes the sandbox's folder lock
  while it works, so two pages, or a page and a terminal, can't act on one sandbox at once.

## Guards

Each disabled control says why.

- An embedded page can't delete or rebuild its own sandbox: use `vibe --manage` or another sandbox's page.
- A sandbox with a vibe attached can't be deleted or rebuilt; "stop its vibe first" does what `vibe --stop` does.
- Bundled profiles can't be deleted.

## From a terminal

`vibe --cleanup` becomes the same profile-grouped checklist, profile lines included, with the same flags,
"likely trash" pre-selection, confirmation and summary. No new commands.

## Under the hood

One inventory (profiles from both layers, vibe's records, `sbx ls`, folder checks, attached vibes, freshness)
gathered by shared code that the page, `--cleanup` and `--update-sandboxes` all use, so they agree about what
exists.

## Order of work

1. The missing folder bug. Done.
2. The shared inventory, and `--update-sandboxes` moved onto it. Done (`src/vibe/inventory.go`). "Likely trash" also
   ticks a sandbox whose folder is gone, as well as one sbx no longer has.
3. `--cleanup` as the profile-grouped checklist, with deleting as above. Done (`src/vibe/cleanup.go`, `src/vibe/manage.go`).
4. The page: `vibe --manage` and the Profiles tab, listing and deleting. Done. The list loads whole, under
   the 20-second backstop, rather than in stages: everything on it waits on `sbx ls`, and the rest is quick.
   The Profiles and Admin tabs share the page's one job at a time.
5. The page's other actions. Done.
