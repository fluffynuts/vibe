# The sandbox's helpers in Go, so a sandbox needs no Node (planned, not built)

Raised 2026-11-04. The want: the clipboard page's server is a Node script, so every sandbox has to install Node (the Node line in `library/base/prerequisites/install-scripts/01-install-prerequisites`, which both the clipboard server and `vibe-review` rely on) now that sbx's claude image no longer ships it. Write the helpers in Go, which vibe already is, and make Node a feature a user picks (`library/node`, as `library/go` is) for projects that want it.

## What there is

- **Node users in the sandbox today** (from a grep of `defaults/`, `library/`, `profiles/`):
  - `clipboard-server.js` (348 lines), `clipboard-hook.js` (97), both on the stdlib alone (`http`, `fs`, `crypto`, `os`, `path`); `clipboard-page.html` (676) is static.
  - `review-cli.js` (392 lines), run by `vibe-review`, a **base** feature, so every sandbox has it. It refuses to run without node.
  - Not Node users: `vibe-band` runs inside Claude Code's own runtime; `vibe-nuget`, `start-rabbit` and the Elasticsearch script only contain the word "node" for other things. The settings merge steps (`internal/claudetheme`, `internal/kitspec/v3.go`) try node first, then python3, which the image ships, so they don't need it.
- **Kit files are text.** `kitspec.FileEntries` reads each agent file into a `content` string in the kit; a compiled binary can't be an agent file as it is.
- **vibe is built for six targets** (linux, macOS, windows × amd64, arm64) but the sandbox is always Linux, in the host's architecture (Apple Silicon hosts get linux/arm64, Windows hosts linux/amd64). The binary a user runs is rarely the one a sandbox needs.
- The Go tests today run the JS with a host `node` (`src/vibe/clipboardserver_test.go`), so building vibe's tests needs Node installed.

## Viable, with two catches

1. **Removing the install step only works if `review-cli.js` goes too.** Porting the clipboard alone leaves `vibe-review` needing Node in every sandbox. So the real scope is about 840 lines of JS (server, hook, review CLI), not 450.
2. **How a Linux binary gets into the sandbox is the hard part,** not the port. The port is mechanical (the JS has no dependencies; `net/http` covers it, and `crypto/rand` replaces `crypto`).

## Getting the binary there (decide first)

- **A. Embed.** Build a small static `vibe-agent` for linux/amd64 and linux/arm64 (`CGO_ENABLED=0`, `-ldflags "-s -w"`, a few MB each), `go:embed` both into vibe, and have vibe write the right one into the sandbox over `sbx exec -i ... tee` (as `sbxrun.WriteFile` does for the published URL; `tee` is binary-safe). Needs a pre-build step in `make.sh`, `make.ps1`, the Makefile and CI, and the tests build the helper first. Works offline and for dev builds. Costs roughly 6–10 MB per release and a place in the lifecycle to copy it (at create and rebuild; the file must be there before `start-clipboard` first runs, so probably an install step that waits, or `on-start` after the copy).
- **B. Download in the sandbox,** pinned and checksummed, as `library/pwsh` does. No size cost, but it needs a published release for every version (dev builds, `vibe-dev`, have none) and the GitHub hosts allowed.
- **C. Python instead of Go.** The image ships Python 3.14 and the kit already relies on `python3`; text files fit the kit as they are and there is no delivery problem. `http.server`/`ThreadingHTTPServer` is enough for this server. It removes Node without a new build step, but it isn't Go (tests would run `python3` on the host instead of `node`), and it depends on the image keeping Python.
- **D. The kit carries the binary** (base64 in a kit file, decoded by an install step). Not verified that sbx accepts a kit that large; it would also change the kit's fingerprint with every helper change. Listed to be ruled out.

Recommendation: **A**, as one binary `vibe-agent` with subcommands (`clipboard-server`, `clipboard-hook`, `review`), so there is one build and one copy. Reconsider **C** if embedding makes the release build too heavy.

## Steps

1. Decide the delivery (above) and check the unverified part: that a few MB over `sbx exec -i` + `tee` arrives intact on both architectures.
2. `cmd/vibe-agent` (or `internal/agent`): the server first, with the existing Go tests rewritten to run it directly (no host node). Keep the API and the page exactly as they are (`/api/claim`, `/api/clipboard`, the history under `~/.local/state/vibe/clipboard`, the ready-file-descriptor handshake `start-clipboard` uses).
3. The hook and `sbx-clipboard-fallback`'s counterpart; then `review`.
4. Delivery: the build step, the embed, the copy, and `start-clipboard`/`clipboard-hook`/`vibe-review` calling the binary instead of `node`. Missing-binary messages follow `missingtool_test.go`.
5. Drop Node from the `prerequisites` base feature (its install line, its `node` check and its tests); add `library/node` (installs `nodejs-24` with the fall-back to Debian's, as that step does now) as an optional feature, with its test and readme section.
6. Readme and a rebuild note: sandboxes composed before the change keep working (they still have the JS and Node), and `vibe -C` moves them over.

## Not decided

- Whether the helper also takes over what `start-clipboard` does in shell (linking the history folder, installing the fallback shim); it works and isn't Node, so leave it.
- Whether to keep the JS for a release as a fall-back; probably not, since `vibe -C` is how a sandbox picks up anything new.
