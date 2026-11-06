#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds the stick creator for Windows (PLAN.md 3.12, T-M4-08) → dist/vanilla-router-stick.exe
#   VR_MANIFEST_URL  URL of manifest.json (https), empty: local image files only (Q-19)
#   VR_PUBLIC_KEYS   public keys (base64, comma-separated) from "vr-sign keygen"
# Usage: packaging/build-stick-exe.sh [version]
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=${1:-0.0.0~dev$(date -u +%Y%m%d%H%M)}
: "${VR_MANIFEST_URL:=}" "${VR_PUBLIC_KEYS:=}"
for v in "$VR_MANIFEST_URL" "$VR_PUBLIC_KEYS"; do
	[[ $v =~ ^[A-Za-z0-9:/._~+=,%-]*$ ]] || { echo "invalid value: $v" >&2; exit 1; }
done
[ -n "$VR_PUBLIC_KEYS" ] || echo "Warning: no key, the creator will reject every image" >&2

# The .syso only for this build: if it stays in the package folder, it also ends up in test binaries,
# and "go test ./cmd/vr-stick" then requires administrator rights on Windows.
trap 'rm -f cmd/vr-stick/*.syso' EXIT
go generate ./cmd/vr-stick
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
	-ldflags "-H windowsgui -s -w -X main.version=$VERSION -X main.manifestURL=$VR_MANIFEST_URL -X main.publicKeys=$VR_PUBLIC_KEYS" \
	-o dist/vanilla-router-stick.exe ./cmd/vr-stick
ls -l dist/vanilla-router-stick.exe
sha256sum dist/vanilla-router-stick.exe
