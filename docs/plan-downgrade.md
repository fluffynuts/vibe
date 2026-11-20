# `vibe --downgrade`: what is left (planned, not built)

The first version is built: one step back for vibe or sbx, opt-in, `~/.vibe` backed up first, what runs stopped first once the user says so
(`docs/adr/2026-11-17-downgrade.md`). What follows is the wider plan it was cut from, kept for later: more than one step back, naming a
version, a cache of downloaded releases, which sbx a vibe release works with, the installer taking a release, and going back for Homebrew
and MSI installs. Raised 2026-11-04, the day an sbx update (v0.51.0) stopped every vibe sandbox from starting. The whole plan as it was is
`git show 7b56280:docs/plan-downgrade.md`.

## Further than one step back

`vibe --downgrade [vibe|sbx] [VERSION]` (no short form: a flag nobody should hit by accident).

1. **What to go back on.** With no product given, at a terminal it asks (vibe, sbx), showing what is installed of each. `-f` and "no terminal" don't guess: they need the product and the version, and fail saying so, with the list printed (so it is scriptable and works over ssh).
2. **The list.** The **five releases before the installed one**, newest first, numbered, the installed one shown at the top as "(installed)" and not selectable. For sbx only stable releases count (`sbxinstall.Stable`), so the five are five stable ones, not RCs and nightlies. Choosing is a single choice (this is not the upgrade's multi-select), and Esc leaves everything as it was. Going back further than five is done by running it again, now from the older one, or by naming a version: `vibe --downgrade vibe 0.1.41`.
3. **A development build** has no build number and counts as older than any release of its version, so for it the list is simply the latest five releases, said so in a line above it.
4. **Doing it** is the upgrade's own code with a chosen tag instead of the latest: `upgradeVibe(tag, ...)` for vibe, the install method's own path for sbx (see below). It checks the checksum like any download, and a release with no `SHA256SUMS` is refused, never installed unchecked.
5. **After it.** Says what is now installed, says that `vibe --upgrade` will offer the newest again (and that vibe's end-of-session "newer release available" note will go on appearing: that is what the user asked for, so it is not suppressed), and, if sandboxes exist, says whether they need `vibe -C` (see "Skew").

## A local cache of the releases vibe has downloaded

Raised the same day: keep the release zips vibe downloads, so most rollbacks need no network and no GitHub listing. A new install has nothing cached, so this *adds to* the upstream list rather than replacing it.

- **Where and what.** `~/.vibe/releases/vibe-<version>-<os>-<arch>.zip`: the version is in the name, because the zips are published under fixed names with no version in them. Next to each, the line from that release's `SHA256SUMS` for it (`<zip>.sha256`), so a cached file is checked again before it is used and a damaged or tampered one is refused (and offered fresh from upstream). Only this machine's own platform is kept.
- **When it is filled.** `upgradeVibe` today downloads into a temp folder and deletes it; after the checksum passes it would first copy the zip into the cache (a failure to cache only warns: it must never fail an upgrade). A downgrade that fetches from upstream caches what it fetched too.
- **Installs that don't go through `upgradeVibe`.** `install.sh` / `install.ps1` unpack a release and run `vibe --install` from it, which never sees the zip. They would hand the zip's path on (say `VIBE_RELEASE_ZIP`, read by `--install`, which copies it into the cache once the install has worked), so a first install is cached as well.
- **The list is the two sources together.** Versions are matched by number; each row says where it is ("downloaded" or "on GitHub"; both when both). With the network down, or GitHub's list failing, the cached versions are still shown, with a line saying the upstream list couldn't be read: the failure is a note, not an error. Choosing a cached one skips the download and still verifies it. The "five before the installed one" rule applies to the merged list, so the cache can only add rows, never hide upstream's.
- **The cold start.** The release that first has this feature caches what it installs, but the version it replaced was never cached, so the first rollback after upgrading to it comes from upstream. That is expected, and said in the readme; re-packing the installed bundle to fill the gap isn't worth doing.
- **Keeping it small.** A vibe zip is about 10 MB. Keep the newest six by version (the installed one included, newer ones too: after a downgrade, the release just left is what `--upgrade` can reinstall without downloading). Never delete the installed version's zip. Pruning happens after a successful install, and only touches files whose names match the pattern, so nothing of the user's is at risk.
- **Not in the backup.** The pre-downgrade backup of `~/.vibe` leaves `releases/` out (they are re-downloadable, and make the backup many times larger), as it leaves out other things that are caches.
- **sbx.** Its release downloads are much larger and differ by method (tarball, .dmg, .msi), so they are **not** cached in the first version: an sbx downgrade comes from upstream. The size is to be measured before deciding whether to keep just the one before the installed.
- **`--upgrade` can use it too:** when the latest is already cached (say, after a downgrade from it), it installs from the cache after verifying, rather than download it again.

## Skew: the part that needs care

A downgrade is only coherent if the other pieces still fit, and today's incident was exactly such a mismatch.

- **vibe ↔ sbx.** vibe 0.1.N writes a schemaVersion 3 kit, which needs the sbx that has the v3 claude agent; an older vibe writes a v2 kit, which a newer sbx refuses to compose with that agent. Going back on one without the other can break every sandbox. Plan: each release carries a small `COMPAT` asset (the sbx versions it was built and tested for: a minimum, and the newest tried), and the picker marks any choice whose pair would be outside it ("works with sbx >= 0.51.0: you have 0.49.2"). It warns, and the user can still go on: a warning, not a block, since the user is the one who knows the sbx they hold. **Open:** nothing records a minimum sbx today. Add the `COMPAT` asset (and a check in the running vibe: "this vibe needs sbx >= X") first; it is useful on its own, and the downgrade depends on it.
- **The agent's kit isn't tied to sbx's version.** A sandbox build prints `claude - now ships as a kit; resolving docker.io/sbx/claude:latest` and then the digest it got (seen 2026-11-04, sbx v0.51.0): the claude kit is fetched from the registry at creation, as `:latest`, not shipped with the sbx binary. So the v3 requirement that broke vibe may come from that kit, not from sbx 0.51, which means (a) **no minimum sbx version can be read off sbx's own version** until an older sbx is tried against today's kit (that test belongs to this work), (b) **downgrading sbx may not bring the old behaviour back**, since the kit stays the newest, and (c) **vibe can break again without any sbx or vibe release**, whenever that kit changes. Open: whether sbx lets a kit reference be pinned (a tag or digest) so vibe or the user could hold the agent on one that works; if it does, a rollback should pin it, and `COMPAT` should name the kit digest it was tested with.
- **vibe and what it has written.** An older vibe reads `~/.vibe/state/*.yaml`, `settings.yaml`, profiles and the library written by a newer one. Unknown YAML fields and new feature folders need to be checked to be harmless to the last five releases; **spike:** run an older release against a `~/.vibe` a newer one has written, and list what breaks. The three-way merge handles the package's own files; it does not handle state the newer vibe added.
- **Sandboxes already built** by a newer vibe keep running; their files and kit are the newer ones. A rebuild (`vibe -C`) with the older vibe rebuilds them in the older shape, which is the point when the newer one is what broke. The downgrade says so, and does not rebuild for the user.
- **sbx and its sandboxes.** A sandbox made under a newer sbx may not start under an older one (kits, the agent). Same advice: rebuild. The sbx daemon is stopped before the change, as `--install-sbx` already does (`stopSbxDaemon`), and started after.

## sbx, by how it was installed

Built: the Linux release script and the macOS tarball or `.dmg` go back as they go forward. Left: the two that today only say how to do it by hand.

- **Windows MSI**: Windows Installer refuses to install an older version over a newer one, so the newer is uninstalled first (the user's or the machine's, whichever it was), then the older installed. **Check first** that this loses nothing the user keeps (their sandboxes live in sbx's data folder, which an uninstall should leave).
- **Homebrew cask**: the cask installs whatever version it currently has, so it can't go back. Say so, and offer the tarball method (which leaves the brew install in place or replaces it: to be decided with the macOS code in front of us).

## The installer, for when vibe itself is broken

If the installed vibe can't run, `vibe --downgrade` can't help. So:

- `install.sh` and `install.ps1` learn to take a release: `install.sh --release 0.1.57` (before the options they pass to `vibe --install`; or `VIBE_RELEASE=0.1.57`), downloading from `releases/download/v0.1.57/` instead of `releases/latest/download/`, checked against that release's `SHA256SUMS`.
- The readme says, next to `--downgrade`, how to do it by hand with no vibe at all: download that release's zip and `SHA256SUMS`, unzip, run `vibe --install` from the folder.

## Tests

- **The cache:** a download is cached under its versioned name with its checksum line, and a failure to cache doesn't fail the upgrade; a cached file whose checksum no longer matches is refused and not installed; the merged list shows a cached version when the network list fails (with the note), shows a version once when both have it, and still stops at five; pruning keeps the newest six, never the installed one's removal, and ignores files that don't match the pattern; `--install` with `VIBE_RELEASE_ZIP` set caches that zip, and without it caches nothing.
- **Listing:** from a stand-in server (as `selfupdate`'s tests point `Releases` at one): ordered numerically (0.1.9 before 0.1.10), the installed one excluded, only the five before it, sbx's RCs and nightlies skipped, fewer than five when that's all there is, a feed that fails says why.
- **Choosing:** the single choice; Esc changes nothing; `-f` without a version fails and prints the list; a version that isn't in the list, or isn't older, or has no release, is refused with the reason.
- **The data:** run `--install` from an older package over a `~/.vibe` a newer one wrote (fixture folders): files the user never edited go back, edited ones are conflicts settled by the strategy, the state files are untouched.
- **sbx:** the per-method paths with stubs for the install scripts and `msiexec` (the MSI's uninstall-then-install order; the brew refusal); the daemon stopped and started around it.
- **The installer scripts:** `--release` picks the right URL (the scripts already take `VIBE_RELEASES` for a stand-in server).

## Not decided

- Whether "the five before the installed one" should instead be "the five before the *latest*": the first makes a second downgrade go on back from where the user is; the second always shows the same five. The plan takes the first, since the point is getting unstuck.
- The cache's folder name and whether it should live under `~/.vibe` at all rather than the user's cache folder (it is a cache, but `~/.vibe` is where the user already looks, and `$VIBE_HOME` moves it with the rest). The plan takes `~/.vibe/releases/`.
- How many to keep (six, above, is a first guess) and whether the user can say (a `settings.yaml` value) or empty it (`vibe --cleanup` is the existing home for that kind of thing: to be looked at).
- A "hold" on a version, so `--upgrade -f` in a script, or the end-of-session note, leaves it alone after a downgrade. Not planned: the want is that `--upgrade` still offers the latest, and a hold is one more thing to forget.
- `--downgrade` for the sandboxes' agents (the claude CLI inside), which sbx's kit controls, not vibe.
