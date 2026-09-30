#!/bin/sh
# Install or upgrade vibe on Linux or macOS, from its latest GitHub release:
#
#   curl -fsSL https://raw.githubusercontent.com/fluffynuts/vibe/master/install.sh | sh
#
# Downloads the release zip for this machine into a temporary folder, checks
# it against the release's SHA256SUMS, unpacks it into another, and runs
# vibe --install from there. Options after "sh -s --" are passed on to it:
#
#   curl -fsSL .../install.sh | sh -s -- --update-strategy merge,keep
#
# POSIX sh on purpose: "| sh" is dash on Debian and Ubuntu, not bash.
# Once vibe is installed, vibe --upgrade does the same job.

set -eu

RELEASES="${VIBE_RELEASES:-https://github.com/fluffynuts/vibe/releases}"

say() { printf 'vibe-install: %s\n' "$*" >&2; }
die() {
  say "$*"
  exit 1
}

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=macos ;;
  *) die "this script is for Linux and macOS — on Windows, use install.ps1 (see the readme)" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "no vibe release for a $(uname -m) machine (only x86-64 and ARM64)" ;;
esac
asset="vibe-$os-$arch.zip"

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$2" "$1"
  else
    die "needs curl or wget to download the release"
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d' ' -f1
  else
    die "needs sha256sum or shasum to check the download"
  fi
}

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t vibe-install)"
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT TERM

say "downloading $asset"
fetch "$RELEASES/latest/download/$asset" "$tmp/$asset" || die "couldn't download $RELEASES/latest/download/$asset"
fetch "$RELEASES/latest/download/SHA256SUMS" "$tmp/SHA256SUMS" || die "couldn't download the release's SHA256SUMS"

want="$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1 }' "$tmp/SHA256SUMS")"
[ -n "$want" ] || die "SHA256SUMS has no entry for $asset"
got="$(sha256 "$tmp/$asset")"
# (A release published between the two downloads above would make them
# disagree too: running this again settles that.)
[ "$got" = "$want" ] || die "$asset doesn't match its checksum (got $got, want $want) — not installing it"
say "checked $asset against SHA256SUMS"

mkdir "$tmp/unpacked"
if command -v unzip >/dev/null 2>&1; then
  unzip -q "$tmp/$asset" -d "$tmp/unpacked"
elif command -v python3 >/dev/null 2>&1; then
  python3 -m zipfile -e "$tmp/$asset" "$tmp/unpacked"
else
  die "needs unzip (or python3) to unpack the release — install unzip and run this again"
fi
bundle="$(find "$tmp/unpacked" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
[ -n "$bundle" ] && [ -f "$bundle/vibe" ] || die "the release zip doesn't hold a vibe binary"
chmod +x "$bundle/vibe" # python's zipfile doesn't keep the executable bit

say "running vibe --install from $(basename "$bundle")"
# vibe asks its questions on the terminal itself, so this works even though
# the script arrived on stdin.
"$bundle/vibe" --install "$@"
