# vibe — build, test and tidy the CLI.
#
# The binary resolves its bundle (defaults/, profiles/, config.yaml,
# settings.yaml) relative to its own location, so it is built into the repo
# root and must stay there. Put it on $PATH with a symlink, never a copy.

GO     ?= go
BINARY ?= vibe
PKG    := ./src/vibe

SOURCES := $(shell find . -name '*.go' -not -path './.git/*')
# Files pulled in by go:embed; editing them must trigger a rebuild too.
EMBEDDED := $(shell find internal/companion/ui -type f)

.DEFAULT_GOAL := build

.PHONY: build
build: $(BINARY) ## build the vibe binary

# The build date goes into vibe --version; Go doesn't record it itself.
$(BINARY): $(SOURCES) $(EMBEDDED) go.mod go.sum VERSION
	$(GO) build -ldflags "-X vibe.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o $@ $(PKG)

.PHONY: test
test: ## run tests
	$(GO) test ./...

.PHONY: vet
vet: ## run go vet
	$(GO) vet ./...

.PHONY: check
check: vet test ## run vet and tests

# A release zip for GOOS/GOARCH (default: this machine) in dist/ — see
# make.sh, which does the packaging for both.
.PHONY: dist
dist: ## build a release zip file in dist/
	./make.sh dist

.PHONY: clean
clean: ## remove vibe binary and dist/ folder
	rm -f $(BINARY)
	rm -rf dist

.PHONY: help
help: ## this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "} {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
