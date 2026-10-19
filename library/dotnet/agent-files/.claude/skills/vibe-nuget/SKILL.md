---
name: vibe-nuget
description: Get a NuGet package this sandbox can't restore. Use when dotnet restore or build fails with NU1101 or NU1102 (unable to find a package, or only older versions of it), or a package should come from a private feed or isn't on nuget.org. Asks the user's machine for it with vibe-nuget fetch; never edit nuget.config or add a source instead.
---

# Packages from private feeds

This sandbox restores from public feeds (nuget.org) and has none of the
user's NuGet configuration: their private feeds and credentials stay on their
machine. So a package from a private feed fails to restore here with
`NU1101` (unable to find the package) or `NU1102` (only other versions), and
that isn't something to fix with configuration. Don't ask the user for feed
URLs or credentials, and never add or edit a `nuget.config`, or add a package
source: ask their machine for the package instead.

## 1. Work out what's missing

Each `NU1101`/`NU1102` names a package and the version asked for. Collect the
distinct package ids and exact versions, from the restore output.

## 2. Ask for each

    vibe-nuget fetch <id> <version>

Add `--framework net8.0` (say) if the project's target framework can't be read
here. It waits for the answer, which can need the user to approve the request
on the companion page's **Admin** tab: it says so while it waits, and prints
what happened. Ask for all the packages you need before waiting on the
approvals, if you can run them together; the user approves them in one go.

An exact version only: not a range, and not `*`.

## 3. Restore again

When it says the package is fetched, run the restore or build again. A
package may bring dependencies that are also private: if the restore then
fails on one of those, ask for it the same way.

## If it isn't fetched

- **Denied:** tell the user, and stop. Don't look for another way to the
  package.
- **Failed:** the message is NuGet's own. Tell the user what it said. If it says
  the folder's `nuget.config` isn't approved, say that the user can approve it
  on the Admin tab, then ask again.
