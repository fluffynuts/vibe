#!/usr/bin/env bash
# Says which apt packages the library's install scripts ask for that the image's apt doesn't have: the mistake the
# mysql feature made (mysql-server, which Debian doesn't package), found when a build is run rather than before it.
#
# Run it inside a sandbox, whose apt is the image's (Debian and Docker Hardened Images): the CI machines' apt is
# Ubuntu's, which has other names. ./tools/check-apt-packages.sh [folder-to-read]  (default: the library, defaults and
# profiles of this checkout). It lists every package it checked, so a name it never saw is plain to see, and exits 1
# if any has no candidate.
#
# It reads the names the scripts spell out: those after "install -y" and "apt_install", and the ones the scripts
# collect in a variable first (need="$need name", tools="$tools name", node="name"). A package named some other way,
# built up in a variable from something else, isn't seen. A name that is only a fall-back for another (libaio1 after
# libaio1t64) may be reported missing: that is expected, and the image not having it is why it is the second choice.
set -u

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${1:-}"
if [[ -n "$target" ]]; then
  folders=("$target")
else
  folders=("$root/library" "$root/defaults" "$root/profiles")
fi

names_in() {
  # One "name<TAB>file" per name found in file. Comment lines are left out.
  local file="$1" lines
  lines="$(grep -v -E '^[[:space:]]*#' "$file" 2>/dev/null)"
  {
    # install -y a b c
    grep -oP 'install -y \K[^|;&<>\\]*' <<<"$lines" | tr -d '"' | tr -s ' ' '\n' | grep -E '^[a-z0-9][a-z0-9.+-]*$'
    # apt_install name (not apt_install install ..., which the line above has, nor apt_install update)
    grep -oP 'apt_install \K(?!install\b|update\b)[a-z0-9][a-z0-9.+-]*' <<<"$lines"
    # need="$need name", tools="$tools name", node="name"
    grep -oP '(need|tools)="\$(need|tools) \K[a-z0-9][a-z0-9.+-]*' <<<"$lines"
    grep -oP '^node="\K[a-z0-9][a-z0-9.+-]*' <<<"$lines"
  } | sort -u | sed "s#\$#\t$file#"
}

found="$(mktemp)"
trap 'rm -f "$found"' EXIT
while IFS= read -r file; do
  names_in "$file"
done < <(find "${folders[@]}" -type f \( -path '*/install-scripts/*' -o -path '*/.local/bin/*' \) 2>/dev/null | sort) >"$found"

if [[ ! -s "$found" ]]; then
  echo "no package names found in ${folders[*]}" >&2
  exit 1
fi

missing=0
while IFS= read -r name; do
  files="$(awk -F'\t' -v n="$name" '$1 == n { sub("^" ENVIRON["PWD"] "/", "", $2); print $2 }' "$found" | sort -u | tr '\n' ' ')"
  candidate="$(apt-cache policy "$name" 2>/dev/null | awk '/Candidate:/ { print $2 }')"
  if [[ -z "$candidate" || "$candidate" == "(none)" ]]; then
    echo "MISSING  $name   (asked for in: $files)"
    missing=1
  else
    echo "ok       $name $candidate"
  fi
done < <(cut -f1 "$found" | sort -u)

exit $missing
