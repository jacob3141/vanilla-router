#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds a test package for the stick creator, without a real image (T-M4-07) and without the real
# signing key (Q-19):
#   dev/stick-test.key, .pub          test key (dev/ is excluded by .gitignore)
#   dist/stick-test/                  test exe with built-in test key, test image (64 MB, not
#                                     bootable, FAT16 with vr/stick.json), manifest.json and .sig
#
# On Windows: start vanilla-router-stick-TEST.exe, "Choose image file …", take the .img.gz from
# dist/stick-test. Afterwards X:\vr\stick.json contains what was taken over from Windows.
# WARNING: the chosen stick is completely erased.
#
# Needs go and dosfstools (mkfs.fat, fsck.fat). Usage: test/stick/test-package.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
export GOFLAGS=${GOFLAGS:--buildvcs=false}
for t in go mkfs.fat fsck.fat; do
	command -v "$t" >/dev/null || [ -x "/usr/sbin/$t" ] || { echo "$t is missing (sudo apt install golang-go dosfstools)" >&2; exit 1; }
done
PATH=$PATH:/usr/sbin

mkdir -p dev dist/stick-test
rm -f dist/stick-test/*
if [ ! -f dev/stick-test.key ]; then
	go run ./cmd/vr-sign keygen -o dev/stick-test.key >dev/stick-test.pub
	echo "new test key: dev/stick-test.key"
fi
PUB=$(cat dev/stick-test.pub)

W=$(mktemp -d)
trap 'rm -rf "$W"' EXIT # build-stick-exe.sh removes its .syso itself
truncate -s 63M "$W/part.img"
mkfs.fat -F 16 -n VRTEST -h 2048 -i 56524d54 "$W/part.img" >/dev/null
go run ./test/stick/mkimage "$W/part.img" "$W/test.img"
tail -c +1048577 "$W/test.img" >"$W/check.img" # Partition ab 1 MiB
fsck.fat -n "$W/check.img" >/dev/null || { echo "file system in the test image is broken" >&2; exit 1; }

go run ./cmd/vr-sign manifest -key dev/stick-test.key -image "$W/test.img" -version 0.0.0-test \
	-name vanilla-router-test -gzip -o dist/stick-test
go run ./cmd/vr-sign verify -pub "$PUB" dist/stick-test/manifest.json

VR_PUBLIC_KEYS=$PUB bash packaging/build-stick-exe.sh 0.0.0-test >/dev/null
mv dist/vanilla-router-stick.exe dist/stick-test/vanilla-router-stick-TEST.exe
ls -l dist/stick-test
