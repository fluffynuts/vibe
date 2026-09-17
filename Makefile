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

$(BINARY): $(SOURCES) go.mod go.sum
	$(GO) build -o $@ $(PKG)

.PHONY: test
test:
	$(GO) test ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: check
check: vet test

.PHONY: clean
clean:
	rm -f $(BINARY)
	rm -rf dist
