---
name: setup-gha-go
description: Prepare a Go project for GitHub - adds a GitHub Actions workflow that tests on linux/macOS/windows, cross-compiles release zips and publishes a GitHub release, plus a Makefile, make.sh and make.ps1. Use when the user asks to "prepare this go project for github", set up CI/CD, GitHub Actions, or release builds for a Go CLI.
---

# setup-gha-go

Give a Go CLI project the same CI/build setup as the other projects: a GitHub
Actions workflow (test, package, release) and build scripts (`Makefile`,
`make.sh`, `make.ps1`) that the workflow itself uses.

The files in `templates/` (next to this file, i.e.
`~/.claude/skills/setup-gha-go/templates/`) are the source of truth. **Copy them
and substitute placeholders; do not rewrite them from memory or "improve" them
per project** — identical output across projects is the point.

## Placeholders

| Placeholder | Meaning | Where to get it |
|---|---|---|
| `{{NAME}}` | binary / release name | the last element of the `go.mod` module path, unless a `cmd/<name>` dir or the repo name says otherwise |
| `{{PKG}}` | main package to build, e.g. `.` or `./cmd/foo` or `./src/foo` | find `package main` dirs (`go list -f '{{.Name}} {{.Dir}}' ./...`); if more than one, ask |
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
5. **VERSION**: if the project has none, create it containing `0.1`. Release
   versions are `VERSION` (major.minor, bumped by hand) + the workflow run
   number, e.g. `0.1.57`.
6. **.gitignore**: make sure it ignores `/{{NAME}}`, `/dist/` and `*.exe`
   (append what's missing; don't reorder the file).
7. **Verify what can be verified here:**
   - `./make.sh check` (vet + test) and `./make.sh dist` must pass; check the
     zip exists in `dist/` and contains the binary.
   - Lint the workflow if `actionlint` is available; otherwise at least parse
     it as YAML.
   - `dist` needs `zip(1)` (the feature installs zip and unzip; if they are missing, say so rather than skipping
     silently, and check the build step it got to).
   - `make.ps1` can't be run here unless `pwsh` is installed; say so if you
     couldn't run it. Keep it in step with `make.sh` by reading, not guessing.
8. **Report** what was added, what was skipped (and why), and what can only be
   verified on GitHub: the release job needs the repo's Actions settings to
   allow workflows to create releases (`contents: write` is requested by that
   job only), and the first push is the real test. Do not commit or push
   unless asked.

## Things to know

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
