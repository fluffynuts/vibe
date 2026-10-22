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
- **Not done**: `--re-create` still deletes the file with the profile (as agreed). A profile the bundle supplies is read, but its choices can't be kept (said, with `--profile` as the way out). The theme's `SaveToProfile` still writes into whatever profile folder it is given.
