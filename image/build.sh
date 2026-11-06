#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds the boot stick (M3):
#   1. package vanilla-router (packaging/build-deb.sh)
#   2. target system as rootfs.tar.zst (image/target)
#   3. stick image with the assistant and target system (image/stick) → dist/vanilla-router-stick.raw
# Needs mkosi (≥ 25), root or unprivileged user namespaces, internet (Ubuntu archive).
# Writing to a stick: sudo dd if=dist/vanilla-router-stick.raw of=/dev/sdX bs=4M conv=fsync status=progress
set -euo pipefail
cd "$(dirname "$0")/.."
command -v mkosi >/dev/null || { echo "mkosi is missing (sudo apt install mkosi)" >&2; exit 1; }
VERSION=${1:-0.0.0~dev$(date -u +%Y%m%d%H%M)}

bash packaging/build-deb.sh "$VERSION"
# The local package folder needs an index
(cd dist && dpkg-scanpackages --multiversion . /dev/null > Packages)

list() { cat "$@" | grep -v '^\s*#' | awk 'NF {print $1}' | paste -sd, -; }

echo "== Target system"
mkosi -C image/target --package="$(list image/packages.txt image/packages-image.txt)" \
	--output-directory="$PWD/dist" -f build
mkdir -p dist/stick-extra/usr/share/vanilla-router
mv dist/rootfs.tar.zst dist/stick-extra/usr/share/vanilla-router/rootfs.tar.zst

echo "== Stick"
mkosi -C image/stick --package="$(list image/packages-stick.txt)" --output-directory="$PWD/dist" -f build
ls -lh dist/vanilla-router-stick*
