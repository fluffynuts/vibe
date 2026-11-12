# GitHub tokens kept in the OS credential store, not in plain files (planned, not built)

Independent of `docs/plan-batch-git-token.md`: that plan only adds a reader of what is stored (copy a token from another sandbox), and reads
through whatever stores them, so it works unchanged either way. Do whichever first.

## What is wanted (raised 2026-11-12)

Today every token is written as plain text: `gittoken.Save` puts a sandbox's at `~/.vibe/git/sandboxes/<name>.token` and the default token
(`--setup`) at `~/.vibe/git/default.token`, readable by the user alone (0600) and not encrypted. Wanted: encrypted at rest, **without asking the user for
anything**: no passphrase, no extra step.

## Decided

- **The OS credential store, no passphrase**: the macOS Keychain (login keychain), Windows Credential Manager, the Linux Secret Service (GNOME
  Keyring, KWallet). The OS holds the key and unlocks it at login, so a rebuild or a headless start never prompts.
- **Where there is no store** (servers, WSL, containers often have none on Linux), **fall back to today's 0600 file**. Accepted by the user.
  The warning is given **when a token is saved to the file** (that is rare, and is when it matters), saying where it went and why, not at every
  start.
- **A new dependency** (the user's choice over shelling out to `security`, PowerShell and `secret-tool`). The candidate is
  `github.com/zalando/go-keyring`. What is believed of it, and **to be checked before adopting it**: macOS through the `security` tool, Windows
  through Credential Manager, Linux through the Secret Service over D-Bus; MIT licence; no cgo. Check, from its source and by building: that it is
  maintained, its licence, that it builds for all six release targets (`.github/workflows/build.yml`: linux, darwin, windows, each amd64 and
  arm64) without cgo, and what it does when there is no store (an error, not a hang). If it fails any, the choice goes back to the user.

## What it protects against, and what not

Against the token leaking from the file: a backup, a home-directory sync, a dotfiles repository, a zip sent for support, a disk that is not
encrypted. **Not** against malware running as the same user, which can read anything the user can, the credential store included: only a
passphrase would, and one is exactly the prompt on every rebuild this avoids. Already limiting the damage of a leak: the tokens are fine-grained
(chosen repositories only) and expire (vibe offers 30 days).

## Every place that touches a token file (the real list)

Not one seam, as first thought: two paths bypass `internal/gittoken`.

- Through `gittoken.Load`, `Save` and `Remove` (`src/vibe/gittoken.go`): the default token read (`askGitAccess`, `setUpDefaultGitToken`),
  written (`shareDefaultToken`) and removed (`--setup`); a sandbox's saved (`giveGitAccess`), removed (`takeGitAccess`) and read back for a
  rebuild (`recordedGitAccess`).
- **`state.Remove`** (`internal/state/state.go`) deletes a sandbox's token file **by path** when the sandbox's record goes. With a credential
  store this would leave the entry behind for ever: a dead credential nobody can see or remove from vibe.
- **`state.Rename`** moves it **by path** when a sandbox is renamed. With a store the token would stay under the old name, and the renamed
  sandbox would lose it on its next rebuild.
- The default token's expiry (`~/.vibe/git/default.token.expires`, `gittoken.SaveExpiry`/`LoadExpiry`) is not secret and **stays a file**.

So the store is keyed by a **name**, not a path (`sandbox/<name>`, `default`), with `Get`, `Put`, `Delete` and a `Move` (put under the new name,
then delete the old), in a package both `gittoken` and `state` can import (whether that needs a new small package, to avoid an import cycle, is
found out when starting). `state.Remove` and `state.Rename` use it instead of the path; `GitTokenPath` is left for the file fallback and the
migration alone. Implementations: the credential store (the library), the file (today's behaviour, also the fallback), and an in-memory one for
tests.

## How it works

- **Which one**: the credential store is tried; if it can't be used (no service, locked and not unlockable, any error) the file is, for that
  call, with the warning above on a save. A token is **never lost to a store failing**: it is the file or the store, and a failure to put one
  falls to the other. (The sandbox's own copy, in sbx's secret store, is separate and not touched.)
- **Reading**: the store first, then the file. A token in both (an interrupted migration) is the store's.
- **Migration, with nothing lost**: reading a token found only in the file, with a store available: put it in the store, **read it back and
  compare**, and only then delete the file. If any step fails, the file stays and nothing is lost. Done on use, not as a sweep, so a user with
  nothing to read is not touched.
- **Removing**: from both (it may be in either, or both).
- **Honest about erasure**: deleting the old file is not a secure erase (an SSD or a backup may still hold the plain text). The user who needs
  certainty regenerates the token, which the tokens' expiry makes routine. Said once, when a token is migrated.
- **Which sandboxes have a copy** (the copy-from-sandbox answer in the batch plan) needs "is there one", without fetching the secret where the
  library allows it; where it doesn't, a fetch that is dropped at once. Not yet known whether a fetch prompts on macOS: found out by trying.

## Can't be tried here, and how it is covered

There is no macOS, Windows or a Secret Service in the sandbox, and CI runners' credential stores behave their own way (a locked keychain, no
D-Bus). So the tests use the in-memory store and a fake that fails, and the real stores are tried by the user, on each platform: give a token;
restart vibe; rebuild the sandbox (it reads the token back); rename the sandbox (still there under the new name); delete it (nothing left in the
store: `security find-generic-password -s vibe` on macOS, `cmdkey /list` on Windows, `secret-tool search service vibe` on Linux). A real-store
test that does this, **skipped unless asked for by an environment variable**, so no CI runner is made to depend on one, is worth having for the
user's own runs.

## Tests

Unit, on the interface with the fakes:
- a saved token is read back, from the store and from the file fallback; one only in the file is moved into the store when read, read back
  and compared first, and the file is gone after; if the put fails, or the read-back differs, the file is **still there** and the token still read;
- a store that errors on every call: the token goes to the file with the warning, and is read from it; no call panics or hangs;
- `Remove` clears both the store and the file; `state.Remove` and `state.Rename` use the store, so after a rename the token is under the new
  name only, and after a delete nothing is left under the old;
- the warning is given on a save to the file, and not on a read, and never contains the token;
- the file fallback is still mode 0600 (as `fscopy.WriteWhole` makes it), and no token is ever logged or put in an error.

## Not decided

Whether `vibe --info` (and the line `--git-token` prints about a sandbox's token) should say where each token is kept, "in the keychain" or "in
a file": cheap and tells the user which sandboxes are still on the file. Whether an opt-in passphrase mode is wanted by anyone who needs
protection from same-user malware: not planned, and not offered until someone asks.
