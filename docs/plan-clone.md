# `vibe --clone`: a second copy of a workspace, with a profile to go with it (planned, not built)

Raised 2026-10-23. The want: take a project the user has been working on in a sandbox, make a copy somewhere else (to try something risky, to have a second agent on another idea, to hand a copy to a colleague's machine through git), and start on it with the profile it needs, without doing the folder, the profile and the sandbox by hand.

## What there is

- A sandbox belongs to a folder (`state.Instance.Target`), takes its name from it, and has a profile (`Profile`), found by `resolveProfileName` (`-p`, else the name, else one derived from the folder). `vibe` in a folder with no sandbox creates one (`doCreateOrAttach`, `createSandbox`) and asks which profile (guided / copy an existing one / blank: `createProfileIfMissing`, "copy an existing profile").
- `--migrate FROM TO` moves a folder and its sandbox (`src/vibe/migrate.go`): it refuses a `TO` that exists, shows what it will do, and asks. It is the nearest existing code, and its refusals and the way it says "what it will do first" are the pattern.
- Per-sandbox state that must not be shared: the name, ports (`allocatePublish` gives each sandbox its own), the companion port and token, the conversation, the agent's memories (`memoryStore`, per name), the git token (`GitToken`, per sandbox), the sandbox's kept model lists.
- Per-profile state: `config.yaml` and its composed files, `settings.yaml`, `agent-instructions.md`, and `user-preferences.yaml` (theme, git author answer, model, effort), plus `README.md`.

## Proposed

`vibe --clone SRC DEST [--profile NAME | --copy-profile [NAME]] [--with-changes] [--no-start]`

- **The folder.** If `SRC` is a git repository: `git clone` it to `DEST` (a local clone is cheap: hard links), with the same `origin` and the same branch checked out. That is the committed state, and the default, because it is what can be reproduced and what a colleague would get. **`--with-changes`** also brings the working tree (modified and untracked files that git doesn't ignore), copied over the clone. If `SRC` isn't a repository: refuse, saying so, unless `--copy-folder` (a plain copy, with its size shown and asked about first: a folder with a `node_modules` is not a thing to copy by surprise).
- **Never overwrites:** `DEST` must not exist, or must be empty (as `--migrate` demands), is made if its parent is there, and the plan is shown and confirmed first ("will make a clone of … at …, with profile …, sandbox named …").
- **The profile**, three ways, asked at the console if none was said (`-f` takes the first):
  1. *same profile* (the default: `DEST`'s sandbox uses `SRC`'s profile as it is, shared: a change to one is a change to both, and the page says "shared with <sandbox>" in the Admin tab),
  2. *copy as a seed* (`--copy-profile`, named from `DEST` unless a name is given): the profile folder is copied (composed files, `settings.yaml`, `agent-instructions.md`, `user-preferences.yaml`) to a new profile that then goes its own way; it keeps its generated-profile record (`features:` in `config.yaml`, the hash), so it still re-composes and auto-updates; the copy's `README.md` is the new one, not a copied one,
  3. *another profile* (`--profile NAME`), which must exist.
- **A new sandbox, not a copy of the old one:** its own name, ports, companion token, conversation, memories and git token are all fresh. The user is asked, never assumed, about two things people would want carried: **the agent's memories** (an offer to copy the old sandbox's memory folder, off by default, since memories can be about the other branch), and the **git push token** (not copied: a sandbox's token is its own; `--git-token` makes one).
- **Then it is a normal `vibe` in `DEST`** (creates the sandbox: the git author question, the theme question, the rest, as for any new sandbox), or stops short with `--no-start`, and says what to type (`vibe` in `DEST`).
- **If `DEST`'s sandbox name is taken** (a sandbox already exists with that name for another folder): refuse and say which, as `--migrate` does.

## Not decided

- `git worktree add` is the lighter alternative to a clone (one repository, two working folders); it gives the second agent the same history without a second copy, and a worktree can't have the branch the first one has checked out. A `--worktree` flag could do it with the same flow; leave it out of the first version, note it.
- Whether `--clone` should be able to clone *from* a remote URL (`vibe --clone https://… DEST`): it is the "start a project" flow, a different feature (`--remote` plans the machine side); not here.
- A running source sandbox is no obstacle (nothing of it is touched), but the page of the source should not be confused with the new one (different ports and tokens, so it isn't).

## Tests

Go, against real temporary repositories: the clone has the same HEAD, branch and `origin`; `--with-changes` brings modified and untracked-unignored files and not ignored ones; a non-repository is refused without `--copy-folder`; `DEST` existing and non-empty is refused and nothing is made; the profile choices (same: the same profile name in the new record; seed: a new profile folder with the files, a fresh README, and a hash that still checks as up to date; named: must exist); the new sandbox record has fresh ports, token and memory store; memories are copied only when said; the git token isn't. A refusal leaves nothing behind (no half-made folder, profile or record). The part that creates the sandbox uses the existing `createSandbox` tests' stand-ins.

## Order

After `docs/plan-extra-folders.md` (a clone of a workspace that has extra folders must say what it does about them: the extras are *references* to other folders, not copied; the clone keeps them, read-only unless said) and the `user-preferences.yaml` work (so a seed carries the user's choices).
