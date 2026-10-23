# A `user-preferences.yaml` for the profile (to fold into the model/effort branch)

Decided 2026-10-22: **do this on `feat/model-and-effort`**, once master (with the git-author fix) is merged into it. That branch has
to move `model:`/`effort:` out of the profile's `settings.yaml` anyway (below), and this is the third such preference, so it is the
time to do it properly. `docs/plan-model-effort.md` on that branch should point here.

## Why

A profile's content hash (`internal/profilegen/freshness.go`, `ContentHash`) covers everything composing wrote, `settings.yaml`
included. A line vibe adds there for the user's choice makes the profile look **edited by hand**, and then auto-update leaves it for
the user ("changed by hand… `vibe -C`"). Found with a failing test for `gitAuthor` (fixed by giving it a file of its own); the
model/effort branch writes `model:`/`effort:` to the profile's `settings.yaml`, so it has the same fault, not yet tested there.

So choices made through vibe live in files outside the hash, carried across a re-compose: today `claude-theme`, and `git-author`
(`profilegen.GitAuthorFile`, added to `notComposed` and carried in `replaceProfile`). Three files, soon four with model and effort:
one `user-preferences.yaml` instead.

## The change

- One file in the profile folder, `user-preferences.yaml`: `theme`, `gitAuthor`, `model`, `effort` (and later ones). Left out of
  the hash (`notComposed`), carried across a re-compose and an automatic update (`replaceProfile`), deleted with the profile by
  `--re-create` (which regenerates it by design).
- Written atomically (`fscopy.WriteWhole`), read through one small type, so a new preference is a field, not a file.
- Replaces `profileChoice` writing `model:`/`effort:` into `settings.yaml`, the `git-author` file, and the `claude-theme` file.

## Migration (existing users' files must be moved, not stranded)

On first read, per profile: read `claude-theme` and `git-author`, and `model:`/`effort:` from the profile's own `settings.yaml`, write
`user-preferences.yaml`, then remove the old. Pin with tests (each source alone, all together, nothing to migrate, an old file
that is invalid, run twice).

Caveat for `model:`/`effort:`: they sit in a hashed file. Removing them with `settings.Unset` re-encodes the YAML, so the file may not
come back byte-identical to what was composed, and the profile would still look edited. Compare the hash of `settings.yaml` with
the keys removed against the recorded one: equal means remove them (the profile is unedited again); otherwise leave them and say so.
Either way the value is read from the new file first.

## To settle when doing it

1. **Precedence.** Today they differ: `model`/`effort`, a profile's own choice (made on the page) wins over the global
   `settings.yaml`; `gitAuthor`, a `gitAuthor:` the user wrote in any `settings.yaml` wins over the kept answer. Suggest one rule:
   *`user-preferences.yaml` (the profile's) > the profile's `settings.yaml` > the global `settings.yaml`*, so the latest thing the
   user did through vibe wins, and editing the file by hand works too. That reverses the `gitAuthor` order; the readme and its tests
   say so.
2. Is `agent-instructions.md` (the user's own, also outside the hash) a preference? It is free text, so probably stays a file.
3. The readme sections for the theme, the git author, and the model/effort drop-downs change together.

# As built (2026-10-22/23, on `feat/model-and-effort`)

Built where it was planned: the model/effort branch, after master (with the git-author fix) was merged into it.

- **`internal/userprefs`**: `Load`, `Update`, `Migrate`, `Path`, `File`, `LegacyFiles`. `Prefs` has `Theme`, `GitAuthor`, `Model`, `Effort` and keeps keys it doesn't know. `Update` changes only the lines that change (via `settings.Set`/`Unset`, which keep comments and other keys), is atomic (`fscopy.WriteWholeMode`) and keeps the mode a file has (0644 for a new one), refuses to write over a file it can't read, never makes a profile folder, and removes the file when nothing is left. A new file starts with a comment saying how to undo a choice.
- **Hash**: `user-preferences.yaml` and the old file names are in `profilegen`'s `notComposed`. `ContentHashWith(dir, changed)` says what the hash would be if a file changed (nil: gone), and `ComposedFile` what composing writes for a file now.
- **Moved into it**: the theme (`claudetheme.FromProfile`/`SaveToProfile` go through it), the git author answer, and the model/effort (`profileChoice`). `replaceProfile` carries the one file across a re-compose or automatic update, instead of three separate carries.
- **Migration** (`src/vibe/prefsmigrate.go`, `migratePreferences`): the old `claude-theme` and `git-author` files are read as fallbacks by `Load` and moved the first time anything changes; `model:`/`effort:` in a profile's settings.yaml are moved into the preferences (an answer already there wins) and taken out of settings.yaml **only if that leaves the profile no more edited than it was composed**: the candidates are settings.yaml as it is with the keys out, or, when it says the same settings, what composing writes now, since earlier vibes rewrote the file in their own layout (a plain `Unset` could not give the composed bytes back). If neither restores the hash (you changed something else in settings.yaml too) the keys stay, said once, and the preferences are read first. Run before a profile is assessed for update, before a re-compose, and when a choice is kept; only for a profile in the user's own vibe home, never the bundle's.
- **Precedence (decided, one rule for every preference)**: the profile's preferences, then the profile's `settings.yaml`, then `~/.vibe/settings.yaml`. For the git author this **reverses** the earlier order (a hand-written `gitAuthor:` used to beat a kept answer); the kept answer, being the newer word and the more specific, now wins; the readme says so. Choosing **Default** on the page also takes a leftover `model:`/`effort:` key out of the profile's settings.yaml, so it can't keep a choice alive that the user has just taken away.
- **Wording**: every message that named a standalone file now names `user-preferences.yaml` (and the line to delete, or change): the "kept: …" note after an answer is kept, the error for a gitAuthor that isn't one, the error for an unsafe model/effort (it says which file holds it), and the readme. The wording of the kept note is the same shape for any future preference.
- **`--re-create`** (decided after the first build; the user's design): the file is kept. With `-f` it is kept without asking and said so once, with how to drop it; otherwise, after the existing "destroy any existing profile or sandbox" confirmation, vibe lists what is in the file and asks whether to keep it, defaulting to keep; no deletes it with the old profile. The bytes are read before anything is removed (the old files and `model:`/`effort:` are migrated in first) and written back right after the new profile is made, before the kit or the theme is read (`afterProfile` hooks in `rebuildSandbox`), so a comment or a key a newer vibe wrote comes through, and a file vibe can't read is carried whole and said so. A profile the bundle supplies is read, but its choices can't be kept (said, with `--profile` as the way out). The theme's `SaveToProfile` still writes into whatever profile folder it is given.
- **Backup** (added after: the user asked for it): `--re-create` zips the profile into `~/.vibe/backup/<sandbox>-<date>.zip` before removing it, as a re-compose does, after the old files are migrated in (so the zip holds the choices as they are now), and removes nothing if it can't (`vibe --restore` lists and rolls back).
- **Not done**: A profile the bundle supplies is read, but its choices can't be kept (said, with `--profile` as the way out). The theme's `SaveToProfile` still writes into whatever profile folder it is given.

# Cleaning up after the move, and saying a profile's files are generated (decided 2026-10-23)

## Dead code to remove once nobody can still have the old files

The migration (above) is for profiles made before it: once none are left, the code that knows the old places is dead, and should go (the user's rule: clean up dead code when the opportunity presents). Who can still have them: `claude-theme` is from releases long before this one, and `git-author` from the release after PR #4 (v0.1.130 is the latest tag as I write); the model and effort in `settings.yaml` never shipped in a release (only dev builds of this branch). So the thing to wait for is the releases that wrote `claude-theme` and `git-author`, and a profile skipping several releases still being migrated.

**Remove, together, two releases after the one this ships in** (the user may say another number when the time comes):

- `userprefs`: `LegacyFiles`, `legacyTheme`, `legacyGitAuthor`, `loadWithLegacy` (Load reads the new file alone), the legacy removal in `Update`, and `Migrate` (and their tests).
- `profilegen`: the old file names in `notComposed`, and the test that they don't count.
- `src/vibe/prefsmigrate.go` (`migratePreferences`, `migrateChoiceKeys`, `sameSettings`), its calls in `updateProfile`, `replaceProfile`, `preferencesToKeep`, `profileChoice.set` and `keepGitAuthorChoice`, `profilegen.ContentHashWith` and `ComposedFile` if nothing else uses them, and their tests.
- the readme paragraph "Earlier vibes kept these in files of their own…".

A comment on `userprefs.LegacyFiles` says so, so a search for it finds this.

## A README in each generated profile (the user likes it; not built yet)

Why: files composing wrote can be edited, but an edit is lost to a re-compose or an automatic update (which back the profile up first, as `--re-create` does), and `settings.yaml`, `config.yaml` and the install scripts can't all carry a header comment without it showing up where it shouldn't (the agent reads `agent-instructions.md` as instructions; JSON takes no comments), and a header in the composed bytes would change every profile's hash, so every sandbox would be rebuilt once on upgrade.

Instead: one `README.md` in the folder of each **guided** profile (and a hand-made or copied one gets none: it isn't generated):

> This profile was generated by vibe, from the features named in config.yaml. You may edit its files, but a re-compose (`vibe -C`) or an automatic update replaces them, and edits are lost; the profile is backed up first (`vibe --restore` lists the backups), so you can get something back. Your own choices (theme, git author, model, effort) are in `user-preferences.yaml`: that file is yours, and is carried across. Your instructions for the agent are in `agent-instructions.md`, which is also carried across.

- Excluded from the hash (`notComposed`), as `user-preferences.yaml` is, so it changes nothing for any profile that already exists.
- Written when a guided profile is composed (`CreateGuided`, `replaceProfile`), and added, when missing, to existing guided profiles by the migration (so its removal goes with the migration's, except the part that writes it at compose time, which stays); never overwritten, so a user who rewrote it keeps theirs.
- Tests: written at compose and on re-compose; not written for a hand-made profile; added by the migration to a profile that lacks it; left alone when it is there and different; not part of the hash (a profile with it, or with it edited or deleted, is still up to date).
- It names `vibe --restore` and `vibe -C`: the readme and the "changed by hand" message say the same things, in the same words where they can.
