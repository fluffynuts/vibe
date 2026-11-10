# NuGet: what is left (planned, not built; for when there is time)

The NuGet design is built and decided in `docs/adr/2026-10-18-nuget-without-the-hosts-nuget.md`. These are the NuGet-specific loose ends, as the
old plan left them. Nothing here is urgent; each needs a machine or an account that isn't the one the design was built on.

## Open

1. **Windows and macOS hosts.** Does a fetch without `--configfile` read the same configs there? Does the mount's path inside the sandbox match on
   Windows? The VM scripts (`docs/vm-tests/nuget/check-nuget.sh`, `check-nuget.ps1`) were written for exactly this, as plain `dotnet` scripts printing
   PASS or FAIL: read-only fallback; `dotnet nuget config paths`; planted `Directory.Build.*` and `nuget.config` ignored; real private feeds; Rider;
   on Windows, the mount's path inside the sandbox. They have not all been run. (The macOS VM can't run sbx.)
2. **Credential providers.** Azure Artifacts and some GitHub setups use NuGet plugins; they should work for the host's fetch as for any restore. Not
   tested; the readme says so and asks users who rely on it to file an issue.
3. **Real private feeds** (GitHub Packages, a credential provider) and **Rider on the overlay's result**: not tried.
4. **A fetch behind a live overlay on virtiofs:** not tried.

## Ideas not tried (the overlay, which is opt-in because of the 775 ownership problem)

- Mount with the agent's identity and an upper it can traverse.
- Shadow agent-owned directories per id in the upper (stale for ids the host adds later).

## Elsewhere

npm is a different problem with the same shape: see `docs/plan-npm-handling.md`.
