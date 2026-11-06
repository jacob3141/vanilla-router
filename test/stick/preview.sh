#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds a preview of the stick creator that starts without administrator rights (manifest with
# asInvoker instead of requireAdministrator). For working on the window: list, texts, buttons, colors.
# Writing to a stick does not work with it, Windows denies access without rights.
# Usage: test/stick/preview.sh   → dist/vr-stick-preview.exe
set -euo pipefail
cd "$(dirname "$0")/../.."
export GOFLAGS=${GOFLAGS:--buildvcs=false}
SYSO=cmd/vr-stick/rsrc_windows_amd64.syso
trap 'rm -f "$SYSO" dist/preview.manifest' EXIT
mkdir -p dist
sed 's/requireAdministrator/asInvoker/' cmd/vr-stick/app.manifest >dist/preview.manifest
go run ./internal/winres/mkres -o "$SYSO" dist/preview.manifest
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -X main.version=preview" \
	-o dist/vr-stick-preview.exe ./cmd/vr-stick
ls -l dist/vr-stick-preview.exe
