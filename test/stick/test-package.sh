#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Baut ein Test-Paket für den Stick-Ersteller, ohne echtes Abbild (T-M4-07) und ohne den echten
# Signierschlüssel (Q-19):
#   dev/stick-test.key, .pub          Testschlüssel (dev/ ist per .gitignore ausgeschlossen)
#   dist/stick-test/                  Test-Exe mit eingebautem Testschlüssel, Test-Abbild (64 MB, nicht
#                                     bootfähig, FAT16 mit vr/stick.json), manifest.json und .sig
#
# Unter Windows: vanilla-router-stick-TEST.exe starten, „Abbild-Datei wählen …“, die .img.gz aus
# dist/stick-test nehmen. Danach steht in X:\vr\stick.json, was aus Windows übernommen wurde.
# ACHTUNG: Der gewählte Stick wird dabei vollständig gelöscht.
#
# Braucht go und dosfstools (mkfs.fat, fsck.fat). Aufruf: test/stick/test-package.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
export GOFLAGS=${GOFLAGS:--buildvcs=false}
for t in go mkfs.fat fsck.fat; do
	command -v "$t" >/dev/null || [ -x "/usr/sbin/$t" ] || { echo "$t fehlt (sudo apt install golang-go dosfstools)" >&2; exit 1; }
done
PATH=$PATH:/usr/sbin

mkdir -p dev dist/stick-test
rm -f dist/stick-test/*
if [ ! -f dev/stick-test.key ]; then
	go run ./cmd/vr-sign keygen -o dev/stick-test.key >dev/stick-test.pub
	echo "neuer Testschlüssel: dev/stick-test.key"
fi
PUB=$(cat dev/stick-test.pub)

W=$(mktemp -d)
trap 'rm -rf "$W"' EXIT # build-stick-exe.sh räumt seine .syso selbst weg
truncate -s 63M "$W/part.img"
mkfs.fat -F 16 -n VRTEST -h 2048 -i 56524d54 "$W/part.img" >/dev/null
go run ./test/stick/mkimage "$W/part.img" "$W/test.img"
tail -c +1048577 "$W/test.img" >"$W/check.img" # Partition ab 1 MiB
fsck.fat -n "$W/check.img" >/dev/null || { echo "Dateisystem im Test-Abbild fehlerhaft" >&2; exit 1; }

go run ./cmd/vr-sign manifest -key dev/stick-test.key -image "$W/test.img" -version 0.0.0-test \
	-name vanilla-router-test -gzip -o dist/stick-test
go run ./cmd/vr-sign verify -pub "$PUB" dist/stick-test/manifest.json

VR_PUBLIC_KEYS=$PUB bash packaging/build-stick-exe.sh 0.0.0-test >/dev/null
mv dist/vanilla-router-stick.exe dist/stick-test/vanilla-router-stick-TEST.exe
ls -l dist/stick-test
