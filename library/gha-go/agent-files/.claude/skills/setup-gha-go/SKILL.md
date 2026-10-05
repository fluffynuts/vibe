---
name: setup-gha-go
description: Prepare a Go project for GitHub - adds a GitHub Actions workflow that tests on linux/macOS/windows, cross-compiles release zips and publishes a GitHub release, plus a Makefile, make.sh and make.ps1. Use when the user asks to "prepare this go project for github", set up CI/CD, GitHub Actions, or release builds for a Go CLI.
---

# setup-gha-go

Give a Go CLI project the same CI/build setup as the other projects: a GitHub
Actions workflow (test, package, release), build scripts (`Makefile`,
`make.sh`, `make.ps1`) that the workflow itself uses, and `--install` /
`--upgrade` flags in the program itself (`selfmanage.go`).

The files in `templates/` (next to this file, i.e.
`~/.claude/skills/setup-gha-go/templates/`) are the source of truth. **Copy them
and substitute placeholders; do not rewrite them from memory or "improve" them
per project** — identical output across projects is the point.

## Placeholders

| Placeholder | Meaning | Where to get it |
|---|---|---|
| `{{NAME}}` | binary / release name | the last element of the `go.mod` module path, unless a `cmd/<name>` dir or the repo name says otherwise |
| `{{PKG}}` | main package to build, e.g. `.` or `./cmd/foo` or `./src/foo` | find `package main` dirs (`go list -f '{{.Name}} {{.Dir}}' ./...`); if more than one, ask |
| `{{REPO_SLUG}}` | `owner/repo` on GitHub, for the release URLs in `--upgrade` | `git remote get-url origin` (strip `https://github.com/` / `git@github.com:` and `.git`); no GitHub remote -> ask |
| `{{DEFAULT_BRANCH}}` | branch that publishes releases | `git symbolic-ref --short HEAD` / `git remote show origin`; default `master` or `main` |

## Steps

1. **Check it's a Go module** (`go.mod` exists) and a git repo. If not, say so and stop.
2. **Work out the placeholders** above. Ask the user only if genuinely ambiguous
   (several `package main` dirs, no obvious name).
3. **Check what exists.** For each target file (`.github/workflows/build.yml`,
   `Makefile`, `make.sh`, `make.ps1`, `VERSION`): if it already exists and
   differs from what you would write, show the difference and ask before
   overwriting. Never silently clobber.
4. **Copy and substitute** every file from `templates/`, preserving paths
   (`templates/.github/workflows/build.yml` -> `.github/workflows/build.yml`).
   `chmod +x make.sh`. Makefile recipes need real tabs.
   `templates/selfmanage.go` and `selfmanage_test.go` are Go files: copy them
   into the **main package's directory** (`{{PKG}}`), not the repo root.
   Before copying, check `go.mod` says `go 1.21` or later (the file uses the
   `max` builtin); if it's older, say so and ask rather than bumping it.
   If the package already declares `Version`, `Build`, `releasesURL`,
   `assetName` or any other name `selfmanage.go` defines (build it to find
   out), reconcile by adapting the copy — and keep `make.sh`'s
   `-X main.Version`/`-X main.Build` ldflags pointing at the real variables.
5. **Wire up `--install` / `--upgrade`.** Make `if runSelfManage() { return }`
   the first statement of `main()`. It only looks at the first argument, so it
   needs no change to the program's own flag parsing. Add `selfManageUsage` to
   the program's help text if it has any; if it uses cobra/flag/etc. and
   prints its own usage, register the two flags there too so they're listed.
   `--install` copies the running binary to `~/.local/bin` and warns when that
   isn't on `PATH`; `--upgrade` downloads the latest release's zip for this
   platform (named as the workflow publishes it), checks `SHA256SUMS`, and
   replaces the running binary. It needs no GitHub login.
6. **VERSION**: if the project has none, create it containing `0.1`. Release
   versions are `VERSION` (major.minor, bumped by hand) + the workflow run
   number, e.g. `0.1.57`.
7. **.gitignore**: make sure it ignores `/{{NAME}}`, `/dist/` and `*.exe`
   (append what's missing; don't reorder the file).
8. **Verify what can be verified here:**
   - `./make.sh check` (vet + test) and `./make.sh dist` must pass; check the
     zip exists in `dist/` and contains the binary.
   - Lint the workflow if `actionlint` is available; otherwise at least parse
     it as YAML.
   - `go vet ./...` and `go test ./...` pass with the new files (the tests
     use a local stand-in for GitHub). Try `--install` with `HOME` pointed at
     a temp dir. `--upgrade` can't be tried for real until the repo has a
     release: before then it fails with "not a redirect to a release", which
     is expected; say that it is untested against GitHub.
   - `dist` needs `zip(1)` (the feature installs zip and unzip; if they are missing, say so rather than skipping
     silently, and check the build step it got to).
   - `make.ps1` can't be run here unless `pwsh` is installed; say so if you
     couldn't run it. Keep it in step with `make.sh` by reading, not guessing.
9. **Report** what was added, what was skipped (and why), and what can only be
   verified on GitHub: the release job needs the repo's Actions settings to
   allow workflows to create releases (`contents: write` is requested by that
   job only), and the first push is the real test. Do not commit or push
   unless asked.

## Things to know

- `--upgrade` depends on the workflow's release naming (`{{NAME}}-<os>-<arch>.zip`
  plus `SHA256SUMS`, with the binary inside the zip). If either is changed,
  change `assetName`/`openExe` in `selfmanage.go` with it.
- The release publishes under fixed asset names (`{{NAME}}-linux-amd64.zip`...)
  so `releases/latest/download/<name>` URLs are stable, plus a `SHA256SUMS`.
- Zips are made with `zip(1)` on Linux so executable bits survive; every target
  is cross-compiled with `CGO_ENABLED=0`. If the project needs cgo, say so:
  this setup doesn't fit it as is.
- To ship extra files in each zip (docs, config), add them to the `BUNDLE`
  array in `make.sh` — they must exist, `cp` fails otherwise.
- Action versions in the template (`actions/checkout@v7` etc.) are pinned to
  what is current as of writing; they go stale. If you have network access to
  GitHub, `gh api repos/actions/checkout/releases/latest --jq .tag_name` shows
  the newest; mention any that are behind rather than silently changing them.
- Adding another CI flavour later (`setup-gha-dotnet`...) is a separate skill
  with its own templates.
