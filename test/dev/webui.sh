#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Starts a test instance of the web interface on the development machine, without root and without
# changing anything on the system:
#   - Helper in dry-run mode: writes generated files only under $WORK/root, loads and starts nothing.
#   - Web server with --dev: HTTP instead of HTTPS, only on 127.0.0.1.
#   - config.json from `vrctl default` with example MACs (02:00:… = locally administered, no real devices).
#   - Random test password, kept in dev/test-credentials.txt (excluded by .gitignore).
#
# Usage: test/dev/webui.sh [port]          default 9443, then open http://127.0.0.1:9443
#        VR_DETECTED=pppoe test/dev/webui.sh   pretend the assistant detected PPPoE over VLAN 7
#        VR_WIFI=1 test/dev/webui.sh           simulate a Wi-Fi adapter, the page appears (F-WIFI-04)
# Stop with Ctrl+C. Work folder: test/dev/work (recreated at every start).
set -euo pipefail
export CGO_ENABLED=0 # the project is pure Go, do not depend on a C toolchain
cd "$(dirname "$0")/../.."
PORT=${1:-9443}
WORK=test/dev/work
# Unix sockets do not work on Windows drives in WSL (/mnt/c), so they live in a temporary folder.
SOCKS=$(mktemp -d)
export GOFLAGS=${GOFLAGS:--buildvcs=false}
command -v go >/dev/null || { echo "go is missing, see docs/development.md" >&2; exit 1; }

rm -rf "$WORK"
mkdir -p "$WORK/bin" "$WORK/root/etc/vanilla-router" "$WORK/data" dev
echo "building vr-webui and vrctl …"
go build -o "$WORK/bin/" ./cmd/vr-webui ./cmd/vrctl

CFG="$WORK/root/etc/vanilla-router/config.json"
"$WORK/bin/vrctl" default --wan-mac 02:00:00:00:00:01 --lan-mac 02:00:00:00:00:02,02:00:00:00:00:03 >"$CFG"
if [ "${VR_DETECTED:-}" = pppoe ]; then
	# Result of detection as from the assistant (F-INST-15): the web interface then asks for credentials
	sed -i '0,/"type": "dhcp",/s//"type": "dhcp", "detected": {"method": "pppoe", "vlan": 7},/' "$CFG"
fi

# VR_LOCALE=de_DE.UTF-8 test/dev/webui.sh shows the web interface in German (D-16)
if [ -n "${VR_LOCALE:-}" ]; then
	sed -i "s/\"locale\": \"[^\"]*\"/\"locale\": \"$VR_LOCALE\"/" "$CFG"
fi

PW_FILE=dev/test-credentials.txt
if [ ! -s "$PW_FILE" ]; then
	(umask 077; head -c 18 /dev/urandom | base64 | tr -d '/+=\n' >"$PW_FILE")
fi
{ cat "$PW_FILE"; echo; } | "$WORK/bin/vr-webui" set-password --data "$WORK/data" 2>/dev/null

FAKE_WIFI=()
[ -n "${VR_WIFI:-}" ] && FAKE_WIFI=(--fake-wifi)

cleanup() { kill $(jobs -p) 2>/dev/null || true; rm -rf "$SOCKS"; }
trap cleanup EXIT INT TERM

"$WORK/bin/vr-webui" helper --dry-run --root "$WORK/root" --allow-user "$(id -un)" \
	--socket "$SOCKS/helper.sock" "${FAKE_WIFI[@]}" >"$WORK/helper.log" 2>&1 &
for _ in $(seq 50); do [ -S "$SOCKS/helper.sock" ] && break; sleep 0.1; done
[ -S "$SOCKS/helper.sock" ] || { cat "$WORK/helper.log"; exit 1; }

"$WORK/bin/vr-webui" serve --dev --config "$CFG" --data "$WORK/data" --helper "$SOCKS/helper.sock" \
	--files "$SOCKS/files.sock" --listen "127.0.0.1:$PORT" --redirect off \
	--origins "http://127.0.0.1:$PORT,http://localhost:$PORT" "${FAKE_WIFI[@]}" >"$WORK/serve.log" 2>&1 &

for _ in $(seq 50); do
	if (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null; then break; fi
	sleep 0.1
done
cat <<EOF

Test instance running: http://127.0.0.1:$PORT
  Password:        in $PW_FILE (only for this test instance)
  Generated files: $WORK/root/etc/…  (the helper only writes there)
  Logs:            $WORK/helper.log, $WORK/serve.log
Stop with Ctrl+C.
EOF
wait
