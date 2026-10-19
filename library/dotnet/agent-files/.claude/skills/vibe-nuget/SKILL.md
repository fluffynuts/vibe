---
name: vibe-nuget
description: Get a NuGet package this sandbox can't restore, or can't find after a restore. Use when dotnet restore or build fails with NU1101 or NU1102 (unable to find a package, or only older versions of it), or a package should come from a private feed or isn't on nuget.org - ask the user's machine with vibe-nuget fetch, and never edit nuget.config or add a source instead. Also use when a build fails with CS0246 for a package's types, or MSB3030 could not copy a file under .nuget/packages, right after a restore that said it was up to date - run dotnet restore --force. Before the first restore or build of a session, run vibe-nuget status to learn what the user has waiting, and tell them if something is.
---

# Before a restore or build

Run `vibe-nuget status` before the first restore, build or test of a session,
and again once the user says they've dealt with what it named. It asks the
user's machine, at once, what is waiting for them: package requests to
approve, or packages this sandbox restored that their package cache lacks (so
their IDE can't see them). If it says something is, Tell the user what it
says, and wait for them: a restore or build now would download your own copies
of the packages, which their IDE won't see either. If it says nothing is
waiting, carry on. If it can't answer, carry on and say so.

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

- **Denied:** the message says whether the user *declined* the request, or
  *left it out* when deciding on several, which may have been a slip. Nothing
  is broken: it's their choice, and the request is gone from their list. Say
  what you're blocked on and ask the user kindly and openly, for example: "Did
  you mean to leave out Foo? If not, I'll ask again and you can approve it; if
  you did, what shall we do instead?" If it was a slip, run `vibe-nuget fetch`
  for it again, which raises a fresh request for them to approve; never tell
  them to approve it again, since there is nothing to approve until you ask.
  Otherwise don't look for another way to the package, and don't ask again
  unless the user says to.
- **Failed:** the message is NuGet's own. Tell the user what it said. If it says
  the folder's `nuget.config` isn't approved, say that the user can approve it
  on the Admin tab, then ask again.

# A build that can't find a package, after a restore that worked

A different failure, with no `NU1101`: the restore exits 0, perhaps saying
"up to date", and the build then fails with `CS0246` for a type from a
package, or `MSB3030` "could not copy" a file under `.nuget/packages`. A
package this sandbox found in the user's cache (the read-only folder it falls
back on) has gone from it: they cleared it, say. A plain restore can miss that.

1. Run `dotnet restore --force`. It downloads the package into this sandbox's
   own cache. A package from nuget.org needs nothing more; ask the host for it
   (above) only if the restore then fails with `NU1101` or `NU1102`.
2. If the build still fails the same way, the SDK's cache of where each package
   is (`obj/<configuration>/<framework>/<project>.assets.cache`) still names the
   old place, and a restore doesn't refresh it. Delete those files and build
   again:

       find . -path '*/obj/*' -name '*.assets.cache' -delete

They are derived files; nothing is lost. Neither step is a reason to change a
`nuget.config`, the packages folder or `NUGET_PACKAGES`.
