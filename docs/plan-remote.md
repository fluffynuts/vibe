# `vibe --remote`: running vibe on another machine as if it were this one (planned, not built)

Raised 2026-10-23. The want: `vibe --remote user@host <path/to/repo>`, and the whole thing is set up: vibe runs on `host` for that folder, and the user gets the experience they would have here: a terminal session, or the companion page in their local browser. With `--install` and `--upgrade` too, so someone with a build box can look after it entirely from their own machine.

## What there is to build on

- **ssh forwarding is already worked out.** `vibe --info` prints the `ssh -N -L p:127.0.0.1:p user@host` command that reaches a headless vibe's page and published ports from another machine (`sshForwarding`, `thisMachine`, `src/vibe/info.go`). It forwards to `127.0.0.1`, not `localhost` (sbx publishes there alone).
- **A headless vibe tells its starter where its page is.** `vibe --headless` starts a background copy (`detachHeadless`, `src/vibe/detach.go`), which sends a `vibe-headless-ready <url>` line (`readyPrefix`) when its page is up; the starter prints it, with the log's path and how to stop it, and if one is already running for the folder it says so and points at its page. The URL carries the page's token in its fragment.
- **vibe installs and upgrades itself:** `install.sh` (POSIX sh: checks the release's `SHA256SUMS`, runs `vibe --install`), `vibe --install`, `vibe --upgrade` (`internal/selfupdate`, `AssetName(goos, goarch)`).
- The commands to project: `--stop`, `--stop-all`, `--list`, `--info`, `--restart`, `--re-init`, `-r`, `-C`, `--restore`, `--cleanup`, `--git-token`, `--install-sbx`, `--update-sandboxes`.

## Proposed

`vibe --remote [user@]host [other vibe flags] [path]`: run this vibe command *there*. The remote's vibe does everything; this one is the way in.

- **A terminal session** (`vibe --remote host ~/code/x`): `ssh -t host vibe <args>`, the terminal passed through, so the agent's screen and the questions behave as they would here. Quoting is the main risk (below).
- **A headless one** (`--headless`): runs `vibe --headless <path>` over ssh, reads the `vibe-headless-ready` line, picks a free *local* port, starts `ssh -N -L local:127.0.0.1:remote host`, and opens the *local* browser at `http://localhost:<local>/#<token>`. If the remote vibe says it is already running for the folder, it just does the tunnel: this is also how to reconnect. The tunnel is held by this vibe in the foreground with a status line (Ctrl-C closes it and leaves the remote vibe running); a `--detach` could hand it to a background process, as `--headless` does, later.
- **Other commands** (`--stop`, `--list`, `--info`, `--restart`, rebuilds, `--git-token`…) are run remotely as they are, output streamed back. `--info` rewrites its forwarding hint for the user's end (`ssh` target as given), since "this machine" there is the remote.
- **`--remote host --install` and `--upgrade`:** if the remote can reach GitHub, run its own `install.sh` (the checks are the script's own). If it can't (a build box behind a firewall: common), this vibe fetches the release asset for the *remote's* platform (found with `ssh host 'uname -sm'`, `selfupdate.AssetName`), checks it against `SHA256SUMS`, and streams it to the remote to install; the remote runs `vibe --install` from there. `--install-sbx` likewise.
- **Questions on the remote:** a terminal session's questions come through the terminal. A headless vibe's questions are on its page (the git author is; the profile re-compose before a restart is planned to be, see `docs/plan-quality-of-life.md`), which is reached through the tunnel like the rest.

## What must not go wrong

- **Quoting.** Everything after `--remote host` is run in the remote user's shell: a path or a flag with a space, quote, `;` or `$()` in it must arrive as one argument and nothing else. Build the remote command by quoting every argument (single quotes, `'` as `'\''`: `shellQuote` exists for the sandbox side, `src/vibe/conversation.go`), never by joining strings; test with hostile paths. Where the shell can be avoided (`ssh host -- vibe arg arg`, which still goes through the remote shell: so quote), say so in a comment.
- **ssh stays ssh.** The system `ssh`, `known_hosts` and keys and agent are the user's. vibe never sets `StrictHostKeyChecking=no`, never stores a password or passphrase, never handles a key. A first-time host's fingerprint question is ssh's own, at the terminal (so `--remote` needs a terminal for the first connection, and says so when it hasn't one).
- **Tokens.** The page's token is in the `ready` line and the URL fragment: shown to the user, never logged by this vibe, never put in a process's argument list (visible in `ps`) or in a file; the local browser is opened with it as vibe does now.
- **The tunnel binds to 127.0.0.1 only** (the `-L` default is localhost; say `127.0.0.1:` explicitly so a `GatewayPorts` setting can't widen it).
- **Windows as the local machine:** OpenSSH's `ssh.exe` is on current Windows; `ControlMaster` isn't supported there, so no multiplexing: a connection per command is acceptable. Detect a missing `ssh` and say how to get one. The remote may be Windows too: a later question, not the first version.
- **Version skew.** The local vibe and the remote one can differ: the page and the sandbox logic are the remote's, so the user gets the remote's behaviour. Say both versions when they differ (`vibe --remote` asks `vibe --version` first), and offer `--upgrade` if the remote is older than this one.
- **A dropped connection** ends a terminal session as an ssh drop does (the remote sbx session may carry on: say how to reattach); a headless vibe is unaffected, and the tunnel is the part to restart (`vibe --remote host <path>` again).

## Decisions to make

1. **ControlMaster multiplexing** (one authentication for the several ssh calls a command makes: `uname`, `--version`, the real command, the tunnel): with a socket under `~/.vibe/` (mode 0700; short path: sockets have a ~100-char limit), `ControlPersist` for a few minutes. It makes passphrase prompts once; it is Unix-only. Start without it, add when the repeated prompts annoy, or ask for an `ssh-agent`.
2. **Where the remote vibe lives:** assume it is on the PATH of a non-interactive ssh shell? Often not (`~/.local/bin` is added by a profile that non-interactive ssh doesn't read): find it by trying `vibe`, then the install location `--install` uses, and say what was tried.
3. **The flag's shape:** `--remote HOST` before the rest, or `vibe HOST:PATH` (scp-like)? The user's example is the flag; keep it.
4. Whether to remember hosts (`~/.vibe/remotes.yaml`: a name, a host, a default path) so `vibe --remote build ~/x` works: not in the first version; the user's own `~/.ssh/config` host aliases already give that for free (`--remote build`).

## Tests

Go, with a stand-in `ssh` on `PATH` (a script that records its arguments and runs the rest locally, as `fakeSandbox` does for `sbx` in `internal/claudetheme`): the command built for a path with spaces, quotes, `;`, `$(…)` and a leading `-` (one argument, quoted, after `--`); the ready line parsed, the local port free, the tunnel arguments (`-N -L 127.0.0.1:L:127.0.0.1:R`); an "already running" answer goes straight to the tunnel; `--install` picks the asset from `uname -sm` output (every OS/arch `AssetName` knows, and an unknown one refused with a message); no password, key or `StrictHostKeyChecking` ever appears in the arguments; the token isn't in any argument or in what is logged; version skew is said. One real run against `localhost` over ssh, by hand, as the proof (and kept as the manual check in this document).

## Order

After `docs/plan-quality-of-life.md`'s restart (its page questions are what a remote headless session relies on) and `docs/plan-clone.md` is independent of it. Start with a terminal session and `--list`/`--info` (the cheap, safe part), then headless with the tunnel, then `--install`/`--upgrade` last: it is the part that writes to another machine.
