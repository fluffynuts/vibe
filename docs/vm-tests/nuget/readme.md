# NuGet checks for a machine that can't run the tests

vibe's NuGet design (`docs/plan-nuget-feeds.md`) rests on how `dotnet` behaves: a read-only
package cache as a fallback, which config files a restore reads, and what a restore runs. These
scripts check that with plain `dotnet`, so they run where vibe's own tests can't (a macOS VM
that can't run `sbx`, a Windows machine).

- `check-nuget.sh` — macOS and Linux (`bash check-nuget.sh`). Run on Linux; not yet on macOS.
- `check-nuget.ps1` — Windows (`.\check-nuget.ps1`). Written without a Windows machine to run
  it on, and not run: if a check misbehaves, say which, and what it printed.

Each prints `PASS`, `FAIL` or `SKIP` per check and exits 1 if any failed. They work in a
temporary folder with a fake home and a NuGet config of its own, and change nothing of yours.
With `--private-package ID --private-version V` (`-PrivatePackage`/`-PrivateVersion` on Windows)
they also restore that package from the feeds in your own NuGet config.

Not automated, said as `MANUAL` at the end: Rider resolving packages after a fetch, and
credential providers (NuGet plugins), which vibe's fetch hasn't been tried with. The Windows
script also reports what path a read-only mount gets inside a sandbox, when `sbx` is there.
