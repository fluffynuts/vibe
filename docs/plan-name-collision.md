# Two folders, one sandbox name: refuse to attach to the other folder's sandbox, and offer a way on (planned, not built)

Raised 2026-11-04. A sandbox's name is the folder's name, lowercased (`pathresolve.DeriveName`: `~/.vibe` and `~/code/opensource/vibe` are both `vibe`; so are `~/a/api` and `~/b/api`). Nothing stops the second from using the first's sandbox.

## What there is

- `doCreateOrAttach(name, target)` (`src/vibe/create.go`): if `sbxrun.Exists(name)`, it attaches to that sandbox. It never compares `target` with the folder the sandbox was made for (`state.Instance.Target`, in `~/.vibe/instances/<name>.yaml`). The sandbox is mounted on the *first* folder, so the agent started "in" the second works on the first folder's files, and the Review tab, the clipboard, the published ports and the profile are the first folder's too, with nothing said.
- `guardFolder` / `running.Claims` (`src/vibe/folderlock.go`) lock by *folder*, so two vibes for two folders with one name don't see each other either.
- The profile has the same collision: `resolveProfileName` is `-p`, else the name, else one derived from the folder, so the second folder also picks up the first's profile, shared.
- `--migrate` and `--clone` (`docs/plan-clone.md`) already refuse a name that is taken, and plan-clone already has the three profile choices: share, copy as a seed, or another one.

## Proposed

Settle the name before anything else, from the folder, not only from its name:

1. **Folder first.** If an instance record's `Target` is this folder (resolved, as `main.go` resolves it), that record's name is the folder's, whatever `DeriveName` says. This is what lets a folder keep a name other than the derived one across runs; without it, the second folder would meet the same question every time. (Cheap: a scan of `instances/*.yaml`; compare resolved paths, and on macOS and Windows compare case-insensitively.)
2. **Else derive the name** as today. If a sandbox or record of that name exists for **another folder**, stop with the choice below instead of attaching.
3. **The choice, at a terminal** ("sandbox 'vibe' already belongs to ~/code/opensource/vibe; this folder is ~/.vibe"):
   - **Make a separate sandbox for this folder** (the default), under a name vibe suggests from the folder and its parent (`davydm-vibe`; the user can edit it; a name that is also taken is offered again, never reused). It then asks the profile question from plan-clone: *a copy of the profile* (default; it goes its own way), *the same profile, shared*, or *another profile*. The name is remembered by the record (step 1), so the next `vibe` here goes straight in.
   - **Cancel.**
   - Not offered: attaching to the other folder's sandbox. If that is really wanted, the user runs vibe in that folder.
4. **No terminal, or `-f`:** refuse with the same explanation and what to type (`--name <other>`), a non-zero exit, nothing created. Never guess a name unattended.
5. **An explicit `--name X`** that belongs to another folder is refused the same way (no menu: the user asked for that name), saying whose it is.
6. **Orphans.** A record whose sandbox has gone (or a sandbox with no record) is not "another folder's": the existing orphan handling (`clearOrphanedSandbox`) applies first.

## Tests

- `DeriveName`-equal folders: the second is refused, not attached, and nothing of the first's (state, profile, ports) is changed; the first still attaches.
- A folder with a record keeps its name even if the derived name would now differ or be taken.
- Terminal choice: separate sandbox with a copied profile (the copy exists, the original untouched), with the shared profile, and cancel; unattended and `-f` refuse; `--name` of another's refuses.
- Resolved paths: a link to the first folder is the first folder; case differences on a case-insensitive filesystem are the same folder.

## Not decided

- Whether the suggested name uses the parent folder or a counter (`vibe-2`). The parent reads better and is stable if the user moves things around less than they add folders; a counter is simpler.
- What `--list` and the page's sandbox list show for a sandbox whose name isn't its folder's (the folder, probably, as the Admin page already shows the target).
