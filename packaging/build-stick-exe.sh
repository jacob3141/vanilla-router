#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Baut den Stick-Ersteller für Windows (PLAN.md 3.12, T-M4-08) → dist/vanilla-router-stick.exe
#   VR_MANIFEST_URL  Adresse von manifest.json (https), leer: nur lokale Abbild-Dateien (Q-19)
#   VR_PUBLIC_KEYS   öffentliche Schlüssel (base64, mit Komma getrennt) aus „vr-sign keygen“
# Aufruf: packaging/build-stick-exe.sh [Version]
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=${1:-0.0.0~dev$(date -u +%Y%m%d%H%M)}
: "${VR_MANIFEST_URL:=}" "${VR_PUBLIC_KEYS:=}"
for v in "$VR_MANIFEST_URL" "$VR_PUBLIC_KEYS"; do
	[[ $v =~ ^[A-Za-z0-9:/._~+=,%-]*$ ]] || { echo "ungültige Angabe: $v" >&2; exit 1; }
done
[ -n "$VR_PUBLIC_KEYS" ] || echo "Warnung: kein Schlüssel, der Ersteller wird jedes Abbild ablehnen" >&2

go generate ./cmd/vr-stick
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
	-ldflags "-H windowsgui -s -w -X main.version=$VERSION -X main.manifestURL=$VR_MANIFEST_URL -X main.publicKeys=$VR_PUBLIC_KEYS" \
	-o dist/vanilla-router-stick.exe ./cmd/vr-stick
ls -l dist/vanilla-router-stick.exe
sha256sum dist/vanilla-router-stick.exe
