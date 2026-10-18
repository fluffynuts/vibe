## NuGet packages from private feeds

This sandbox can restore packages from public feeds (nuget.org), but has none
of the user's NuGet configuration: private feeds and their credentials stay on
the host. A restore that fails with `NU1101` or `NU1102` for a package that
should come from a private feed is not yours to fix by configuration.

- Ask the host for it: `vibe-nuget fetch <id> <version>` (`--framework net8.0`
  if the project's can't be read). It waits for the answer, which may need the
  user to approve the request on the companion page's Admin tab, then prints
  what happened. After it says the package is fetched, restore again.
- Never add or edit a `nuget.config`, or add a package source, to get around
  it. A `nuget.config` in this folder is used on the host only if the user
  approves it, and one you changed will be shown to them as changed.
- If the request is denied or fails, tell the user what it said; don't look
  for another way to the package.
