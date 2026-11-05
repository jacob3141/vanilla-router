#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Alle Prüfungen ohne Root-Rechte, die vor jedem Commit grün sein müssen (CONTRIBUTING.md):
#   gofmt · go vet (Linux und Windows) · go test · SPDX-Zeilen und Paketquellen (test/check-main.sh)
# Aufruf: test/check-all.sh            alles
#         test/check-all.sh --quick    ohne go vet für Windows und ohne Paketquellen (schneller)
# Exit-Code 0 nur, wenn alles bestanden hat.
set -uo pipefail
cd "$(dirname "$0")/.."
export GOFLAGS=${GOFLAGS:--buildvcs=false}
QUICK=0
[ "${1:-}" = "--quick" ] && QUICK=1
FAIL=0
step() { printf '\n== %s\n' "$1"; }
bad() { echo "  FEHLER: $1"; FAIL=1; }

command -v go >/dev/null || { echo "go fehlt, siehe docs/entwicklung.md"; exit 1; }

step "gofmt"
out=$(gofmt -l . 2>&1)
if [ -n "$out" ]; then
	echo "$out" | sed 's/^/  /'
	bad "nicht formatiert, beheben mit: gofmt -w ."
else
	echo "  in Ordnung"
fi

step "go vet (Linux)"
go vet ./... || bad "go vet"

if [ $QUICK = 0 ]; then
	step "go vet (Windows: Stick-Ersteller)"
	GOOS=windows go vet ./cmd/vr-stick ./internal/windisk ./internal/winres/... ./internal/logo/... || bad "go vet für Windows"
fi

step "go test"
go test ./... 2>&1 | grep -v 'no test files' || true
[ "${PIPESTATUS[0]}" = 0 ] || bad "go test"

step "Projektprüfungen (test/check-main.sh)"
if [ $QUICK = 1 ]; then
	bash test/check-main.sh --no-apt || bad "check-main"
else
	bash test/check-main.sh || bad "check-main"
fi

echo
if [ $FAIL = 0 ]; then echo "ALLES BESTANDEN"; else echo "NICHT BESTANDEN"; fi
exit $FAIL
