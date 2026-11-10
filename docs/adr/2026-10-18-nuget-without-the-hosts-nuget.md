# NuGet packages without giving the agent the host's NuGet

- **Status:** accepted, built and merged (`feat/nuget-feeds`, then the edge-case fixes). What is left that is NuGet's own is in
  `docs/plan-nuget-next-steps.md`, and the same idea for npm is in `docs/plan-npm-handling.md`.
- **Decided:** 2026-10-18, with the user; the overlay was made opt-in on 2026-10-19.
- **Came from:** `docs/plan-nuget-feeds.md`, now deleted: `git show 23f5743:docs/plan-nuget-feeds.md`. Its experiments are kept as checks in
  `docs/vm-tests/nuget/` (`check-nuget.sh`, `check-nuget.ps1`).

## Context

The dotnet feature mounted the host's whole `~/.nuget` into the sandbox, read-write, and pointed `NUGET_PACKAGES` at it. That shared the
package cache (no downloads per sandbox, and `obj/project.assets.json` named paths Rider on the host could resolve), but the agent could then
**plant code in any package** (a `build/*.targets`, a DLL), which every project on the host using that package runs at its next restore or build,
beyond this project, outliving the sandbox, in no diff; **read the host's NuGet config**, whose private feeds' URLs and credentials are in it; and
**reach `~/.nuget/plugins`**, credential providers NuGet runs. Private feeds are common (GitHub Packages, an older NuGet server), often more than
one, so the fix had to keep them working.

## Decision

- **The host's package cache is shared read-only, as a fallback; nothing else of `~/.nuget` enters the sandbox.** The sandbox mounts
  `<nugetDir>/packages` read-only at its host path and sets `NUGET_FALLBACK_PACKAGES` to it, leaves `NUGET_PACKAGES` unset (the sandbox's own
  cache, writable, for public packages and SDK packs), and sets `RestoreIgnoreFailedSources=true`, so a private feed the sandbox can't reach is a
  warning, not a failure. (Read-only as the *only* cache fails once the sandbox's SDK needs a pack the host never fetched; hence the fallback.)
  `link-nuget` writes the environment and no longer symlinks `~/.nuget`.
- **A private package the sandbox can't get, the agent asks vibe for**, by id and exact version, with `vibe-nuget fetch <id> <version> [--framework]`,
  which drops a request in an inbox folder on the host (the pattern `vibe-review` uses: inbox read-write, answers in a read-only view) and waits for
  the answer. The agent's instructions say to ask on `NU1101`/`NU1102` for a package from a private feed, and never to edit a `nuget.config` to
  get around it.
- **vibe fetches, and never restores the agent's project on the host.** The request is data only (an id matching `^[A-Za-z0-9_.-]+$`, a SemVer 2
  version, a framework from a known set). vibe writes a throwaway project under `~/.vibe/nuget-fetch/<random>` (a `PackageReference` with an exact
  `[version]`), restores it with `-p:ImportDirectoryBuildProps=false -p:ImportDirectoryBuildTargets=false -p:ImportDirectoryPackagesProps=false` and
  `NUGET_PACKAGES` the host's cache, and deletes it. **No `--configfile`:** NuGet reads the user's own config as for any project, so nothing is set up per feed; a
  `nuget.config` planted in the workspace is ignored (it is not an ancestor), while one in an *ancestor* of the throwaway folder would be read,
  so the folder and its parents are never mounted into any sandbox (a test pins that against `sandboxFolders`). A restore runs no package code, so the
  host only gains files, from the user's own feeds. Bounded: a time limit, one fetch at a time, and each fetch is shown on the Admin tab and in vibe's
  output.
- **Requests are approved in batches on the Admin tab** (a generic "pending package requests" panel, so npm can join): a tickbox per request (all
  ticked), "Allow selected" and "Deny all", an attention dot on the tab. `packages: ask|allow` in settings, default `ask`. **vibe decides itself**
  whether a request is missing, new, an upgrade or a downgrade, reading the project files as plain text where the branch left the default branch;
  it never takes the agent's word.
- **A workspace `nuget.config` is used only once the user approves it** on the Admin tab, which shows its sources (names and URLs), and the approval is
  keyed by the file's content, so any change asks again. The agent can edit that file, and the experiment showed a redefined source name sends the
  stored credentials to whatever URL it names. An approved copy sits beside the throwaway project, so NuGet layers it over the user's config.
- **Modes, opt-in** (`nuget:` in settings, default `fallback`): `shared` is today's read-write sharing, for a remote machine or anyone who would
  rather have no friction, and creating such a sandbox says plainly what the agent can do with it; `overlay` (2026-10-19) overlays the host's
  cache at its own path (lower: the read-only mount; upper: a folder on a disk that isn't an overlay) and sets `NUGET_PACKAGES` to it, so the files a restore
  writes name the host's path, which Rider resolves. `vibe-nuget fetch` drops the kernel's dentry caches on success, as an overlay answers "missing"
  for what it looked for before the host added it.

**What the experiments showed** (.NET SDK 10.0.112 on Linux, with stand-ins for the feeds): a read-only host cache as a fallback restores and builds
and the host cache is unchanged; `Directory.Build.props`/`.targets` planted above the fetch's folder are not run with the three `Import*=false` switches;
**restoring the agent's project on the host ran a command a `Directory.Build.targets` hooked onto restore**, which is why vibe never does;
a `nuget.config` redefining a source's name sent the stored username and password to the other URL.

## Consequences

- The agent can read the host's package cache but cannot change it, and cannot reach the host's NuGet config, plugins or credentials. The cost is a
  second or two for a private package the agent adds, and that a public package or SDK pack lands in the sandbox's own cache, so Rider on the host
  doesn't resolve it until the host restores.
- Existing sandboxes get this at their rebuild: the mount is set at creation, and the kit fingerprint changes, so they show as out of date.
- **Why the overlay is opt-in (2026-10-19):** a restore in a real project failed with "Permission denied" creating `<id>/<version>` beside a
  host-fetched version, because the host mount reports a file's owner as whoever asks and an overlay looks its lower layer up as its mounter, root, so
  every lower directory is root's 775 to the agent. `nooverride_creds` was accepted with no effect, and mounting with `setfsuid(1000)` failed (the
  upper isn't traversable as the agent).
- **A warning that cost something:** `mount -o remount,rw|ro <path>` on a virtiofs mount of the sandbox, without `bind`, changes the superblock all the
  sandbox's host mounts share (the repository included); use `remount,bind,ro|rw`.

## Not built, and not verified

All in `docs/plan-nuget-next-steps.md`: credential providers (NuGet plugins) are a TODO (the readme says the flow is untested and asks users who rely
on it to file an issue); Windows and macOS hosts are unverified (the VM scripts exist, the macOS VM can't run sbx); the overlay's untried ideas; and Rider
on the result. npm is its own plan.
