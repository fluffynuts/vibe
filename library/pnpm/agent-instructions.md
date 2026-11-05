## pnpm

`pnpm` is on the PATH, through corepack: it runs the version the project's `package.json` names in `packageManager`
(or corepack's default when it names none), downloading it the first time, so the first `pnpm` in a project can take a
moment and needs the network. A project that changes its `packageManager` is followed the next time `pnpm` is run.
If `pnpm` isn't there, its install failed: the reason is in `/tmp/sbx-setup.log`.
