# vibe — build, test and tidy the CLI.
#
# The binary resolves its bundle (defaults/, profiles/, config.yaml,
# settings.yaml) relative to its own location, so it is built into the repo
# root and must stay there. Put it on $PATH with a symlink, never a copy.

GO     ?= go
BINARY ?= vibe
PKG    := ./src/vibe

SOURCES := $(shell find . -name '*.go' -not -path './.git/*')

.DEFAULT_GOAL := build

.PHONY: build
build: $(BINARY)

# The build date goes into vibe --version; Go doesn't record it itself.
$(BINARY): $(SOURCES) go.mod go.sum VERSION
	$(GO) build -ldflags "-X vibe.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o $@ $(PKG)

.PHONY: test
test:
	$(GO) test ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: check
check: vet test

# A release zip for GOOS/GOARCH (default: this machine) in dist/ — see
# make.sh, which does the packaging for both.
.PHONY: dist
dist:
	./make.sh dist

.PHONY: clean
clean:
	rm -f $(BINARY)
	rm -rf dist
