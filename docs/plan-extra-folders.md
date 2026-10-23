# Giving the agent more folders than the workspace (planned, not built)

Raised 2026-10-23. Today a sandbox has one project: its workspace. Two cases want more: two related projects the user would otherwise put under a folder of their own (the `yumbi-and-dashboard` folder, which is not a git repo: the git status, the Review tab, the default branch and the git author all behave better when the workspace *is* the repository), and a friend's World of Warcraft mods, where the agent wants the source folder and the folder it builds out to, to test.

## What there is

- `sbx create <agent> <path> [mounts…]` takes more folders after the workspace, each `path` or `path:ro` (`sbxrun.CreateOpts.Mounts`). vibe already uses it: `sandboxFolders` and `folder.mount()` in `src/vibe/folders.go` (the agent's memory store, the host's NuGet cache). `createSandbox` collects them (`src/vibe/create.go`). Each appears in the sandbox at the same path it has on the host.
- What a sandbox was built from is fingerprinted (`kitFingerprint`, mounts included), so a change to the folders makes the sandbox out of date, and an auto-update offers the rebuild.
- Settings layer (`~/.vibe/settings.yaml` under a profile's `settings.yaml`), with lists appended (`publish`).
- Not known, and the first thing to settle with the real sbx: whether a mount can be added to an existing sandbox, or only at `create`; and how sbx treats a path that doesn't exist, is a symlink, or lies inside another mount.

## What is wanted

A built-in way to say "also these folders", each read-write or read-only, that survives a rebuild, and that the user can see and change later.

**Proposed**

- **Where it is said.** Two places, for two meanings:
  - `--folder PATH[:ro]` (repeatable) on the command that creates or rebuilds the sandbox: this sandbox's own extra folders, recorded in its state record (`state.Instance`, like `Publish`), kept across `-r`, `-R` and `-C` as the published ports are, and shown by `--info`.
  - `folders:` in a `settings.yaml` (a list of `path` or `path:ro`): always-on folders, for every sandbox that layer applies to, appended like `publish` (the user's, then the profile's).
  - A folder added to an existing sandbox is a rebuild, and says so (it asks, as `-r` does): the sandbox is made again with the folder in.
- **Read-only is the cheap safe choice and the default offered**, read-write is a decision: the question at the console says what it is ("this lets the agent change files in /path"), and the WoW case would answer rw for the build output. The page shows the folders with their mode in the Admin tab's sandbox overview.
- **The agent is told.** The kit's agent instructions list the extra folders and their modes (and that the workspace is the one with the git repository), so it doesn't have to find them. A line in the generated text vibe already writes for the git rules (`syncGitInstructions` is the pattern).
- **Mechanical refusals, since the threat model is the agent** (vibe is for one user or a small team: the thing to guard against is the agent, not other users): a folder is refused, with the reason, when it is the user's home, `/`, a drive root, the vibe home, `~/.ssh`, `~/.aws`, `~/.config` and the other places credentials live (a list in one place, with a test that names each), a parent of the workspace (it would give the agent the workspace's siblings), inside vibe's or sbx's own state, or not there (never made for the user: a typo would make an empty folder). Symlinks are resolved first, and the *resolved* path is checked. Anything else is the user's call, and it is confirmed once, showing the resolved path and the mode.
- **The Review tab and the git status stay about the workspace** (they ask git in the workspace). A folder with its own repository shows only in the tree; reviewing it is a separate piece of work (the Review tab per folder), noted here and not done.
- **The file viewer (`docs/plan-plans-view.md`, `viewfile.go`) stays workspace-only:** an extra folder is the agent's, not a place the page opens files from, until that is decided on its own.

**Tests:** the argument parser (repeatable, `:ro`, Windows drive letters: `C:\x:ro` is a path with a mode, so the mode is parsed from the end); the refusal list, one case per entry and a resolved symlink into it; settings layering and the order; the sandbox's record survives `-r`, `-R`, `-C` (the existing rebuild tests are the pattern); the fingerprint changes with a folder and the auto-update offers the rebuild; `Mounts` reaches `sbx create` in order after the workspace; the instructions text lists them; `--info` shows them.

**Open questions for the user**

1. Per sandbox only, or the `settings.yaml` layer too? (The plan says both; the layer could wait.)
2. Is `--folder` the name? (`--with`, `--add`, `--mount` are the others; `--mount` says what it is, and sbx's own word.)
3. Should a folder be allowed to sit *inside* the workspace as a different mode (a read-only `vendor/`)? sbx may not allow overlapping mounts: check with the real sbx before promising it.
