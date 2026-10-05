# {{NAME}} — build, test and tidy the CLI. Mirrors make.sh / make.ps1.

GO     ?= go
BINARY ?= {{NAME}}
PKG    := {{PKG}}

SOURCES := $(shell find . -name '*.go' -not -path './.git/*')

.DEFAULT_GOAL := build

.PHONY: build
build: $(BINARY)

$(BINARY): $(SOURCES) go.mod $(wildcard go.sum) VERSION
	$(GO) build -o $@ $(PKG)

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
