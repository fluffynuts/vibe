## Go

Go is installed: `go` and `gofmt` are on the PATH. A go.mod that asks for a newer Go than the installed one
makes `go` download that toolchain on first use, which takes a moment.
If `go` isn't there, its install failed: the reason is in `/tmp/sbx-setup.log`.
