#!/usr/bin/env bash

# Copyright 2026 Docker Packaging authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Install docker-sbx from a static tarball.
#
# Usage:
#   ./install.sh                     # installs to ~/.docker/sbx
#   PREFIX=/usr/local ./install.sh   # installs to /usr/local
#
# Layout:
#   <prefix>/bin/sbx
#   <prefix>/libexec/containerd-shim-nerdbox-v1
#   <prefix>/libexec/containerd-shim-nerdbox-gpu-v1  (amd64 only; non-suid — run "sudo <shim> install" once to enable GPU passthrough)
#   <prefix>/libexec/mkfs.erofs
#   <prefix>/libexec/mkfs.ext4
#   <prefix>/libexec/nerdbox-kernel-*
#   <prefix>/libexec/nerdbox-rootfs-*.erofs
#   <prefix>/libexec/lib/libsailor.so

set -eu

PREFIX="${PREFIX:-${HOME}/.docker/sbx}"
SRCDIR="$(cd "$(dirname "$0")" && pwd)"

bin_dir="${PREFIX}/bin"
libexec_dir="${PREFIX}/libexec"

echo "Installing docker-sbx to ${PREFIX} ..."

mkdir -p "${bin_dir}" "${libexec_dir}/lib"

install -m 755 "${SRCDIR}/sbx" "${bin_dir}/sbx"
rm -f "${bin_dir}/sbx-version.json"

for f in containerd-shim-nerdbox-v1 mkfs.erofs mkfs.ext4; do
  install -m 755 "${SRCDIR}/${f}" "${libexec_dir}/${f}"
done

# GPU trampoline shim: present only in amd64 packages. Install non-suid;
# the user runs "sudo <shim> install" once to enable GPU passthrough.
if [ -f "${SRCDIR}/containerd-shim-nerdbox-gpu-v1" ]; then
  install -m 755 "${SRCDIR}/containerd-shim-nerdbox-gpu-v1" "${libexec_dir}/containerd-shim-nerdbox-gpu-v1"
fi

for f in "${SRCDIR}"/nerdbox-kernel-* "${SRCDIR}"/nerdbox-rootfs-*.erofs; do
  [ -f "$f" ] && install -m 644 "$f" "${libexec_dir}/$(basename "$f")"
done

install -m 755 "${SRCDIR}/libsailor.so" "${libexec_dir}/lib/libsailor.so"

# Install AppArmor profile if the file is bundled and AppArmor is active
if [ -f "${SRCDIR}/apparmor-profile" ] && [ -d /sys/kernel/security/apparmor ]; then
  apparmor_dir="/etc/apparmor.d"
  if [ -d "${apparmor_dir}" ] && command -v apparmor_parser >/dev/null 2>&1; then
    if [ -w "${apparmor_dir}" ]; then
      echo "Installing AppArmor profile for shim..."
      install -m 644 "${SRCDIR}/apparmor-profile" "${apparmor_dir}/docker-sbx-nerdbox-shim"
      apparmor_parser -r -W "${apparmor_dir}/docker-sbx-nerdbox-shim" || true
    else
      echo "Skipping AppArmor profile: ${apparmor_dir} needs root." >&2
    fi
  fi
fi

echo "Installed sbx to ${bin_dir}/sbx"

if ! echo "${PATH}" | tr ':' '\n' | grep -qx "${bin_dir}"; then
  echo ""
  echo "Add ${bin_dir} to your PATH:"
  echo ""
  echo "  export PATH=\"${bin_dir}:\$PATH\""
fi
