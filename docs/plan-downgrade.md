# `vibe --downgrade`: go back to an earlier vibe or sbx when an update breaks things (planned, not built)

Raised 2026-11-04, the day an sbx update (v0.51.0, which moved its claude agent to a schemaVersion 3 kit) stopped every vibe sandbox from starting, and there was no way back except doing it by hand. The want: an escape hatch for a broken sbx update, or a broken vibe release, so the user can carry on working. `vibe --upgrade` is unchanged and still offers the latest when run again.

## What there is

- `vibe --upgrade` (`src/vibe/selfupgrade.go`) finds the latest vibe (`selfupdate.LatestTag`: it reads where `releases/latest` redirects to, which needs no GitHub login and no API) and the latest stable sbx (`sbxinstall.LatestStableTag`, the same trick, refusing an RC), offers them in a pick list, and installs. vibe's way: `selfupdate.Download` (checked against the tag's `SHA256SUMS`) → `Unzip` → run `--install` from the unpacked release, which replaces the binary and the package files in `~/.vibe`. sbx's way: `installSbxRelease` / the install method that put it there (`installedSbxMethod`).
- Releases are kept: every push to master is a release `v0.1.<run>`, with fixed asset names (`vibe-linux-amd64.zip`, ... plus `SHA256SUMS`), so `https://github.com/fluffynuts/vibe/releases/download/v0.1.57/vibe-linux-amd64.zip` is a stable URL for any release. (No step in the workflow prunes old ones.)
- `--install` already works from "the package's version from the last install" (`~/.vibe/package-files`, a three-way merge base: `internal/upgrade`). Going to an older package is the same operation in the other direction: a file only the package changed goes back to the older version; one the user edited is a conflict settled by `--update-strategy` or by asking.
- `selfupdate.Newer` compares versions numerically, so `0.1.9` sorts below `0.1.10`.

## Proposed

`vibe --downgrade [vibe|sbx] [VERSION]` (no short form: a flag nobody should hit by accident).

1. **What to go back on.** With no product given, at a terminal it asks (vibe, sbx), showing what is installed of each. `-f` and "no terminal" don't guess: they need the product and the version, and fail saying so, with the list printed (so it is scriptable and works over ssh).
2. **The list.** The **five releases before the installed one**, newest first, numbered, the installed one shown at the top as "(installed)" and not selectable. For sbx only stable releases count (`sbxinstall.Stable`), so the five are five stable ones, not RCs and nightlies. Choosing is a single choice (this is not the upgrade's multi-select), and Esc leaves everything as it was. Going back further than five is done by running it again, now from the older one, or by naming a version: `vibe --downgrade vibe 0.1.41`.
3. **A development build** has no build number and counts as older than any release of its version, so for it the list is simply the latest five releases, said so in a line above it.
4. **Doing it** is the upgrade's own code with a chosen tag instead of the latest: `upgradeVibe(tag, ...)` for vibe, the install method's own path for sbx (see below). It checks the checksum like any download, and a release with no `SHA256SUMS` is refused, never installed unchecked.
5. **After it.** Says what is now installed, says that `vibe --upgrade` will offer the newest again (and that vibe's end-of-session "newer release available" note will go on appearing: that is what the user asked for, so it is not suppressed), and, if sandboxes exist, says whether they need `vibe -C` (see "Skew").

## Before it changes anything

- **A backup, outside `~/.vibe`.** Zip `~/.vibe` (as `--re-compose` zips a profile into `~/.vibe/backup/`) to a file *beside* it, such as `~/.vibe-backup-<date>_<time>.zip`, so losing `~/.vibe` doesn't lose its backup (the very thing that went wrong today). Keep the last three; say where it is.
- **Refuse while a vibe session is running** for a sandbox (`internal/running` knows), as `--restore` does, or ask to end it, rather than replace files under it. (`--upgrade` today replaces the binary under a running vibe; a downgrade is more likely to be done because something is wrong, so it is stricter.)
- **Say what will happen, then ask** (`-f` skips the question, not the backup): "will replace vibe 0.1.60 with 0.1.57; your ~/.vibe is backed up to ...".

## Skew: the part that needs care

A downgrade is only coherent if the other pieces still fit, and today's incident was exactly such a mismatch.

- **vibe ↔ sbx.** vibe 0.1.N writes a schemaVersion 3 kit, which needs the sbx that has the v3 claude agent; an older vibe writes a v2 kit, which a newer sbx refuses to compose with that agent. Going back on one without the other can break every sandbox. Plan: each release carries a small `COMPAT` asset (the sbx versions it was built and tested for: a minimum, and the newest tried), and the picker marks any choice whose pair would be outside it ("works with sbx >= 0.51.0: you have 0.49.2"). It warns, and the user can still go on: a warning, not a block, since the user is the one who knows the sbx they hold. **Open:** nothing records a minimum sbx today. Add the `COMPAT` asset (and a check in the running vibe: "this vibe needs sbx >= X") first; it is useful on its own, and the downgrade depends on it.
- **vibe and what it has written.** An older vibe reads `~/.vibe/state/*.yaml`, `settings.yaml`, profiles and the library written by a newer one. Unknown YAML fields and new feature folders need to be checked to be harmless to the last five releases; **spike:** run an older release against a `~/.vibe` a newer one has written, and list what breaks. The three-way merge handles the package's own files; it does not handle state the newer vibe added.
- **Sandboxes already built** by a newer vibe keep running; their files and kit are the newer ones. A rebuild (`vibe -C`) with the older vibe rebuilds them in the older shape, which is the point when the newer one is what broke. The downgrade says so, and does not rebuild for the user.
- **sbx and its sandboxes.** A sandbox made under a newer sbx may not start under an older one (kits, the agent). Same advice: rebuild. The sbx daemon is stopped before the change, as `--install-sbx` already does (`stopSbxDaemon`), and started after.

## sbx, by how it was installed

`vibe --downgrade sbx` goes by the method vibe installed it with, as the upgrade does; an sbx vibe didn't install is left alone and said so.

- **Linux** (release script) and **macOS tarball / .dmg**: the same code as the upgrade with an older tag; replacing the files is what they do already.
- **Windows MSI**: Windows Installer refuses to install an older version over a newer one, so the newer is uninstalled first (the user's or the machine's, whichever it was), then the older installed. **Check first** that this loses nothing the user keeps (their sandboxes live in sbx's data folder, which an uninstall should leave).
- **Homebrew cask**: the cask installs whatever version it currently has, so it can't go back. Say so, and offer the tarball method (which leaves the brew install in place or replaces it: to be decided with the macOS code in front of us).

## Where the list comes from

`releases/latest` gives one tag; five need a list. Candidates, to settle in a spike:

- **The Atom feed** (`<releases>.atom`): no API, no rate limit that matters. Pages are short (about ten entries), and sbx's feed is mostly RCs and nightlies, so five *stable* ones may be past the first page. **Check** whether it pages (`?page=2` or `?after=`).
- **The REST API** (`api.github.com/repos/<repo>/releases?per_page=100`): simple and complete, but anonymous calls are limited to 60 an hour per address (a shared office address can hit that). Acceptable for a command that is run in a crisis, if the feed doesn't do. If it is used, a failure says it was the limit and how to name a version directly (`vibe --downgrade vibe 0.1.57` needs no list).
- Naming a version directly never needs a list: it goes straight to `releases/download/v<version>/...`.

## The installer, for when vibe itself is broken

If the installed vibe can't run, `vibe --downgrade` can't help. So:

- `install.sh` and `install.ps1` learn to take a release: `install.sh --release 0.1.57` (before the options they pass to `vibe --install`; or `VIBE_RELEASE=0.1.57`), downloading from `releases/download/v0.1.57/` instead of `releases/latest/download/`, checked against that release's `SHA256SUMS`.
- The readme says, next to `--downgrade`, how to do it by hand with no vibe at all: download that release's zip and `SHA256SUMS`, unzip, run `vibe --install` from the folder.

## Tests

- **Listing:** from a stand-in server (as `selfupdate`'s tests point `Releases` at one): ordered numerically (0.1.9 before 0.1.10), the installed one excluded, only the five before it, sbx's RCs and nightlies skipped, fewer than five when that's all there is, a feed that fails says why.
- **Choosing:** the single choice; Esc changes nothing; `-f` without a version fails and prints the list; a version that isn't in the list, or isn't older, or has no release, is refused with the reason.
- **Doing it:** a release with a missing or wrong checksum is refused and nothing is replaced; the backup is made first and its failure stops the downgrade; a running session stops it.
- **The data:** run `--install` from an older package over a `~/.vibe` a newer one wrote (fixture folders): files the user never edited go back, edited ones are conflicts settled by the strategy, the state files are untouched.
- **sbx:** the per-method paths with stubs for the install scripts and `msiexec` (the MSI's uninstall-then-install order; the brew refusal); the daemon stopped and started around it.
- **The installer scripts:** `--release` picks the right URL (the scripts already take `VIBE_RELEASES` for a stand-in server).

## Not decided

- Whether "the five before the installed one" should instead be "the five before the *latest*": the first makes a second downgrade go on back from where the user is; the second always shows the same five. The plan takes the first, since the point is getting unstuck.
- A "hold" on a version, so `--upgrade -f` in a script, or the end-of-session note, leaves it alone after a downgrade. Not planned: the want is that `--upgrade` still offers the latest, and a hold is one more thing to forget.
- `--downgrade` for the sandboxes' agents (the claude CLI inside), which sbx's kit controls, not vibe.
