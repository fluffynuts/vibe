# npm: packages without giving the agent the host's npm (a basic plan; planned, not built)

Raised 2026-11-10, as the sibling of the NuGet design, which this takes its ideas from: `docs/adr/2026-10-18-nuget-without-the-hosts-nuget.md`. This is a
starting point to be turned into a design with an experiment first, as NuGet's was. Nothing is built.

## What is true today

- The `playwright` and `pnpm` features install Node tooling (`docs/adr/2026-10-28-playwright-feature.md`), and a project's `npm install` runs inside the
  sandbox, into the sandbox's own cache and `node_modules` (a project's `node_modules` is in the workspace, which is the host's folder).
- Nothing mounts the host's npm into the sandbox, so the agent can't read the host's `~/.npmrc` or plant code in the host's npm cache. There is no
  "ask the host for a private package" flow: a private registry needs a token, which the sandbox doesn't have.
- The package-request inbox on the Admin tab was built generic, so npm can join (the NuGet ADR says so).

## What to carry over from NuGet, and what doesn't carry

| NuGet idea | For npm |
|---|---|
| The agent never gets the host's config or credentials; it asks by name and exact version | Same, through the same generic request panel (ask or allow, batches, vibe judges new/upgrade/downgrade itself) |
| vibe fetches in a throwaway project on the host, never restoring the agent's project | The same shape, with a throwaway `package.json` and `npm install --ignore-scripts` (or `npm pack`), because **npm runs packages' lifecycle scripts**, which NuGet's restore doesn't |
| A read-only host cache as a fallback | **Doesn't carry:** npm's cache isn't made to be shared read-only, and a fetch lands in a cache the sandbox would have to read |
| Approving the workspace's `nuget.config` by content | `.npmrc` in the workspace can name registries and tokens: the same approval-by-content idea, with the same warning that the agent can edit it |
| Never edit `nuget.config` to get around a failure | Never edit `.npmrc` to get around one |

## To find out first (the experiment)

1. Where would a fetched package go that the sandbox can then use (a local tarball, a read-only folder, a registry mirror), given npm's cache can't be shared?
2. Whether `--ignore-scripts` on the host is enough, and what `postinstall`-style scripts do when the sandbox later installs the same package itself.
3. Private registries and scoped tokens (`@scope:registry`), and what a token in `~/.npmrc` would be exposed to.
4. pnpm: its store is content-addressed and can be shared differently; does that change the answer? (The `pnpm` feature exists; the request flow does not.)
5. Windows and macOS hosts, as for NuGet (unverified there).

## Out of scope for now

Anything npm-specific in the sandbox's own installs (the tooling features are built); a general "package manager" abstraction before there are two real ones.
