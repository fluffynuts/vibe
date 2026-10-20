## Before you restore or build

The user's machine can be holding something for them that you would otherwise
work around: package requests waiting for their approval, or packages this
sandbox restored that their package cache doesn't have yet (their IDE can't see
what you download yourself, and the files the build generates name your cache).
Before the first `dotnet restore`, `build` or `test` of a session, and again
after the user says they've dealt with it, run:

    vibe-nuget status

If it says something is waiting for the user, Tell the user what it says, and
wait for them: don't download your own copies of the packages in the meantime.
If it says nothing is waiting, carry on. If it can't answer, carry on and say so.

If it also says packages you downloaded yourself are now in the user's package
cache too, run `vibe-nuget tidy` and then `dotnet restore`: your own copies of
just those are dropped, so the files generated here name the user's cache, which
their IDE can read.

## NuGet packages from private feeds

This sandbox can restore packages from public feeds (nuget.org), but has none
of the user's NuGet configuration: private feeds and their credentials stay on
the host. A restore that fails with `NU1101` or `NU1102` for a package that
should come from a private feed is not yours to fix by configuration.

- Use the `vibe-nuget` skill. In short, ask the host for it: `vibe-nuget fetch <id> <version>` (`--framework net8.0`
  if the project's can't be read). It waits for the answer, which may need the
  user to approve the request on the companion page's Admin tab, then prints
  what happened. After it says the package is fetched, restore again.
- Never add or edit a `nuget.config`, or add a package source, to get around
  it. A `nuget.config` in this folder is used on the host only if the user
  approves it, and one you changed will be shown to them as changed.
- If the request is denied or fails, tell the user what it said; don't look
  for another way to the package.

## A build that can't find a package right after a restore

A package this sandbox used to find in the host's cache (the read-only folder
listed second in `obj/project.assets.json`'s `packageFolders`) can disappear
from it: the user cleared their cache, say. A plain `dotnet restore` can then
say it's up to date, exit 0 and download nothing, and the build fails: `CS0246`
for a type from the package, or `MSB3030` "could not copy" a file under
`.nuget/packages`. The `vibe-nuget` skill has the detail. In short:

1. `dotnet restore --force` downloads it into this sandbox's own cache.
2. If the build still fails the same way, the SDK's cache of where each package
   is (`obj/<configuration>/<framework>/<project>.assets.cache`) still names
   the old place, and a restore doesn't refresh it. Delete those files and
   build again:

       find . -path '*/obj/*' -name '*.assets.cache' -delete

They are derived files; nothing is lost. This isn't a reason to change a
`nuget.config`, the packages folder or `NUGET_PACKAGES`.
