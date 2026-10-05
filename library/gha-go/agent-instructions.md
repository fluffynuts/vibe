## GitHub Actions for Go

To prepare a Go project for GitHub (CI that tests on linux/macOS/windows,
cross-compiled release zips, a published GitHub release, plus `Makefile`,
`make.sh`, `make.ps1`, and `--install`/`--upgrade` flags), use the `/setup-gha-go` skill. It copies vetted
templates and substitutes the project's name and main package; don't hand-write
those files instead. Don't commit or push unless asked.
