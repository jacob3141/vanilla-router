#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# All checks without root that must pass before every commit (CONTRIBUTING.md):
#   gofmt · go vet (Linux and Windows) · go test · SPDX lines and package sources (test/check-main.sh)
# Usage: test/check-all.sh            everything
#        test/check-all.sh --quick    without go vet for Windows and package sources (faster)
# Exit code 0 only if everything passed.
set -uo pipefail
export CGO_ENABLED=0 # the project is pure Go, do not depend on a C toolchain
cd "$(dirname "$0")/.."
export GOFLAGS=${GOFLAGS:--buildvcs=false}
QUICK=0
[ "${1:-}" = "--quick" ] && QUICK=1
FAIL=0
step() { printf '\n== %s\n' "$1"; }
bad() { echo "  FAILED: $1"; FAIL=1; }

command -v go >/dev/null || { echo "go is missing, see docs/development.md"; exit 1; }

step "gofmt"
out=$(gofmt -l . 2>&1)
if [ -n "$out" ]; then
	echo "$out" | sed 's/^/  /'
	bad "not formatted, fix with: gofmt -w ."
else
	echo "  OK"
fi

step "go vet (Linux)"
go vet ./... || bad "go vet"

if [ $QUICK = 0 ]; then
	step "go vet (Windows: stick creator)"
	GOOS=windows go vet ./cmd/vr-stick ./internal/windisk ./internal/winres/... ./internal/logo/... || bad "go vet for Windows"
fi

step "go test"
go test ./... 2>&1 | grep -v 'no test files' || true
[ "${PIPESTATUS[0]}" = 0 ] || bad "go test"

step "Project checks (test/check-main.sh)"
if [ $QUICK = 1 ]; then
	bash test/check-main.sh --no-apt || bad "check-main"
else
	bash test/check-main.sh || bad "check-main"
fi

echo
if [ $FAIL = 0 ]; then echo "ALL PASSED"; else echo "NOT PASSED"; fi
exit $FAIL
