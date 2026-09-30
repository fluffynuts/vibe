#!/usr/bin/env bash
# vibe — build, test and tidy the CLI, for machines without make.
# Mirrors the Makefile: ./make.sh [build|test|vet|check|clean|dist]... (default: build)
#
# The binary resolves its bundle (defaults/, profiles/, config.yaml,
# settings.yaml) relative to its own location, so it is built into the repo
# root and must stay there. Put it on $PATH with a symlink, never a copy.
#
# GO and BINARY can be overridden from the environment, as with make.
#
# dist packages a release zip in dist/: the binary plus the bundle it needs
# beside it, under one top-level folder. It builds for GOOS/GOARCH when those
# are set (cross-compiling, with cgo off), else for this machine. BUILD, when
# set, is the CI build number: it becomes the version's third part, in the
# name and in the binary (vibe --version). DIST_LABEL, when set, goes into
# the name too: vibe-<version>[.<build>][-<label>]-<os>-<arch>.
# The zip is made with zip(1) so the binary keeps its executable bit.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

GO="${GO:-go}"
BINARY="${BINARY:-vibe}"
PKG="./src/vibe"

# When this build ran, for vibe --version: Go doesn't record it itself.
build_date() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# build only when a source is newer than the binary, as make would.
target_build() {
  if [[ -f "$BINARY" ]] && [[ -z "$(find . \( -name '*.go' -o -name go.mod -o -name go.sum -o -name VERSION \) \
      -not -path './.git/*' -newer "$BINARY" -print -quit)" ]]; then
    echo "'$BINARY' is up to date."
    return
  fi
  echo "$GO build -o $BINARY $PKG"
  "$GO" build -ldflags "-X vibe.BuildDate=$(build_date)" -o "$BINARY" "$PKG"
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

# The files the binary reads from beside itself (see pathresolve.BundleRoot),
# plus what a person unpacking the zip wants to read.
BUNDLE=(config.yaml settings.yaml defaults profiles library readme.md VERSION)

target_dist() {
  local goos goarch os_name version name exe stage
  goos="${GOOS:-$("$GO" env GOOS)}"
  goarch="${GOARCH:-$("$GO" env GOARCH)}"
  os_name="$goos"
  [[ "$goos" == darwin ]] && os_name=macos
  version="$(tr -d '[:space:]' <VERSION)${BUILD:+.$BUILD}"
  name="vibe-$version${DIST_LABEL:+-$DIST_LABEL}-$os_name-$goarch"
  exe=vibe
  [[ "$goos" == windows ]] && exe=vibe.exe
  stage="dist/$name"

  rm -rf "$stage" "$stage.zip"
  mkdir -p "$stage"
  echo "GOOS=$goos GOARCH=$goarch $GO build -o $stage/$exe $PKG"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" "$GO" build -trimpath \
    -ldflags "-X vibe.Build=${BUILD:-} -X vibe.BuildDate=$(build_date)" -o "$stage/$exe" "$PKG"
  cp -R "${BUNDLE[@]}" "$stage/"
  (cd dist && zip -qrX "$name.zip" "$name")
  rm -rf "$stage"
  echo "dist/$name.zip"
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
    build | test | vet | check | clean | dist) "target_$target" ;;
    *)
      echo "make.sh: no such target '$target' (build, test, vet, check, clean, dist)" >&2
      exit 2
      ;;
  esac
done
