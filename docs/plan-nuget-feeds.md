# Plan: NuGet packages without giving the agent the host's NuGet

Status: agreed 2026-10-18, building on branch `feat/nuget-feeds`. Experiment: `/tmp/nx` in the sandbox it was run in
(results below).

## The problem

The dotnet feature mounts the host's whole `~/.nuget` into the sandbox, read-write, and points
`NUGET_PACKAGES` at it (`library/dotnet/settings.yaml` `nugetDir`, `src/vibe/create.go`, `link-nuget`). That
shares the package cache (no downloads per sandbox, and `obj/project.assets.json` names paths Rider on the
host can resolve). But the agent can then:

- **Plant code in any package** (`build/*.targets`, a DLL): every project on the host using that package
  runs it at its next restore or build. Beyond this project, outliving the sandbox, in no diff.
- **Read the host's NuGet config**, `~/.nuget/NuGet/NuGet.Config`: private feeds' URLs and credentials.
- **Reach `~/.nuget/plugins`**, credential providers NuGet runs.

Private feeds are common (ours: GitHub Packages, and an older NuGet server still around), often more
than one, so the fix has to keep them working.

## The outcome

- The host's package cache is shared **read-only**; nothing else of `~/.nuget` enters the sandbox.
- The sandbox gets **public** packages itself, into a cache of its own.
- A **private** package the sandbox can't get, the agent asks vibe for, by name and version. vibe fetches
  it on the host, with the host's own NuGet config (every feed, every credential, nothing to set up per
  feed), into the host's cache, where the sandbox then finds it.
- vibe **never restores the agent's project** on the host, and never uses a `nuget.config` from the
  workspace that the user hasn't approved.
- Today's read-write sharing stays available as an explicit opt-in.

## What the experiment showed

.NET SDK 10.0.112 on Linux; a stand-in host cache made read-only and fingerprinted (any write would show);
packages of my own with `build/*.targets`, from a local feed, an authenticated HTTP feed (stand-in for the
old server) and a second local feed (stand-in for GitHub Packages); and nuget.org.

| Question | Result |
|---|---|
| Read-only host cache as a **fallback** (`NUGET_FALLBACK_PACKAGES`), sandbox cache writable | Restores and builds. Cached packages are used in place (their build targets run); only new ones land in the sandbox's cache. Host cache unchanged. |
| Read-only host cache **as the only cache** (`NUGET_PACKAGES`), everything cached | Works, writes nothing — but fails as soon as the sandbox's SDK needs a pack the host never fetched (here the net8.0 app host): which packs, and which versions, depend on the sandbox's SDK, so the host can't fetch them ahead. Hence the fallback design. |
| A private source the sandbox can't authenticate to | NuGet checks every source's index even when all is cached: `NU1301` (401) fails the restore. With `RestoreIgnoreFailedSources=true` (an MSBuild property, set from the environment) it's a warning. |
| A private package not yet on the host | Clear: `NU1101: Unable to find package NewCorp`. |
| vibe's fetch: a throwaway project in a folder of vibe's, restored on the host with the host's NuGet config | Fetched a package from the "GitHub" feed, its dependency from the authenticated feed (with the stored password) and one from nuget.org, into the host cache. The sandbox then built. |
| `Directory.Build.props`/`.targets` planted above the fetch's folder (Exec on restore) | Not run, with `-p:ImportDirectoryBuildProps=false -p:ImportDirectoryBuildTargets=false -p:ImportDirectoryPackagesProps=false`. |
| `nuget.config` planted above the fetch's folder | Ignored, with `--configfile` naming the user's config. Its server got no request. |
| **Restoring the agent's project on the host** (the tempting shortcut) | `dotnet restore` ran a command a `Directory.Build.targets` hooked onto restore: code execution on the host. |
| A workspace `nuget.config` redefining a source's name (key) to another URL, restored on the host | NuGet sent the stored username and password for that name to the other URL, once it asked for them. |

Not verified: Rider itself; macOS and Windows hosts; a real `sbx` read-only mount (vibe already mounts the
review view `:ro`); real private feeds (GitHub Packages, a credential provider); npm.

## Design

### The sandbox

- Mount `<nugetDir>/packages` read-only, at its host path (`:ro`). Nothing else of `~/.nuget`.
- Environment (persisted, as `link-nuget` does for `NUGET_PACKAGES` today):
  - `NUGET_FALLBACK_PACKAGES=<host path>/packages` — the host's cache, read-only.
  - `NUGET_PACKAGES` unset: the sandbox's own `~/.nuget/packages`, writable, for public packages and SDK
    packs.
  - `RestoreIgnoreFailedSources=true` — a private feed the sandbox can't reach is a warning, not a failure.
- `link-nuget` stops symlinking `~/.nuget` to the host; it only writes the environment.

### The agent asks for a package

- A command in the dotnet feature, `vibe-nuget fetch <id> <version> [--framework <tfm>]` (the framework
  defaults to the project's, read in the sandbox with `dotnet msbuild -getProperty`), drops a request in an
  inbox folder on the host, mounted into the sandbox — the pattern `vibe-review` uses (inbox read-write,
  answers in a read-only view). It waits for the answer and prints it: fetched (with what came along), or
  why not.
- The agent's instructions (the feature's `agent-instructions.md`): on `NU1101`/`NU1102` for a package from
  a private feed, ask with `vibe-nuget fetch`, then restore again. Never add or edit a `nuget.config` to get
  around it.

### vibe fetches

- The request is data only: an ID and an exact version checked against NuGet's rules (`^[A-Za-z0-9_.-]+$`,
  a SemVer 2 version), a framework from a known set. Nothing from the workspace is read, bar an approved
  `nuget.config` (below).
- vibe writes a throwaway project in a folder of its own under `~/.vibe/nuget-fetch/<random>` (`PackageReference`
  with an exact `[version]`, the framework), restores it with the three `Import*=false` switches and
  `NUGET_PACKAGES` = the host's cache, then deletes the folder. No `--configfile`: NuGet reads the user's own
  config as for any project, so the user sets nothing up (experiment 2, below). The folder and its parents
  must not be mounted into any sandbox, since NuGet reads a `nuget.config` in any parent; a unit test pins that
  against `sandboxFolders`. A restore runs
  no package code, so the host only gains files, from the user's own feeds.
- Bounded: a time limit, one fetch at a time, and the answer says what was fetched or what NuGet said.
- Every fetch is shown on the companion page (Admin tab's log) and in vibe's output.

### A workspace `nuget.config`

Many repositories name their private feeds in their own `nuget.config` (credentials in the user's). The
agent can edit that file, and the experiment showed a redefined source name sends the stored credentials to
whatever URL it names. So:

- vibe uses it only once the user has approved it on the Admin tab, which shows its sources (names and
  URLs). The approval is remembered by the file's content; any change asks again.
- An approved copy is put beside the throwaway project, so NuGet layers it over the user's config (verified).

### Opt-in: share it all, read-write

`settings.yaml` `nuget: shared` (default `fallback`, as above): today's behaviour, for a remote machine or
anyone who'd rather have no friction. Creating such a sandbox says plainly what the agent can do with it.

## What the user sees

- Packages already on the host: as today.
- A private package the agent adds: it asks, vibe fetches, the page says so; a second or two.
- A public package the agent adds, or an SDK pack: in the sandbox's cache, so Rider on the host doesn't
  resolve it until the host restores (its next build, from the user's own feeds). Shown in the restore's
  output as today.
- A workspace `nuget.config` changed by the agent: the page asks before vibe uses it.

## Existing sandboxes

The mount is set at creation, so the change reaches a sandbox at its rebuild; the kit fingerprint changes,
so `--update-sandboxes` (and the session-start check) see them as out of date.

## Tests

- Unit: request validation (IDs, versions, frameworks); the throwaway project's content and command line
  (the `Import*=false` switches, `--configfile`, exact versions); approval keyed by content.
- Integration (needs `dotnet`, skipped without, as the git tests are): a fetch from a local feed into a
  cache; planted `Directory.Build.targets` and `nuget.config` above the fetch's folder not used; the
  sandbox-side restore against a read-only fallback (the experiment's checks, kept).
- Kit: the dotnet feature mounts `packages` `:ro` and sets the environment; `nuget: shared` keeps today's.

## Decisions of 2026-10-18

- Requests are approved in batches on the Admin tab: a generic "pending package requests" panel (npm can join
  later), a tickbox per request (all ticked), "Allow selected" and "Deny all", and an attention dot on the tab.
  Setting `packages: ask|allow`, default `ask`. vibe decides itself whether a request is missing, new, upgrade
  or downgrade, reading the project files as plain text where the branch left the default branch; it never
  takes the agent's word.
- Standalone test scripts for the Windows and macOS VMs (the macOS VM can't run sbx): plain `dotnet` scripts
  printing PASS or FAIL: read-only fallback; `dotnet nuget config paths`; planted `Directory.Build.*` and
  `nuget.config` ignored; real private feeds; Rider; on Windows, the mount's path inside the sandbox.
- Credential providers (NuGet plugins) are a TODO; the readme says the flow is untested and asks users who rely
  on it to file an issue.
- NuGet is its own branch; npm follows as its own experiment.
- Nothing here depends on a real `sbx`; the read-only mount is covered by unit tests of the mount list only.

## Experiment 2 (no `--configfile`)

.NET SDK 10.0.112, fake home. Throwaway project under `~/.vibe/fetch`, the three `Import*=false` switches:
the user's `NuGet.Config` alone is read; a `nuget.config` in the workspace is ignored (not an ancestor); one in
`~/.vibe` (an ancestor) is read, so ancestors must be unwritable by the agent; an approved config beside the
project is layered over the user's. Windows and macOS: unverified.

## Open questions

1. Windows and macOS: does the no-`--configfile` fetch read the same configs? (VM scripts.)
2. Credential providers (Azure Artifacts, some GitHub setups) run as NuGet plugins: they should work for the
   host's fetch as for any restore. Not tested.
3. (Settled: ask by default; `packages: allow` skips it.)
4. npm: the same idea, but `npm install` runs packages' scripts (`--ignore-scripts` on the host) and npm's
   cache isn't made to be shared read-only. Its own experiment, after this.
