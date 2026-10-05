#!/usr/bin/env bash
# {{NAME}} — build, test and tidy the CLI, for machines without make.
# Mirrors the Makefile: ./make.sh [build|test|vet|check|clean|dist]... (default: build)
#
# GO and BINARY can be overridden from the environment, as with make.
#
# dist packages a release zip in dist/: the binary plus anything in BUNDLE,
# under one top-level folder. It builds for GOOS/GOARCH when those are set
# (cross-compiling, with cgo off), else for this machine. BUILD, when set, is
# the CI build number: it becomes the version's third part, in the name and
# in the binary (main.Build). The zip is made with zip(1) so the binary keeps
# its executable bit. The last line printed is the zip's path.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

GO="${GO:-go}"
BINARY="${BINARY:-{{NAME}}}"
PKG="{{PKG}}"
NAME="{{NAME}}"

# Extra files shipped beside the binary in each zip; they must exist.
BUNDLE=(VERSION)

# build only when a source is newer than the binary, as make would.
target_build() {
  if [[ -f "$BINARY" ]] && [[ -z "$(find . \( -name '*.go' -o -name go.mod -o -name go.sum -o -name VERSION \) \
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

target_dist() {
  local goos goarch os_name version name exe stage
  goos="${GOOS:-$("$GO" env GOOS)}"
  goarch="${GOARCH:-$("$GO" env GOARCH)}"
  os_name="$goos"
  [[ "$goos" == darwin ]] && os_name=macos
  version="$(tr -d '[:space:]' <VERSION)${BUILD:+.$BUILD}"
  name="$NAME-$version-$os_name-$goarch"
  exe="$NAME"
  [[ "$goos" == windows ]] && exe="$NAME.exe"
  stage="dist/$name"

  rm -rf "$stage" "$stage.zip"
  mkdir -p "$stage"
  echo "GOOS=$goos GOARCH=$goarch $GO build -o $stage/$exe $PKG" >&2
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" "$GO" build -trimpath \
    -ldflags "-X main.Version=$(tr -d '[:space:]' <VERSION) -X main.Build=${BUILD:-}" \
    -o "$stage/$exe" "$PKG" >&2
  cp -R "${BUNDLE[@]}" "$stage/"
  (cd dist && zip -qrX "$name.zip" "$name") >&2
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
