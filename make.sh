#!/usr/bin/env bash
# vibe — build, test and tidy the CLI, for machines without make.
# Mirrors the Makefile: ./make.sh [build|test|vet|check|clean]... (default: build)
#
# The binary resolves its bundle (defaults/, profiles/, config.yaml,
# settings.yaml) relative to its own location, so it is built into the repo
# root and must stay there. Put it on $PATH with a symlink, never a copy.
#
# GO and BINARY can be overridden from the environment, as with make.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

GO="${GO:-go}"
BINARY="${BINARY:-vibe}"
PKG="./src/vibe"

# build only when a source is newer than the binary, as make would.
target_build() {
  if [[ -f "$BINARY" ]] && [[ -z "$(find . \( -name '*.go' -o -name go.mod -o -name go.sum \) \
      -not -path './.git/*' -newer "$BINARY" -print -quit)" ]]; then
    echo "'$BINARY' is up to date."
    return
  fi
  echo "$GO build -o $BINARY $PKG"
  "$GO" build -o "$BINARY" "$PKG"
}

target_test() {
  echo "$GO test ./..."
  "$GO" test ./...
}

target_vet() {
  echo "$GO vet ./..."
  "$GO" vet ./...
}

target_check() {
  target_vet
  target_test
}

target_clean() {
  echo "rm -f $BINARY"
  rm -f "$BINARY"
  echo "rm -rf dist"
  rm -rf dist
}

[[ $# -eq 0 ]] && set -- build

for target in "$@"; do
  case "$target" in
    build | test | vet | check | clean) "target_$target" ;;
    *)
      echo "make.sh: no such target '$target' (build, test, vet, check, clean)" >&2
      exit 2
      ;;
  esac
done
